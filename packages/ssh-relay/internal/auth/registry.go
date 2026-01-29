package auth

import (
	"database/sql"
	"time"

	_ "github.com/mattn/go-sqlite3"
	"golang.org/x/crypto/ssh"
)

// Registry manages SSH key storage and authentication
type Registry struct {
	db *sql.DB
}

// KeyInfo contains information about a registered SSH key
type KeyInfo struct {
	Fingerprint string
	PublicKey   []byte
	CreatedAt   time.Time
	LastUsed    *time.Time
}

// NewRegistry creates a new key registry with SQLite storage
func NewRegistry(dbPath string) (*Registry, error) {
	db, err := sql.Open("sqlite3", dbPath)
	if err != nil {
		return nil, err
	}

	// Create table if not exists
	_, err = db.Exec(`
		CREATE TABLE IF NOT EXISTS keys (
			fingerprint TEXT PRIMARY KEY,
			public_key BLOB NOT NULL,
			created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
			last_used DATETIME
		)
	`)
	if err != nil {
		db.Close()
		return nil, err
	}

	return &Registry{db: db}, nil
}

// Close closes the registry database
func (r *Registry) Close() error {
	return r.db.Close()
}

// KeyExists checks if a key fingerprint is registered
func (r *Registry) KeyExists(fingerprint string) (bool, error) {
	var count int
	err := r.db.QueryRow("SELECT COUNT(*) FROM keys WHERE fingerprint = ?", fingerprint).Scan(&count)
	if err != nil {
		return false, err
	}
	return count > 0, nil
}

// RegisterKey adds a new SSH key to the registry
func (r *Registry) RegisterKey(fingerprint string, publicKey ssh.PublicKey) error {
	_, err := r.db.Exec(
		"INSERT OR REPLACE INTO keys (fingerprint, public_key, created_at) VALUES (?, ?, ?)",
		fingerprint, publicKey.Marshal(), time.Now(),
	)
	return err
}

// UpdateLastUsed updates the last_used timestamp for a key
func (r *Registry) UpdateLastUsed(fingerprint string) error {
	_, err := r.db.Exec(
		"UPDATE keys SET last_used = ? WHERE fingerprint = ?",
		time.Now(), fingerprint,
	)
	return err
}

// GetKey retrieves key info by fingerprint
func (r *Registry) GetKey(fingerprint string) (*KeyInfo, error) {
	var info KeyInfo
	var lastUsed sql.NullTime
	err := r.db.QueryRow(
		"SELECT fingerprint, public_key, created_at, last_used FROM keys WHERE fingerprint = ?",
		fingerprint,
	).Scan(&info.Fingerprint, &info.PublicKey, &info.CreatedAt, &lastUsed)
	if err != nil {
		return nil, err
	}
	if lastUsed.Valid {
		info.LastUsed = &lastUsed.Time
	}
	return &info, nil
}

// ListKeys returns all registered keys
func (r *Registry) ListKeys() ([]*KeyInfo, error) {
	rows, err := r.db.Query("SELECT fingerprint, public_key, created_at, last_used FROM keys ORDER BY created_at DESC")
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var keys []*KeyInfo
	for rows.Next() {
		var info KeyInfo
		var lastUsed sql.NullTime
		if err := rows.Scan(&info.Fingerprint, &info.PublicKey, &info.CreatedAt, &lastUsed); err != nil {
			return nil, err
		}
		if lastUsed.Valid {
			info.LastUsed = &lastUsed.Time
		}
		keys = append(keys, &info)
	}
	return keys, nil
}

// DeleteKey removes a key from the registry
func (r *Registry) DeleteKey(fingerprint string) error {
	_, err := r.db.Exec("DELETE FROM keys WHERE fingerprint = ?", fingerprint)
	return err
}

// Count returns the number of registered keys
func (r *Registry) Count() (int, error) {
	var count int
	err := r.db.QueryRow("SELECT COUNT(*) FROM keys").Scan(&count)
	return count, err
}
