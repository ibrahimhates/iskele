import { describe, expect, it } from 'vitest';

import en from './en.json';
import tr from './tr.json';

/** Flattens a nested translation object into dotted keys. */
function keysOf(value: unknown, prefix = ''): string[] {
  if (typeof value !== 'object' || value === null) return [prefix];
  return Object.entries(value as Record<string, unknown>).flatMap(([key, child]) =>
    keysOf(child, prefix ? `${prefix}.${key}` : key),
  );
}

describe('translations', () => {
  it('define the same keys in both languages', () => {
    // A missing key falls back to English silently, so the mismatch would only
    // surface as a stray English string in a Turkish UI.
    const english = keysOf(en).sort();
    const turkish = keysOf(tr).sort();

    expect(turkish.filter((key) => !english.includes(key))).toEqual([]);
    expect(english.filter((key) => !turkish.includes(key))).toEqual([]);
  });

  it('leave no empty strings', () => {
    for (const [language, resource] of [
      ['en', en],
      ['tr', tr],
    ] as const) {
      const empty = keysOf(resource).filter((key) => {
        const value = key.split('.').reduce<unknown>((node, part) => {
          return (node as Record<string, unknown>)?.[part];
        }, resource);
        return value === '';
      });
      expect(empty, `${language} has empty translations`).toEqual([]);
    }
  });
});
