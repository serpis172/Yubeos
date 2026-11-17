#!/usr/bin/env bash
# End-to-end smoke test of the real binaries: helper as root, daemon as an unprivileged user, real Linux accounts.
# Needs: root, Linux, curl, python3, setpriv. Creates users alice (admin), bob (user) and the service user 'yubeos'
# if missing — run it in the VM lab or a container, NOT on a machine where that matters.
#   usage: test/e2e/auth-smoke.sh [dir-with-built-binaries]     (default: ./dist)
set -euo pipefail
BIN=${1:-./dist}; W=$(mktemp -d); B=https://localhost:18443; PW='correct-horse-battery-staple'
fail() { echo "FAIL: $*" >&2; exit 1; }
cleanup() { pkill -x yubeosd 2>/dev/null || true; pkill -x yubeos-priv 2>/dev/null || true; rm -rf "$W"; }
trap cleanup EXIT
[ "$(id -u)" = 0 ] || fail "run as root"
for b in yubeosd yubeos-priv yubeosctl; do [ -x "$BIN/$b" ] || fail "missing $BIN/$b (build with: go build -o dist/ ./cmd/...)"; done

getent group yubeos-admin >/dev/null || groupadd yubeos-admin
getent group yubeos-user  >/dev/null || groupadd yubeos-user
id yubeos >/dev/null 2>&1 || useradd -r -s /usr/sbin/nologin yubeos
id alice  >/dev/null 2>&1 || useradd -m -G yubeos-admin alice
id bob    >/dev/null 2>&1 || useradd -m -G yubeos-user bob

mkdir -p "$W/run" "$W/yd" "$W/priv"; chmod 755 "$W" "$W/run" "$W/priv"; chown yubeos:yubeos "$W/yd"
setsid "$BIN/yubeos-priv" --socket "$W/run/priv.sock" --state-dir "$W/priv" --daemon-user yubeos >"$W/helper.log" 2>&1 </dev/null &
for _ in $(seq 80); do [ -S "$W/run/priv.sock" ] && break; sleep 0.25; done; [ -S "$W/run/priv.sock" ] || fail "helper did not start"
setsid setpriv --reuid=yubeos --regid=yubeos --init-groups "$BIN/yubeosd" --state-dir "$W/yd" --listen 127.0.0.1:18443 --priv-socket "$W/run/priv.sock" >"$W/yd.log" 2>&1 </dev/null &
for _ in $(seq 40); do curl -sk "$B/healthz" >/dev/null 2>&1 && break; sleep 0.25; done; curl -sk "$B/healthz" | grep -q '"ok":true' || fail "daemon did not start"

# isolation: the daemon user can neither read the credential store nor mint setup links
setpriv --reuid=yubeos --regid=yubeos --init-groups cat "$W/priv/pepper.key" 2>/dev/null && fail "daemon user can read the pepper"
out=$(setpriv --reuid=yubeos --regid=yubeos --init-groups "$BIN/yubeosctl" user add bob --socket "$W/run/priv.sock" 2>&1 || true)  # non-zero exit is expected here
grep -q "requires a root caller" <<<"$out" || fail "daemon user could create a setup link: $out"

code() { curl -sk -o /dev/null -w '%{http_code}' "$@"; }
tok() { "$BIN/yubeosctl" user add "$1" ${2:-} --socket "$W/run/priv.sock" --url "$B" | grep -o 'token=[A-Za-z0-9_-]*' | cut -d= -f2; }
JSON=(-H 'Content-Type: application/json')

T=$(tok bob)
[ "$(code -X POST "$B/api/v1/auth/setup" "${JSON[@]}" -d "{\"token\":\"$T\",\"newPassword\":\"short\"}")" = 422 ] || fail "weak password not rejected"
[ "$(code -X POST "$B/api/v1/auth/setup" "${JSON[@]}" -d "{\"token\":\"$T\",\"newPassword\":\"$PW\"}")" = 204 ] || fail "setup failed"
[ "$(code -X POST "$B/api/v1/auth/setup" "${JSON[@]}" -d "{\"token\":\"$T\",\"newPassword\":\"${PW}2\"}")" = 400 ] || fail "setup link reusable"

curl -sk -c "$W/jar" -X POST "$B/api/v1/auth/login" "${JSON[@]}" -d "{\"username\":\"bob\",\"password\":\"$PW\"}" >"$W/me.json"
CSRF=$(python3 -c "import json,sys;print(json.load(open('$W/me.json'))['csrf'])")
[ "$(code -b "$W/jar" "$B/api/v1/system/info")" = 200 ] || fail "session not accepted"
[ "$(code -b "$W/jar" -X POST "$B/api/v1/auth/logout")" = 403 ] || fail "CSRF header not required"
[ "$(code -b "$W/jar" -X POST -H "X-Yubeos-CSRF: $CSRF" -H 'Origin: https://evil.example' "$B/api/v1/auth/logout")" = 403 ] || fail "foreign Origin accepted"
[ "$(code -b "$W/jar" -c "$W/jar" -X POST -H "X-Yubeos-CSRF: $CSRF" "$B/api/v1/auth/logout")" = 204 ] || fail "logout failed"
[ "$(code -b "$W/jar" "$B/api/v1/auth/me")" = 401 ] || fail "session survived logout"

T=$(tok alice --admin)
code -X POST "$B/api/v1/auth/setup" "${JSON[@]}" -d "{\"token\":\"$T\",\"newPassword\":\"$PW\"}" >/dev/null
curl -sk -c "$W/jar2" -X POST "$B/api/v1/auth/login" "${JSON[@]}" -d "{\"username\":\"alice\",\"password\":\"$PW\"}" | grep -q '"enrollmentRequired":true' || fail "admin without 2FA must get an enrollment-only session"
[ "$(code -b "$W/jar2" "$B/api/v1/system/info")" = 403 ] || fail "enrollment-only session reached a normal route"

for i in $(seq 10); do code -X POST "$B/api/v1/auth/login" "${JSON[@]}" -d "{\"username\":\"bob\",\"password\":\"wrong-$i-xxxxxx\"}" >/dev/null; done
[ "$(code -X POST "$B/api/v1/auth/login" "${JSON[@]}" -d "{\"username\":\"bob\",\"password\":\"$PW\"}")" = 429 ] || fail "no lockout after 10 failures"

"$BIN/yubeosctl" audit verify --state-dir "$W/yd" | grep -q "intact" || fail "audit chain not intact"
grep -q 'yubeos-auth fail user=bob ip=127.0.0.1 reason=bad-credentials' "$W/yd.log" || fail "fail2ban line missing"
echo "auth smoke test: OK"
