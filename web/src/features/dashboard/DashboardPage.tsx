import { useCallback, useEffect, useRef } from 'react';
import { useQuery, useQueryClient } from '@tanstack/react-query';
import { useTranslation } from 'react-i18next';
import { Boxes, Database, HardDrive, Network } from 'lucide-react';

import {
  containers as containersApi,
  images,
  networks,
  system,
  volumes,
} from '../../api/endpoints';
import { PageHeader } from '../../components/PageHeader';
import { StatCard } from '../../components/StatCard';
import { ErrorPanel } from '../../components/ErrorPanel';
import { Spinner } from '../../components/Spinner';
import { formatBytes, formatUptime } from '../../lib/format';
import { ActivityFeed } from './ActivityFeed';
import { Gauge } from './Gauge';
import { useDockerEvents } from './useDockerEvents';

/** How long to wait for the burst of events a `docker compose up` produces. */
const REFRESH_DEBOUNCE_MS = 1_000;

export function DashboardPage() {
  const { t } = useTranslation();
  const queryClient = useQueryClient();

  const containerQuery = useQuery({
    queryKey: ['containers', { all: true }],
    queryFn: () => containersApi.list({ all: true }),
    refetchInterval: 10_000,
  });
  const imageQuery = useQuery({ queryKey: ['images'], queryFn: () => images.list() });
  const volumeQuery = useQuery({ queryKey: ['volumes'], queryFn: volumes.list });
  const networkQuery = useQuery({ queryKey: ['networks'], queryFn: networks.list });
  const infoQuery = useQuery({ queryKey: ['system', 'info'], queryFn: system.info });
  const dfQuery = useQuery({ queryKey: ['system', 'df'], queryFn: system.diskUsage });
  const hostQuery = useQuery({
    queryKey: ['system', 'host'],
    queryFn: system.host,
    // The first reading has no CPU percentage to report, and every reading is
    // measured against the previous one, so this poll is what makes the CPU
    // gauge mean anything.
    refetchInterval: 5_000,
  });

  const refresh = useDebouncedRefresh(queryClient);
  const { events, connected } = useDockerEvents(50, refresh);

  const error =
    containerQuery.error ??
    imageQuery.error ??
    volumeQuery.error ??
    networkQuery.error ??
    infoQuery.error;

  if (error) {
    return (
      <>
        <PageHeader title={t('nav.dashboard')} />
        <ErrorPanel error={error} onRetry={() => void containerQuery.refetch()} />
      </>
    );
  }

  if (containerQuery.isLoading) {
    return (
      <div className="flex justify-center py-16">
        <Spinner />
      </div>
    );
  }

  const all = containerQuery.data?.items ?? [];
  const running = all.filter((c) => c.state === 'running').length;
  const unhealthy = all.filter((c) => c.health === 'unhealthy').length;
  const info = infoQuery.data;
  const df = dfQuery.data;
  const host = hostQuery.data;
  const unknown = t('dashboard.unknown');

  return (
    <>
      <PageHeader
        title={t('nav.dashboard')}
        description={info ? `Docker ${info.server_version} · API ${info.api_version}` : undefined}
      />

      <div className="grid gap-3 sm:grid-cols-2 lg:grid-cols-4">
        <StatCard
          label={t('nav.containers')}
          value={`${running} / ${all.length}`}
          hint={
            unhealthy > 0
              ? t('dashboard.unhealthy', { count: unhealthy })
              : t('dashboard.running_total')
          }
          icon={<Boxes size={16} aria-hidden />}
        />
        <StatCard
          label={t('nav.images')}
          value={imageQuery.data?.total ?? '—'}
          hint={df ? formatBytes(df.images.size) : undefined}
          icon={<HardDrive size={16} aria-hidden />}
        />
        <StatCard
          label={t('nav.volumes')}
          value={volumeQuery.data?.total ?? '—'}
          hint={df ? formatBytes(df.volumes.size) : undefined}
          icon={<Database size={16} aria-hidden />}
        />
        <StatCard
          label={t('nav.networks')}
          value={networkQuery.data?.total ?? '—'}
          icon={<Network size={16} aria-hidden />}
        />
      </div>

      {host && (
        <div className="mt-3 grid gap-3 sm:grid-cols-2 lg:grid-cols-4">
          <Gauge
            label={t('dashboard.cpu')}
            percent={host.cpu.percent}
            detail={t('dashboard.cores', { count: host.cpu.cores })}
            hint={host.cpu.model}
            unknownLabel={unknown}
          />
          <Gauge
            label={t('dashboard.memory')}
            percent={host.memory.percent}
            detail={`${formatBytes(host.memory.used)} / ${formatBytes(host.memory.total)}`}
            hint={
              host.load
                ? t('dashboard.load', {
                    one: host.load.one.toFixed(2),
                    five: host.load.five.toFixed(2),
                    fifteen: host.load.fifteen.toFixed(2),
                  })
                : undefined
            }
            unknownLabel={unknown}
          />
          {host.disks.map((disk) => (
            <Gauge
              key={disk.path}
              label={t(`dashboard.disk_${disk.label}`, { defaultValue: disk.label })}
              percent={disk.percent}
              detail={`${formatBytes(disk.used)} / ${formatBytes(disk.total)}`}
              hint={disk.path}
              unknownLabel={unknown}
            />
          ))}
          {host.swap.total > 0 && (
            <Gauge
              label={t('dashboard.swap')}
              percent={host.swap.percent}
              detail={`${formatBytes(host.swap.used)} / ${formatBytes(host.swap.total)}`}
              unknownLabel={unknown}
            />
          )}
        </div>
      )}

      <div className="mt-3 grid gap-3 lg:grid-cols-2">
        <ActivityFeed events={events} connected={connected} />

        <div className="card p-4">
          <h2 className="mb-3 text-sm font-medium">{host?.hostname || info?.name}</h2>
          <dl className="grid gap-x-6 gap-y-2 text-sm sm:grid-cols-2">
            {info && (
              <>
                <Row label={t('dashboard.operating_system')} value={info.operating_system} />
                <Row label={t('dashboard.kernel')} value={info.kernel_version} />
                <Row label={t('dashboard.architecture')} value={info.architecture} />
                <Row label={t('dashboard.cpus')} value={String(info.ncpu)} />
                <Row label={t('dashboard.total_memory')} value={formatBytes(info.mem_total)} />
                <Row label={t('dashboard.storage_driver')} value={info.storage_driver} />
              </>
            )}
            {host && (
              <>
                <Row
                  label={t('dashboard.host_uptime')}
                  value={formatUptime(host.uptime, unknown)}
                />
                <Row
                  label={t('dashboard.daemon_uptime')}
                  value={formatUptime(host.daemon.uptime, unknown)}
                />
                <Row label={t('dashboard.daemon_version')} value={host.daemon.version} />
                <Row
                  label={t('dashboard.docker_version')}
                  value={host.engine?.version ?? unknown}
                />
              </>
            )}
          </dl>

          {host?.errors && host.errors.length > 0 && (
            <div className="mt-3 border-t border-border pt-3">
              <p className="text-xs text-muted">{t('dashboard.reading_problems')}</p>
              <ul className="mt-1 space-y-0.5">
                {host.errors.map((problem) => (
                  <li key={problem} className="font-mono text-xs text-warn">
                    {problem}
                  </li>
                ))}
              </ul>
            </div>
          )}
        </div>
      </div>
    </>
  );
}

function Row({ label, value }: { label: string; value: string }) {
  return (
    <div className="flex justify-between gap-4 border-b border-border/50 py-1 last:border-0">
      <dt className="text-muted">{label}</dt>
      <dd className="truncate font-mono text-xs" title={value}>
        {value || '—'}
      </dd>
    </div>
  );
}

/**
 * Refetches the counts after the engine reports something changed.
 *
 * Debounced because a single `docker compose up` fires dozens of events in a
 * second, and refetching four lists per event would hammer the daemon exactly
 * when it is busiest.
 */
function useDebouncedRefresh(queryClient: ReturnType<typeof useQueryClient>) {
  const timer = useRef<ReturnType<typeof setTimeout> | null>(null);

  useEffect(
    () => () => {
      if (timer.current) clearTimeout(timer.current);
    },
    [],
  );

  return useCallback(() => {
    if (timer.current) clearTimeout(timer.current);
    timer.current = setTimeout(() => {
      for (const key of [['containers'], ['images'], ['volumes'], ['networks'], ['system']]) {
        void queryClient.invalidateQueries({ queryKey: key });
      }
    }, REFRESH_DEBOUNCE_MS);
  }, [queryClient]);
}
