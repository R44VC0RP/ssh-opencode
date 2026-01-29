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
│   └── container/      # Docker + PTY bridge
├── scripts/            # setup.sh, deploy-vps.sh
├── docker-compose.yml  # Local dev (port 2222)
└── .env.example        # Config template
```

## WHERE TO LOOK

| Task | Location | Notes |
|------|----------|-------|
| SSH auth flow | `packages/ssh-relay/internal/auth/` | Public key auto-registration |
| WebSocket protocol | `packages/*/protocol.{go,ts}` | Shared message types |
| Container lifecycle | `packages/worker/src/container-manager.ts` | DO manages spawn/sleep |
| PTY bridge | `packages/container/pty-bridge/main.go` | TCP→PTY→OpenCode |
| GitHub URL parsing | `packages/ssh-relay/internal/github/parser.go` | `user/repo` variants |
| Deployment | `scripts/deploy-vps.sh` | Docker on VPS via SSH |

## CONVENTIONS

- **Two separate Go modules**: `ssh-relay` and `pty-bridge` intentionally not shared (independent deployment)
- **No tests**: Not yet implemented
- **No CI/CD**: Manual deployment via scripts
- **Port 2222**: docker-compose uses 2222 to avoid host SSH conflict

## ANTI-PATTERNS

| Pattern | Reason |
|---------|--------|
| Committing `.env`, host keys, `*.db` | Secrets/state - use .env.example |
| Running container as non-root | Port 22 requires root (commented code exists) |
| Assuming CF Containers API stable | Beta - container spawning code is placeholder |

## UNIQUE STYLES

- **Protocol messages**: JSON over newline-delimited streams, base64 for binary data
- **Session ID**: SSH key fingerprint (SHA256)
- **Auto-registration**: First connecting SSH key is registered (single-user mode)

## COMMANDS

```bash
# Setup all packages
./scripts/setup.sh

# Local dev
docker-compose up ssh-relay     # Port 2222
docker-compose --profile local-test up  # Include container

# Worker
cd packages/worker
npm run dev      # wrangler dev
npm run deploy   # wrangler deploy

# Deploy SSH relay to VPS
WORKER_URL=wss://your.workers.dev/ws ./scripts/deploy-vps.sh user@vps-host
```

## NOTES

- **CF Containers beta**: `wrangler.toml` has containers config commented out - uncomment when API stable
- **R2 persistence**: Container mounts `/data/opencode` and `/data/dev` if available
- **Entrypoint git config**: Sets generic `opencode@localhost` - may override user prefs
