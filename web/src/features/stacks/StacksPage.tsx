import { useState } from 'react';
import { Link, useNavigate } from 'react-router-dom';
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import { useTranslation } from 'react-i18next';
import { Download, Layers, Plus } from 'lucide-react';

import { stacks as stacksApi } from '../../api/endpoints';
import type { DiscoveredStack, Stack, StackStatus } from '../../api/types';
import { PageHeader } from '../../components/PageHeader';
import { EmptyState } from '../../components/EmptyState';
import { ErrorPanel } from '../../components/ErrorPanel';
import { Spinner } from '../../components/Spinner';
import { useAuth } from '../../stores/auth';
import { formatRelative } from '../../lib/format';

const STATUS_CLASS: Record<StackStatus, string> = {
  created: 'text-muted',
  deploying: 'text-accent',
  deployed: 'text-ok',
  failed: 'text-danger',
  stopped: 'text-muted',
};

/** The stack list, plus the compose projects running here that are not stacks yet. */
export function StacksPage() {
  const { t } = useTranslation();
  const navigate = useNavigate();
  const canCreate = useAuth((s) => s.can('create'));

  const query = useQuery({ queryKey: ['stacks'], queryFn: stacksApi.list });
  const discovered = useQuery({
    queryKey: ['stacks', 'discovered'],
    queryFn: stacksApi.discovered,
  });

  if (query.isLoading) {
    return (
      <div className="flex justify-center py-16">
        <Spinner />
      </div>
    );
  }

  const items = query.data?.items ?? [];
  const found = discovered.data?.items ?? [];

  return (
    <>
      <PageHeader
        title={t('nav.stacks')}
        description={t('stacks.count', { count: items.length })}
        actions={
          canCreate ? (
            <button type="button" className="btn-primary" onClick={() => navigate('/stacks/new')}>
              <Plus size={14} aria-hidden />
              {t('stacks.create')}
            </button>
          ) : undefined
        }
      />

      {query.error ? (
        <ErrorPanel error={query.error} onRetry={() => void query.refetch()} />
      ) : items.length === 0 ? (
        <EmptyState
          icon={<Layers size={32} aria-hidden />}
          title={t('stacks.empty')}
          description={t('stacks.empty_hint')}
        />
      ) : (
        <div className="card overflow-x-auto">
          <table className="w-full text-sm">
            <thead className="border-b border-border text-left text-xs uppercase tracking-wide text-muted">
              <tr>
                <th className="px-3 py-2 font-medium">{t('common.name')}</th>
                <th className="px-3 py-2 font-medium">{t('common.status')}</th>
                <th className="px-3 py-2 font-medium">{t('stacks.source')}</th>
                <th className="px-3 py-2 font-medium">{t('stacks.deployed')}</th>
                <th className="px-3 py-2 font-medium">{t('common.created')}</th>
              </tr>
            </thead>
            <tbody>
              {items.map((stack) => (
                <StackRow key={stack.id} stack={stack} />
              ))}
            </tbody>
          </table>
        </div>
      )}

      {found.length > 0 && <DiscoveredStacks found={found} />}
    </>
  );
}

/** One row of the stack list. */
function StackRow({ stack }: { stack: Stack }) {
  const { t } = useTranslation();

  return (
    <tr className="border-b border-border/50 last:border-0">
      <td className="px-3 py-2 font-medium">
        <Link to={`/stacks/${encodeURIComponent(stack.id)}`} className="hover:text-accent">
          {stack.name}
        </Link>
        {stack.last_error && (
          <span className="block max-w-96 truncate text-xs text-danger">{stack.last_error}</span>
        )}
      </td>
      <td className={`whitespace-nowrap px-3 py-2 font-medium ${STATUS_CLASS[stack.status]}`}>
        {t(`stacks.status_${stack.status}`)}
      </td>
      <td className="px-3 py-2 text-muted">
        {t(`stacks.source_${stack.source}`)}
        {stack.git_url && (
          <span className="block max-w-72 truncate font-mono text-xs">{stack.git_url}</span>
        )}
        {stack.path && (
          <span className="block max-w-72 truncate font-mono text-xs">{stack.path}</span>
        )}
      </td>
      <td className="whitespace-nowrap px-3 py-2 text-muted">
        {stack.last_deployed_at ? formatRelative(stack.last_deployed_at) : '—'}
      </td>
      <td className="whitespace-nowrap px-3 py-2 text-muted">
        {formatRelative(stack.created_at)}
        {stack.created_by && <span className="block text-xs">{stack.created_by}</span>}
      </td>
    </tr>
  );
}

/**
 * Compose projects running on this host that Iskele has no record of.
 *
 * `docker compose up` on the command line produces the same labelled
 * containers, so leaving them out would misrepresent what is running.
 */
function DiscoveredStacks({ found }: { found: DiscoveredStack[] }) {
  const { t } = useTranslation();
  const queryClient = useQueryClient();
  const canCreate = useAuth((s) => s.can('create'));
  const [failed, setFailed] = useState<string | null>(null);

  const adopt = useMutation({
    mutationFn: (name: string) => stacksApi.import(name),
    onSuccess: () => {
      setFailed(null);
      void queryClient.invalidateQueries({ queryKey: ['stacks'] });
    },
    onError: (err: unknown) => setFailed(err instanceof Error ? err.message : String(err)),
  });

  return (
    <section className="mt-8">
      <h2 className="text-sm font-semibold">{t('stacks.discovered')}</h2>
      <p className="mb-3 mt-0.5 text-xs text-muted">{t('stacks.discovered_hint')}</p>

      <div className="card overflow-x-auto">
        <table className="w-full text-sm">
          <thead className="border-b border-border text-left text-xs uppercase tracking-wide text-muted">
            <tr>
              <th className="px-3 py-2 font-medium">{t('common.name')}</th>
              <th className="px-3 py-2 font-medium">{t('stacks.services')}</th>
              <th className="px-3 py-2 font-medium">{t('stacks.containers')}</th>
              <th className="px-3 py-2 font-medium">{t('stacks.compose_file')}</th>
              <th className="px-3 py-2" />
            </tr>
          </thead>
          <tbody>
            {found.map((stack) => (
              <tr key={stack.name} className="border-b border-border/50 last:border-0">
                <td className="px-3 py-2 font-medium">{stack.name}</td>
                <td className="px-3 py-2 text-muted">{stack.services.join(', ') || '—'}</td>
                <td className="whitespace-nowrap px-3 py-2 text-muted">
                  {t('stacks.running_of', { running: stack.running, total: stack.containers })}
                </td>
                <td className="max-w-72 px-3 py-2 font-mono text-xs text-muted">
                  <span className="block truncate">{stack.config_file || '—'}</span>
                  {!stack.importable && stack.reason && (
                    <span className="block text-xs text-warn">{stack.reason}</span>
                  )}
                </td>
                <td className="whitespace-nowrap px-3 py-2 text-right">
                  {canCreate && stack.importable && (
                    <button
                      type="button"
                      className="btn-default"
                      disabled={adopt.isPending}
                      onClick={() => adopt.mutate(stack.name)}
                    >
                      <Download size={14} aria-hidden />
                      {t('stacks.import')}
                    </button>
                  )}
                </td>
              </tr>
            ))}
          </tbody>
        </table>
      </div>

      {failed && <p className="mt-2 text-xs text-danger">{failed}</p>}
    </section>
  );
}
