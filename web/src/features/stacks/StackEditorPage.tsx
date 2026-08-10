import { useEffect, useMemo, useState } from 'react';
import { useNavigate, useParams } from 'react-router-dom';
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import { useTranslation } from 'react-i18next';
import { AlertTriangle, Check, GitBranch, Save, ShieldAlert } from 'lucide-react';

import { stacks as stacksApi } from '../../api/endpoints';
import type { StackDiff, StackInput, StackSource, StackValidation } from '../../api/types';
import { PageHeader } from '../../components/PageHeader';
import { ErrorPanel } from '../../components/ErrorPanel';
import { Spinner } from '../../components/Spinner';
import { Field, Section } from '../create/fields';
import { ComposeEditor } from './ComposeEditor';

/** The compose file a new stack starts from, so the editor is never a blank page. */
const STARTER = `services:
  app:
    image: nginx:1.27-alpine
    restart: unless-stopped
    ports:
      - "8080:80"
`;

type Tab = 'compose' | 'env' | 'source';

/**
 * Writes a stack: its compose file, its `.env`, and where they come from.
 *
 * Validation and the diff both come from the server, which is the only thing
 * that knows the path whitelist and the caller's permissions — the same checks
 * a deploy runs, so the editor and the deploy cannot disagree.
 */
export function StackEditorPage() {
  const { t } = useTranslation();
  const navigate = useNavigate();
  const queryClient = useQueryClient();
  const { id } = useParams<{ id: string }>();
  const editing = Boolean(id);

  const [name, setName] = useState('');
  const [source, setSource] = useState<StackSource>('editor');
  const [compose, setCompose] = useState(STARTER);
  const [env, setEnv] = useState('');
  const [path, setPath] = useState('');
  const [gitURL, setGitURL] = useState('');
  const [gitRef, setGitRef] = useState('');
  const [tab, setTab] = useState<Tab>('compose');
  const [report, setReport] = useState<StackValidation | null>(null);
  const [diff, setDiff] = useState<StackDiff | null>(null);

  const existing = useQuery({
    queryKey: ['stacks', id],
    queryFn: () => stacksApi.get(id as string),
    enabled: editing,
  });

  // Seeded once the record arrives; re-seeding would fight an operator mid-edit.
  useEffect(() => {
    const stack = existing.data;
    if (!stack) return;
    setName(stack.name);
    setSource(stack.source);
    setCompose(stack.compose);
    setEnv(stack.env ?? '');
    setPath(stack.path ?? '');
    setGitURL(stack.git_url ?? '');
    setGitRef(stack.git_ref ?? '');
  }, [existing.data]);

  const input: StackInput = useMemo(
    () => ({
      name: name.trim(),
      source,
      compose,
      env,
      path: path.trim() || undefined,
      git_url: gitURL.trim() || undefined,
      git_ref: gitRef.trim() || undefined,
    }),
    [name, source, compose, env, path, gitURL, gitRef],
  );

  const validate = useMutation({
    mutationFn: () => stacksApi.validate(input),
    onSuccess: (result) => {
      setReport(result);
      setDiff(null);
    },
  });

  const showDiff = useMutation({
    mutationFn: () => stacksApi.diff(id as string, input),
    onSuccess: (result) => {
      setDiff(result);
      setReport(null);
    },
  });

  const save = useMutation({
    mutationFn: () => (editing ? stacksApi.update(id as string, input) : stacksApi.create(input)),
    onSuccess: (stack) => {
      void queryClient.invalidateQueries({ queryKey: ['stacks'] });
      navigate(`/stacks/${encodeURIComponent(stack.id)}`);
    },
  });

  if (editing && existing.isLoading) {
    return (
      <div className="flex justify-center py-16">
        <Spinner />
      </div>
    );
  }

  const canSave = name.trim() !== '' && (source !== 'editor' || compose.trim() !== '');

  return (
    <>
      <PageHeader
        title={editing ? t('stacks.edit_title', { name }) : t('stacks.create')}
        description={t('stacks.editor_subtitle')}
        actions={
          <>
            <button
              type="button"
              className="btn-default"
              disabled={validate.isPending}
              onClick={() => validate.mutate()}
            >
              <Check size={14} aria-hidden />
              {t('stacks.validate')}
            </button>
            {editing && (
              <button
                type="button"
                className="btn-default"
                disabled={showDiff.isPending}
                onClick={() => showDiff.mutate()}
              >
                <GitBranch size={14} aria-hidden />
                {t('stacks.diff')}
              </button>
            )}
            <button
              type="button"
              className="btn-primary"
              disabled={!canSave || save.isPending}
              onClick={() => save.mutate()}
            >
              <Save size={14} aria-hidden />
              {t('common.save')}
            </button>
          </>
        }
      />

      {save.error && (
        <div className="mb-4">
          <ErrorPanel error={save.error} />
        </div>
      )}

      <div className="grid gap-4 lg:grid-cols-[2fr,1fr]">
        <div className="card p-4">
          <div className="mb-3 flex gap-1 border-b border-border">
            {(['compose', 'env', 'source'] as Tab[]).map((key) => (
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

          {tab === 'compose' && (
            <ComposeEditor
              value={compose}
              onChange={setCompose}
              language="yaml"
              ariaLabel={t('stacks.tab_compose')}
              readOnly={source !== 'editor'}
            />
          )}

          {tab === 'env' && (
            <>
              <p className="mb-2 text-xs text-muted">{t('stacks.env_hint')}</p>
              <ComposeEditor
                value={env}
                onChange={setEnv}
                language="ini"
                height="18rem"
                ariaLabel={t('stacks.tab_env')}
              />
            </>
          )}

          {tab === 'source' && (
            <div className="space-y-4">
              <Section title={t('stacks.source')} description={t('stacks.source_hint')}>
                <Field label={t('common.name')} htmlFor="stack-name">
                  <input
                    id="stack-name"
                    className="input font-mono"
                    value={name}
                    placeholder="my-stack"
                    disabled={editing}
                    spellCheck={false}
                    onChange={(e) => setName(e.target.value)}
                  />
                </Field>
                {editing && <p className="text-xs text-muted">{t('stacks.name_fixed')}</p>}

                <div className="flex flex-wrap gap-1.5">
                  {(['editor', 'file', 'git'] as StackSource[]).map((key) => (
                    <button
                      key={key}
                      type="button"
                      className={source === key ? 'btn-primary' : 'btn-default'}
                      onClick={() => setSource(key)}
                    >
                      {t(`stacks.source_${key}`)}
                    </button>
                  ))}
                </div>
              </Section>

              {source === 'file' && (
                <Field label={t('stacks.path')} htmlFor="stack-path" hint={t('stacks.path_hint')}>
                  <input
                    id="stack-path"
                    className="input font-mono"
                    value={path}
                    placeholder="/srv/projects/app/compose.yaml"
                    spellCheck={false}
                    onChange={(e) => setPath(e.target.value)}
                  />
                </Field>
              )}

              {source === 'git' && (
                <div className="space-y-3">
                  <Field
                    label={t('stacks.git_url')}
                    htmlFor="stack-git"
                    hint={t('stacks.git_url_hint')}
                  >
                    <input
                      id="stack-git"
                      className="input font-mono"
                      value={gitURL}
                      placeholder="https://github.com/example/repo.git"
                      spellCheck={false}
                      onChange={(e) => setGitURL(e.target.value)}
                    />
                  </Field>
                  <div className="grid gap-3 sm:grid-cols-2">
                    <Field label={t('stacks.git_ref')} htmlFor="stack-ref">
                      <input
                        id="stack-ref"
                        className="input font-mono"
                        value={gitRef}
                        placeholder="main"
                        spellCheck={false}
                        onChange={(e) => setGitRef(e.target.value)}
                      />
                    </Field>
                    <Field
                      label={t('stacks.path_in_repo')}
                      htmlFor="stack-repo-path"
                      hint={t('stacks.path_in_repo_hint')}
                    >
                      <input
                        id="stack-repo-path"
                        className="input font-mono"
                        value={path}
                        placeholder="compose.yaml"
                        spellCheck={false}
                        onChange={(e) => setPath(e.target.value)}
                      />
                    </Field>
                  </div>
                </div>
              )}
            </div>
          )}
        </div>

        <div className="space-y-4">
          {validate.error && <ErrorPanel error={validate.error} />}
          {showDiff.error && <ErrorPanel error={showDiff.error} />}
          {report && <ValidationReport report={report} />}
          {diff && <DiffReport diff={diff} />}
          {!report && !diff && (
            <div className="card p-4 text-sm text-muted">{t('stacks.validate_hint')}</div>
          )}
        </div>
      </div>
    </>
  );
}

/** The server's verdict on the file as it stands. */
function ValidationReport({ report }: { report: StackValidation }) {
  const { t } = useTranslation();

  return (
    <div className="card space-y-3 p-4">
      <h3 className={`text-sm font-semibold ${report.valid ? 'text-ok' : 'text-danger'}`}>
        {report.valid ? t('stacks.valid') : t('stacks.invalid')}
      </h3>

      {report.error && (
        <pre className="whitespace-pre-wrap break-words rounded bg-bg p-3 font-mono text-xs text-danger">
          {report.error}
        </pre>
      )}

      {report.services && report.services.length > 0 && (
        <p className="text-xs text-muted">
          {t('stacks.services')}: <span className="font-mono">{report.services.join(', ')}</span>
        </p>
      )}

      {report.problems.length > 0 && (
        <ul className="space-y-2">
          {report.problems.map((problem, index) => (
            <li key={`${problem.service}-${problem.field}-${index}`} className="flex gap-2 text-xs">
              <ShieldAlert size={14} className="mt-0.5 shrink-0 text-danger" aria-hidden />
              <span>
                <span className="font-mono">
                  {problem.service}.{problem.field}
                </span>
                <span className="block text-muted">{problem.message}</span>
              </span>
            </li>
          ))}
        </ul>
      )}

      {report.warnings.length > 0 && <WarningList warnings={report.warnings} />}
    </div>
  );
}

/** Compose fields Iskele read but will not act on. */
function WarningList({ warnings }: { warnings: StackValidation['warnings'] }) {
  const { t } = useTranslation();

  return (
    <div>
      <h4 className="mb-1.5 text-xs font-semibold uppercase tracking-wide text-muted">
        {t('stacks.warnings')}
      </h4>
      <ul className="space-y-2">
        {warnings.map((warning, index) => (
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
  );
}

/** What saving and deploying this content would do. */
function DiffReport({ diff }: { diff: StackDiff }) {
  const { t } = useTranslation();

  const empty =
    diff.services.length === 0 && diff.networks.length === 0 && diff.volumes.length === 0;

  return (
    <div className="card space-y-3 p-4">
      <h3 className="text-sm font-semibold">{t('stacks.diff')}</h3>

      {empty ? (
        <p className="text-sm text-muted">{t('stacks.diff_empty')}</p>
      ) : (
        <ul className="space-y-2">
          {diff.services.map((change) => (
            <li key={change.service} className="text-xs">
              <span className="font-mono font-medium">{change.service}</span>{' '}
              <span
                className={
                  change.kind === 'removed'
                    ? 'text-danger'
                    : change.kind === 'added'
                      ? 'text-ok'
                      : 'text-warn'
                }
              >
                {t(`stacks.change_${change.kind}`)}
              </span>
              {change.fields && change.fields.length > 0 && (
                <span className="block text-muted">{change.fields.join(', ')}</span>
              )}
              {change.recreates && (
                <span className="block text-muted">{t('stacks.change_recreates')}</span>
              )}
            </li>
          ))}
          {[...diff.networks, ...diff.volumes].map((change) => (
            <li key={change.name} className="text-xs">
              <span className="font-mono">{change.name}</span>{' '}
              <span className="text-muted">{t(`stacks.change_${change.kind}`)}</span>
            </li>
          ))}
        </ul>
      )}

      {diff.warnings.length > 0 && <WarningList warnings={diff.warnings} />}
    </div>
  );
}
