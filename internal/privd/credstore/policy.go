package credstore

import (
	"strings"
	"unicode/utf8"

	"golang.org/x/text/unicode/norm"

	"github.com/serpis172/Yubeos/internal/priv"
)

const (
	MinPasswordRunes = 12
	MaxPasswordRunes = 1024
)

// commonPasswords is a deliberately small starter list (already ≥ 12 characters, lower-cased). Extending it to
// the 10 000 most common passwords is tracked as TD-017.
var commonPasswords = map[string]bool{}

func init() {
	for _, p := range strings.Fields(`
		123456789012 1234567890123 password1234 password12345 passwordpassword qwertyuiop12 qwertyuiopas
		qwertyuiop123 qwertyuiopasdf 1q2w3e4r5t6y 1qaz2wsx3edc iloveyou1234 iloveyouiloveyou administrator
		administrator1 changemeplease changemechangeme letmein123456 welcome12345 welcome123456 yubeos123456
		yubeospassword homeserver123 truenas123456 ubuntuubuntu1 raspberry1234 trustno1trustno1 abcdefghijkl
		abcdefghijklm abc123abc123 zaq12wsxcde3 monkey123456 dragon123456 football12345 baseball12345
		superman12345 sunshine12345 princess12345 starwars12345 mustang12345 master123456 shadow123456
		admin1234567 adminadminadmin rootrootroot1 passw0rd1234 p@ssw0rd1234 p@ssword1234 p@55w0rd1234
	`) {
		commonPasswords[p] = true
	}
}

// CheckPassword enforces docs/AUTHENTICATION.md §3: length 12..1024 characters (no composition rules), not the
// username, not trivially repetitive or sequential, not on the common list. Returns nil when acceptable.
func CheckPassword(username, password string) *priv.Error {
	n := utf8.RuneCountInString(password)
	switch {
	case !utf8.ValidString(password) || strings.ContainsRune(password, 0):
		return priv.Errorf(priv.CodeWeakPassword, "password contains invalid characters")
	case n < MinPasswordRunes:
		return priv.Errorf(priv.CodeWeakPassword, "password must be at least %d characters", MinPasswordRunes)
	case n > MaxPasswordRunes:
		return priv.Errorf(priv.CodeWeakPassword, "password must be at most %d characters", MaxPasswordRunes)
	}
	norm := strings.ToLower(strings.TrimSpace(normalize(password)))
	u := strings.ToLower(username)
	switch {
	case norm == u || strings.Contains(norm, u) && len(u) >= 4 && n < MinPasswordRunes+len(u):
		return priv.Errorf(priv.CodeWeakPassword, "password must not be based on the username")
	case commonPasswords[norm]:
		return priv.Errorf(priv.CodeWeakPassword, "password is too common")
	case oneRepeatedRune(norm) || sequential(norm):
		return priv.Errorf(priv.CodeWeakPassword, "password is too predictable")
	}
	return nil
}

func normalize(s string) string { return norm.NFKC.String(s) }

func oneRepeatedRune(s string) bool {
	var first rune
	for i, r := range s {
		if i == 0 {
			first = r
		} else if r != first {
			return false
		}
	}
	return true
}

// sequential reports strings that are a single ascending/descending run of code points ("abcdefghijkl", "987654321098" is not).
func sequential(s string) bool {
	rs := []rune(s)
	if len(rs) < 2 {
		return false
	}
	d := rs[1] - rs[0]
	if d != 1 && d != -1 {
		return false
	}
	for i := 2; i < len(rs); i++ {
		if rs[i]-rs[i-1] != d {
			return false
		}
	}
	return true
}
