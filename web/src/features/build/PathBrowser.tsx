import { useEffect, useState } from 'react';
import { useQuery } from '@tanstack/react-query';
import { useTranslation } from 'react-i18next';
import { ArrowUp, FileText, Folder, HardDrive, Link2 } from 'lucide-react';

import { fs as fsApi } from '../../api/endpoints';
import { ErrorPanel } from '../../components/ErrorPanel';
import { Spinner } from '../../components/Spinner';
import { formatBytes, formatRelative } from '../../lib/format';

interface Props {
  open: boolean;
  /** Where to open. Empty starts at the whitelist roots. */
  initialPath?: string;
  onPick: (path: string, dockerfiles: string[]) => void;
  onClose: () => void;
}

/**
 * Picks a build context from the host's whitelisted directories.
 *
 * The whitelist is the whole point: this browser cannot leave it, so an
 * operator learns what is available by looking rather than by being refused.
 * Files are shown but not selectable — a build context is a directory.
 */
export function PathBrowser({ open, initialPath, onPick, onClose }: Props) {
  const { t } = useTranslation();
  const [path, setPath] = useState(initialPath ?? '');

  useEffect(() => {
    if (open) setPath(initialPath ?? '');
  }, [open, initialPath]);

  useEffect(() => {
    if (!open) return;
    function onKeyDown(event: KeyboardEvent) {
      if (event.key === 'Escape') onClose();
    }
    window.addEventListener('keydown', onKeyDown);
    return () => window.removeEventListener('keydown', onKeyDown);
  }, [open, onClose]);

  const listing = useQuery({
    queryKey: ['fs', 'browse', path],
    queryFn: () => fsApi.browse(path),
    enabled: open,
  });

  if (!open) return null;

  const data = listing.data;
  const entries = data?.entries ?? [];
  const roots = data?.allowed_roots ?? [];

  return (
    <div className="fixed inset-0 z-50 flex items-center justify-center bg-black/60 p-4">
      <div
        role="dialog"
        aria-modal="true"
        aria-labelledby="browse-title"
        className="card flex h-[32rem] w-full max-w-2xl flex-col p-5"
      >
        <div className="flex items-start justify-between gap-3">
          <div className="min-w-0">
            <h2 id="browse-title" className="font-medium">
              {t('build.browse_title')}
            </h2>
            <p className="mt-1 truncate font-mono text-xs text-muted">
              {path || t('build.browse_roots')}
            </p>
          </div>
          <div className="flex shrink-0 gap-2">
            {data?.parent !== undefined && (
              <button
                type="button"
                className="btn-default"
                onClick={() => setPath(data.parent ?? '')}
              >
                <ArrowUp size={14} aria-hidden />
                {t('build.browse_up')}
              </button>
            )}
            {path && roots.length > 1 && (
              <button type="button" className="btn-default" onClick={() => setPath('')}>
                <HardDrive size={14} aria-hidden />
                {t('build.browse_roots_short')}
              </button>
            )}
          </div>
        </div>

        <div className="mt-4 min-h-0 flex-1 overflow-y-auto rounded border border-border">
          {listing.isLoading ? (
            <div className="flex justify-center py-12">
              <Spinner />
            </div>
          ) : listing.error ? (
            <div className="p-3">
              <ErrorPanel error={listing.error} onRetry={() => void listing.refetch()} />
            </div>
          ) : entries.length === 0 ? (
            <p className="p-6 text-center text-sm text-muted">{t('build.browse_empty')}</p>
          ) : (
            <ul className="divide-y divide-border/50">
              {entries.map((entry) => (
                <li key={entry.path}>
                  {entry.is_dir ? (
                    <button
                      type="button"
                      className="flex w-full items-center gap-2 px-3 py-2 text-left text-sm hover:bg-elevated"
                      onClick={() => setPath(entry.path)}
                    >
                      <Folder size={15} className="shrink-0 text-accent" aria-hidden />
                      <span className="min-w-0 flex-1 truncate">{entry.name}</span>
                      {entry.symlink && (
                        <Link2
                          size={13}
                          className="shrink-0 text-muted"
                          aria-label={t('build.browse_symlink')}
                        />
                      )}
                      <span className="shrink-0 text-xs text-muted">
                        {formatRelative(entry.mod_time)}
                      </span>
                    </button>
                  ) : (
                    <div className="flex items-center gap-2 px-3 py-2 text-sm text-muted">
                      <FileText size={15} className="shrink-0" aria-hidden />
                      <span className="min-w-0 flex-1 truncate">{entry.name}</span>
                      <span className="shrink-0 text-xs">{formatBytes(entry.size)}</span>
                    </div>
                  )}
                </li>
              ))}
            </ul>
          )}
        </div>

        {data?.truncated && <p className="mt-2 text-xs text-warn">{t('build.browse_truncated')}</p>}
        {data?.dockerfiles && data.dockerfiles.length > 0 && (
          <p className="mt-2 text-xs text-muted">
            {t('build.browse_found', { files: data.dockerfiles.join(', ') })}
          </p>
        )}

        <div className="mt-4 flex justify-end gap-2">
          <button type="button" className="btn-default" onClick={onClose}>
            {t('common.cancel')}
          </button>
          <button
            type="button"
            className="btn-primary"
            disabled={!path}
            onClick={() => onPick(path, data?.dockerfiles ?? [])}
          >
            {t('build.browse_pick')}
          </button>
        </div>
      </div>
    </div>
  );
}
