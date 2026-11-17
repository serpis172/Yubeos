package credstore

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"errors"
	"fmt"
	"strings"
	"time"

	"golang.org/x/crypto/argon2"
	"golang.org/x/text/unicode/norm"
)

// Params are the Argon2id cost parameters (memory in KiB).
type Params struct {
	M uint32 `json:"m"`
	T uint32 `json:"t"`
	P uint8  `json:"p"`
}

// Floor and defaults: docs/AUTHENTICATION.md §3 (never below m=19 MiB, t=2 in production).
var (
	DefaultParams = Params{M: 64 * 1024, T: 3, P: 2}
	MinParams     = Params{M: 19 * 1024, T: 2, P: 1}
)

const (
	keyLen  = 32
	saltLen = 16
	// Upper bounds applied when PARSING a stored hash, so a corrupted or tampered row cannot trigger a huge allocation.
	maxM = 1 << 20 // 1 GiB
	maxT = 16
	maxP = 16
)

// prehash binds the password to the server-side pepper and normalises Unicode (NFKC) so visually
// identical passwords typed on different devices match.
func prehash(pepper []byte, password string) []byte {
	m := hmac.New(sha256.New, pepper)
	m.Write([]byte(norm.NFKC.String(password)))
	return m.Sum(nil)
}

// Hash returns a PHC-formatted Argon2id hash with a fresh random salt.
func Hash(pepper []byte, password string, p Params) (string, error) {
	salt := make([]byte, saltLen)
	if _, err := rand.Read(salt); err != nil {
		return "", err
	}
	key := argon2.IDKey(prehash(pepper, password), salt, p.T, p.M, p.P, keyLen)
	enc := base64.RawStdEncoding
	return fmt.Sprintf("$argon2id$v=%d$m=%d,t=%d,p=%d$%s$%s", argon2.Version, p.M, p.T, p.P, enc.EncodeToString(salt), enc.EncodeToString(key)), nil
}

var errBadHash = errors.New("credstore: malformed password hash")

func parse(encoded string) (Params, []byte, []byte, error) {
	parts := strings.Split(encoded, "$")
	if len(parts) != 6 || parts[1] != "argon2id" {
		return Params{}, nil, nil, errBadHash
	}
	var v int
	if _, err := fmt.Sscanf(parts[2], "v=%d", &v); err != nil || v != argon2.Version {
		return Params{}, nil, nil, errBadHash
	}
	var p Params
	var pp uint32
	if _, err := fmt.Sscanf(parts[3], "m=%d,t=%d,p=%d", &p.M, &p.T, &pp); err != nil {
		return Params{}, nil, nil, errBadHash
	}
	if pp == 0 || pp > maxP || p.T == 0 || p.T > maxT || p.M < 8*pp || p.M > maxM {
		return Params{}, nil, nil, errBadHash
	}
	p.P = uint8(pp)
	salt, err1 := base64.RawStdEncoding.DecodeString(parts[4])
	key, err2 := base64.RawStdEncoding.DecodeString(parts[5])
	if err1 != nil || err2 != nil || len(salt) < 8 || len(key) < 16 || len(key) > 64 {
		return Params{}, nil, nil, errBadHash
	}
	return p, salt, key, nil
}

// Verify checks password against an encoded hash in constant time. needsRehash is true when the stored
// parameters differ from current.
func Verify(pepper []byte, password, encoded string, current Params) (ok, needsRehash bool, err error) {
	p, salt, key, err := parse(encoded)
	if err != nil {
		return false, false, err
	}
	got := argon2.IDKey(prehash(pepper, password), salt, p.T, p.M, p.P, uint32(len(key)))
	ok = subtle.ConstantTimeCompare(got, key) == 1
	return ok, ok && p != current, nil
}

// Calibrate picks the strongest parameters (starting from DefaultParams, never below MinParams) whose single
// hash takes about target on this machine. It is run once at first start and the result is persisted.
func Calibrate(target time.Duration) Params {
	measure := func(p Params) time.Duration {
		start := time.Now()
		argon2.IDKey([]byte("calibration"), make([]byte, saltLen), p.T, p.M, p.P, keyLen)
		return time.Since(start)
	}
	p := DefaultParams
	// Too slow already? shrink memory towards the floor, then time.
	for measure(p) > target*3/2 && p.M > MinParams.M {
		p.M = max(p.M/2, MinParams.M)
	}
	for measure(p) > target*3/2 && p.T > MinParams.T {
		p.T--
	}
	// Fast machine: raise t until the target is met (cap 6).
	for measure(p) < target && p.T < 6 {
		p.T++
	}
	return p
}
