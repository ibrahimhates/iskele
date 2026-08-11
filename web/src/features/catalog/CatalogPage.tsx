import { useMemo, useState } from 'react';
import { Link } from 'react-router-dom';
import { useQuery } from '@tanstack/react-query';
import { useTranslation } from 'react-i18next';
import { AlertTriangle, LayoutGrid, ShieldAlert } from 'lucide-react';

import { catalog as catalogApi } from '../../api/endpoints';
import type { Template } from '../../api/types';
import { PageHeader } from '../../components/PageHeader';
import { EmptyState } from '../../components/EmptyState';
import { ErrorPanel } from '../../components/ErrorPanel';
import { Spinner } from '../../components/Spinner';
import { TemplateIcon } from './TemplateIcon';

/** The app catalog: one card per template, filtered by category and search. */
export function CatalogPage() {
  const { t } = useTranslation();
  const [category, setCategory] = useState('');
  const [search, setSearch] = useState('');

  const query = useQuery({ queryKey: ['templates'], queryFn: catalogApi.list });

  const shown = useMemo(() => {
    const items = query.data?.items ?? [];
    const needle = search.trim().toLowerCase();

    return items.filter((template) => {
      if (category && template.category !== category) return false;
      if (!needle) return true;
      return matches(template, needle);
    });
  }, [query.data, category, search]);

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

  const categories = query.data?.categories ?? [];
  const problems = query.data?.problems ?? [];

  return (
    <>
      <PageHeader
        title={t('nav.catalog')}
        description={t('catalog.count', { count: query.data?.total ?? 0 })}
      />

      {problems.length > 0 && (
        <div className="card mb-4 border-warn/40 p-4">
          <h2 className="flex items-center gap-2 text-sm font-semibold">
            <AlertTriangle size={16} className="text-warn" aria-hidden />
            {t('catalog.problems')}
          </h2>
          <ul className="mt-2 space-y-1">
            {problems.map((problem) => (
              <li key={problem.path} className="text-xs">
                <span className="font-mono">{problem.path}</span>
                <span className="block text-muted">{problem.message}</span>
              </li>
            ))}
          </ul>
        </div>
      )}

      <div className="mb-4 flex flex-wrap items-center gap-2">
        <input
          className="input max-w-64"
          value={search}
          placeholder={t('common.search')}
          aria-label={t('common.search')}
          onChange={(e) => setSearch(e.target.value)}
        />

        <button
          type="button"
          className={category === '' ? 'btn-primary' : 'btn-default'}
          onClick={() => setCategory('')}
        >
          {t('common.all')}
        </button>
        {categories.map((name) => (
          <button
            key={name}
            type="button"
            className={category === name ? 'btn-primary' : 'btn-default'}
            onClick={() => setCategory(name)}
          >
            {t(`catalog.category_${name}`, { defaultValue: name })}
          </button>
        ))}
      </div>

      {shown.length === 0 ? (
        <EmptyState
          icon={<LayoutGrid size={32} aria-hidden />}
          title={t('catalog.no_matches')}
          description={t('catalog.no_matches_hint')}
        />
      ) : (
        <div className="grid gap-3 sm:grid-cols-2 lg:grid-cols-3">
          {shown.map((template) => (
            <TemplateCard key={template.id} template={template} />
          ))}
        </div>
      )}
    </>
  );
}

/** One card in the catalog grid. */
function TemplateCard({ template }: { template: Template }) {
  const { t } = useTranslation();

  return (
    <Link
      to={`/catalog/${encodeURIComponent(template.id)}`}
      className="card flex flex-col gap-2 p-4 transition-colors hover:border-accent"
    >
      <div className="flex items-start gap-3">
        <TemplateIcon name={template.icon} className="mt-0.5 shrink-0 text-accent" />
        <div className="min-w-0 flex-1">
          <h3 className="font-medium">{template.title}</h3>
          <p className="truncate font-mono text-xs text-muted">{template.image}</p>
        </div>
        {template.deployed ? (
          <span className="badge bg-ok/15 text-ok">
            {t('catalog.deployed', { count: template.deployed })}
          </span>
        ) : null}
      </div>

      {template.description && <p className="text-sm text-muted">{template.description}</p>}

      <div className="mt-auto flex flex-wrap items-center gap-1.5 pt-1">
        <span className="badge bg-elevated text-muted">
          {t(`catalog.category_${template.category}`, { defaultValue: template.category })}
        </span>
        {template.source === 'custom' && (
          <span className="badge bg-elevated text-muted">{t('catalog.custom')}</span>
        )}
        {template.needs_privileged && (
          <span className="badge flex items-center gap-1 bg-warn/15 text-warn">
            <ShieldAlert size={12} aria-hidden />
            {t('catalog.privileged')}
          </span>
        )}
      </div>
    </Link>
  );
}

/** matches searches the fields an operator would type into a search box. */
function matches(template: Template, needle: string): boolean {
  const haystack = [
    template.title,
    template.id,
    template.description ?? '',
    template.image,
    ...(template.keywords ?? []),
  ]
    .join(' ')
    .toLowerCase();

  return haystack.includes(needle);
}
