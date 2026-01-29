#!/bin/bash
set -e

# SSH Relay Deployment Script for VPS
# Usage: ./deploy-vps.sh [user@host]

if [ -z "$1" ]; then
    echo "Usage: $0 user@host"
    echo ""
    echo "This script deploys the SSH relay to a VPS via SSH."
    echo ""
    echo "Prerequisites:"
    echo "  - Docker installed on the VPS"
    echo "  - Port 22 available (or configure alternate port)"
    echo "  - WORKER_URL environment variable set"
    exit 1
fi

VPS_HOST="$1"
REGISTRY="${REGISTRY:-ghcr.io}"
IMAGE_NAME="${IMAGE_NAME:-ssh-opencode/ssh-relay}"
IMAGE_TAG="${IMAGE_TAG:-latest}"

echo "=== Deploying SSH Relay to $VPS_HOST ==="
echo ""

# Check for .env file
if [ -f .env ]; then
    echo "Loading configuration from .env..."
    source .env
fi

if [ -z "$WORKER_URL" ]; then
    echo "ERROR: WORKER_URL is not set"
    echo "Set it in .env or as environment variable"
    exit 1
fi

echo "Worker URL: $WORKER_URL"
echo "Image: $REGISTRY/$IMAGE_NAME:$IMAGE_TAG"
echo ""

# Build the image locally
echo "Building SSH relay image..."
cd packages/ssh-relay
docker build --platform linux/amd64 -t "$REGISTRY/$IMAGE_NAME:$IMAGE_TAG" .
cd ../..

# Push to registry (if using remote registry)
if [[ "$REGISTRY" != "local" ]]; then
    echo "Pushing image to registry..."
    docker push "$REGISTRY/$IMAGE_NAME:$IMAGE_TAG"
fi

# Deploy to VPS
echo "Deploying to VPS..."
ssh "$VPS_HOST" bash -s << EOF
set -e

# Pull the image
docker pull $REGISTRY/$IMAGE_NAME:$IMAGE_TAG || true

# Stop existing container
docker stop ssh-relay 2>/dev/null || true
docker rm ssh-relay 2>/dev/null || true

# Create directories
sudo mkdir -p /etc/ssh-opencode /var/lib/ssh-opencode

# Run the container
docker run -d \\
    --name ssh-relay \\
    --restart unless-stopped \\
    --network host \\
    -v /etc/ssh-opencode:/etc/ssh-opencode \\
    -v /var/lib/ssh-opencode:/var/lib/ssh-opencode \\
    -e WORKER_URL="$WORKER_URL" \\
    -e AUTH_SECRET="${AUTH_SECRET:-}" \\
    $REGISTRY/$IMAGE_NAME:$IMAGE_TAG

echo "SSH relay deployed successfully!"
docker logs ssh-relay
EOF

echo ""
echo "=== Deployment Complete ==="
echo ""
echo "Test with: ssh $VPS_HOST"
