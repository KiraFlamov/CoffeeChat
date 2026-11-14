import { describe, expect, it } from 'vitest';
import { randomBytes } from './index';

describe('randomBytes', () => {
  it('возвращает массив нужной длины', () => {
    expect(randomBytes(32)).toHaveLength(32);
  });

  it('два вызова дают разные значения', () => {
    expect(randomBytes(32)).not.toEqual(randomBytes(32));
  });

  it.each([0, -1, 1.5, 65537])('отвергает длину %s', (length) => {
    expect(() => randomBytes(length)).toThrow(RangeError);
  });
});
