import { useCallback, useEffect, useMemo, useRef, useState } from 'react';
import { Link, useNavigate, useParams } from 'react-router-dom';
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import { useTranslation } from 'react-i18next';
import {
  AlertTriangle,
  Download,
  Pencil,
  Play,
  RotateCw,
  Square,
  Trash2,
  Upload,
} from 'lucide-react';

import { stacks as stacksApi } from '../../api/endpoints';
import type { StackDetail, StackServiceStatus } from '../../api/types';
import { PageHeader } from '../../components/PageHeader';
import { ConfirmDialog } from '../../components/ConfirmDialog';
import { ErrorPanel } from '../../components/ErrorPanel';
import { Spinner } from '../../components/Spinner';
import { StateBadge } from '../containers/StateBadge';
import { useAuth } from '../../stores/auth';
import { formatRelative, formatPort } from '../../lib/format';
import { useStackStream } from './useStackStream';
import { useStackLogs } from './useStackLogs';

type Tab = 'services' | 'logs' | 'compose';

/** One stack: what it is running, what a deploy would do, and its output. */
export function StackDetailPage() {
  const { t } = useTranslation();
  const navigate = useNavigate();
  const queryClient = useQueryClient();
  const { id = '' } = useParams<{ id: string }>();

  const canOperate = useAuth((s) => s.can('operate'));
  const canDelete = useAuth((s) => s.can('delete'));
  const canCreate = useAuth((s) => s.can('create'));

  const [tab, setTab] = useState<Tab>('services');
  const [removing, setRemoving] = useState(false);
  const [takingDown, setTakingDown] = useState(false);
  const [withVolumes, setWithVolumes] = useState(false);

  const refresh = useCallback(
    () => queryClient.invalidateQueries({ queryKey: ['stacks'] }),
    [queryClient],
  );

  const query = useQuery({
    queryKey: ['stacks', id],
    queryFn: () => stacksApi.get(id),
    enabled: id !== '',
    // A deploy started from another tab should show up without a manual reload.
    refetchInterval: 10_000,
  });

  const stream = useStackStream(() => void refresh());

  const act = useMutation({
    mutationFn: (action: 'stop' | 'start' | 'restart') => stacksApi.act(id, action),
    onSuccess: () => void refresh(),
  });

  const down = useMutation({
    mutationFn: () => stacksApi.down(id, { volumes: withVolumes }),
    onSuccess: () => {
      setTakingDown(false);
      setWithVolumes(false);
      void refresh();
    },
  });

  const remove = useMutation({
    mutationFn: () => stacksApi.remove(id),
    onSuccess: () => {
      void refresh();
      navigate('/stacks');
    },
  });

  if (query.isLoading) {
    return (
      <div className="flex justify-center py-16">
        <Spinner />
      </div>
    );
  }
  if (query.error) {
    return <ErrorPanel error={query.error} onRetry={() => void query.refetch()} />;
  }

  const stack = query.data as StackDetail;
  const deploying = stream.state.phase === 'running';

  return (
    <>
      <PageHeader
        title={stack.name}
        description={t(`stacks.status_${stack.status}`)}
        actions={
          <>
            {canOperate && (
              <button
                type="button"
                className="btn-primary"
                disabled={deploying}
                onClick={() => void stream.start(`/stacks/${encodeURIComponent(id)}/up`)}
              >
                <Upload size={14} aria-hidden />
                {t('stacks.deploy')}
              </button>
            )}
            {canOperate && (
              <button
                type="button"
                className="btn-default"
                disabled={deploying}
                onClick={() => void stream.start(`/stacks/${encodeURIComponent(id)}/pull`)}
              >
                <Download size={14} aria-hidden />
                {t('stacks.pull')}
              </button>
            )}
            {canCreate && (
              <Link className="btn-default" to={`/stacks/${encodeURIComponent(id)}/edit`}>
                <Pencil size={14} aria-hidden />
                {t('common.edit')}
              </Link>
            )}
          </>
        }
      />

      {stack.parse_error && (
        <div className="mb-4 rounded border border-danger/40 bg-danger/10 p-3">
          <p className="text-sm font-medium text-danger">{t('stacks.parse_error')}</p>
          <pre className="mt-1 whitespace-pre-wrap break-words font-mono text-xs">
            {stack.parse_error}
          </pre>
        </div>
      )}

      {stack.last_error && !stack.parse_error && (
        <div className="mb-4 rounded border border-danger/40 bg-danger/10 p-3 text-sm text-danger">
          {stack.last_error}
        </div>
      )}

      {/* The definition still arrived; only the live state is missing. */}
      {stack.engine_error && (
        <div className="mb-4 rounded border border-warn/40 bg-warn/10 p-3 text-sm">
          {t('stacks.engine_error')}
          <span className="mt-1 block text-xs text-muted">{stack.engine_error}</span>
        </div>
      )}

      {canOperate && (
        <div className="mb-4 flex flex-wrap gap-2">
          <button
            type="button"
            className="btn-default"
            disabled={act.isPending}
            onClick={() => act.mutate('start')}
          >
            <Play size={14} aria-hidden />
            {t('containers.start')}
          </button>
          <button
            type="button"
            className="btn-default"
            disabled={act.isPending}
            onClick={() => act.mutate('stop')}
          >
            <Square size={14} aria-hidden />
            {t('containers.stop')}
          </button>
          <button
            type="button"
            className="btn-default"
            disabled={act.isPending}
            onClick={() => act.mutate('restart')}
          >
            <RotateCw size={14} aria-hidden />
            {t('containers.restart')}
          </button>
          {canDelete && (
            <>
              <button type="button" className="btn-default" onClick={() => setTakingDown(true)}>
                {t('stacks.down')}
              </button>
              <button
                type="button"
                className="btn-ghost text-muted hover:text-danger"
                onClick={() => setRemoving(true)}
              >
                <Trash2 size={14} aria-hidden />
                {t('stacks.forget')}
              </button>
            </>
          )}
        </div>
      )}

      {act.error && (
        <div className="mb-4">
          <ErrorPanel error={act.error} />
        </div>
      )}

      {stream.state.phase !== 'idle' && <DeployLog stream={stream} />}

      {stack.warnings.length > 0 && (
        <div className="card mb-4 p-4">
          <h3 className="mb-2 text-sm font-semibold">{t('stacks.warnings')}</h3>
          <ul className="space-y-1.5">
            {stack.warnings.map((warning, index) => (
              <li key={`${warning.field}-${index}`} className="flex gap-2 text-xs">
                <AlertTriangle size={14} className="mt-0.5 shrink-0 text-warn" aria-hidden />
                <span>
                  <span className="font-mono">
                    {warning.service ? `${warning.service}.` : ''}
                    {warning.field}
                  </span>
                  <span className="block text-muted">{warning.message}</span>
                </span>
              </li>
            ))}
          </ul>
        </div>
      )}

      <div className="mb-3 flex gap-1 border-b border-border">
        {(['services', 'logs', 'compose'] as Tab[]).map((key) => (
          <button
            key={key}
            type="button"
            className={`-mb-px border-b-2 px-3 py-2 text-sm ${
              tab === key
                ? 'border-accent font-medium text-accent'
                : 'border-transparent text-muted hover:text-fg'
            }`}
            onClick={() => setTab(key)}
          >
            {t(`stacks.tab_${key}`)}
          </button>
        ))}
      </div>

      {tab === 'services' && <ServiceTable stack={stack} />}
      {tab === 'logs' && <StackLogs stackID={id} services={stack.services} />}
      {tab === 'compose' && (
        <pre className="card overflow-x-auto p-4 font-mono text-xs leading-relaxed">
          {stack.compose}
        </pre>
      )}

      <ConfirmDialog
        open={takingDown}
        title={t('stacks.down_title', { name: stack.name })}
        description={t('stacks.down_hint')}
        confirmLabel={t('stacks.down')}
        destructive
        busy={down.isPending}
        onCancel={() => setTakingDown(false)}
        onConfirm={() => down.mutate()}
      >
        <label className="flex items-start gap-2 text-sm">
          <input
            type="checkbox"
            className="mt-0.5 accent-accent"
            checked={withVolumes}
            onChange={(e) => setWithVolumes(e.target.checked)}
          />
          <span>
            <span className="font-medium">{t('stacks.down_volumes')}</span>
            <span className="block text-xs text-muted">{t('stacks.down_volumes_hint')}</span>
          </span>
        </label>
      </ConfirmDialog>

      <ConfirmDialog
        open={removing}
        title={t('stacks.forget_title', { name: stack.name })}
        description={t('stacks.forget_hint')}
        confirmText={stack.name}
        confirmLabel={t('stacks.forget')}
        destructive
        busy={remove.isPending}
        onCancel={() => setRemoving(false)}
        onConfirm={() => remove.mutate()}
      />
    </>
  );
}

/** The live output of a deploy or a pull. */
function DeployLog({ stream }: { stream: ReturnType<typeof useStackStream> }) {
  const { t } = useTranslation();
  const ref = useRef<HTMLDivElement>(null);

  useEffect(() => {
    const node = ref.current;
    if (node) node.scrollTop = node.scrollHeight;
  }, [stream.state.lines]);

  return (
    <div className="card mb-4 p-4">
      <div className="flex items-start justify-between gap-3">
        <h3 className="text-sm font-semibold">{t('stacks.deploy_output')}</h3>
        {stream.state.phase !== 'running' && (
          <button type="button" className="btn-ghost" onClick={stream.reset}>
            {t('common.clear')}
          </button>
        )}
      </div>

      <div
        ref={ref}
        className="mt-2 max-h-64 overflow-auto rounded border border-border bg-bg p-3 font-mono text-xs leading-relaxed"
      >
        {stream.state.lines.map((line) => (
          <div
            key={line.id}
            className={line.kind === 'warn' ? 'text-warn' : line.kind === 'log' ? 'text-muted' : ''}
          >
            {line.service ? `${line.service}: ` : ''}
            {line.message}
          </div>
        ))}
      </div>

      {stream.state.error && <p className="mt-2 text-xs text-danger">{stream.state.error}</p>}

      {stream.state.problems.length > 0 && (
        <ul className="mt-2 space-y-1">
          {stream.state.problems.map((problem, index) => (
            <li key={`${problem.service}-${index}`} className="text-xs text-danger">
              <span className="font-mono">
                {problem.service}.{problem.field}
              </span>{' '}
              {problem.message}
            </li>
          ))}
        </ul>
      )}
    </div>
  );
}

/** What each service is running right now. */
function ServiceTable({ stack }: { stack: StackDetail }) {
  const { t } = useTranslation();

  if (stack.services.length === 0) {
    return <p className="card p-4 text-sm text-muted">{t('stacks.no_services')}</p>;
  }

  return (
    <div className="card overflow-x-auto">
      <table className="w-full text-sm">
        <thead className="border-b border-border text-left text-xs uppercase tracking-wide text-muted">
          <tr>
            <th className="px-3 py-2 font-medium">{t('stacks.service')}</th>
            <th className="px-3 py-2 font-medium">{t('common.image')}</th>
            <th className="px-3 py-2 font-medium">{t('stacks.replicas')}</th>
            <th className="px-3 py-2 font-medium">{t('common.ports')}</th>
            <th className="px-3 py-2 font-medium">{t('stacks.containers')}</th>
          </tr>
        </thead>
        <tbody>
          {stack.services.map((service) => (
            <ServiceRow key={service.name} service={service} />
          ))}
        </tbody>
      </table>
    </div>
  );
}

function ServiceRow({ service }: { service: StackServiceStatus }) {
  const { t } = useTranslation();

  const ports = useMemo(
    () => (service.ports ?? []).map((port) => formatPort(port)).filter(Boolean),
    [service.ports],
  );

  return (
    <tr className="border-b border-border/50 last:border-0 align-top">
      <td className="px-3 py-2 font-medium">
        {service.name}
        {service.drifted && <span className="block text-xs text-warn">{t('stacks.drifted')}</span>}
      </td>
      <td className="max-w-64 px-3 py-2 font-mono text-xs text-muted">
        <span className="block truncate">{service.image ?? '—'}</span>
      </td>
      <td className="whitespace-nowrap px-3 py-2 text-muted">
        {t('stacks.running_of', { running: service.running, total: service.replicas })}
      </td>
      <td className="px-3 py-2 font-mono text-xs text-muted">
        {ports.length > 0 ? ports.join(', ') : '—'}
      </td>
      <td className="px-3 py-2">
        <div className="space-y-1">
          {service.containers.map((container) => (
            <div key={container.id} className="flex items-center gap-2 text-xs">
              <StateBadge state={container.state} health={container.health} />
              <Link
                to={`/containers/${encodeURIComponent(container.id)}`}
                className="font-mono hover:text-accent"
              >
                {container.name}
              </Link>
              <span className="text-muted">{formatRelative(container.created)}</span>
            </div>
          ))}
          {service.containers.length === 0 && <span className="text-xs text-muted">—</span>}
        </div>
      </td>
    </tr>
  );
}

/** Every service's output on one stream. */
function StackLogs({ stackID, services }: { stackID: string; services: StackServiceStatus[] }) {
  const { t } = useTranslation();
  const [selected, setSelected] = useState<string[]>([]);
  const [timestamps, setTimestamps] = useState(false);
  const ref = useRef<HTMLDivElement>(null);
  const pinned = useRef(true);

  const { entries, state, error, clear } = useStackLogs({
    stackID,
    services: selected,
    tail: 200,
    timestamps,
    enabled: true,
  });

  useEffect(() => {
    const node = ref.current;
    if (node && pinned.current) node.scrollTop = node.scrollHeight;
  }, [entries]);

  const toggle = (name: string) =>
    setSelected((current) =>
      current.includes(name) ? current.filter((item) => item !== name) : [...current, name],
    );

  return (
    <div className="card p-4">
      <div className="mb-3 flex flex-wrap items-center gap-2">
        {services.map((service) => (
          <button
            key={service.name}
            type="button"
            className={selected.includes(service.name) ? 'btn-primary' : 'btn-default'}
            onClick={() => toggle(service.name)}
          >
            {service.name}
          </button>
        ))}

        <div className="flex-1" />

        <label className="flex cursor-pointer items-center gap-2 text-sm">
          <input
            type="checkbox"
            className="accent-accent"
            checked={timestamps}
            onChange={(e) => setTimestamps(e.target.checked)}
          />
          {t('logs.timestamps')}
        </label>
        <button type="button" className="btn-ghost" onClick={clear}>
          {t('common.clear')}
        </button>
      </div>

      <div
        ref={ref}
        onScroll={() => {
          const node = ref.current;
          if (node) {
            pinned.current = node.scrollHeight - node.scrollTop - node.clientHeight < 40;
          }
        }}
        className="h-96 overflow-auto rounded border border-border bg-bg p-3 font-mono text-xs leading-relaxed"
      >
        {entries.length === 0 ? (
          <p className="text-muted">{t('logs.empty')}</p>
        ) : (
          entries.map((entry) => (
            <div key={entry.id} className={entry.stream === 'stderr' ? 'text-danger' : ''}>
              <span className="text-accent">{entry.service}</span>
              {entry.timestamp && <span className="text-muted"> {entry.timestamp}</span>}{' '}
              <span className="whitespace-pre-wrap break-all">{entry.message}</span>
            </div>
          ))
        )}
      </div>

      {error && <p className="mt-2 text-xs text-danger">{error}</p>}
      {state === 'connecting' && <p className="mt-2 text-xs text-muted">{t('common.loading')}</p>}
    </div>
  );
}
