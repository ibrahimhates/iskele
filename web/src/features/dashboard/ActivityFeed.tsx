import { Link } from 'react-router-dom';
import { useTranslation } from 'react-i18next';
import { Activity, Circle } from 'lucide-react';

import { cn } from '../../lib/cn';
import { formatRelative } from '../../lib/format';
import type { FeedEvent } from './useDockerEvents';

interface Props {
  events: FeedEvent[];
  connected: boolean;
}

/**
 * The engine's event stream, newest first.
 *
 * It shows what happened to this host, not what this panel did: a container
 * someone stopped over SSH appears here exactly like one stopped from the
 * containers page. That is the point — it is the host's activity, and the
 * audit log is where the panel's own actions are recorded.
 */
export function ActivityFeed({ events, connected }: Props) {
  const { t } = useTranslation();

  return (
    <div className="card flex h-full flex-col p-4">
      <div className="mb-3 flex items-center justify-between gap-2">
        <h2 className="flex items-center gap-2 text-sm font-medium">
          <Activity size={16} className="text-muted" aria-hidden />
          {t('dashboard.activity')}
        </h2>
        <span
          className={cn('flex items-center gap-1.5 text-xs', connected ? 'text-ok' : 'text-muted')}
        >
          <Circle size={8} className="fill-current" aria-hidden />
          {connected ? t('dashboard.live') : t('dashboard.reconnecting')}
        </span>
      </div>

      {events.length === 0 ? (
        <p className="py-6 text-center text-sm text-muted">{t('dashboard.no_activity')}</p>
      ) : (
        <ul className="-mx-1 max-h-96 flex-1 space-y-0.5 overflow-y-auto">
          {events.map((event) => (
            <EventRow key={event.key} event={event} />
          ))}
        </ul>
      )}
    </div>
  );
}

function EventRow({ event }: { event: FeedEvent }) {
  const { t } = useTranslation();
  const subject = event.name || event.actor;

  return (
    <li className="flex items-baseline gap-2 rounded px-1 py-1 text-sm hover:bg-elevated">
      <span className={cn('shrink-0 text-xs font-medium', actionColor(event.action))}>
        {event.action}
      </span>
      <span className="badge shrink-0 bg-elevated text-[10px] text-muted">{event.type}</span>
      <span className="min-w-0 flex-1 truncate font-mono text-xs" title={subject}>
        {linkFor(event) ? (
          <Link className="hover:text-accent" to={linkFor(event) as string}>
            {subject}
          </Link>
        ) : (
          subject
        )}
      </span>
      <time className="shrink-0 text-xs tabular-nums text-muted" dateTime={event.time}>
        {formatRelative(event.time)}
      </time>
      <span className="sr-only">{t('dashboard.activity')}</span>
    </li>
  );
}

/**
 * Where an event's subject lives in the UI, when it has a page at all.
 *
 * Only containers do: an image event names a tag that may already be gone, and
 * a network event's actor is an ID the networks page does not route by.
 */
function linkFor(event: FeedEvent): string | null {
  if (event.type !== 'container' || !event.actor) return null;
  // A removed container has no page left to visit.
  if (event.action === 'destroy' || event.action === 'remove') return null;
  return `/containers/${encodeURIComponent(event.actor)}`;
}

/** The engine's actions, coloured by what they mean for the host. */
function actionColor(action: string): string {
  switch (action) {
    case 'start':
    case 'create':
    case 'pull':
    case 'health_status: healthy':
      return 'text-ok';
    case 'die':
    case 'destroy':
    case 'kill':
    case 'oom':
    case 'health_status: unhealthy':
      return 'text-danger';
    case 'stop':
    case 'pause':
    case 'restart':
      return 'text-warn';
    default:
      return 'text-muted';
  }
}
