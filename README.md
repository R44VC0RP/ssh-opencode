# SSH OpenCode

Run [OpenCode](https://opencode.ai) in the cloud via SSH. Connect from anywhere with a simple command.

```bash
ssh code.example.com              # Open your workspace
ssh code.example.com user/repo    # Clone and open a GitHub repo
```

## How It Works

```
Your Terminal
     │
     │ SSH (port 22)
     ▼
┌─────────────────┐      WebSocket       ┌────────────────────┐
│   VPS Server    │─────────────────────▶│  Cloudflare Worker │
│  (SSH Relay)    │                      └────────────────────┘
└─────────────────┘                               │
                                                  ▼
                                        ┌────────────────────┐
                                        │     Container      │
                                        │  ┌──────────────┐  │
                                        │  │ OpenCode TUI │  │
                                        │  └──────────────┘  │
                                        │   Persistent R2    │
                                        │     Storage        │
                                        └────────────────────┘
```

1. **SSH Relay** (your VPS) accepts SSH connections and authenticates via public key
2. **Cloudflare Worker** routes sessions to Durable Objects on the edge
3. **Container** runs OpenCode TUI with persistent storage via R2

## Features

- **Instant access** — Just SSH to your domain, no client setup needed
- **Persistent state** — Sessions, config, and `~/dev` workspace survive restarts
- **GitHub integration** — `ssh domain user/repo` clones and opens repos automatically
- **Auto-sleep** — Containers sleep after 30 min idle to save costs
- **SSH key auth** — Secure public key authentication with auto-registration
- **Edge deployment** — Containers run on Cloudflare's global network

## Quick Start

### Prerequisites

- VPS with port 22 available (Ubuntu 22.04+ recommended)
- Domain pointing to your VPS
- Cloudflare account with [Workers Paid](https://developers.cloudflare.com/workers/platform/pricing/) ($5/mo)
- Cloudflare Containers enabled ([beta](https://developers.cloudflare.com/containers/))

### 1. Clone and Configure

```bash
git clone https://github.com/R44VC0RP/ssh-opencode
cd ssh-opencode
cp .env.example .env
```

Edit `.env`:

```bash
DOMAIN=code.example.com
CLOUDFLARE_ACCOUNT_ID=your-account-id
CLOUDFLARE_API_TOKEN=your-api-token
```

### 2. Deploy Cloudflare Worker

```bash
cd packages/worker
npm install
npx wrangler r2 bucket create opencode-state
npm run deploy
```

### 3. Setup VPS

**Option A: One-liner install**

```bash
curl -fsSL https://raw.githubusercontent.com/R44VC0RP/ssh-opencode/main/scripts/setup-vps.sh | sudo bash
```

**Option B: Manual setup**

```bash
# On your VPS
git clone https://github.com/R44VC0RP/ssh-opencode
cd ssh-opencode
sudo ./scripts/setup-vps.sh
```

Then configure and start:

```bash
sudo nano /etc/ssh-opencode/ssh-relay.env  # Set WORKER_URL
sudo systemctl enable ssh-relay
sudo systemctl start ssh-relay
```

### 4. Configure DNS

Point your domain's A record to your VPS IP. **Proxy must be OFF** (grey cloud) since SSH can't go through Cloudflare's HTTP proxy.

### 5. Connect!

```bash
ssh code.example.com
```

First connection auto-registers your SSH key. You'll be dropped into OpenCode TUI.

## Architecture

```
ssh-opencode/
├── packages/
│   ├── ssh-relay/      # Go SSH server (runs on VPS)
│   ├── worker/         # Cloudflare Worker + Durable Object
│   ├── container/      # Docker image with PTY bridge
│   └── local-proxy/    # Local dev: simulates CF Worker
├── scripts/
│   ├── setup.sh        # Local dev setup
│   ├── setup-vps.sh    # VPS provisioning
│   └── deploy-vps.sh   # Deploy to VPS
└── docker-compose.yml  # Local development
```

| Package | Description | Tech |
|---------|-------------|------|
| `ssh-relay` | Accepts SSH, proxies to Worker via WebSocket | Go, gliderlabs/ssh |
| `worker` | Routes sessions, manages container lifecycle, WebSocket streaming | TypeScript, Cloudflare Workers |
| `container` | Runs PTY bridge + OpenCode TUI | Go, Docker |
| `local-proxy` | Local dev only: simulates CF Worker | Go |

## Configuration

### Environment Variables

**SSH Relay** (`/etc/ssh-opencode/ssh-relay.env`):

| Variable | Description | Default |
|----------|-------------|---------|
| `WORKER_URL` | Cloudflare Worker WebSocket URL | **Required** |
| `AUTH_SECRET` | Shared secret for worker auth | Optional |
| `SSH_LISTEN_ADDR` | Listen address | `:22` |
| `AUTO_REGISTER` | Auto-register new SSH keys | `true` |

**Cloudflare Worker** (via `wrangler.jsonc` or secrets):

| Variable | Description |
|----------|-------------|
| `IDLE_TIMEOUT_MINUTES` | Minutes before container sleeps (default: 30) |
| `AUTH_SECRET` | Shared secret (set via `wrangler secret put`) |

### SSH Client Config

Add to `~/.ssh/config` for convenience:

```
Host code
    HostName code.example.com
    User _
    RequestTTY yes
    ForwardAgent yes
```

Then: `ssh code` or `ssh code user/repo`

## Development

### Local Testing

```bash
# Setup
./scripts/setup.sh

# Run SSH relay (port 2222 to avoid conflict)
docker-compose up ssh-relay

# Test
ssh -p 2222 localhost
```

### Worker Development

```bash
cd packages/worker
npm run dev  # Local wrangler dev server
```

## Costs

| Component | Cost |
|-----------|------|
| Cloudflare Workers Paid | $5/month |
| Container compute | ~$0.036/hour when active |
| R2 Storage | ~$0.015/GB/month |
| VPS | Your existing cost |

**Typical usage** (few hours/day): **$10-20/month total**

## Status

- [x] SSH relay server
- [x] Cloudflare Worker + Durable Object
- [x] PTY bridge container image
- [x] WebSocket protocol (relay↔worker↔container)
- [x] VPS setup scripts
- [x] Container spawning via CF Containers
- [x] WebSocket streaming (low-latency I/O)
- [ ] GitHub Actions CI/CD
- [ ] Multi-user support

## Troubleshooting

### "Connection refused" on SSH
- Check VPS firewall allows port 22
- Verify ssh-relay is running: `systemctl status ssh-relay`

### "Failed to connect to backend"
- Check Worker is deployed: `curl https://YOUR_WORKER.workers.dev/health`
- Verify WORKER_URL in ssh-relay config

### Container doesn't start
- Check Cloudflare Containers is enabled in your account
- Verify container image is accessible
- Check Worker logs: `wrangler tail`

## Contributing

Contributions welcome! Please open an issue first to discuss what you'd like to change.

## License

[MIT](LICENSE)

## Acknowledgments

- [OpenCode](https://opencode.ai) — The AI coding assistant this project runs
- [gliderlabs/ssh](https://github.com/gliderlabs/ssh) — Go SSH server library
- [Cloudflare Workers](https://workers.cloudflare.com) — Edge compute platform
