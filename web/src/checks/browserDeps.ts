import { randomBytes, startCryptoWorker } from '@coffeechat/crypto';
import { SystemService } from '@coffeechat/proto/coffeechat/v1/system_pb';
import { createClient } from '@connectrpc/connect';
import { createConnectTransport } from '@connectrpc/connect-web';
import type { Config } from '../config';
import { strings } from '../strings';
import type { CheckDeps } from './runChecks';
import { browserSocket, connectWs } from './ws';

/** Длина nonce в Ping и Echo: наибольшая, которую принимает сервер. */
const nonceLength = 32;
/** Срок Ping: с запасом на холодный старт контейнера в облаке. */
const pingTimeoutMs = 10_000;
const wsOpenTimeoutMs = 10_000;
const workerTimeoutMs = 5000;

const sleep = (ms: number) => new Promise<void>((resolve) => setTimeout(resolve, ms));

function withTimeout<T>(promise: Promise<T>, ms: number): Promise<T> {
  return new Promise<T>((resolve, reject) => {
    const timer = setTimeout(() => reject(new Error(strings.reasons.timeout)), ms);
    promise.then(resolve, reject).finally(() => clearTimeout(timer));
  });
}

/** Настоящие зависимости проверок: сеть, воркеры и случайность браузера. */
export function createBrowserDeps(config: Config): CheckDeps {
  const client = createClient(
    SystemService,
    createConnectTransport({ baseUrl: config.apiUrl, useBinaryFormat: true }),
  );
  return {
    randomNonce: () => randomBytes(nonceLength),
    ping: (nonce) => client.ping({ nonce }, { timeoutMs: pingTimeoutMs }),
    connectWs: () => connectWs(config.wsUrl, browserSocket, wsOpenTimeoutMs),
    async callWorker() {
      const worker = startCryptoWorker();
      try {
        // Если файл воркера не загрузился, ответа не будет — нужен таймаут.
        await withTimeout(worker.api.randomBytes(nonceLength), workerTimeoutMs);
      } finally {
        worker.terminate();
      }
    },
    async registerServiceWorker() {
      if (!('serviceWorker' in navigator)) {
        throw new Error(strings.reasons.swUnsupported);
      }
      try {
        await navigator.serviceWorker.register(`${import.meta.env.BASE_URL}sw.js`);
      } catch (error) {
        const detail = error instanceof Error ? ` (${error.message})` : '';
        throw new Error(`${strings.reasons.swFailed}${detail}`, { cause: error });
      }
    },
    sleep,
  };
}
