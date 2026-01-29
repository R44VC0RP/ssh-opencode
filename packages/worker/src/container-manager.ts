import { Container } from '@cloudflare/containers';
import { parseMessage, serializeMessage, type Message, type InitMessage, type DataMessage } from './protocol';

function getDataLength(msg: Message): number {
  return msg.type === 'data' ? (msg as DataMessage).data.length : 0;
}

/**
 * ContainerManager - Cloudflare Container-enabled Durable Object
 * 
 * Manages a single OpenCode container instance for a user session.
 * Uses fast HTTP polling (triggered by 1-second client pings) for responsive I/O.
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

  override onStart(): void {
    console.log('[Container] Started for session:', this.ctx.id.toString());
  }

  override onStop(): void {
    console.log('[Container] Stopped for session:', this.ctx.id.toString());
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
        
        await this.initializePTY(cols, rows, repo);
        server.send(JSON.stringify({ type: 'status', message: 'Ready!' }));
        
        // Initial read with logging
        console.log('[Init] Doing initial read...');
        await this.readAndBroadcast();
        console.log('[Init] Initial read complete');
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

  private async sendToContainer(msg: Message): Promise<void> {
    console.log('[Write] Sending type:', msg.type, 'data length:', getDataLength(msg));
    try {
      const response = await this.containerFetch('http://container:8080/write', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify(msg),
      });
      if (!response.ok) {
        console.error('[Write] Failed:', response.status);
      } else {
        console.log('[Write] OK');
      }
    } catch (err) {
      console.error('[Write] Error:', err);
    }
  }

  private async readAndBroadcast(): Promise<void> {
    try {
      console.log('[Read] Fetching /read...');
      const response = await this.containerFetch('http://container:8080/read', { 
        method: 'GET',
        signal: AbortSignal.timeout(2000),
      });
      
      console.log('[Read] Response status:', response.status);
      
      if (response.ok) {
        const text = await response.text();
        console.log('[Read] Response length:', text.length, 'bytes');
        
        if (text.trim()) {
          const lines = text.split('\n').filter((l: string) => l.trim());
          console.log('[Read] Got', lines.length, 'message(s)');
          
          for (const line of lines) {
            const msg = parseMessage(line);
            if (msg) {
              console.log('[Read] Broadcasting type:', msg.type, 'data length:', getDataLength(msg));
              this.broadcastToWebSockets(msg);
              if (msg.type === 'exit') {
                console.log('[Read] PTY exited with code:', msg.code);
              }
            } else {
              console.log('[Read] Failed to parse:', line.substring(0, 100));
            }
          }
        } else {
          console.log('[Read] Empty response');
        }
      } else {
        console.log('[Read] Non-OK status:', response.status);
      }
    } catch (err) {
      if (err instanceof DOMException && err.name === 'TimeoutError') {
        console.log('[Read] Timeout (no data)');
      } else {
        console.error('[Read] Error:', err);
      }
    }
  }

  private broadcastToWebSockets(msg: Message): void {
    const data = serializeMessage(msg);
    const wsCount = this.ctx.getWebSockets().length;
    console.log('[Broadcast] To', wsCount, 'client(s), data length:', data.length);
    
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

    console.log('[WS] Received type:', msg.type);

    switch (msg.type) {
      case 'init':
        console.log('[WS] Init message');
        await this.ensureContainerReady();
        await this.readAndBroadcast();
        break;

      case 'data':
        await this.sendToContainer(msg);
        await new Promise(r => setTimeout(r, 10));
        await this.readAndBroadcast();
        break;

      case 'resize':
        await this.sendToContainer(msg);
        if (this.sessionState) {
          this.sessionState.cols = msg.cols;
          this.sessionState.rows = msg.rows;
        }
        break;

      case 'ping':
        ws.send(serializeMessage({ type: 'pong', timestamp: msg.timestamp }));
        await this.readAndBroadcast();
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
  }

  webSocketError(ws: WebSocket, error: unknown): void {
    console.error('[WS] Error:', error);
  }
}
