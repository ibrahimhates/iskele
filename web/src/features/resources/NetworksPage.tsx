import { useQuery } from '@tanstack/react-query';
import { useTranslation } from 'react-i18next';
import { Network as NetworkIcon } from 'lucide-react';

import { networks } from '../../api/endpoints';
import { PageHeader } from '../../components/PageHeader';
import { EmptyState } from '../../components/EmptyState';
import { ErrorPanel } from '../../components/ErrorPanel';
import { Spinner } from '../../components/Spinner';

/** Read-only network list. Create, connect and prune arrive in M5. */
export function NetworksPage() {
  const { t } = useTranslation();
  const query = useQuery({ queryKey: ['networks'], queryFn: networks.list });

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
        <PageHeader title={t('nav.networks')} />
        <ErrorPanel error={query.error} onRetry={() => void query.refetch()} />
      </>
    );
  }

  const items = query.data?.items ?? [];

  return (
    <>
      <PageHeader title={t('nav.networks')} description={`${items.length}`} />
      {items.length === 0 ? (
        <EmptyState icon={<NetworkIcon size={32} aria-hidden />} title={t('nav.networks')} />
      ) : (
        <div className="card overflow-x-auto">
          <table className="w-full text-sm">
            <thead className="border-b border-border text-left text-xs uppercase tracking-wide text-muted">
              <tr>
                <th className="px-3 py-2 font-medium">{t('common.name')}</th>
                <th className="px-3 py-2 font-medium">Driver</th>
                <th className="px-3 py-2 font-medium">Scope</th>
                <th className="px-3 py-2 font-medium">Subnet</th>
              </tr>
            </thead>
            <tbody>
              {items.map((network) => (
                <tr key={network.id} className="border-b border-border/50 last:border-0">
                  <td className="px-3 py-2 font-medium">{network.name}</td>
                  <td className="px-3 py-2 text-muted">{network.driver}</td>
                  <td className="px-3 py-2 text-muted">{network.scope}</td>
                  <td className="px-3 py-2 font-mono text-xs text-muted">
                    {network.ipam
                      ?.map((c) => c.subnet)
                      .filter(Boolean)
                      .join(', ') || '—'}
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      )}
    </>
  );
}
