package tlsutil

import (
	"crypto/tls"
	"errors"
	"testing"
)

func TestManagerReloadSwapsOnlyOnChangeAndKeepsOldOnError(t *testing.T) {
	n := 0
	var fail bool
	load := func() (tls.Certificate, string, error) {
		if fail {
			return tls.Certificate{}, "", errors.New("disk gone")
		}
		return tls.Certificate{Certificate: [][]byte{{byte(n)}}}, []string{"A", "A", "B"}[n], nil
	}
	m, err := NewManager(load)
	if err != nil || m.Fingerprint() != "A" {
		t.Fatalf("initial: %v %q", err, m.Fingerprint())
	}
	n = 1 // same fingerprint → no change
	if changed, err := m.Reload(); err != nil || changed {
		t.Fatalf("unchanged reload: %v %v", changed, err)
	}
	fail = true
	if _, err := m.Reload(); err == nil {
		t.Fatal("expected error")
	}
	if m.Fingerprint() != "A" {
		t.Fatal("previous certificate must stay in service after a failed reload")
	}
	fail, n = false, 2
	if changed, err := m.Reload(); err != nil || !changed || m.Fingerprint() != "B" {
		t.Fatalf("changed reload: %v %v %q", changed, err, m.Fingerprint())
	}
	if c, _ := m.GetCertificate(nil); c == nil || c.Certificate[0][0] != 2 {
		t.Fatal("GetCertificate must serve the new certificate")
	}
}

func TestNewManagerFailsFast(t *testing.T) {
	if _, err := NewManager(func() (tls.Certificate, string, error) { return tls.Certificate{}, "", errors.New("x") }); err == nil {
		t.Fatal("initial load error must be returned")
	}
}
