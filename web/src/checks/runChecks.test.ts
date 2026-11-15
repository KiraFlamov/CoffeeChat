import { describe, expect, it, vi } from 'vitest';
import { strings } from '../strings';
import { reconnectDelayMs, runChecks, type CheckDeps } from './runChecks';
import { initialChecks, type Checks } from './types';
import { WsError, type WsConnection } from './ws';

const okConnection = (): WsConnection => ({
  echo: () => Promise.resolve(),
  close: vi.fn(),
});

/** Зависимости, с которыми все проверки проходят; тест подменяет нужные. */
function makeDeps(overrides: Partial<CheckDeps> = {}): CheckDeps {
  return {
    randomNonce: () => new Uint8Array([1, 2, 3]),
    ping: (nonce) => Promise.resolve({ nonce, version: 'abc1234', storeOk: true }),
    connectWs: () => Promise.resolve(okConnection()),
    callWorker: () => Promise.resolve(),
    registerServiceWorker: () => Promise.resolve(),
    sleep: () => Promise.resolve(),
    ...overrides,
  };
}

async function run(deps: CheckDeps): Promise<Checks> {
  let checks = initialChecks();
  await runChecks(deps, (id, state) => {
    checks = { ...checks, [id]: state };
  });
  return checks;
}

describe('runChecks', () => {
  it('всё работает — пять зелёных строк', async () => {
    const checks = await run(makeDeps());
    expect(Object.values(checks).map((c) => c.status)).toEqual(['ok', 'ok', 'ok', 'ok', 'ok']);
    expect(checks.http).toEqual({ status: 'ok', detail: strings.details.version('abc1234') });
  });

  it('Ping не прошёл — красные «HTTP API» и «База», остальные зелёные', async () => {
    const checks = await run(
      makeDeps({ ping: () => Promise.reject(new TypeError('Failed to fetch')) }),
    );
    expect(checks.http).toMatchObject({
      status: 'fail',
      reason: expect.stringContaining(strings.reasons.noConnection),
    });
    expect(checks.store.status).toBe('fail');
    expect(checks.wsBody.status).toBe('ok');
    expect(checks.workers.status).toBe('ok');
  });

  it('сервер вернул другой nonce — «HTTP API» красная', async () => {
    const checks = await run(
      makeDeps({
        ping: () =>
          Promise.resolve({ nonce: new Uint8Array([9]), version: 'abc1234', storeOk: true }),
      }),
    );
    expect(checks.http).toEqual({ status: 'fail', reason: strings.reasons.nonceMismatch });
  });

  it('store_ok = false — «HTTP API» зелёная, «База» красная', async () => {
    const checks = await run(
      makeDeps({ ping: (nonce) => Promise.resolve({ nonce, version: 'v', storeOk: false }) }),
    );
    expect(checks.http.status).toBe('ok');
    expect(checks.store).toEqual({ status: 'fail', reason: strings.reasons.storeDown });
  });

  it('Echo шлётся дважды: телом и через Send API', async () => {
    const echo = vi.fn<WsConnection['echo']>(() => Promise.resolve());
    const close = vi.fn();
    await run(makeDeps({ connectWs: () => Promise.resolve({ echo, close }) }));
    expect(echo.mock.calls.map((call) => call[1])).toEqual([false, true]);
    expect(close).toHaveBeenCalledOnce();
  });

  it('WebSocket не открылся — одна попытка через 1 с, затем красные строки', async () => {
    const connectWs = vi.fn(() => Promise.reject(new WsError('closed')));
    const sleep = vi.fn(() => Promise.resolve());
    const checks = await run(makeDeps({ connectWs, sleep }));

    expect(connectWs).toHaveBeenCalledTimes(2);
    expect(sleep).toHaveBeenCalledExactlyOnceWith(reconnectDelayMs);
    expect(checks.wsBody).toEqual({ status: 'fail', reason: strings.reasons.wsClosed });
    expect(checks.wsSend).toEqual({ status: 'fail', reason: strings.reasons.wsClosed });
  });

  it('WebSocket закрылся посреди проверки — после переподключения строка зелёная', async () => {
    const broken: WsConnection = {
      echo: (_nonce, viaSendApi) =>
        viaSendApi ? Promise.reject(new WsError('closed')) : Promise.resolve(),
      close: vi.fn(),
    };
    const connectWs = vi
      .fn<CheckDeps['connectWs']>()
      .mockResolvedValueOnce(broken)
      .mockResolvedValueOnce(okConnection());
    const checks = await run(makeDeps({ connectWs }));

    expect(connectWs).toHaveBeenCalledTimes(2);
    expect(checks.wsBody.status).toBe('ok');
    expect(checks.wsSend.status).toBe('ok');
  });

  it('таймаут и кадр Error не вызывают переподключения', async () => {
    const connection: WsConnection = {
      echo: (_nonce, viaSendApi) =>
        Promise.reject(viaSendApi ? new WsError('timeout') : new WsError('server', 'internal')),
      close: vi.fn(),
    };
    const connectWs = vi.fn(() => Promise.resolve(connection));
    const checks = await run(makeDeps({ connectWs }));

    expect(connectWs).toHaveBeenCalledOnce();
    expect(checks.wsBody).toEqual({
      status: 'fail',
      reason: strings.reasons.serverError('internal'),
    });
    expect(checks.wsSend).toEqual({ status: 'fail', reason: strings.reasons.timeout });
  });

  it('воркеры: сбой любого из двух — красная строка с причиной', async () => {
    const worker = await run(makeDeps({ callWorker: () => Promise.reject(new Error('boom')) }));
    expect(worker.workers).toMatchObject({
      status: 'fail',
      reason: expect.stringContaining(strings.reasons.workerFailed),
    });

    const sw = await run(
      makeDeps({
        registerServiceWorker: () => Promise.reject(new Error(strings.reasons.swUnsupported)),
      }),
    );
    expect(sw.workers).toEqual({ status: 'fail', reason: strings.reasons.swUnsupported });
  });
});
