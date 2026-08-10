import { useEffect, useMemo } from 'react';
import Editor from '@monaco-editor/react';
import { useTranslation } from 'react-i18next';

import { useUI } from '../../stores/ui';
import { configureMonaco, editorOptions } from '../../lib/monaco';

interface Props {
  value: string;
  onChange: (value: string) => void;
  /** `yaml` for a compose file, `ini` for a .env — Monaco's closest match. */
  language: 'yaml' | 'ini';
  height?: string;
  readOnly?: boolean;
  ariaLabel: string;
}

/**
 * A code editor for a compose file or a `.env`.
 *
 * Monaco is bundled rather than fetched: Iskele is a single binary serving its
 * own frontend, often on a host with no route to the internet, and an editor
 * that needs a CDN is an editor that does not work.
 *
 * There is no client-side schema validation. The server's `POST
 * /stacks/validate` runs the same checks a deploy runs — including the path
 * whitelist and the privileged-option gate, which no YAML schema knows about —
 * so a second, weaker validator here would only disagree with the one that
 * decides.
 */
export function ComposeEditor({
  value,
  onChange,
  language,
  height = '24rem',
  readOnly,
  ariaLabel,
}: Props) {
  const { t } = useTranslation();
  const theme = useUI((s) => s.theme);

  useEffect(() => configureMonaco(), []);

  const resolvedTheme = useMemo(() => {
    if (theme === 'system') {
      return window.matchMedia('(prefers-color-scheme: dark)').matches ? 'vs-dark' : 'light';
    }
    return theme === 'dark' ? 'vs-dark' : 'light';
  }, [theme]);

  return (
    <div
      className="overflow-hidden rounded border border-border"
      role="group"
      aria-label={ariaLabel}
    >
      <Editor
        height={height}
        language={language}
        theme={resolvedTheme}
        value={value}
        onChange={(next) => onChange(next ?? '')}
        options={{ ...editorOptions, readOnly }}
        loading={<div className="p-4 text-xs text-muted">{t('common.loading')}</div>}
      />
    </div>
  );
}
