# Build stage
FROM golang:1.22-alpine AS builder

RUN apk add --no-cache git gcc musl-dev

WORKDIR /build

# Copy go mod files
COPY go.mod go.sum* ./
RUN go mod download 2>/dev/null || true

# Copy source
COPY . .

# Download dependencies and build
RUN go mod tidy
RUN CGO_ENABLED=1 GOOS=linux go build -ldflags="-s -w" -o ssh-relay ./cmd/relay

# Runtime stage
FROM alpine:3.19

RUN apk add --no-cache \
    ca-certificates \
    openssh-keygen \
    sqlite

# Copy binary
COPY --from=builder /build/ssh-relay /usr/local/bin/ssh-relay

# Create directories
RUN mkdir -p /etc/ssh-opencode /var/lib/ssh-opencode

# Environment defaults
ENV SSH_LISTEN_ADDR=:22
ENV SSH_HOST_KEY_PATH=/etc/ssh-opencode/host_key
ENV SSH_KEY_DB_PATH=/var/lib/ssh-opencode/keys.db

EXPOSE 22

# Run as non-root (optional, comment out if port 22 requires root)
# RUN adduser -D -u 1000 sshrelay
# USER sshrelay

ENTRYPOINT ["/usr/local/bin/ssh-relay"]
