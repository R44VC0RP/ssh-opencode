# SSH-RELAY

Go SSH server proxying to Cloudflare Worker via WebSocket.

## STRUCTURE

```
ssh-relay/
├── cmd/relay/main.go           # Entry, flags, ping interval (100ms)
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
| Change ping interval | `cmd/relay/main.go:85` | Currently 100ms |

## CONVENTIONS

- **CGO required**: SQLite needs `CGO_ENABLED=1`
- **Module name**: `ssh-relay` (not path-based)
- **Fingerprint context**: Auth stores `fingerprint` in `ssh.Context`
- **Base64 encoding**: All PTY data base64 encoded
- **Status handling**: Has `MsgStatus` type (worker doesn't define it)

## ANTI-PATTERNS

| Pattern | Reason |
|---------|--------|
| Protocol change without sync | Must update all 3 files |
| Secrets in registry.go | Use env vars |
| CGO_ENABLED=0 | SQLite fails |
| Ping interval >5s | UI unresponsive |

## COMMANDS

```bash
# Build
CGO_ENABLED=1 go build -o ssh-relay ./cmd/relay

# Run
./ssh-relay --worker-url wss://your.workers.dev/ws --listen :2222

# Docker
docker build -t ssh-relay .
docker run --network host -e WORKER_URL=wss://... ssh-relay
```

## NOTES

- **Host key**: Auto-generates ED25519 if missing
- **Auto-register**: `--auto-register=true` accepts first-time keys
- **Ping interval**: 100ms - keepalive for WebSocket streaming
- **Headers**: `X-Session-ID`, `X-Cols`, `X-Rows`, `X-Repo`
