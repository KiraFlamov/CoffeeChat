/**
 * API криптографии, который выполняется внутри Web Worker.
 * На этапе 0 в нём одна функция-заглушка: она доказывает, что воркер запускается
 * на хостинге и отвечает через Comlink. Настоящие операции появятся на этапе 1.
 */

/** Возвращает массив случайных байтов из системного генератора. */
export function randomBytes(length: number): Uint8Array {
  if (!Number.isInteger(length) || length <= 0 || length > 65536) {
    throw new RangeError('length должен быть целым числом от 1 до 65536');
  }
  return crypto.getRandomValues(new Uint8Array(length));
}

/** Функции, доступные основному потоку через Comlink. */
export const cryptoApi = {
  randomBytes,
};

export type CryptoApi = typeof cryptoApi;
