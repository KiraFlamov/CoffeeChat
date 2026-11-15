import { create, fromBinary, toBinary } from '@bufbuild/protobuf';
import { ClientFrameSchema, ServerFrameSchema } from '@coffeechat/proto/coffeechat/v1/ws_pb';

/** Что умеет сокет: отправить двоичный кадр и закрыться. */
export interface Socket {
  send(data: Uint8Array<ArrayBuffer>): void;
  close(): void;
}

/** События сокета. data — то, что пришло в сообщении: ArrayBuffer или строка. */
export interface SocketHandlers {
  onOpen(): void;
  onMessage(data: unknown): void;
  onClose(): void;
}

/** Открывает сокет. В браузере — browserSocket, в тестах — подставной. */
export type SocketFactory = (url: string, handlers: SocketHandlers) => Socket;

export const browserSocket: SocketFactory = (url, handlers) => {
  const ws = new WebSocket(url);
  ws.binaryType = 'arraybuffer';
  ws.onopen = () => handlers.onOpen();
  ws.onmessage = (event) => handlers.onMessage(event.data);
  // После ошибки браузер всегда присылает close, отдельно её не слушаем.
  ws.onclose = () => handlers.onClose();
  return {
    send: (data) => ws.send(data),
    close: () => ws.close(),
  };
};

export type WsErrorKind =
  /** Соединение не открылось или закрылось. */
  | 'closed'
  /** Нет ответа за отведённое время. */
  | 'timeout'
  /** Сервер ответил кадром Error. */
  | 'server'
  /** Пришло то, что не разбирается как ServerFrame. */
  | 'protocol';

export class WsError extends Error {
  readonly kind: WsErrorKind;
  /** Код из кадра Error, если kind = 'server'. */
  readonly code: string;

  constructor(kind: WsErrorKind, code = '') {
    super(code ? `websocket: ${kind} (${code})` : `websocket: ${kind}`);
    this.name = 'WsError';
    this.kind = kind;
    this.code = code;
  }
}

/** Открытое соединение с сервером. */
export interface WsConnection {
  /**
   * Отправляет Echo и ждёт такой же в ответ.
   * viaSendApi = true просит сервер ответить через Send API шлюза, а не телом ответа.
   * Запросы идут по одному: следующий — после ответа на предыдущий.
   */
  echo(nonce: Uint8Array, viaSendApi: boolean, timeoutMs: number): Promise<void>;
  close(): void;
}

interface Pending {
  readonly nonce: Uint8Array;
  readonly viaSendApi: boolean;
  readonly resolve: () => void;
  readonly reject: (error: WsError) => void;
  readonly timer: ReturnType<typeof setTimeout>;
}

export function bytesEqual(a: Uint8Array, b: Uint8Array): boolean {
  return a.length === b.length && a.every((byte, i) => byte === b[i]);
}

/** Открывает соединение. Отклоняется с WsError, если оно не открылось за timeoutMs. */
export function connectWs(
  url: string,
  factory: SocketFactory,
  timeoutMs: number,
): Promise<WsConnection> {
  return new Promise((resolveOpen, rejectOpen) => {
    let opened = false;
    let closed = false;
    let pending: Pending | undefined;

    const settle = (error?: WsError) => {
      if (!pending) return;
      const { resolve, reject, timer } = pending;
      pending = undefined;
      clearTimeout(timer);
      if (error) reject(error);
      else resolve();
    };

    const socket = factory(url, {
      onOpen() {
        opened = true;
        clearTimeout(openTimer);
        resolveOpen(connection);
      },
      onMessage(data) {
        if (!(data instanceof ArrayBuffer)) {
          // Протокол только двоичный.
          settle(new WsError('protocol'));
          return;
        }
        let frame;
        try {
          frame = fromBinary(ServerFrameSchema, new Uint8Array(data));
        } catch {
          settle(new WsError('protocol'));
          return;
        }
        switch (frame.payload.case) {
          case 'echo': {
            const echo = frame.payload.value;
            // Чужой или запоздавший ответ пропускаем: ждём свой до таймаута.
            if (
              pending &&
              echo.viaSendApi === pending.viaSendApi &&
              bytesEqual(echo.nonce, pending.nonce)
            ) {
              settle();
            }
            return;
          }
          case 'error':
            settle(new WsError('server', frame.payload.value.code));
            return;
          default:
            settle(new WsError('protocol'));
        }
      },
      onClose() {
        closed = true;
        clearTimeout(openTimer);
        if (!opened) rejectOpen(new WsError('closed'));
        settle(new WsError('closed'));
      },
    });

    const openTimer = setTimeout(() => {
      if (opened || closed) return;
      closed = true;
      socket.close();
      rejectOpen(new WsError('timeout'));
    }, timeoutMs);

    const connection: WsConnection = {
      echo(nonce, viaSendApi, echoTimeoutMs) {
        return new Promise<void>((resolve, reject) => {
          if (closed) {
            reject(new WsError('closed'));
            return;
          }
          if (pending) {
            reject(new WsError('protocol'));
            return;
          }
          const timer = setTimeout(() => settle(new WsError('timeout')), echoTimeoutMs);
          pending = { nonce, viaSendApi, resolve, reject, timer };
          const frame = create(ClientFrameSchema, {
            payload: { case: 'echo', value: { nonce, viaSendApi } },
          });
          socket.send(toBinary(ClientFrameSchema, frame));
        });
      },
      close() {
        closed = true;
        socket.close();
        settle(new WsError('closed'));
      },
    };
  });
}
