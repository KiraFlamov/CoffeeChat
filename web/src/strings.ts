import type { CheckId } from './checks/types';

/**
 * Все тексты интерфейса в одном месте. На этапе 1 этот файл заменит i18n (RU + EN);
 * в компонентах строк нет.
 */
export const strings = {
  title: 'Состояние системы',
  subtitle: 'Страница сама проверяет все пути от браузера до базы.',
  retry: 'Повторить',
  running: 'Проверяю…',
  status: {
    pending: 'проверяется',
    ok: 'работает',
    fail: 'ошибка',
  },
  checks: {
    http: 'HTTP API',
    store: 'База',
    wsBody: 'WebSocket: ответ телом',
    wsSend: 'WebSocket: Send API',
    workers: 'Воркеры',
  } satisfies Record<CheckId, string>,
  reasons: {
    noConnection: 'нет соединения',
    timeout: 'нет ответа за отведённое время',
    nonceMismatch: 'сервер вернул другой nonce',
    storeDown: 'сервер не достучался до базы',
    noPing: 'нет ответа Ping',
    wsClosed: 'соединение закрылось',
    serverError: (code: string) => `сервер ответил ошибкой «${code}»`,
    workerFailed: 'Web Worker не ответил',
    swUnsupported: 'Service Worker не поддерживается браузером',
    swFailed: 'Service Worker не зарегистрировался',
    unknown: 'неизвестная ошибка',
  },
  details: {
    version: (version: string) => `версия ${version}`,
    workers: 'Web Worker ответил, Service Worker зарегистрирован',
  },
};
