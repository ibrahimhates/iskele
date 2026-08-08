import { useQuery } from '@tanstack/react-query';
import { useTranslation } from 'react-i18next';
import { Database } from 'lucide-react';

import { volumes } from '../../api/endpoints';
import { PageHeader } from '../../components/PageHeader';
import { EmptyState } from '../../components/EmptyState';
import { ErrorPanel } from '../../components/ErrorPanel';
import { Spinner } from '../../components/Spinner';
import { formatBytes, formatRelative } from '../../lib/format';

/** Read-only volume list. Create, remove and prune arrive in M5. */
export function VolumesPage() {
  const { t } = useTranslation();
  const query = useQuery({ queryKey: ['volumes'], queryFn: volumes.list });

  if (query.isLoading) {
    return (
      <div className="flex justify-center py-16">
        <Spinner />
      </div>
    );
  }
  if (query.error) {
    return (
      <>
        <PageHeader title={t('nav.volumes')} />
        <ErrorPanel error={query.error} onRetry={() => void query.refetch()} />
      </>
    );
  }

  const items = query.data?.items ?? [];

  return (
    <>
      <PageHeader title={t('nav.volumes')} description={`${items.length}`} />
      {items.length === 0 ? (
        <EmptyState icon={<Database size={32} aria-hidden />} title={t('nav.volumes')} />
      ) : (
        <div className="card overflow-x-auto">
          <table className="w-full text-sm">
            <thead className="border-b border-border text-left text-xs uppercase tracking-wide text-muted">
              <tr>
                <th className="px-3 py-2 font-medium">{t('common.name')}</th>
                <th className="px-3 py-2 font-medium">Driver</th>
                <th className="px-3 py-2 font-medium">Mountpoint</th>
                <th className="px-3 py-2 font-medium">{t('common.created')}</th>
                <th className="px-3 py-2 text-right font-medium">Size</th>
              </tr>
            </thead>
            <tbody>
              {items.map((volume) => (
                <tr key={volume.name} className="border-b border-border/50 last:border-0">
                  <td className="px-3 py-2 font-medium">{volume.name}</td>
                  <td className="px-3 py-2 text-muted">{volume.driver}</td>
                  <td className="px-3 py-2 font-mono text-xs text-muted">{volume.mountpoint}</td>
                  <td className="px-3 py-2 text-muted">{formatRelative(volume.created_at)}</td>
                  <td className="px-3 py-2 text-right tabular-nums">{formatBytes(volume.size)}</td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      )}
    </>
  );
}
