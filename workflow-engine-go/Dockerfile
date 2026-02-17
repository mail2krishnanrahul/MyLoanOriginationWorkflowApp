# Build Stage
FROM alpine:latest AS builder

WORKDIR /app

# Install Go
RUN apk add --no-cache go

# Copy source
COPY go.mod go.sum ./
RUN go mod download

# Build the application
COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -o workflow-engine ./cmd/workflow-engine

# Runtime Stage
FROM alpine:latest

WORKDIR /

COPY --from=builder /app/workflow-engine /workflow-engine

# Expose Health Check Port
EXPOSE 8080

ENTRYPOINT ["/workflow-engine"]
