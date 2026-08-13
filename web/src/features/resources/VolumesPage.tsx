import { useState } from 'react';
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import { useTranslation } from 'react-i18next';
import { Database, Plus, Trash2 } from 'lucide-react';

import { volumes as volumesApi } from '../../api/endpoints';
import type { Volume } from '../../api/types';
import { PageHeader } from '../../components/PageHeader';
import { EmptyState } from '../../components/EmptyState';
import { ErrorPanel } from '../../components/ErrorPanel';
import { Spinner } from '../../components/Spinner';
import { ConfirmDialog } from '../../components/ConfirmDialog';
import { useAuth } from '../../stores/auth';
import { formatBytes, formatRelative } from '../../lib/format';

export function VolumesPage() {
  const { t } = useTranslation();
  const queryClient = useQueryClient();
  const canCreate = useAuth((s) => s.can('create'));
  const canDelete = useAuth((s) => s.can('delete'));
  const canPrune = useAuth((s) => s.can('prune'));

  const [creating, setCreating] = useState(false);
  const [name, setName] = useState('');
  const [driver, setDriver] = useState('');
  const [removing, setRemoving] = useState<Volume | null>(null);
  const [pruning, setPruning] = useState(false);

  const refresh = () => queryClient.invalidateQueries({ queryKey: ['volumes'] });
  const query = useQuery({ queryKey: ['volumes'], queryFn: volumesApi.list });

  const create = useMutation({
    mutationFn: () => volumesApi.create({ name: name.trim(), driver: driver.trim() || undefined }),
    onSuccess: () => {
      setCreating(false);
      setName('');
      setDriver('');
      void refresh();
    },
  });

  const remove = useMutation({
    mutationFn: (volume: Volume) => volumesApi.remove(volume.name),
    onSuccess: () => {
      setRemoving(null);
      void refresh();
    },
  });

  const prune = useMutation({
    mutationFn: () => volumesApi.prune(),
    onSuccess: () => {
      setPruning(false);
      void refresh();
    },
  });

  if (query.isLoading) {
    return (
      <div className="flex justify-center py-16">
        <Spinner />
      </div>
    );
  }

  const items = query.data?.items ?? [];

  return (
    <>
      <PageHeader
        title={t('nav.volumes')}
        description={t('volumes.count', { count: items.length })}
        actions={
          <>
            {canCreate && (
              <button type="button" className="btn-primary" onClick={() => setCreating(true)}>
                <Plus size={14} aria-hidden />
                {t('volumes.create')}
              </button>
            )}
            {canPrune && (
              <button type="button" className="btn-default" onClick={() => setPruning(true)}>
                <Trash2 size={14} aria-hidden />
                {t('common.prune')}
              </button>
            )}
          </>
        }
      />

      {query.error ? (
        <ErrorPanel error={query.error} onRetry={() => void query.refetch()} />
      ) : items.length === 0 ? (
        <EmptyState
          icon={<Database size={32} aria-hidden />}
          title={t('volumes.empty')}
          description={t('volumes.empty_hint')}
        />
      ) : (
        <div className="card overflow-x-auto">
          <table className="w-full text-sm">
            <thead className="border-b border-border text-left text-xs uppercase tracking-wide text-muted">
              <tr>
                <th className="px-3 py-2 font-medium">{t('common.name')}</th>
                <th className="px-3 py-2 font-medium">{t('common.driver')}</th>
                <th className="px-3 py-2 font-medium">{t('volumes.mountpoint')}</th>
                <th className="px-3 py-2 font-medium">{t('common.created')}</th>
                <th className="px-3 py-2 text-right font-medium">{t('common.size')}</th>
                <th className="px-3 py-2" />
              </tr>
            </thead>
            <tbody>
              {items.map((volume) => (
                <tr key={volume.name} className="border-b border-border/50 last:border-0">
                  <td className="px-3 py-2 font-medium">{volume.name}</td>
                  <td className="px-3 py-2 text-muted">{volume.driver}</td>
                  <td className="max-w-72 truncate px-3 py-2 font-mono text-xs text-muted">
                    {volume.mountpoint}
                  </td>
                  <td className="whitespace-nowrap px-3 py-2 text-muted">
                    {volume.created_at ? formatRelative(volume.created_at) : '—'}
                  </td>
                  <td className="whitespace-nowrap px-3 py-2 text-right font-mono text-xs">
                    {volume.size < 0 ? '—' : formatBytes(volume.size)}
                  </td>
                  <td className="px-3 py-2 text-right">
                    {canDelete && (
                      <button
                        type="button"
                        className="btn-ghost p-1.5 text-muted hover:text-danger"
                        onClick={() => setRemoving(volume)}
                        aria-label={t('common.delete')}
                        title={t('common.delete')}
                      >
                        <Trash2 size={15} aria-hidden />
                      </button>
                    )}
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      )}

      <ConfirmDialog
        open={creating}
        title={t('volumes.create')}
        confirmLabel={t('common.create')}
        busy={create.isPending}
        onCancel={() => setCreating(false)}
        onConfirm={() => create.mutate()}
      >
        <div className="space-y-3">
          <input
            className="input"
            value={name}
            placeholder="pgdata"
            aria-label={t('common.name')}
            onChange={(e) => setName(e.target.value)}
          />
          <input
            className="input font-mono"
            value={driver}
            placeholder="local"
            aria-label={t('common.driver')}
            onChange={(e) => setDriver(e.target.value)}
          />
          {create.error && <ErrorPanel error={create.error} />}
        </div>
      </ConfirmDialog>

      <ConfirmDialog
        open={removing !== null}
        title={t('volumes.remove_title')}
        description={t('volumes.remove_hint')}
        confirmText={removing?.name}
        confirmLabel={t('common.delete')}
        destructive
        busy={remove.isPending}
        onCancel={() => setRemoving(null)}
        onConfirm={() => removing && remove.mutate(removing)}
      />

      <ConfirmDialog
        open={pruning}
        title={t('volumes.prune_title')}
        description={t('volumes.prune_hint')}
        confirmLabel={t('common.prune')}
        destructive
        busy={prune.isPending}
        onCancel={() => setPruning(false)}
        onConfirm={() => prune.mutate()}
      />
    </>
  );
}
