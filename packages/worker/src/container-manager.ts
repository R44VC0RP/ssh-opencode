import { parseMessage, serializeMessage, type Message } from './protocol';

export interface ContainerManagerEnv {
  OPENCODE_STATE: R2Bucket;
  IDLE_TIMEOUT_MINUTES: string;
  CONTAINER_INSTANCE_TYPE: string;
}

interface SessionState {
  lastActive: number;
  cols: number;
  rows: number;
  repo?: string;
  containerStarted?: number;
  reconnectCount: number;
}

/**
 * ContainerManager Durable Object
 * 
 * Manages a single container instance for a user session.
 * The session ID (Durable Object name) is the SSH key fingerprint.
 * 
 * NOTE: This implementation is prepared for Cloudflare Containers (beta).
 * Container spawning code is commented out until the API is stable.
 */
export class ContainerManager implements DurableObject {
  private state: SessionState | null = null;
  private wsConnections: Set<WebSocket> = new Set();
  private ctx: DurableObjectState;
  private env: ContainerManagerEnv;
  
  // Container connection (will be used when CF Containers API is available)
  // private containerSocket: Socket | null = null;
  // private containerWriter: WritableStreamDefaultWriter<Uint8Array> | null = null;

  constructor(ctx: DurableObjectState, env: ContainerManagerEnv) {
    this.ctx = ctx;
    this.env = env;
  }

  async fetch(request: Request): Promise<Response> {
    const url = new URL(request.url);

    // Health check
    if (url.pathname === '/health') {
      return new Response('OK');
    }

    // Status endpoint
    if (url.pathname === '/status') {
      const state = await this.getState();
      return Response.json({
        active: this.wsConnections.size > 0,
        connections: this.wsConnections.size,
        state,
      });
    }

    // WebSocket upgrade
    const upgradeHeader = request.headers.get('Upgrade');
    if (upgradeHeader !== 'websocket') {
      return new Response('Expected WebSocket upgrade', { status: 426 });
    }

    // Extract session parameters from headers
    const cols = parseInt(request.headers.get('X-Cols') || '80');
    const rows = parseInt(request.headers.get('X-Rows') || '24');
    const repo = request.headers.get('X-Repo') || undefined;

    // Create WebSocket pair
    const pair = new WebSocketPair();
    const [client, server] = Object.values(pair);

    // Accept the WebSocket connection via Durable Object hibernation API
    this.ctx.acceptWebSocket(server);

    // Initialize or update state
    await this.initializeState(cols, rows, repo);

    // Ensure container is running and connected
    await this.ensureContainerConnected();

    return new Response(null, {
      status: 101,
      webSocket: client,
    });
  }

  private async getState(): Promise<SessionState> {
    if (!this.state) {
      const stored = await this.ctx.storage.get<SessionState>('state');
      this.state = stored || {
        lastActive: Date.now(),
        cols: 80,
        rows: 24,
        reconnectCount: 0,
      };
    }
    return this.state;
  }

  private async saveState(): Promise<void> {
    if (this.state) {
      await this.ctx.storage.put('state', this.state);
    }
  }

  private async initializeState(cols: number, rows: number, repo?: string): Promise<void> {
    const state = await this.getState();
    state.lastActive = Date.now();
    state.cols = cols;
    state.rows = rows;
    if (repo) {
      state.repo = repo;
    }
    state.reconnectCount++;
    await this.saveState();
  }

  private async ensureContainerConnected(): Promise<void> {
    // NOTE: Cloudflare Containers API is in beta and may change.
    // The actual container spawning code will need to be implemented
    // once the Containers API is stable and available.
    //
    // For now, this is a placeholder that shows the intended flow:
    //
    // 1. Check if container exists via this.ctx.container
    // 2. If not, spawn container with appropriate config
    // 3. Get TCP port via getTcpPort(8080)
    // 4. Connect and store socket
    //
    // Example pseudocode:
    //
    // if (!this.ctx.container?.running) {
    //   await this.ctx.container.start({
    //     image: 'ghcr.io/your-org/opencode-container:latest',
    //     env: { PTY_BRIDGE_PORT: '8080' },
    //   });
    // }
    //
    // const port = this.ctx.container.getTcpPort(8080);
    // this.containerSocket = await port.connect();
    // this.containerWriter = this.containerSocket.writable.getWriter();
    //
    // Send init message:
    // const state = await this.getState();
    // const initMsg = { type: 'init', cols: state.cols, rows: state.rows, repo: state.repo };
    // await this.sendToContainer(initMsg);

    console.log('Container connection placeholder - DO ID:', this.ctx.id.toString());
  }

  private async sendToContainer(msg: Message): Promise<void> {
    // TODO: Implement when container socket is available
    // const data = serializeMessage(msg) + '\n';
    // const encoder = new TextEncoder();
    // await this.containerWriter?.write(encoder.encode(data));
    console.log('Would send to container:', msg.type);
  }

  private broadcastToWebSockets(msg: Message): void {
    const data = serializeMessage(msg);
    for (const ws of this.wsConnections) {
      try {
        ws.send(data);
      } catch {
        this.wsConnections.delete(ws);
      }
    }
  }

  // WebSocket event handlers (called by Durable Object Hibernation API)

  async webSocketMessage(ws: WebSocket, message: string | ArrayBuffer): Promise<void> {
    const state = await this.getState();
    state.lastActive = Date.now();
    await this.saveState();

    const msgStr = typeof message === 'string' ? message : new TextDecoder().decode(message);
    const msg = parseMessage(msgStr);

    if (!msg) {
      console.error('Failed to parse message:', msgStr.substring(0, 100));
      return;
    }

    switch (msg.type) {
      case 'data':
      case 'resize':
        // Forward to container
        await this.sendToContainer(msg);

        // Update state for resize
        if (msg.type === 'resize') {
          state.cols = msg.cols;
          state.rows = msg.rows;
          await this.saveState();
        }
        break;

      case 'ping':
        ws.send(serializeMessage({ type: 'pong', timestamp: msg.timestamp }));
        break;

      case 'exit':
        // Client is disconnecting gracefully
        break;

      default:
        console.log('Unknown message type:', (msg as Message).type);
    }
  }

  async webSocketOpen(ws: WebSocket): Promise<void> {
    this.wsConnections.add(ws);
    console.log('WebSocket opened, total connections:', this.wsConnections.size);
  }

  async webSocketClose(ws: WebSocket, code: number, reason: string): Promise<void> {
    this.wsConnections.delete(ws);
    console.log('WebSocket closed:', code, reason, 'remaining:', this.wsConnections.size);

    // Don't kill container on disconnect - let it sleep after idle timeout
    await this.saveState();
  }

  async webSocketError(ws: WebSocket, error: unknown): Promise<void> {
    console.error('WebSocket error:', error);
    this.wsConnections.delete(ws);
  }

  // Scheduled cleanup (called by Durable Object alarm)
  async alarm(): Promise<void> {
    const state = await this.getState();
    const idleTimeout = parseInt(this.env.IDLE_TIMEOUT_MINUTES || '30') * 60 * 1000;
    const now = Date.now();

    if (now - state.lastActive > idleTimeout && this.wsConnections.size === 0) {
      console.log('Session idle, cleaning up');
      // Container will auto-sleep via its sleepAfter setting
    }
  }
}
