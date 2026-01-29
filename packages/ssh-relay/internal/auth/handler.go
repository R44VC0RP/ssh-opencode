package auth

import (
	"log"

	"github.com/gliderlabs/ssh"
	gossh "golang.org/x/crypto/ssh"
)

// NewPublicKeyHandler creates an SSH public key authentication handler
// In single-user mode, it auto-registers the first connecting key
func NewPublicKeyHandler(registry *Registry, autoRegister bool) ssh.PublicKeyHandler {
	return func(ctx ssh.Context, key ssh.PublicKey) bool {
		fingerprint := gossh.FingerprintSHA256(key)

		// Check if key exists
		exists, err := registry.KeyExists(fingerprint)
		if err != nil {
			log.Printf("Error checking key: %v", err)
			return false
		}

		// Auto-register new keys (single-user mode)
		if !exists {
			if !autoRegister {
				log.Printf("Unknown key rejected: %s", fingerprint)
				return false
			}

			log.Printf("Auto-registering new key: %s", fingerprint)
			if err := registry.RegisterKey(fingerprint, key); err != nil {
				log.Printf("Error registering key: %v", err)
				return false
			}
		}

		// Store fingerprint in context for session handler
		ctx.SetValue("fingerprint", fingerprint)
		log.Printf("Authenticated: %s", fingerprint)

		return true
	}
}

// ContextKey is used for storing values in the SSH context
type ContextKey string

const (
	// FingerprintKey is the context key for the SSH key fingerprint
	FingerprintKey ContextKey = "fingerprint"
)

// GetFingerprint retrieves the SSH key fingerprint from the context
func GetFingerprint(ctx ssh.Context) string {
	if fp, ok := ctx.Value("fingerprint").(string); ok {
		return fp
	}
	return ""
}
