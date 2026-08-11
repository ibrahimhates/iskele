import { useEffect, useRef, useState } from 'react';

import { eventSourceURL, fetchStreamTicket } from '../../api/client';
import type { DockerEvent } from '../../api/types';

const MAX_BACKOFF_MS = 30_000;

/** One event, with a key of our own: the engine does not give events an id. */
export interface FeedEvent extends DockerEvent {
  key: string;
}

export interface EventFeed {
  events: FeedEvent[];
  /** False while reconnecting, so the feed can say why it is not moving. */
  connected: boolean;
}

/**
 * The engine's event stream, kept as a bounded list of the most recent events.
 *
 * This is the dashboard's activity feed: every container that starts, dies or
 * is removed, every image that is pulled, by whatever means — the panel, a
 * `docker` command on the host, a container restarting itself. It is also what
 * tells the dashboard that its counts are stale.
 *
 * @param limit how many events to keep; older ones are dropped.
 * @param onEvent called for each event, for callers that want to refetch.
 */
export function useDockerEvents(limit = 50, onEvent?: () => void): EventFeed {
  const [events, setEvents] = useState<FeedEvent[]>([]);
  const [connected, setConnected] = useState(false);
  const attempt = useRef(0);
  const counter = useRef(0);
  // Held in a ref so a caller passing an inline function does not tear the
  // stream down and reconnect on every render.
  const notify = useRef(onEvent);
  notify.current = onEvent;

  useEffect(() => {
    let cancelled = false;
    let source: EventSource | null = null;
    let retry: ReturnType<typeof setTimeout> | null = null;

    async function connect() {
      let ticket: string;
      try {
        ticket = await fetchStreamTicket();
      } catch {
        scheduleRetry();
        return;
      }
      if (cancelled) return;

      source = new EventSource(eventSourceURL('/system/events', { ticket }));

      source.addEventListener('open', () => {
        attempt.current = 0;
        setConnected(true);
      });

      source.addEventListener('docker', (message) => {
        let event: DockerEvent;
        try {
          event = JSON.parse((message as MessageEvent<string>).data) as DockerEvent;
        } catch {
          return;
        }
        counter.current += 1;
        const keyed: FeedEvent = { ...event, key: `${counter.current}` };
        // Newest first, bounded: this panel is a tail, not a log.
        setEvents((current) => [keyed, ...current].slice(0, limit));
        notify.current?.();
      });

      // A ticket is single-use, so the browser's own reconnect would arrive
      // unauthenticated. Reconnecting is ours to do, with a fresh ticket.
      source.addEventListener('error', () => {
        source?.close();
        source = null;
        setConnected(false);
        if (!cancelled) scheduleRetry();
      });
    }

    function scheduleRetry() {
      if (cancelled) return;
      const delay = Math.min(1000 * 2 ** attempt.current, MAX_BACKOFF_MS);
      attempt.current += 1;
      retry = setTimeout(() => void connect(), delay);
    }

    void connect();

    return () => {
      cancelled = true;
      if (retry) clearTimeout(retry);
      source?.close();
    };
  }, [limit]);

  return { events, connected };
}
