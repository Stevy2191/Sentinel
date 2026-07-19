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
EXPOSE 3001
CMD ["./sentinel"]
