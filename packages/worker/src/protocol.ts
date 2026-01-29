/**
 * Protocol types for communication between SSH relay, worker, and container
 */

export type MessageType = 'init' | 'data' | 'resize' | 'exit' | 'ping' | 'pong' | 'error';

export interface BaseMessage {
  type: MessageType;
}

export interface InitMessage extends BaseMessage {
  type: 'init';
  cols: number;
  rows: number;
  repo?: string;
}

export interface DataMessage extends BaseMessage {
  type: 'data';
  data: string; // Base64 encoded binary data
}

export interface ResizeMessage extends BaseMessage {
  type: 'resize';
  cols: number;
  rows: number;
}

export interface ExitMessage extends BaseMessage {
  type: 'exit';
  code: number;
}

export interface PingMessage extends BaseMessage {
  type: 'ping';
  timestamp: number;
}

export interface PongMessage extends BaseMessage {
  type: 'pong';
  timestamp: number;
}

export interface ErrorMessage extends BaseMessage {
  type: 'error';
  message: string;
}

export type Message = 
  | InitMessage 
  | DataMessage 
  | ResizeMessage 
  | ExitMessage 
  | PingMessage 
  | PongMessage
  | ErrorMessage;

export function parseMessage(data: string): Message | null {
  try {
    return JSON.parse(data) as Message;
  } catch {
    return null;
  }
}

export function serializeMessage(msg: Message): string {
  return JSON.stringify(msg);
}
