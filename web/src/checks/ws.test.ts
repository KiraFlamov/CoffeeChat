import { create, fromBinary, toBinary } from '@bufbuild/protobuf';
import {
  ClientFrameSchema,
  ServerFrameSchema,
  type ServerFrame,
} from '@coffeechat/proto/coffeechat/v1/ws_pb';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { connectWs, WsError, type SocketFactory, type SocketHandlers } from './ws';

/** Подставной сокет: запоминает отправленное и даёт тесту играть роль сервера. */
class FakeSocket {
  readonly sent: Uint8Array[] = [];
  closed = false;
  handlers!: SocketHandlers;

  readonly factory: SocketFactory = (_url, handlers) => {
    this.handlers = handlers;
    return {
      send: (data) => this.sent.push(data),
      close: () => {
        this.closed = true;
      },
    };
  };

  /** Сервер присылает двоичный кадр. */
  receive(frame: ServerFrame): void {
    const bytes = toBinary(ServerFrameSchema, frame);
    this.handlers.onMessage(bytes.buffer.slice(bytes.byteOffset, bytes.byteOffset + bytes.length));
  }

  lastSent() {
    const data = this.sent.at(-1);
    if (!data) throw new Error('клиент ничего не отправил');
    return fromBinary(ClientFrameSchema, data);
  }
}

const echoFrame = (nonce: Uint8Array, viaSendApi: boolean) =>
  create(ServerFrameSchema, { payload: { case: 'echo', value: { nonce, viaSendApi } } });

const errorFrame = (code: string) =>
  create(ServerFrameSchema, { payload: { case: 'error', value: { code, message: 'x' } } });

async function open(socket: FakeSocket) {
  const connecting = connectWs('ws://test/ws', socket.factory, 1000);
  socket.handlers.onOpen();
  return connecting;
}

describe('клиент WebSocket', () => {
  beforeEach(() => {
    vi.useFakeTimers();
  });
  afterEach(() => {
    vi.useRealTimers();
  });

  const nonce = new Uint8Array([1, 2, 3]);

  it('кодирует ClientFrame и принимает ServerFrame с тем же Echo', async () => {
    const socket = new FakeSocket();
    const connection = await open(socket);

    const done = connection.echo(nonce, false, 5000);
    const sent = socket.lastSent();
    expect(sent.payload.case).toBe('echo');
    expect(sent.payload.value).toMatchObject({ nonce, viaSendApi: false });

    socket.receive(echoFrame(nonce, false));
    await expect(done).resolves.toBeUndefined();
  });

  it('передаёт флаг via_send_api и ждёт ответ с тем же флагом', async () => {
    const socket = new FakeSocket();
    const connection = await open(socket);

    const done = connection.echo(nonce, true, 5000);
    expect(socket.lastSent().payload.value).toMatchObject({ viaSendApi: true });

    // Ответ с другим флагом и с чужим nonce — не наш.
    socket.receive(echoFrame(nonce, false));
    socket.receive(echoFrame(new Uint8Array([9]), true));
    socket.receive(echoFrame(nonce, true));
    await expect(done).resolves.toBeUndefined();
  });

  it('кадр Error отклоняет запрос с кодом сервера', async () => {
    const socket = new FakeSocket();
    const connection = await open(socket);

    const done = connection.echo(nonce, false, 5000);
    socket.receive(errorFrame('too_large'));
    await expect(done).rejects.toMatchObject({ kind: 'server', code: 'too_large' });
  });

  it('текстовое сообщение и мусор — ошибка протокола', async () => {
    const socket = new FakeSocket();
    const connection = await open(socket);

    const text = connection.echo(nonce, false, 5000);
    socket.handlers.onMessage('hello');
    await expect(text).rejects.toMatchObject({ kind: 'protocol' });

    const garbage = connection.echo(nonce, false, 5000);
    socket.handlers.onMessage(new Uint8Array([0xff, 0xff, 0xff]).buffer);
    await expect(garbage).rejects.toMatchObject({ kind: 'protocol' });
  });

  it('нет ответа — таймаут', async () => {
    const socket = new FakeSocket();
    const connection = await open(socket);

    const done = connection.echo(nonce, false, 5000);
    const expectation = expect(done).rejects.toMatchObject({ kind: 'timeout' });
    await vi.advanceTimersByTimeAsync(5000);
    await expectation;
  });

  it('закрытие соединения отклоняет запрос и следующие вызовы', async () => {
    const socket = new FakeSocket();
    const connection = await open(socket);

    const done = connection.echo(nonce, false, 5000);
    socket.handlers.onClose();
    await expect(done).rejects.toMatchObject({ kind: 'closed' });
    await expect(connection.echo(nonce, false, 5000)).rejects.toBeInstanceOf(WsError);
  });

  it('соединение не открылось — ошибка closed', async () => {
    const socket = new FakeSocket();
    const connecting = connectWs('ws://test/ws', socket.factory, 1000);
    socket.handlers.onClose();
    await expect(connecting).rejects.toMatchObject({ kind: 'closed' });
  });

  it('соединение не открылось вовремя — таймаут, сокет закрыт', async () => {
    const socket = new FakeSocket();
    const connecting = connectWs('ws://test/ws', socket.factory, 1000);
    const expectation = expect(connecting).rejects.toMatchObject({ kind: 'timeout' });
    await vi.advanceTimersByTimeAsync(1000);
    await expectation;
    expect(socket.closed).toBe(true);
  });
});
