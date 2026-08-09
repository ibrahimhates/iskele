import { useCallback, useEffect, useMemo, useRef, useState } from 'react';
import { useNavigate } from 'react-router-dom';
import { useQueryClient } from '@tanstack/react-query';
import { useTranslation } from 'react-i18next';
import { Check, Copy, FolderOpen, Hammer, Rocket, X } from 'lucide-react';

import { PageHeader } from '../../components/PageHeader';
import { EmptyState } from '../../components/EmptyState';
import { Field, RowList, Section, Toggle } from '../create/fields';
import { useAuth } from '../../stores/auth';
import { BuildHistory } from './BuildHistory';
import { PathBrowser } from './PathBrowser';
import { dockerBuildCommand, useBuildForm } from './state';
import { useBuildStream } from './useBuildStream';

/**
 * Builds an image from a Dockerfile on the host.
 *
 * Building runs whatever the Dockerfile says, as root, inside the daemon —
 * which is why it takes its own permission, and why the context directory can
 * only ever be one the whitelist already allows.
 */
export function BuildPage() {
  const { t } = useTranslation();
  const navigate = useNavigate();
  const queryClient = useQueryClient();
  const canBuild = useAuth((s) => s.can('build'));

  const { form, set, addPair, removePair, updatePair, request, problems } = useBuildForm();
  const [browsing, setBrowsing] = useState(false);
  const [copied, setCopied] = useState(false);
  const [found, setFound] = useState<string[]>([]);

  const onFinished = useCallback(() => {
    void queryClient.invalidateQueries({ queryKey: ['builds'] });
    void queryClient.invalidateQueries({ queryKey: ['images'] });
  }, [queryClient]);

  const { state, start, cancel, reset } = useBuildStream(onFinished);

  const command = useMemo(() => dockerBuildCommand(request), [request]);
  const primaryTag = request.tags[0];
  const running = state.phase === 'connecting' || state.phase === 'running';

  const logRef = useRef<HTMLDivElement>(null);
  const pinned = useRef(true);

  // Follow the output, but stop following the moment the operator scrolls up:
  // reading an error while the view jumps to the bottom is not reading.
  useEffect(() => {
    const node = logRef.current;
    if (node && pinned.current) node.scrollTop = node.scrollHeight;
  }, [state.lines]);

  const onScroll = () => {
    const node = logRef.current;
    if (!node) return;
    pinned.current = node.scrollHeight - node.scrollTop - node.clientHeight < 40;
  };

  if (!canBuild) {
    return (
      <>
        <PageHeader title={t('build.title')} description={t('build.subtitle')} />
        <EmptyState
          icon={<Hammer size={32} aria-hidden />}
          title={t('build.forbidden')}
          description={t('build.forbidden_hint')}
        />
      </>
    );
  }

  return (
    <>
      <PageHeader
        title={t('build.title')}
        description={t('build.subtitle')}
        actions={
          running ? (
            <button type="button" className="btn-danger" onClick={() => void cancel()}>
              <X size={14} aria-hidden />
              {t('build.cancel')}
            </button>
          ) : (
            <button
              type="button"
              className="btn-primary"
              disabled={problems.length > 0}
              onClick={() => void start(request)}
            >
              <Hammer size={14} aria-hidden />
              {t('build.start')}
            </button>
          )
        }
      />

      <div className="grid gap-4 lg:grid-cols-2">
        <div className="card space-y-5 p-4">
          <Section title={t('build.section_context')} description={t('build.section_context_hint')}>
            <Field label={t('build.context')} htmlFor="build-context">
              <div className="flex gap-2">
                <input
                  id="build-context"
                  className="input flex-1 font-mono"
                  value={form.context}
                  placeholder="/srv/projects/app"
                  spellCheck={false}
                  onChange={(e) => set('context', e.target.value)}
                />
                <button type="button" className="btn-default" onClick={() => setBrowsing(true)}>
                  <FolderOpen size={14} aria-hidden />
                  {t('build.browse')}
                </button>
              </div>
            </Field>

            <Field
              label={t('build.dockerfile')}
              htmlFor="build-dockerfile"
              hint={
                found.length > 0
                  ? t('build.dockerfile_found', { files: found.join(', ') })
                  : t('build.dockerfile_hint')
              }
            >
              <input
                id="build-dockerfile"
                className="input font-mono"
                value={form.dockerfile}
                placeholder="Dockerfile"
                spellCheck={false}
                onChange={(e) => set('dockerfile', e.target.value)}
              />
            </Field>

            {found.length > 1 && (
              <div className="flex flex-wrap gap-1.5">
                {found.map((name) => (
                  <button
                    key={name}
                    type="button"
                    className="btn-ghost font-mono text-xs"
                    onClick={() => set('dockerfile', name)}
                  >
                    {name}
                  </button>
                ))}
              </div>
            )}
          </Section>

          <Section title={t('build.section_image')} description={t('build.section_image_hint')}>
            <Field label={t('build.tags')} htmlFor="build-tags" hint={t('build.tags_hint')}>
              <input
                id="build-tags"
                className="input font-mono"
                value={form.tags}
                placeholder="app:latest, registry.example.com/app:1.2.3"
                spellCheck={false}
                onChange={(e) => set('tags', e.target.value)}
              />
            </Field>

            <div className="grid gap-3 sm:grid-cols-2">
              <Field label={t('build.target')} htmlFor="build-target" hint={t('build.target_hint')}>
                <input
                  id="build-target"
                  className="input font-mono"
                  value={form.target}
                  placeholder="builder"
                  spellCheck={false}
                  onChange={(e) => set('target', e.target.value)}
                />
              </Field>
              <Field
                label={t('build.platform')}
                htmlFor="build-platform"
                hint={t('build.platform_hint')}
              >
                <input
                  id="build-platform"
                  className="input font-mono"
                  value={form.platform}
                  placeholder="linux/amd64"
                  spellCheck={false}
                  onChange={(e) => set('platform', e.target.value)}
                />
              </Field>
            </div>

            <div className="space-y-2">
              <Toggle
                label={t('build.no_cache')}
                hint={t('build.no_cache_hint')}
                checked={form.noCache}
                onChange={(value) => set('noCache', value)}
              />
              <Toggle
                label={t('build.pull')}
                hint={t('build.pull_hint')}
                checked={form.pull}
                onChange={(value) => set('pull', value)}
              />
            </div>
          </Section>

          <Section title={t('build.section_args')} description={t('build.section_args_hint')}>
            <RowList
              rows={form.buildArgs}
              onAdd={() => addPair('buildArgs')}
              onRemove={(id) => removePair('buildArgs', id)}
              addLabel={t('build.add_arg')}
              emptyLabel={t('build.no_args')}
              columns={[t('create.key'), t('create.value')]}
            >
              {(row) => (
                <>
                  <input
                    className="input font-mono"
                    value={row.key}
                    placeholder="VERSION"
                    spellCheck={false}
                    onChange={(e) => updatePair('buildArgs', row.id, { key: e.target.value })}
                  />
                  <input
                    className="input font-mono"
                    value={row.value}
                    spellCheck={false}
                    onChange={(e) => updatePair('buildArgs', row.id, { value: e.target.value })}
                  />
                </>
              )}
            </RowList>
          </Section>

          <Section title={t('build.section_labels')} description={t('build.section_labels_hint')}>
            <RowList
              rows={form.labels}
              onAdd={() => addPair('labels')}
              onRemove={(id) => removePair('labels', id)}
              addLabel={t('build.add_label')}
              emptyLabel={t('build.no_labels')}
              columns={[t('create.key'), t('create.value')]}
            >
              {(row) => (
                <>
                  <input
                    className="input font-mono"
                    value={row.key}
                    placeholder="org.opencontainers.image.source"
                    spellCheck={false}
                    onChange={(e) => updatePair('labels', row.id, { key: e.target.value })}
                  />
                  <input
                    className="input font-mono"
                    value={row.value}
                    spellCheck={false}
                    onChange={(e) => updatePair('labels', row.id, { value: e.target.value })}
                  />
                </>
              )}
            </RowList>
          </Section>

          {problems.length > 0 && (
            <ul className="space-y-1 text-xs text-warn">
              {problems.map((key) => (
                <li key={key}>{t(key)}</li>
              ))}
            </ul>
          )}
        </div>

        <div className="space-y-4">
          <div className="card p-4">
            <div className="flex items-start justify-between gap-3">
              <h3 className="text-sm font-semibold">{t('build.equivalent')}</h3>
              <button
                type="button"
                className="btn-ghost"
                onClick={() => {
                  void navigator.clipboard.writeText(command);
                  setCopied(true);
                  window.setTimeout(() => setCopied(false), 1500);
                }}
              >
                {copied ? <Check size={14} aria-hidden /> : <Copy size={14} aria-hidden />}
                {copied ? t('common.copied') : t('common.copy')}
              </button>
            </div>
            <pre className="mt-2 overflow-x-auto rounded bg-bg p-3 font-mono text-xs">
              {command}
            </pre>
          </div>

          <div className="card flex h-[28rem] flex-col p-4">
            <div className="flex items-start justify-between gap-3">
              <div className="min-w-0">
                <h3 className="text-sm font-semibold">{t('build.output')}</h3>
                <p className="mt-0.5 truncate text-xs text-muted">
                  {state.phase === 'idle'
                    ? t('build.output_idle')
                    : state.totalSteps > 0
                      ? t('build.step', { step: state.step, total: state.totalSteps })
                      : state.status || t(`build.phase_${state.phase}`)}
                </p>
              </div>
              {state.phase !== 'idle' && !running && (
                <button type="button" className="btn-ghost" onClick={reset}>
                  {t('common.clear')}
                </button>
              )}
            </div>

            <div
              ref={logRef}
              onScroll={onScroll}
              className="mt-3 min-h-0 flex-1 overflow-auto rounded border border-border bg-bg p-3"
            >
              {state.lines.length === 0 ? (
                <p className="text-xs text-muted">{t('build.output_empty')}</p>
              ) : (
                <pre className="whitespace-pre-wrap break-all font-mono text-xs leading-relaxed">
                  {state.lines.map((line) => `${line.text}\n`).join('')}
                </pre>
              )}
            </div>

            {state.error && <p className="mt-2 text-xs text-danger">{state.error}</p>}

            {state.phase === 'succeeded' && (
              <div className="mt-3 flex items-center justify-between gap-3 rounded border border-ok/40 bg-ok/10 px-3 py-2">
                <p className="min-w-0 truncate text-xs">
                  {t('build.succeeded', { image: primaryTag ?? state.imageID ?? '' })}
                </p>
                {primaryTag && (
                  <button
                    type="button"
                    className="btn-primary shrink-0"
                    onClick={() =>
                      navigate(`/containers/new?image=${encodeURIComponent(primaryTag)}`)
                    }
                  >
                    <Rocket size={14} aria-hidden />
                    {t('build.run_image')}
                  </button>
                )}
              </div>
            )}
          </div>
        </div>
      </div>

      <div className="mt-6">
        <h2 className="mb-3 text-sm font-semibold">{t('build.history')}</h2>
        <BuildHistory />
      </div>

      <PathBrowser
        open={browsing}
        initialPath={form.context}
        onClose={() => setBrowsing(false)}
        onPick={(path, dockerfiles) => {
          set('context', path);
          setFound(dockerfiles);
          // One Dockerfile in the directory is not a choice worth making by
          // hand; several are, so the form only fills in the unambiguous case.
          const only = dockerfiles.length === 1 ? dockerfiles[0] : undefined;
          if (only && only !== 'Dockerfile') {
            set('dockerfile', only);
          }
          setBrowsing(false);
        }}
      />
    </>
  );
}
