import { useState } from 'react';
import { useNavigate } from 'react-router-dom';
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import { useTranslation } from 'react-i18next';
import { Download, HardDrive, Layers, Rocket, Tag, Trash2 } from 'lucide-react';

import { images as imagesApi } from '../../api/endpoints';
import type { Image } from '../../api/types';
import { PageHeader } from '../../components/PageHeader';
import { EmptyState } from '../../components/EmptyState';
import { ErrorPanel } from '../../components/ErrorPanel';
import { Spinner } from '../../components/Spinner';
import { ConfirmDialog } from '../../components/ConfirmDialog';
import { JsonViewer } from '../containers/JsonViewer';
import { usePullStream } from '../tasks/usePullStream';
import { useAuth } from '../../stores/auth';
import { formatBytes, formatRelative, shortID } from '../../lib/format';

export function ImagesPage() {
  const { t } = useTranslation();
  const queryClient = useQueryClient();
  const navigate = useNavigate();
  const canOperate = useAuth((s) => s.can('operate'));
  const canCreate = useAuth((s) => s.can('create'));
  const canDelete = useAuth((s) => s.can('delete'));
  const canPrune = useAuth((s) => s.can('prune'));

  const [showAll, setShowAll] = useState(false);
  const [pullRef, setPullRef] = useState('');
  const [tagging, setTagging] = useState<Image | null>(null);
  const [tagValue, setTagValue] = useState('');
  const [removing, setRemoving] = useState<Image | null>(null);
  const [pruning, setPruning] = useState(false);
  const [inspecting, setInspecting] = useState<string | null>(null);

  const refresh = () => queryClient.invalidateQueries({ queryKey: ['images'] });

  const query = useQuery({
    queryKey: ['images', { all: showAll }],
    queryFn: () => imagesApi.list({ all: showAll }),
  });

  const pull = usePullStream(() => void refresh());

  const tag = useMutation({
    mutationFn: ({ id, value }: { id: string; value: string }) => imagesApi.tag(id, value),
    onSuccess: () => {
      setTagging(null);
      setTagValue('');
      void refresh();
    },
  });

  const remove = useMutation({
    mutationFn: (image: Image) => imagesApi.remove(image.id, { force: true }),
    onSuccess: () => {
      setRemoving(null);
      void refresh();
    },
  });

  const prune = useMutation({
    mutationFn: () => imagesApi.prune(false),
    onSuccess: () => {
      setPruning(false);
      void refresh();
    },
  });

  const inspect = useQuery({
    queryKey: ['images', inspecting, 'inspect'],
    queryFn: () => imagesApi.inspect(inspecting as string),
    enabled: inspecting !== null,
  });

  const history = useQuery({
    queryKey: ['images', inspecting, 'history'],
    queryFn: () => imagesApi.history(inspecting as string),
    enabled: inspecting !== null,
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
        title={t('nav.images')}
        description={t('images.count', { count: items.length })}
        actions={
          canPrune ? (
            <button type="button" className="btn-default" onClick={() => setPruning(true)}>
              <Trash2 size={14} aria-hidden />
              {t('common.prune')}
            </button>
          ) : undefined
        }
      />

      {canOperate && (
        <div className="card mb-4 space-y-3 p-3">
          <div className="flex flex-col gap-2 sm:flex-row">
            <input
              className="input font-mono"
              value={pullRef}
              placeholder="nginx:1.27"
              aria-label={t('images.pull')}
              disabled={pull.state.active}
              onChange={(e) => setPullRef(e.target.value)}
              onKeyDown={(e) => {
                if (e.key === 'Enter' && pullRef.trim()) void pull.start(pullRef.trim());
              }}
            />
            {pull.state.active ? (
              <button
                type="button"
                className="btn-default shrink-0"
                onClick={() => void pull.cancel()}
              >
                {t('common.cancel')}
              </button>
            ) : (
              <button
                type="button"
                className="btn-primary shrink-0"
                disabled={pullRef.trim() === ''}
                onClick={() => void pull.start(pullRef.trim())}
              >
                <Download size={14} aria-hidden />
                {t('images.pull')}
              </button>
            )}
          </div>

          {(pull.state.active || pull.state.error) && (
            <div className="space-y-1">
              <div className="h-1.5 overflow-hidden rounded-full bg-elevated">
                <div
                  className={`h-full bg-accent transition-[width] ${
                    pull.state.percent < 0 ? 'w-1/3 animate-pulse' : ''
                  }`}
                  style={pull.state.percent >= 0 ? { width: `${pull.state.percent}%` } : undefined}
                  role="progressbar"
                  aria-valuenow={pull.state.percent >= 0 ? pull.state.percent : undefined}
                  aria-valuemin={0}
                  aria-valuemax={100}
                  aria-label={t('images.pull')}
                />
              </div>
              <p className={`text-xs ${pull.state.error ? 'text-danger' : 'text-muted'}`}>
                {pull.state.error ?? pull.state.status}
              </p>
            </div>
          )}
        </div>
      )}

      <label className="mb-3 flex cursor-pointer items-center gap-2 text-sm">
        <input
          type="checkbox"
          className="accent-accent"
          checked={showAll}
          onChange={(e) => setShowAll(e.target.checked)}
        />
        {t('images.show_intermediate')}
      </label>

      {query.error ? (
        <ErrorPanel error={query.error} onRetry={() => void query.refetch()} />
      ) : items.length === 0 ? (
        <EmptyState
          icon={<HardDrive size={32} aria-hidden />}
          title={t('images.empty')}
          description={t('images.empty_hint')}
        />
      ) : (
        <div className="card overflow-x-auto">
          <table className="w-full text-sm">
            <thead className="border-b border-border text-left text-xs uppercase tracking-wide text-muted">
              <tr>
                <th className="px-3 py-2 font-medium">{t('common.tags')}</th>
                <th className="px-3 py-2 font-medium">ID</th>
                <th className="px-3 py-2 font-medium">{t('common.created')}</th>
                <th className="px-3 py-2 text-right font-medium">{t('common.size')}</th>
                <th className="px-3 py-2 text-right font-medium">{t('common.used_by')}</th>
                <th className="px-3 py-2" />
              </tr>
            </thead>
            <tbody>
              {items.map((image) => {
                const runTag = image.repo_tags[0];
                return (
                  <tr key={image.id} className="border-b border-border/50 last:border-0">
                    <td className="px-3 py-2">
                      {image.repo_tags.length === 0 ? (
                        <span className="badge bg-elevated text-muted">{t('images.dangling')}</span>
                      ) : (
                        <div className="space-y-0.5">
                          {image.repo_tags.map((repoTag) => (
                            <div key={repoTag} className="font-mono text-xs">
                              {repoTag}
                            </div>
                          ))}
                        </div>
                      )}
                    </td>
                    <td className="px-3 py-2 font-mono text-xs text-muted">{shortID(image.id)}</td>
                    <td className="whitespace-nowrap px-3 py-2 text-muted">
                      {formatRelative(image.created)}
                    </td>
                    <td className="whitespace-nowrap px-3 py-2 text-right font-mono text-xs">
                      {formatBytes(image.size)}
                    </td>
                    <td className="px-3 py-2 text-right font-mono text-xs text-muted">
                      {image.containers < 0 ? '—' : image.containers}
                    </td>
                    <td className="px-3 py-2">
                      <div className="flex justify-end gap-1">
                        <button
                          type="button"
                          className="btn-ghost p-1.5"
                          onClick={() => setInspecting(image.id)}
                          aria-label={t('images.inspect')}
                          title={t('images.inspect')}
                        >
                          <Layers size={15} aria-hidden />
                        </button>
                        {canCreate && runTag && (
                          <button
                            type="button"
                            className="btn-ghost p-1.5"
                            onClick={() =>
                              navigate(`/containers/new?image=${encodeURIComponent(runTag)}`)
                            }
                            aria-label={t('images.run')}
                            title={t('images.run')}
                          >
                            <Rocket size={15} aria-hidden />
                          </button>
                        )}
                        {canOperate && (
                          <button
                            type="button"
                            className="btn-ghost p-1.5"
                            onClick={() => {
                              setTagging(image);
                              setTagValue(image.repo_tags[0] ?? '');
                            }}
                            aria-label={t('images.tag')}
                            title={t('images.tag')}
                          >
                            <Tag size={15} aria-hidden />
                          </button>
                        )}
                        {canDelete && (
                          <button
                            type="button"
                            className="btn-ghost p-1.5 text-muted hover:text-danger"
                            onClick={() => setRemoving(image)}
                            aria-label={t('common.delete')}
                            title={t('common.delete')}
                          >
                            <Trash2 size={15} aria-hidden />
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
      )}

      {inspecting && (
        <div className="mt-6 space-y-4">
          <div className="flex items-center justify-between">
            <h2 className="text-sm font-semibold">{shortID(inspecting)}</h2>
            <button type="button" className="btn-ghost" onClick={() => setInspecting(null)}>
              {t('common.close')}
            </button>
          </div>

          {history.data && (
            <div className="card overflow-x-auto">
              <table className="w-full text-xs">
                <thead className="border-b border-border text-left uppercase tracking-wide text-muted">
                  <tr>
                    <th className="px-3 py-2 font-medium">{t('images.layer')}</th>
                    <th className="px-3 py-2 text-right font-medium">{t('common.size')}</th>
                  </tr>
                </thead>
                <tbody>
                  {history.data.items.map((layer, index) => (
                    <tr
                      key={`${layer.id}-${index}`}
                      className="border-b border-border/50 last:border-0"
                    >
                      <td className="px-3 py-2 font-mono">{layer.created_by || '—'}</td>
                      <td className="whitespace-nowrap px-3 py-2 text-right font-mono">
                        {formatBytes(layer.size)}
                      </td>
                    </tr>
                  ))}
                </tbody>
              </table>
            </div>
          )}

          {inspect.data && <JsonViewer value={inspect.data} />}
        </div>
      )}

      <ConfirmDialog
        open={tagging !== null}
        title={t('images.tag')}
        description={t('images.tag_hint')}
        confirmLabel={t('common.save')}
        busy={tag.isPending}
        onCancel={() => setTagging(null)}
        onConfirm={() => tagging && tag.mutate({ id: tagging.id, value: tagValue })}
      >
        <input
          className="input font-mono"
          value={tagValue}
          placeholder="registry.example.com/app:v2"
          aria-label={t('images.tag')}
          onChange={(e) => setTagValue(e.target.value)}
        />
      </ConfirmDialog>

      <ConfirmDialog
        open={removing !== null}
        title={t('images.remove_title')}
        description={t('images.remove_hint')}
        confirmText={removing ? (removing.repo_tags[0] ?? shortID(removing.id)) : undefined}
        confirmLabel={t('common.delete')}
        destructive
        busy={remove.isPending}
        onCancel={() => setRemoving(null)}
        onConfirm={() => removing && remove.mutate(removing)}
      />

      <ConfirmDialog
        open={pruning}
        title={t('images.prune_title')}
        description={t('images.prune_hint')}
        confirmLabel={t('common.prune')}
        destructive
        busy={prune.isPending}
        onCancel={() => setPruning(false)}
        onConfirm={() => prune.mutate()}
      />
    </>
  );
}
