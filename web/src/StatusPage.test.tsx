import { cleanup, fireEvent, render, screen } from '@testing-library/react';
import { afterEach, describe, expect, it, vi } from 'vitest';
import { App } from './App';
import type { CheckDeps } from './checks/runChecks';
import { initialChecks, type Checks } from './checks/types';
import { StatusPage } from './StatusPage';
import { strings } from './strings';

afterEach(cleanup);

function row(name: string): HTMLElement {
  const item = screen.getByText(name).closest('li');
  if (!item) throw new Error(`нет строки «${name}»`);
  return item;
}

describe('StatusPage', () => {
  it('показывает зелёную, красную и ожидающую строку', () => {
    const checks: Checks = {
      ...initialChecks(),
      http: { status: 'ok', detail: 'версия abc1234' },
      store: { status: 'fail', reason: strings.reasons.storeDown },
    };
    render(<StatusPage checks={checks} running={false} onRetry={() => {}} />);

    expect(row(strings.checks.http).dataset.status).toBe('ok');
    expect(row(strings.checks.http).textContent).toContain(strings.status.ok);
    expect(row(strings.checks.http).textContent).toContain('версия abc1234');

    expect(row(strings.checks.store).dataset.status).toBe('fail');
    expect(row(strings.checks.store).textContent).toContain(strings.status.fail);
    expect(row(strings.checks.store).textContent).toContain(strings.reasons.storeDown);

    expect(row(strings.checks.wsBody).dataset.status).toBe('pending');
    expect(screen.getAllByRole('listitem')).toHaveLength(5);
  });

  it('кнопка «Повторить» вызывает onRetry и выключена во время проверки', () => {
    const onRetry = vi.fn();
    const { rerender } = render(
      <StatusPage checks={initialChecks()} running={false} onRetry={onRetry} />,
    );
    fireEvent.click(screen.getByRole('button', { name: strings.retry }));
    expect(onRetry).toHaveBeenCalledOnce();

    rerender(<StatusPage checks={initialChecks()} running={true} onRetry={onRetry} />);
    expect(screen.getByRole('button', { name: strings.running })).toHaveProperty('disabled', true);
  });
});

describe('App', () => {
  const makeDeps = (ping: CheckDeps['ping']): CheckDeps => ({
    randomNonce: () => crypto.getRandomValues(new Uint8Array(32)),
    ping,
    connectWs: () => Promise.resolve({ echo: () => Promise.resolve(), close: () => {} }),
    callWorker: () => Promise.resolve(),
    registerServiceWorker: () => Promise.resolve(),
    sleep: () => Promise.resolve(),
  });

  it('проверяет при открытии, «Повторить» запускает проверки с новым nonce', async () => {
    const ping = vi.fn<CheckDeps['ping']>((nonce) =>
      Promise.resolve({ nonce, version: 'abc1234', storeOk: true }),
    );
    render(<App deps={makeDeps(ping)} />);

    const retry = await screen.findByRole('button', { name: strings.retry });
    for (const item of screen.getAllByRole('listitem')) {
      expect(item.dataset.status).toBe('ok');
    }

    fireEvent.click(retry);
    await screen.findByRole('button', { name: strings.retry });
    expect(ping).toHaveBeenCalledTimes(2);
    expect(ping.mock.calls[0]?.[0]).not.toEqual(ping.mock.calls[1]?.[0]);
  });
});
