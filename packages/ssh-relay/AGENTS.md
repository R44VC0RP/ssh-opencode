# SSH-RELAY

Go SSH server that accepts connections and proxies to Cloudflare Worker via WebSocket.

## STRUCTURE

```
ssh-relay/
├── cmd/relay/main.go           # Entry point, flag parsing
└── internal/
    ├── auth/
    │   ├── handler.go          # SSH public key auth
    │   └── registry.go         # SQLite key storage
    ├── session/handler.go      # PTY + WebSocket proxy
    ├── proxy/protocol.go       # Message types (shared with worker)
    └── github/parser.go        # URL parsing (user/repo variants)
```

## WHERE TO LOOK

| Task | Location | Notes |
|------|----------|-------|
| Add auth method | `internal/auth/handler.go` | `NewPublicKeyHandler()` |
| Modify session flow | `internal/session/handler.go` | `Handler()` func |
| Change protocol | `internal/proxy/protocol.go` | Match `packages/worker/src/protocol.ts` |
| Support new repo URLs | `internal/github/parser.go` | Regex patterns |

## CONVENTIONS

- **CGO required**: SQLite needs CGO_ENABLED=1
- **Module name**: `ssh-relay` (not path-based)
- **Fingerprint context**: Auth stores `fingerprint` in `ssh.Context`

## ANTI-PATTERNS

| Pattern | Reason |
|---------|--------|
| Changing protocol without updating worker | Must stay in sync |
| Storing secrets in registry.go | Use env vars only |

## COMMANDS

```bash
go build -o ssh-relay ./cmd/relay
./ssh-relay --worker-url wss://... --listen :2222

# Docker
docker build -t ssh-relay .
docker run --network host -e WORKER_URL=wss://... ssh-relay
```

## NOTES

- **Host key generation**: Auto-generates ED25519 if missing (uses ssh-keygen)
- **Auto-register**: `--auto-register=true` (default) accepts any first-time key
