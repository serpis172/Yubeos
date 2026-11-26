package webauthn

import (
	"bytes"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"encoding/binary"
	"encoding/json"
	"errors"
	"testing"
)

const (
	rp     = "nas.example.org"
	origin = "https://nas.example.org"
)

func cborBytes(b []byte) []byte { return append(cborHead(2, len(b)), b...) }
func cborText(s string) []byte  { return append(cborHead(3, len(s)), s...) }
func cborHead(major byte, n int) []byte {
	if n < 24 {
		return []byte{major<<5 | byte(n)}
	}
	if n < 256 {
		return []byte{major<<5 | 24, byte(n)}
	}
	return []byte{major<<5 | 25, byte(n >> 8), byte(n)}
}
func cborInt(n int64) []byte {
	if n >= 0 {
		return cborHead(0, int(n))
	}
	return cborHead(1, int(-1-n))
}

func newKey(t *testing.T) (*ecdsa.PrivateKey, []byte) {
	k, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	x, y := k.X.FillBytes(make([]byte, 32)), k.Y.FillBytes(make([]byte, 32))
	return k, append([]byte{4}, append(x, y...)...)
}

func clientJSON(typ string, chal []byte) []byte {
	b, _ := json.Marshal(map[string]string{"type": typ, "challenge": B64(chal), "origin": origin})
	return b
}

func authData(flags byte, count uint32, attested []byte) []byte {
	h := sha256.Sum256([]byte(rp))
	out := append(h[:], flags)
	out = binary.BigEndian.AppendUint32(out, count)
	return append(out, attested...)
}

func attestation(id []byte, pub []byte, chal []byte) (attObj, cd []byte) {
	cose := []byte{0xa5}
	cose = append(cose, cborInt(1)...)
	cose = append(cose, cborInt(2)...)
	cose = append(cose, cborInt(3)...)
	cose = append(cose, cborInt(-7)...)
	cose = append(cose, cborInt(-1)...)
	cose = append(cose, cborInt(1)...)
	cose = append(cose, cborInt(-2)...)
	cose = append(cose, cborBytes(pub[1:33])...)
	cose = append(cose, cborInt(-3)...)
	cose = append(cose, cborBytes(pub[33:])...)
	at := append(make([]byte, 16), byte(len(id)>>8), byte(len(id)))
	at = append(append(at, id...), cose...)
	ad := authData(flagUP|flagUV|flagAT, 0, at)
	obj := []byte{0xa3}
	obj = append(obj, cborText("fmt")...)
	obj = append(obj, cborText("none")...)
	obj = append(obj, cborText("attStmt")...)
	obj = append(obj, 0xa0)
	obj = append(obj, cborText("authData")...)
	obj = append(obj, cborBytes(ad)...)
	return obj, clientJSON("webauthn.create", chal)
}

func TestRegisterThenAssert(t *testing.T) {
	key, pub := newKey(t)
	id := bytes.Repeat([]byte{7}, 32)
	chal := NewChallenge()
	obj, cd := attestation(id, pub, chal)
	cred, err := ParseRegistration(obj, cd, chal, origin, rp)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(cred.ID, id) || !bytes.Equal(cred.PublicKey, pub) {
		t.Fatal("credential mismatch")
	}

	ch2 := NewChallenge()
	ad := authData(flagUP|flagUV, 1, nil)
	cdj := clientJSON("webauthn.get", ch2)
	h := sha256.Sum256(cdj)
	dg := sha256.Sum256(append(bytes.Clone(ad), h[:]...))
	sig, _ := ecdsa.SignASN1(rand.Reader, key, dg[:])
	n, err := VerifyAssertion(cred, ad, cdj, sig, ch2, origin, rp)
	if err != nil || n != 1 {
		t.Fatalf("assert: %v %d", err, n)
	}
	cred.SignCount = 1
	if _, err := VerifyAssertion(cred, ad, cdj, sig, ch2, origin, rp); !errors.Is(err, ErrCounter) {
		t.Fatalf("want counter regression, got %v", err)
	}
	if _, err := VerifyAssertion(cred, ad, cdj, sig, NewChallenge(), origin, rp); !errors.Is(err, ErrChallenge) {
		t.Fatalf("want challenge mismatch, got %v", err)
	}
	if _, err := VerifyAssertion(cred, ad, cdj, sig, ch2, "https://evil.example", rp); !errors.Is(err, ErrOrigin) {
		t.Fatalf("want origin mismatch, got %v", err)
	}
	noUV := authData(flagUP, 2, nil)
	if _, err := VerifyAssertion(cred, noUV, cdj, sig, ch2, origin, rp); !errors.Is(err, ErrFlags) {
		t.Fatalf("want flags error, got %v", err)
	}
}

func TestCBORRejectsGarbage(t *testing.T) {
	for _, in := range [][]byte{{}, {0x5f}, {0xa1}, {0x9b, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff}} {
		if _, _, err := decodeCBOR(in); err == nil {
			t.Errorf("accepted %x", in)
		}
	}
}
