import { useState } from 'react';
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import { useTranslation } from 'react-i18next';
import { FolderLock, Lock, Save, ServerCog, ShieldAlert } from 'lucide-react';

import { settings as settingsApi } from '../../api/endpoints';
import { ErrorPanel } from '../../components/ErrorPanel';
import { Spinner } from '../../components/Spinner';
import { Field, Toggle } from '../create/fields';
import { formatUptime } from '../../lib/format';
import { toast } from '../../stores/toast';

/**
 * The runtime settings, and the installation facts shown beside them.
 *
 * The split is the point: retention and the bind-mount warning are stored in
 * the database and change while the daemon runs. The socket path and the path
 * whitelist come from the config file and are shown read-only, because an
 * admin who could widen `allowed_paths` from a browser would be one request
 * away from mounting the whole filesystem into a container.
 */
export function InstallationPanel() {
  const { t } = useTranslation();
  const queryClient = useQueryClient();

  const query = useQuery({ queryKey: ['settings'], queryFn: settingsApi.get });

  const [retention, setRetention] = useState<string | null>(null);

  const save = useMutation({
    mutationFn: settingsApi.update,
    onSuccess: () => {
      setRetention(null);
      void queryClient.invalidateQueries({ queryKey: ['settings'] });
      toast.success(t('settings.saved'));
    },
    onError: (error: unknown) => {
      toast.error(t('settings.save_failed'), error instanceof Error ? error.message : undefined);
    },
  });

  if (query.isLoading) {
    return (
      <section className="card flex justify-center p-8">
        <Spinner />
      </section>
    );
  }
  if (query.error) {
    return (
      <section className="card p-4">
        <ErrorPanel error={query.error} onRetry={() => void query.refetch()} />
      </section>
    );
  }

  const view = query.data;
  if (!view) return null;

  const install = view.installation;
  const days = retention ?? String(view.audit_retention_days);
  const dirty = retention !== null && retention !== String(view.audit_retention_days);

  return (
    <>
      <section className="card p-4">
        <h2 className="mb-3 flex items-center gap-2 text-sm font-medium">
          <ServerCog size={16} className="text-muted" aria-hidden />
          {t('settings.runtime')}
        </h2>

        <div className="space-y-4">
          <Field
            label={t('settings.audit_retention')}
            htmlFor="audit-retention"
            hint={
              Number(days) > 0
                ? t('settings.audit_retention_hint', {
                    window: formatUptime(Number(days) * 86_400),
                  })
                : t('settings.audit_retention_forever')
            }
          >
            <div className="flex items-center gap-2">
              <input
                id="audit-retention"
                type="number"
                className="input w-32"
                min={0}
                max={3650}
                value={days}
                onChange={(e) => setRetention(e.target.value)}
              />
              <span className="text-xs text-muted">{t('settings.days')}</span>
              {dirty && (
                <button
                  type="button"
                  className="btn-primary"
                  disabled={save.isPending}
                  onClick={() => save.mutate({ audit_retention_days: Number(days) })}
                >
                  <Save size={14} aria-hidden />
                  {t('common.save')}
                </button>
              )}
            </div>
          </Field>

          <Toggle
            label={t('settings.bind_warning')}
            hint={t('settings.bind_warning_hint')}
            checked={view.bind_mount_warning}
            disabled={save.isPending}
            onChange={(next) => save.mutate({ bind_mount_warning: next })}
          />
        </div>
      </section>

      <section className="card p-4">
        <h2 className="mb-1 flex items-center gap-2 text-sm font-medium">
          <Lock size={16} className="text-muted" aria-hidden />
          {t('settings.installation')}
        </h2>
        <p className="mb-3 text-xs text-muted">
          {install.config_file
            ? t('settings.installation_hint', { file: install.config_file })
            : t('settings.installation_defaults')}
        </p>

        {exposed(install.listen, install.tls_enabled) && (
          // The daemon logs this at startup, where nobody rereads it. An
          // operator who moved `listen` off loopback months ago should still
          // be told, on the screen that shows them the address.
          <div className="mb-3 flex items-start gap-2 rounded border border-warn/40 bg-warn/10 p-3 text-xs">
            <ShieldAlert size={14} className="mt-0.5 shrink-0 text-warn" aria-hidden />
            <span>{t('settings.exposed_warning')}</span>
          </div>
        )}

        <dl className="space-y-1.5 text-sm">
          <Row label={t('settings.docker_host')} value={install.docker_host} />
          <Row label={t('settings.listen')} value={install.listen} />
          <Row label={t('settings.data_dir')} value={install.data_dir} />
          {install.template_dir && (
            <Row label={t('settings.template_dir')} value={install.template_dir} />
          )}
          <Row label="TLS" value={install.tls_enabled ? t('common.yes') : t('common.no')} />
          <Row label={t('settings.access_ttl')} value={formatUptime(install.access_ttl)} />
          <Row label={t('settings.refresh_ttl')} value={formatUptime(install.refresh_ttl)} />
        </dl>

        <div className="mt-3 border-t border-border pt-3">
          <h3 className="mb-1 flex items-center gap-2 text-xs font-medium">
            <FolderLock size={14} className="text-muted" aria-hidden />
            {t('settings.allowed_paths')}
          </h3>
          <p className="mb-2 text-xs text-muted">{t('settings.allowed_paths_hint')}</p>
          {install.allowed_paths.length === 0 ? (
            // An empty whitelist refuses every bind mount, which is a
            // configuration an operator may not have meant.
            <p className="text-xs text-warn">{t('settings.allowed_paths_empty')}</p>
          ) : (
            <ul className="space-y-1">
              {install.allowed_paths.map((path) => (
                <li key={path} className="font-mono text-xs">
                  {path}
                </li>
              ))}
            </ul>
          )}
        </div>
      </section>
    </>
  );
}

/**
 * Whether the panel is reachable from off this host without TLS.
 *
 * A hostname that is not a loopback literal counts as exposed: it may well
 * resolve to one, but assuming so is the wrong way to be wrong about a
 * root-equivalent API.
 */
function exposed(listen: string, tls: boolean): boolean {
  if (tls) return false;

  const host = listen.replace(/:\d+$/, '').replace(/^\[|\]$/g, '');
  return !(
    host === '127.0.0.1' ||
    host === 'localhost' ||
    host === '::1' ||
    host.startsWith('127.')
  );
}

function Row({ label, value }: { label: string; value: string }) {
  return (
    <div className="flex justify-between gap-4">
      <dt className="text-muted">{label}</dt>
      <dd className="truncate font-mono text-xs" title={value}>
        {value || '—'}
      </dd>
    </div>
  );
}
