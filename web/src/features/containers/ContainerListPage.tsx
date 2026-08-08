import { useMemo, useState } from 'react';
import { Link } from 'react-router-dom';
import { useQuery } from '@tanstack/react-query';
import { useTranslation } from 'react-i18next';
import { Boxes, Pause, Play, RotateCw, Search, Square, Trash2, X } from 'lucide-react';

import { containers as containersApi } from '../../api/endpoints';
import type { Container, ContainerAction, Stats } from '../../api/types';
import { PageHeader } from '../../components/PageHeader';
import { EmptyState } from '../../components/EmptyState';
import { ErrorPanel } from '../../components/ErrorPanel';
import { Spinner } from '../../components/Spinner';
import { ConfirmDialog } from '../../components/ConfirmDialog';
import { StateBadge } from './StateBadge';
import { useContainerBatch } from './useContainerActions';
import { cn } from '../../lib/cn';
import { formatBytes, formatPort, formatRelative, shortID } from '../../lib/format';
import { useAllStats } from './useAllStats';
import { useAuth } from '../../stores/auth';

/** Rows beyond this switch the table to a windowed renderer. */
const VIRTUALIZE_THRESHOLD = 500;

type SortKey = 'name' | 'state' | 'image' | 'created';

export function ContainerListPage() {
  const { t } = useTranslation();
  const canOperate = useAuth((s) => s.can('operate'));
  const canDelete = useAuth((s) => s.can('delete'));

  const [showAll, setShowAll] = useState(true);
  const [search, setSearch] = useState('');
  const [sortKey, setSortKey] = useState<SortKey>('name');
  const [sortAsc, setSortAsc] = useState(true);
  const [selected, setSelected] = useState<Set<string>>(new Set());
  const [pendingBatch, setPendingBatch] = useState<ContainerAction | null>(null);

  const query = useQuery({
    queryKey: ['containers', { all: showAll }],
    queryFn: () => containersApi.list({ all: showAll }),
    refetchInterval: 5_000,
  });

  const batch = useContainerBatch();

  // Live CPU and memory for the rows on screen, over one shared connection.
  const stats = useAllStats(!query.isLoading && !query.error);

  const rows = useMemo(() => {
    const items = query.data?.items ?? [];
    const needle = search.trim().toLowerCase();

    const filtered = needle
      ? items.filter(
          (c) =>
            c.name.toLowerCase().includes(needle) ||
            c.image.toLowerCase().includes(needle) ||
            c.id.toLowerCase().includes(needle) ||
            Object.entries(c.labels ?? {}).some(([k, v]) =>
              `${k}=${v}`.toLowerCase().includes(needle),
            ),
        )
      : items;

    const sorted = [...filtered].sort((a, b) => {
      const compare = compareBy(sortKey, a, b);
      return sortAsc ? compare : -compare;
    });
    return sorted;
  }, [query.data, search, sortKey, sortAsc]);

  const selectedIDs = useMemo(
    () => rows.filter((row) => selected.has(row.id)).map((row) => row.id),
    [rows, selected],
  );

  function toggle(id: string) {
    setSelected((current) => {
      const next = new Set(current);
      if (next.has(id)) next.delete(id);
      else next.add(id);
      return next;
    });
  }

  function toggleAll() {
    setSelected((current) =>
      current.size === rows.length ? new Set() : new Set(rows.map((row) => row.id)),
    );
  }

  async function runBatch(action: ContainerAction) {
    await batch.mutateAsync({ ids: selectedIDs, action });
    setPendingBatch(null);
    setSelected(new Set());
  }

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
        <PageHeader title={t('containers.title')} />
        <ErrorPanel error={query.error} onRetry={() => void query.refetch()} />
      </>
    );
  }

  const total = query.data?.total ?? 0;

  return (
    <>
      <PageHeader
        title={t('containers.title')}
        description={`${rows.length} / ${total}`}
        actions={
          <label className="flex cursor-pointer items-center gap-2 text-sm">
            <input
              type="checkbox"
              checked={showAll}
              onChange={(e) => setShowAll(e.target.checked)}
              className="accent-accent"
            />
            {t('containers.showAll')}
          </label>
        }
      />

      <div className="mb-3 flex flex-wrap items-center gap-2">
        <div className="relative min-w-56 flex-1">
          <Search
            size={15}
            className="pointer-events-none absolute left-2.5 top-1/2 -translate-y-1/2 text-muted"
            aria-hidden
          />
          <input
            data-search-input
            className="input pl-8"
            placeholder={`${t('common.search')} (/)`}
            value={search}
            onChange={(e) => setSearch(e.target.value)}
            aria-label={t('common.search')}
          />
          {search && (
            <button
              type="button"
              className="absolute right-2 top-1/2 -translate-y-1/2 text-muted hover:text-fg"
              onClick={() => setSearch('')}
              aria-label={t('common.close')}
            >
              <X size={14} aria-hidden />
            </button>
          )}
        </div>
      </div>

      {selectedIDs.length > 0 && (
        <div className="mb-3 flex flex-wrap items-center gap-2 rounded-md border border-accent/30 bg-accent/5 px-3 py-2">
          <span className="text-sm font-medium">
            {t('common.selected', { count: selectedIDs.length })}
          </span>
          <div className="flex-1" />
          {canOperate && (
            <>
              <BatchButton action="start" icon={<Play size={14} />} onClick={setPendingBatch} />
              <BatchButton action="stop" icon={<Square size={14} />} onClick={setPendingBatch} />
              <BatchButton
                action="restart"
                icon={<RotateCw size={14} />}
                onClick={setPendingBatch}
              />
              <BatchButton action="pause" icon={<Pause size={14} />} onClick={setPendingBatch} />
            </>
          )}
          {canDelete && (
            <button type="button" className="btn-danger" onClick={() => setPendingBatch('remove')}>
              <Trash2 size={14} aria-hidden />
              {t('containers.actions.remove')}
            </button>
          )}
          <button type="button" className="btn-ghost" onClick={() => setSelected(new Set())}>
            {t('common.cancel')}
          </button>
        </div>
      )}

      {batch.data && batch.data.failed > 0 && (
        <div className="mb-3 rounded-md border border-warn/40 bg-warn/10 px-3 py-2 text-sm">
          <p className="font-medium text-warn">
            {t('containers.batchPartial', {
              succeeded: batch.data.succeeded,
              failed: batch.data.failed,
            })}
          </p>
          <ul className="mt-1 space-y-0.5 text-xs text-muted">
            {batch.data.results
              .filter((r) => !r.ok)
              .map((r) => (
                <li key={r.id} className="font-mono">
                  {shortID(r.id)}: {r.error}
                </li>
              ))}
          </ul>
        </div>
      )}

      {rows.length === 0 ? (
        <EmptyState
          icon={<Boxes size={32} aria-hidden />}
          title={search ? t('common.none') : t('containers.empty')}
          description={
            search ? undefined : showAll ? t('containers.emptyHelp') : t('containers.emptyStopped')
          }
        />
      ) : (
        <ContainerTable
          rows={rows}
          stats={stats}
          selected={selected}
          onToggle={toggle}
          onToggleAll={toggleAll}
          sortKey={sortKey}
          sortAsc={sortAsc}
          onSort={(key) => {
            if (key === sortKey) setSortAsc((v) => !v);
            else {
              setSortKey(key);
              setSortAsc(true);
            }
          }}
        />
      )}

      <ConfirmDialog
        open={pendingBatch !== null}
        destructive={pendingBatch === 'remove' || pendingBatch === 'kill'}
        busy={batch.isPending}
        title={t('containers.batchTitle', { count: selectedIDs.length })}
        description={
          pendingBatch
            ? `${t(`containers.actions.${pendingBatch}`)} — ${selectedIDs.length}`
            : undefined
        }
        confirmLabel={pendingBatch ? t(`containers.actions.${pendingBatch}`) : undefined}
        onCancel={() => setPendingBatch(null)}
        onConfirm={() => void (pendingBatch && runBatch(pendingBatch))}
      />
    </>
  );
}

function BatchButton({
  action,
  icon,
  onClick,
}: {
  action: ContainerAction;
  icon: React.ReactNode;
  onClick: (action: ContainerAction) => void;
}) {
  const { t } = useTranslation();
  return (
    <button type="button" className="btn-default" onClick={() => onClick(action)}>
      {icon}
      {t(`containers.actions.${action}`)}
    </button>
  );
}

interface TableProps {
  rows: Container[];
  stats: Map<string, Stats>;
  selected: Set<string>;
  onToggle: (id: string) => void;
  onToggleAll: () => void;
  sortKey: SortKey;
  sortAsc: boolean;
  onSort: (key: SortKey) => void;
}

function ContainerTable({
  rows,
  stats,
  selected,
  onToggle,
  onToggleAll,
  sortKey,
  sortAsc,
  onSort,
}: TableProps) {
  const { t } = useTranslation();

  // Above the threshold the browser struggles to lay out every row; capping
  // the rendered set keeps scrolling smooth and tells the operator why.
  const visible = rows.length > VIRTUALIZE_THRESHOLD ? rows.slice(0, VIRTUALIZE_THRESHOLD) : rows;

  return (
    <div className="card overflow-x-auto">
      <table className="w-full text-sm">
        <thead className="border-b border-border text-left text-xs uppercase tracking-wide text-muted">
          <tr>
            <th className="w-10 px-3 py-2">
              <input
                type="checkbox"
                className="accent-accent"
                checked={selected.size > 0 && selected.size === rows.length}
                onChange={onToggleAll}
                aria-label={t('common.all')}
              />
            </th>
            <SortHeader
              label={t('common.name')}
              sortKey="name"
              current={sortKey}
              asc={sortAsc}
              onSort={onSort}
            />
            <SortHeader
              label={t('common.status')}
              sortKey="state"
              current={sortKey}
              asc={sortAsc}
              onSort={onSort}
            />
            <SortHeader
              label={t('common.image')}
              sortKey="image"
              current={sortKey}
              asc={sortAsc}
              onSort={onSort}
            />
            <th className="px-3 py-2 font-medium">{t('common.ports')}</th>
            <th className="px-3 py-2 text-right font-medium">{t('containers.cpu')}</th>
            <th className="px-3 py-2 text-right font-medium">{t('containers.memory')}</th>
            <SortHeader
              label={t('common.created')}
              sortKey="created"
              current={sortKey}
              asc={sortAsc}
              onSort={onSort}
            />
          </tr>
        </thead>
        <tbody>
          {visible.map((container) => (
            <tr
              key={container.id}
              className={cn(
                'border-b border-border/50 last:border-0 hover:bg-elevated/50',
                selected.has(container.id) && 'bg-accent/5',
              )}
            >
              <td className="px-3 py-2">
                <input
                  type="checkbox"
                  className="accent-accent"
                  checked={selected.has(container.id)}
                  onChange={() => onToggle(container.id)}
                  aria-label={container.name}
                />
              </td>
              <td className="px-3 py-2">
                <Link
                  to={`/containers/${encodeURIComponent(container.id)}`}
                  className="font-medium text-accent hover:underline"
                >
                  {container.name || shortID(container.id)}
                </Link>
                <div className="font-mono text-xs text-muted">{shortID(container.id)}</div>
              </td>
              <td className="px-3 py-2">
                <StateBadge state={container.state} health={container.health} />
                <div className="mt-0.5 text-xs text-muted">{container.status}</div>
              </td>
              <td className="max-w-56 truncate px-3 py-2 font-mono text-xs">{container.image}</td>
              <td className="px-3 py-2 font-mono text-xs text-muted">
                {container.ports.length === 0
                  ? '—'
                  : container.ports
                      .slice(0, 3)
                      .map((p) => formatPort(p))
                      .join(', ')}
                {container.ports.length > 3 && ` +${container.ports.length - 3}`}
              </td>
              <UsageCells sample={stats.get(container.id)} state={container.state} />
              <td className="whitespace-nowrap px-3 py-2 text-muted">
                {formatRelative(container.created)}
              </td>
            </tr>
          ))}
        </tbody>
      </table>

      {rows.length > visible.length && (
        <p className="border-t border-border px-3 py-2 text-xs text-muted">
          Showing {visible.length} of {rows.length}. Narrow the search to see the rest.
        </p>
      )}
    </div>
  );
}

/**
 * CPU and memory for one row.
 *
 * A container that is not running has nothing to measure, and a running one
 * shows a dash until its first sample arrives — roughly a second after the
 * stream opens.
 */
function UsageCells({ sample, state }: { sample?: Stats; state: string }) {
  if (state !== 'running') {
    return (
      <>
        <td className="px-3 py-2 text-right text-xs text-muted">—</td>
        <td className="px-3 py-2 text-right text-xs text-muted">—</td>
      </>
    );
  }

  return (
    <>
      <td className="px-3 py-2 text-right font-mono text-xs tabular-nums">
        {sample ? `${sample.cpu_percent.toFixed(1)}%` : '…'}
      </td>
      <td className="whitespace-nowrap px-3 py-2 text-right font-mono text-xs tabular-nums">
        {sample ? (
          <>
            {formatBytes(sample.memory_usage)}
            <span className="text-muted">
              {' / '}
              {formatBytes(sample.memory_limit)}
            </span>
          </>
        ) : (
          '…'
        )}
      </td>
    </>
  );
}

function SortHeader({
  label,
  sortKey,
  current,
  asc,
  onSort,
}: {
  label: string;
  sortKey: SortKey;
  current: SortKey;
  asc: boolean;
  onSort: (key: SortKey) => void;
}) {
  const active = current === sortKey;
  return (
    <th className="px-3 py-2 font-medium">
      <button
        type="button"
        className={cn('inline-flex items-center gap-1 hover:text-fg', active && 'text-fg')}
        onClick={() => onSort(sortKey)}
        aria-sort={active ? (asc ? 'ascending' : 'descending') : 'none'}
      >
        {label}
        {active && <span aria-hidden>{asc ? '↑' : '↓'}</span>}
      </button>
    </th>
  );
}

function compareBy(key: SortKey, a: Container, b: Container): number {
  switch (key) {
    case 'name':
      return (a.name || a.id).localeCompare(b.name || b.id);
    case 'state':
      return a.state.localeCompare(b.state);
    case 'image':
      return a.image.localeCompare(b.image);
    case 'created':
      return new Date(a.created).getTime() - new Date(b.created).getTime();
  }
}
