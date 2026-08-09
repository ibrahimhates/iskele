import { useState } from 'react';
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import { useTranslation } from 'react-i18next';
import { FileText, Hammer, Rocket, X } from 'lucide-react';
import { useNavigate } from 'react-router-dom';

import { builds as buildsApi } from '../../api/endpoints';
import type { Build, BuildStatus } from '../../api/types';
import { EmptyState } from '../../components/EmptyState';
import { ErrorPanel } from '../../components/ErrorPanel';
import { Spinner } from '../../components/Spinner';
import { useAuth } from '../../stores/auth';
import { formatBytes, formatDuration, formatRelative, shortID } from '../../lib/format';

const STATUS_CLASS: Record<BuildStatus, string> = {
  running: 'text-accent',
  success: 'text-ok',
  failed: 'text-danger',
  canceled: 'text-muted',
};

/**
 * The build history, and the way back into a build that has already finished.
 *
 * A build outlives the socket that watched it, so this is not a convenience:
 * it is how an operator who closed the tab finds out what happened.
 */
export function BuildHistory() {
  const { t } = useTranslation();
  const queryClient = useQueryClient();
  const navigate = useNavigate();
  const canBuild = useAuth((s) => s.can('build'));

  const [replaying, setReplaying] = useState<Build | null>(null);

  const query = useQuery({
    queryKey: ['builds'],
    queryFn: () => buildsApi.list({ limit: 50 }),
    // A build started from another tab, or still running from this one, should
    // not need a manual refresh to show its outcome.
    refetchInterval: 10_000,
  });

  const cancel = useMutation({
    mutationFn: (build: Build) => buildsApi.cancel(build.id),
    onSuccess: () => void queryClient.invalidateQueries({ queryKey: ['builds'] }),
  });

  if (query.isLoading) {
    return (
      <div className="flex justify-center py-12">
        <Spinner />
      </div>
    );
  }

  if (query.error) {
    return <ErrorPanel error={query.error} onRetry={() => void query.refetch()} />;
  }

  const items = query.data?.items ?? [];

  if (items.length === 0) {
    return (
      <EmptyState
        icon={<Hammer size={32} aria-hidden />}
        title={t('build.history_empty')}
        description={t('build.history_empty_hint')}
      />
    );
  }

  return (
    <>
      <div className="card overflow-x-auto">
        <table className="w-full text-sm">
          <thead className="border-b border-border text-left text-xs uppercase tracking-wide text-muted">
            <tr>
              <th className="px-3 py-2 font-medium">{t('common.status')}</th>
              <th className="px-3 py-2 font-medium">{t('build.tags')}</th>
              <th className="px-3 py-2 font-medium">{t('build.context')}</th>
              <th className="px-3 py-2 font-medium">{t('build.started')}</th>
              <th className="px-3 py-2 text-right font-medium">{t('build.duration')}</th>
              <th className="px-3 py-2" />
            </tr>
          </thead>
          <tbody>
            {items.map((build) => {
              const runTag = build.tags[0];
              return (
                <tr key={build.id} className="border-b border-border/50 last:border-0">
                  <td
                    className={`whitespace-nowrap px-3 py-2 font-medium ${STATUS_CLASS[build.status]}`}
                  >
                    {t(`build.status_${build.status}`)}
                  </td>
                  <td className="px-3 py-2">
                    {build.tags.length > 0 ? (
                      <span className="font-mono text-xs">{build.tags.join(', ')}</span>
                    ) : build.image_id ? (
                      <span className="font-mono text-xs text-muted">
                        {shortID(build.image_id)}
                      </span>
                    ) : (
                      <span className="text-muted">—</span>
                    )}
                    {build.error && (
                      <span className="block max-w-96 truncate text-xs text-danger">
                        {build.error}
                      </span>
                    )}
                  </td>
                  <td className="max-w-72 px-3 py-2 font-mono text-xs text-muted">
                    <span className="block truncate">{build.context_dir}</span>
                    {build.context_files > 0 && (
                      <span className="block text-xs">
                        {t('build.context_stats', {
                          files: build.context_files,
                          size: formatBytes(build.context_bytes),
                        })}
                      </span>
                    )}
                  </td>
                  <td className="whitespace-nowrap px-3 py-2 text-muted">
                    {formatRelative(build.started_at)}
                    {build.username && <span className="block text-xs">{build.username}</span>}
                  </td>
                  <td className="whitespace-nowrap px-3 py-2 text-right text-muted">
                    {formatDuration(build.duration_ms)}
                  </td>
                  <td className="whitespace-nowrap px-3 py-2 text-right">
                    <div className="flex justify-end gap-1">
                      {build.log_archived && (
                        <button
                          type="button"
                          className="btn-ghost"
                          onClick={() => setReplaying(build)}
                          title={t('build.replay')}
                        >
                          <FileText size={14} aria-hidden />
                          <span className="sr-only">{t('build.replay')}</span>
                        </button>
                      )}
                      {build.status === 'success' && runTag && (
                        <button
                          type="button"
                          className="btn-ghost"
                          title={t('build.run_image')}
                          onClick={() =>
                            navigate(`/containers/new?image=${encodeURIComponent(runTag)}`)
                          }
                        >
                          <Rocket size={14} aria-hidden />
                          <span className="sr-only">{t('build.run_image')}</span>
                        </button>
                      )}
                      {build.status === 'running' && canBuild && (
                        <button
                          type="button"
                          className="btn-ghost text-danger"
                          title={t('build.cancel')}
                          disabled={cancel.isPending}
                          onClick={() => cancel.mutate(build)}
                        >
                          <X size={14} aria-hidden />
                          <span className="sr-only">{t('build.cancel')}</span>
                        </button>
                      )}
                    </div>
                  </td>
                </tr>
              );
            })}
          </tbody>
        </table>
      </div>

      {cancel.error && (
        <div className="mt-3">
          <ErrorPanel error={cancel.error} />
        </div>
      )}

      <LogReplay build={replaying} onClose={() => setReplaying(null)} />
    </>
  );
}

/** Shows one finished build's archived output. */
function LogReplay({ build, onClose }: { build: Build | null; onClose: () => void }) {
  const { t } = useTranslation();

  const log = useQuery({
    queryKey: ['builds', build?.id, 'log'],
    queryFn: () => buildsApi.log(build?.id ?? ''),
    enabled: Boolean(build),
  });

  if (!build) return null;

  return (
    <div className="fixed inset-0 z-50 flex items-center justify-center bg-black/60 p-4">
      <div
        role="dialog"
        aria-modal="true"
        aria-labelledby="replay-title"
        className="card flex h-[36rem] w-full max-w-4xl flex-col p-5"
      >
        <div className="flex items-start justify-between gap-3">
          <div className="min-w-0">
            <h2 id="replay-title" className="font-medium">
              {t('build.replay')}
            </h2>
            <p className="mt-1 truncate font-mono text-xs text-muted">
              {build.tags.join(', ') || build.context_dir}
            </p>
          </div>
          <button type="button" className="btn-default" onClick={onClose}>
            {t('common.close')}
          </button>
        </div>

        <div className="mt-4 min-h-0 flex-1 overflow-auto rounded border border-border bg-bg p-3">
          {log.isLoading ? (
            <div className="flex justify-center py-12">
              <Spinner />
            </div>
          ) : log.error ? (
            <ErrorPanel error={log.error} onRetry={() => void log.refetch()} />
          ) : (
            <pre className="whitespace-pre-wrap break-all font-mono text-xs leading-relaxed">
              {log.data || t('build.replay_empty')}
            </pre>
          )}
        </div>
      </div>
    </div>
  );
}
