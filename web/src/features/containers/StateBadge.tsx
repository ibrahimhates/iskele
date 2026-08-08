import { useTranslation } from 'react-i18next';

import { cn } from '../../lib/cn';

const STATE_STYLES: Record<string, string> = {
  running: 'bg-ok/15 text-ok',
  paused: 'bg-warn/15 text-warn',
  restarting: 'bg-warn/15 text-warn',
  exited: 'bg-muted/15 text-muted',
  created: 'bg-muted/15 text-muted',
  removing: 'bg-danger/15 text-danger',
  dead: 'bg-danger/15 text-danger',
};

export function StateBadge({ state, health }: { state: string; health?: string }) {
  const { t } = useTranslation();
  const label = t(`containers.state.${state}`, { defaultValue: state });

  return (
    <span className="inline-flex items-center gap-1.5">
      <span className={cn('badge', STATE_STYLES[state] ?? 'bg-muted/15 text-muted')}>{label}</span>
      {health && <HealthDot health={health} />}
    </span>
  );
}

function HealthDot({ health }: { health: string }) {
  const { t } = useTranslation();
  const label = t(`containers.health.${health}`, { defaultValue: health });

  const color =
    health === 'healthy' ? 'bg-ok' : health === 'unhealthy' ? 'bg-danger' : 'bg-warn animate-pulse';

  return (
    <span className="inline-flex items-center gap-1 text-xs text-muted" title={label}>
      <span className={cn('h-1.5 w-1.5 rounded-full', color)} aria-hidden />
      <span className="sr-only">{label}</span>
    </span>
  );
}
