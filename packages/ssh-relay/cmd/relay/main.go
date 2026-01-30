package main

import (
	"flag"
	"log"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"github.com/gliderlabs/ssh"

	"ssh-relay/internal/auth"
	"ssh-relay/internal/session"
)

func main() {
	// Command line flags
	var (
		listenAddr  = flag.String("listen", ":22", "Address to listen on")
		hostKeyPath = flag.String("host-key", "", "Path to SSH host key")
		keyDBPath   = flag.String("key-db", "", "Path to authorized keys database")
		workerURL   = flag.String("worker-url", "", "Cloudflare Worker WebSocket URL")
		authSecret  = flag.String("auth-secret", "", "Shared secret for worker authentication")
		autoReg     = flag.Bool("auto-register", true, "Auto-register new SSH keys")
	)
	flag.Parse()

	// Environment variable overrides
	if env := os.Getenv("SSH_LISTEN_ADDR"); env != "" && *listenAddr == ":22" {
		*listenAddr = env
	}
	if env := os.Getenv("SSH_HOST_KEY_PATH"); env != "" && *hostKeyPath == "" {
		*hostKeyPath = env
	}
	if env := os.Getenv("SSH_KEY_DB_PATH"); env != "" && *keyDBPath == "" {
		*keyDBPath = env
	}
	if env := os.Getenv("WORKER_URL"); env != "" && *workerURL == "" {
		*workerURL = env
	}
	if env := os.Getenv("AUTH_SECRET"); env != "" && *authSecret == "" {
		*authSecret = env
	}
	if os.Getenv("AUTO_REGISTER") == "false" {
		*autoReg = false
	}

	// Validate required flags
	if *workerURL == "" {
		log.Fatal("Worker URL is required: --worker-url or WORKER_URL")
	}

	// Set defaults for paths
	if *hostKeyPath == "" {
		*hostKeyPath = "/etc/ssh-opencode/host_key"
	}
	if *keyDBPath == "" {
		*keyDBPath = "/var/lib/ssh-opencode/keys.db"
	}

	// Ensure directories exist
	if err := os.MkdirAll(filepath.Dir(*hostKeyPath), 0700); err != nil {
		log.Fatalf("Failed to create host key directory: %v", err)
	}
	if err := os.MkdirAll(filepath.Dir(*keyDBPath), 0700); err != nil {
		log.Fatalf("Failed to create key DB directory: %v", err)
	}

	// Initialize key registry
	registry, err := auth.NewRegistry(*keyDBPath)
	if err != nil {
		log.Fatalf("Failed to initialize key registry: %v", err)
	}
	defer registry.Close()

	count, _ := registry.Count()
	log.Printf("Key registry initialized with %d keys", count)

	// Session configuration
	// Fast ping interval (100ms) for responsive output polling
	// This triggers reads from the container on each ping
	sessionCfg := session.Config{
		WorkerURL:    *workerURL,
		AuthSecret:   *authSecret,
		PingInterval: 100 * time.Millisecond,
	}

	// Create SSH server
	server := &ssh.Server{
		Addr:             *listenAddr,
		Handler:          session.Handler(sessionCfg, registry),
		PublicKeyHandler: auth.NewPublicKeyHandler(registry, *autoReg),
		PtyCallback: func(ctx ssh.Context, pty ssh.Pty) bool {
			return true // Accept all PTY requests
		},
		Version: "SSH-OpenCode-1.0",
	}

	// Load or generate host key
	if _, err := os.Stat(*hostKeyPath); os.IsNotExist(err) {
		log.Printf("Host key not found at %s, generating...", *hostKeyPath)
		if err := generateHostKey(*hostKeyPath); err != nil {
			log.Fatalf("Failed to generate host key: %v", err)
		}
	}

	if err := server.SetOption(ssh.HostKeyFile(*hostKeyPath)); err != nil {
		log.Fatalf("Failed to load host key: %v", err)
	}

	// Graceful shutdown
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)

	go func() {
		<-sigCh
		log.Println("Shutting down...")
		server.Close()
	}()

	// Start server
	log.Printf("SSH relay listening on %s", *listenAddr)
	log.Printf("Proxying to: %s", *workerURL)
	log.Printf("Auto-register new keys: %v", *autoReg)

	if err := server.ListenAndServe(); err != nil && err != ssh.ErrServerClosed {
		log.Fatalf("SSH server error: %v", err)
	}

	log.Println("Server stopped")
}

// generateHostKey creates a new ED25519 host key
func generateHostKey(path string) error {
	// Use ssh-keygen for simplicity
	// In production, you might want to use Go's crypto/ed25519
	cmd := []string{"ssh-keygen", "-t", "ed25519", "-f", path, "-N", "", "-q"}
	log.Printf("Running: %v", cmd)

	// Execute ssh-keygen
	proc, err := os.StartProcess("/usr/bin/ssh-keygen", cmd, &os.ProcAttr{
		Env:   os.Environ(),
		Files: []*os.File{nil, os.Stdout, os.Stderr},
	})
	if err != nil {
		return err
	}

	state, err := proc.Wait()
	if err != nil {
		return err
	}
	if !state.Success() {
		return os.ErrInvalid
	}

	return nil
}
