import { useState } from 'react';
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import { useTranslation } from 'react-i18next';
import { Network as NetworkIcon, Plus, Trash2, Unplug } from 'lucide-react';

import { containers as containersApi, networks as networksApi } from '../../api/endpoints';
import type { NetworkResource } from '../../api/types';
import { PageHeader } from '../../components/PageHeader';
import { EmptyState } from '../../components/EmptyState';
import { ErrorPanel } from '../../components/ErrorPanel';
import { Spinner } from '../../components/Spinner';
import { ConfirmDialog } from '../../components/ConfirmDialog';
import { useAuth } from '../../stores/auth';

/** The engine's own networks, which cannot be removed or pruned. */
const PREDEFINED = new Set(['bridge', 'host', 'none']);

export function NetworksPage() {
  const { t } = useTranslation();
  const queryClient = useQueryClient();
  const canCreate = useAuth((s) => s.can('create'));
  const canOperate = useAuth((s) => s.can('operate'));
  const canDelete = useAuth((s) => s.can('delete'));
  const canPrune = useAuth((s) => s.can('prune'));

  const [creating, setCreating] = useState(false);
  const [form, setForm] = useState({
    name: '',
    driver: 'bridge',
    subnet: '',
    gateway: '',
    internal: false,
  });
  const [removing, setRemoving] = useState<NetworkResource | null>(null);
  const [pruning, setPruning] = useState(false);
  const [connecting, setConnecting] = useState<NetworkResource | null>(null);
  const [containerID, setContainerID] = useState('');

  const refresh = () => queryClient.invalidateQueries({ queryKey: ['networks'] });
  const query = useQuery({ queryKey: ['networks'], queryFn: networksApi.list });

  const containers = useQuery({
    queryKey: ['containers', { all: true }],
    queryFn: () => containersApi.list({ all: true }),
    enabled: connecting !== null,
  });

  const create = useMutation({
    mutationFn: () =>
      networksApi.create({
        name: form.name.trim(),
        driver: form.driver.trim() || undefined,
        internal: form.internal,
        ipam: form.subnet.trim()
          ? [{ subnet: form.subnet.trim(), gateway: form.gateway.trim() || undefined }]
          : undefined,
      }),
    onSuccess: () => {
      setCreating(false);
      setForm({ name: '', driver: 'bridge', subnet: '', gateway: '', internal: false });
      void refresh();
    },
  });

  const remove = useMutation({
    mutationFn: (network: NetworkResource) => networksApi.remove(network.id),
    onSuccess: () => {
      setRemoving(null);
      void refresh();
    },
  });

  const prune = useMutation({
    mutationFn: () => networksApi.prune(),
    onSuccess: () => {
      setPruning(false);
      void refresh();
    },
  });

  const connect = useMutation({
    mutationFn: () => networksApi.connect((connecting as NetworkResource).id, containerID),
    onSuccess: () => {
      setConnecting(null);
      setContainerID('');
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
        title={t('nav.networks')}
        description={t('networks.count', { count: items.length })}
        actions={
          <>
            {canCreate && (
              <button type="button" className="btn-primary" onClick={() => setCreating(true)}>
                <Plus size={14} aria-hidden />
                {t('networks.create')}
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
        <EmptyState icon={<NetworkIcon size={32} aria-hidden />} title={t('networks.empty')} />
      ) : (
        <div className="card overflow-x-auto">
          <table className="w-full text-sm">
            <thead className="border-b border-border text-left text-xs uppercase tracking-wide text-muted">
              <tr>
                <th className="px-3 py-2 font-medium">{t('common.name')}</th>
                <th className="px-3 py-2 font-medium">{t('common.driver')}</th>
                <th className="px-3 py-2 font-medium">{t('networks.scope')}</th>
                <th className="px-3 py-2 font-medium">{t('networks.subnet')}</th>
                <th className="px-3 py-2 text-right font-medium">{t('networks.containers')}</th>
                <th className="px-3 py-2" />
              </tr>
            </thead>
            <tbody>
              {items.map((network) => (
                <tr key={network.id} className="border-b border-border/50 last:border-0">
                  <td className="px-3 py-2 font-medium">
                    {network.name}
                    {network.internal && (
                      <span className="ml-2 badge bg-elevated text-muted">
                        {t('networks.internal')}
                      </span>
                    )}
                  </td>
                  <td className="px-3 py-2 text-muted">{network.driver}</td>
                  <td className="px-3 py-2 text-muted">{network.scope}</td>
                  <td className="px-3 py-2 font-mono text-xs text-muted">
                    {network.ipam
                      ?.map((c) => c.subnet)
                      .filter(Boolean)
                      .join(', ') || '—'}
                  </td>
                  <td className="px-3 py-2 text-right font-mono text-xs text-muted">
                    {network.container_count}
                  </td>
                  <td className="px-3 py-2">
                    <div className="flex justify-end gap-1">
                      {canOperate && (
                        <button
                          type="button"
                          className="btn-ghost p-1.5"
                          onClick={() => setConnecting(network)}
                          aria-label={t('networks.connect')}
                          title={t('networks.connect')}
                        >
                          <Unplug size={15} aria-hidden />
                        </button>
                      )}
                      {canDelete && !PREDEFINED.has(network.name) && (
                        <button
                          type="button"
                          className="btn-ghost p-1.5 text-muted hover:text-danger"
                          onClick={() => setRemoving(network)}
                          aria-label={t('common.delete')}
                          title={t('common.delete')}
                        >
                          <Trash2 size={15} aria-hidden />
                        </button>
                      )}
                    </div>
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      )}

      <ConfirmDialog
        open={creating}
        title={t('networks.create')}
        confirmLabel={t('common.create')}
        busy={create.isPending}
        onCancel={() => setCreating(false)}
        onConfirm={() => create.mutate()}
      >
        <div className="space-y-3">
          <input
            className="input"
            value={form.name}
            placeholder="backend"
            aria-label={t('common.name')}
            onChange={(e) => setForm({ ...form, name: e.target.value })}
          />
          <select
            className="input"
            value={form.driver}
            aria-label={t('common.driver')}
            onChange={(e) => setForm({ ...form, driver: e.target.value })}
          >
            <option value="bridge">bridge</option>
            <option value="macvlan">macvlan</option>
            <option value="ipvlan">ipvlan</option>
            <option value="overlay">overlay</option>
          </select>
          <input
            className="input font-mono"
            value={form.subnet}
            placeholder="172.30.0.0/16"
            aria-label={t('networks.subnet')}
            onChange={(e) => setForm({ ...form, subnet: e.target.value })}
          />
          <input
            className="input font-mono"
            value={form.gateway}
            placeholder="172.30.0.1"
            aria-label={t('networks.gateway')}
            onChange={(e) => setForm({ ...form, gateway: e.target.value })}
          />
          <label className="flex cursor-pointer items-center gap-2 text-sm">
            <input
              type="checkbox"
              className="accent-accent"
              checked={form.internal}
              onChange={(e) => setForm({ ...form, internal: e.target.checked })}
            />
            {t('networks.internal_hint')}
          </label>
          {create.error && <ErrorPanel error={create.error} />}
        </div>
      </ConfirmDialog>

      <ConfirmDialog
        open={connecting !== null}
        title={t('networks.connect')}
        description={t('networks.connect_hint')}
        confirmLabel={t('networks.connect')}
        busy={connect.isPending}
        onCancel={() => setConnecting(null)}
        onConfirm={() => connect.mutate()}
      >
        <div className="space-y-3">
          <select
            className="input"
            value={containerID}
            aria-label={t('nav.containers')}
            onChange={(e) => setContainerID(e.target.value)}
          >
            <option value="">—</option>
            {(containers.data?.items ?? []).map((container) => (
              <option key={container.id} value={container.id}>
                {container.name}
              </option>
            ))}
          </select>
          {connect.error && <ErrorPanel error={connect.error} />}
        </div>
      </ConfirmDialog>

      <ConfirmDialog
        open={removing !== null}
        title={t('networks.remove_title')}
        description={t('networks.remove_hint')}
        confirmText={removing?.name}
        confirmLabel={t('common.delete')}
        destructive
        busy={remove.isPending}
        onCancel={() => setRemoving(null)}
        onConfirm={() => removing && remove.mutate(removing)}
      />

      <ConfirmDialog
        open={pruning}
        title={t('networks.prune_title')}
        description={t('networks.prune_hint')}
        confirmLabel={t('common.prune')}
        destructive
        busy={prune.isPending}
        onCancel={() => setPruning(false)}
        onConfirm={() => prune.mutate()}
      />
    </>
  );
}
