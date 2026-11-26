package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"slices"
	"testing"
	"time"

	"github.com/serpis172/Yubeos/internal/auth"
	"github.com/serpis172/Yubeos/internal/totp"
	"github.com/serpis172/Yubeos/internal/webauthn"
	"github.com/serpis172/Yubeos/internal/webauthn/webauthntest"
)

type sessn struct{ ck, csrf string }

// as sends a state-changing request as a signed-in browser (cookie + CSRF + same-origin).
func (f *flow) as(s sessn, method, path string, body any, opts ...reqOpt) *httptest.ResponseRecorder {
	f.t.Helper()
	o := append([]reqOpt{withCookie(s.ck), withCSRF(s.csrf), withHeader("Origin", "https://nas.lan")}, opts...)
	return f.do(method, path, body, o...)
}

func withDevice(v string) reqOpt {
	return func(r *http.Request) { r.AddCookie(&http.Cookie{Name: auth.DeviceCookie, Value: v}) }
}

func cookieNamed(w *httptest.ResponseRecorder, name string) *http.Cookie {
	for _, c := range w.Result().Cookies() {
		if c.Name == name {
			return c
		}
	}
	return nil
}

func (f *flow) mustSignIn(user string, opts ...reqOpt) sessn {
	f.t.Helper()
	w, m := f.login(user, flowPW, opts...)
	if w.Code != 200 || cookieOf(w) == nil {
		f.t.Fatalf("sign-in: %d %s", w.Code, w.Body)
	}
	return sessn{cookieOf(w).Value, m.CSRF}
}

// code returns a TOTP code from a fresh time step (each step is usable once).
func (f *flow) code(secret []byte) string {
	f.clk.Advance(31 * time.Second)
	return totp.Code(secret, totp.Step(f.clk.Now()))
}

func decode[T any](t *testing.T, w *httptest.ResponseRecorder) T {
	t.Helper()
	var v T
	if err := json.Unmarshal(w.Body.Bytes(), &v); err != nil {
		t.Fatalf("bad JSON %q: %v", w.Body, err)
	}
	return v
}

// setupWithTOTP enrols a password and a TOTP secret for user through the HTTP API and returns the secret and codes.
func (f *flow) setupWithTOTP(user string) (secret []byte, recovery []string) {
	f.t.Helper()
	f.enrol(user, flowPW)
	s := f.mustSignIn(user) // enrolment-only session (policy demands a second factor)
	w := f.as(s, "POST", "/api/v1/auth/totp/enroll", nil)
	if w.Code != 200 {
		f.t.Fatalf("totp enroll: %d %s", w.Code, w.Body)
	}
	e := decode[struct {
		Secret string `json:"secret"`
		URI    string `json:"otpauthUri"`
	}](f.t, w)
	secret, err := totp.DecodeSecret(e.Secret)
	if err != nil || e.URI == "" {
		f.t.Fatalf("bad enrol answer: %s", w.Body)
	}
	w = f.as(s, "POST", "/api/v1/auth/totp/confirm", map[string]string{"code": f.code(secret)})
	if w.Code != 200 {
		f.t.Fatalf("totp confirm: %d %s", w.Code, w.Body)
	}
	recovery = decode[struct {
		Codes []string `json:"codes"`
	}](f.t, w).Codes
	if len(recovery) != 10 {
		f.t.Fatalf("want 10 recovery codes, got %d", len(recovery))
	}
	if w := f.do("GET", "/api/v1/system/info", nil, withCookie(s.ck)); w.Code != 200 {
		f.t.Fatalf("session must be unlocked after enrolment: %d", w.Code)
	}
	f.as(s, "POST", "/api/v1/auth/logout", nil)
	return secret, recovery
}

type needs2fa struct {
	Needs2FA    bool     `json:"needs2fa"`
	Methods     []string `json:"methods"`
	ChallengeID string   `json:"challengeId"`
}

func (f *flow) challenge(user string, opts ...reqOpt) needs2fa {
	f.t.Helper()
	w, _ := f.login(user, flowPW, opts...)
	if w.Code != 202 {
		f.t.Fatalf("want a second-factor challenge (202), got %d %s", w.Code, w.Body)
	}
	if cookieOf(w) != nil {
		f.t.Fatal("no session cookie may exist before the second factor")
	}
	return decode[needs2fa](f.t, w)
}

func (f *flow) verify(chID, method, code string, trust bool) *httptest.ResponseRecorder {
	return f.do("POST", "/api/v1/auth/2fa/verify", map[string]any{"challengeId": chID, "method": method, "code": code, "trustDevice": trust})
}

func (f *flow) signInWith2FA(user string, secret []byte) sessn {
	f.t.Helper()
	ch := f.challenge(user)
	w := f.verify(ch.ChallengeID, "totp", f.code(secret), false)
	if w.Code != 200 {
		f.t.Fatalf("verify: %d %s", w.Code, w.Body)
	}
	return sessn{cookieOf(w).Value, decode[me](f.t, w).CSRF}
}

func TestSecondFactorLoginWithTOTP(t *testing.T) {
	f := newFlow(t, "all", nil)
	secret, _ := f.setupWithTOTP("bob")

	ch := f.challenge("bob")
	if !ch.Needs2FA || ch.ChallengeID == "" || !slices.Contains(ch.Methods, "totp") || !slices.Contains(ch.Methods, "recovery") {
		t.Fatalf("challenge: %+v", ch)
	}
	if w := f.verify(ch.ChallengeID, "totp", "000000", false); w.Code != 401 || problemType(t, w) != "invalid-second-factor" {
		t.Fatalf("wrong code: %d %s", w.Code, w.Body)
	}
	code := f.code(secret)
	w := f.verify(ch.ChallengeID, "totp", code, false)
	if w.Code != 200 || cookieOf(w) == nil {
		t.Fatalf("right code: %d %s", w.Code, w.Body)
	}
	if m := decode[me](t, w); m.Username != "bob" || !slices.Equal(m.Factors, []string{"password", "totp"}) {
		t.Fatalf("me: %+v", m)
	}
	// the challenge is single use and the same code cannot be replayed on a new one
	if w := f.verify(ch.ChallengeID, "totp", code, false); w.Code != 400 {
		t.Fatalf("used challenge must be refused: %d", w.Code)
	}
	ch2 := f.challenge("bob")
	if w := f.verify(ch2.ChallengeID, "totp", code, false); w.Code != 401 {
		t.Fatalf("replayed TOTP code must be refused: %d %s", w.Code, w.Body)
	}
}

func TestChallengeIsBoundToTheClient(t *testing.T) {
	f := newFlow(t, "all", nil)
	secret, _ := f.setupWithTOTP("bob")
	ch := f.challenge("bob")
	if w := f.do("POST", "/api/v1/auth/2fa/verify", map[string]any{"challengeId": ch.ChallengeID, "method": "totp", "code": f.code(secret)}, fromIP("192.168.1.99")); w.Code != 400 {
		t.Fatalf("challenge used from another address: %d %s", w.Code, w.Body)
	}
}

func TestRecoveryCodesWorkOnce(t *testing.T) {
	f := newFlow(t, "all", nil)
	_, codes := f.setupWithTOTP("bob")
	ch := f.challenge("bob")
	if w := f.verify(ch.ChallengeID, "recovery", codes[0], false); w.Code != 200 {
		t.Fatalf("recovery code: %d %s", w.Code, w.Body)
	}
	ch = f.challenge("bob")
	if w := f.verify(ch.ChallengeID, "recovery", codes[0], false); w.Code != 401 {
		t.Fatalf("a recovery code must work once: %d", w.Code)
	}
	if w := f.verify(ch.ChallengeID, "recovery", codes[1], false); w.Code != 200 {
		t.Fatalf("second code: %d %s", w.Code, w.Body)
	}
}

func TestTrustedDeviceSkipsTOTPRotatesAndDetectsReuse(t *testing.T) {
	f := newFlow(t, "all", nil)
	secret, _ := f.setupWithTOTP("bob")

	ch := f.challenge("bob")
	w := f.verify(ch.ChallengeID, "totp", f.code(secret), true)
	dev := cookieNamed(w, auth.DeviceCookie)
	if w.Code != 200 || dev == nil || !dev.Secure || !dev.HttpOnly || dev.SameSite != http.SameSiteStrictMode || dev.MaxAge <= 0 {
		t.Fatalf("trust device: %d %+v", w.Code, dev)
	}

	// next sign-in with the device cookie needs no code, rotates the token, and is marked as device-based
	w, m := f.login("bob", flowPW, withDevice(dev.Value))
	dev2 := cookieNamed(w, auth.DeviceCookie)
	if w.Code != 200 || !slices.Equal(m.Factors, []string{"password", "device"}) || dev2 == nil || dev2.Value == dev.Value {
		t.Fatalf("device login: %d %s", w.Code, w.Body)
	}
	// a different browser family does not benefit from it
	firefox := withHeader("User-Agent", "Mozilla/5.0 (X11; Linux x86_64; rv:130.0) Gecko/20100101 Firefox/130.0")
	if w, _ := f.login("bob", flowPW, withDevice(dev2.Value), firefox); w.Code != 202 {
		t.Fatalf("device token from another browser must not skip 2FA: %d", w.Code)
	}
	// the previous token still works inside the short grace window (parallel requests) without rotating again
	if w, _ := f.login("bob", flowPW, withDevice(dev.Value)); w.Code != 200 || cookieNamed(w, auth.DeviceCookie) != nil {
		t.Fatalf("grace window: %d", w.Code)
	}
	// ...but a rotated token presented after the grace window is theft: device revoked, both tokens dead
	f.clk.Advance(time.Minute)
	if w, _ := f.login("bob", flowPW, withDevice(dev.Value)); w.Code != 202 {
		t.Fatalf("reuse of a rotated token must demand 2FA: %d", w.Code)
	}
	if w, _ := f.login("bob", flowPW, withDevice(dev2.Value)); w.Code != 202 {
		t.Fatalf("the revoked device's current token must be dead too: %d", w.Code)
	}
}

func TestTrustedDeviceListRevokeAndStepUpRule(t *testing.T) {
	f := newFlow(t, "all", nil)
	secret, _ := f.setupWithTOTP("bob")
	ch := f.challenge("bob")
	dev := cookieNamed(f.verify(ch.ChallengeID, "totp", f.code(secret), true), auth.DeviceCookie)

	w, m := f.login("bob", flowPW, withDevice(dev.Value)) // session opened by the trusted device
	s := sessn{cookieOf(w).Value, m.CSRF}
	var id string
	for _, it := range decode[struct {
		Items []struct{ ID, Kind string }
	}](t, f.do("GET", "/api/v1/auth/devices", nil, withCookie(s.ck))).Items {
		if it.Kind == "trusted-device" {
			id = it.ID
		}
	}
	if id == "" {
		t.Fatal("trusted device must be listed")
	}
	// a session that skipped 2FA through a trusted device cannot satisfy step-up on its own
	if w := f.as(s, "POST", "/api/v1/auth/recovery-codes:regenerate", nil); w.Code != 403 || problemType(t, w) != "step-up-required" {
		t.Fatalf("trusted device must not unlock step-up: %d %s", w.Code, w.Body)
	}
	if w := f.as(s, "DELETE", "/api/v1/auth/devices/"+id, nil); w.Code != 204 {
		t.Fatalf("revoke trusted device: %d %s", w.Code, w.Body)
	}
	if w := f.do("GET", "/api/v1/system/info", nil, withCookie(s.ck)); w.Code != 401 {
		t.Fatalf("sessions opened by a revoked device must die: %d", w.Code)
	}
	if w, _ := f.login("bob", flowPW, withDevice(dev.Value)); w.Code != 202 {
		t.Fatalf("revoked device must demand 2FA: %d", w.Code)
	}
}

func TestStepUpGuardsFactorChanges(t *testing.T) {
	f := newFlow(t, "all", nil)
	secret, _ := f.setupWithTOTP("bob")
	s := f.signInWith2FA("bob", secret)

	if w := f.as(s, "POST", "/api/v1/auth/recovery-codes:regenerate", nil); w.Code != 403 || problemType(t, w) != "step-up-required" {
		t.Fatalf("without step-up: %d %s", w.Code, w.Body)
	}
	w := f.as(s, "POST", "/api/v1/auth/stepup/begin", nil)
	b := decode[struct {
		ChallengeID string   `json:"challengeId"`
		Methods     []string `json:"methods"`
	}](t, w)
	if w.Code != 200 || b.ChallengeID == "" || !slices.Contains(b.Methods, "totp") {
		t.Fatalf("stepup/begin: %d %s", w.Code, w.Body)
	}
	if w := f.as(s, "POST", "/api/v1/auth/stepup", map[string]any{"challengeId": b.ChallengeID, "method": "totp", "code": "111111"}); w.Code != 401 {
		t.Fatalf("wrong step-up code: %d", w.Code)
	}
	if w := f.as(s, "POST", "/api/v1/auth/stepup", map[string]any{"challengeId": b.ChallengeID, "method": "totp", "code": f.code(secret)}); w.Code != 204 {
		t.Fatalf("step-up: %d %s", w.Code, w.Body)
	}
	w = f.as(s, "POST", "/api/v1/auth/recovery-codes:regenerate", nil)
	if w.Code != 200 || len(decode[struct{ Codes []string }](t, w).Codes) != 10 {
		t.Fatalf("regenerate after step-up: %d %s", w.Code, w.Body)
	}
	// the window is short
	f.clk.Advance(6 * time.Minute)
	if w := f.as(s, "POST", "/api/v1/auth/recovery-codes:regenerate", nil); w.Code != 403 {
		t.Fatalf("step-up window must expire: %d", w.Code)
	}
	// policy "all": the last second factor cannot be removed even with a fresh step-up
	b = decode[struct {
		ChallengeID string   `json:"challengeId"`
		Methods     []string `json:"methods"`
	}](t, f.as(s, "POST", "/api/v1/auth/stepup/begin", nil))
	if w := f.as(s, "POST", "/api/v1/auth/stepup", map[string]any{"challengeId": b.ChallengeID, "method": "totp", "code": f.code(secret)}); w.Code != 204 {
		t.Fatalf("second step-up: %d", w.Code)
	}
	if w := f.as(s, "DELETE", "/api/v1/auth/totp", nil); w.Code != 409 || problemType(t, w) != "last-factor" {
		t.Fatalf("removing the last factor: %d %s", w.Code, w.Body)
	}
	fa := decode[struct {
		TOTP     bool `json:"totp"`
		Required bool `json:"required"`
		Left     int  `json:"recoveryCodesLeft"`
	}](t, f.do("GET", "/api/v1/auth/factors", nil, withCookie(s.ck)))
	if !fa.TOTP || !fa.Required || fa.Left != 10 {
		t.Fatalf("factors: %+v", fa)
	}
}

func TestPasswordStepUpWhenNoSecondFactorExists(t *testing.T) {
	f := newFlow(t, "none", nil)
	f.enrol("alice", flowPW)
	s := f.mustSignIn("alice")
	b := decode[struct {
		ChallengeID string   `json:"challengeId"`
		Methods     []string `json:"methods"`
	}](t, f.as(s, "POST", "/api/v1/auth/stepup/begin", nil))
	if !slices.Equal(b.Methods, []string{"password"}) {
		t.Fatalf("methods: %v", b.Methods)
	}
	if w := f.as(s, "POST", "/api/v1/auth/stepup", map[string]any{"challengeId": b.ChallengeID, "method": "password", "password": "wrong-wrong-wrong"}); w.Code != 401 {
		t.Fatalf("wrong password: %d", w.Code)
	}
	if w := f.as(s, "POST", "/api/v1/auth/stepup", map[string]any{"challengeId": b.ChallengeID, "method": "password", "password": flowPW}); w.Code != 204 {
		t.Fatalf("password step-up: %d %s", w.Code, w.Body)
	}
}

func assertionBody(chID string, au *webauthntest.Authenticator, ad, cd, sig []byte, extra map[string]any) map[string]any {
	b := map[string]any{"challengeId": chID, "credential": map[string]any{
		"id": webauthn.B64(au.ID), "rawId": webauthn.B64(au.ID),
		"response": map[string]any{"authenticatorData": webauthn.B64(ad), "clientDataJSON": webauthn.B64(cd), "signature": webauthn.B64(sig)},
	}}
	for k, v := range extra {
		b[k] = v
	}
	return b
}

type waBegin struct {
	ChallengeID string   `json:"challengeId"`
	Methods     []string `json:"methods"`
	WebAuthn    struct {
		Challenge        string `json:"challenge"`
		RPID             string `json:"rpId"`
		AllowCredentials []struct {
			ID string `json:"id"`
		} `json:"allowCredentials"`
		RP struct {
			ID string `json:"id"`
		} `json:"rp"`
	} `json:"webauthn"`
}

func TestPasskeyRegistrationAndLogin(t *testing.T) {
	f := newFlow(t, "all", nil)
	f.enrol("bob", flowPW)
	s := f.mustSignIn("bob") // enrolment-only
	au := webauthntest.New(t, "nas.lan", "https://nas.lan")

	w := f.as(s, "POST", "/api/v1/auth/webauthn/register/begin", nil)
	reg := decode[waBegin](t, w)
	if w.Code != 200 || reg.WebAuthn.RP.ID != "nas.lan" {
		t.Fatalf("register/begin: %d %s", w.Code, w.Body)
	}
	chal, _ := webauthn.FromB64(reg.WebAuthn.Challenge)
	att, cd := au.Register(chal)
	w = f.as(s, "POST", "/api/v1/auth/webauthn/register/finish", map[string]any{"challengeId": reg.ChallengeID, "label": "Test key", "credential": map[string]any{
		"id": webauthn.B64(au.ID), "rawId": webauthn.B64(au.ID),
		"response": map[string]any{"attestationObject": webauthn.B64(att), "clientDataJSON": webauthn.B64(cd)},
	}})
	if w.Code != 200 {
		t.Fatalf("register/finish: %d %s", w.Code, w.Body)
	}
	if w := f.do("GET", "/api/v1/system/info", nil, withCookie(s.ck)); w.Code != 200 {
		t.Fatalf("a passkey satisfies the enrolment requirement: %d", w.Code)
	}
	f.as(s, "POST", "/api/v1/auth/logout", nil)

	// 1) passkey as the sole factor
	w = f.do("POST", "/api/v1/auth/webauthn/login/begin", nil)
	b := decode[waBegin](t, w)
	if w.Code != 200 || len(b.WebAuthn.AllowCredentials) != 0 {
		t.Fatalf("usernameless begin: %d %s", w.Code, w.Body)
	}
	chal, _ = webauthn.FromB64(b.WebAuthn.Challenge)
	ad, cdj, sig := au.Assert(chal)
	w = f.do("POST", "/api/v1/auth/webauthn/login/finish", assertionBody(b.ChallengeID, au, ad, cdj, sig, nil))
	if w.Code != 200 || !slices.Equal(decode[me](t, w).Factors, []string{"passkey"}) {
		t.Fatalf("passkey login: %d %s", w.Code, w.Body)
	}

	// 2) passkey as the second factor after the password
	ch := f.challenge("bob")
	if !slices.Contains(ch.Methods, "webauthn") {
		t.Fatalf("methods: %v", ch.Methods)
	}
	w = f.do("POST", "/api/v1/auth/webauthn/login/begin", map[string]string{"challengeId": ch.ChallengeID})
	b = decode[waBegin](t, w)
	if w.Code != 200 || len(b.WebAuthn.AllowCredentials) != 1 {
		t.Fatalf("bound begin: %d %s", w.Code, w.Body)
	}
	chal, _ = webauthn.FromB64(b.WebAuthn.Challenge)
	ad, cdj, sig = au.Assert(chal)
	w = f.do("POST", "/api/v1/auth/webauthn/login/finish", assertionBody(ch.ChallengeID, au, ad, cdj, sig, nil))
	if w.Code != 200 || !slices.Equal(decode[me](t, w).Factors, []string{"password", "passkey"}) {
		t.Fatalf("password + passkey: %d %s", w.Code, w.Body)
	}

	// 3) a signature counter that does not increase = cloned authenticator: refused and the credential is blocked
	w = f.do("POST", "/api/v1/auth/webauthn/login/begin", nil)
	b = decode[waBegin](t, w)
	chal, _ = webauthn.FromB64(b.WebAuthn.Challenge)
	ad, cdj, sig = au.AssertWithCount(chal)
	if w := f.do("POST", "/api/v1/auth/webauthn/login/finish", assertionBody(b.ChallengeID, au, ad, cdj, sig, nil)); w.Code != 403 || problemType(t, w) != "passkey-flagged" {
		t.Fatalf("counter regression: %d %s", w.Code, w.Body)
	}
	w = f.do("POST", "/api/v1/auth/webauthn/login/begin", nil)
	b = decode[waBegin](t, w)
	chal, _ = webauthn.FromB64(b.WebAuthn.Challenge)
	ad, cdj, sig = au.Assert(chal)
	if w := f.do("POST", "/api/v1/auth/webauthn/login/finish", assertionBody(b.ChallengeID, au, ad, cdj, sig, nil)); w.Code != 403 {
		t.Fatalf("a flagged passkey stays blocked: %d", w.Code)
	}
}

func TestPasskeyFromAnotherOriginIsRejected(t *testing.T) {
	f := newFlow(t, "all", nil)
	f.enrol("bob", flowPW)
	s := f.mustSignIn("bob")
	phisher := webauthntest.New(t, "nas.lan", "https://evil.example") // page on another origin
	reg := decode[waBegin](t, f.as(s, "POST", "/api/v1/auth/webauthn/register/begin", nil))
	chal, _ := webauthn.FromB64(reg.WebAuthn.Challenge)
	att, cd := phisher.Register(chal)
	w := f.as(s, "POST", "/api/v1/auth/webauthn/register/finish", map[string]any{"challengeId": reg.ChallengeID, "credential": map[string]any{
		"id": webauthn.B64(phisher.ID), "rawId": webauthn.B64(phisher.ID),
		"response": map[string]any{"attestationObject": webauthn.B64(att), "clientDataJSON": webauthn.B64(cd)},
	}})
	if w.Code != 400 || problemType(t, w) != "webauthn-invalid" {
		t.Fatalf("origin mismatch: %d %s", w.Code, w.Body)
	}
}

func TestLockdownRefusesRemoteOriginsOnly(t *testing.T) {
	f := newFlow(t, "none", nil)
	if err := os.WriteFile(f.lock, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if w := f.do("GET", "/api/v1/auth/me", nil, fromIP("203.0.113.9")); w.Code != 503 || problemType(t, w) != "lockdown" {
		t.Fatalf("remote during lockdown: %d %s", w.Code, w.Body)
	}
	if w := f.do("GET", "/api/v1/auth/me", nil); w.Code != 401 {
		t.Fatalf("local origin must be unaffected: %d", w.Code)
	}
	_ = os.Remove(f.lock)
	time.Sleep(1100 * time.Millisecond) // the flag file is polled once per second
	if w := f.do("GET", "/api/v1/auth/me", nil, fromIP("203.0.113.9")); w.Code == 503 {
		t.Fatalf("lockdown must lift when the flag is removed: %d", w.Code)
	}
}

func TestRemoteStatusAndLockdownSwitch(t *testing.T) {
	f := newFlow(t, "none", nil)
	f.enrol("alice", flowPW)
	f.enrol("bob", flowPW)
	bob := f.mustSignIn("bob")
	if w := f.do("GET", "/api/v1/security/remote-status", nil, withCookie(bob.ck)); w.Code != 403 {
		t.Fatalf("non-admin: %d", w.Code)
	}
	alice := f.mustSignIn("alice")
	w := f.do("GET", "/api/v1/security/remote-status", nil, withCookie(alice.ck))
	st := decode[struct {
		Exposure string `json:"exposure"`
		Lockdown bool   `json:"lockdown"`
		Checks   []struct{ ID, Status, Title string }
	}](t, w)
	if w.Code != 200 || st.Exposure != "lan" || st.Lockdown || len(st.Checks) < 6 {
		t.Fatalf("remote-status: %d %s", w.Code, w.Body)
	}
	// switching lockdown needs step-up
	if w := f.as(alice, "POST", "/api/v1/security/lockdown", map[string]bool{"enabled": true}); w.Code != 403 || problemType(t, w) != "step-up-required" {
		t.Fatalf("lockdown without step-up: %d %s", w.Code, w.Body)
	}
	b := decode[waBegin](t, f.as(alice, "POST", "/api/v1/auth/stepup/begin", nil))
	if w := f.as(alice, "POST", "/api/v1/auth/stepup", map[string]any{"challengeId": b.ChallengeID, "method": "password", "password": flowPW}); w.Code != 204 {
		t.Fatalf("step-up: %d %s", w.Code, w.Body)
	}
	if w := f.as(alice, "POST", "/api/v1/security/lockdown", map[string]bool{"enabled": true}); w.Code != 204 {
		t.Fatalf("lockdown on: %d %s", w.Code, w.Body)
	}
	if _, err := os.Stat(f.lock); err != nil {
		t.Fatalf("flag file must exist: %v", err)
	}
	if w := f.as(alice, "POST", "/api/v1/security/lockdown", map[string]bool{"enabled": false}); w.Code != 204 {
		t.Fatalf("lockdown off: %d", w.Code)
	}
	if _, err := os.Stat(f.lock); !os.IsNotExist(err) {
		t.Fatalf("flag file must be gone: %v", err)
	}
}

func TestPreferencesArePerUserAndValidated(t *testing.T) {
	f := newFlow(t, "none", nil)
	f.enrol("alice", flowPW)
	f.enrol("bob", flowPW)
	a, b := f.mustSignIn("alice"), f.mustSignIn("bob")

	empty := decode[struct {
		Theme     string `json:"theme"`
		Language  string `json:"language"`
		UpdatedAt int64  `json:"updatedAt"`
	}](t, f.do("GET", "/api/v1/me/preferences", nil, withCookie(a.ck)))
	if empty.Theme != "system" || empty.Language != "en" || empty.UpdatedAt != 0 {
		t.Fatalf("defaults: %+v", empty)
	}
	if w := f.as(a, "PUT", "/api/v1/me/preferences", map[string]any{"theme": "yubeos-midnight", "language": "it", "units": "bits", "temperature": "f"}); w.Code != 200 {
		t.Fatalf("put: %d %s", w.Code, w.Body)
	}
	got := decode[struct {
		Theme, Language, Units, Temperature string
		UpdatedAt                           int64
	}](t, f.do("GET", "/api/v1/me/preferences", nil, withCookie(a.ck)))
	if got.Theme != "yubeos-midnight" || got.Language != "it" || got.Units != "bits" || got.Temperature != "f" || got.UpdatedAt == 0 {
		t.Fatalf("round trip: %+v", got)
	}
	other := decode[struct{ Language string }](t, f.do("GET", "/api/v1/me/preferences", nil, withCookie(b.ck)))
	if other.Language != "en" {
		t.Fatalf("preferences must be per user, bob got %+v", other)
	}
	for name, body := range map[string]map[string]any{
		"bad language": {"language": "fr"},
		"bad theme id": {"theme": "Bad Theme!"},
		"bad unit":     {"units": "furlongs"},
		"junk theme":   {"customThemes": []any{map[string]any{"schemaVersion": 1, "id": "x"}}},
		"too many":     {"customThemes": make([]any, 13)},
	} {
		if w := f.as(a, "PUT", "/api/v1/me/preferences", body); w.Code != 422 && w.Code != 400 {
			t.Errorf("%s accepted: %d %s", name, w.Code, w.Body)
		}
	}
	if w := f.do("GET", "/api/v1/me/preferences", nil); w.Code != 401 {
		t.Fatalf("anonymous: %d", w.Code)
	}
}
