/**
 * Вся криптография клиента живёт в этом пакете.
 * На этапе 0 здесь только заглушка: она нужна, чтобы проверить Web Worker на хостинге
 * и подключить пакет к сборке, тестам и CI.
 */
import { wrap, type Endpoint, type Remote } from 'comlink';
import type { CryptoApi } from './api';

export { randomBytes } from './api';
export type { CryptoApi } from './api';

/** Запущенный воркер криптографии. */
export interface CryptoWorker {
  /** Функции воркера; каждая возвращает Promise. */
  readonly api: Remote<CryptoApi>;
  /** Останавливает воркер. */
  terminate(): void;
}

/** Подключается к API криптографии на другом конце канала (воркер или MessagePort). */
export function connectCrypto(endpoint: Endpoint): Remote<CryptoApi> {
  return wrap<CryptoApi>(endpoint);
}

/** Запускает Web Worker с криптографией. Работает только в браузере. */
export function startCryptoWorker(): CryptoWorker {
  // Запись new Worker(new URL(...)) — способ, которым сборщик (Vite) находит файл воркера.
  const worker = new Worker(new URL('./worker.ts', import.meta.url), {
    type: 'module',
    name: 'coffeechat-crypto',
  });
  return {
    api: connectCrypto(worker),
    terminate: () => worker.terminate(),
  };
}
