#!/bin/bash
set -e

# SSH OpenCode Setup Script
# This script helps set up the project for local development and deployment

echo "=== SSH OpenCode Setup ==="
echo ""

# Check for required tools
check_tool() {
    if ! command -v "$1" &> /dev/null; then
        echo "ERROR: $1 is not installed"
        return 1
    fi
    echo "  [OK] $1"
}

echo "Checking required tools..."
MISSING=0
check_tool "go" || MISSING=1
check_tool "node" || MISSING=1
check_tool "npm" || MISSING=1
check_tool "docker" || MISSING=1

if [ $MISSING -eq 1 ]; then
    echo ""
    echo "Please install missing tools and run again."
    exit 1
fi

echo ""
echo "Setting up packages..."

# Setup worker package
echo ""
echo "==> packages/worker"
cd packages/worker
npm install
cd ../..

# Setup ssh-relay package
echo ""
echo "==> packages/ssh-relay"
cd packages/ssh-relay
go mod tidy
cd ../..

# Setup container pty-bridge
echo ""
echo "==> packages/container/pty-bridge"
cd packages/container/pty-bridge
go mod tidy
cd ../../..

echo ""
echo "=== Setup Complete ==="
echo ""
echo "Next steps:"
echo ""
echo "1. Copy .env.example to .env and fill in your values:"
echo "   cp .env.example .env"
echo ""
echo "2. Deploy the Cloudflare Worker:"
echo "   cd packages/worker"
echo "   npx wrangler r2 bucket create opencode-state"
echo "   npm run deploy"
echo ""
echo "3. Build and push the container image:"
echo "   cd packages/container"
echo "   docker build --platform linux/amd64 -t YOUR_REGISTRY/opencode-container:latest ."
echo "   docker push YOUR_REGISTRY/opencode-container:latest"
echo ""
echo "4. Deploy the SSH relay to your VPS:"
echo "   cd packages/ssh-relay"
echo "   # See README.md for deployment options"
echo ""
