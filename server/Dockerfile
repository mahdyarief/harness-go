# syntax=docker/dockerfile:1

# Build context is the repository root, so the web app is reachable:
#   docker build -f server/Dockerfile .

FROM node:22-alpine AS web
WORKDIR /web
RUN corepack enable
COPY web/package.json web/pnpm-lock.yaml ./
RUN --mount=type=cache,target=/root/.local/share/pnpm/store \
    corepack pnpm install --frozen-lockfile
COPY web/ ./
# Vite inlines VITE_* at build time, so the API origin is baked into the SPA.
ARG VITE_API_URL
ENV VITE_API_URL=$VITE_API_URL
RUN corepack pnpm build

# The build stage runs on the builder's own architecture and cross-compiles,
# so a multi-platform image needs no emulation.
FROM --platform=$BUILDPLATFORM golang:1.27.3-alpine AS build
WORKDIR /src

COPY server/go.mod server/go.sum ./
RUN --mount=type=cache,target=/go/pkg/mod go mod download

COPY server/ ./
# The SPA is embedded into the binary, so the final image carries no assets.
COPY --from=web /web/dist ./internal/web/dist

ARG TARGETOS TARGETARCH
RUN --mount=type=cache,target=/go/pkg/mod \
    --mount=type=cache,target=/root/.cache/go-build \
    CGO_ENABLED=0 GOOS=$TARGETOS GOARCH=$TARGETARCH \
    go build -trimpath -ldflags="-s -w" -o /out/ ./cmd/api ./cmd/migrate
# Empty knowledge base directory; distroless has no shell to mkdir at runtime.
RUN mkdir /kb

# Static image with CA certificates and tzdata, no shell, running as nonroot.
FROM gcr.io/distroless/static-debian12:nonroot
WORKDIR /app

COPY --from=build /out/api /out/migrate ./
# 65532:65532 is distroless "nonroot". A named volume mounted at /data/kb
# inherits this ownership when it is first created.
COPY --from=build --chown=65532:65532 --chmod=0700 /kb /data/kb

EXPOSE 8080
ENTRYPOINT ["/app/api"]
