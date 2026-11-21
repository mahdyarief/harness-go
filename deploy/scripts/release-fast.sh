#!/usr/bin/env bash
# Fast local redeploy: build the SPA and the Go binaries on the host, then
# assemble a runtime-only image. No node/golang stages run inside Docker.
#
# Use ./scripts/release.sh when you need a reproducible from-source image build
# (another machine, CI, or a clean dependency cache). This script assumes the
# host already has Go, Node, pnpm's node_modules and the distroless base image.
#
#   ./scripts/release-fast.sh                 # build + recreate api
#   ./scripts/release-fast.sh --with-migrations
#   ./scripts/release-fast.sh --no-health-wait
#   ./scripts/release-fast.sh --build-only    # build the image, do not deploy
set -euo pipefail

DEPLOY_DIR="$(cd "$(dirname "$0")/.." && pwd)"
REPO="${REPO:-$(cd "$DEPLOY_DIR/.." && pwd)}"
API_ORIGIN="${API_ORIGIN:-https://api-hsg.aspirecx.my.id}"
IMAGE="${IMAGE:-hsg-app:dev}"
HEALTH_URL="${HEALTH_URL:-http://127.0.0.1:8432/health}"
STAGE_DIR="${STAGE_DIR:-$REPO/server/.release}"

WITH_MIGRATIONS=0
WAIT_HEALTH=1
BUILD_ONLY=0
for arg in "$@"; do
  case "$arg" in
    --with-migrations) WITH_MIGRATIONS=1 ;;
    --no-health-wait) WAIT_HEALTH=0 ;;
    --build-only) BUILD_ONLY=1 ;;
    *) echo "unknown flag: $arg" >&2; exit 2 ;;
  esac
done

log() { printf '\n==> %s\n' "$*"; }
elapsed() { awk -v end="$(date +%s%N)" -v start="$1" 'BEGIN { printf "    %.1fs\n", (end-start)/1e9 }'; }

start_all=$(date +%s%N)

log "building SPA (VITE_API_URL=$API_ORIGIN)"
t0=$(date +%s%N)
cd "$REPO/web"
VITE_API_URL="$API_ORIGIN" ./node_modules/.bin/vite build
elapsed "$t0"

log "staging SPA into server/internal/web/dist"
rsync -a --delete --exclude='.gitkeep' "$REPO/web/dist/" "$REPO/server/internal/web/dist/"

log "building linux/amd64 binaries"
t0=$(date +%s%N)
mkdir -p "$STAGE_DIR"
cd "$REPO/server"
CGO_ENABLED=0 GOOS=linux GOARCH=amd64 \
  go build -trimpath -ldflags="-s -w" -o "$STAGE_DIR/" ./cmd/api ./cmd/migrate
elapsed "$t0"

log "assembling runtime image $IMAGE"
t0=$(date +%s%N)
docker build -f "$REPO/server/Dockerfile.runtime" -t "$IMAGE" "$STAGE_DIR"
elapsed "$t0"

if [ "$BUILD_ONLY" = 1 ]; then
  log "image built; skipping deploy"
  exit 0
fi

if [ "$WITH_MIGRATIONS" = 1 ]; then
  log "applying migrations"
  "$DEPLOY_DIR/scripts/init-db.sh"
fi

log "recreating api container"
t0=$(date +%s%N)
cd "$DEPLOY_DIR"
docker compose up -d --force-recreate api
elapsed "$t0"

if [ "$WAIT_HEALTH" = 1 ]; then
  log "waiting for $HEALTH_URL"
  t0=$(date +%s%N)
  for _ in $(seq 1 30); do
    if curl -fsS "$HEALTH_URL" >/dev/null 2>&1; then
      elapsed "$t0"
      break
    fi
    sleep 1
  done
  if ! curl -fsS "$HEALTH_URL" >/dev/null 2>&1; then
    echo "health check failed; inspect with: docker compose logs --tail=50 api" >&2
    exit 1
  fi
fi

printf '\n==> total %s\n' "$(awk -v end="$(date +%s%N)" -v start="$start_all" 'BEGIN { printf "%.1fs", (end-start)/1e9 }')"
docker compose ps api
