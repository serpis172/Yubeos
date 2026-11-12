package tlsutil

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestCreateThenReuse(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "tls")
	now := time.Now()

	c1, fp1, err := LoadOrCreate(dir, []string{"nas.lan", "192.168.1.10"}, now)
	if err != nil {
		t.Fatal(err)
	}
	if c1.Leaf == nil || len(fp1) != 95 { // 32 bytes → 64 hex + 31 colons
		t.Fatalf("unexpected leaf/fingerprint: %v %q", c1.Leaf, fp1)
	}
	wantDNS, wantIP := false, false
	for _, n := range c1.Leaf.DNSNames {
		wantDNS = wantDNS || n == "nas.lan"
	}
	for _, ip := range c1.Leaf.IPAddresses {
		wantIP = wantIP || ip.String() == "192.168.1.10"
	}
	if !wantDNS || !wantIP {
		t.Fatalf("SANs missing: dns=%v ip=%v", c1.Leaf.DNSNames, c1.Leaf.IPAddresses)
	}

	st, err := os.Stat(filepath.Join(dir, "key.pem"))
	if err != nil {
		t.Fatal(err)
	}
	if st.Mode().Perm() != 0o600 {
		t.Fatalf("key mode = %o, want 600", st.Mode().Perm())
	}

	_, fp2, err := LoadOrCreate(dir, nil, now.Add(time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	if fp1 != fp2 {
		t.Fatal("certificate should be reused, fingerprint changed")
	}
}

func TestRegeneratesWhenNearExpiry(t *testing.T) {
	dir := t.TempDir()
	now := time.Now()
	_, fp1, err := LoadOrCreate(dir, nil, now)
	if err != nil {
		t.Fatal(err)
	}
	_, fp2, err := LoadOrCreate(dir, nil, now.Add(340*24*time.Hour)) // < 30 days left
	if err != nil {
		t.Fatal(err)
	}
	if fp1 == fp2 {
		t.Fatal("expected a new certificate near expiry")
	}
}

func TestCorruptFilesAreReplaced(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "cert.pem"), []byte("junk"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "key.pem"), []byte("junk"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, _, err := LoadOrCreate(dir, nil, time.Now()); err != nil {
		t.Fatalf("should recover from corrupt files: %v", err)
	}
}
