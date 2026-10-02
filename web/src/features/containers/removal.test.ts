import { describe, expect, it } from 'vitest';

import { needsForce, stackOf } from './removal';

describe('needsForce', () => {
  it('is true for the states the engine refuses to remove', () => {
    expect(needsForce('running')).toBe(true);
    expect(needsForce('paused')).toBe(true);
    expect(needsForce('restarting')).toBe(true);
  });

  it('is false for stopped containers', () => {
    for (const state of ['exited', 'created', 'dead']) {
      expect(needsForce(state)).toBe(false);
    }
  });
});

describe('stackOf', () => {
  it('prefers the Iskele stack label over the compose project', () => {
    expect(
      stackOf({ labels: { 'com.iskele.stack': 'blog', 'com.docker.compose.project': 'other' } }),
    ).toBe('blog');
  });

  it('falls back to the compose project', () => {
    expect(stackOf({ labels: { 'com.docker.compose.project': 'shop' } })).toBe('shop');
  });

  it('is undefined for a standalone container', () => {
    expect(stackOf({ labels: {} })).toBeUndefined();
  });
});
