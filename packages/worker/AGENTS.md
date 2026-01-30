# WORKER

Cloudflare Worker + Durable Object managing container lifecycle via WebSocket streaming.

## STRUCTURE

```
worker/
├── src/
│   ├── index.ts              # Worker entry, WebSocket routing
│   ├── container-manager.ts  # Container DO with WebSocket to PTY bridge
│   └── protocol.ts           # Message types (sync with ssh-relay + pty-bridge)
├── wrangler.jsonc            # CF config (NOT .toml)
└── package.json
```

## WHERE TO LOOK

| Task | Location | Notes |
|------|----------|-------|
| Add endpoint | `src/index.ts` | `fetch()` routes |
| Container lifecycle | `src/container-manager.ts` | Extends `Container` |
| Change protocol | `src/protocol.ts` | **Must sync** with ssh-relay + pty-bridge |
| DO storage | `src/container-manager.ts` | `ctx.storage.put/get` |
| Configure container | `wrangler.jsonc` | Instance type, R2 |

## CONVENTIONS

- **Session ID**: SSH fingerprint from `X-Session-ID` header
- **DO naming**: `env.CONTAINER_MANAGER.idFromName(sessionId)`
- **WebSocket hibernation**: `ctx.acceptWebSocket()` API
- **Container WebSocket**: `connectContainerWebSocket()` for push-based I/O
- **HTTP fallback**: `writeAndReadHttp()` if WS unavailable

## CONTAINER API

```typescript
export class ContainerManager extends Container {
  defaultPort = 8080;
  sleepAfter = '30m';
  enableInternet = true;
  
  // WebSocket to container
  private containerWs: WebSocket | null;
  private containerWsReady = false;
}
```

Key methods:
- `connectContainerWebSocket()` - WS to container for streaming
- `containerFetch()` - HTTP fallback
- `broadcastToWebSockets()` - Send to all clients

## ANTI-PATTERNS

| Pattern | Reason |
|---------|--------|
| Protocol change without sync | Must update all 3 files |
| Sensitive data in DO storage | Not encrypted |
| Forget `renewActivityTimeout()` | Container sleeps |
| `msg.data` without type guard | Only on `DataMessage` |
| `lite` instance type | OOM - use `basic` |

## COMMANDS

```bash
npm install
npm run dev      # wrangler dev
npm run deploy   # wrangler deploy

npx wrangler secret put AUTH_SECRET
npx wrangler r2 bucket create opencode-state
npx wrangler tail --format=pretty
```

## NOTES

- **Container image**: Built from `../container/Dockerfile`
- **Instance type**: `basic` (1GB) required
- **PTY streaming**: WebSocket to `/ws` endpoint (push-based)
- **HTTP fallback**: `/writeread` if WS fails
- **Status messages**: Sends `{type:'status'}` but not in protocol.ts
