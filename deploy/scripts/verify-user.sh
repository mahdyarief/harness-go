#!/usr/bin/env bash
# Staging-only: mark an account's email as verified so a tester can sign in
# without the Resend verification mail. Run after the tester signs up.
#
#   ./scripts/verify-user.sh tester@example.com
#
# Why this exists: Authula's email plugin is always enabled with Resend and
# refuses sign-in until the address is verified. Until a real RESEND_API_KEY is
# configured, this is the only way to onboard a tester.
set -euo pipefail

if [ $# -ne 1 ]; then
  echo "usage: $0 <email>" >&2
  exit 2
fi

cd "$(dirname "$0")/.."
set -a
# shellcheck disable=SC1091
. ./.env
set +a

docker compose exec -T -e PGPASSWORD="$POSTGRES_PASSWORD" db \
  psql -v ON_ERROR_STOP=1 -U hsg -d hsg -v email="$1" <<'SQL'
-- Only authula.users tracks verification; public.users is the app profile
-- copy and has no such column.
UPDATE authula.users SET email_verified = true, updated_at = now() WHERE email = :'email';
SQL
