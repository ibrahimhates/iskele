import { useCallback, useEffect, useRef, useState } from 'react';

import { fetchStreamTicket, websocketURL } from '../../api/client';
import { builds as buildsApi } from '../../api/endpoints';
import type { BuildFrame, BuildRequest } from '../../api/types';

export interface BuildLine {
  id: number;
  text: string;
}

export type BuildPhase = 'idle' | 'connecting' | 'running' | 'succeeded' | 'failed' | 'canceled';

export interface BuildStreamState {
  phase: BuildPhase;
  /** The build's id, known from the socket's first frame. */
  buildID: string | null;
  lines: BuildLine[];
  /** The Dockerfile instruction being run, and how many there are. */
  step: number;
  totalSteps: number;
  /** The engine's most recent base-image pull line. */
  status: string;
  /** What the build produced, on success. */
  imageID: string | null;
  error: string | null;
}

const idle: BuildStreamState = {
  phase: 'idle',
  buildID: null,
  lines: [],
  step: 0,
  totalSteps: 0,
  status: '',
  imageID: null,
  error: null,
};

/** How many output lines to keep. A build that prints continuously would
 * otherwise grow the tab's memory without limit. */
const MAX_LINES = 5000;

/**
 * Runs one image build over a WebSocket and reports its output.
 *
 * The build is server-side work with a record behind it, so closing this page
 * does not abandon it: the history and the archived log are how it is picked up
 * again, and cancelling goes through the build's own endpoint rather than
 * through the socket.
 */
export function useBuildStream(onFinished?: (imageID: string | null) => void) {
  const [state, setState] = useState<BuildStreamState>(idle);

  const socketRef = useRef<WebSocket | null>(null);
  const nextLineID = useRef(0);
  const buildRef = useRef<string | null>(null);
  const finishedRef = useRef(onFinished);

  useEffect(() => {
    finishedRef.current = onFinished;
  }, [onFinished]);

  const closeSocket = useCallback(() => {
    const socket = socketRef.current;
    socketRef.current = null;
    if (socket) {
      socket.onmessage = null;
      socket.onclose = null;
      socket.onerror = null;
      socket.close();
    }
  }, []);

  useEffect(() => closeSocket, [closeSocket]);

  const append = useCallback((text: string) => {
    setState((current) => {
      // Engine output arrives with its own newlines; splitting here is what
      // keeps one frame carrying three lines from rendering as one.
      const parts = text.replace(/\n$/, '').split('\n');
      const added = parts.map((part) => ({ id: nextLineID.current++, text: part }));
      const lines = [...current.lines, ...added];
      return { ...current, lines: lines.slice(-MAX_LINES) };
    });
  }, []);

  const start = useCallback(
    async (request: BuildRequest) => {
      closeSocket();
      nextLineID.current = 0;
      buildRef.current = null;
      setState({ ...idle, phase: 'connecting' });

      let ticket: string;
      try {
        ticket = await fetchStreamTicket();
      } catch (err) {
        setState({
          ...idle,
          phase: 'failed',
          error: err instanceof Error ? err.message : String(err),
        });
        return;
      }

      const params: Record<string, string> = {
        ticket,
        context: request.context,
        nocache: String(request.noCache),
        pull: String(request.pull),
      };
      if (request.dockerfile) params.dockerfile = request.dockerfile;
      if (request.target) params.target = request.target;
      if (request.platform) params.platform = request.platform;
      if (Object.keys(request.buildArgs).length > 0) {
        params.buildargs = JSON.stringify(request.buildArgs);
      }
      if (Object.keys(request.labels).length > 0) {
        params.labels = JSON.stringify(request.labels);
      }

      const url = new URL(websocketURL('/build', params));
      // Tags repeat rather than joining: an image reference may contain a comma
      // in no place a split would survive.
      for (const tag of request.tags) url.searchParams.append('tag', tag);

      const socket = new WebSocket(url.toString());
      socketRef.current = socket;

      socket.onopen = () => setState((current) => ({ ...current, phase: 'running' }));

      socket.onmessage = (event: MessageEvent<string>) => {
        let frame: BuildFrame;
        try {
          frame = JSON.parse(event.data) as BuildFrame;
        } catch {
          return;
        }

        switch (frame.t) {
          case 'build':
            buildRef.current = frame.id ?? null;
            setState((current) => ({
              ...current,
              phase: 'running',
              buildID: frame.id ?? null,
            }));
            break;

          case 'log':
            if (frame.step) {
              setState((current) => ({
                ...current,
                step: frame.step ?? current.step,
                totalSteps: frame.total_steps ?? current.totalSteps,
              }));
            }
            if (frame.line) append(frame.line);
            break;

          case 'status':
            setState((current) => ({
              ...current,
              status: frame.status
                ? frame.layer_id
                  ? `${frame.layer_id}: ${frame.status}`
                  : frame.status
                : current.status,
              imageID: frame.image_id ?? current.imageID,
            }));
            break;

          case 'done':
            closeSocket();
            setState((current) => {
              const imageID = frame.image_id ?? current.imageID;
              return {
                ...current,
                phase: frame.status === 'canceled' ? 'canceled' : 'succeeded',
                imageID,
              };
            });
            finishedRef.current?.(frame.image_id ?? null);
            break;

          case 'err':
            closeSocket();
            setState((current) => ({
              ...current,
              phase: 'failed',
              error: frame.m ?? 'the build failed',
            }));
            finishedRef.current?.(null);
            break;
        }
      };

      socket.onclose = () => {
        socketRef.current = null;
        // A socket that closes without a terminal frame took the connection
        // with it, not the build: the record still says what happened.
        setState((current) =>
          current.phase === 'running' || current.phase === 'connecting'
            ? { ...current, phase: 'failed', error: 'the build stream closed unexpectedly' }
            : current,
        );
      };
    },
    [append, closeSocket],
  );

  const cancel = useCallback(async () => {
    const id = buildRef.current;
    if (!id) {
      closeSocket();
      setState((current) => ({ ...current, phase: 'canceled' }));
      return;
    }
    try {
      await buildsApi.cancel(id);
      setState((current) => ({ ...current, phase: 'canceled' }));
    } catch (err) {
      setState((current) => ({
        ...current,
        error: err instanceof Error ? err.message : String(err),
      }));
    }
  }, [closeSocket]);

  const reset = useCallback(() => {
    closeSocket();
    nextLineID.current = 0;
    buildRef.current = null;
    setState(idle);
  }, [closeSocket]);

  return { state, start, cancel, reset };
}
