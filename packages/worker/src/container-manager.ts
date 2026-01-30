import { Container } from '@cloudflare/containers';
import { parseMessage, serializeMessage, type Message, type InitMessage, type DataMessage } from './protocol';

function getDataLength(msg: Message): number {
  return msg.type === 'data' ? (msg as DataMessage).data.length : 0;
}

/**
 * ContainerManager - Cloudflare Container-enabled Durable Object
 * 
 * Manages a single OpenCode container instance for a user session.
 * Uses WebSocket streaming to container for low-latency bidirectional I/O.
 */
export class ContainerManager extends Container {
  defaultPort = 8080;
  sleepAfter = '30m';
  
  envVars: Record<string, string> = {
    PTY_BRIDGE_PORT: '8080',
    TERM: 'xterm-256color',
    COLORTERM: 'truecolor',
  };

  enableInternet = true;

  private sessionState: {
    cols: number;
    rows: number;
    repo?: string;
    lastActive: number;
  } | null = null;

  // WebSocket connection to the container's PTY bridge
  private containerWs: WebSocket | null = null;
  private containerWsReady = false;

  override onStart(): void {
    console.log('[Container] Started for session:', this.ctx.id.toString());
  }

  override onStop(): void {
    console.log('[Container] Stopped for session:', this.ctx.id.toString());
    this.closeContainerWs();
  }

  override onError(error: unknown): void {
    console.error('[Container] Error:', error);
    
    const errorMsg = serializeMessage({
      type: 'error',
      message: `Container error: ${error}`,
    });
    for (const ws of this.ctx.getWebSockets()) {
      try { ws.send(errorMsg); } catch {}
    }
  }

  private closeContainerWs(): void {
    if (this.containerWs) {
      try {
        this.containerWs.close();
      } catch {}
      this.containerWs = null;
      this.containerWsReady = false;
    }
  }

  override async fetch(request: Request): Promise<Response> {
    const url = new URL(request.url);

    if (url.pathname === '/health') {
      return new Response('OK');
    }

    if (url.pathname === '/status') {
      const state = await this.getState();
      return Response.json({
        active: this.ctx.getWebSockets().length > 0,
        connections: this.ctx.getWebSockets().length,
        containerState: state,
        sessionState: this.sessionState,
        containerWsReady: this.containerWsReady,
      });
    }

    const upgradeHeader = request.headers.get('Upgrade');
    if (upgradeHeader !== 'websocket') {
      return new Response('Expected WebSocket upgrade', { status: 426 });
    }

    const cols = parseInt(request.headers.get('X-Cols') || '80');
    const rows = parseInt(request.headers.get('X-Rows') || '24');
    const repo = request.headers.get('X-Repo') || undefined;

    this.sessionState = { cols, rows, repo, lastActive: Date.now() };
    await this.ctx.storage.put('sessionState', this.sessionState);

    const pair = new WebSocketPair();
    const [client, server] = Object.values(pair);

    this.ctx.acceptWebSocket(server);
    server.serializeAttachment({ cols, rows, repo });

    console.log('[WS] Client connected, total:', this.ctx.getWebSockets().length);
    server.send(JSON.stringify({ type: 'status', message: 'Connecting...' }));

    this.ctx.waitUntil((async () => {
      try {
        const state = await this.getState();
        console.log('[Init] Container state:', state.status);
        
        if (state.status !== 'healthy' && state.status !== 'running') {
          server.send(JSON.stringify({ type: 'status', message: 'Starting container...' }));
        }
        
        await this.ensureContainerReady();
        server.send(JSON.stringify({ type: 'status', message: 'Initializing PTY...' }));
        
        // Initialize PTY via HTTP (one-time setup)
        await this.initializePTY(cols, rows, repo);
        
        // Establish WebSocket streaming connection to container
        server.send(JSON.stringify({ type: 'status', message: 'Connecting stream...' }));
        await this.connectContainerWebSocket(cols, rows, repo);
        
        server.send(JSON.stringify({ type: 'status', message: 'Ready!' }));
      } catch (err) {
        console.error('[Init] Failed:', err);
        try {
          server.send(JSON.stringify({ type: 'error', message: `Container startup failed: ${err}` }));
          server.close(1011, 'Container startup failed');
        } catch {}
      }
    })());

    return new Response(null, { status: 101, webSocket: client });
  }

  private async ensureContainerReady(): Promise<boolean> {
    const state = await this.getState();
    console.log('[Container] ensureReady, state:', state.status);
    
    if (state.status === 'healthy' || state.status === 'running') {
      try {
        const pingResp = await this.containerFetch('http://container:8080/ping', { 
          method: 'GET',
          signal: AbortSignal.timeout(3000),
        });
        if (pingResp.ok) {
          console.log('[Container] Ping OK');
          return false;
        }
        console.log('[Container] Ping failed:', pingResp.status);
      } catch (err) {
        console.log('[Container] Ping error:', err);
      }
      console.log('[Container] Unresponsive, restarting...');
    }
    
    console.log('[Container] Starting...');
    
    await this.startAndWaitForPorts({
      startOptions: {
        envVars: {
          ...this.envVars,
          SESSION_ID: this.ctx.id.toString(),
          ...(this.sessionState?.repo && { GITHUB_REPO: this.sessionState.repo }),
        },
      },
      ports: [8080],
    });
    
    console.log('[Container] Started, port 8080 ready');
    
    const cols = this.sessionState?.cols || 80;
    const rows = this.sessionState?.rows || 24;
    await this.initializePTY(cols, rows, this.sessionState?.repo);
    
    return true;
  }

  private async initializePTY(cols: number, rows: number, repo?: string): Promise<void> {
    const initMsg: InitMessage = { type: 'init', cols, rows, repo };
    console.log('[PTY] Initializing with cols:', cols, 'rows:', rows);

    try {
      const response = await this.containerFetch('http://container:8080/init', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify(initMsg),
      });

      if (!response.ok) {
        const text = await response.text();
        console.error('[PTY] Init failed:', response.status, text);
      } else {
        const result = await response.json() as { workDir?: string };
        console.log('[PTY] Initialized, workDir:', result.workDir);
      }
    } catch (err) {
      console.error('[PTY] Init error:', err);
    }
  }

  /**
   * Establish WebSocket connection to container's PTY bridge.
   * This enables push-based output instead of polling.
   */
  private async connectContainerWebSocket(cols: number, rows: number, repo?: string): Promise<void> {
    // Close any existing connection
    this.closeContainerWs();

    try {
      console.log('[ContainerWS] Connecting to container WebSocket...');
      
      // Use containerFetch with WebSocket upgrade
      const response = await this.containerFetch('http://container:8080/ws', {
        headers: {
          'Upgrade': 'websocket',
        },
      });

      console.log('[ContainerWS] Got response, status:', response.status);
      
      // Check if we got a WebSocket back
      const ws = (response as any).webSocket;
      if (!ws) {
        console.log('[ContainerWS] No WebSocket in response (status:', response.status, '), falling back to HTTP polling');
        return;
      }

      ws.accept();
      this.containerWs = ws;

      // Send init message to container WebSocket
      const initMsg: InitMessage = { type: 'init', cols, rows, repo };
      ws.send(JSON.stringify(initMsg));

      // Handle messages from container - forward to all clients
      ws.addEventListener('message', (event: MessageEvent) => {
        const data = typeof event.data === 'string' ? event.data : new TextDecoder().decode(event.data);
        const msg = parseMessage(data);
        
        if (msg) {
          // Forward to all connected client WebSockets
          this.broadcastToWebSockets(msg);
          
          if (msg.type === 'exit') {
            console.log('[ContainerWS] PTY exited with code:', msg.code);
          }
        }
      });

      ws.addEventListener('close', () => {
        console.log('[ContainerWS] Connection closed');
        this.containerWs = null;
        this.containerWsReady = false;
      });

      ws.addEventListener('error', (err: Event) => {
        console.error('[ContainerWS] Error:', err);
      });

      this.containerWsReady = true;
      console.log('[ContainerWS] Connected and ready - using WebSocket streaming!');

    } catch (err) {
      console.error('[ContainerWS] Failed to connect:', err);
      // Fall back to HTTP polling if WebSocket fails
      this.containerWs = null;
      this.containerWsReady = false;
    }
  }

  /**
   * Send message to container - uses WebSocket if available, falls back to HTTP
   */
  private async sendToContainerWs(msg: Message): Promise<void> {
    if (this.containerWs && this.containerWsReady) {
      try {
        this.containerWs.send(serializeMessage(msg));
        return;
      } catch (err) {
        console.error('[ContainerWS] Send error, falling back to HTTP:', err);
        this.containerWsReady = false;
      }
    }

    // Fallback to HTTP
    await this.sendToContainerHttp(msg);
  }

  private async sendToContainerHttp(msg: Message): Promise<void> {
    try {
      const response = await this.containerFetch('http://container:8080/write', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify(msg),
      });
      if (!response.ok) {
        console.error('[Write] Failed:', response.status);
      }
    } catch (err) {
      console.error('[Write] Error:', err);
    }
  }

  // HTTP fallback: Combined write + read for lower latency (single HTTP round-trip)
  private async writeAndReadHttp(msg: Message): Promise<void> {
    try {
      const response = await this.containerFetch('http://container:8080/writeread', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify(msg),
        signal: AbortSignal.timeout(5000),
      });

      if (response.ok) {
        const text = await response.text();
        if (text.trim()) {
          const lines = text.split('\n').filter((l: string) => l.trim());
          for (const line of lines) {
            const parsed = parseMessage(line);
            if (parsed) {
              this.broadcastToWebSockets(parsed);
              if (parsed.type === 'exit') {
                console.log('[WriteRead] PTY exited with code:', parsed.code);
              }
            }
          }
        }
      } else {
        console.error('[WriteRead] Failed:', response.status);
      }
    } catch (err) {
      console.error('[WriteRead] Error:', err);
    }
  }

  // HTTP fallback: Read and broadcast
  private async readAndBroadcastHttp(): Promise<void> {
    try {
      const response = await this.containerFetch('http://container:8080/read', { 
        method: 'GET',
        signal: AbortSignal.timeout(2000),
      });
      
      if (response.ok) {
        const text = await response.text();
        if (text.trim()) {
          const lines = text.split('\n').filter((l: string) => l.trim());
          for (const line of lines) {
            const msg = parseMessage(line);
            if (msg) {
              this.broadcastToWebSockets(msg);
              if (msg.type === 'exit') {
                console.log('[Read] PTY exited with code:', msg.code);
              }
            }
          }
        }
      }
    } catch (err) {
      if (!(err instanceof DOMException && err.name === 'TimeoutError')) {
        console.error('[Read] Error:', err);
      }
    }
  }

  private broadcastToWebSockets(msg: Message): void {
    const data = serializeMessage(msg);
    for (const ws of this.ctx.getWebSockets()) {
      try {
        ws.send(data);
      } catch (err) {
        console.error('[Broadcast] Send error:', err);
      }
    }
  }

  webSocketOpen(ws: WebSocket): void {
    console.log('[WS] Opened (hibernation wake)');
    
    const attachment = ws.deserializeAttachment() as { cols: number; rows: number; repo?: string } | null;
    if (attachment && !this.sessionState) {
      this.sessionState = { ...attachment, lastActive: Date.now() };
    }
  }

  async webSocketMessage(ws: WebSocket, message: string | ArrayBuffer): Promise<void> {
    if (this.sessionState) {
      this.sessionState.lastActive = Date.now();
    }
    
    await this.renewActivityTimeout();

    const msgStr = typeof message === 'string' ? message : new TextDecoder().decode(message);
    const msg = parseMessage(msgStr);

    if (!msg) {
      console.error('[WS] Failed to parse:', msgStr.substring(0, 100));
      return;
    }

    switch (msg.type) {
      case 'init':
        console.log('[WS] Init message');
        await this.ensureContainerReady();
        // Try to reconnect container WebSocket if needed
        if (!this.containerWsReady) {
          await this.connectContainerWebSocket(
            this.sessionState?.cols || 80,
            this.sessionState?.rows || 24,
            this.sessionState?.repo
          );
        }
        // Do initial read via HTTP (container WS may not have data yet)
        await this.readAndBroadcastHttp();
        break;

      case 'data':
        // Send to container via WebSocket (immediate) or HTTP fallback
        if (this.containerWsReady && this.containerWs) {
          // WebSocket: just send, output comes back via container WS message handler
          await this.sendToContainerWs(msg);
        } else {
          // HTTP fallback: use combined write+read
          await this.writeAndReadHttp(msg);
        }
        break;

      case 'resize':
        await this.sendToContainerWs(msg);
        if (this.sessionState) {
          this.sessionState.cols = msg.cols;
          this.sessionState.rows = msg.rows;
        }
        break;

      case 'ping':
        ws.send(serializeMessage({ type: 'pong', timestamp: msg.timestamp }));
        // With WebSocket streaming, we don't need to poll on ping
        // But if WS is not ready, do HTTP read as fallback
        if (!this.containerWsReady) {
          await this.readAndBroadcastHttp();
        }
        break;

      case 'exit':
        console.log('[WS] Client exit');
        break;

      default:
        console.log('[WS] Unknown type:', (msg as Message).type);
    }
  }

  async webSocketClose(ws: WebSocket, code: number, reason: string): Promise<void> {
    console.log('[WS] Closed:', code, reason, 'remaining:', this.ctx.getWebSockets().length);
    
    if (this.sessionState) {
      await this.ctx.storage.put('sessionState', this.sessionState);
    }
    
    // If no more clients, close container WebSocket
    if (this.ctx.getWebSockets().length === 0) {
      this.closeContainerWs();
    }
  }

  webSocketError(ws: WebSocket, error: unknown): void {
    console.error('[WS] Error:', error);
  }
}
