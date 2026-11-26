// Package webauthn is a minimal WebAuthn relying-party implementation: registration with attestation "none" and
// assertion verification for ES256 (P-256) credentials — what platform authenticators, security keys and password
// managers produce by default. User verification is always required. It uses only the standard library.
//
// Known limits (tracked as technical debt): EdDSA/RS256 credentials are rejected at registration, and no
// attestation statements are verified (privacy-preserving "none" attestation is requested).
package webauthn

import (
	"bytes"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"math/big"
)

var (
	ErrChallenge   = errors.New("webauthn: challenge mismatch")
	ErrOrigin      = errors.New("webauthn: origin or RP ID mismatch")
	ErrFlags       = errors.New("webauthn: user presence/verification missing")
	ErrSignature   = errors.New("webauthn: invalid signature")
	ErrUnsupported = errors.New("webauthn: unsupported credential type (ES256 only)")
	ErrCounter     = errors.New("webauthn: signature counter regression (possible cloned authenticator)")
	ErrMalformed   = errors.New("webauthn: malformed response")
)

const (
	flagUP = 0x01
	flagUV = 0x04
	flagAT = 0x40
)

// Credential is the public part stored server-side.
type Credential struct {
	ID        []byte
	PublicKey []byte // uncompressed P-256 point: 0x04 || X || Y
	SignCount uint32
}

// NewChallenge returns 32 random bytes.
func NewChallenge() []byte {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		panic("webauthn: crypto/rand failed: " + err.Error())
	}
	return b
}

// B64 is the base64url (no padding) encoding used on the wire.
func B64(b []byte) string { return base64.RawURLEncoding.EncodeToString(b) }

// FromB64 decodes base64url, tolerating padding.
func FromB64(s string) ([]byte, error) {
	for len(s) > 0 && s[len(s)-1] == '=' {
		s = s[:len(s)-1]
	}
	return base64.RawURLEncoding.DecodeString(s)
}

type clientData struct {
	Type      string `json:"type"`
	Challenge string `json:"challenge"`
	Origin    string `json:"origin"`
}

func checkClientData(raw []byte, typ string, challenge []byte, origin string) error {
	var cd clientData
	if err := json.Unmarshal(raw, &cd); err != nil {
		return ErrMalformed
	}
	if cd.Type != typ {
		return ErrMalformed
	}
	got, err := FromB64(cd.Challenge)
	if err != nil || subtle.ConstantTimeCompare(got, challenge) != 1 {
		return ErrChallenge
	}
	if cd.Origin != origin {
		return ErrOrigin
	}
	return nil
}

func checkAuthData(ad []byte, rpID string) (flags byte, count uint32, err error) {
	if len(ad) < 37 {
		return 0, 0, ErrMalformed
	}
	h := sha256.Sum256([]byte(rpID))
	if subtle.ConstantTimeCompare(ad[:32], h[:]) != 1 {
		return 0, 0, ErrOrigin
	}
	flags = ad[32]
	if flags&flagUP == 0 || flags&flagUV == 0 {
		return 0, 0, ErrFlags
	}
	return flags, binary.BigEndian.Uint32(ad[33:37]), nil
}

// ParseRegistration validates a navigator.credentials.create() response (attestation "none").
func ParseRegistration(attestationObject, clientDataJSON, challenge []byte, origin, rpID string) (*Credential, error) {
	if err := checkClientData(clientDataJSON, "webauthn.create", challenge, origin); err != nil {
		return nil, err
	}
	v, _, err := decodeCBOR(attestationObject)
	if err != nil {
		return nil, ErrMalformed
	}
	m, ok := v.(map[any]any)
	if !ok {
		return nil, ErrMalformed
	}
	if f, _ := m["fmt"].(string); f != "none" {
		return nil, fmt.Errorf("webauthn: attestation format %q not accepted (use none)", f)
	}
	ad, ok := m["authData"].([]byte)
	if !ok {
		return nil, ErrMalformed
	}
	flags, count, err := checkAuthData(ad, rpID)
	if err != nil {
		return nil, err
	}
	if flags&flagAT == 0 || len(ad) < 55 {
		return nil, ErrMalformed
	}
	idLen := int(binary.BigEndian.Uint16(ad[53:55]))
	if idLen < 16 || idLen > 1023 || len(ad) < 55+idLen {
		return nil, ErrMalformed
	}
	id := append([]byte(nil), ad[55:55+idLen]...)
	key, _, err := decodeCBOR(ad[55+idLen:])
	if err != nil {
		return nil, ErrMalformed
	}
	pub, err := coseES256(key)
	if err != nil {
		return nil, err
	}
	return &Credential{ID: id, PublicKey: pub, SignCount: count}, nil
}

func coseES256(v any) ([]byte, error) {
	k, ok := v.(map[any]any)
	if !ok {
		return nil, ErrMalformed
	}
	kty, _ := k[int64(1)].(int64)
	alg, _ := k[int64(3)].(int64)
	crv, _ := k[int64(-1)].(int64)
	x, _ := k[int64(-2)].([]byte)
	y, _ := k[int64(-3)].([]byte)
	if kty != 2 || alg != -7 || crv != 1 || len(x) != 32 || len(y) != 32 {
		return nil, ErrUnsupported
	}
	out := make([]byte, 0, 65)
	out = append(out, 4)
	out = append(out, x...)
	out = append(out, y...)
	if !onCurve(out) {
		return nil, ErrMalformed
	}
	return out, nil
}

func onCurve(p []byte) bool {
	x, y := new(big.Int).SetBytes(p[1:33]), new(big.Int).SetBytes(p[33:65])
	return elliptic.P256().IsOnCurve(x, y)
}

// VerifyAssertion validates a navigator.credentials.get() response and returns the new signature counter.
// A counter that does not increase (when either side is non-zero) yields ErrCounter: the caller must flag the
// credential and refuse the login.
func VerifyAssertion(c *Credential, authData, clientDataJSON, sig, challenge []byte, origin, rpID string) (uint32, error) {
	if err := checkClientData(clientDataJSON, "webauthn.get", challenge, origin); err != nil {
		return 0, err
	}
	_, count, err := checkAuthData(authData, rpID)
	if err != nil {
		return 0, err
	}
	if len(c.PublicKey) != 65 || c.PublicKey[0] != 4 || !onCurve(c.PublicKey) {
		return 0, ErrMalformed
	}
	pub := &ecdsa.PublicKey{Curve: elliptic.P256(), X: new(big.Int).SetBytes(c.PublicKey[1:33]), Y: new(big.Int).SetBytes(c.PublicKey[33:65])}
	cdh := sha256.Sum256(clientDataJSON)
	digest := sha256.Sum256(append(bytes.Clone(authData), cdh[:]...))
	if !ecdsa.VerifyASN1(pub, digest[:], sig) {
		return 0, ErrSignature
	}
	if (count != 0 || c.SignCount != 0) && count <= c.SignCount {
		return count, ErrCounter
	}
	return count, nil
}
