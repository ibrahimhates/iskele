import { useMemo, useState } from 'react';
import { useTranslation } from 'react-i18next';
import { Check, Copy, Search } from 'lucide-react';

/**
 * Shows the engine's raw inspect payload.
 *
 * It is rendered as formatted JSON rather than a field-by-field table: the
 * point of this tab is that nothing the engine reports is hidden or reshaped.
 */
export function JsonViewer({ value }: { value: unknown }) {
  const { t } = useTranslation();
  const [filter, setFilter] = useState('');
  const [copied, setCopied] = useState(false);

  const text = useMemo(() => JSON.stringify(value, null, 2), [value]);

  const lines = useMemo(() => {
    const all = text.split('\n');
    const needle = filter.trim().toLowerCase();
    if (!needle) return all;
    return all.filter((line) => line.toLowerCase().includes(needle));
  }, [text, filter]);

  async function copy() {
    try {
      await navigator.clipboard.writeText(text);
      setCopied(true);
      setTimeout(() => setCopied(false), 1500);
    } catch {
      /* clipboard permission denied; nothing useful to say */
    }
  }

  return (
    <div className="space-y-2">
      <div className="flex flex-wrap items-center gap-2">
        <div className="relative min-w-48 flex-1">
          <Search
            size={14}
            className="pointer-events-none absolute left-2.5 top-1/2 -translate-y-1/2 text-muted"
            aria-hidden
          />
          <input
            className="input py-1.5 pl-8 text-sm"
            placeholder={t('common.search')}
            value={filter}
            onChange={(e) => setFilter(e.target.value)}
            aria-label={t('common.search')}
          />
        </div>
        <button type="button" className="btn-default py-1.5" onClick={() => void copy()}>
          {copied ? <Check size={14} aria-hidden /> : <Copy size={14} aria-hidden />}
          {copied ? t('common.copied') : t('common.copy')}
        </button>
      </div>

      <pre className="max-h-[60vh] overflow-auto rounded-md border border-border bg-elevated/50 p-3 font-mono text-xs leading-relaxed">
        {lines.join('\n')}
      </pre>
    </div>
  );
}
