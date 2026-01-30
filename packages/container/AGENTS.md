# CONTAINER

Docker container running OpenCode TUI with PTY bridge HTTP/WebSocket server.

## STRUCTURE

```
container/
├── Dockerfile                  # Multi-stage: golang → ghcr.io/anomalyco/opencode
├── pty-bridge/
│   ├── main.go                 # HTTP + WebSocket server (~700 lines)
│   ├── go.mod                  # Module: pty-bridge
│   └── go.sum
└── scripts/
    └── entrypoint.sh           # Setup dirs, git config, exec pty-bridge
```

## WHERE TO LOOK

| Task | Location | Notes |
|------|----------|-------|
| Add PTY endpoint | `pty-bridge/main.go` | HTTP handlers ~line 420+ |
| WebSocket handling | `pty-bridge/main.go:handleWebSocket()` | ~line 152 |
| Combined write+read | `pty-bridge/main.go:handleWriteRead()` | ~line 519 |
| Modify startup | `scripts/entrypoint.sh` | R2 mounts, git config |
| Change base image | `Dockerfile:1` | `ghcr.io/anomalyco/opencode:latest` |
| Bump cache | `Dockerfile` | `LABEL version="..."` |

## PTY BRIDGE API

| Endpoint | Method | Purpose |
|----------|--------|---------|
| `/ping` | GET | Health check |
| `/init` | POST | Initialize PTY: `{type:"init", cols, rows, repo?}` |
| `/write` | POST | Write to PTY: `{type:"data", data:"base64..."}` |
| `/read` | GET | Read buffered output (newline-delimited JSON) |
| `/writeread` | POST | Combined write+read (lower latency) |
| `/resize` | POST | Resize: `{type:"resize", cols, rows}` |
| `/status` | GET | Session status |
| `/ws` | WebSocket | Real-time streaming (primary mode) |

## CONVENTIONS

- **Output buffer**: 1MB max, prevents unbounded growth
- **Newline-delimited JSON**: `/read` returns `\n`-separated messages
- **Protocol inline**: Types in main.go:23-46 (not shared module)
- **CGO_ENABLED=0**: Static binary
- **Root required**: Port 22 + directory creation

## ANTI-PATTERNS

| Pattern | Reason |
|---------|--------|
| Non-root | Port 22 + dirs need root |
| Protocol change without sync | Must match ssh-relay + worker |
| Output buffer >1MB | Memory issues |
| Missing version label | Cache won't bust |

## COMMANDS

```bash
# Build
docker build -t opencode-container .

# Run standalone
docker run -p 8080:8080 -e PTY_BRIDGE_PORT=8080 opencode-container

# Via docker-compose
docker-compose up container

# Force rebuild
docker-compose build --no-cache container
```

## NOTES

- **Base image**: Requires `ghcr.io/anomalyco/opencode:latest`
- **Version label**: Change to bust Docker cache
- **Git config**: Entrypoint sets `opencode@localhost`
- **R2 mounts**: Symlinks `/data/opencode` → `~/.local/share/opencode`
- **Memory**: ~600MB needed, CF `basic` (1GB) required
