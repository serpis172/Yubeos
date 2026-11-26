package credstore

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"errors"
)

// Secrets that must be recoverable (TOTP seeds) are sealed with AES-256-GCM. The key is derived from pepper.key,
// which lives next to credentials.db in a root-only directory: this protects the database file on its own
// (backups, copies, SQL injection into a read primitive), not an attacker who already has both files as root.

func (s *Store) sealKey() []byte {
	m := hmac.New(sha256.New, s.pepper)
	m.Write([]byte("yubeos/credstore/seal/v1"))
	return m.Sum(nil)
}

func (s *Store) aead() (cipher.AEAD, error) {
	b, err := aes.NewCipher(s.sealKey())
	if err != nil {
		return nil, err
	}
	return cipher.NewGCM(b)
}

// seal returns nonce || ciphertext||tag, bound to the account name (a blob copied to another row will not open).
func (s *Store) seal(plain []byte, username string) ([]byte, error) {
	g, err := s.aead()
	if err != nil {
		return nil, err
	}
	nonce := make([]byte, g.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return nil, err
	}
	return g.Seal(nonce, nonce, plain, []byte("totp:"+username)), nil
}

func (s *Store) open(blob []byte, username string) ([]byte, error) {
	g, err := s.aead()
	if err != nil {
		return nil, err
	}
	if len(blob) < g.NonceSize()+g.Overhead() {
		return nil, errors.New("credstore: sealed secret too short")
	}
	return g.Open(nil, blob[:g.NonceSize()], blob[g.NonceSize():], []byte("totp:"+username))
}
