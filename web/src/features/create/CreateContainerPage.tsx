import { useEffect, useMemo, useRef, useState } from 'react';
import { useNavigate, useSearchParams } from 'react-router-dom';
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import { useTranslation } from 'react-i18next';
import { Check, Copy, Rocket } from 'lucide-react';

import { containers as containersApi, networks as networksApi, system } from '../../api/endpoints';
import { PageHeader } from '../../components/PageHeader';
import { ErrorPanel } from '../../components/ErrorPanel';
import { useAuth } from '../../stores/auth';
import { cleanSpec, dockerRunCommand } from './preview';
import { privilegedOptionsUsed, useCreateForm } from './state';
import type { TabProps } from './tabs';
import {
  AdvancedTab,
  CommandTab,
  EnvTab,
  GeneralTab,
  HealthTab,
  LabelsTab,
  NetworkTab,
  PortsTab,
  ResourcesTab,
  VolumesTab,
} from './tabs';

const TABS: { key: string; Component: (props: TabProps) => JSX.Element }[] = [
  { key: 'general', Component: GeneralTab },
  { key: 'command', Component: CommandTab },
  { key: 'ports', Component: PortsTab },
  { key: 'volumes', Component: VolumesTab },
  { key: 'env', Component: EnvTab },
  { key: 'network', Component: NetworkTab },
  { key: 'labels', Component: LabelsTab },
  { key: 'resources', Component: ResourcesTab },
  { key: 'health', Component: HealthTab },
  { key: 'advanced', Component: AdvancedTab },
];

export function CreateContainerPage() {
  const { t } = useTranslation();
  const navigate = useNavigate();
  const queryClient = useQueryClient();
  const canPrivileged = useAuth((s) => s.can('privileged'));

  const { form, set, spec } = useCreateForm();
  const [active, setActive] = useState('general');

  // An image can be handed in by whoever sent the operator here — the image
  // list, or a build that has just produced one. Seeded once: re-applying it
  // would fight an operator who then edits the field.
  const [params] = useSearchParams();
  const seeded = useRef(false);
  useEffect(() => {
    const image = params.get('image');
    if (image && !seeded.current) {
      seeded.current = true;
      set('image', image);
    }
  }, [params, set]);

  const allowedPaths = useQuery({
    queryKey: ['system', 'allowed-paths'],
    queryFn: system.allowedPaths,
    staleTime: Infinity,
  });

  const networks = useQuery({ queryKey: ['networks'], queryFn: networksApi.list });

  const create = useMutation({
    mutationFn: () => containersApi.create(spec),
    onSuccess: (result) => {
      void queryClient.invalidateQueries({ queryKey: ['containers'] });
      navigate(`/containers/${encodeURIComponent(result.id)}`);
    },
  });

  const command = useMemo(() => dockerRunCommand(spec), [spec]);
  const payload = useMemo(() => JSON.stringify(cleanSpec(spec), null, 2), [spec]);
  const privileged = privilegedOptionsUsed(form);

  const tabProps: TabProps = {
    form,
    set,
    canPrivileged,
    allowedPaths: allowedPaths.data?.paths ?? [],
    networks: (networks.data?.items ?? []).map((n) => n.name),
  };

  const ActiveTab = TABS.find((tab) => tab.key === active)?.Component ?? GeneralTab;
  const canSubmit = spec.image.trim() !== '' && !create.isPending;

  return (
    <>
      <PageHeader title={t('create.title')} description={t('create.subtitle')} />

      <div className="grid gap-6 lg:grid-cols-[minmax(0,1fr)_24rem]">
        <div className="min-w-0 space-y-4">
          <nav className="flex flex-wrap gap-1" aria-label={t('create.title')}>
            {TABS.map((tab) => (
              <button
                key={tab.key}
                type="button"
                className={`rounded-md px-3 py-1.5 text-sm transition-colors ${
                  active === tab.key
                    ? 'bg-accent text-accent-fg'
                    : 'text-muted hover:bg-elevated hover:text-fg'
                }`}
                aria-current={active === tab.key ? 'page' : undefined}
                onClick={() => setActive(tab.key)}
              >
                {t(`create.tabs.${tab.key}`)}
              </button>
            ))}
          </nav>

          <div className="card p-4">
            <ActiveTab {...tabProps} />
          </div>

          {create.error && <ErrorPanel error={create.error} />}

          <div className="flex flex-wrap items-center gap-3">
            <button
              type="button"
              className="btn-primary"
              disabled={!canSubmit}
              onClick={() => create.mutate()}
            >
              <Rocket size={14} aria-hidden />
              {create.isPending ? t('create.submitting') : t('create.submit')}
            </button>

            {spec.image.trim() === '' && (
              <span className="text-xs text-muted">{t('create.image_required')}</span>
            )}

            {privileged.length > 0 && !canPrivileged && (
              <span className="text-xs text-danger">
                {t('create.privileged_denied')} ({privileged.join(', ')})
              </span>
            )}
          </div>
        </div>

        <aside className="space-y-4 lg:sticky lg:top-4 lg:self-start">
          <PreviewBlock title={t('create.preview_command')} content={command} />
          <PreviewBlock title={t('create.preview_payload')} content={payload} />
        </aside>
      </div>
    </>
  );
}

/**
 * One preview pane.
 *
 * Both panes render the same object the API will receive, so neither can drift
 * from what is actually sent.
 */
function PreviewBlock({ title, content }: { title: string; content: string }) {
  const { t } = useTranslation();
  const [copied, setCopied] = useState(false);

  async function copy() {
    try {
      await navigator.clipboard.writeText(content);
      setCopied(true);
      setTimeout(() => setCopied(false), 1500);
    } catch {
      // A clipboard the browser refuses is not worth an error dialog; the text
      // is on screen and selectable.
    }
  }

  return (
    <div className="card">
      <div className="flex items-center justify-between border-b border-border px-3 py-2">
        <h2 className="text-xs font-semibold uppercase tracking-wide text-muted">{title}</h2>
        <button type="button" className="btn-ghost py-1" onClick={() => void copy()}>
          {copied ? <Check size={14} aria-hidden /> : <Copy size={14} aria-hidden />}
          {copied ? t('common.copied') : t('common.copy')}
        </button>
      </div>
      <pre className="max-h-96 overflow-auto p-3 font-mono text-xs leading-relaxed">{content}</pre>
    </div>
  );
}
