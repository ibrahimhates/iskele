import type { ReactNode } from 'react';
import { ShieldAlert } from 'lucide-react';
import { useTranslation } from 'react-i18next';

interface Props {
  title: string;
  description?: string;
  children: ReactNode;
}

/** The shared frame for the two pre-sign-in screens. */
export function AuthLayout({ title, description, children }: Props) {
  const { t } = useTranslation();

  return (
    <div className="flex min-h-full items-center justify-center bg-bg p-4">
      <div className="w-full max-w-sm">
        <div className="mb-6 text-center">
          <h1 className="text-2xl font-semibold tracking-tight">{t('app.name')}</h1>
          <p className="mt-1 text-sm text-muted">{t('app.tagline')}</p>
        </div>

        <div className="card p-6">
          <h2 className="text-lg font-medium">{title}</h2>
          {description && <p className="mt-1.5 text-sm text-muted">{description}</p>}
          <div className="mt-5">{children}</div>
        </div>

        {/* The panel controls the Docker socket; nobody should reach this
            screen without being told what that means. */}
        <p className="mt-4 flex items-start gap-2 text-xs text-muted">
          <ShieldAlert size={14} className="mt-0.5 shrink-0" aria-hidden />
          {t('auth.socketWarning')}
        </p>
      </div>
    </div>
  );
}
