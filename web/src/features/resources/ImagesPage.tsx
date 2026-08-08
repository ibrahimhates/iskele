import { useQuery } from '@tanstack/react-query';
import { useTranslation } from 'react-i18next';
import { HardDrive } from 'lucide-react';

import { images } from '../../api/endpoints';
import { PageHeader } from '../../components/PageHeader';
import { EmptyState } from '../../components/EmptyState';
import { ErrorPanel } from '../../components/ErrorPanel';
import { Spinner } from '../../components/Spinner';
import { formatBytes, formatRelative, shortID } from '../../lib/format';

/** Read-only image list. Pull, prune and tagging arrive in M5. */
export function ImagesPage() {
  const { t } = useTranslation();
  const query = useQuery({ queryKey: ['images'], queryFn: () => images.list() });

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
        <PageHeader title={t('nav.images')} />
        <ErrorPanel error={query.error} onRetry={() => void query.refetch()} />
      </>
    );
  }

  const items = query.data?.items ?? [];

  return (
    <>
      <PageHeader title={t('nav.images')} description={`${items.length}`} />
      {items.length === 0 ? (
        <EmptyState icon={<HardDrive size={32} aria-hidden />} title={t('nav.images')} />
      ) : (
        <div className="card overflow-x-auto">
          <table className="w-full text-sm">
            <thead className="border-b border-border text-left text-xs uppercase tracking-wide text-muted">
              <tr>
                <th className="px-3 py-2 font-medium">Repository</th>
                <th className="px-3 py-2 font-medium">ID</th>
                <th className="px-3 py-2 font-medium">{t('common.created')}</th>
                <th className="px-3 py-2 text-right font-medium">Size</th>
                <th className="px-3 py-2 text-right font-medium">Used by</th>
              </tr>
            </thead>
            <tbody>
              {items.map((image) => (
                <tr key={image.id} className="border-b border-border/50 last:border-0">
                  <td className="px-3 py-2">
                    {image.repo_tags.length > 0 && image.repo_tags[0] !== '<none>:<none>' ? (
                      <span className="font-mono text-xs">{image.repo_tags.join(', ')}</span>
                    ) : (
                      <span className="badge bg-muted/15 text-muted">dangling</span>
                    )}
                  </td>
                  <td className="px-3 py-2 font-mono text-xs text-muted">{shortID(image.id)}</td>
                  <td className="px-3 py-2 text-muted">{formatRelative(image.created)}</td>
                  <td className="px-3 py-2 text-right tabular-nums">{formatBytes(image.size)}</td>
                  <td className="px-3 py-2 text-right tabular-nums text-muted">
                    {image.containers < 0 ? '—' : image.containers}
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
