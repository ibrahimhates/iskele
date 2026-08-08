import { describe, expect, it } from 'vitest';

import { characterClasses, passwordProblem, MIN_PASSWORD_LENGTH } from './password';

describe('password policy', () => {
  it('mirrors the server floor', () => {
    // A drift here would let the form accept a password the API rejects.
    expect(MIN_PASSWORD_LENGTH).toBe(12);
  });

  it('rejects passwords that are too short', () => {
    expect(passwordProblem('Short1!')).not.toBeNull();
    expect(passwordProblem('aB1'.repeat(3) + 'x')).not.toBeNull();
  });

  it('rejects a long password using a single character class', () => {
    expect(passwordProblem('a'.repeat(20))).not.toBeNull();
  });

  it('accepts a passphrase with two classes', () => {
    expect(passwordProblem('correct-horse-battery-staple1')).toBeNull();
    expect(passwordProblem('CorrectHorseBattery')).toBeNull();
  });

  it('counts classes the way the server does', () => {
    expect(characterClasses('abcdefghijkl')).toBe(1);
    expect(characterClasses('abcdefghijk1')).toBe(2);
    expect(characterClasses('Abcdefghijk1')).toBe(3);
    expect(characterClasses('Abcdefghij1!')).toBe(4);
  });
});
