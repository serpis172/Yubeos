// Package webauthntest is a virtual WebAuthn authenticator (ES256, attestation "none", user verification on) for
// tests of relying-party code. It is not for production use.
package webauthntest

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"encoding/binary"
	"encoding/json"
	"testing"

	"github.com/serpis172/Yubeos/internal/webauthn"
)

const (
	flagUP = 0x01
	flagUV = 0x04
	flagAT = 0x40
)

// Authenticator holds one credential.
type Authenticator struct {
	RPID, Origin string
	ID           []byte
	Count        uint32
	key          *ecdsa.PrivateKey
}

// New creates an authenticator for the relying party rpID reached at origin (e.g. "nas.lan", "https://nas.lan").
func New(t testing.TB, rpID, origin string) *Authenticator {
	t.Helper()
	k, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	id := make([]byte, 32)
	_, _ = rand.Read(id)
	return &Authenticator{RPID: rpID, Origin: origin, ID: id, key: k}
}

func head(major byte, n int) []byte {
	switch {
	case n < 24:
		return []byte{major<<5 | byte(n)}
	case n < 256:
		return []byte{major<<5 | 24, byte(n)}
	}
	return []byte{major<<5 | 25, byte(n >> 8), byte(n)}
}
func bstr(b []byte) []byte { return append(head(2, len(b)), b...) }
func tstr(s string) []byte { return append(head(3, len(s)), s...) }
func cint(n int) []byte {
	if n >= 0 {
		return head(0, n)
	}
	return head(1, -1-n)
}

func (a *Authenticator) clientData(typ string, challenge []byte) []byte {
	b, _ := json.Marshal(map[string]any{"type": typ, "challenge": webauthn.B64(challenge), "origin": a.Origin})
	return b
}

func (a *Authenticator) authData(flags byte, attested []byte) []byte {
	h := sha256.Sum256([]byte(a.RPID))
	out := append(h[:], flags)
	out = binary.BigEndian.AppendUint32(out, a.Count)
	return append(out, attested...)
}

// Register answers navigator.credentials.create(): attestationObject and clientDataJSON.
func (a *Authenticator) Register(challenge []byte) (attestationObject, clientDataJSON []byte) {
	x, y := a.key.X.FillBytes(make([]byte, 32)), a.key.Y.FillBytes(make([]byte, 32))
	cose := []byte{0xa5}
	for _, kv := range [][]byte{cint(1), cint(2), cint(3), cint(-7), cint(-1), cint(1), cint(-2), bstr(x), cint(-3), bstr(y)} {
		cose = append(cose, kv...)
	}
	at := append(make([]byte, 16), byte(len(a.ID)>>8), byte(len(a.ID)))
	at = append(append(at, a.ID...), cose...)
	obj := []byte{0xa3}
	for _, kv := range [][]byte{tstr("fmt"), tstr("none"), tstr("attStmt"), {0xa0}, tstr("authData"), bstr(a.authData(flagUP|flagUV|flagAT, at))} {
		obj = append(obj, kv...)
	}
	return obj, a.clientData("webauthn.create", challenge)
}

// Assert answers navigator.credentials.get(), incrementing the signature counter.
func (a *Authenticator) Assert(challenge []byte) (authData, clientDataJSON, signature []byte) {
	a.Count++
	return a.assertWithCount(challenge)
}

// AssertWithCount signs with the current counter without incrementing it (simulates a cloned authenticator).
func (a *Authenticator) AssertWithCount(challenge []byte) (authData, clientDataJSON, signature []byte) {
	return a.assertWithCount(challenge)
}

func (a *Authenticator) assertWithCount(challenge []byte) (authData, clientDataJSON, signature []byte) {
	authData = a.authData(flagUP|flagUV, nil)
	clientDataJSON = a.clientData("webauthn.get", challenge)
	h := sha256.Sum256(clientDataJSON)
	dg := sha256.Sum256(append(append([]byte(nil), authData...), h[:]...))
	sig, err := ecdsa.SignASN1(rand.Reader, a.key, dg[:])
	if err != nil {
		panic(err)
	}
	return authData, clientDataJSON, sig
}
