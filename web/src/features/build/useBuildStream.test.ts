import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { act, renderHook, waitFor } from '@testing-library/react';

import type { BuildRequest } from '../../api/types';
import { useBuildStream } from './useBuildStream';

const cancelBuild = vi.fn(async (id: string) => ({ id }));

vi.mock('../../api/client', () => ({
  fetchStreamTicket: vi.fn(async () => 'ticket-1'),
  websocketURL: (path: string, params: Record<string, string>) =>
    `ws://localhost${path}?${new URLSearchParams(params).toString()}`,
}));

vi.mock('../../api/endpoints', () => ({
  builds: { cancel: (id: string) => cancelBuild(id) },
}));

/** The minimum of the WebSocket surface the hook touches. */
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

  emit(frame: unknown) {
    this.onmessage?.({ data: JSON.stringify(frame) });
  }
}

function latest(): FakeSocket {
  const socket = FakeSocket.instances.at(-1);
  if (!socket) throw new Error('no socket was opened');
  return socket;
}

const request: BuildRequest = {
  context: '/srv/app',
  dockerfile: 'docker/Dockerfile',
  tags: ['app:latest', 'app:1.2.3'],
  buildArgs: { VERSION: '1.2.3' },
  labels: {},
  target: 'runtime',
  platform: '',
  noCache: true,
  pull: false,
};

describe('useBuildStream', () => {
  beforeEach(() => {
    FakeSocket.instances = [];
    cancelBuild.mockClear();
    vi.stubGlobal('WebSocket', FakeSocket);
  });

  afterEach(() => {
    vi.unstubAllGlobals();
  });

  it('carries the build parameters in the URL', async () => {
    const { result } = renderHook(() => useBuildStream());

    await act(async () => {
      await result.current.start(request);
    });

    const url = new URL(latest().url);
    expect(url.pathname).toBe('/build');
    expect(url.searchParams.get('ticket')).toBe('ticket-1');
    expect(url.searchParams.get('context')).toBe('/srv/app');
    expect(url.searchParams.get('dockerfile')).toBe('docker/Dockerfile');
    expect(url.searchParams.get('target')).toBe('runtime');
    expect(url.searchParams.get('nocache')).toBe('true');
    expect(url.searchParams.get('pull')).toBe('false');
    expect(url.searchParams.get('buildargs')).toBe('{"VERSION":"1.2.3"}');
    // Tags repeat rather than joining, so a reference containing a comma
    // cannot be split back into two.
    expect(url.searchParams.getAll('tag')).toEqual(['app:latest', 'app:1.2.3']);
    // An empty platform is left out rather than sent blank.
    expect(url.searchParams.has('platform')).toBe(false);
    expect(url.searchParams.has('labels')).toBe(false);
  });

  it('splits a frame carrying several lines', async () => {
    const { result } = renderHook(() => useBuildStream());
    await act(async () => {
      await result.current.start(request);
    });

    act(() => latest().emit({ t: 'log', line: 'one\ntwo\n', step: 2, total_steps: 5 }));

    await waitFor(() => expect(result.current.state.lines).toHaveLength(2));
    expect(result.current.state.lines.map((line) => line.text)).toEqual(['one', 'two']);
    expect(result.current.state.step).toBe(2);
    expect(result.current.state.totalSteps).toBe(5);
  });

  it('reports the image a finished build produced', async () => {
    const finished = vi.fn();
    const { result } = renderHook(() => useBuildStream(finished));
    await act(async () => {
      await result.current.start(request);
    });

    act(() => latest().emit({ t: 'build', id: 'build-1' }));
    act(() =>
      latest().emit({ t: 'done', id: 'build-1', image_id: 'sha256:abc', status: 'success' }),
    );

    await waitFor(() => expect(result.current.state.phase).toBe('succeeded'));
    expect(result.current.state.imageID).toBe('sha256:abc');
    expect(finished).toHaveBeenCalledWith('sha256:abc');
  });

  it('reports a build the engine refused', async () => {
    const { result } = renderHook(() => useBuildStream());
    await act(async () => {
      await result.current.start(request);
    });

    act(() => latest().emit({ t: 'err', code: 'DOCKER_ERROR', m: 'COPY failed' }));

    await waitFor(() => expect(result.current.state.phase).toBe('failed'));
    expect(result.current.state.error).toBe('COPY failed');
  });

  it('treats a canceled build as canceled rather than as a success', async () => {
    const { result } = renderHook(() => useBuildStream());
    await act(async () => {
      await result.current.start(request);
    });

    act(() => latest().emit({ t: 'done', id: 'build-1', status: 'canceled' }));

    await waitFor(() => expect(result.current.state.phase).toBe('canceled'));
  });

  it('cancels through the build endpoint, not the socket', async () => {
    const { result } = renderHook(() => useBuildStream());
    await act(async () => {
      await result.current.start(request);
    });

    act(() => latest().emit({ t: 'build', id: 'build-1' }));
    await waitFor(() => expect(result.current.state.buildID).toBe('build-1'));

    await act(async () => {
      await result.current.cancel();
    });

    // Closing the socket would only stop the frames; the build has to be told.
    expect(cancelBuild).toHaveBeenCalledWith('build-1');
    expect(result.current.state.phase).toBe('canceled');
  });

  it('does not report a failure when the socket closes after the build ended', async () => {
    const { result } = renderHook(() => useBuildStream());
    await act(async () => {
      await result.current.start(request);
    });

    const socket = latest();
    act(() => socket.emit({ t: 'done', id: 'build-1', status: 'success' }));
    act(() => socket.onclose?.());

    expect(result.current.state.phase).toBe('succeeded');
    expect(result.current.state.error).toBeNull();
  });

  it('reports a socket that closes mid-build', async () => {
    const { result } = renderHook(() => useBuildStream());
    await act(async () => {
      await result.current.start(request);
    });

    act(() => latest().onclose?.());

    await waitFor(() => expect(result.current.state.phase).toBe('failed'));
    expect(result.current.state.error).toContain('closed');
  });
});
