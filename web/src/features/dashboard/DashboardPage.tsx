import { useQuery } from '@tanstack/react-query';
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
import { formatBytes } from '../../lib/format';

export function DashboardPage() {
  const { t } = useTranslation();

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
          hint={unhealthy > 0 ? `${unhealthy} unhealthy` : 'running / total'}
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

      {info && (
        <div className="card mt-4 p-4">
          <h2 className="mb-3 text-sm font-medium">{info.name}</h2>
          <dl className="grid gap-x-6 gap-y-2 text-sm sm:grid-cols-2 lg:grid-cols-3">
            <Row label="Operating system" value={info.operating_system} />
            <Row label="Kernel" value={info.kernel_version} />
            <Row label="Architecture" value={info.architecture} />
            <Row label="CPUs" value={String(info.ncpu)} />
            <Row label="Memory" value={formatBytes(info.mem_total)} />
            <Row label="Storage driver" value={info.storage_driver} />
          </dl>
        </div>
      )}
    </>
  );
}

function Row({ label, value }: { label: string; value: string }) {
  return (
    <div className="flex justify-between gap-4 border-b border-border/50 py-1 last:border-0">
      <dt className="text-muted">{label}</dt>
      <dd className="truncate font-mono text-xs">{value || '—'}</dd>
    </div>
  );
}
