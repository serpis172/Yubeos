package tlsutil

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"sync"
	"time"
)

// Loader returns the current certificate and its fingerprint.
type Loader func() (tls.Certificate, string, error)

// Manager serves the current certificate through tls.Config.GetCertificate and swaps it at runtime:
// periodic re-check (renews self-signed certs before expiry; picks up replaced certificate files) and
// explicit Reload (SIGHUP). A failed reload keeps serving the previous certificate.
type Manager struct {
	mu   sync.RWMutex
	cert *tls.Certificate
	fp   string
	load Loader
}

// NewManager loads the initial certificate; failure is fatal for the caller.
func NewManager(load Loader) (*Manager, error) {
	c, fp, err := load()
	if err != nil {
		return nil, err
	}
	return &Manager{cert: &c, fp: fp, load: load}, nil
}

// GetCertificate implements tls.Config.GetCertificate.
func (m *Manager) GetCertificate(*tls.ClientHelloInfo) (*tls.Certificate, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.cert, nil
}

// Fingerprint returns the SHA-256 fingerprint of the certificate currently served.
func (m *Manager) Fingerprint() string {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.fp
}

// Reload re-runs the loader. changed is true when a different certificate is now served.
func (m *Manager) Reload() (changed bool, err error) {
	c, fp, err := m.load()
	if err != nil {
		return false, fmt.Errorf("tls reload (keeping previous certificate): %w", err)
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if fp == m.fp {
		return false, nil
	}
	m.cert, m.fp = &c, fp
	return true, nil
}

// Run reloads every interval until ctx is done; logf receives one line per event.
func (m *Manager) Run(ctx context.Context, every time.Duration, logf func(msg string, args ...any)) {
	t := time.NewTicker(every)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			switch changed, err := m.Reload(); {
			case err != nil:
				logf("tls certificate reload failed", "err", err)
			case changed:
				logf("tls certificate changed", "sha256", m.Fingerprint())
			}
		}
	}
}

// FileLoader loads a certificate/key pair from disk (tls.mode=files).
func FileLoader(certPath, keyPath string) Loader {
	return func() (tls.Certificate, string, error) {
		c, err := tls.LoadX509KeyPair(certPath, keyPath)
		if err != nil {
			return tls.Certificate{}, "", err
		}
		leaf, err := x509.ParseCertificate(c.Certificate[0])
		if err != nil {
			return tls.Certificate{}, "", err
		}
		c.Leaf = leaf
		return c, Fingerprint(leaf.Raw), nil
	}
}

// SelfSignedLoader loads or (re)generates the self-signed certificate in dir.
func SelfSignedLoader(dir string, names []string) Loader {
	return func() (tls.Certificate, string, error) { return LoadOrCreate(dir, names, time.Now()) }
}
