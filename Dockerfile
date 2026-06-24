# Build Stage
FROM golang:1.25-alpine AS builder

WORKDIR /src

# Install build dependencies
RUN apk add --no-cache git ca-certificates

# Copy go.mod and go.sum for caching
COPY go.mod go.sum ./
RUN go mod download

# Copy source code
COPY . .

# Build statically linked binaries
RUN CGO_ENABLED=0 GOOS=linux go build -ldflags="-w -s" -o bin/doktriai-api ./cmd/doktriai-api
RUN CGO_ENABLED=0 GOOS=linux go build -ldflags="-w -s" -o bin/doktriai-cli ./cmd/doktriai-cli

# Run Stage
FROM alpine:3.19

WORKDIR /app

# Install standard security tools and ca-certificates
RUN apk add --no-cache ca-certificates curl

# Copy binaries from builder
COPY --from=builder /src/bin/doktriai-api /app/doktriai-api
COPY --from=builder /src/bin/doktriai-cli /app/doktriai-cli

# Copy static frontend assets
COPY --from=builder /src/web /app/web

# Expose API port
EXPOSE 18080

# Set volume mount for persistent SQLite state
VOLUME [ "/app/data" ]

# Run the API server with SQLite directory directed to /app/data
ENTRYPOINT [ "/app/doktriai-api", "-addr", ":18080", "-data-dir", "/app/data", "-web-dir", "/app/web" ]
