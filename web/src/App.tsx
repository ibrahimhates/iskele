import { lazy, Suspense, useEffect } from 'react';
import { Navigate, Route, Routes } from 'react-router-dom';
import { useQuery } from '@tanstack/react-query';

import { auth as authApi, system } from './api/endpoints';
import { ApiError } from './api/client';
import { AppShell } from './components/AppShell';
import { ProtectedRoute } from './components/ProtectedRoute';
import { BootstrapPage } from './features/auth/BootstrapPage';
import { LoginPage } from './features/auth/LoginPage';
import { DashboardPage } from './features/dashboard/DashboardPage';
import { ContainerListPage } from './features/containers/ContainerListPage';
import { ContainerDetailPage } from './features/containers/ContainerDetailPage';
import { CreateContainerPage } from './features/create/CreateContainerPage';
import { BuildPage } from './features/build/BuildPage';
import { StacksPage } from './features/stacks/StacksPage';
import { CatalogPage } from './features/catalog/CatalogPage';
import { UsersPage } from './features/users/UsersPage';
import { TemplateDeployPage } from './features/catalog/TemplateDeployPage';
import { StackDetailPage } from './features/stacks/StackDetailPage';
// The compose editor carries Monaco, which is most of a megabyte gzipped.
// Loading it lazily keeps that out of the first paint for the many sessions
// that never open it.
const StackEditorPage = lazy(() =>
  import('./features/stacks/StackEditorPage').then((m) => ({ default: m.StackEditorPage })),
);
import { ImagesPage } from './features/resources/ImagesPage';
import { VolumesPage } from './features/resources/VolumesPage';
import { NetworksPage } from './features/resources/NetworksPage';
import { SettingsPage } from './features/settings/SettingsPage';
import { useAuth } from './stores/auth';
import { useConnection } from './stores/connection';

export default function App() {
  const user = useAuth((s) => s.user);
  const setUser = useAuth((s) => s.setUser);
  const setDocker = useConnection((s) => s.setDocker);
  const setApi = useConnection((s) => s.setApi);

  // Is this installation set up yet? Everything else depends on the answer.
  const status = useQuery({
    queryKey: ['auth', 'status'],
    queryFn: authApi.status,
    staleTime: Infinity,
    retry: 1,
  });

  // Refresh the cached profile once on load: a role change made while the
  // operator was away must not leave stale permissions in the UI.
  const profile = useQuery({
    queryKey: ['auth', 'me'],
    queryFn: authApi.me,
    enabled: Boolean(user),
    retry: false,
  });

  useEffect(() => {
    if (profile.data) setUser(profile.data);
  }, [profile.data, setUser]);

  // Poll the engine so the banner reflects Docker going away, not just the
  // API. Cheap enough at this interval to be worth the certainty.
  const engine = useQuery({
    queryKey: ['system', 'ping'],
    queryFn: system.ping,
    enabled: Boolean(user),
    refetchInterval: 15_000,
    retry: false,
  });

  useEffect(() => {
    if (engine.data) {
      setDocker(engine.data.reachable, engine.data.error ?? null);
      setApi('connected');
    } else if (engine.error) {
      setApi(engine.error instanceof ApiError ? 'connected' : 'reconnecting');
    }
  }, [engine.data, engine.error, setDocker, setApi]);

  if (status.isLoading) {
    return <FullPageSpinner />;
  }

  const initialized = status.data?.initialized ?? true;

  return (
    <Routes>
      <Route
        path="/bootstrap"
        element={initialized ? <Navigate to="/login" replace /> : <BootstrapPage />}
      />
      <Route
        path="/login"
        element={!initialized ? <Navigate to="/bootstrap" replace /> : <LoginPage />}
      />

      <Route
        element={
          <ProtectedRoute initialized={initialized}>
            <AppShell />
          </ProtectedRoute>
        }
      >
        <Route path="/" element={<Navigate to="/dashboard" replace />} />
        <Route path="/dashboard" element={<DashboardPage />} />
        <Route path="/containers" element={<ContainerListPage />} />
        <Route path="/containers/new" element={<CreateContainerPage />} />
        <Route path="/containers/:id" element={<ContainerDetailPage />} />
        <Route path="/containers/:id/:tab" element={<ContainerDetailPage />} />
        <Route path="/catalog" element={<CatalogPage />} />
        <Route path="/catalog/:id" element={<TemplateDeployPage />} />
        <Route path="/stacks" element={<StacksPage />} />
        <Route
          path="/stacks/new"
          element={
            <LazyPage>
              <StackEditorPage />
            </LazyPage>
          }
        />
        <Route path="/stacks/:id" element={<StackDetailPage />} />
        <Route
          path="/stacks/:id/edit"
          element={
            <LazyPage>
              <StackEditorPage />
            </LazyPage>
          }
        />
        <Route path="/images" element={<ImagesPage />} />
        <Route path="/build" element={<BuildPage />} />
        <Route path="/volumes" element={<VolumesPage />} />
        <Route path="/networks" element={<NetworksPage />} />
        <Route path="/users" element={<UsersPage />} />
        <Route path="/settings" element={<SettingsPage />} />
      </Route>

      <Route path="*" element={<Navigate to="/dashboard" replace />} />
    </Routes>
  );
}

/** Wraps a route whose code is fetched on demand. */
function LazyPage({ children }: { children: React.ReactNode }) {
  return <Suspense fallback={<FullPageSpinner />}>{children}</Suspense>;
}

function FullPageSpinner() {
  return (
    <div className="flex h-full items-center justify-center">
      <div
        className="h-8 w-8 animate-spin rounded-full border-2 border-border border-t-accent"
        role="status"
        aria-label="Loading"
      />
    </div>
  );
}
