import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { act, renderHook, waitFor } from '@testing-library/react';

import { useLogStream } from './useLogStream';

vi.mock('../../api/client', () => ({
  fetchStreamTicket: vi.fn(async () => 'ticket-1'),
  websocketURL: (path: string, params: Record<string, string>) =>
    `ws://localhost${path}?${new URLSearchParams(params).toString()}`,
}));

/**
 * The minimum of the WebSocket surface the hook touches, plus a handle on the
 * instances so a test can push frames in.
 */
class FakeSocket {
  static instances: FakeSocket[] = [];

  onopen: (() => void) | null = null;
  onmessage: ((event: { data: string }) => void) | null = null;
  onerror: (() => void) | null = null;
  onclose: (() => void) | null = null;
  closed = false;

  constructor(readonly url: string) {
    FakeSocket.instances.push(this);
  }

  close() {
    this.closed = true;
  }

  /** Delivers one server frame. */
  emit(frame: unknown) {
    this.onmessage?.({ data: JSON.stringify(frame) });
  }
}

function latest(): FakeSocket {
  const socket = FakeSocket.instances.at(-1);
  if (!socket) throw new Error('no socket was opened');
  return socket;
}

const options = {
  containerID: 'abc',
  tail: 100,
  timestamps: false,
  follow: true,
  enabled: true,
};

describe('useLogStream', () => {
  beforeEach(() => {
    FakeSocket.instances = [];
    vi.stubGlobal('WebSocket', FakeSocket);
  });

  afterEach(() => {
    vi.unstubAllGlobals();
    vi.useRealTimers();
  });

  it('carries the ticket and the log options in the URL', async () => {
    renderHook(() => useLogStream(options));

    await waitFor(() => expect(FakeSocket.instances).toHaveLength(1));

    const url = new URL(latest().url);
    expect(url.pathname).toBe('/containers/abc/logs');
    expect(url.searchParams.get('ticket')).toBe('ticket-1');
    expect(url.searchParams.get('tail')).toBe('100');
    expect(url.searchParams.get('follow')).toBe('true');
  });

  it('collects log frames and ignores the ones it cannot parse', async () => {
    const { result } = renderHook(() => useLogStream(options));
    await waitFor(() => expect(FakeSocket.instances).toHaveLength(1));

    act(() => {
      latest().onopen?.();
      latest().emit({ t: 'log', s: 'stdout', m: 'first' });
      latest().emit({ t: 'log', s: 'stderr', m: 'second' });
      latest().onmessage?.({ data: 'not json' });
    });

    expect(result.current.state).toBe('open');
    expect(result.current.entries.map((e) => e.message)).toEqual(['first', 'second']);
    expect(result.current.entries[1]!.stream).toBe('stderr');
  });

  // A container printing without pause would otherwise grow the tab's memory
  // until it is killed.
  it('drops the oldest lines once the buffer is full', async () => {
    const { result } = renderHook(() => useLogStream({ ...options, bufferSize: 3 }));
    await waitFor(() => expect(FakeSocket.instances).toHaveLength(1));

    act(() => {
      for (const message of ['1', '2', '3', '4', '5']) {
        latest().emit({ t: 'log', m: message });
      }
    });

    expect(result.current.entries.map((e) => e.message)).toEqual(['3', '4', '5']);
  });

  it('surfaces a stream error without clearing what was already received', async () => {
    const { result } = renderHook(() => useLogStream(options));
    await waitFor(() => expect(FakeSocket.instances).toHaveLength(1));

    act(() => {
      latest().emit({ t: 'log', m: 'before the failure' });
      latest().emit({ t: 'err', code: 'DOCKER_UNAVAILABLE', m: 'daemon is gone' });
    });

    expect(result.current.state).toBe('error');
    expect(result.current.error).toBe('daemon is gone');
    expect(result.current.entries).toHaveLength(1);
  });

  it('ends quietly when the container stream reaches its end', async () => {
    const { result } = renderHook(() => useLogStream(options));
    await waitFor(() => expect(FakeSocket.instances).toHaveLength(1));

    act(() => {
      latest().emit({ t: 'eof' });
    });

    expect(result.current.state).toBe('closed');
    expect(result.current.error).toBeNull();
  });

  it('reconnects a following stream after the socket drops', async () => {
    vi.useFakeTimers();
    renderHook(() => useLogStream(options));

    // The ticket fetch is a promise, so the first socket appears a microtask
    // later; advancing timers by zero flushes it.
    await vi.advanceTimersByTimeAsync(0);
    expect(FakeSocket.instances).toHaveLength(1);

    act(() => {
      latest().onclose?.();
    });

    await vi.advanceTimersByTimeAsync(1000);
    expect(FakeSocket.instances).toHaveLength(2);
  });

  // A backlog request that ends has finished, and reconnecting would replay it
  // forever.
  it('does not reconnect when the caller only asked for the backlog', async () => {
    vi.useFakeTimers();
    renderHook(() => useLogStream({ ...options, follow: false }));

    await vi.advanceTimersByTimeAsync(0);
    expect(FakeSocket.instances).toHaveLength(1);

    act(() => {
      latest().onclose?.();
    });

    await vi.advanceTimersByTimeAsync(60_000);
    expect(FakeSocket.instances).toHaveLength(1);
  });

  it('closes the socket when the component goes away', async () => {
    const { unmount } = renderHook(() => useLogStream(options));
    await waitFor(() => expect(FakeSocket.instances).toHaveLength(1));

    const socket = latest();
    unmount();

    expect(socket.closed).toBe(true);
  });

  it('opens no socket at all while disabled', async () => {
    const { result } = renderHook(() => useLogStream({ ...options, enabled: false }));

    await Promise.resolve();
    expect(FakeSocket.instances).toHaveLength(0);
    expect(result.current.state).toBe('closed');
  });
});
