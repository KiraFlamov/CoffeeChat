import { strings } from '../strings';
import type { CheckId, CheckState } from './types';
import { bytesEqual, WsError, type WsConnection } from './ws';

/** Срок ответа на Echo. */
export const echoTimeoutMs = 5000;
/** Пауза перед единственной попыткой переподключения WebSocket. */
export const reconnectDelayMs = 1000;

export interface PingResult {
  readonly nonce: Uint8Array;
  readonly version: string;
  readonly storeOk: boolean;
}

/** Всё, что проверкам нужно от внешнего мира. В тестах подменяется. */
export interface CheckDeps {
  /** Случайный nonce для Ping и Echo. */
  randomNonce(): Uint8Array;
  /** Вызов SystemService.Ping. */
  ping(nonce: Uint8Array): Promise<PingResult>;
  /** Открывает WebSocket. */
  connectWs(): Promise<WsConnection>;
  /** Запускает Web Worker из packages/crypto и ждёт его ответа. */
  callWorker(): Promise<void>;
  /** Регистрирует Service Worker. */
  registerServiceWorker(): Promise<void>;
  sleep(ms: number): Promise<void>;
}

export type Report = (id: CheckId, state: CheckState) => void;

const ok = (detail?: string): CheckState =>
  detail === undefined ? { status: 'ok' } : { status: 'ok', detail };
const fail = (reason: string): CheckState => ({ status: 'fail', reason });

function errorText(error: unknown): string {
  return error instanceof Error && error.message ? error.message : strings.reasons.unknown;
}

function wsReason(error: unknown): string {
  if (!(error instanceof WsError)) return errorText(error);
  switch (error.kind) {
    case 'closed':
      return strings.reasons.noConnection;
    case 'timeout':
      return strings.reasons.timeout;
    case 'server':
      return strings.reasons.serverError(error.code);
    case 'protocol':
      return strings.reasons.serverError('bad_frame');
  }
}

/** Строки «HTTP API» и «База»: один вызов Ping. */
async function checkPing(deps: CheckDeps, report: Report): Promise<void> {
  const nonce = deps.randomNonce();
  let result: PingResult;
  try {
    result = await deps.ping(nonce);
  } catch (error) {
    report('http', fail(`${strings.reasons.noConnection} (${errorText(error)})`));
    report('store', fail(strings.reasons.noPing));
    return;
  }
  if (!bytesEqual(result.nonce, nonce)) {
    report('http', fail(strings.reasons.nonceMismatch));
    report('store', fail(strings.reasons.noPing));
    return;
  }
  report('http', ok(strings.details.version(result.version)));
  report('store', result.storeOk ? ok() : fail(strings.reasons.storeDown));
}

type WsCheckId = 'wsBody' | 'wsSend';

/**
 * Один проход проверок WebSocket. Возвращает проверку, на которой соединение
 * закрылось, или undefined, если обе строки получили окончательный результат.
 */
async function wsAttempt(
  deps: CheckDeps,
  report: Report,
  todo: readonly WsCheckId[],
): Promise<WsCheckId | undefined> {
  let connection: WsConnection;
  try {
    connection = await deps.connectWs();
  } catch (error) {
    if (error instanceof WsError && error.kind === 'closed') return todo[0];
    for (const id of todo) report(id, fail(wsReason(error)));
    return undefined;
  }
  try {
    for (const id of todo) {
      try {
        await connection.echo(deps.randomNonce(), id === 'wsSend', echoTimeoutMs);
        report(id, ok());
      } catch (error) {
        if (error instanceof WsError && error.kind === 'closed') return id;
        report(id, fail(wsReason(error)));
      }
    }
    return undefined;
  } finally {
    connection.close();
  }
}

/** Строки WebSocket. Если соединение закрылось — одна попытка переподключения через 1 с. */
async function checkWs(deps: CheckDeps, report: Report): Promise<void> {
  const all: readonly WsCheckId[] = ['wsBody', 'wsSend'];
  const interrupted = await wsAttempt(deps, report, all);
  if (interrupted === undefined) return;

  await deps.sleep(reconnectDelayMs);
  const rest = all.slice(all.indexOf(interrupted));
  const again = await wsAttempt(deps, report, rest);
  if (again === undefined) return;
  for (const id of rest.slice(rest.indexOf(again))) {
    report(id, fail(strings.reasons.wsClosed));
  }
}

/** Строка «Воркеры»: Web Worker ответил и Service Worker зарегистрировался. */
async function checkWorkers(deps: CheckDeps, report: Report): Promise<void> {
  const [worker, serviceWorker] = await Promise.allSettled([
    deps.callWorker(),
    deps.registerServiceWorker(),
  ]);
  const reasons: string[] = [];
  if (worker.status === 'rejected') {
    reasons.push(`${strings.reasons.workerFailed} (${errorText(worker.reason)})`);
  }
  if (serviceWorker.status === 'rejected') {
    reasons.push(errorText(serviceWorker.reason));
  }
  report('workers', reasons.length > 0 ? fail(reasons.join('; ')) : ok(strings.details.workers));
}

/**
 * Выполняет все проверки одновременно и сообщает результат каждой через report.
 * Никогда не отклоняется: любая ошибка становится красной строкой.
 */
export async function runChecks(deps: CheckDeps, report: Report): Promise<void> {
  await Promise.all([checkPing(deps, report), checkWs(deps, report), checkWorkers(deps, report)]);
}
