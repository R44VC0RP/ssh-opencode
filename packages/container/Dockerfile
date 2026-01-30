# Build PTY bridge
FROM golang:1.22-alpine AS builder

RUN apk add --no-cache git

WORKDIR /build
COPY pty-bridge/go.mod pty-bridge/go.sum* ./
RUN go mod download 2>/dev/null || true

COPY pty-bridge/ .
RUN go mod tidy
RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -ldflags="-s -w" -o pty-bridge .

# Final image
FROM ghcr.io/anomalyco/opencode:latest

# Switch to root for setup
USER root

# Install additional dependencies (Alpine uses apk)
RUN apk add --no-cache \
    ca-certificates \
    git \
    openssh-client \
    curl

# Copy PTY bridge binary
COPY --from=builder /build/pty-bridge /usr/local/bin/pty-bridge

# Copy entrypoint script
COPY scripts/entrypoint.sh /usr/local/bin/entrypoint.sh
RUN chmod +x /usr/local/bin/entrypoint.sh /usr/local/bin/pty-bridge

# Create directories
RUN mkdir -p /root/dev /root/.local/share/opencode /root/.config/opencode /data

# Environment
ENV HOME=/root
ENV USER=root
ENV TERM=xterm-256color
ENV COLORTERM=truecolor
ENV PTY_BRIDGE_PORT=8080

# Expose PTY bridge port
EXPOSE 8080

# Run entrypoint
ENTRYPOINT ["/usr/local/bin/entrypoint.sh"]

# Version label to force new image digest
LABEL version="2026.01.29.v7" \
      description="PTY bridge with zero-wait writeread for minimum latency"
