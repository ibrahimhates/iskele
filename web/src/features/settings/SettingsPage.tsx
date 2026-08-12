import { useQuery } from '@tanstack/react-query';
import { useTranslation } from 'react-i18next';

import { system } from '../../api/endpoints';
import { PageHeader } from '../../components/PageHeader';
import { RegistriesPanel } from './RegistriesPanel';
import { TwoFactorPanel } from './TwoFactorPanel';
import { PrunePanel } from './PrunePanel';
import { ThemeToggle } from '../../components/ThemeToggle';
import { setLanguage } from '../../lib/i18n';
import { useAuth } from '../../stores/auth';

export function SettingsPage() {
  const { t, i18n } = useTranslation();
  const user = useAuth((s) => s.user);
  const isAdmin = useAuth((s) => s.can('admin'));
  const canPrune = useAuth((s) => s.can('prune'));
  const version = useQuery({ queryKey: ['version'], queryFn: system.version });

  return (
    <>
      <PageHeader title={t('settings.title')} />

      <div className="grid max-w-3xl gap-4">
        <section className="card p-4">
          <h2 className="mb-3 text-sm font-medium">{t('settings.appearance')}</h2>

          <div className="flex items-center justify-between gap-4 py-2">
            <span className="text-sm">{t('settings.theme')}</span>
            <ThemeToggle />
          </div>

          <div className="flex items-center justify-between gap-4 py-2">
            <label htmlFor="language" className="text-sm">
              {t('settings.language')}
            </label>
            <select
              id="language"
              className="input w-40"
              value={i18n.language.startsWith('tr') ? 'tr' : 'en'}
              onChange={(e) => setLanguage(e.target.value as 'en' | 'tr')}
            >
              <option value="en">English</option>
              <option value="tr">Türkçe</option>
            </select>
          </div>
        </section>

        <TwoFactorPanel />

        {/* Registry credentials reach outside this host, so only an admin
            sees them at all. */}
        {isAdmin && (
          <div className="card p-4">
            <RegistriesPanel />
          </div>
        )}

        {/* Prune needs the prune permission, which only an admin has. */}
        {canPrune && <PrunePanel />}

        <section className="card p-4">
          <h2 className="mb-3 text-sm font-medium">{t('settings.about')}</h2>
          <dl className="space-y-1.5 text-sm">
            <div className="flex justify-between gap-4">
              <dt className="text-muted">Version</dt>
              <dd className="font-mono text-xs">{version.data?.version ?? '—'}</dd>
            </div>
            <div className="flex justify-between gap-4">
              <dt className="text-muted">Commit</dt>
              <dd className="font-mono text-xs">{version.data?.commit ?? '—'}</dd>
            </div>
            <div className="flex justify-between gap-4">
              <dt className="text-muted">Platform</dt>
              <dd className="font-mono text-xs">{version.data?.platform ?? '—'}</dd>
            </div>
            <div className="flex justify-between gap-4">
              <dt className="text-muted">Signed in as</dt>
              <dd className="font-mono text-xs">
                {user?.username} ({user?.role})
              </dd>
            </div>
          </dl>
        </section>
      </div>
    </>
  );
}
