import { useCallback, useEffect, useRef, useState } from 'react';

import { fetchStreamTicket, websocketURL } from '../../api/client';
import type { LogFrame } from '../../api/types';

export interface LogEntry {
  id: number;
  stream: 'stdout' | 'stderr';
  timestamp?: string;
  message: string;
}

export type StreamState = 'connecting' | 'open' | 'closed' | 'error';

interface Options {
  containerID: string;
  tail: number;
  timestamps: boolean;
  follow: boolean;
  enabled: boolean;
  /** How many lines to keep. Older ones are dropped. */
  bufferSize?: number;
}

const DEFAULT_BUFFER = 5000;
const MAX_BACKOFF_MS = 30_000;

/**
 * Streams a container's logs over a WebSocket, reconnecting with backoff.
 *
 * Lines are kept in a bounded ring: a container that prints continuously would
 * otherwise grow the tab's memory without limit.
 */
export function useLogStream({
  containerID,
  tail,
  timestamps,
  follow,
  enabled,
  bufferSize = DEFAULT_BUFFER,
}: Options) {
  const [entries, setEntries] = useState<LogEntry[]>([]);
  const [state, setState] = useState<StreamState>('closed');
  const [error, setError] = useState<string | null>(null);

  const socketRef = useRef<WebSocket | null>(null);
  const nextID = useRef(0);
  const attempt = useRef(0);
  const retryTimer = useRef<ReturnType<typeof setTimeout> | null>(null);
  const cancelled = useRef(false);

  const clear = useCallback(() => {
    setEntries([]);
    nextID.current = 0;
  }, []);

  useEffect(() => {
    cancelled.current = false;
    if (!enabled || !containerID) {
      setState('closed');
      return;
    }

    async function connect() {
      setState('connecting');
      setError(null);

      let socket: WebSocket;
      try {
        const ticket = await fetchStreamTicket();
        if (cancelled.current) return;

        socket = new WebSocket(
          websocketURL(`/containers/${encodeURIComponent(containerID)}/logs`, {
            ticket,
            tail: String(tail),
            timestamps: String(timestamps),
            follow: String(follow),
          }),
        );
      } catch (err) {
        if (cancelled.current) return;
        setState('error');
        setError(err instanceof Error ? err.message : String(err));
        scheduleRetry();
        return;
      }

      socketRef.current = socket;

      socket.onopen = () => {
        attempt.current = 0;
        setState('open');
      };

      socket.onmessage = (event) => {
        let frame: LogFrame;
        try {
          frame = JSON.parse(event.data as string) as LogFrame;
        } catch {
          return;
        }

        if (frame.t === 'log') {
          setEntries((current) => {
            const entry: LogEntry = {
              id: nextID.current++,
              stream: frame.s ?? 'stdout',
              timestamp: frame.ts,
              message: frame.m ?? '',
            };
            const next = current.length >= bufferSize ? current.slice(1) : current.slice();
            next.push(entry);
            return next;
          });
          return;
        }

        if (frame.t === 'err') {
          setError(frame.m ?? 'stream failed');
          setState('error');
          return;
        }

        if (frame.t === 'eof') {
          setState('closed');
        }
      };

      socket.onerror = () => {
        if (!cancelled.current) setState('error');
      };

      socket.onclose = () => {
        socketRef.current = null;
        if (cancelled.current) return;
        // Only a following stream is meant to stay up; a backlog request that
        // ends has simply finished.
        if (follow) {
          setState('connecting');
          scheduleRetry();
        } else {
          setState('closed');
        }
      };
    }

    function scheduleRetry() {
      if (cancelled.current) return;
      const delay = Math.min(1000 * 2 ** attempt.current, MAX_BACKOFF_MS);
      attempt.current += 1;
      retryTimer.current = setTimeout(() => void connect(), delay);
    }

    void connect();

    return () => {
      cancelled.current = true;
      if (retryTimer.current) clearTimeout(retryTimer.current);
      socketRef.current?.close();
      socketRef.current = null;
    };
  }, [containerID, tail, timestamps, follow, enabled, bufferSize]);

  return { entries, state, error, clear };
}
