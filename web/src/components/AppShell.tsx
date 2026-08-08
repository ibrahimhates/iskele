import { useEffect } from 'react';
import { NavLink, Outlet, useNavigate } from 'react-router-dom';
import { useTranslation } from 'react-i18next';
import {
  Boxes,
  Database,
  LayoutDashboard,
  LogOut,
  Menu,
  Network,
  Settings,
  HardDrive,
} from 'lucide-react';

import { auth as authApi } from '../api/endpoints';
import { useAuth } from '../stores/auth';
import { useUI } from '../stores/ui';
import { cn } from '../lib/cn';
import { ConnectionBanner } from './ConnectionBanner';
import { ThemeToggle } from './ThemeToggle';
import { useKeyboardShortcuts } from '../lib/useKeyboardShortcuts';

const NAV = [
  { to: '/dashboard', key: 'nav.dashboard', Icon: LayoutDashboard },
  { to: '/containers', key: 'nav.containers', Icon: Boxes },
  { to: '/images', key: 'nav.images', Icon: HardDrive },
  { to: '/volumes', key: 'nav.volumes', Icon: Database },
  { to: '/networks', key: 'nav.networks', Icon: Network },
  { to: '/settings', key: 'nav.settings', Icon: Settings },
] as const;

export function AppShell() {
  const { t } = useTranslation();
  const navigate = useNavigate();
  const user = useAuth((s) => s.user);
  const refreshToken = useAuth((s) => s.refreshToken);
  const signOut = useAuth((s) => s.signOut);
  const sidebarOpen = useUI((s) => s.sidebarOpen);
  const toggleSidebar = useUI((s) => s.toggleSidebar);
  const setSidebarOpen = useUI((s) => s.setSidebarOpen);

  useKeyboardShortcuts();

  // On a phone the sidebar overlays the content, so it must not start open.
  useEffect(() => {
    if (window.innerWidth < 768) setSidebarOpen(false);
  }, [setSidebarOpen]);

  async function handleSignOut() {
    if (refreshToken) {
      // Revoking server-side is best effort: the local session ends either way.
      try {
        await authApi.logout(refreshToken);
      } catch {
        /* ignored */
      }
    }
    signOut();
    navigate('/login', { replace: true });
  }

  return (
    <div className="flex h-full">
      <aside
        className={cn(
          'fixed inset-y-0 left-0 z-30 w-60 shrink-0 border-r border-border bg-surface',
          'transition-transform md:static md:translate-x-0',
          sidebarOpen ? 'translate-x-0' : '-translate-x-full',
        )}
        aria-label={t('app.name')}
      >
        <div className="flex h-14 items-center gap-2 border-b border-border px-4">
          <span className="text-lg font-semibold tracking-tight">{t('app.name')}</span>
        </div>

        <nav className="flex flex-col gap-0.5 p-2">
          {NAV.map(({ to, key, Icon }) => (
            <NavLink
              key={to}
              to={to}
              className={({ isActive }) =>
                cn(
                  'flex items-center gap-2.5 rounded-md px-3 py-2 text-sm transition-colors',
                  isActive
                    ? 'bg-accent/10 font-medium text-accent'
                    : 'text-muted hover:bg-elevated hover:text-fg',
                )
              }
              onClick={() => {
                if (window.innerWidth < 768) setSidebarOpen(false);
              }}
            >
              <Icon size={16} aria-hidden />
              {t(key)}
            </NavLink>
          ))}
        </nav>
      </aside>

      {sidebarOpen && (
        <button
          type="button"
          className="fixed inset-0 z-20 bg-black/50 md:hidden"
          onClick={() => setSidebarOpen(false)}
          aria-label={t('common.close')}
        />
      )}

      <div className="flex min-w-0 flex-1 flex-col">
        <header className="flex h-14 items-center gap-3 border-b border-border bg-surface px-4">
          <button
            type="button"
            className="btn-ghost md:hidden"
            onClick={toggleSidebar}
            aria-label={t('app.name')}
          >
            <Menu size={18} aria-hidden />
          </button>

          <div className="flex-1" />

          <ThemeToggle />

          <div className="flex items-center gap-2 border-l border-border pl-3">
            <div className="text-right leading-tight">
              <div className="text-sm font-medium">{user?.username}</div>
              <div className="text-xs text-muted">{user?.role}</div>
            </div>
            <button
              type="button"
              className="btn-ghost"
              onClick={() => void handleSignOut()}
              aria-label={t('auth.signOut')}
              title={t('auth.signOut')}
            >
              <LogOut size={16} aria-hidden />
            </button>
          </div>
        </header>

        <ConnectionBanner />

        <main className="min-w-0 flex-1 overflow-auto p-4 md:p-6">
          <Outlet />
        </main>
      </div>
    </div>
  );
}
