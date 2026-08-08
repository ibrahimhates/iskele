import { useState } from 'react';
import { useNavigate } from 'react-router-dom';
import { useTranslation } from 'react-i18next';
import { useQueryClient } from '@tanstack/react-query';

import { auth as authApi } from '../../api/endpoints';
import { useAuth } from '../../stores/auth';
import { AuthLayout } from './AuthLayout';
import { ErrorPanel } from '../../components/ErrorPanel';
import { Spinner } from '../../components/Spinner';
import { passwordProblem } from './password';

export function BootstrapPage() {
  const { t } = useTranslation();
  const navigate = useNavigate();
  const queryClient = useQueryClient();
  const signIn = useAuth((s) => s.signIn);

  const [username, setUsername] = useState('');
  const [password, setPassword] = useState('');
  const [confirm, setConfirm] = useState('');
  const [error, setError] = useState<unknown>(null);
  const [busy, setBusy] = useState(false);

  // Validate before the request so the operator is not told "422" for a rule
  // the form could have explained.
  const localProblem = password ? passwordProblem(password) : null;
  const mismatch = confirm.length > 0 && password !== confirm;
  const canSubmit = username.trim() !== '' && !localProblem && !mismatch && confirm !== '';

  async function onSubmit(event: React.FormEvent) {
    event.preventDefault();
    setError(null);
    setBusy(true);
    try {
      const session = await authApi.bootstrap(username, password);
      signIn(session);
      await queryClient.invalidateQueries({ queryKey: ['auth', 'status'] });
      navigate('/dashboard', { replace: true });
    } catch (err) {
      setError(err);
    } finally {
      setBusy(false);
    }
  }

  return (
    <AuthLayout title={t('auth.bootstrapTitle')} description={t('auth.bootstrapHelp')}>
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
            autoComplete="new-password"
            required
            aria-describedby="password-rules"
          />
          <p id="password-rules" className="mt-1.5 text-xs text-muted">
            {t('auth.passwordRules')}
          </p>
          {localProblem && <p className="mt-1 text-xs text-danger">{t(localProblem)}</p>}
        </div>

        <div>
          <label htmlFor="confirm" className="mb-1.5 block text-sm font-medium">
            {t('auth.confirmPassword')}
          </label>
          <input
            id="confirm"
            type="password"
            className="input"
            value={confirm}
            onChange={(e) => setConfirm(e.target.value)}
            autoComplete="new-password"
            required
          />
          {mismatch && <p className="mt-1 text-xs text-danger">{t('auth.passwordMismatch')}</p>}
        </div>

        {error != null && <ErrorPanel error={error} />}

        <button type="submit" className="btn-primary w-full" disabled={busy || !canSubmit}>
          {busy ? <Spinner className="h-4 w-4" /> : t('auth.create')}
        </button>
      </form>
    </AuthLayout>
  );
}
