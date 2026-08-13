import { useState } from 'react';
import { useQuery } from '@tanstack/react-query';
import { useTranslation } from 'react-i18next';
import { ChevronLeft, ChevronRight, Download, ScrollText, X } from 'lucide-react';

import { audit as auditApi } from '../../api/endpoints';
import type { AuditEntry, AuditQuery } from '../../api/types';
import { PageHeader } from '../../components/PageHeader';
import { EmptyState } from '../../components/EmptyState';
import { ErrorPanel } from '../../components/ErrorPanel';
import { Spinner } from '../../components/Spinner';
import { cn } from '../../lib/cn';
import { formatTime } from '../../lib/format';
import { toast } from '../../stores/toast';

const PAGE_SIZE = 50;

/**
 * The audit trail.
 *
 * It answers one question — who did what, when, and did it work — so the
 * screen is a filter and a table, and nothing here can change a record. The
 * only thing that removes one is the retention sweep, on age alone.
 */
export function AuditPage() {
  const { t } = useTranslation();

  const [filter, setFilter] = useState<AuditQuery>({});
  const [offset, setOffset] = useState(0);
  const [expanded, setExpanded] = useState<number | null>(null);
  const [exporting, setExporting] = useState(false);

  const query = useQuery({
    queryKey: ['audit', filter, offset],
    queryFn: () => auditApi.list({ ...filter, limit: PAGE_SIZE, offset }),
  });
  const facets = useQuery({ queryKey: ['audit', 'facets'], queryFn: auditApi.facets });

  // Changing a filter starts from the first page: staying on page 7 of a
  // result that now has two pages shows an empty screen.
  const narrow = (patch: AuditQuery) => {
    setFilter((current) => ({ ...current, ...patch }));
    setOffset(0);
  };

  const clear = () => {
    setFilter({});
    setOffset(0);
  };

  const download = async (format: 'csv' | 'json') => {
    setExporting(true);
    try {
      await auditApi.export(format, filter);
      toast.success(t('audit.export_started'));
    } catch (err) {
      toast.error(t('audit.export_failed'), err instanceof Error ? err.message : undefined);
    } finally {
      setExporting(false);
    }
  };

  const active = Object.values(filter).some((value) => value !== undefined && value !== '');
  const total = query.data?.total ?? 0;
  const items = query.data?.items ?? [];

  return (
    <>
      <PageHeader
        title={t('audit.title')}
        description={t('audit.count', { count: total })}
        actions={
          <>
            <button
              type="button"
              className="btn-default"
              disabled={exporting}
              onClick={() => void download('csv')}
            >
              <Download size={14} aria-hidden />
              CSV
            </button>
            <button
              type="button"
              className="btn-default"
              disabled={exporting}
              onClick={() => void download('json')}
            >
              <Download size={14} aria-hidden />
              JSON
            </button>
          </>
        }
      />

      <div className="card mb-4 flex flex-wrap items-end gap-3 p-4">
        <Select
          label={t('audit.actor')}
          value={filter.user_id ?? ''}
          onChange={(value) => narrow({ user_id: value || undefined })}
          options={(facets.data?.actors ?? []).map((actor) => ({
            value: actor.user_id ?? '',
            label: actor.username,
          }))}
          anyLabel={t('audit.any_actor')}
        />

        <Select
          label={t('audit.action')}
          value={filter.action ?? ''}
          onChange={(value) => narrow({ action: value || undefined })}
          options={(facets.data?.actions ?? []).map((action) => ({
            value: action,
            label: action,
          }))}
          anyLabel={t('audit.any_action')}
        />

        <Select
          label={t('audit.result')}
          value={filter.result ?? ''}
          onChange={(value) =>
            narrow({ result: (value || undefined) as 'ok' | 'error' | undefined })
          }
          options={[
            { value: 'ok', label: t('audit.result_ok') },
            { value: 'error', label: t('audit.result_error') },
          ]}
          anyLabel={t('audit.any_result')}
        />

        <div>
          <label htmlFor="audit-from" className="mb-1.5 block text-xs text-muted">
            {t('audit.from')}
          </label>
          <input
            id="audit-from"
            type="date"
            className="input w-40"
            value={filter.from ?? ''}
            onChange={(e) => narrow({ from: e.target.value || undefined })}
          />
        </div>

        <div>
          <label htmlFor="audit-to" className="mb-1.5 block text-xs text-muted">
            {t('audit.to')}
          </label>
          <input
            id="audit-to"
            type="date"
            className="input w-40"
            value={filter.to ?? ''}
            onChange={(e) => narrow({ to: e.target.value || undefined })}
          />
        </div>

        {active && (
          <button type="button" className="btn-ghost" onClick={clear}>
            <X size={14} aria-hidden />
            {t('common.clear')}
          </button>
        )}
      </div>

      {query.error ? (
        <ErrorPanel error={query.error} onRetry={() => void query.refetch()} />
      ) : query.isLoading ? (
        <div className="flex justify-center py-16">
          <Spinner />
        </div>
      ) : items.length === 0 ? (
        <EmptyState
          icon={<ScrollText size={32} aria-hidden />}
          title={t('audit.none')}
          description={active ? t('audit.none_match') : t('audit.none_yet')}
        />
      ) : (
        <>
          <div className="card overflow-x-auto">
            <table className="w-full text-sm">
              <thead>
                <tr className="border-b border-border text-left text-xs text-muted">
                  <th className="px-4 py-2 font-medium">{t('audit.when')}</th>
                  <th className="px-4 py-2 font-medium">{t('audit.actor')}</th>
                  <th className="px-4 py-2 font-medium">{t('audit.action')}</th>
                  <th className="px-4 py-2 font-medium">{t('audit.resource')}</th>
                  <th className="px-4 py-2 font-medium">{t('audit.result')}</th>
                </tr>
              </thead>
              <tbody>
                {items.map((entry) => (
                  <Row
                    key={entry.id}
                    entry={entry}
                    expanded={expanded === entry.id}
                    onToggle={() => setExpanded(expanded === entry.id ? null : entry.id)}
                  />
                ))}
              </tbody>
            </table>
          </div>

          <div className="mt-3 flex items-center justify-between gap-4 text-xs text-muted">
            <span>
              {t('audit.showing', {
                first: offset + 1,
                last: offset + items.length,
                total,
              })}
            </span>
            <div className="flex items-center gap-2">
              <button
                type="button"
                className="btn-default"
                disabled={offset === 0}
                onClick={() => setOffset(Math.max(0, offset - PAGE_SIZE))}
              >
                <ChevronLeft size={14} aria-hidden />
                {t('audit.newer')}
              </button>
              <button
                type="button"
                className="btn-default"
                disabled={offset + items.length >= total}
                onClick={() => setOffset(offset + PAGE_SIZE)}
              >
                {t('audit.older')}
                <ChevronRight size={14} aria-hidden />
              </button>
            </div>
          </div>
        </>
      )}
    </>
  );
}

/** One entry, with its detail revealed on click. */
function Row({
  entry,
  expanded,
  onToggle,
}: {
  entry: AuditEntry;
  expanded: boolean;
  onToggle: () => void;
}) {
  const { t } = useTranslation();
  const failed = entry.result === 'error';

  return (
    <>
      <tr
        className="cursor-pointer border-b border-border/50 last:border-0 hover:bg-elevated"
        onClick={onToggle}
      >
        <td className="whitespace-nowrap px-4 py-2 text-xs tabular-nums text-muted">
          {formatTime(entry.created_at)}
        </td>
        <td className="px-4 py-2">{entry.username || <span className="text-muted">—</span>}</td>
        <td className="px-4 py-2 font-mono text-xs">{entry.action}</td>
        <td className="max-w-64 truncate px-4 py-2 font-mono text-xs text-muted">
          {entry.resource_id || entry.resource_type || '—'}
        </td>
        <td className="px-4 py-2">
          <span className={cn('badge', failed ? 'bg-danger/15 text-danger' : 'bg-ok/15 text-ok')}>
            {failed ? t('audit.result_error') : t('audit.result_ok')}
          </span>
        </td>
      </tr>

      {expanded && (
        <tr className="border-b border-border/50 bg-elevated/50">
          <td colSpan={5} className="px-4 py-3">
            <dl className="grid gap-x-6 gap-y-1 text-xs sm:grid-cols-2">
              <Detail label={t('audit.resource_type')} value={entry.resource_type} />
              <Detail label={t('audit.resource_id')} value={entry.resource_id} />
              <Detail label="IP" value={entry.ip} />
              <Detail label={t('audit.user_agent')} value={entry.user_agent} />
            </dl>
            {entry.detail && entry.detail !== '{}' && (
              <pre className="mt-2 overflow-x-auto rounded bg-bg p-2 font-mono text-xs">
                {prettyDetail(entry.detail)}
              </pre>
            )}
          </td>
        </tr>
      )}
    </>
  );
}

function Detail({ label, value }: { label: string; value?: string }) {
  if (!value) return null;
  return (
    <div className="flex gap-2">
      <dt className="shrink-0 text-muted">{label}</dt>
      <dd className="min-w-0 break-all font-mono">{value}</dd>
    </div>
  );
}

/** Re-indents the stored detail. It is JSON, but stored as a string. */
function prettyDetail(detail: string): string {
  try {
    return JSON.stringify(JSON.parse(detail), null, 2);
  } catch {
    // Not JSON after all: showing it verbatim beats showing nothing.
    return detail;
  }
}

/** A labelled dropdown with an "any" option. */
function Select({
  label,
  value,
  onChange,
  options,
  anyLabel,
}: {
  label: string;
  value: string;
  onChange: (value: string) => void;
  options: { value: string; label: string }[];
  anyLabel: string;
}) {
  const id = `audit-${label.toLowerCase().replace(/\s+/g, '-')}`;
  return (
    <div>
      <label htmlFor={id} className="mb-1.5 block text-xs text-muted">
        {label}
      </label>
      <select
        id={id}
        className="input w-48"
        value={value}
        onChange={(e) => onChange(e.target.value)}
      >
        <option value="">{anyLabel}</option>
        {options.map((option) => (
          <option key={option.value} value={option.value}>
            {option.label}
          </option>
        ))}
      </select>
    </div>
  );
}
