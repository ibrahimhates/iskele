import { useState } from 'react';
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import { useTranslation } from 'react-i18next';
import { Activity, CheckCircle2, CircleSlash, Loader2, X, XCircle } from 'lucide-react';

import { tasks as tasksApi } from '../../api/endpoints';
import type { Task } from '../../api/types';
import { formatRelative } from '../../lib/format';

/** How often the drawer refreshes while something is running. */
const ACTIVE_POLL_MS = 1000;
/** And while nothing is: the badge still has to notice a new task. */
const IDLE_POLL_MS = 15_000;

/**
 * The global drawer for long-running operations.
 *
 * A pull started on the images page keeps running when the operator navigates
 * away — it is a server-side task, not a page's state — so there has to be one
 * place that shows what is still in flight and can stop it.
 */
export function TaskDrawer() {
  const { t } = useTranslation();
  const [open, setOpen] = useState(false);
  const queryClient = useQueryClient();

  const query = useQuery({
    queryKey: ['tasks'],
    queryFn: tasksApi.list,
    refetchInterval: (q) => {
      const items = q.state.data?.items ?? [];
      return items.some((task) => task.state === 'running') ? ACTIVE_POLL_MS : IDLE_POLL_MS;
    },
  });

  const cancel = useMutation({
    mutationFn: (id: string) => tasksApi.cancel(id),
    onSettled: () => queryClient.invalidateQueries({ queryKey: ['tasks'] }),
  });

  const items = query.data?.items ?? [];
  const running = items.filter((task) => task.state === 'running').length;

  return (
    <>
      <button
        type="button"
        className="btn-ghost relative"
        onClick={() => setOpen((v) => !v)}
        aria-label={t('tasks.title')}
        aria-expanded={open}
      >
        <Activity size={18} aria-hidden />
        {running > 0 && (
          <span className="absolute -right-0.5 -top-0.5 flex h-4 min-w-4 items-center justify-center rounded-full bg-accent px-1 text-[10px] font-semibold text-accent-fg">
            {running}
          </span>
        )}
      </button>

      {open && (
        <div className="absolute right-4 top-14 z-40 w-96 max-w-[calc(100vw-2rem)]">
          <div className="card shadow-lg">
            <div className="flex items-center justify-between border-b border-border px-3 py-2">
              <h2 className="text-sm font-semibold">{t('tasks.title')}</h2>
              <button
                type="button"
                className="btn-ghost p-1"
                onClick={() => setOpen(false)}
                aria-label={t('common.close')}
              >
                <X size={16} aria-hidden />
              </button>
            </div>

            {items.length === 0 ? (
              <p className="px-3 py-6 text-center text-xs text-muted">{t('tasks.empty')}</p>
            ) : (
              <ul className="max-h-96 divide-y divide-border overflow-auto">
                {items.map((task) => (
                  <TaskRow
                    key={task.id}
                    task={task}
                    onCancel={() => cancel.mutate(task.id)}
                    busy={cancel.isPending && cancel.variables === task.id}
                  />
                ))}
              </ul>
            )}
          </div>
        </div>
      )}
    </>
  );
}

function TaskRow({ task, onCancel, busy }: { task: Task; onCancel: () => void; busy: boolean }) {
  const { t } = useTranslation();

  return (
    <li className="space-y-1.5 px-3 py-2.5">
      <div className="flex items-start justify-between gap-2">
        <div className="min-w-0">
          <p className="truncate font-mono text-xs">{task.target}</p>
          <p className="text-[11px] text-muted">
            {task.kind} · {formatRelative(task.started_at)}
            {task.username ? ` · ${task.username}` : ''}
          </p>
        </div>
        <StateIcon state={task.state} />
      </div>

      {task.state === 'running' && (
        <div className="h-1.5 overflow-hidden rounded-full bg-elevated">
          <div
            className={`h-full bg-accent transition-[width] ${
              task.progress < 0 ? 'w-1/3 animate-pulse' : ''
            }`}
            style={task.progress >= 0 ? { width: `${task.progress}%` } : undefined}
            role="progressbar"
            aria-valuenow={task.progress >= 0 ? task.progress : undefined}
            aria-valuemin={0}
            aria-valuemax={100}
            aria-label={task.target}
          />
        </div>
      )}

      {task.message && task.state === 'running' && (
        <p className="truncate text-[11px] text-muted">{task.message}</p>
      )}
      {task.error && <p className="text-[11px] text-danger">{task.error}</p>}

      {task.cancelable && (
        <button type="button" className="btn-ghost py-1 text-xs" disabled={busy} onClick={onCancel}>
          {t('common.cancel')}
        </button>
      )}
    </li>
  );
}

function StateIcon({ state }: { state: Task['state'] }) {
  const { t } = useTranslation();

  switch (state) {
    case 'running':
      return (
        <Loader2 size={16} className="animate-spin text-accent" aria-label={t('tasks.running')} />
      );
    case 'succeeded':
      return <CheckCircle2 size={16} className="text-ok" aria-label={t('tasks.succeeded')} />;
    case 'failed':
      return <XCircle size={16} className="text-danger" aria-label={t('tasks.failed')} />;
    case 'canceled':
      return <CircleSlash size={16} className="text-muted" aria-label={t('tasks.canceled')} />;
  }
}
