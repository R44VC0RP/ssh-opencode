package main

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"time"
	"unsafe"

	"github.com/creack/pty"
	"github.com/gorilla/websocket"
)

// Message types for the protocol
type MessageType string

const (
	MsgInit   MessageType = "init"
	MsgData   MessageType = "data"
	MsgResize MessageType = "resize"
	MsgExit   MessageType = "exit"
	MsgPing   MessageType = "ping"
	MsgPong   MessageType = "pong"
	MsgError  MessageType = "error"
)

// Message is the JSON protocol message
type Message struct {
	Type      MessageType `json:"type"`
	Cols      int         `json:"cols,omitempty"`
	Rows      int         `json:"rows,omitempty"`
	Repo      string      `json:"repo,omitempty"`
	Data      string      `json:"data,omitempty"` // base64 encoded
	Code      int         `json:"code,omitempty"`
	Message   string      `json:"message,omitempty"`
	Timestamp int64       `json:"timestamp,omitempty"`
}

// OutputBuffer is a thread-safe buffer for PTY output (for HTTP polling)
type OutputBuffer struct {
	mu   sync.Mutex
	data []byte
}

func (b *OutputBuffer) Write(data []byte) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.data = append(b.data, data...)
	// Keep only last 1MB to prevent unbounded growth
	if len(b.data) > 1024*1024 {
		b.data = b.data[len(b.data)-1024*1024:]
	}
}

func (b *OutputBuffer) Read() []byte {
	b.mu.Lock()
	defer b.mu.Unlock()
	data := b.data
	b.data = nil
	return data
}

// PTYSession manages a PTY instance
type PTYSession struct {
	mu        sync.RWMutex
	ptmx      *os.File
	cmd       *exec.Cmd
	output    *OutputBuffer // Buffer for HTTP polling
	done      chan struct{}
	exitCode  int
	isRunning bool
	workDir   string

	// WebSocket clients for streaming output
	clientsMu sync.RWMutex
	clients   map[*websocket.Conn]bool
}

// Broadcast sends a message to all connected WebSocket clients
func (s *PTYSession) Broadcast(msg Message) {
	data, err := json.Marshal(msg)
	if err != nil {
		return
	}

	s.clientsMu.RLock()
	defer s.clientsMu.RUnlock()

	for client := range s.clients {
		// Use longer timeout for reliability - 5 seconds
		client.SetWriteDeadline(time.Now().Add(5 * time.Second))
		if err := client.WriteMessage(websocket.TextMessage, data); err != nil {
			log.Printf("WebSocket write error: %v", err)
		}
	}
}

// AddClient registers a WebSocket client
func (s *PTYSession) AddClient(conn *websocket.Conn) {
	s.clientsMu.Lock()
	s.clients[conn] = true
	s.clientsMu.Unlock()
}

// RemoveClient unregisters a WebSocket client
func (s *PTYSession) RemoveClient(conn *websocket.Conn) {
	s.clientsMu.Lock()
	delete(s.clients, conn)
	s.clientsMu.Unlock()
}

var (
	session     *PTYSession
	sessionOnce sync.Once
	upgrader    = websocket.Upgrader{
		CheckOrigin: func(r *http.Request) bool { return true },
	}
)

func main() {
	port := os.Getenv("PTY_BRIDGE_PORT")
	if port == "" {
		port = "8080"
	}

	// HTTP endpoints
	http.HandleFunc("/ping", handlePing)
	http.HandleFunc("/init", handleInit)
	http.HandleFunc("/write", handleWrite)
	http.HandleFunc("/read", handleRead)
	http.HandleFunc("/resize", handleResize)
	http.HandleFunc("/status", handleStatus)
	http.HandleFunc("/writeread", handleWriteRead) // Combined write+read for low latency

	// WebSocket endpoint for streaming (future use)
	http.HandleFunc("/ws", handleWebSocket)

	log.Printf("PTY bridge listening on :%s (HTTP + WebSocket)", port)
	if err := http.ListenAndServe(":"+port, nil); err != nil {
		log.Fatalf("Failed to start server: %v", err)
	}
}

// handleWebSocket handles WebSocket connections for real-time streaming
func handleWebSocket(w http.ResponseWriter, r *http.Request) {
	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		log.Printf("WebSocket upgrade error: %v", err)
		return
	}
	defer conn.Close()

	log.Printf("WebSocket client connected")

	// Wait for init message
	_, message, err := conn.ReadMessage()
	if err != nil {
		log.Printf("WebSocket read error: %v", err)
		return
	}

	var initMsg Message
	if err := json.Unmarshal(message, &initMsg); err != nil {
		sendWSError(conn, "Failed to parse init message")
		return
	}

	if initMsg.Type != MsgInit {
		sendWSError(conn, "Expected init message")
		return
	}

	// Initialize session
	var initErr error
	sessionOnce.Do(func() {
		initErr = initializeSession(initMsg.Cols, initMsg.Rows, initMsg.Repo)
	})

	if initErr != nil {
		sendWSError(conn, "Failed to initialize session: "+initErr.Error())
		return
	}

	// If session already exists, just resize
	if session != nil && session.isRunning {
		session.mu.Lock()
		setWinsize(session.ptmx, initMsg.Cols, initMsg.Rows)
		session.mu.Unlock()
	}

	// Register this client for broadcasts
	session.AddClient(conn)
	defer session.RemoveClient(conn)

	// Send success response
	conn.WriteJSON(Message{Type: MsgPong, Message: "connected"})

	// Handle incoming messages (writes, resizes, pings)
	for {
		_, message, err := conn.ReadMessage()
		if err != nil {
			if websocket.IsUnexpectedCloseError(err, websocket.CloseGoingAway, websocket.CloseAbnormalClosure) {
				log.Printf("WebSocket error: %v", err)
			}
			return
		}

		var msg Message
		if err := json.Unmarshal(message, &msg); err != nil {
			continue
		}

		switch msg.Type {
		case MsgData:
			data, err := base64.StdEncoding.DecodeString(msg.Data)
			if err != nil {
				continue
			}
			session.mu.Lock()
			session.ptmx.Write(data)
			session.mu.Unlock()

		case MsgResize:
			session.mu.Lock()
			setWinsize(session.ptmx, msg.Cols, msg.Rows)
			session.mu.Unlock()

		case MsgPing:
			conn.WriteJSON(Message{Type: MsgPong, Timestamp: msg.Timestamp})
		}
	}
}

func sendWSError(conn *websocket.Conn, message string) {
	conn.WriteJSON(Message{Type: MsgError, Message: message})
}

func handlePing(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(Message{Type: MsgPong, Timestamp: 0})
}

func handleStatus(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	if session == nil {
		json.NewEncoder(w).Encode(map[string]interface{}{
			"initialized": false,
			"running":     false,
		})
		return
	}

	session.mu.RLock()
	defer session.mu.RUnlock()

	session.clientsMu.RLock()
	clientCount := len(session.clients)
	session.clientsMu.RUnlock()

	json.NewEncoder(w).Encode(map[string]interface{}{
		"initialized": true,
		"running":     session.isRunning,
		"exitCode":    session.exitCode,
		"workDir":     session.workDir,
		"wsClients":   clientCount,
	})
}

func handleInit(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var msg Message
	if err := json.NewDecoder(r.Body).Decode(&msg); err != nil {
		sendError(w, "Failed to parse init message: "+err.Error())
		return
	}

	if msg.Type != MsgInit {
		sendError(w, "Expected init message")
		return
	}

	// Initialize session only once
	var initErr error
	sessionOnce.Do(func() {
		initErr = initializeSession(msg.Cols, msg.Rows, msg.Repo)
	})

	if initErr != nil {
		sendError(w, "Failed to initialize session: "+initErr.Error())
		return
	}

	// If already initialized, just resize
	if session != nil && session.isRunning {
		session.mu.Lock()
		setWinsize(session.ptmx, msg.Cols, msg.Rows)
		session.mu.Unlock()
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"success": true,
		"workDir": session.workDir,
	})
}

func initializeSession(cols, rows int, repo string) error {
	log.Printf("Initializing session: cols=%d, rows=%d, repo=%s", cols, rows, repo)

	workDir := getWorkDir(repo)

	// Handle GitHub repo cloning if specified
	if repo != "" {
		if err := ensureRepo(repo, workDir); err != nil {
			log.Printf("Failed to ensure repo: %v", err)
			// Continue anyway
		}
	}

	// Start OpenCode with PTY
	cmd := exec.Command("opencode")
	cmd.Dir = workDir
	cmd.Env = append(os.Environ(),
		"TERM=xterm-256color",
		"COLORTERM=truecolor",
		"HOME=/root",
		"USER=root",
	)

	ptmx, err := pty.StartWithSize(cmd, &pty.Winsize{
		Rows: uint16(rows),
		Cols: uint16(cols),
	})
	if err != nil {
		return fmt.Errorf("failed to start PTY: %w", err)
	}

	session = &PTYSession{
		ptmx:      ptmx,
		cmd:       cmd,
		output:    &OutputBuffer{},
		done:      make(chan struct{}),
		isRunning: true,
		workDir:   workDir,
		clients:   make(map[*websocket.Conn]bool),
	}

	// Start reading PTY output
	go readPTYOutput()

	// Wait for process to exit in background
	go func() {
		exitCode := 0
		if err := cmd.Wait(); err != nil {
			if exitErr, ok := err.(*exec.ExitError); ok {
				exitCode = exitErr.ExitCode()
			}
		}

		session.mu.Lock()
		session.isRunning = false
		session.exitCode = exitCode
		session.mu.Unlock()
		close(session.done)

		// Broadcast exit to WebSocket clients
		session.Broadcast(Message{Type: MsgExit, Code: exitCode})

		log.Printf("OpenCode exited with code: %d - exiting container", exitCode)
		time.Sleep(500 * time.Millisecond)
		os.Exit(exitCode)
	}()

	return nil
}

// readPTYOutput reads from PTY and writes to both buffer (HTTP) and WebSocket clients
func readPTYOutput() {
	buf := make([]byte, 32*1024)
	for {
		select {
		case <-session.done:
			return
		default:
			n, err := session.ptmx.Read(buf)
			if err != nil {
				if err != io.EOF {
					log.Printf("PTY read error: %v", err)
				}
				return
			}
			if n > 0 {
				data := buf[:n]

				// Write to HTTP buffer for polling clients
				session.output.Write(data)

				// Broadcast to WebSocket clients
				session.Broadcast(Message{
					Type: MsgData,
					Data: base64.StdEncoding.EncodeToString(data),
				})
			}
		}
	}
}

func handleWrite(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	if session == nil || !session.isRunning {
		sendError(w, "Session not initialized or not running")
		return
	}

	var msg Message
	if err := json.NewDecoder(r.Body).Decode(&msg); err != nil {
		sendError(w, "Failed to parse message: "+err.Error())
		return
	}

	switch msg.Type {
	case MsgData:
		data, err := base64.StdEncoding.DecodeString(msg.Data)
		if err != nil {
			sendError(w, "Failed to decode data: "+err.Error())
			return
		}

		session.mu.Lock()
		_, err = session.ptmx.Write(data)
		session.mu.Unlock()

		if err != nil {
			sendError(w, "Failed to write to PTY: "+err.Error())
			return
		}

	case MsgResize:
		session.mu.Lock()
		setWinsize(session.ptmx, msg.Cols, msg.Rows)
		session.mu.Unlock()

	default:
		sendError(w, "Unknown message type: "+string(msg.Type))
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]bool{"success": true})
}

func handleRead(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	w.Header().Set("Content-Type", "application/json")

	if session == nil {
		json.NewEncoder(w).Encode(Message{
			Type:    MsgError,
			Message: "Session not initialized",
		})
		return
	}

	// Check if session is still running
	session.mu.RLock()
	isRunning := session.isRunning
	exitCode := session.exitCode
	session.mu.RUnlock()

	// Read buffered output
	data := session.output.Read()

	var messages []Message

	if len(data) > 0 {
		messages = append(messages, Message{
			Type: MsgData,
			Data: base64.StdEncoding.EncodeToString(data),
		})
	}

	// If session ended, send exit message
	if !isRunning {
		messages = append(messages, Message{
			Type: MsgExit,
			Code: exitCode,
		})
	}

	// Write newline-delimited JSON
	for _, msg := range messages {
		jsonData, _ := json.Marshal(msg)
		w.Write(jsonData)
		w.Write([]byte("\n"))
	}
}

// handleWriteRead combines write and read in a single request for lower latency
// Writes to PTY, waits briefly for output, then returns any available data
func handleWriteRead(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	if session == nil || !session.isRunning {
		sendError(w, "Session not initialized or not running")
		return
	}

	var msg Message
	if err := json.NewDecoder(r.Body).Decode(&msg); err != nil {
		sendError(w, "Failed to parse message: "+err.Error())
		return
	}

	// Handle write
	switch msg.Type {
	case MsgData:
		data, err := base64.StdEncoding.DecodeString(msg.Data)
		if err != nil {
			sendError(w, "Failed to decode data: "+err.Error())
			return
		}

		session.mu.Lock()
		_, err = session.ptmx.Write(data)
		session.mu.Unlock()

		if err != nil {
			sendError(w, "Failed to write to PTY: "+err.Error())
			return
		}

	case MsgResize:
		session.mu.Lock()
		setWinsize(session.ptmx, msg.Cols, msg.Rows)
		session.mu.Unlock()

	default:
		sendError(w, "Unknown message type: "+string(msg.Type))
		return
	}

	// No sleep - read whatever is immediately available
	// The PTY output goes to buffer asynchronously via readPTYOutput goroutine

	// Read available output
	w.Header().Set("Content-Type", "application/json")

	session.mu.RLock()
	isRunning := session.isRunning
	exitCode := session.exitCode
	session.mu.RUnlock()

	data := session.output.Read()

	var messages []Message

	if len(data) > 0 {
		messages = append(messages, Message{
			Type: MsgData,
			Data: base64.StdEncoding.EncodeToString(data),
		})
	}

	if !isRunning {
		messages = append(messages, Message{
			Type: MsgExit,
			Code: exitCode,
		})
	}

	for _, m := range messages {
		jsonData, _ := json.Marshal(m)
		w.Write(jsonData)
		w.Write([]byte("\n"))
	}
}

func handleResize(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	if session == nil || !session.isRunning {
		sendError(w, "Session not initialized or not running")
		return
	}

	var msg Message
	if err := json.NewDecoder(r.Body).Decode(&msg); err != nil {
		sendError(w, "Failed to parse message: "+err.Error())
		return
	}

	session.mu.Lock()
	setWinsize(session.ptmx, msg.Cols, msg.Rows)
	session.mu.Unlock()

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]bool{"success": true})
}

func sendError(w http.ResponseWriter, message string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusBadRequest)
	json.NewEncoder(w).Encode(Message{
		Type:    MsgError,
		Message: message,
	})
}

func setWinsize(f *os.File, cols, rows int) {
	ws := &struct {
		Row    uint16
		Col    uint16
		Xpixel uint16
		Ypixel uint16
	}{
		Row: uint16(rows),
		Col: uint16(cols),
	}
	syscall.Syscall(
		syscall.SYS_IOCTL,
		f.Fd(),
		uintptr(syscall.TIOCSWINSZ),
		uintptr(unsafe.Pointer(ws)),
	)
}

func getWorkDir(repo string) string {
	baseDir := "/root/dev"
	os.MkdirAll(baseDir, 0755)

	if repo == "" {
		return baseDir
	}

	parts := strings.Split(repo, "/")
	repoName := parts[len(parts)-1]
	repoName = strings.TrimSuffix(repoName, ".git")

	return filepath.Join(baseDir, repoName)
}

func ensureRepo(repo, workDir string) error {
	gitDir := filepath.Join(workDir, ".git")
	if _, err := os.Stat(gitDir); err == nil {
		log.Printf("Repo already exists at %s, pulling latest", workDir)
		cmd := exec.Command("git", "pull", "--ff-only")
		cmd.Dir = workDir
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr
		cmd.Run()
		return nil
	}

	repoURL := repo
	if !strings.HasPrefix(repo, "http") && !strings.HasPrefix(repo, "git@") {
		repoURL = "https://github.com/" + repo
	}

	log.Printf("Cloning %s to %s", repoURL, workDir)

	cmd := exec.Command("git", "clone", "--depth=1", repoURL, workDir)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}
