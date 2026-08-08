import { useState } from 'react';
import { useLocation, useNavigate } from 'react-router-dom';
import { useTranslation } from 'react-i18next';

import { auth as authApi } from '../../api/endpoints';
import { ApiError } from '../../api/client';
import { useAuth } from '../../stores/auth';
import { AuthLayout } from './AuthLayout';
import { ErrorPanel } from '../../components/ErrorPanel';
import { Spinner } from '../../components/Spinner';

export function LoginPage() {
  const { t } = useTranslation();
  const navigate = useNavigate();
  const location = useLocation();
  const signIn = useAuth((s) => s.signIn);

  const [username, setUsername] = useState('');
  const [password, setPassword] = useState('');
  const [error, setError] = useState<unknown>(null);
  const [busy, setBusy] = useState(false);

  async function onSubmit(event: React.FormEvent) {
    event.preventDefault();
    setError(null);
    setBusy(true);
    try {
      const session = await authApi.login(username, password);
      signIn(session);
      const from = (location.state as { from?: string } | null)?.from;
      navigate(from ?? '/dashboard', { replace: true });
    } catch (err) {
      setError(err);
    } finally {
      setBusy(false);
    }
  }

  const retryAfter =
    error instanceof ApiError && typeof error.details?.retry_after_seconds === 'number'
      ? error.details.retry_after_seconds
      : null;

  return (
    <AuthLayout title={t('auth.loginTitle')}>
      <form onSubmit={onSubmit} className="space-y-4">
        <div>
          <label htmlFor="username" className="mb-1.5 block text-sm font-medium">
            {t('auth.username')}
          </label>
          <input
            id="username"
            className="input"
            value={username}
            onChange={(e) => setUsername(e.target.value)}
            autoComplete="username"
            autoFocus
            required
          />
        </div>

        <div>
          <label htmlFor="password" className="mb-1.5 block text-sm font-medium">
            {t('auth.password')}
          </label>
          <input
            id="password"
            type="password"
            className="input"
            value={password}
            onChange={(e) => setPassword(e.target.value)}
            autoComplete="current-password"
            required
          />
        </div>

        {error != null && <ErrorPanel error={error} />}
        {retryAfter !== null && <p className="text-xs text-muted">Retry in {retryAfter}s.</p>}

        <button type="submit" className="btn-primary w-full" disabled={busy}>
          {busy ? <Spinner className="h-4 w-4" /> : t('auth.signIn')}
        </button>
      </form>
    </AuthLayout>
  );
}
