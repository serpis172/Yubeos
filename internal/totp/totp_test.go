package totp

import (
	"strings"
	"testing"
	"time"
)

// RFC 6238 appendix B, SHA-1 secret "12345678901234567890", 8-digit values truncated to 6 digits.
func TestRFC6238Vectors(t *testing.T) {
	secret := []byte("12345678901234567890")
	for _, v := range []struct {
		unix int64
		want string
	}{{59, "287082"}, {1111111109, "081804"}, {1111111111, "050471"}, {1234567890, "005924"}, {2000000000, "279037"}} {
		if got := Code(secret, v.unix/30); got != v.want {
			t.Errorf("t=%d got %s want %s", v.unix, got, v.want)
		}
	}
}

func TestVerifyWindowAndReplay(t *testing.T) {
	s := NewSecret()
	now := time.Unix(1_700_000_000, 0)
	code := Code(s, Step(now))
	st, ok := Verify(s, code, now, 0)
	if !ok || st != Step(now) {
		t.Fatalf("fresh code rejected")
	}
	if _, ok := Verify(s, code, now, st); ok {
		t.Fatal("replay accepted")
	}
	if _, ok := Verify(s, Code(s, Step(now)-1), now, 0); !ok {
		t.Fatal("previous step rejected")
	}
	if _, ok := Verify(s, Code(s, Step(now)-2), now, 0); ok {
		t.Fatal("two steps back accepted")
	}
	if _, ok := Verify(s, "12345", now, 0); ok {
		t.Fatal("short code accepted")
	}
}

func TestSecretAndURI(t *testing.T) {
	s := NewSecret()
	d, err := DecodeSecret(strings.ToLower(EncodeSecret(s)))
	if err != nil || string(d) != string(s) {
		t.Fatal("secret round trip")
	}
	if u := URI("Yubeos", "alice@nas", s); !strings.HasPrefix(u, "otpauth://totp/Yubeos:alice@nas?") || !strings.Contains(u, "secret=") {
		t.Fatal(u)
	}
}
