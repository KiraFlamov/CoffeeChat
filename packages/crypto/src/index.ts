/**
 * Вся криптография клиента живёт в этом пакете.
 * На этапе 0 здесь только заглушка: она нужна, чтобы подключить пакет к сборке, тестам и CI.
 */

/** Возвращает массив случайных байтов из системного генератора. */
export function randomBytes(length: number): Uint8Array {
  if (!Number.isInteger(length) || length <= 0 || length > 65536) {
    throw new RangeError('length должен быть целым числом от 1 до 65536');
  }
  return crypto.getRandomValues(new Uint8Array(length));
}
