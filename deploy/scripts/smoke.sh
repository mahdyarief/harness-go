#!/usr/bin/env bash
# Post-release smoke test. Run after scripts/release.sh to confirm the public
# hostnames, the split-origin build, CORS and auth still work.
#
#   ./scripts/smoke.sh
#
# Exit code 0 means every check passed.
set -uo pipefail

FE="${FE:-https://hsg.aspirecx.my.id}"
API="${API:-https://api-hsg.aspirecx.my.id}"
fail=0

check() {
  local label="$1" expected="$2" actual="$3"
  if [ "$actual" = "$expected" ]; then
    echo "PASS  $label ($actual)"
  else
    echo "FAIL  $label (expected $expected, got $actual)"
    fail=1
  fi
}

code() { curl -sS -o /dev/null -w '%{http_code}' --max-time 25 "$@"; }

check "api health" 200 "$(code "$API/health")"
check "spa root" 200 "$(code "$FE/")"
check "spa deep link" 200 "$(code "$FE/sign-in")"

cors=$(curl -sS -o /dev/null -D - --max-time 25 -X OPTIONS \
  "$API/api/v1/auth/email-password/sign-in" \
  -H "Origin: $FE" \
  -H 'Access-Control-Request-Method: POST' \
  -H 'Access-Control-Request-Headers: content-type' |
  tr -d '\r' | awk -F': ' 'tolower($1)=="access-control-allow-origin"{print $2}')
check "cors preflight" "$FE" "$cors"

# The SPA must call the API hostname, not its own origin.
bundle=$(curl -sS --max-time 25 "$FE/" | grep -oE '/assets/index-[A-Za-z0-9_-]+\.js' | head -1)
if [ -n "$bundle" ]; then
  entry=$(curl -sS --max-time 25 "$FE$bundle")
  hit=0
  for asset in $(echo "$entry" | grep -oE 'assets/[A-Za-z0-9_.-]+\.js' | sort -u); do
    if curl -sS --max-time 20 "$FE/$asset" | grep -q "${API#https://}"; then
      hit=1
      break
    fi
  done
  check "spa uses api origin" 1 "$hit"
else
  echo "FAIL  spa uses api origin (no entry bundle found)"
  fail=1
fi

check "admin-only route rejects anonymous" 401 \
  "$(code -H 'Authorization: Bearer invalid' "$API/api/v1/platform/llm-config")"

if [ -n "${SMOKE_EMAIL:-}" ] && [ -n "${SMOKE_PASSWORD:-}" ]; then
  login=$(curl -sS -o /dev/null -w '%{http_code}' --max-time 25 \
    -X POST "$API/api/v1/auth/email-password/sign-in" \
    -H 'Content-Type: application/json' -H "Origin: $FE" \
    -d "{\"email\":\"$SMOKE_EMAIL\",\"password\":\"$SMOKE_PASSWORD\"}")
  check "smoke user sign-in" 200 "$login"
fi

if [ "$fail" -ne 0 ]; then
  echo
  echo "smoke test failed"
  exit 1
fi
echo
echo "smoke test passed"
