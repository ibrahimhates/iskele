import { useEffect, useRef, useState } from 'react';

import { eventSourceURL, fetchStreamTicket } from '../../api/client';
import type { Stats } from '../../api/types';

/** One sample, tagged with the container it belongs to. */
interface IdentifiedStats extends Stats {
  id: string;
}

const MAX_BACKOFF_MS = 30_000;

/**
 * Live CPU and memory for every running container, over one connection.
 *
 * A stream per row would stall the list at six containers — that is the
 * browser's per-origin connection limit — so the daemon fans all of them into
 * a single event stream and this hook demultiplexes it back into a map.
 */
export function useAllStats(enabled: boolean): Map<string, Stats> {
  const [samples, setSamples] = useState<Map<string, Stats>>(new Map());
  const attempt = useRef(0);

  useEffect(() => {
    if (!enabled) {
      setSamples(new Map());
      return;
    }

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

      source = new EventSource(eventSourceURL('/containers/stats', { ticket }));

      source.addEventListener('open', () => {
        attempt.current = 0;
      });

      source.addEventListener('stats', (event) => {
        let sample: IdentifiedStats;
        try {
          sample = JSON.parse((event as MessageEvent<string>).data) as IdentifiedStats;
        } catch {
          return;
        }
        setSamples((current) => {
          const next = new Map(current);
          next.set(sample.id, sample);
          return next;
        });
      });

      // The daemon closes the stream on a fatal engine error; the browser
      // would otherwise reconnect on its own schedule, ignoring ours.
      source.addEventListener('error', () => {
        source?.close();
        source = null;
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
  }, [enabled]);

  return samples;
}
