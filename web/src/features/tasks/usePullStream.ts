import { useCallback, useRef, useState } from 'react';

import { eventSourceURL, fetchStreamTicket } from '../../api/client';
import { tasks as tasksApi } from '../../api/endpoints';
import type { PullProgress } from '../../api/types';

export interface PullState {
  /** Whether a pull is in flight. */
  active: boolean;
  /** 0..100, or -1 while no layer has reported a size. */
  percent: number;
  /** The engine's most recent status line. */
  status: string;
  error: string | null;
  /** Set once the server announces the task, so the pull can be canceled. */
  taskID: string | null;
}

const idle: PullState = { active: false, percent: -1, status: '', error: null, taskID: null };

/**
 * Runs an image pull over SSE and reports its progress.
 *
 * The pull is a server-side task, so closing the page does not abandon it: the
 * task drawer can still show and cancel it. This hook is the view of one pull
 * from the screen that started it.
 */
export function usePullStream(onFinished?: () => void) {
  const [state, setState] = useState<PullState>(idle);
  const sourceRef = useRef<EventSource | null>(null);
  const taskRef = useRef<string | null>(null);

  const close = useCallback(() => {
    sourceRef.current?.close();
    sourceRef.current = null;
  }, []);

  const start = useCallback(
    async (ref: string) => {
      close();
      taskRef.current = null;
      setState({ ...idle, active: true, status: ref });

      let ticket: string;
      try {
        ticket = await fetchStreamTicket();
      } catch (err) {
        setState({ ...idle, error: err instanceof Error ? err.message : String(err) });
        return;
      }

      const source = new EventSource(eventSourceURL('/images/pull', { ref, ticket }));
      sourceRef.current = source;

      source.addEventListener('task', (event) => {
        try {
          const { id } = JSON.parse((event as MessageEvent<string>).data) as { id: string };
          taskRef.current = id;
          setState((current) => ({ ...current, taskID: id }));
        } catch {
          // Without the id the pull still runs; only cancelling is lost.
        }
      });

      source.addEventListener('progress', (event) => {
        try {
          const line = JSON.parse((event as MessageEvent<string>).data) as PullProgress;
          setState((current) => ({
            ...current,
            percent: line.percent,
            status: line.status || current.status,
          }));
        } catch {
          // A line we cannot parse is not worth failing the pull over.
        }
      });

      source.addEventListener('error', (event) => {
        // Two different things arrive here: the server's own `error` event,
        // which carries a message, and the browser's transport error, which
        // does not.
        let message = 'the pull failed';
        const data = (event as MessageEvent<string>).data;
        if (data) {
          try {
            message = (JSON.parse(data) as { message?: string }).message ?? message;
          } catch {
            message = data;
          }
        }
        close();
        setState((current) => ({ ...current, active: false, error: message }));
      });

      source.addEventListener('done', () => {
        close();
        setState((current) => ({ ...current, active: false, percent: 100 }));
        onFinished?.();
      });
    },
    [close, onFinished],
  );

  const cancel = useCallback(async () => {
    const id = taskRef.current;
    close();
    setState((current) => ({ ...current, active: false }));
    if (id) {
      try {
        await tasksApi.cancel(id);
      } catch {
        // The pull is already stopping from this end; a failed cancel only
        // means the server finished first.
      }
    }
  }, [close]);

  const reset = useCallback(() => {
    close();
    setState(idle);
  }, [close]);

  return { state, start, cancel, reset };
}
