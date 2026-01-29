// Local development proxy that simulates the Cloudflare Worker
// Accepts WebSocket from ssh-relay and proxies to local container's HTTP API
package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"sync"
	"time"

	"github.com/gorilla/websocket"
)

var upgrader = websocket.Upgrader{
	CheckOrigin: func(r *http.Request) bool { return true },
}

func main() {
	port := os.Getenv("PROXY_PORT")
	if port == "" {
		port = "8081"
	}

	containerURL := os.Getenv("CONTAINER_URL")
	if containerURL == "" {
		containerURL = "http://localhost:8080"
	}

	http.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("OK"))
	})

	http.HandleFunc("/ws", func(w http.ResponseWriter, r *http.Request) {
		handleWebSocket(w, r, containerURL)
	})

	log.Printf("Local proxy listening on :%s, forwarding to %s", port, containerURL)
	if err := http.ListenAndServe(":"+port, nil); err != nil {
		log.Fatal(err)
	}
}

func handleWebSocket(w http.ResponseWriter, r *http.Request, containerURL string) {
	// Extract session info from headers
	cols := r.Header.Get("X-Cols")
	rows := r.Header.Get("X-Rows")
	repo := r.Header.Get("X-Repo")
	sessionID := r.Header.Get("X-Session-ID")

	log.Printf("New session: %s (cols=%s, rows=%s, repo=%s)", sessionID[:16], cols, rows, repo)

	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		log.Printf("WebSocket upgrade error: %v", err)
		return
	}
	defer conn.Close()

	// Initialize the container's PTY
	initMsg := map[string]interface{}{
		"type": "init",
		"cols": parseIntOrDefault(cols, 80),
		"rows": parseIntOrDefault(rows, 24),
	}
	if repo != "" {
		initMsg["repo"] = repo
	}

	initBody, _ := json.Marshal(initMsg)
	resp, err := http.Post(containerURL+"/init", "application/json", bytes.NewReader(initBody))
	if err != nil {
		log.Printf("Failed to init container: %v", err)
		sendError(conn, "Failed to initialize container: "+err.Error())
		return
	}
	resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		log.Printf("Container init failed with status: %d", resp.StatusCode)
		sendError(conn, fmt.Sprintf("Container init failed: %d", resp.StatusCode))
		return
	}

	log.Printf("Session %s: PTY initialized", sessionID[:16])

	var wg sync.WaitGroup
	done := make(chan struct{})

	// Poll container for output and send to WebSocket
	wg.Add(1)
	go func() {
		defer wg.Done()
		for {
			select {
			case <-done:
				return
			default:
				resp, err := http.Get(containerURL + "/read")
				if err != nil {
					time.Sleep(100 * time.Millisecond)
					continue
				}

				body, _ := io.ReadAll(resp.Body)
				resp.Body.Close()

				if len(body) > 0 {
					// Forward each line as a separate message
					for _, line := range bytes.Split(body, []byte("\n")) {
						if len(line) == 0 {
							continue
						}
						if err := conn.WriteMessage(websocket.TextMessage, line); err != nil {
							log.Printf("WebSocket write error: %v", err)
							return
						}

						// Check for exit message
						var msg map[string]interface{}
						if json.Unmarshal(line, &msg) == nil {
							if msg["type"] == "exit" {
								log.Printf("Session %s: container exited", sessionID[:16])
								close(done)
								return
							}
						}
					}
				}

				time.Sleep(50 * time.Millisecond)
			}
		}
	}()

	// Read from WebSocket and forward to container
	wg.Add(1)
	go func() {
		defer wg.Done()
		for {
			select {
			case <-done:
				return
			default:
				_, message, err := conn.ReadMessage()
				if err != nil {
					if websocket.IsUnexpectedCloseError(err, websocket.CloseGoingAway, websocket.CloseNormalClosure) {
						log.Printf("WebSocket read error: %v", err)
					}
					close(done)
					return
				}

				// Parse message to determine endpoint
				var msg map[string]interface{}
				if err := json.Unmarshal(message, &msg); err != nil {
					log.Printf("Failed to parse message: %v", err)
					continue
				}

				msgType, _ := msg["type"].(string)

				switch msgType {
				case "data", "resize":
					resp, err := http.Post(containerURL+"/write", "application/json", bytes.NewReader(message))
					if err != nil {
						log.Printf("Failed to write to container: %v", err)
						continue
					}
					resp.Body.Close()

				case "ping":
					// Respond with pong
					pong := map[string]interface{}{
						"type":      "pong",
						"timestamp": msg["timestamp"],
					}
					pongData, _ := json.Marshal(pong)
					conn.WriteMessage(websocket.TextMessage, pongData)

				case "init":
					// Already initialized, but handle resize if dimensions changed
					if cols, ok := msg["cols"].(float64); ok {
						if rows, ok := msg["rows"].(float64); ok {
							resizeMsg := map[string]interface{}{
								"type": "resize",
								"cols": int(cols),
								"rows": int(rows),
							}
							resizeBody, _ := json.Marshal(resizeMsg)
							resp, _ := http.Post(containerURL+"/write", "application/json", bytes.NewReader(resizeBody))
							if resp != nil {
								resp.Body.Close()
							}
						}
					}
				}
			}
		}
	}()

	wg.Wait()
	log.Printf("Session %s: ended", sessionID[:16])
}

func sendError(conn *websocket.Conn, message string) {
	errMsg := map[string]interface{}{
		"type":    "error",
		"message": message,
	}
	data, _ := json.Marshal(errMsg)
	conn.WriteMessage(websocket.TextMessage, data)
}

func parseIntOrDefault(s string, def int) int {
	var result int
	if _, err := fmt.Sscanf(s, "%d", &result); err != nil {
		return def
	}
	return result
}
