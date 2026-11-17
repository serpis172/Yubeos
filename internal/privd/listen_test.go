package privd

import (
	"os"
	"path/filepath"
	"testing"
)

func TestListenUnixPermissionsAndStaleSocket(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "priv.sock")
	ln, err := ListenUnix(p, "")
	if err != nil {
		t.Fatal(err)
	}
	st, _ := os.Stat(p)
	if st.Mode().Perm() != 0o660 || st.Mode()&os.ModeSocket == 0 {
		t.Fatalf("socket mode %v, want srw-rw----", st.Mode())
	}
	ln.Close() // leaves a stale socket file behind (Go removes it, but simulate a crash below)
	_ = os.WriteFile(p, nil, 0o600)
	if _, err := ListenUnix(p, ""); err == nil {
		t.Fatal("a regular file at the socket path must not be deleted")
	}
	_ = os.Remove(p)
	ln2, err := ListenUnix(p, "")
	if err != nil {
		t.Fatalf("fresh listen: %v", err)
	}
	ln2.Close()
	if _, err := ListenUnix(p, "no-such-group-xyz"); err == nil {
		t.Fatal("unknown group must fail instead of leaving a mis-owned socket")
	}
}
