#!/bin/bash
set -e

# Setup directories
mkdir -p /root/dev
mkdir -p /root/.local/share/opencode
mkdir -p /root/.config/opencode

# If R2 mount is available, symlink state directories
if [ -d "/data/opencode" ]; then
    # Remove existing directory and create symlink
    rm -rf /root/.local/share/opencode
    ln -sf /data/opencode /root/.local/share/opencode
    echo "Linked OpenCode state to /data/opencode"
fi

if [ -d "/data/dev" ]; then
    rm -rf /root/dev
    ln -sf /data/dev /root/dev
    echo "Linked dev workspace to /data/dev"
fi

# Ensure git is configured
git config --global init.defaultBranch main
git config --global user.email "opencode@localhost"
git config --global user.name "OpenCode"
git config --global --add safe.directory '*'

# Start PTY bridge
exec /usr/local/bin/pty-bridge "$@"
