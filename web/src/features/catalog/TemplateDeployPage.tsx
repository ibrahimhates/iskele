import { useEffect, useMemo, useState } from 'react';
import { Link, useNavigate, useParams } from 'react-router-dom';
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import { useTranslation } from 'react-i18next';
import { ArrowLeft, ExternalLink, Rocket, ShieldAlert, Sparkles } from 'lucide-react';

import { ApiError } from '../../api/client';
import { catalog as catalogApi, networks as networksApi } from '../../api/endpoints';
import type { Template, TemplateField } from '../../api/types';
import { PageHeader } from '../../components/PageHeader';
import { ErrorPanel } from '../../components/ErrorPanel';
import { Spinner } from '../../components/Spinner';
import { Field, Toggle } from '../create/fields';
import { useAuth } from '../../stores/auth';
import { TemplateIcon } from './TemplateIcon';

/**
 * One template's form.
 *
 * The questions come from the template, so this page has no knowledge of any
 * particular application: adding an entry to the catalog adds a working form.
 */
export function TemplateDeployPage() {
  const { t } = useTranslation();
  const navigate = useNavigate();
  const queryClient = useQueryClient();
  const { id = '' } = useParams<{ id: string }>();

  const canCreate = useAuth((s) => s.can('create'));
  const canPrivileged = useAuth((s) => s.can('privileged'));

  const [name, setName] = useState('');
  const [values, setValues] = useState<Record<string, string>>({});
  const [network, setNetwork] = useState('');
  const [start, setStart] = useState(true);
  const [generating, setGenerating] = useState<string | null>(null);

  const query = useQuery({
    queryKey: ['templates', id],
    queryFn: () => catalogApi.get(id),
    enabled: id !== '',
  });

  const networks = useQuery({ queryKey: ['networks'], queryFn: networksApi.list });

  // The template's defaults are the starting point, seeded once it arrives.
  useEffect(() => {
    const template = query.data;
    if (!template) return;

    setName((current) => current || template.id);
    setValues((current) => {
      if (Object.keys(current).length > 0) return current;
      const seeded: Record<string, string> = {};
      for (const field of template.fields ?? []) {
        if (field.default) seeded[field.name] = field.default;
      }
      return seeded;
    });
  }, [query.data]);

  const deploy = useMutation({
    mutationFn: () =>
      catalogApi.deploy(id, { name: name.trim(), values, network: network || undefined, start }),
    onSuccess: (result) => {
      void queryClient.invalidateQueries({ queryKey: ['containers'] });
      void queryClient.invalidateQueries({ queryKey: ['templates'] });
      navigate(`/containers/${encodeURIComponent(result.id)}`, {
        state: { notes: result.notes },
      });
    },
  });

  const fieldErrors = useMemo(() => rejectedFields(deploy.error), [deploy.error]);

  const generate = async (field: TemplateField) => {
    setGenerating(field.name);
    try {
      const { secret } = await catalogApi.secret(field.generate_length);
      setValues((current) => ({ ...current, [field.name]: secret }));
    } finally {
      setGenerating(null);
    }
  };

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

  const template = query.data as Template;
  const blocked = template.needs_privileged && !canPrivileged;

  return (
    <>
      <PageHeader
        title={template.title}
        description={template.description}
        actions={
          <>
            <Link className="btn-default" to="/catalog">
              <ArrowLeft size={14} aria-hidden />
              {t('common.back')}
            </Link>
            {canCreate && (
              <button
                type="button"
                className="btn-primary"
                disabled={deploy.isPending || blocked}
                onClick={() => deploy.mutate()}
              >
                <Rocket size={14} aria-hidden />
                {t('catalog.deploy')}
              </button>
            )}
          </>
        }
      />

      {blocked && (
        <div className="mb-4 flex items-start gap-2 rounded border border-warn/40 bg-warn/10 p-3 text-sm">
          <ShieldAlert size={16} className="mt-0.5 shrink-0 text-warn" aria-hidden />
          <span>
            {t('catalog.privileged_blocked')}
            <span className="mt-1 block text-xs text-muted">
              {t('catalog.privileged_blocked_hint')}
            </span>
          </span>
        </div>
      )}

      {deploy.error && (
        <div className="mb-4">
          <ErrorPanel error={deploy.error} />
        </div>
      )}

      <div className="grid gap-4 lg:grid-cols-[2fr,1fr]">
        <div className="card space-y-4 p-4">
          <Field label={t('catalog.container_name')} htmlFor="template-name">
            <input
              id="template-name"
              className="input font-mono"
              value={name}
              spellCheck={false}
              onChange={(e) => setName(e.target.value)}
            />
          </Field>

          {(template.fields ?? []).map((field) => (
            <TemplateFieldInput
              key={field.name}
              field={field}
              value={values[field.name] ?? ''}
              error={fieldErrors[field.name]}
              generating={generating === field.name}
              onChange={(next) => setValues((current) => ({ ...current, [field.name]: next }))}
              onGenerate={() => void generate(field)}
            />
          ))}

          <Field
            label={t('catalog.network')}
            htmlFor="template-network"
            hint={t('catalog.network_hint')}
          >
            <select
              id="template-network"
              className="input"
              value={network}
              onChange={(e) => setNetwork(e.target.value)}
            >
              <option value="">{t('catalog.network_default')}</option>
              {(networks.data?.items ?? []).map((item) => (
                <option key={item.id} value={item.name}>
                  {item.name}
                </option>
              ))}
            </select>
          </Field>

          <Toggle
            label={t('catalog.start')}
            hint={t('catalog.start_hint')}
            checked={start}
            onChange={setStart}
          />
        </div>

        <div className="space-y-4">
          <div className="card space-y-3 p-4">
            <div className="flex items-center gap-2">
              <TemplateIcon name={template.icon} className="text-accent" />
              <h3 className="text-sm font-semibold">{template.title}</h3>
            </div>
            <dl className="space-y-1.5 text-xs">
              <div>
                <dt className="text-muted">{t('common.image')}</dt>
                <dd className="font-mono">{template.image}</dd>
              </div>
              <div>
                <dt className="text-muted">{t('catalog.category')}</dt>
                <dd>
                  {t(`catalog.category_${template.category}`, { defaultValue: template.category })}
                </dd>
              </div>
              {template.deployed ? (
                <div>
                  <dt className="text-muted">{t('catalog.already_deployed')}</dt>
                  <dd>{template.deployed}</dd>
                </div>
              ) : null}
            </dl>

            <div className="flex flex-wrap gap-2 pt-1">
              {template.website && (
                <a
                  className="btn-ghost text-xs"
                  href={template.website}
                  target="_blank"
                  rel="noreferrer noopener"
                >
                  <ExternalLink size={12} aria-hidden />
                  {t('catalog.website')}
                </a>
              )}
              {template.documentation && (
                <a
                  className="btn-ghost text-xs"
                  href={template.documentation}
                  target="_blank"
                  rel="noreferrer noopener"
                >
                  <ExternalLink size={12} aria-hidden />
                  {t('catalog.documentation')}
                </a>
              )}
            </div>
          </div>

          {template.notes && (
            <div className="card p-4">
              <h3 className="mb-2 text-sm font-semibold">{t('catalog.notes')}</h3>
              <p className="whitespace-pre-wrap text-xs text-muted">{template.notes}</p>
            </div>
          )}
        </div>
      </div>
    </>
  );
}

/** One question, rendered by its type. */
function TemplateFieldInput({
  field,
  value,
  error,
  generating,
  onChange,
  onGenerate,
}: {
  field: TemplateField;
  value: string;
  error?: string;
  generating: boolean;
  onChange: (value: string) => void;
  onGenerate: () => void;
}) {
  const { t } = useTranslation();
  const id = `template-field-${field.name}`;

  const label = field.required ? `${field.label} *` : field.label;
  const hint = error ? <span className="text-danger">{error}</span> : field.help;

  if (field.type === 'bool') {
    return (
      <Toggle
        label={label}
        hint={hint}
        checked={value === 'true'}
        onChange={(next) => onChange(String(next))}
      />
    );
  }

  if (field.type === 'select') {
    return (
      <Field label={label} htmlFor={id} hint={hint}>
        <select id={id} className="input" value={value} onChange={(e) => onChange(e.target.value)}>
          {!field.required && <option value="" />}
          {(field.options ?? []).map((option) => (
            <option key={option.value} value={option.value}>
              {option.label}
            </option>
          ))}
        </select>
      </Field>
    );
  }

  const inputType = field.type === 'password' ? 'password' : 'text';
  const numeric = field.type === 'number' || field.type === 'port';

  return (
    <Field label={label} htmlFor={id} hint={hint}>
      <div className="flex gap-2">
        <input
          id={id}
          className={`input flex-1 ${field.type === 'text' ? '' : 'font-mono'}`}
          type={numeric ? 'number' : inputType}
          value={value}
          min={field.min}
          max={field.max}
          spellCheck={false}
          autoComplete={field.type === 'password' ? 'new-password' : 'off'}
          onChange={(e) => onChange(e.target.value)}
        />
        {field.generate && (
          <button
            type="button"
            className="btn-default shrink-0"
            disabled={generating}
            onClick={onGenerate}
          >
            <Sparkles size={14} aria-hidden />
            {t('catalog.generate')}
          </button>
        )}
      </div>
    </Field>
  );
}

/**
 * Reads the per-field messages out of a rejected deploy.
 *
 * The server reports every bad answer at once, so the form can mark all of
 * them rather than the first.
 */
function rejectedFields(error: unknown): Record<string, string> {
  if (!(error instanceof ApiError)) return {};

  const fields = error.details?.fields;
  if (!Array.isArray(fields)) return {};

  const out: Record<string, string> = {};
  for (const item of fields) {
    if (item && typeof item === 'object' && 'field' in item && 'message' in item) {
      out[String(item.field)] = String(item.message);
    }
  }
  return out;
}
