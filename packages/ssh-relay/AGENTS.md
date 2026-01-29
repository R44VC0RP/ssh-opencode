# SSH-RELAY

Go SSH server that accepts connections and proxies to Cloudflare Worker via WebSocket.

## STRUCTURE

```
ssh-relay/
├── cmd/relay/main.go           # Entry point, flags, ping interval (1s)
└── internal/
    ├── auth/
    │   ├── handler.go          # SSH public key auth
    │   └── registry.go         # SQLite key storage
    ├── session/handler.go      # PTY + WebSocket proxy loop
    ├── proxy/protocol.go       # Message types (sync with worker + pty-bridge)
    └── github/parser.go        # URL parsing (user/repo variants)
```

## WHERE TO LOOK

| Task | Location | Notes |
|------|----------|-------|
| Add auth method | `internal/auth/handler.go` | `NewPublicKeyHandler()` |
| Modify session flow | `internal/session/handler.go` | `Handler()` goroutines |
| Change protocol | `internal/proxy/protocol.go` | **Must sync** with worker + pty-bridge |
| Support new repo URLs | `internal/github/parser.go` | Regex patterns |
| Change ping interval | `cmd/relay/main.go:85` | Currently 1 second |

## CONVENTIONS

- **CGO required**: SQLite needs `CGO_ENABLED=1`
- **Module name**: `ssh-relay` (not path-based)
- **Fingerprint context**: Auth stores `fingerprint` in `ssh.Context`
- **Base64 encoding**: All PTY data base64 encoded in `data` field
- **Status handling**: Has `MsgStatus` type for status messages

## ANTI-PATTERNS

| Pattern | Reason |
|---------|--------|
| Changing protocol without updating worker/pty-bridge | Must stay in sync across 3 files |
| Storing secrets in registry.go | Use env vars only |
| CGO_ENABLED=0 | SQLite won't work |
| Ping interval >5s | UI becomes unresponsive |

## COMMANDS

```bash
# Build
CGO_ENABLED=1 go build -o ssh-relay ./cmd/relay

# Run
./ssh-relay --worker-url wss://your.workers.dev/ws --listen :2222

# Docker (local)
docker-compose up ssh-relay       # Uses local-proxy
docker-compose --profile cf up ssh-relay-cf  # Uses real CF Worker

# Docker (standalone)
docker build -t ssh-relay .
docker run --network host -e WORKER_URL=wss://... ssh-relay
```

## NOTES

- **Host key generation**: Auto-generates ED25519 if missing (uses ssh-keygen)
- **Auto-register**: `--auto-register=true` (default) accepts any first-time key
- **Ping interval**: 1 second - triggers worker to poll container for output
- **Session headers**: `X-Session-ID`, `X-Cols`, `X-Rows`, `X-Repo`
