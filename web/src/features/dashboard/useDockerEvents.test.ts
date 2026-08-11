import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { act, renderHook, waitFor } from '@testing-library/react';

import { useDockerEvents } from './useDockerEvents';

const fetchStreamTicket = vi.fn(async () => 'ticket-1');

vi.mock('../../api/client', () => ({
  fetchStreamTicket: () => fetchStreamTicket(),
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

  emit(type: string, data: unknown) {
    const payload = { data: JSON.stringify(data) } as MessageEvent<string>;
    for (const handler of this.listeners.get(type) ?? []) handler(payload);
  }

  /** Fires a listener that carries no payload, the way the browser does. */
  fire(type: string) {
    for (const handler of this.listeners.get(type) ?? []) {
      handler({} as MessageEvent<string>);
    }
  }
}

function latest(): FakeSource {
  const source = FakeSource.instances.at(-1);
  if (!source) throw new Error('no stream was opened');
  return source;
}

const started = {
  type: 'container',
  action: 'start',
  actor: 'abc123',
  name: 'web',
  time: '2026-08-11T10:00:00Z',
};
const died = {
  type: 'container',
  action: 'die',
  actor: 'abc123',
  name: 'web',
  time: '2026-08-11T10:01:00Z',
};

describe('useDockerEvents', () => {
  beforeEach(() => {
    FakeSource.instances = [];
    fetchStreamTicket.mockClear();
    fetchStreamTicket.mockResolvedValue('ticket-1');
    vi.stubGlobal('EventSource', FakeSource);
  });

  afterEach(() => {
    vi.unstubAllGlobals();
    vi.useRealTimers();
  });

  it('opens the engine event stream with a ticket', async () => {
    renderHook(() => useDockerEvents());

    await waitFor(() => expect(FakeSource.instances).toHaveLength(1));
    const url = new URL(latest().url);
    expect(url.pathname).toBe('/system/events');
    expect(url.searchParams.get('ticket')).toBe('ticket-1');
  });

  it('collects events newest first and reports the connection', async () => {
    const { result } = renderHook(() => useDockerEvents());
    await waitFor(() => expect(FakeSource.instances).toHaveLength(1));

    act(() => latest().fire('open'));
    act(() => latest().emit('docker', started));
    act(() => latest().emit('docker', died));

    await waitFor(() => expect(result.current.events).toHaveLength(2));
    expect(result.current.events[0]?.action).toBe('die');
    expect(result.current.events[1]?.action).toBe('start');
    expect(result.current.connected).toBe(true);
  });

  // The feed is a tail, not a log: a host churning through events must not
  // grow the page without bound.
  it('keeps only the most recent events', async () => {
    const { result } = renderHook(() => useDockerEvents(3));
    await waitFor(() => expect(FakeSource.instances).toHaveLength(1));

    for (let i = 0; i < 6; i += 1) {
      act(() => latest().emit('docker', { ...started, name: `web-${i}` }));
    }

    await waitFor(() => expect(result.current.events).toHaveLength(3));
    expect(result.current.events[0]?.name).toBe('web-5');
  });

  it('gives every event a distinct key, however identical', async () => {
    const { result } = renderHook(() => useDockerEvents());
    await waitFor(() => expect(FakeSource.instances).toHaveLength(1));

    act(() => latest().emit('docker', started));
    act(() => latest().emit('docker', started));

    await waitFor(() => expect(result.current.events).toHaveLength(2));
    expect(result.current.events[0]?.key).not.toBe(result.current.events[1]?.key);
  });

  it('tells the caller so it can refetch what the event changed', async () => {
    const onEvent = vi.fn();
    renderHook(() => useDockerEvents(50, onEvent));
    await waitFor(() => expect(FakeSource.instances).toHaveLength(1));

    act(() => latest().emit('docker', started));

    await waitFor(() => expect(onEvent).toHaveBeenCalledTimes(1));
  });

  it('ignores a frame it cannot parse', async () => {
    const { result } = renderHook(() => useDockerEvents());
    await waitFor(() => expect(FakeSource.instances).toHaveLength(1));

    act(() => {
      for (const handler of latest().listeners.get('docker') ?? []) {
        handler({ data: 'not json' } as MessageEvent<string>);
      }
    });

    expect(result.current.events).toHaveLength(0);
  });

  // A ticket dies on first use, so the browser's own reconnect would arrive
  // unauthenticated: the hook has to reconnect with a fresh one.
  it('reconnects with a new ticket after the stream drops', async () => {
    vi.useFakeTimers();
    const { result } = renderHook(() => useDockerEvents());

    await vi.waitFor(() => expect(FakeSource.instances).toHaveLength(1));
    act(() => latest().fire('open'));
    expect(result.current.connected).toBe(true);

    const first = latest();
    act(() => first.fire('error'));

    expect(first.closed).toBe(true);
    expect(result.current.connected).toBe(false);

    await act(async () => {
      await vi.advanceTimersByTimeAsync(1_000);
    });

    expect(FakeSource.instances).toHaveLength(2);
    expect(fetchStreamTicket).toHaveBeenCalledTimes(2);
  });

  it('closes the stream when the page goes away', async () => {
    const { unmount } = renderHook(() => useDockerEvents());
    await waitFor(() => expect(FakeSource.instances).toHaveLength(1));

    const source = latest();
    unmount();

    expect(source.closed).toBe(true);
  });

  // A ticket the server refuses must not spin: the hook backs off and tries
  // again rather than opening a stream it cannot authenticate.
  it('retries when the ticket cannot be fetched', async () => {
    vi.useFakeTimers();
    fetchStreamTicket.mockRejectedValueOnce(new Error('unauthorized'));

    renderHook(() => useDockerEvents());

    await vi.waitFor(() => expect(fetchStreamTicket).toHaveBeenCalledTimes(1));
    expect(FakeSource.instances).toHaveLength(0);

    await act(async () => {
      await vi.advanceTimersByTimeAsync(1_000);
    });

    expect(FakeSource.instances).toHaveLength(1);
  });
});
