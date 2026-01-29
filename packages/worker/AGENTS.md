# WORKER

Cloudflare Worker + Durable Object that manages container lifecycle for SSH sessions.

## STRUCTURE

```
worker/
└── src/
    ├── index.ts              # Worker entry, WebSocket routing
    ├── container-manager.ts  # Durable Object class
    └── protocol.ts           # Message types (shared with ssh-relay)
```

## WHERE TO LOOK

| Task | Location | Notes |
|------|----------|-------|
| Add endpoint | `src/index.ts` | `fetch()` handler |
| Modify container lifecycle | `src/container-manager.ts` | `ensureContainerConnected()` |
| Change protocol | `src/protocol.ts` | Match `packages/ssh-relay/internal/proxy/protocol.go` |
| Add DO storage | `src/container-manager.ts` | `ctx.storage.put/get` |

## CONVENTIONS

- **Session ID**: SSH key fingerprint from `X-Session-ID` header
- **DO naming**: `env.CONTAINER_MANAGER.idFromName(sessionId)`
- **WebSocket hibernation**: Uses `ctx.acceptWebSocket()` API

## ANTI-PATTERNS

| Pattern | Reason |
|---------|--------|
| Changing protocol without updating ssh-relay | Must stay in sync |
| Using `ctx.container.*` directly | API is beta - code is placeholder |
| Storing sensitive data in DO storage | Not encrypted |

## COMMANDS

```bash
npm install
npm run dev      # wrangler dev (local)
npm run deploy   # wrangler deploy

# Secrets
npx wrangler secret put AUTH_SECRET

# R2 bucket
npx wrangler r2 bucket create opencode-state
```

## NOTES

- **Container spawning**: Code in `ensureContainerConnected()` is placeholder - CF Containers API in beta
- **Idle timeout**: `IDLE_TIMEOUT_MINUTES` env var (default 30)
- **R2 mount**: Configured in `wrangler.toml` but requires container API
