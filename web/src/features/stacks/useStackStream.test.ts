import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { act, renderHook, waitFor } from '@testing-library/react';

import { useStackStream } from './useStackStream';

vi.mock('../../api/client', () => ({
  fetchStreamTicket: vi.fn(async () => 'ticket-1'),
  eventSourceURL: (path: string, params: Record<string, string>) =>
    `http://localhost${path}?${new URLSearchParams(params).toString()}`,
}));

/** The minimum of the EventSource surface the hook touches. */
class FakeSource {
  static instances: FakeSource[] = [];

  listeners = new Map<string, ((event: MessageEvent<string>) => void)[]>();
  closed = false;

  constructor(readonly url: string) {
    FakeSource.instances.push(this);
  }

  addEventListener(type: string, handler: (event: MessageEvent<string>) => void) {
    this.listeners.set(type, [...(this.listeners.get(type) ?? []), handler]);
  }

  close() {
    this.closed = true;
  }

  /** Delivers one server event. */
  emit(type: string, data: unknown) {
    const payload = { data: JSON.stringify(data) } as MessageEvent<string>;
    for (const handler of this.listeners.get(type) ?? []) handler(payload);
  }
}

function latest(): FakeSource {
  const source = FakeSource.instances.at(-1);
  if (!source) throw new Error('no stream was opened');
  return source;
}

describe('useStackStream', () => {
  beforeEach(() => {
    FakeSource.instances = [];
    vi.stubGlobal('EventSource', FakeSource);
  });

  afterEach(() => {
    vi.unstubAllGlobals();
  });

  it('carries the ticket to the endpoint it was asked for', async () => {
    const { result } = renderHook(() => useStackStream());

    await act(async () => {
      await result.current.start('/stacks/s1/up', { pull: 'true' });
    });

    const url = new URL(latest().url);
    expect(url.pathname).toBe('/stacks/s1/up');
    expect(url.searchParams.get('ticket')).toBe('ticket-1');
    expect(url.searchParams.get('pull')).toBe('true');
    expect(result.current.state.phase).toBe('running');
  });

  it('collects progress, tagged with the service it came from', async () => {
    const { result } = renderHook(() => useStackStream());
    await act(async () => {
      await result.current.start('/stacks/s1/up');
    });

    act(() => latest().emit('step', { kind: 'step', service: 'db', message: 'shop-db-1 started' }));
    act(() => latest().emit('warn', { kind: 'warn', field: 'secrets', message: 'swarm-only' }));

    await waitFor(() => expect(result.current.state.lines).toHaveLength(2));
    expect(result.current.state.lines[0]?.service).toBe('db');
    expect(result.current.state.lines[1]?.kind).toBe('warn');
  });

  it('finishes on the done event', async () => {
    const finished = vi.fn();
    const { result } = renderHook(() => useStackStream(finished));
    await act(async () => {
      await result.current.start('/stacks/s1/up');
    });

    const source = latest();
    act(() => source.emit('done', { kind: 'done', message: 'stack deployed' }));

    await waitFor(() => expect(result.current.state.phase).toBe('succeeded'));
    expect(source.closed).toBe(true);
    expect(finished).toHaveBeenCalled();
  });

  // "This stack cannot be deployed" on its own tells an operator nothing about
  // which service to fix.
  it('keeps the per-service problems of a refused deploy', async () => {
    const { result } = renderHook(() => useStackStream());
    await act(async () => {
      await result.current.start('/stacks/s1/up');
    });

    act(() =>
      latest().emit('error', {
        code: 'VALIDATION_FAILED',
        message: 'this stack cannot be deployed',
        problems: [{ service: 'app', field: 'volumes', message: '/etc is outside allowed_paths' }],
      }),
    );

    await waitFor(() => expect(result.current.state.phase).toBe('failed'));
    expect(result.current.state.problems).toHaveLength(1);
    expect(result.current.state.problems[0]?.service).toBe('app');
  });

  it('survives a transport error with no payload', async () => {
    const { result } = renderHook(() => useStackStream());
    await act(async () => {
      await result.current.start('/stacks/s1/up');
    });

    act(() => {
      for (const handler of latest().listeners.get('error') ?? []) {
        handler({ data: undefined } as unknown as MessageEvent<string>);
      }
    });

    await waitFor(() => expect(result.current.state.phase).toBe('failed'));
    expect(result.current.state.error).toBeTruthy();
  });

  it('starts clean on a second run', async () => {
    const { result } = renderHook(() => useStackStream());
    await act(async () => {
      await result.current.start('/stacks/s1/up');
    });
    act(() => latest().emit('step', { kind: 'step', message: 'first' }));
    await waitFor(() => expect(result.current.state.lines).toHaveLength(1));

    const first = latest();
    await act(async () => {
      await result.current.start('/stacks/s1/pull');
    });

    expect(first.closed).toBe(true);
    expect(result.current.state.lines).toHaveLength(0);
    expect(result.current.state.phase).toBe('running');
  });
});
