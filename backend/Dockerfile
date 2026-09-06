# Build stage. Go version must satisfy go.mod (go 1.25).
FROM golang:1.25-alpine AS builder

# Module source and network behaviour, overridable at build time:
#   --build-arg GOPROXY=https://your-mirror/,direct
#   --build-arg GOPROXY=off            (fully offline, needs a vendor/ dir)
# GOPRIVATE/GONOSUMDB are not needed here: every dependency is public.
#
# goproxy.cn is listed first because Google's CDN times out from some regions.
# The chain falls through to proxy.golang.org and then a direct fetch, so the
# ordering is a latency preference rather than a dependency on any one mirror.
#
# Note this is set as an ENV, not via `go env -w`: an OS environment variable
# takes precedence over the go env config file, and `go env -w` in a later RUN
# would be silently ignored here - Go itself warns
# "does not override conflicting OS environment variable".
ARG GOPROXY=https://goproxy.cn,https://proxy.golang.org,direct
ARG GOSUMDB=sum.golang.org
ENV GOPROXY=${GOPROXY}     GOSUMDB=${GOSUMDB}     GOFLAGS=-mod=mod

WORKDIR /app
COPY go.mod go.sum ./
# Cache mounts keep the module and build caches across image builds, so a code
# change no longer re-downloads every dependency.
RUN --mount=type=cache,target=/go/pkg/mod     go mod download

COPY . .
# Static binary (pure-Go pgx driver) so it runs on a minimal alpine image.
# -trimpath drops absolute build paths; -s -w strips the symbol table.
RUN --mount=type=cache,target=/go/pkg/mod     --mount=type=cache,target=/root/.cache/go-build     CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o sentinel ./cmd/sentinel

# Runtime stage.
FROM alpine:latest
# curl is used by the docker-compose healthcheck; tzdata for time zones.
RUN apk --no-cache add ca-certificates tzdata curl
WORKDIR /app
COPY --from=builder /app/sentinel .
COPY migrations/ ./migrations/
# Generated report PDFs land here. Mount it as a volume in compose, or reports
# are lost whenever the container is replaced.
#
# No PDF engine is installed on purpose: reports are drawn in pure Go
# (services/pdf_renderer.go). wkhtmltopdf, the usual choice, has no Alpine
# package and was archived upstream in 2023 with open CVEs.
RUN mkdir -p /app/reports && chmod 755 /app/reports
VOLUME ["/app/reports"]
EXPOSE 3001
CMD ["./sentinel"]
