// Package tlsutil creates and loads the self-signed certificate used when no certificate is configured.
package tlsutil

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/hex"
	"encoding/pem"
	"fmt"
	"math/big"
	"net"
	"os"
	"path/filepath"
	"strings"
	"time"
)

const (
	certFile    = "cert.pem"
	keyFile     = "key.pem"
	validity    = 365 * 24 * time.Hour
	renewBefore = 30 * 24 * time.Hour
)

// LoadOrCreate returns the certificate stored in dir, generating a new ECDSA P-256 one if it is missing,
// unreadable, not yet valid, or expires within 30 days. extraNames are added to the SANs (DNS names or IPs).
// The second result is the SHA-256 fingerprint of the leaf, for display at first start.
func LoadOrCreate(dir string, extraNames []string, now time.Time) (tls.Certificate, string, error) {
	if c, fp, ok := load(dir, now); ok {
		return c, fp, nil
	}
	if err := generate(dir, extraNames, now); err != nil {
		return tls.Certificate{}, "", err
	}
	c, fp, ok := load(dir, now)
	if !ok {
		return tls.Certificate{}, "", fmt.Errorf("tlsutil: generated certificate could not be reloaded from %s", dir)
	}
	return c, fp, nil
}

func load(dir string, now time.Time) (tls.Certificate, string, bool) {
	c, err := tls.LoadX509KeyPair(filepath.Join(dir, certFile), filepath.Join(dir, keyFile))
	if err != nil {
		return tls.Certificate{}, "", false
	}
	leaf, err := x509.ParseCertificate(c.Certificate[0])
	if err != nil || now.Before(leaf.NotBefore) || leaf.NotAfter.Sub(now) < renewBefore {
		return tls.Certificate{}, "", false
	}
	c.Leaf = leaf
	return c, Fingerprint(leaf.Raw), true
}

// Fingerprint formats a DER certificate's SHA-256 as colon-separated upper-case hex.
func Fingerprint(der []byte) string {
	sum := sha256.Sum256(der)
	h := strings.ToUpper(hex.EncodeToString(sum[:]))
	parts := make([]string, 0, len(h)/2)
	for i := 0; i < len(h); i += 2 {
		parts = append(parts, h[i:i+2])
	}
	return strings.Join(parts, ":")
}

func generate(dir string, extraNames []string, now time.Time) error {
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return fmt.Errorf("tlsutil: create dir: %w", err)
	}
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return err
	}
	serial, err := rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), 127))
	if err != nil {
		return err
	}

	dns := map[string]bool{"localhost": true}
	ips := []net.IP{net.IPv4(127, 0, 0, 1), net.IPv6loopback}
	if h, err := os.Hostname(); err == nil && h != "" {
		dns[h] = true
	}
	for _, n := range extraNames {
		if ip := net.ParseIP(n); ip != nil {
			ips = append(ips, ip)
		} else if n != "" {
			dns[n] = true
		}
	}
	if addrs, err := net.InterfaceAddrs(); err == nil {
		for _, a := range addrs {
			if ipn, ok := a.(*net.IPNet); ok && !ipn.IP.IsLinkLocalUnicast() && !ipn.IP.IsMulticast() {
				ips = append(ips, ipn.IP)
			}
		}
	}
	names := make([]string, 0, len(dns))
	for n := range dns {
		names = append(names, n)
	}

	tmpl := &x509.Certificate{
		SerialNumber:          serial,
		Subject:               pkix.Name{CommonName: "Yubeos", Organization: []string{"Yubeos self-signed"}},
		NotBefore:             now.Add(-time.Hour),
		NotAfter:              now.Add(validity),
		KeyUsage:              x509.KeyUsageDigitalSignature,
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		BasicConstraintsValid: true,
		DNSNames:              names,
		IPAddresses:           ips,
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	if err != nil {
		return err
	}
	keyDER, err := x509.MarshalECPrivateKey(key)
	if err != nil {
		return err
	}
	if err := writeAtomic(filepath.Join(dir, keyFile), pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: keyDER}), 0o600); err != nil {
		return err
	}
	return writeAtomic(filepath.Join(dir, certFile), pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}), 0o644)
}

// writeAtomic writes via a temp file in the same directory, fsyncs, then renames.
func writeAtomic(path string, data []byte, mode os.FileMode) error {
	tmp, err := os.CreateTemp(filepath.Dir(path), ".tmp-*")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name())
	if err := tmp.Chmod(mode); err != nil {
		tmp.Close()
		return err
	}
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Sync(); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmp.Name(), path)
}
