package api

import (
	"crypto/hmac"
	"crypto/sha1" //nolint:gosec
	"encoding/base32"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// totpCode is an independent RFC 6238 implementation, so the test does not trust the code under test.
func totpCode(t *testing.T, b32secret string, at time.Time) string {
	t.Helper()
	key, err := base32.StdEncoding.WithPadding(base32.NoPadding).DecodeString(b32secret)
	if err != nil {
		t.Fatal(err)
	}
	var c [8]byte
	binary.BigEndian.PutUint64(c[:], uint64(at.Unix()/30))
	m := hmac.New(sha1.New, key) //nolint:gosec
	m.Write(c[:])
	sum := m.Sum(nil)
	off := sum[len(sum)-1] & 0xF
	return fmt.Sprintf("%06d", (binary.BigEndian.Uint32(sum[off:off+4])&0x7FFFFFFF)%1_000_000)
}

type session struct{ cookie, csrf string }

func (s session) opts() []reqOpt { return []reqOpt{withCookie(s.cookie), withCSRF(s.csrf)} }

func (f *flow) sessionOf(w *httptest.ResponseRecorder) session {
	f.t.Helper()
	var m me
	_ = json.Unmarshal(w.Body.Bytes(), &m)
	ck := cookieOf(w)
	if w.Code != 200 || ck == nil {
		f.t.Fatalf("no session: %d %s", w.Code, w.Body)
	}
	return session{cookie: ck.Value, csrf: m.CSRF}
}

func (f *flow) field(w *httptest.ResponseRecorder, name string) string {
	f.t.Helper()
	var m map[string]any
	_ = json.Unmarshal(w.Body.Bytes(), &m)
	s, _ := m[name].(string)
	return s
}

func TestTOTPEnrolmentLoginAndStepUpEndToEnd(t *testing.T) {
	f := newFlow(t, "admins", nil)
	f.enrol("alice", flowPW)

	// admin without a factor on a local network: enrollment-only session
	w, _ := f.login("alice", flowPW)
	s := f.sessionOf(w)
	if w := f.do("GET", "/api/v1/system/info", nil, withCookie(s.cookie)); w.Code != 403 {
		t.Fatalf("enrollment-only session must be restricted: %d", w.Code)
	}
	w = f.do("POST", "/api/v1/auth/totp/enroll", nil, s.opts()...)
	secret := f.field(w, "secret")
	if w.Code != 200 || len(secret) != 32 || !strings.HasPrefix(f.field(w, "otpauthUri"), "otpauth://totp/Yubeos:alice?secret="+secret) {
		t.Fatalf("enroll: %d %s", w.Code, w.Body)
	}
	if w := f.do("POST", "/api/v1/auth/totp/confirm", map[string]string{"code": "000000"}, s.opts()...); w.Code != 401 || problemType(t, w) != "invalid-code" {
		t.Fatalf("wrong confirming code: %d %s", w.Code, w.Body)
	}
	w = f.do("POST", "/api/v1/auth/totp/confirm", map[string]string{"code": totpCode(t, secret, f.clk.Now())}, s.opts()...)
	var conf struct{ Codes []string }
	if err := json.Unmarshal(w.Body.Bytes(), &conf); err != nil || w.Code != 200 || len(conf.Codes) != 10 {
		t.Fatalf("confirm: %d %s", w.Code, w.Body)
	}
	if w := f.do("GET", "/api/v1/system/info", nil, withCookie(s.cookie)); w.Code != 200 {
		t.Fatalf("session must be upgraded after confirming: %d %s", w.Code, w.Body)
	}
	var fac struct {
		TOTP bool `json:"totp"`
		Left int  `json:"recoveryCodesLeft"`
	}
	_ = json.Unmarshal(f.do("GET", "/api/v1/auth/factors", nil, s.opts()...).Body.Bytes(), &fac)
	if !fac.TOTP || fac.Left != 10 {
		t.Fatalf("factors: %+v", fac)
	}
	f.do("POST", "/api/v1/auth/logout", nil, s.opts()...)

	// login now needs the second factor
	f.clk.Advance(30 * time.Second) // the confirming step is burnt
	w, _ = f.login("alice", flowPW)
	chID := f.field(w, "challengeId")
	if w.Code != 202 || chID == "" || cookieOf(w) != nil || !strings.Contains(w.Body.String(), `"totp"`) {
		t.Fatalf("expected a 202 challenge without a cookie: %d %s", w.Code, w.Body)
	}
	if w := f.do("POST", "/api/v1/auth/2fa/verify", map[string]any{"challengeId": chID, "method": "totp", "code": "000000", "trustDevice": false}); w.Code != 401 || problemType(t, w) != "invalid-code" {
		t.Fatalf("wrong code: %d %s", w.Code, w.Body)
	}
	if w := f.do("POST", "/api/v1/auth/2fa/verify", map[string]any{"challengeId": chID, "method": "totp", "code": totpCode(t, secret, f.clk.Now())},
		withHeader("User-Agent", "curl/8")); w.Code != 401 {
		t.Fatalf("a challenge is bound to the browser that started it: %d", w.Code)
	}
	w = f.do("POST", "/api/v1/auth/2fa/verify", map[string]any{"challengeId": chID, "method": "totp", "code": totpCode(t, secret, f.clk.Now())})
	s = f.sessionOf(w)
	if !strings.Contains(w.Body.String(), `"totp"`) {
		t.Fatalf("session must record the factor used: %s", w.Body)
	}
	if w := f.do("POST", "/api/v1/auth/2fa/verify", map[string]any{"challengeId": chID, "method": "totp", "code": totpCode(t, secret, f.clk.Now())}); w.Code != 401 {
		t.Fatalf("challenge must be single use: %d", w.Code)
	}
	if w := f.do("GET", "/api/v1/system/info", nil, withCookie(s.cookie)); w.Code != 200 {
		t.Fatalf("full session expected: %d", w.Code)
	}

	// dangerous actions need a step-up
	if w := f.do("POST", "/api/v1/auth/recovery-codes:regenerate", nil, s.opts()...); w.Code != 403 || problemType(t, w) != "step-up-required" {
		t.Fatalf("regenerate without step-up: %d %s", w.Code, w.Body)
	}
	if w := f.do("DELETE", "/api/v1/auth/totp", nil, s.opts()...); w.Code != 403 || problemType(t, w) != "step-up-required" {
		t.Fatalf("remove without step-up: %d %s", w.Code, w.Body)
	}
	w = f.do("POST", "/api/v1/auth/stepup/begin", nil, s.opts()...)
	su := f.field(w, "challengeId")
	if w.Code != 200 || su == "" || strings.Contains(w.Body.String(), `"password"`) {
		t.Fatalf("stepup/begin must offer the second factor, not the password: %d %s", w.Code, w.Body)
	}
	f.clk.Advance(30 * time.Second)
	code := totpCode(t, secret, f.clk.Now())
	if w := f.do("POST", "/api/v1/auth/stepup", map[string]any{"challengeId": su, "method": "totp", "code": code}, s.opts()...); w.Code != 204 {
		t.Fatalf("stepup: %d %s", w.Code, w.Body)
	}
	// a step-up challenge belongs to its session
	other := f.sessionOf(func() *httptest.ResponseRecorder { f.clk.Advance(30 * time.Second); return f.fullLogin(t, secret) }())
	if w := f.do("POST", "/api/v1/auth/stepup", map[string]any{"challengeId": su, "method": "totp", "code": code}, other.opts()...); w.Code != 401 {
		t.Fatalf("challenge reused from another session: %d", w.Code)
	}
	w = f.do("POST", "/api/v1/auth/recovery-codes:regenerate", nil, s.opts()...)
	if err := json.Unmarshal(w.Body.Bytes(), &conf); err != nil || w.Code != 200 || len(conf.Codes) != 10 {
		t.Fatalf("regenerate inside the window: %d %s", w.Code, w.Body)
	}
	f.clk.Advance(6 * time.Minute) // window closed
	if w := f.do("DELETE", "/api/v1/auth/totp", nil, s.opts()...); w.Code != 403 {
		t.Fatalf("the step-up window must expire: %d", w.Code)
	}

	// recovery code login, once
	f.clk.Advance(30 * time.Second)
	w, _ = f.login("alice", flowPW)
	rc := f.field(w, "challengeId")
	if w := f.do("POST", "/api/v1/auth/2fa/verify", map[string]any{"challengeId": rc, "method": "recovery", "code": strings.ToLower(conf.Codes[0])}); w.Code != 200 {
		t.Fatalf("recovery login: %d %s", w.Code, w.Body)
	}
	w, _ = f.login("alice", flowPW)
	rc = f.field(w, "challengeId")
	if w := f.do("POST", "/api/v1/auth/2fa/verify", map[string]any{"challengeId": rc, "method": "recovery", "code": conf.Codes[0]}); w.Code != 401 {
		t.Fatalf("a recovery code must work once: %d", w.Code)
	}
}

// fullLogin signs alice in through the 202 flow.
func (f *flow) fullLogin(t *testing.T, secret string) *httptest.ResponseRecorder {
	t.Helper()
	w, _ := f.login("alice", flowPW)
	return f.do("POST", "/api/v1/auth/2fa/verify", map[string]any{"challengeId": f.field(w, "challengeId"), "method": "totp", "code": totpCode(t, secret, f.clk.Now())})
}

func TestChallengeAllowsOnlyFiveAttempts(t *testing.T) {
	f := newFlow(t, "admins", nil)
	f.enrol("alice", flowPW)
	w, _ := f.login("alice", flowPW)
	s := f.sessionOf(w)
	secret := f.field(f.do("POST", "/api/v1/auth/totp/enroll", nil, s.opts()...), "secret")
	f.do("POST", "/api/v1/auth/totp/confirm", map[string]string{"code": totpCode(t, secret, f.clk.Now())}, s.opts()...)
	f.clk.Advance(30 * time.Second)

	w, _ = f.login("alice", flowPW)
	ch := f.field(w, "challengeId")
	for i := 0; i < 5; i++ {
		f.do("POST", "/api/v1/auth/2fa/verify", map[string]any{"challengeId": ch, "method": "totp", "code": "111111"})
	}
	// the challenge is gone: even the right code is refused now
	if w := f.do("POST", "/api/v1/auth/2fa/verify", map[string]any{"challengeId": ch, "method": "totp", "code": totpCode(t, secret, f.clk.Now())}); w.Code == 200 {
		t.Fatal("challenge survived five failed attempts")
	}
}

func TestStepUpWithPasswordOnlyForAccountsWithoutSecondFactor(t *testing.T) {
	f := newFlow(t, "none", nil)
	f.enrol("bob", flowPW)
	w, _ := f.login("bob", flowPW)
	s := f.sessionOf(w)
	w = f.do("POST", "/api/v1/auth/stepup/begin", nil, s.opts()...)
	if !strings.Contains(w.Body.String(), `"password"`) || strings.Contains(w.Body.String(), `"totp"`) {
		t.Fatalf("methods: %s", w.Body)
	}
	ch := f.field(w, "challengeId")
	if w := f.do("POST", "/api/v1/auth/stepup", map[string]any{"challengeId": ch, "method": "password", "password": "wrong-password-xx"}, s.opts()...); w.Code != 401 {
		t.Fatalf("wrong password: %d", w.Code)
	}
	if w := f.do("POST", "/api/v1/auth/stepup", map[string]any{"challengeId": ch, "method": "totp", "code": "123456"}, s.opts()...); w.Code != 400 {
		t.Fatalf("totp is not offered to this account: %d", w.Code)
	}
	if w := f.do("POST", "/api/v1/auth/stepup", map[string]any{"challengeId": ch, "method": "password", "password": flowPW}, s.opts()...); w.Code != 204 {
		t.Fatalf("right password: %d %s", w.Code, w.Body)
	}
}
