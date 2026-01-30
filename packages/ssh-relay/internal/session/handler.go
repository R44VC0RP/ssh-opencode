package session

import (
	"encoding/base64"
	"fmt"
	"io"
	"log"
	"net/http"
	"sync"
	"sync/atomic"
	"time"

	"github.com/gliderlabs/ssh"
	"github.com/gorilla/websocket"

	"ssh-relay/internal/auth"
	"ssh-relay/internal/github"
	"ssh-relay/internal/proxy"
)

// Config holds session handler configuration
type Config struct {
	WorkerURL    string
	AuthSecret   string
	PingInterval time.Duration
}

// safeConn wraps a WebSocket connection with a mutex for safe concurrent writes
type safeConn struct {
	conn   *websocket.Conn
	mu     sync.Mutex
	closed atomic.Bool
}

func (c *safeConn) WriteMessage(messageType int, data []byte) error {
	if c.closed.Load() {
		return websocket.ErrCloseSent
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.closed.Load() {
		return websocket.ErrCloseSent
	}
	return c.conn.WriteMessage(messageType, data)
}

func (c *safeConn) ReadMessage() (int, []byte, error) {
	return c.conn.ReadMessage()
}

func (c *safeConn) Close() error {
	c.closed.Store(true)
	return c.conn.Close()
}

// Handler creates an SSH session handler that proxies to Cloudflare Worker
func Handler(cfg Config, registry *auth.Registry) ssh.Handler {
	return func(s ssh.Session) {
		fingerprint := auth.GetFingerprint(s.Context())
		if fingerprint == "" {
			io.WriteString(s, "Authentication failed\r\n")
			s.Exit(1)
			return
		}

		// Update last used time
		registry.UpdateLastUsed(fingerprint)

		// Check for PTY
		pty, winCh, isPty := s.Pty()
		if !isPty {
			io.WriteString(s, "PTY required. Use: ssh -t ...\r\n")
			s.Exit(1)
			return
		}

		// Parse command for GitHub repo
		cmd := s.Command()
		var repo string
		if len(cmd) > 0 {
			repo = github.ParseRepo(cmd[0])
			if repo != "" {
				log.Printf("Session %s: cloning repo %s", fingerprint[:16], repo)
			}
		}

		log.Printf("Session %s: starting (cols=%d, rows=%d, repo=%s)",
			fingerprint[:16], pty.Window.Width, pty.Window.Height, repo)

		// Connect to Cloudflare Worker via WebSocket
		headers := http.Header{}
		headers.Set("X-Session-ID", fingerprint)
		headers.Set("X-Cols", fmt.Sprintf("%d", pty.Window.Width))
		headers.Set("X-Rows", fmt.Sprintf("%d", pty.Window.Height))
		if repo != "" {
			headers.Set("X-Repo", repo)
		}
		if cfg.AuthSecret != "" {
			headers.Set("X-Auth-Secret", cfg.AuthSecret)
		}

		dialer := websocket.Dialer{
			HandshakeTimeout: 30 * time.Second,
		}
		rawConn, resp, err := dialer.Dial(cfg.WorkerURL, headers)
		if err != nil {
			log.Printf("Session %s: WebSocket dial error: %v", fingerprint[:16], err)
			if resp != nil {
				log.Printf("Session %s: HTTP status: %d", fingerprint[:16], resp.StatusCode)
			}
			io.WriteString(s, "Failed to connect to backend\r\n")
			s.Exit(1)
			return
		}
		conn := &safeConn{conn: rawConn}
		defer conn.Close()

		// Send init message
		initMsg := proxy.NewInitMessage(pty.Window.Width, pty.Window.Height, repo)
		data, _ := initMsg.Marshal()
		if err := conn.WriteMessage(websocket.TextMessage, data); err != nil {
			log.Printf("Session %s: failed to send init: %v", fingerprint[:16], err)
			io.WriteString(s, "Failed to initialize session\r\n")
			s.Exit(1)
			return
		}

		var wg sync.WaitGroup
		done := make(chan struct{})

		// Ping goroutine to keep connection alive
		if cfg.PingInterval > 0 {
			wg.Add(1)
			go func() {
				defer wg.Done()
				ticker := time.NewTicker(cfg.PingInterval)
				defer ticker.Stop()
				for {
					select {
					case <-done:
						return
					case <-ticker.C:
						pingMsg := proxy.NewPingMessage(time.Now().UnixMilli())
						data, _ := pingMsg.Marshal()
						if err := conn.WriteMessage(websocket.TextMessage, data); err != nil {
							// Connection closed, exit quietly
							return
						}
					}
				}
			}()
		}

		// SSH input → WebSocket (user keystrokes)
		wg.Add(1)
		go func() {
			defer wg.Done()
			buf := make([]byte, 32*1024)
			for {
				select {
				case <-done:
					return
				default:
					n, err := s.Read(buf)
					if err != nil {
						if err != io.EOF {
							log.Printf("Session %s: SSH read error: %v", fingerprint[:16], err)
						}
						return
					}

					// Send immediately - no buffering delay
					encoded := base64.StdEncoding.EncodeToString(buf[:n])
					msg := proxy.NewDataMessage(encoded)
					data, _ := msg.Marshal()
					if err := conn.WriteMessage(websocket.TextMessage, data); err != nil {
						// Connection closed, exit quietly
						return
					}
				}
			}
		}()

		// WebSocket → SSH (terminal output)
		wg.Add(1)
		go func() {
			defer wg.Done()
			for {
				_, message, err := conn.ReadMessage()
				if err != nil {
					if websocket.IsUnexpectedCloseError(err, websocket.CloseGoingAway, websocket.CloseAbnormalClosure) {
						log.Printf("Session %s: WS read error: %v", fingerprint[:16], err)
					}
					close(done)
					return
				}

				msg, err := proxy.ParseMessage(message)
				if err != nil {
					log.Printf("Session %s: parse error: %v", fingerprint[:16], err)
					continue
				}

				switch msg.Type {
				case proxy.MsgData:
					// Decode base64 and write to SSH
					decoded, err := base64.StdEncoding.DecodeString(msg.Data)
					if err != nil {
						log.Printf("Session %s: base64 decode error: %v", fingerprint[:16], err)
						continue
					}
					s.Write(decoded)

				case proxy.MsgExit:
					log.Printf("Session %s: exit with code %d", fingerprint[:16], msg.Code)
					close(done)
					return

				case proxy.MsgError:
					log.Printf("Session %s: error: %s", fingerprint[:16], msg.Error)
					io.WriteString(s, fmt.Sprintf("Error: %s\r\n", msg.Error))

				case proxy.MsgStatus:
					// Display status message to user
					log.Printf("Session %s: status: %s", fingerprint[:16], msg.Message)
					io.WriteString(s, fmt.Sprintf("\r%s\r\n", msg.Message))

				case proxy.MsgPong:
					// Connection is alive, nothing to do
				}
			}
		}()

		// Window resize events
		wg.Add(1)
		go func() {
			defer wg.Done()
			for {
				select {
				case <-done:
					return
				case win, ok := <-winCh:
					if !ok {
						return
					}
					msg := proxy.NewResizeMessage(win.Width, win.Height)
					data, _ := msg.Marshal()
					if err := conn.WriteMessage(websocket.TextMessage, data); err != nil {
						// Connection closed, exit quietly
						return
					}
				}
			}
		}()

		// Wait for completion
		wg.Wait()
		log.Printf("Session %s: ended", fingerprint[:16])
		s.Exit(0)
	}
}
