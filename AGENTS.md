# SSH-OPENCODE

**Generated:** 2026-01-29

## OVERVIEW

SSH relay service that spawns OpenCode TUI containers on Cloudflare edge. User runs `ssh domain [repo]` → VPS relays via WebSocket → CF Worker → Durable Object → Container with PTY bridge.

## STRUCTURE

```
ssh-opencode/
├── packages/
│   ├── ssh-relay/      # Go SSH server (VPS) - see packages/ssh-relay/AGENTS.md
│   ├── worker/         # CF Worker + DO - see packages/worker/AGENTS.md
│   ├── container/      # Docker + PTY bridge - see packages/container/AGENTS.md
│   └── local-proxy/    # Local dev only: simulates CF Worker (HTTP polling bridge)
├── scripts/            # setup.sh, deploy-vps.sh, setup-vps.sh
├── docker-compose.yml  # Local dev (port 2222)
└── .env.example        # Config template
```

## WHERE TO LOOK

| Task | Location | Notes |
|------|----------|-------|
| SSH auth flow | `packages/ssh-relay/internal/auth/` | Public key auto-registration |
| WebSocket protocol | `packages/*/protocol.{go,ts}` | **Must sync manually** - 3 locations |
| Container lifecycle | `packages/worker/src/container-manager.ts` | Extends `@cloudflare/containers` |
| PTY bridge | `packages/container/pty-bridge/main.go` | HTTP + WebSocket server |
| GitHub URL parsing | `packages/ssh-relay/internal/github/parser.go` | `user/repo` variants |
| VPS deployment | `scripts/deploy-vps.sh` | Docker on VPS via SSH heredoc |
| VPS provisioning | `scripts/setup-vps.sh` | Systemd service, firewall, host keys |

## CONVENTIONS

- **Three separate Go modules**: `ssh-relay`, `pty-bridge`, `local-proxy` - intentionally independent
- **Protocol sync**: Changes to protocol require updates in 3 files (see WHERE TO LOOK)
- **No tests**: Not yet implemented
- **No CI/CD**: Manual deployment via scripts
- **Port 2222**: docker-compose uses 2222 to avoid host SSH conflict
- **wrangler.jsonc**: Worker uses JSONC format (not .toml)

## ANTI-PATTERNS

| Pattern | Reason |
|---------|--------|
| Committing `.env`, host keys, `*.db` | Secrets/state - use .env.example |
| Running container as non-root | Port 22 requires root (commented code exists) |
| Changing protocol in one package only | Must update all 3: ssh-relay, worker, pty-bridge |
| Using `wrangler.toml` | Project uses `wrangler.jsonc` |

## UNIQUE STYLES

- **Protocol messages**: JSON over newline-delimited streams, base64 for binary data
- **Session ID**: SSH key fingerprint (SHA256) via `X-Session-ID` header
- **Auto-registration**: First connecting SSH key is registered (single-user mode)
- **Container API**: DO extends `Container` from `@cloudflare/containers`
- **Ping-triggered polling**: Client pings (1s) trigger `/read` from container

## COMMANDS

```bash
# Setup all packages
./scripts/setup.sh

# Local dev (full stack)
docker-compose up                           # ssh-relay + local-proxy + container
docker-compose --profile cf up ssh-relay-cf # Connect to real CF Worker

# Worker
cd packages/worker
npm run dev      # wrangler dev
npm run deploy   # wrangler deploy

# Deploy SSH relay to VPS
./scripts/setup-vps.sh user@vps-host                              # First time: provision
WORKER_URL=wss://your.workers.dev/ws ./scripts/deploy-vps.sh user@vps-host  # Deploy
```

## NOTES

- **CF Containers**: `wrangler.jsonc` has containers configured pointing to `../container`
- **R2 persistence**: Container mounts `/data/opencode` and `/data/dev` if available
- **Entrypoint git config**: Sets generic `opencode@localhost` - may override user prefs
- **PTY bridge endpoints**: `/init`, `/read`, `/write`, `/resize`, `/status`, `/ws`
- **Container instance type**: `basic` (1GB RAM) - `lite` (256MB) causes OOM
- **Protocol inconsistency**: Go has `status` message type, TypeScript doesn't define it (but sends it)
