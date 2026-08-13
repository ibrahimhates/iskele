import { Monitor, Moon, Sun } from 'lucide-react';
import { useTranslation } from 'react-i18next';

import { useUI, type Theme } from '../stores/ui';
import { cn } from '../lib/cn';

const OPTIONS: { value: Theme; Icon: typeof Sun; labelKey: string }[] = [
  { value: 'light', Icon: Sun, labelKey: 'settings.themeLight' },
  { value: 'dark', Icon: Moon, labelKey: 'settings.themeDark' },
  { value: 'system', Icon: Monitor, labelKey: 'settings.themeSystem' },
];

export function ThemeToggle() {
  const { t } = useTranslation();
  const theme = useUI((s) => s.theme);
  const setTheme = useUI((s) => s.setTheme);

  return (
    <div
      className="flex rounded-md border border-border p-0.5"
      role="group"
      aria-label={t('settings.theme')}
    >
      {OPTIONS.map(({ value, Icon, labelKey }) => (
        <button
          key={value}
          type="button"
          onClick={() => setTheme(value)}
          className={cn(
            'rounded px-2 py-1 transition-colors',
            theme === value ? 'bg-elevated text-fg' : 'text-muted hover:text-fg',
          )}
          aria-pressed={theme === value}
          aria-label={t(labelKey)}
          title={t(labelKey)}
        >
          <Icon size={14} aria-hidden />
        </button>
      ))}
    </div>
  );
}
