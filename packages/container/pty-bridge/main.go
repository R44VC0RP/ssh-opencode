package main

import (
	"bufio"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"unsafe"

	"github.com/creack/pty"
)

// Message types for the protocol
type MessageType string

const (
	MsgInit   MessageType = "init"
	MsgData   MessageType = "data"
	MsgResize MessageType = "resize"
	MsgExit   MessageType = "exit"
)

// Message is the JSON protocol message
type Message struct {
	Type MessageType `json:"type"`
	// For init
	Cols int    `json:"cols,omitempty"`
	Rows int    `json:"rows,omitempty"`
	Repo string `json:"repo,omitempty"`
	// For data (base64 encoded)
	Data string `json:"data,omitempty"`
	// For exit
	Code int `json:"code,omitempty"`
}

func main() {
	port := os.Getenv("PTY_BRIDGE_PORT")
	if port == "" {
		port = "8080"
	}

	listener, err := net.Listen("tcp", ":"+port)
	if err != nil {
		log.Fatalf("Failed to listen on port %s: %v", port, err)
	}
	defer listener.Close()

	log.Printf("PTY bridge listening on :%s", port)

	for {
		conn, err := listener.Accept()
		if err != nil {
			log.Printf("Accept error: %v", err)
			continue
		}
		go handleConnection(conn)
	}
}

func handleConnection(conn net.Conn) {
	defer conn.Close()
	log.Printf("New connection from %s", conn.RemoteAddr())

	reader := bufio.NewReader(conn)

	// Read init message (first line is JSON)
	line, err := reader.ReadBytes('\n')
	if err != nil {
		log.Printf("Failed to read init message: %v", err)
		return
	}

	var initMsg Message
	if err := json.Unmarshal(line, &initMsg); err != nil {
		log.Printf("Failed to parse init message: %v", err)
		return
	}

	if initMsg.Type != MsgInit {
		log.Printf("Expected init message, got: %s", initMsg.Type)
		return
	}

	log.Printf("Init: cols=%d, rows=%d, repo=%s", initMsg.Cols, initMsg.Rows, initMsg.Repo)

	// Determine working directory
	workDir := getWorkDir(initMsg.Repo)

	// Handle GitHub repo cloning if specified
	if initMsg.Repo != "" {
		if err := ensureRepo(initMsg.Repo, workDir); err != nil {
			log.Printf("Failed to ensure repo: %v", err)
			// Send error message but continue anyway
			sendMessage(conn, Message{
				Type: MsgData,
				Data: base64.StdEncoding.EncodeToString(
					[]byte(fmt.Sprintf("Warning: failed to clone repo: %v\r\n", err)),
				),
			})
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
		Rows: uint16(initMsg.Rows),
		Cols: uint16(initMsg.Cols),
	})
	if err != nil {
		log.Printf("Failed to start PTY: %v", err)
		sendMessage(conn, Message{Type: MsgExit, Code: 1})
		return
	}
	defer ptmx.Close()

	var wg sync.WaitGroup
	done := make(chan struct{})

	// PTY output -> TCP (encode as base64 data messages)
	wg.Add(1)
	go func() {
		defer wg.Done()
		buf := make([]byte, 32*1024)
		for {
			select {
			case <-done:
				return
			default:
				n, err := ptmx.Read(buf)
				if err != nil {
					if err != io.EOF {
						log.Printf("PTY read error: %v", err)
					}
					return
				}
				msg := Message{
					Type: MsgData,
					Data: base64.StdEncoding.EncodeToString(buf[:n]),
				}
				if err := sendMessage(conn, msg); err != nil {
					log.Printf("Failed to send data: %v", err)
					return
				}
			}
		}
	}()

	// TCP input -> PTY (handle control messages and data)
	wg.Add(1)
	go func() {
		defer wg.Done()
		defer close(done)

		scanner := bufio.NewScanner(reader)
		// Increase buffer size for large messages
		scanner.Buffer(make([]byte, 1024*1024), 1024*1024)

		for scanner.Scan() {
			line := scanner.Bytes()
			if len(line) == 0 {
				continue
			}

			var msg Message
			if err := json.Unmarshal(line, &msg); err != nil {
				log.Printf("Failed to parse message: %v", err)
				continue
			}

			switch msg.Type {
			case MsgData:
				// Decode base64 and write to PTY
				data, err := base64.StdEncoding.DecodeString(msg.Data)
				if err != nil {
					log.Printf("Failed to decode data: %v", err)
					continue
				}
				if _, err := ptmx.Write(data); err != nil {
					log.Printf("Failed to write to PTY: %v", err)
					return
				}

			case MsgResize:
				setWinsize(ptmx, msg.Cols, msg.Rows)

			case MsgExit:
				log.Printf("Received exit message")
				return
			}
		}

		if err := scanner.Err(); err != nil {
			log.Printf("Scanner error: %v", err)
		}
	}()

	// Wait for process to exit
	exitCode := 0
	if err := cmd.Wait(); err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			exitCode = exitErr.ExitCode()
		}
	}

	// Signal done and wait for goroutines
	select {
	case <-done:
	default:
		close(done)
	}
	wg.Wait()

	// Send exit message
	sendMessage(conn, Message{Type: MsgExit, Code: exitCode})
	log.Printf("Connection closed, exit code: %d", exitCode)
}

func sendMessage(conn net.Conn, msg Message) error {
	data, err := json.Marshal(msg)
	if err != nil {
		return err
	}
	data = append(data, '\n')
	_, err = conn.Write(data)
	return err
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

	// Ensure base directory exists
	os.MkdirAll(baseDir, 0755)

	if repo == "" {
		return baseDir
	}

	// Extract repo name from path (e.g., "user/repo" -> "repo")
	parts := strings.Split(repo, "/")
	repoName := parts[len(parts)-1]
	repoName = strings.TrimSuffix(repoName, ".git")

	return filepath.Join(baseDir, repoName)
}

func ensureRepo(repo, workDir string) error {
	// Check if directory already exists and has .git
	gitDir := filepath.Join(workDir, ".git")
	if _, err := os.Stat(gitDir); err == nil {
		log.Printf("Repo already exists at %s, pulling latest", workDir)
		cmd := exec.Command("git", "pull", "--ff-only")
		cmd.Dir = workDir
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr
		// Don't fail if pull fails (might be detached HEAD, etc.)
		cmd.Run()
		return nil
	}

	// Build GitHub URL
	repoURL := repo
	if !strings.HasPrefix(repo, "http") && !strings.HasPrefix(repo, "git@") {
		// Assume it's a GitHub repo in format "user/repo"
		repoURL = "https://github.com/" + repo
	}

	log.Printf("Cloning %s to %s", repoURL, workDir)

	cmd := exec.Command("git", "clone", "--depth=1", repoURL, workDir)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}
