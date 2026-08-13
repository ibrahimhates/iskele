import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';

import { toast, useToasts } from './toast';

describe('toasts', () => {
  beforeEach(() => {
    vi.useFakeTimers();
    useToasts.getState().clear();
  });

  afterEach(() => {
    vi.useRealTimers();
  });

  it('queues in the order they were raised', () => {
    toast.success('first');
    toast.info('second');

    const { toasts } = useToasts.getState();
    expect(toasts.map((t) => t.message)).toEqual(['first', 'second']);
    expect(toasts[0]?.kind).toBe('success');
  });

  it('carries a detail line', () => {
    toast.error('prune failed', 'cannot reach the Docker daemon');
    expect(useToasts.getState().toasts[0]?.detail).toBe('cannot reach the Docker daemon');
  });

  it('dismisses a success on its own', () => {
    toast.success('done');
    expect(useToasts.getState().toasts).toHaveLength(1);

    vi.advanceTimersByTime(4_000);
    expect(useToasts.getState().toasts).toHaveLength(0);
  });

  // A failure is the one an operator has to read, and possibly copy out of the
  // screen; it must not vanish while they are reaching for the mouse.
  it('keeps an error until it is dismissed', () => {
    toast.error('boom');

    vi.advanceTimersByTime(60_000);
    expect(useToasts.getState().toasts).toHaveLength(1);

    useToasts.getState().dismiss(useToasts.getState().toasts[0]!.id);
    expect(useToasts.getState().toasts).toHaveLength(0);
  });

  it('gives every toast a distinct id, however identical', () => {
    const first = toast.info('same');
    const second = toast.info('same');

    expect(first).not.toBe(second);
    expect(useToasts.getState().toasts).toHaveLength(2);
  });

  // Dismissing one must not take its neighbours with it.
  it('dismisses only the toast asked for', () => {
    const keep = toast.error('keep me');
    const drop = toast.error('drop me');

    useToasts.getState().dismiss(drop);

    const { toasts } = useToasts.getState();
    expect(toasts).toHaveLength(1);
    expect(toasts[0]?.id).toBe(keep);
  });

  it('ignores a dismissal for a toast that is already gone', () => {
    const id = toast.success('done');
    useToasts.getState().dismiss(id);

    expect(() => useToasts.getState().dismiss(id)).not.toThrow();
    expect(useToasts.getState().toasts).toHaveLength(0);
  });

  // The auto-dismiss timer fires after the toast was dismissed by hand; it
  // must not take whatever was raised in the meantime.
  it('does not let a stale timer dismiss a later toast', () => {
    const first = toast.success('first');
    useToasts.getState().dismiss(first);

    toast.success('second');
    vi.advanceTimersByTime(4_000 - 1);

    expect(useToasts.getState().toasts.map((t) => t.message)).toEqual(['second']);
  });
});
