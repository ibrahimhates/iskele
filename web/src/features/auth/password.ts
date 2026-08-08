/** Mirrors the server's policy so the form can explain a rejection first. */
export const MIN_PASSWORD_LENGTH = 12;

/** Returns a translation key describing the problem, or null when valid. */
export function passwordProblem(password: string): string | null {
  if (password.length < MIN_PASSWORD_LENGTH) return 'auth.passwordRules';
  if (characterClasses(password) < 2) return 'auth.passwordRules';
  return null;
}

/** Counts how many of lowercase, uppercase, digit and symbol appear. */
export function characterClasses(password: string): number {
  let count = 0;
  if (/[a-z]/.test(password)) count++;
  if (/[A-Z]/.test(password)) count++;
  if (/[0-9]/.test(password)) count++;
  // Anything that is not a letter or digit counts as a symbol, matching the
  // server's "default" branch rather than a fixed punctuation list.
  if (/[^a-zA-Z0-9]/.test(password)) count++;
  return count;
}
