import { useEffect, useState } from 'react';
import { useMutation, useQueryClient } from '@tanstack/react-query';
import { useTranslation } from 'react-i18next';
import { ShieldCheck, ShieldOff } from 'lucide-react';

import { auth as authApi } from '../../api/endpoints';
import type { TOTPSetup } from '../../api/types';
import { ErrorPanel } from '../../components/ErrorPanel';
import { Spinner } from '../../components/Spinner';
import { useAuth } from '../../stores/auth';
import { qrDataURL } from '../../lib/qr';

/**
 * Two-factor enrollment for the signed-in account.
 *
 * It only ever acts on the caller's own account — an admin who needs to clear
 * somebody else's lost device does that from the users page, and even they
 * cannot read the secret.
 */
export function TwoFactorPanel() {
  const { t } = useTranslation();
  const queryClient = useQueryClient();
  const user = useAuth((s) => s.user);
  const setUser = useAuth((s) => s.setUser);

  const [setup, setSetup] = useState<TOTPSetup | null>(null);
  const [qr, setQR] = useState<string | null>(null);
  const [code, setCode] = useState('');

  const enabled = user?.totp_enabled ?? false;

  // The QR is drawn from the URI the server sent; nothing is generated here
  // that the server did not already commit to.
  useEffect(() => {
    if (!setup) {
      setQR(null);
      return;
    }
    let live = true;
    void qrDataURL(setup.uri).then((url) => {
      if (live) setQR(url);
    });
    return () => {
      live = false;
    };
  }, [setup]);

  const begin = useMutation({
    mutationFn: () => authApi.totpSetup(),
    onSuccess: (result) => {
      setSetup(result);
      setCode('');
    },
  });

  const confirm = useMutation({
    mutationFn: () => authApi.totpVerify(code),
    onSuccess: () => {
      setSetup(null);
      setCode('');
      if (user) setUser({ ...user, totp_enabled: true });
      void queryClient.invalidateQueries({ queryKey: ['users'] });
    },
  });

  const disable = useMutation({
    mutationFn: () => authApi.totpDisable(code),
    onSuccess: () => {
      setCode('');
      if (user) setUser({ ...user, totp_enabled: false });
      void queryClient.invalidateQueries({ queryKey: ['users'] });
    },
  });

  const busy = begin.isPending || confirm.isPending || disable.isPending;
  const error = begin.error ?? confirm.error ?? disable.error;

  return (
    <section className="card p-4">
      <h2 className="mb-1 flex items-center gap-2 text-sm font-medium">
        {enabled ? (
          <ShieldCheck size={16} className="text-ok" aria-hidden />
        ) : (
          <ShieldOff size={16} className="text-muted" aria-hidden />
        )}
        {t('settings.two_factor')}
      </h2>
      <p className="mb-3 text-xs text-muted">
        {enabled ? t('settings.two_factor_on') : t('settings.two_factor_off')}
      </p>

      {error != null && (
        <div className="mb-3">
          <ErrorPanel error={error} />
        </div>
      )}

      {enabled ? (
        <form
          className="flex flex-wrap items-end gap-2"
          onSubmit={(e) => {
            e.preventDefault();
            disable.mutate();
          }}
        >
          <div>
            <label htmlFor="totp-disable" className="mb-1.5 block text-xs text-muted">
              {t('settings.two_factor_confirm_disable')}
            </label>
            <CodeInput id="totp-disable" value={code} onChange={setCode} />
          </div>
          <button type="submit" className="btn-danger" disabled={busy || code.length < 6}>
            {t('settings.two_factor_disable')}
          </button>
        </form>
      ) : setup ? (
        <div className="space-y-3">
          <div className="flex flex-wrap items-start gap-4">
            {qr ? (
              // The QR is decorative: the secret beside it is the same value,
              // and typing it in is a supported path, not a fallback.
              <img
                src={qr}
                alt={t('settings.two_factor_qr_alt')}
                className="h-40 w-40 rounded bg-white p-2"
              />
            ) : (
              <div className="flex h-40 w-40 items-center justify-center rounded border border-border">
                <Spinner />
              </div>
            )}

            <div className="min-w-0 flex-1 space-y-2">
              <p className="text-xs text-muted">{t('settings.two_factor_scan')}</p>
              <div>
                <span className="mb-1 block text-xs text-muted">
                  {t('settings.two_factor_manual')}
                </span>
                <code className="block break-all rounded bg-elevated px-2 py-1.5 font-mono text-xs">
                  {setup.secret}
                </code>
              </div>
            </div>
          </div>

          <form
            className="flex flex-wrap items-end gap-2"
            onSubmit={(e) => {
              e.preventDefault();
              confirm.mutate();
            }}
          >
            <div>
              <label htmlFor="totp-confirm" className="mb-1.5 block text-xs text-muted">
                {t('settings.two_factor_confirm')}
              </label>
              <CodeInput id="totp-confirm" value={code} onChange={setCode} autoFocus />
            </div>
            <button type="submit" className="btn-primary" disabled={busy || code.length < 6}>
              {t('settings.two_factor_enable')}
            </button>
            <button type="button" className="btn-default" onClick={() => setSetup(null)}>
              {t('common.cancel')}
            </button>
          </form>
        </div>
      ) : (
        <button
          type="button"
          className="btn-primary"
          disabled={busy}
          onClick={() => begin.mutate()}
        >
          {begin.isPending ? <Spinner className="h-4 w-4" /> : t('settings.two_factor_start')}
        </button>
      )}
    </section>
  );
}

/** A six-digit code field, shared by the confirm and disable forms. */
function CodeInput({
  id,
  value,
  onChange,
  autoFocus,
}: {
  id: string;
  value: string;
  onChange: (value: string) => void;
  autoFocus?: boolean;
}) {
  return (
    <input
      id={id}
      className="input w-32 text-center font-mono tracking-[0.3em]"
      value={value}
      inputMode="numeric"
      autoComplete="one-time-code"
      maxLength={6}
      placeholder="000000"
      autoFocus={autoFocus}
      // The app displays codes as digits; anything else is a typo, and
      // silently dropping it beats an error message about a stray space.
      onChange={(e) => onChange(e.target.value.replace(/\D/g, '').slice(0, 6))}
    />
  );
}
