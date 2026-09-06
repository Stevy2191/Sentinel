// Package cryptutil provides at-rest encryption for sensitive config values
// (SMTP passwords, bot tokens, webhook URLs) stored in Postgres. It uses
// AES-256-GCM with a key derived from configuration, never a hardcoded key.
package cryptutil

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"log"
	"os"
	"sync"
)

var (
	keyOnce sync.Once
	key     [32]byte
)

// resolveKey derives the AES-256 key once per process. Preference order:
//  1. NOTIFICATION_SECRET_KEY, if set — dedicated key material, recommended.
//  2. JWT_SECRET, if set — reused so existing deployments get real at-rest
//     encryption with no new required config, at the cost of a DB dump alone
//     no longer being sufficient (JWT_SECRET would also need to leak).
//  3. A fixed fallback string — only reached in a dev environment with
//     neither set. This provides no real protection; a startup warning says
//     so loudly. Production deployments already need to set JWT_SECRET, so
//     this path is not expected to be hit outside of local development.
func resolveKey() [32]byte {
	keyOnce.Do(func() {
		if k := os.Getenv("NOTIFICATION_SECRET_KEY"); k != "" {
			key = sha256.Sum256([]byte(k))
			return
		}
		if j := os.Getenv("JWT_SECRET"); j != "" {
			key = sha256.Sum256([]byte("sentinel-notification-secret:" + j))
			log.Printf("[cryptutil] NOTIFICATION_SECRET_KEY is not set; deriving the " +
				"notification-secret encryption key from JWT_SECRET. Set a dedicated " +
				"NOTIFICATION_SECRET_KEY for independent key material.")
			return
		}
		key = sha256.Sum256([]byte("sentinel-insecure-default-notification-key"))
		log.Printf("[cryptutil] WARNING: neither NOTIFICATION_SECRET_KEY nor JWT_SECRET is " +
			"set; notification secrets will be encrypted with a well-known default key, " +
			"which provides no real protection. Set NOTIFICATION_SECRET_KEY (or JWT_SECRET) " +
			"before storing real credentials.")
	})
	return key
}

func newGCM() (cipher.AEAD, error) {
	k := resolveKey()
	block, err := aes.NewCipher(k[:])
	if err != nil {
		return nil, err
	}
	return cipher.NewGCM(block)
}

// Encrypt returns a base64-encoded, AES-256-GCM-sealed form of plaintext.
// An empty string encrypts to an empty string, so "not set" round-trips
// cleanly without callers needing to special-case it.
func Encrypt(plaintext string) (string, error) {
	if plaintext == "" {
		return "", nil
	}
	g, err := newGCM()
	if err != nil {
		return "", fmt.Errorf("cryptutil: building cipher: %w", err)
	}
	nonce := make([]byte, g.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return "", fmt.Errorf("cryptutil: generating nonce: %w", err)
	}
	sealed := g.Seal(nonce, nonce, []byte(plaintext), nil)
	return base64.StdEncoding.EncodeToString(sealed), nil
}

// Decrypt reverses Encrypt. An empty string decrypts to an empty string.
func Decrypt(ciphertext string) (string, error) {
	if ciphertext == "" {
		return "", nil
	}
	raw, err := base64.StdEncoding.DecodeString(ciphertext)
	if err != nil {
		return "", fmt.Errorf("cryptutil: invalid ciphertext encoding: %w", err)
	}
	g, err := newGCM()
	if err != nil {
		return "", fmt.Errorf("cryptutil: building cipher: %w", err)
	}
	if len(raw) < g.NonceSize() {
		return "", errors.New("cryptutil: ciphertext too short")
	}
	nonce, sealed := raw[:g.NonceSize()], raw[g.NonceSize():]
	plain, err := g.Open(nil, nonce, sealed, nil)
	if err != nil {
		return "", fmt.Errorf("cryptutil: decryption failed (wrong key or not ciphertext): %w", err)
	}
	return string(plain), nil
}
