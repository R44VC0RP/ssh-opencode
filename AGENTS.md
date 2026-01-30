# SSH-OPENCODE

**Generated:** 2026-01-30
**Commit:** ecee63b
**Branch:** main

## OVERVIEW

SSH relay service spawning OpenCode TUI containers on Cloudflare edge. `ssh domain [repo]` → VPS relay → WebSocket → CF Worker DO → Container PTY bridge.

## STRUCTURE

```
ssh-opencode/
├── packages/
│   ├── ssh-relay/      # Go SSH server (VPS) - see AGENTS.md
│   ├── worker/         # CF Worker + DO - see AGENTS.md
│   ├── container/      # Docker + PTY bridge - see AGENTS.md
│   └── local-proxy/    # Local dev only: simulates CF Worker
├── scripts/            # setup.sh, deploy-vps.sh, setup-vps.sh
├── docker-compose.yml  # Local dev (port 2222)
└── .env.example
```

## WHERE TO LOOK

| Task | Location | Notes |
|------|----------|-------|
| SSH auth | `packages/ssh-relay/internal/auth/` | Public key auto-registration |
| WebSocket protocol | `packages/*/protocol.{go,ts}` | **Must sync** - 3 locations |
| Container lifecycle | `packages/worker/src/container-manager.ts` | WebSocket streaming to container |
| PTY bridge | `packages/container/pty-bridge/main.go` | HTTP + WS server |
| GitHub URL parsing | `packages/ssh-relay/internal/github/parser.go` | `user/repo` variants |
| VPS deployment | `scripts/deploy-vps.sh` | Docker on VPS |
| VPS provisioning | `scripts/setup-vps.sh` | Systemd, firewall, host keys |

## CONVENTIONS

- **Three Go modules**: `ssh-relay`, `pty-bridge`, `local-proxy` - independent
- **Protocol sync**: Changes require updates in 3 files
- **No tests/CI**: Manual deployment via scripts
- **Port 2222**: docker-compose avoids host SSH conflict
- **wrangler.jsonc**: JSONC format (not .toml)

## ANTI-PATTERNS

| Pattern | Reason |
|---------|--------|
| Commit `.env`, host keys, `*.db` | Secrets/state |
| Container as non-root | Port 22 needs root |
| Protocol change in one package | Must update all 3 |
| `wrangler.toml` | Uses `.jsonc` |
| `lite` instance type | OOM - use `basic` (1GB) |

## UNIQUE STYLES

- **Protocol**: JSON + newline-delimited, base64 binary data
- **Session ID**: SSH fingerprint via `X-Session-ID` header
- **Auto-registration**: First SSH key auto-registered
- **Worker↔Container**: WebSocket streaming (push-based)
- **Ping interval**: 100ms for keepalive

## COMMANDS

```bash
# Setup
./scripts/setup.sh

# Local dev
docker-compose up                           # Full stack
docker-compose --profile cf up ssh-relay-cf # Real CF Worker

# Worker
cd packages/worker && npm run deploy

# VPS
./scripts/setup-vps.sh user@host           # First time
WORKER_URL=wss://... ./scripts/deploy-vps.sh user@host
```

## NOTES

- **CF Containers**: `wrangler.jsonc` points to `../container`
- **R2 persistence**: `/data/opencode`, `/data/dev` if available
- **PTY endpoints**: `/init`, `/read`, `/write`, `/writeread`, `/resize`, `/status`, `/ws`
- **Instance type**: `basic` (1GB) - `lite` causes OOM
- **Protocol issue**: `status` type in Go but not TypeScript
