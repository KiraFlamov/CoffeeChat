import { expose } from 'comlink';
import { afterEach, describe, expect, it } from 'vitest';
import { cryptoApi } from './api';
import { connectCrypto } from './index';

// В тестах нет настоящего Web Worker: его заменяет MessageChannel —
// тот же обмен сообщениями, что и между страницей и воркером.
describe('API воркера через Comlink', () => {
  let channel: MessageChannel | undefined;

  afterEach(() => {
    channel?.port1.close();
    channel?.port2.close();
  });

  function connect() {
    channel = new MessageChannel();
    expose(cryptoApi, channel.port1);
    return connectCrypto(channel.port2);
  }

  it('заглушка отвечает', async () => {
    const api = connect();
    const bytes = await api.randomBytes(32);
    expect(bytes).toBeInstanceOf(Uint8Array);
    expect(bytes).toHaveLength(32);
  });

  it('ошибка функции доходит до вызывающего', async () => {
    const api = connect();
    // Comlink передаёт имя и текст ошибки, но не её класс.
    await expect(api.randomBytes(0)).rejects.toMatchObject({
      name: 'RangeError',
      message: expect.stringContaining('length'),
    });
  });
});
