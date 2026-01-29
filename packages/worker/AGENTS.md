# WORKER

Cloudflare Worker + Durable Object that manages container lifecycle for SSH sessions.

## STRUCTURE

```
worker/
├── src/
│   ├── index.ts              # Worker entry, WebSocket routing
│   ├── container-manager.ts  # Container-enabled DO (extends @cloudflare/containers)
│   └── protocol.ts           # Message types (sync with ssh-relay + pty-bridge)
├── wrangler.jsonc            # CF config (NOT .toml)
└── package.json
```

## WHERE TO LOOK

| Task | Location | Notes |
|------|----------|-------|
| Add endpoint | `src/index.ts` | `fetch()` handler routes |
| Modify container lifecycle | `src/container-manager.ts` | Extends `Container` class |
| Change protocol | `src/protocol.ts` | **Must sync** with `ssh-relay` and `pty-bridge` |
| Add DO storage | `src/container-manager.ts` | `ctx.storage.put/get` |
| Configure container | `wrangler.jsonc` | Instance type, R2 mounts |

## CONVENTIONS

- **Session ID**: SSH key fingerprint from `X-Session-ID` header
- **DO naming**: `env.CONTAINER_MANAGER.idFromName(sessionId)`
- **WebSocket hibernation**: Uses `ctx.acceptWebSocket()` API
- **Ping-triggered reads**: Each client ping triggers `readAndBroadcast()`
- **Type guards**: Use `getDataLength()` helper for `Message` union types

## CONTAINER API

```typescript
export class ContainerManager extends Container {
  defaultPort = 8080;           // PTY bridge port
  sleepAfter = '30m';           // Idle timeout
  enableInternet = true;        // For git clone, npm install
  
  // Lifecycle hooks
  onStart(): void { }
  onStop(): void { }
  onError(error: unknown): void { }
}
```

Key methods:
- `startAndWaitForPorts({ ports: [8080] })` - Start and wait for ready
- `containerFetch('http://container:8080/endpoint')` - HTTP to container
- `getState()` - Status (running, healthy, stopped)
- `renewActivityTimeout()` - Reset sleep timer

## ANTI-PATTERNS

| Pattern | Reason |
|---------|--------|
| Changing protocol without updating ssh-relay/pty-bridge | Must stay in sync across 3 files |
| Storing sensitive data in DO storage | Not encrypted |
| Forgetting `renewActivityTimeout()` | Container sleeps unexpectedly |
| Using `msg.data` without type guard | `data` only exists on `DataMessage` |
| Using `lite` instance type | OOM - OpenCode needs ~600MB, use `basic` |

## COMMANDS

```bash
npm install
npm run dev      # wrangler dev (local)
npm run deploy   # wrangler deploy

# Secrets
npx wrangler secret put AUTH_SECRET

# R2 bucket
npx wrangler r2 bucket create opencode-state

# Logs
npx wrangler tail --format=pretty
```

## NOTES

- **Container image**: Built from `packages/container/Dockerfile`
- **Instance type**: Must be `basic` (1GB) not `lite` (256MB)
- **PTY communication**: HTTP polling via `/read`, POST to `/write`
- **Verbose logging**: `[Tag]` prefixed logs throughout for debugging
- **Status messages**: Sends `{ type: 'status' }` but type not in protocol.ts
