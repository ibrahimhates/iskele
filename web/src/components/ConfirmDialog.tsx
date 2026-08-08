import { useEffect, useRef, useState } from 'react';
import { useTranslation } from 'react-i18next';
import { AlertTriangle } from 'lucide-react';

interface Props {
  open: boolean;
  title: string;
  description?: string;
  /** When set, the operator must type this exact text to enable the button. */
  confirmText?: string;
  confirmLabel?: string;
  destructive?: boolean;
  busy?: boolean;
  children?: React.ReactNode;
  onConfirm: () => void;
  onCancel: () => void;
}

/**
 * A modal that makes a destructive action deliberate.
 *
 * For anything irreversible the operator retypes the resource's name: it is
 * the difference between "I meant this container" and "I clicked the wrong
 * row".
 */
export function ConfirmDialog({
  open,
  title,
  description,
  confirmText,
  confirmLabel,
  destructive,
  busy,
  children,
  onConfirm,
  onCancel,
}: Props) {
  const { t } = useTranslation();
  const [typed, setTyped] = useState('');
  const inputRef = useRef<HTMLInputElement>(null);

  useEffect(() => {
    if (open) {
      setTyped('');
      // Focus after paint so the dialog is in the accessibility tree first.
      requestAnimationFrame(() => inputRef.current?.focus());
    }
  }, [open]);

  useEffect(() => {
    if (!open) return;
    function onKeyDown(event: KeyboardEvent) {
      if (event.key === 'Escape') onCancel();
    }
    window.addEventListener('keydown', onKeyDown);
    return () => window.removeEventListener('keydown', onKeyDown);
  }, [open, onCancel]);

  if (!open) return null;

  const canConfirm = !confirmText || typed === confirmText;

  return (
    <div className="fixed inset-0 z-50 flex items-center justify-center bg-black/60 p-4">
      <div
        role="dialog"
        aria-modal="true"
        aria-labelledby="confirm-title"
        className="card w-full max-w-md p-5"
      >
        <div className="flex items-start gap-3">
          {destructive && (
            <AlertTriangle size={20} className="mt-0.5 shrink-0 text-danger" aria-hidden />
          )}
          <div className="min-w-0 flex-1">
            <h2 id="confirm-title" className="font-medium">
              {title}
            </h2>
            {description && <p className="mt-1.5 text-sm text-muted">{description}</p>}
          </div>
        </div>

        {children && <div className="mt-4">{children}</div>}

        {confirmText && (
          <div className="mt-4">
            <label htmlFor="confirm-input" className="mb-1.5 block text-sm">
              <code className="rounded bg-elevated px-1.5 py-0.5 font-mono text-xs">
                {confirmText}
              </code>
            </label>
            <input
              id="confirm-input"
              ref={inputRef}
              className="input font-mono"
              value={typed}
              onChange={(e) => setTyped(e.target.value)}
              autoComplete="off"
              spellCheck={false}
            />
          </div>
        )}

        <div className="mt-5 flex justify-end gap-2">
          <button type="button" className="btn-default" onClick={onCancel} disabled={busy}>
            {t('common.cancel')}
          </button>
          <button
            type="button"
            className={destructive ? 'btn-danger' : 'btn-primary'}
            onClick={onConfirm}
            disabled={!canConfirm || busy}
          >
            {confirmLabel ?? t('common.confirm')}
          </button>
        </div>
      </div>
    </div>
  );
}
