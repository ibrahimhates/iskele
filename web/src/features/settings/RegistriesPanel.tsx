import { useState } from 'react';
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import { useTranslation } from 'react-i18next';
import { KeyRound, Pencil, Plus, Trash2 } from 'lucide-react';

import { registries as registriesApi } from '../../api/endpoints';
import type { Registry, RegistryInput } from '../../api/types';
import { EmptyState } from '../../components/EmptyState';
import { ErrorPanel } from '../../components/ErrorPanel';
import { ConfirmDialog } from '../../components/ConfirmDialog';
import { formatRelative } from '../../lib/format';

const blank: RegistryInput = { name: '', server: '', username: '', password: '', email: '' };

/**
 * Private registry credentials, in Settings because they are admin-only.
 *
 * A stored password is never sent back to the browser, so the edit form starts
 * with an empty password field and an empty field means "keep the stored one".
 * The form says so rather than leaving the operator to guess.
 */
export function RegistriesPanel() {
  const { t } = useTranslation();
  const queryClient = useQueryClient();

  const [editing, setEditing] = useState<Registry | null>(null);
  const [creating, setCreating] = useState(false);
  const [form, setForm] = useState<RegistryInput>(blank);
  const [removing, setRemoving] = useState<Registry | null>(null);

  const refresh = () => queryClient.invalidateQueries({ queryKey: ['registries'] });
  const query = useQuery({ queryKey: ['registries'], queryFn: registriesApi.list });

  const save = useMutation({
    mutationFn: () =>
      editing ? registriesApi.update(editing.id, form) : registriesApi.create(form),
    onSuccess: () => {
      close();
      void refresh();
    },
  });

  const remove = useMutation({
    mutationFn: (registry: Registry) => registriesApi.remove(registry.id),
    onSuccess: () => {
      setRemoving(null);
      void refresh();
    },
  });

  function open(registry: Registry | null) {
    setEditing(registry);
    setCreating(registry === null);
    setForm(
      registry
        ? {
            name: registry.name,
            server: registry.server,
            username: registry.username,
            password: '',
            email: registry.email ?? '',
          }
        : blank,
    );
    save.reset();
  }

  function close() {
    setEditing(null);
    setCreating(false);
    setForm(blank);
  }

  const items = query.data?.items ?? [];
  const dialogOpen = creating || editing !== null;

  return (
    <section className="space-y-3">
      <div className="flex items-center justify-between">
        <div>
          <h2 className="text-sm font-semibold">{t('registries.title')}</h2>
          <p className="text-xs text-muted">{t('registries.subtitle')}</p>
        </div>
        <button type="button" className="btn-default" onClick={() => open(null)}>
          <Plus size={14} aria-hidden />
          {t('common.add')}
        </button>
      </div>

      {query.error ? (
        <ErrorPanel error={query.error} onRetry={() => void query.refetch()} />
      ) : items.length === 0 ? (
        <EmptyState
          icon={<KeyRound size={28} aria-hidden />}
          title={t('registries.empty')}
          description={t('registries.empty_hint')}
        />
      ) : (
        <div className="card overflow-x-auto">
          <table className="w-full text-sm">
            <thead className="border-b border-border text-left text-xs uppercase tracking-wide text-muted">
              <tr>
                <th className="px-3 py-2 font-medium">{t('common.name')}</th>
                <th className="px-3 py-2 font-medium">{t('registries.server')}</th>
                <th className="px-3 py-2 font-medium">{t('registries.username')}</th>
                <th className="px-3 py-2 font-medium">{t('registries.last_used')}</th>
                <th className="px-3 py-2" />
              </tr>
            </thead>
            <tbody>
              {items.map((registry) => (
                <tr key={registry.id} className="border-b border-border/50 last:border-0">
                  <td className="px-3 py-2 font-medium">{registry.name}</td>
                  <td className="px-3 py-2 font-mono text-xs">{registry.server}</td>
                  <td className="px-3 py-2 text-muted">
                    {registry.username || t('registries.anonymous')}
                    {registry.has_password && (
                      <span className="ml-2 badge bg-elevated text-muted">
                        {t('registries.has_password')}
                      </span>
                    )}
                  </td>
                  <td className="whitespace-nowrap px-3 py-2 text-muted">
                    {registry.last_used_at ? formatRelative(registry.last_used_at) : '—'}
                  </td>
                  <td className="px-3 py-2">
                    <div className="flex justify-end gap-1">
                      <button
                        type="button"
                        className="btn-ghost p-1.5"
                        onClick={() => open(registry)}
                        aria-label={t('common.edit')}
                        title={t('common.edit')}
                      >
                        <Pencil size={15} aria-hidden />
                      </button>
                      <button
                        type="button"
                        className="btn-ghost p-1.5 text-muted hover:text-danger"
                        onClick={() => setRemoving(registry)}
                        aria-label={t('common.delete')}
                        title={t('common.delete')}
                      >
                        <Trash2 size={15} aria-hidden />
                      </button>
                    </div>
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      )}

      <ConfirmDialog
        open={dialogOpen}
        title={editing ? t('registries.edit') : t('registries.add')}
        confirmLabel={t('common.save')}
        busy={save.isPending}
        onCancel={close}
        onConfirm={() => save.mutate()}
      >
        <div className="space-y-3">
          <input
            className="input"
            value={form.name}
            placeholder="GitHub Container Registry"
            aria-label={t('common.name')}
            onChange={(e) => setForm({ ...form, name: e.target.value })}
          />
          <input
            className="input font-mono"
            value={form.server}
            placeholder="ghcr.io"
            aria-label={t('registries.server')}
            onChange={(e) => setForm({ ...form, server: e.target.value })}
          />
          <input
            className="input font-mono"
            value={form.username ?? ''}
            placeholder="deploy"
            aria-label={t('registries.username')}
            autoComplete="off"
            onChange={(e) => setForm({ ...form, username: e.target.value })}
          />
          <div className="space-y-1">
            <input
              className="input font-mono"
              type="password"
              value={form.password ?? ''}
              aria-label={t('registries.password')}
              autoComplete="new-password"
              onChange={(e) => setForm({ ...form, password: e.target.value })}
            />
            <p className="text-xs text-muted">
              {editing ? t('registries.password_keep') : t('registries.password_hint')}
            </p>
          </div>
          {save.error && <ErrorPanel error={save.error} />}
        </div>
      </ConfirmDialog>

      <ConfirmDialog
        open={removing !== null}
        title={t('registries.remove_title')}
        description={t('registries.remove_hint')}
        confirmLabel={t('common.delete')}
        destructive
        busy={remove.isPending}
        onCancel={() => setRemoving(null)}
        onConfirm={() => removing && remove.mutate(removing)}
      />
    </section>
  );
}
