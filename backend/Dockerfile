# Build stage. Go version must satisfy go.mod (go 1.25).
FROM golang:1.25-alpine AS builder
WORKDIR /app
COPY go.mod go.sum ./
RUN go mod download
COPY . .
# Static binary (pure-Go pgx driver) so it runs on a minimal alpine image.
RUN CGO_ENABLED=0 go build -o sentinel ./cmd/sentinel

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
