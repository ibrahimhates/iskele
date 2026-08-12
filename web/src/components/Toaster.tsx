import { useTranslation } from 'react-i18next';
import { AlertCircle, CheckCircle2, Info, X } from 'lucide-react';

import { cn } from '../lib/cn';
import { useToasts, type ToastKind } from '../stores/toast';

const ICONS: Record<ToastKind, typeof Info> = {
  success: CheckCircle2,
  error: AlertCircle,
  info: Info,
};

const TONES: Record<ToastKind, string> = {
  success: 'border-ok/40 text-ok',
  error: 'border-danger/40 text-danger',
  info: 'border-border text-accent',
};

/**
 * The notification stack, mounted once at the app root.
 *
 * It is an aria-live region so a screen reader announces an outcome the same
 * way a sighted operator sees it — the whole point of a toast is that the
 * result of an action reaches you when your attention is elsewhere.
 */
export function Toaster() {
  const { t } = useTranslation();
  const toasts = useToasts((s) => s.toasts);
  const dismiss = useToasts((s) => s.dismiss);

  if (toasts.length === 0) return null;

  return (
    <div
      className="pointer-events-none fixed bottom-4 right-4 z-50 flex w-80 max-w-[calc(100vw-2rem)] flex-col gap-2"
      role="region"
      aria-label={t('toast.region')}
    >
      {toasts.map((item) => {
        const Icon = ICONS[item.kind];
        return (
          <div
            key={item.id}
            className={cn(
              'pointer-events-auto flex items-start gap-2 rounded-md border bg-surface p-3 shadow-lg',
              TONES[item.kind],
            )}
            // A failure is not an interruption to announce over whatever the
            // operator is reading; a success is even less so.
            role={item.kind === 'error' ? 'alert' : 'status'}
            aria-live={item.kind === 'error' ? 'assertive' : 'polite'}
          >
            <Icon size={16} className="mt-0.5 shrink-0" aria-hidden />
            <div className="min-w-0 flex-1">
              <p className="text-sm text-fg">{item.message}</p>
              {item.detail && (
                <p className="mt-0.5 break-words text-xs text-muted">{item.detail}</p>
              )}
            </div>
            <button
              type="button"
              className="shrink-0 text-muted transition-colors hover:text-fg"
              onClick={() => dismiss(item.id)}
              aria-label={t('common.close')}
            >
              <X size={14} aria-hidden />
            </button>
          </div>
        );
      })}
    </div>
  );
}
