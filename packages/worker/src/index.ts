/**
 * OpenCode Relay Worker
 * 
 * Handles WebSocket connections from SSH relay and routes them to
 * ContainerManager Durable Objects.
 */

import { ContainerManager } from './container-manager';

export interface Env {
  CONTAINER_MANAGER: DurableObjectNamespace;
  OPENCODE_STATE: R2Bucket;
  AUTH_SECRET?: string;
  IDLE_TIMEOUT_MINUTES: string;
  CONTAINER_INSTANCE_TYPE: string;
}

export default {
  async fetch(request: Request, env: Env, ctx: ExecutionContext): Promise<Response> {
    const url = new URL(request.url);

    // CORS headers for preflight
    if (request.method === 'OPTIONS') {
      return new Response(null, {
        headers: {
          'Access-Control-Allow-Origin': '*',
          'Access-Control-Allow-Methods': 'GET, POST, OPTIONS',
          'Access-Control-Allow-Headers': '*',
        },
      });
    }

    // Health check endpoint
    if (url.pathname === '/health') {
      return new Response('OK', {
        headers: { 'Content-Type': 'text/plain' },
      });
    }

    // Info endpoint
    if (url.pathname === '/info') {
      return Response.json({
        service: 'opencode-relay',
        version: '1.0.0',
        status: 'running',
      });
    }

    // WebSocket endpoint
    if (url.pathname === '/ws') {
      return handleWebSocket(request, env);
    }

    // Session status endpoint
    if (url.pathname.startsWith('/session/') && url.pathname.endsWith('/status')) {
      const sessionId = url.pathname.split('/')[2];
      return getSessionStatus(sessionId, env);
    }

    return new Response('Not Found', { status: 404 });
  },
};

async function handleWebSocket(request: Request, env: Env): Promise<Response> {
  // Verify WebSocket upgrade
  const upgradeHeader = request.headers.get('Upgrade');
  if (upgradeHeader !== 'websocket') {
    return new Response('Expected WebSocket upgrade', { status: 426 });
  }

  // Get session ID from header (SSH key fingerprint)
  const sessionId = request.headers.get('X-Session-ID');
  if (!sessionId) {
    return new Response('Missing X-Session-ID header', { status: 401 });
  }

  // Optional: Verify auth secret
  const authSecret = request.headers.get('X-Auth-Secret');
  if (env.AUTH_SECRET && authSecret !== env.AUTH_SECRET) {
    return new Response('Unauthorized', { status: 401 });
  }

  // Log connection
  console.log('WebSocket connection for session:', sessionId);

  // Get or create Durable Object for this session
  const id = env.CONTAINER_MANAGER.idFromName(sessionId);
  const stub = env.CONTAINER_MANAGER.get(id);

  // Forward request to Durable Object
  // The DO will handle WebSocket upgrade and container management
  return stub.fetch(request);
}

async function getSessionStatus(sessionId: string, env: Env): Promise<Response> {
  try {
    const id = env.CONTAINER_MANAGER.idFromName(sessionId);
    const stub = env.CONTAINER_MANAGER.get(id);
    
    const response = await stub.fetch(new Request('http://internal/status'));
    return response;
  } catch (error) {
    return Response.json({ error: 'Session not found' }, { status: 404 });
  }
}

// Export the Durable Object class
export { ContainerManager };
