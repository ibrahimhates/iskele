import { useCallback, useEffect, useRef, useState } from 'react';

import { fetchStreamTicket, websocketURL } from '../../api/client';
import type { StackLogFrame } from '../../api/types';

export interface StackLogEntry {
  id: number;
  service: string;
  container: string;
  stream: 'stdout' | 'stderr';
  timestamp?: string;
  message: string;
}

export type LogState = 'connecting' | 'open' | 'closed' | 'error';

interface Options {
  stackID: string;
  /** Empty reads the whole stack. */
  services: string[];
  tail: number;
  timestamps: boolean;
  enabled: boolean;
}

/** How many lines to keep. A stack that prints continuously would otherwise
 * grow the tab's memory without limit. */
const BUFFER = 5000;

/**
 * Streams every container in a stack over one socket.
 *
 * One connection rather than one per service: the server interleaves the lines
 * and tags each with where it came from, which is the only way to read a deploy
 * without the browser having to merge streams itself.
 */
export function useStackLogs({ stackID, services, tail, timestamps, enabled }: Options) {
  const [entries, setEntries] = useState<StackLogEntry[]>([]);
  const [state, setState] = useState<LogState>('closed');
  const [error, setError] = useState<string | null>(null);

  const socketRef = useRef<WebSocket | null>(null);
  const nextID = useRef(0);
  const cancelled = useRef(false);

  const clear = useCallback(() => {
    setEntries([]);
    nextID.current = 0;
  }, []);

  // The service list is an array; joining it keeps the effect from re-running
  // on every render just because a new array was built.
  const serviceKey = services.join(',');

  useEffect(() => {
    cancelled.current = false;
    if (!enabled || !stackID) {
      setState('closed');
      return;
    }

    let socket: WebSocket | null = null;

    async function connect() {
      setState('connecting');
      setError(null);

      let ticket: string;
      try {
        ticket = await fetchStreamTicket();
      } catch (err) {
        if (cancelled.current) return;
        setState('error');
        setError(err instanceof Error ? err.message : String(err));
        return;
      }
      if (cancelled.current) return;

      const url = new URL(
        websocketURL(`/stacks/${encodeURIComponent(stackID)}/logs`, {
          ticket,
          tail: String(tail),
          timestamps: String(timestamps),
          follow: 'true',
        }),
      );
      for (const service of serviceKey ? serviceKey.split(',') : []) {
        url.searchParams.append('service', service);
      }

      socket = new WebSocket(url.toString());
      socketRef.current = socket;

      socket.onopen = () => setState('open');

      socket.onmessage = (event: MessageEvent<string>) => {
        let frame: StackLogFrame;
        try {
          frame = JSON.parse(event.data) as StackLogFrame;
        } catch {
          return;
        }

        if (frame.t === 'err') {
          setState('error');
          setError(frame.m ?? 'the log stream failed');
          return;
        }
        if (frame.t === 'eof') {
          setState('closed');
          return;
        }

        setEntries((current) => {
          const next = [
            ...current,
            {
              id: nextID.current++,
              service: frame.service ?? '',
              container: frame.container ?? '',
              stream: frame.s ?? 'stdout',
              timestamp: frame.ts,
              message: frame.m ?? '',
            },
          ];
          return next.slice(-BUFFER);
        });
      };

      socket.onclose = () => {
        socketRef.current = null;
        setState((current) => (current === 'error' ? current : 'closed'));
      };
    }

    void connect();

    return () => {
      cancelled.current = true;
      socket?.close();
      socketRef.current = null;
    };
  }, [stackID, serviceKey, tail, timestamps, enabled]);

  return { entries, state, error, clear };
}
