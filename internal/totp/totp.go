// Package totp implements RFC 6238 time-based one-time passwords (SHA-1, 6 digits, 30 s) with replay protection.
package totp

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha1" //nolint:gosec // RFC 6238 default; maximum authenticator-app compatibility
	"crypto/subtle"
	"encoding/base32"
	"encoding/binary"
	"fmt"
	"net/url"
	"strings"
	"time"
)

const (
	Period = 30
	Digits = 6
)

var b32 = base32.StdEncoding.WithPadding(base32.NoPadding)

// NewSecret returns 20 random bytes (160 bits).
func NewSecret() []byte {
	b := make([]byte, 20)
	if _, err := rand.Read(b); err != nil {
		panic("totp: crypto/rand failed: " + err.Error())
	}
	return b
}

// EncodeSecret renders a secret as unpadded base32 (what authenticator apps display).
func EncodeSecret(s []byte) string { return b32.EncodeToString(s) }

// DecodeSecret parses a base32 secret (case and spaces tolerated).
func DecodeSecret(s string) ([]byte, error) {
	s = strings.ToUpper(strings.ReplaceAll(strings.TrimSpace(s), " ", ""))
	return b32.DecodeString(s)
}

// Step returns the time step of t.
func Step(t time.Time) int64 { return t.Unix() / Period }

// Code computes the code for a time step.
func Code(secret []byte, step int64) string {
	var msg [8]byte
	binary.BigEndian.PutUint64(msg[:], uint64(step))
	m := hmac.New(sha1.New, secret)
	m.Write(msg[:])
	sum := m.Sum(nil)
	off := sum[len(sum)-1] & 0x0f
	v := binary.BigEndian.Uint32(sum[off:off+4]) & 0x7fffffff
	return fmt.Sprintf("%0*d", Digits, v%1_000_000)
}

// Verify accepts code for steps now-1..now+1, but only a step strictly greater than lastStep (each step is
// usable once). It returns the matched step so the caller can persist it.
func Verify(secret []byte, code string, now time.Time, lastStep int64) (int64, bool) {
	code = strings.ReplaceAll(strings.TrimSpace(code), " ", "")
	if len(code) != Digits {
		return 0, false
	}
	cur := Step(now)
	matched := int64(-1)
	for d := int64(-1); d <= 1; d++ {
		st := cur + d
		if subtle.ConstantTimeCompare([]byte(Code(secret, st)), []byte(code)) == 1 && st > lastStep {
			matched = st
		}
	}
	return matched, matched >= 0
}

// URI builds the otpauth:// provisioning URI shown as a QR code.
func URI(issuer, account string, secret []byte) string {
	v := url.Values{}
	v.Set("secret", EncodeSecret(secret))
	v.Set("issuer", issuer)
	v.Set("algorithm", "SHA1")
	v.Set("digits", fmt.Sprint(Digits))
	v.Set("period", fmt.Sprint(Period))
	return "otpauth://totp/" + url.PathEscape(issuer+":"+account) + "?" + v.Encode()
}
