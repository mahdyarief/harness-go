#!/usr/bin/env bash
# Rebuild and redeploy after a feature lands. User testing sees the new build
# once this exits; the tunnel and database keep running.
set -euo pipefail

DEPLOY_DIR="$(cd "$(dirname "$0")/.." && pwd)"
REPO="${REPO:-$(cd "$DEPLOY_DIR/.." && pwd)}"
API_ORIGIN="${API_ORIGIN:-https://api-hsg.aspirecx.my.id}"

cd "$REPO"
docker build -f server/Dockerfile --build-arg "VITE_API_URL=$API_ORIGIN" -t hsg-app:dev .

cd "$DEPLOY_DIR"
docker compose up -d --force-recreate api
docker compose ps
