import { AlertCircle } from 'lucide-react';
import { useTranslation } from 'react-i18next';

import { ApiError } from '../api/client';

interface Props {
  error: unknown;
  onRetry?: () => void;
}

/**
 * Renders a failure without hiding it.
 *
 * The engine's own wording is shown verbatim: "port is already allocated" is
 * the sentence that tells an operator what to do next, and paraphrasing it
 * would throw that away.
 */
export function ErrorPanel({ error, onRetry }: Props) {
  const { t } = useTranslation();

  const apiError = error instanceof ApiError ? error : null;
  const message = apiError?.message ?? (error instanceof Error ? error.message : String(error));

  return (
    <div className="card border-danger/40 bg-danger/5 p-4" role="alert">
      <div className="flex items-start gap-3">
        <AlertCircle size={18} className="mt-0.5 shrink-0 text-danger" aria-hidden />
        <div className="min-w-0 flex-1">
          <p className="font-medium text-danger">{apiError?.code ?? t('errors.generic')}</p>
          <p className="mt-1 break-words text-sm text-fg/80">{message}</p>
        </div>
        {onRetry && (
          <button type="button" className="btn-default shrink-0" onClick={onRetry}>
            {t('common.retry')}
          </button>
        )}
      </div>
    </div>
  );
}
