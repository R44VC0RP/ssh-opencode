# CONTAINER

Docker container running OpenCode TUI with PTY bridge HTTP/WebSocket server.

## STRUCTURE

```
container/
├── Dockerfile                  # Multi-stage: golang → ghcr.io/anomalyco/opencode
├── pty-bridge/
│   ├── main.go                 # HTTP + WebSocket server (609 lines)
│   ├── go.mod                  # Module: pty-bridge
│   └── go.sum
└── scripts/
    └── entrypoint.sh           # Setup dirs, git config, exec pty-bridge
```

## WHERE TO LOOK

| Task | Location | Notes |
|------|----------|-------|
| Add PTY endpoint | `pty-bridge/main.go` | HTTP handlers ~line 350+ |
| Change WebSocket handling | `pty-bridge/main.go:handleWebSocket()` | ~line 142 |
| Modify startup | `scripts/entrypoint.sh` | R2 mounts, git config |
| Change base image | `Dockerfile:1` | `ghcr.io/anomalyco/opencode:latest` |
| Bump cache version | `Dockerfile` | `LABEL version="..."` |

## PTY BRIDGE API

| Endpoint | Method | Purpose |
|----------|--------|---------|
| `/ping` | GET | Health check |
| `/init` | POST | Initialize PTY: `{type:"init", cols, rows, repo?}` |
| `/write` | POST | Write to PTY: `{type:"data", data:"base64..."}` or resize |
| `/read` | GET | Read buffered output (newline-delimited JSON) |
| `/resize` | POST | Resize terminal: `{type:"resize", cols, rows}` |
| `/status` | GET | Session status JSON |
| `/ws` | WebSocket | Real-time streaming (future use, HTTP polling is primary) |

## CONVENTIONS

- **Output buffer**: Last 1MB only, prevents unbounded growth
- **Newline-delimited JSON**: `/read` returns multiple messages separated by `\n`
- **Protocol inline**: Message types defined in main.go:23-46 (not shared module)
- **CGO_ENABLED=0**: Static binary, no SQLite needed here
- **Root required**: Port 22 and directory creation need root

## ANTI-PATTERNS

| Pattern | Reason |
|---------|--------|
| Running as non-root | Port 22 + dir creation need root |
| Changing protocol without syncing | Must match ssh-relay + worker |
| Large output buffers | >1MB causes memory issues |
| Missing version label | Docker cache won't bust on rebuild |

## COMMANDS

```bash
# Build (from container/ directory)
docker build -t opencode-container .

# Run standalone
docker run -p 8080:8080 -e PTY_BRIDGE_PORT=8080 opencode-container

# Via docker-compose (from repo root)
docker-compose up container

# Force rebuild
docker-compose build --no-cache container
```

## NOTES

- **Base image**: Requires `ghcr.io/anomalyco/opencode:latest` to exist
- **Version label**: Change `LABEL version="..."` to bust Docker cache
- **Git config**: Entrypoint sets `opencode@localhost` - may override user prefs
- **R2 mounts**: Symlinks `/data/opencode` → `~/.local/share/opencode` if exists
- **Memory**: Needs ~600MB, CF `basic` instance (1GB) required
