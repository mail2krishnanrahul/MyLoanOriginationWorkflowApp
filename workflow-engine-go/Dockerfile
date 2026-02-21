FROM golang:1.24-alpine AS builder

WORKDIR /build

# Cache dependencies
COPY go.mod go.sum ./
RUN go mod download

# Build
COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -ldflags="-w -s" -o server ./cmd/api
RUN CGO_ENABLED=0 GOOS=linux go build -ldflags="-w -s" -o migrate_cli ./cmd/migrate

# Runtime stage
FROM alpine:3.19

RUN apk --no-cache add ca-certificates curl

WORKDIR /app
COPY --from=builder /build/server .
COPY --from=builder /build/migrate_cli ./migrate
COPY --from=builder /build/db/migrations ./db/migrations

EXPOSE 8080
HEALTHCHECK CMD curl -f http://localhost:8080/health || exit 1

# Run as non-root
USER 1000

ENTRYPOINT ["/app/server"]
