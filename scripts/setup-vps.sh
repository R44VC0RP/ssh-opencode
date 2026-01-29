#!/bin/bash
set -e

# VPS Setup Script for SSH OpenCode
# Tested on: Ubuntu 22.04+, Debian 12+
# Usage: curl -fsSL https://raw.githubusercontent.com/.../setup-vps.sh | bash

echo "=== SSH OpenCode VPS Setup ==="
echo ""

# Detect OS
if [ -f /etc/os-release ]; then
    . /etc/os-release
    OS=$ID
    VERSION=$VERSION_ID
else
    echo "ERROR: Cannot detect OS"
    exit 1
fi

echo "Detected: $OS $VERSION"

# Check if running as root
if [ "$EUID" -ne 0 ]; then
    echo "Please run as root (sudo)"
    exit 1
fi

# Install Docker if not present
install_docker() {
    if command -v docker &> /dev/null; then
        echo "[OK] Docker already installed"
        return
    fi

    echo "Installing Docker..."
    
    case $OS in
        ubuntu|debian)
            apt-get update
            apt-get install -y ca-certificates curl gnupg
            install -m 0755 -d /etc/apt/keyrings
            curl -fsSL https://download.docker.com/linux/$OS/gpg | gpg --dearmor -o /etc/apt/keyrings/docker.gpg
            chmod a+r /etc/apt/keyrings/docker.gpg
            echo "deb [arch=$(dpkg --print-architecture) signed-by=/etc/apt/keyrings/docker.gpg] https://download.docker.com/linux/$OS $(. /etc/os-release && echo "$VERSION_CODENAME") stable" | tee /etc/apt/sources.list.d/docker.list > /dev/null
            apt-get update
            apt-get install -y docker-ce docker-ce-cli containerd.io docker-buildx-plugin docker-compose-plugin
            ;;
        fedora|centos|rhel)
            dnf -y install dnf-plugins-core
            dnf config-manager --add-repo https://download.docker.com/linux/fedora/docker-ce.repo
            dnf install -y docker-ce docker-ce-cli containerd.io docker-buildx-plugin docker-compose-plugin
            systemctl start docker
            systemctl enable docker
            ;;
        *)
            echo "ERROR: Unsupported OS for Docker auto-install: $OS"
            echo "Please install Docker manually and re-run this script"
            exit 1
            ;;
    esac

    systemctl enable docker
    systemctl start docker
    echo "[OK] Docker installed"
}

# Create directories
setup_directories() {
    echo "Creating directories..."
    mkdir -p /etc/ssh-opencode
    mkdir -p /var/lib/ssh-opencode
    chmod 700 /etc/ssh-opencode
    chmod 700 /var/lib/ssh-opencode
    echo "[OK] Directories created"
}

# Generate SSH host key if not exists
setup_host_key() {
    if [ -f /etc/ssh-opencode/host_key ]; then
        echo "[OK] Host key already exists"
        return
    fi

    echo "Generating SSH host key..."
    ssh-keygen -t ed25519 -f /etc/ssh-opencode/host_key -N "" -q
    chmod 600 /etc/ssh-opencode/host_key
    echo "[OK] Host key generated"
}

# Create environment file template
setup_env() {
    if [ -f /etc/ssh-opencode/ssh-relay.env ]; then
        echo "[OK] Environment file exists"
        return
    fi

    echo "Creating environment file..."
    cat > /etc/ssh-opencode/ssh-relay.env << 'EOF'
# SSH Relay Configuration
# Edit this file with your settings

# REQUIRED: Cloudflare Worker WebSocket URL
# Example: wss://opencode-relay.your-subdomain.workers.dev/ws
WORKER_URL=

# OPTIONAL: Shared secret for worker authentication
AUTH_SECRET=

# SSH relay settings (defaults shown)
SSH_LISTEN_ADDR=:22
SSH_HOST_KEY_PATH=/etc/ssh-opencode/host_key
SSH_KEY_DB_PATH=/var/lib/ssh-opencode/keys.db
AUTO_REGISTER=true
EOF
    chmod 600 /etc/ssh-opencode/ssh-relay.env
    echo "[OK] Environment file created at /etc/ssh-opencode/ssh-relay.env"
}

# Create systemd service
setup_systemd() {
    echo "Creating systemd service..."
    cat > /etc/systemd/system/ssh-relay.service << 'EOF'
[Unit]
Description=SSH OpenCode Relay Server
After=network.target docker.service
Requires=docker.service

[Service]
Type=simple
Restart=always
RestartSec=5

# Load environment
EnvironmentFile=/etc/ssh-opencode/ssh-relay.env

# Run via Docker
ExecStartPre=-/usr/bin/docker stop ssh-relay
ExecStartPre=-/usr/bin/docker rm ssh-relay
ExecStartPre=/usr/bin/docker pull ghcr.io/anomalyco/ssh-relay:latest
ExecStart=/usr/bin/docker run --rm --name ssh-relay \
    --network host \
    -v /etc/ssh-opencode:/etc/ssh-opencode:ro \
    -v /var/lib/ssh-opencode:/var/lib/ssh-opencode \
    -e WORKER_URL \
    -e AUTH_SECRET \
    -e SSH_LISTEN_ADDR \
    -e SSH_HOST_KEY_PATH \
    -e SSH_KEY_DB_PATH \
    -e AUTO_REGISTER \
    ghcr.io/anomalyco/ssh-relay:latest
ExecStop=/usr/bin/docker stop ssh-relay

[Install]
WantedBy=multi-user.target
EOF
    systemctl daemon-reload
    echo "[OK] Systemd service created"
}

# Configure firewall
setup_firewall() {
    echo "Configuring firewall..."
    
    # Try ufw first (Ubuntu/Debian)
    if command -v ufw &> /dev/null; then
        ufw allow 22/tcp comment 'SSH OpenCode Relay'
        echo "[OK] UFW rule added for port 22"
        return
    fi

    # Try firewalld (Fedora/CentOS/RHEL)
    if command -v firewall-cmd &> /dev/null; then
        firewall-cmd --permanent --add-port=22/tcp
        firewall-cmd --reload
        echo "[OK] Firewalld rule added for port 22"
        return
    fi

    echo "[WARN] No firewall detected. Ensure port 22 is open."
}

# Check if port 22 is available
check_port() {
    if ss -tlnp | grep -q ':22 '; then
        echo ""
        echo "[WARN] Port 22 is already in use!"
        echo "       You may need to:"
        echo "       1. Move system SSH to another port (e.g., 2222)"
        echo "       2. Or configure ssh-relay to use a different port"
        echo ""
        echo "To move system SSH, edit /etc/ssh/sshd_config:"
        echo "  Port 2222"
        echo "Then: systemctl restart sshd"
        echo ""
    fi
}

# Main setup
main() {
    install_docker
    setup_directories
    setup_host_key
    setup_env
    setup_systemd
    setup_firewall
    check_port

    echo ""
    echo "=== Setup Complete ==="
    echo ""
    echo "Next steps:"
    echo ""
    echo "1. Edit the configuration file:"
    echo "   nano /etc/ssh-opencode/ssh-relay.env"
    echo ""
    echo "   Set WORKER_URL to your Cloudflare Worker URL"
    echo ""
    echo "2. Start the service:"
    echo "   systemctl enable ssh-relay"
    echo "   systemctl start ssh-relay"
    echo ""
    echo "3. Check status:"
    echo "   systemctl status ssh-relay"
    echo "   journalctl -u ssh-relay -f"
    echo ""
    echo "4. Test connection:"
    echo "   ssh your-domain.com"
    echo ""
}

main "$@"
