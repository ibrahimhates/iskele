import type { ReactNode } from 'react';
import { Plus, Trash2 } from 'lucide-react';
import { useTranslation } from 'react-i18next';

/** A labelled form control with optional help text. */
export function Field({
  label,
  hint,
  htmlFor,
  children,
}: {
  label: string;
  hint?: ReactNode;
  htmlFor?: string;
  children: ReactNode;
}) {
  return (
    <div className="space-y-1">
      <label className="block text-sm font-medium" htmlFor={htmlFor}>
        {label}
      </label>
      {children}
      {hint && <p className="text-xs text-muted">{hint}</p>}
    </div>
  );
}

/** A checkbox with its label, which is the whole control's click target. */
export function Toggle({
  label,
  hint,
  checked,
  disabled,
  onChange,
}: {
  label: string;
  hint?: ReactNode;
  checked: boolean;
  disabled?: boolean;
  onChange: (value: boolean) => void;
}) {
  return (
    <label
      className={`flex items-start gap-2 text-sm ${disabled ? 'opacity-50' : 'cursor-pointer'}`}
    >
      <input
        type="checkbox"
        className="mt-0.5 accent-accent"
        checked={checked}
        disabled={disabled}
        onChange={(e) => onChange(e.target.checked)}
      />
      <span>
        <span className="font-medium">{label}</span>
        {hint && <span className="block text-xs text-muted">{hint}</span>}
      </span>
    </label>
  );
}

/** A group of related fields with a heading. */
export function Section({
  title,
  description,
  children,
}: {
  title: string;
  description?: ReactNode;
  children: ReactNode;
}) {
  return (
    <section className="space-y-3">
      <div>
        <h3 className="text-sm font-semibold">{title}</h3>
        {description && <p className="mt-0.5 text-xs text-muted">{description}</p>}
      </div>
      {children}
    </section>
  );
}

/**
 * A repeatable list of rows with add and remove controls.
 *
 * Rows carry their own id (see [Row] in ./state) so removing the second of
 * three does not move the third one's value into its place.
 */
export function RowList<T extends { id: number }>({
  rows,
  onAdd,
  onRemove,
  addLabel,
  emptyLabel,
  columns,
  children,
}: {
  rows: T[];
  onAdd: () => void;
  onRemove: (id: number) => void;
  addLabel: string;
  emptyLabel: string;
  /** Header labels, aligned with whatever the row renderer lays out. */
  columns?: string[];
  children: (row: T, index: number) => ReactNode;
}) {
  const { t } = useTranslation();

  return (
    <div className="space-y-2">
      {rows.length === 0 ? (
        <p className="rounded-md border border-dashed border-border px-3 py-4 text-center text-xs text-muted">
          {emptyLabel}
        </p>
      ) : (
        <div className="space-y-2">
          {columns && (
            <div className="hidden gap-2 px-1 text-xs uppercase tracking-wide text-muted sm:flex">
              {columns.map((column) => (
                <span key={column} className="flex-1">
                  {column}
                </span>
              ))}
              <span className="w-9" />
            </div>
          )}
          {rows.map((row, index) => (
            <div key={row.id} className="flex items-start gap-2">
              <div className="flex flex-1 flex-col gap-2 sm:flex-row">{children(row, index)}</div>
              <button
                type="button"
                className="btn-ghost h-9 w-9 shrink-0 p-0 text-muted hover:text-danger"
                onClick={() => onRemove(row.id)}
                aria-label={t('common.remove')}
              >
                <Trash2 size={16} aria-hidden />
              </button>
            </div>
          ))}
        </div>
      )}

      <button type="button" className="btn-default" onClick={onAdd}>
        <Plus size={14} aria-hidden />
        {addLabel}
      </button>
    </div>
  );
}

/**
 * The banner that marks a section as needing the privileged permission.
 *
 * It is shown whether or not the operator holds it: someone who does needs to
 * know these fields are the dangerous ones, and someone who does not needs to
 * know why the server will refuse.
 */
export function PrivilegedNotice({ allowed, children }: { allowed: boolean; children: ReactNode }) {
  return (
    <div
      className={`rounded-md border px-3 py-2 text-xs ${
        allowed ? 'border-warn/40 bg-warn/10' : 'border-danger/40 bg-danger/10'
      }`}
    >
      {children}
    </div>
  );
}
