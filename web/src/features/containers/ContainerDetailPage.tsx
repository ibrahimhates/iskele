import { useState } from 'react';
import { Link, useNavigate, useParams } from 'react-router-dom';
import { useQuery } from '@tanstack/react-query';
import { useTranslation } from 'react-i18next';
import { ArrowLeft, Pause, Play, RefreshCw, RotateCw, Square, Trash2, Zap } from 'lucide-react';

import { containers as containersApi } from '../../api/endpoints';
import type { ContainerDetail } from '../../api/types';
import { PageHeader } from '../../components/PageHeader';
import { ErrorPanel } from '../../components/ErrorPanel';
import { Spinner } from '../../components/Spinner';
import { ConfirmDialog } from '../../components/ConfirmDialog';
import { StateBadge } from './StateBadge';
import { LogViewer } from './LogViewer';
import { StatsPanel } from './StatsPanel';
import { ConsolePanel } from './ConsolePanel';
import { JsonViewer } from './JsonViewer';
import {
  useContainerAction,
  useContainerRedeploy,
  useContainerRemove,
  useContainerRename,
} from './useContainerActions';
import { cn } from '../../lib/cn';
import { formatBytes, formatPort, formatRelative, formatTime, shortID } from '../../lib/format';
import { useAuth } from '../../stores/auth';

const TABS = [
  'overview',
  'logs',
  'stats',
  'console',
  'inspect',
  'env',
  'mounts',
  'network',
] as const;
type Tab = (typeof TABS)[number];

export function ContainerDetailPage() {
  const { t } = useTranslation();
  const { id = '', tab } = useParams();
  const navigate = useNavigate();
  const canOperate = useAuth((s) => s.can('operate'));
  const canDelete = useAuth((s) => s.can('delete'));

  const active: Tab = TABS.includes(tab as Tab) ? (tab as Tab) : 'overview';

  const [confirmRemove, setConfirmRemove] = useState(false);
  const [confirmRedeploy, setConfirmRedeploy] = useState(false);
  const [renaming, setRenaming] = useState(false);
  const [newName, setNewName] = useState('');
  const [forceRemove, setForceRemove] = useState(false);
  const [removeVolumes, setRemoveVolumes] = useState(false);

  const query = useQuery({
    queryKey: ['container', id],
    queryFn: () => containersApi.get(id),
    // Only the overview needs to stay live; polling behind a terminal or a
    // chart would fight the stream for the same data.
    refetchInterval: active === 'overview' ? 5_000 : false,
  });

  const inspect = useQuery({
    queryKey: ['container', id, 'inspect'],
    queryFn: () => containersApi.inspect(id),
    enabled: active === 'inspect',
  });

  const action = useContainerAction();
  const remove = useContainerRemove();
  const redeploy = useContainerRedeploy();
  const rename = useContainerRename();

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
        <BackLink />
        <ErrorPanel error={query.error} onRetry={() => void query.refetch()} />
      </>
    );
  }

  const container = query.data as ContainerDetail;
  const running = container.state === 'running';
  const paused = container.state === 'paused';

  return (
    <>
      <BackLink />

      <PageHeader
        title={container.name || shortID(container.id)}
        description={container.image}
        actions={
          <div className="flex flex-wrap items-center gap-2">
            {canOperate && (
              <>
                {running ? (
                  <>
                    <button
                      type="button"
                      className="btn-default"
                      onClick={() => action.mutate({ id, action: 'stop' })}
                      disabled={action.isPending}
                    >
                      <Square size={14} aria-hidden />
                      {t('containers.actions.stop')}
                    </button>
                    <button
                      type="button"
                      className="btn-default"
                      onClick={() => action.mutate({ id, action: 'pause' })}
                      disabled={action.isPending}
                    >
                      <Pause size={14} aria-hidden />
                      {t('containers.actions.pause')}
                    </button>
                  </>
                ) : (
                  <button
                    type="button"
                    className="btn-default"
                    onClick={() => action.mutate({ id, action: paused ? 'unpause' : 'start' })}
                    disabled={action.isPending}
                  >
                    <Play size={14} aria-hidden />
                    {t(paused ? 'containers.actions.unpause' : 'containers.actions.start')}
                  </button>
                )}

                <button
                  type="button"
                  className="btn-default"
                  onClick={() => action.mutate({ id, action: 'restart' })}
                  disabled={action.isPending}
                >
                  <RotateCw size={14} aria-hidden />
                  {t('containers.actions.restart')}
                </button>

                <button
                  type="button"
                  className="btn-default"
                  onClick={() => {
                    setNewName(container.name);
                    setRenaming(true);
                  }}
                >
                  {t('containers.actions.rename')}
                </button>

                <button
                  type="button"
                  className="btn-default"
                  onClick={() => setConfirmRedeploy(true)}
                  disabled={redeploy.isPending}
                >
                  <Zap size={14} aria-hidden />
                  {t('containers.actions.redeploy')}
                </button>
              </>
            )}

            {canDelete && (
              <button type="button" className="btn-danger" onClick={() => setConfirmRemove(true)}>
                <Trash2 size={14} aria-hidden />
                {t('containers.actions.remove')}
              </button>
            )}

            <button
              type="button"
              className="btn-ghost"
              onClick={() => void query.refetch()}
              aria-label={t('common.refresh')}
            >
              <RefreshCw size={14} aria-hidden />
            </button>
          </div>
        }
      />

      {action.error != null && <ErrorPanel error={action.error} />}
      {remove.error != null && <ErrorPanel error={remove.error} />}
      {redeploy.error != null && <ErrorPanel error={redeploy.error} />}
      {redeploy.data?.rolled_back && (
        <div className="mb-3 rounded-md border border-warn/40 bg-warn/10 px-3 py-2 text-sm text-warn">
          The replacement failed and the original container was restored.
        </div>
      )}

      <nav className="mb-4 flex gap-1 overflow-x-auto border-b border-border" role="tablist">
        {TABS.map((name) => (
          <button
            key={name}
            type="button"
            role="tab"
            aria-selected={active === name}
            className={cn(
              'whitespace-nowrap border-b-2 px-3 py-2 text-sm transition-colors',
              active === name
                ? 'border-accent font-medium text-accent'
                : 'border-transparent text-muted hover:text-fg',
            )}
            onClick={() =>
              navigate(
                `/containers/${encodeURIComponent(id)}${name === 'overview' ? '' : `/${name}`}`,
              )
            }
          >
            {t(`containers.tabs.${name}`)}
          </button>
        ))}
      </nav>

      {active === 'overview' && <Overview container={container} />}
      {active === 'logs' && <LogViewer containerID={id} name={container.name} />}
      {active === 'stats' && <StatsPanel containerID={id} running={running} />}
      {active === 'console' && <ConsolePanel containerID={id} running={running} />}
      {active === 'inspect' &&
        (inspect.isLoading ? (
          <Spinner />
        ) : inspect.error ? (
          <ErrorPanel error={inspect.error} />
        ) : (
          <JsonViewer value={inspect.data} />
        ))}
      {active === 'env' && <EnvTab env={container.env ?? []} />}
      {active === 'mounts' && <MountsTab container={container} />}
      {active === 'network' && <NetworkTab container={container} />}

      <ConfirmDialog
        open={confirmRemove}
        destructive
        busy={remove.isPending}
        title={t('containers.actions.remove')}
        description={t('containers.confirmRemoveHelp', { name: container.name })}
        confirmText={container.name || shortID(container.id)}
        confirmLabel={t('containers.actions.remove')}
        onCancel={() => setConfirmRemove(false)}
        onConfirm={async () => {
          await remove.mutateAsync({ id, force: forceRemove, volumes: removeVolumes });
          navigate('/containers', { replace: true });
        }}
      >
        <div className="space-y-2 text-sm">
          <label className="flex cursor-pointer items-center gap-2">
            <input
              type="checkbox"
              className="accent-accent"
              checked={forceRemove}
              onChange={(e) => setForceRemove(e.target.checked)}
            />
            {t('containers.forceRemove')}
          </label>
          <label className="flex cursor-pointer items-center gap-2">
            <input
              type="checkbox"
              className="accent-accent"
              checked={removeVolumes}
              onChange={(e) => setRemoveVolumes(e.target.checked)}
            />
            {t('containers.removeVolumes')}
          </label>
        </div>
      </ConfirmDialog>

      <ConfirmDialog
        open={confirmRedeploy}
        busy={redeploy.isPending}
        title={t('containers.actions.redeploy')}
        description={t('containers.redeployHelp')}
        confirmLabel={t('containers.actions.redeploy')}
        onCancel={() => setConfirmRedeploy(false)}
        onConfirm={async () => {
          const result = await redeploy.mutateAsync(id);
          setConfirmRedeploy(false);
          if (result.new_id && result.new_id !== id) {
            navigate(`/containers/${encodeURIComponent(result.new_id)}`, { replace: true });
          }
        }}
      />

      <ConfirmDialog
        open={renaming}
        busy={rename.isPending}
        title={t('containers.renameTitle')}
        confirmLabel={t('containers.actions.rename')}
        onCancel={() => setRenaming(false)}
        onConfirm={async () => {
          await rename.mutateAsync({ id, name: newName });
          setRenaming(false);
        }}
      >
        <input
          className="input"
          value={newName}
          onChange={(e) => setNewName(e.target.value)}
          aria-label={t('common.name')}
        />
      </ConfirmDialog>
    </>
  );
}

function BackLink() {
  const { t } = useTranslation();
  return (
    <Link
      to="/containers"
      className="mb-3 inline-flex items-center gap-1.5 text-sm text-muted hover:text-fg"
    >
      <ArrowLeft size={14} aria-hidden />
      {t('containers.title')}
    </Link>
  );
}

function Overview({ container }: { container: ContainerDetail }) {
  const { t } = useTranslation();

  return (
    <div className="grid gap-4 lg:grid-cols-2">
      <section className="card p-4">
        <h2 className="mb-3 text-sm font-medium">{t('containers.tabs.overview')}</h2>
        <dl className="space-y-1.5 text-sm">
          <Row label="ID" value={<code className="font-mono text-xs">{container.id}</code>} />
          <Row
            label={t('common.status')}
            value={<StateBadge state={container.state} health={container.health} />}
          />
          <Row
            label={t('common.image')}
            value={<code className="font-mono text-xs">{container.image}</code>}
          />
          <Row label={t('common.created')} value={formatTime(container.created)} />
          <Row label={t('containers.uptime')} value={formatRelative(container.started_at)} />
          <Row label={t('containers.restarts')} value={String(container.restart_count)} />
          <Row label="Restart policy" value={container.restart_policy || '—'} />
          <Row
            label="Command"
            value={
              <code className="font-mono text-xs">
                {container.command || (container.cmd ?? []).join(' ') || '—'}
              </code>
            }
          />
          <Row label="Working directory" value={container.working_dir || '—'} />
          <Row label="User" value={container.user || '—'} />
          <Row label="PID" value={container.pid ? String(container.pid) : '—'} />
          {container.exit_code !== 0 && (
            <Row label="Exit code" value={String(container.exit_code)} />
          )}
          {container.error && (
            <Row label="Error" value={<span className="text-danger">{container.error}</span>} />
          )}
          {container.privileged && (
            <Row
              label="Privileged"
              value={<span className="badge bg-danger/15 text-danger">yes</span>}
            />
          )}
          {container.size_rw >= 0 && (
            <Row label="Writable layer" value={formatBytes(container.size_rw)} />
          )}
        </dl>
      </section>

      <section className="card p-4">
        <h2 className="mb-3 text-sm font-medium">{t('common.ports')}</h2>
        {container.ports.length === 0 ? (
          <p className="text-sm text-muted">—</p>
        ) : (
          <ul className="space-y-1 font-mono text-xs">
            {container.ports.map((port, index) => (
              <li key={`${port.private_port}-${index}`}>{formatPort(port)}</li>
            ))}
          </ul>
        )}

        {container.health_check && (
          <>
            <h2 className="mb-2 mt-4 text-sm font-medium">Health</h2>
            <dl className="space-y-1.5 text-sm">
              <Row label={t('common.status')} value={container.health_check.status} />
              <Row label="Failing streak" value={String(container.health_check.failing_streak)} />
            </dl>
            {container.health_check.last_output && (
              <pre className="mt-2 max-h-32 overflow-auto rounded bg-elevated/50 p-2 font-mono text-xs">
                {container.health_check.last_output}
              </pre>
            )}
          </>
        )}

        {Object.keys(container.labels ?? {}).length > 0 && (
          <>
            <h2 className="mb-2 mt-4 text-sm font-medium">Labels</h2>
            <ul className="space-y-1 font-mono text-xs">
              {Object.entries(container.labels).map(([key, value]) => (
                <li key={key} className="break-all">
                  <span className="text-muted">{key}</span>={value}
                </li>
              ))}
            </ul>
          </>
        )}
      </section>
    </div>
  );
}

function EnvTab({ env }: { env: string[] }) {
  const { t } = useTranslation();
  if (env.length === 0) return <p className="text-sm text-muted">—</p>;

  return (
    <div className="card overflow-x-auto">
      <table className="w-full text-sm">
        <thead className="border-b border-border text-left text-xs uppercase tracking-wide text-muted">
          <tr>
            <th className="px-3 py-2 font-medium">{t('common.name')}</th>
            <th className="px-3 py-2 font-medium">Value</th>
          </tr>
        </thead>
        <tbody>
          {env.map((entry, index) => {
            const separator = entry.indexOf('=');
            const key = separator === -1 ? entry : entry.slice(0, separator);
            const value = separator === -1 ? '' : entry.slice(separator + 1);
            return (
              <tr key={`${key}-${index}`} className="border-b border-border/50 last:border-0">
                <td className="px-3 py-1.5 font-mono text-xs">{key}</td>
                <td className="break-all px-3 py-1.5 font-mono text-xs text-muted">{value}</td>
              </tr>
            );
          })}
        </tbody>
      </table>
    </div>
  );
}

function MountsTab({ container }: { container: ContainerDetail }) {
  if (container.mount_points.length === 0) return <p className="text-sm text-muted">—</p>;

  return (
    <div className="card overflow-x-auto">
      <table className="w-full text-sm">
        <thead className="border-b border-border text-left text-xs uppercase tracking-wide text-muted">
          <tr>
            <th className="px-3 py-2 font-medium">Type</th>
            <th className="px-3 py-2 font-medium">Source</th>
            <th className="px-3 py-2 font-medium">Destination</th>
            <th className="px-3 py-2 font-medium">Mode</th>
          </tr>
        </thead>
        <tbody>
          {container.mount_points.map((mount, index) => (
            <tr
              key={`${mount.destination}-${index}`}
              className="border-b border-border/50 last:border-0"
            >
              <td className="px-3 py-1.5">{mount.type}</td>
              <td className="break-all px-3 py-1.5 font-mono text-xs">
                {mount.name || mount.source}
              </td>
              <td className="break-all px-3 py-1.5 font-mono text-xs">{mount.destination}</td>
              <td className="px-3 py-1.5 text-muted">{mount.rw ? 'rw' : 'ro'}</td>
            </tr>
          ))}
        </tbody>
      </table>
    </div>
  );
}

function NetworkTab({ container }: { container: ContainerDetail }) {
  if (container.network_list.length === 0) return <p className="text-sm text-muted">—</p>;

  return (
    <div className="grid gap-3 md:grid-cols-2">
      {container.network_list.map((network) => (
        <section key={network.name} className="card p-4">
          <h3 className="mb-2 text-sm font-medium">{network.name}</h3>
          <dl className="space-y-1.5 text-sm">
            <Row label="IP address" value={network.ip_address || '—'} />
            <Row label="Gateway" value={network.gateway || '—'} />
            <Row label="MAC" value={network.mac_address || '—'} />
            {network.aliases && network.aliases.length > 0 && (
              <Row label="Aliases" value={network.aliases.join(', ')} />
            )}
          </dl>
        </section>
      ))}
    </div>
  );
}

function Row({ label, value }: { label: string; value: React.ReactNode }) {
  return (
    <div className="flex items-start justify-between gap-4 border-b border-border/40 py-1 last:border-0">
      <dt className="shrink-0 text-muted">{label}</dt>
      <dd className="min-w-0 break-all text-right">{value}</dd>
    </div>
  );
}
