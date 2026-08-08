import { AlertTriangle, WifiOff } from 'lucide-react';
import { useTranslation } from 'react-i18next';

import { useConnection } from '../stores/connection';

/**
 * A persistent band for the two failures that make the rest of the UI lie:
 * the API being unreachable, and Docker being down behind a working API.
 */
export function ConnectionBanner() {
  const { t } = useTranslation();
  const api = useConnection((s) => s.api);
  const dockerReachable = useConnection((s) => s.dockerReachable);
  const dockerError = useConnection((s) => s.dockerError);

  if (api !== 'connected') {
    return (
      <div
        className="flex items-center gap-2 bg-warn/15 px-4 py-2 text-sm text-warn"
        role="status"
        aria-live="polite"
      >
        <WifiOff size={15} aria-hidden />
        {api === 'reconnecting' ? t('connection.reconnecting') : t('connection.offline')}
      </div>
    );
  }

  if (!dockerReachable) {
    return (
      <div
        className="flex items-center gap-2 bg-danger/15 px-4 py-2 text-sm text-danger"
        role="alert"
      >
        <AlertTriangle size={15} aria-hidden />
        {t('connection.dockerDown', { reason: dockerError ?? '' })}
      </div>
    );
  }

  return null;
}
