import { useCallback, useEffect, useRef, useState } from 'react';

import { eventSourceURL, fetchStreamTicket } from '../../api/client';
import type { StackEvent, StackProblem } from '../../api/types';

export interface StackStreamLine {
  id: number;
  kind: StackEvent['kind'] | 'error';
  service?: string;
  message: string;
}

export type StackPhase = 'idle' | 'running' | 'succeeded' | 'failed';

export interface StackStreamState {
  phase: StackPhase;
  lines: StackStreamLine[];
  error: string | null;
  /** Why a refused deploy was refused, per service. */
  problems: StackProblem[];
}

const idle: StackStreamState = { phase: 'idle', lines: [], error: null, problems: [] };

/** How many lines to keep. A deploy that builds an image can print thousands. */
const MAX_LINES = 3000;

/**
 * Runs one stack operation over SSE and reports its progress.
 *
 * The work is server-side and outlives this page: closing the tab stops the
 * events, not the deploy. The stack's status is how it is picked up again.
 */
export function useStackStream(onFinished?: () => void) {
  const [state, setState] = useState<StackStreamState>(idle);

  const sourceRef = useRef<EventSource | null>(null);
  const nextID = useRef(0);
  const finishedRef = useRef(onFinished);

  useEffect(() => {
    finishedRef.current = onFinished;
  }, [onFinished]);

  const close = useCallback(() => {
    sourceRef.current?.close();
    sourceRef.current = null;
  }, []);

  useEffect(() => close, [close]);

  const append = useCallback((line: Omit<StackStreamLine, 'id'>) => {
    setState((current) => {
      const lines = [...current.lines, { ...line, id: nextID.current++ }];
      return { ...current, lines: lines.slice(-MAX_LINES) };
    });
  }, []);

  const start = useCallback(
    async (path: string, params: Record<string, string> = {}) => {
      close();
      nextID.current = 0;
      setState({ ...idle, phase: 'running' });

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

      const source = new EventSource(eventSourceURL(path, { ...params, ticket }));
      sourceRef.current = source;

      const onProgress = (kind: StackEvent['kind']) => (event: Event) => {
        const data = (event as MessageEvent<string>).data;
        try {
          const parsed = JSON.parse(data) as StackEvent;
          append({ kind, service: parsed.service, message: parsed.message });
        } catch {
          // A line we cannot parse is not worth failing the deploy over.
        }
      };

      source.addEventListener('step', onProgress('step'));
      source.addEventListener('log', onProgress('log'));
      source.addEventListener('warn', onProgress('warn'));

      source.addEventListener('done', (event) => {
        onProgress('done')(event);
        close();
        setState((current) => ({ ...current, phase: 'succeeded' }));
        finishedRef.current?.();
      });

      source.addEventListener('error', (event) => {
        // Two different things arrive here: the server's own `error` event,
        // which carries a message and the per-service problems, and the
        // browser's transport error, which carries nothing.
        const data = (event as MessageEvent<string>).data;
        let message = 'the operation failed';
        let problems: StackProblem[] = [];

        if (data) {
          try {
            const parsed = JSON.parse(data) as { message?: string; problems?: StackProblem[] };
            message = parsed.message ?? message;
            problems = parsed.problems ?? [];
          } catch {
            message = data;
          }
        }

        close();
        setState((current) => ({ ...current, phase: 'failed', error: message, problems }));
        finishedRef.current?.();
      });
    },
    [append, close],
  );

  const reset = useCallback(() => {
    close();
    nextID.current = 0;
    setState(idle);
  }, [close]);

  return { state, start, reset };
}
