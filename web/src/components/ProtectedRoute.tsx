import type { ReactNode } from 'react';
import { Navigate, useLocation } from 'react-router-dom';

import { useAuth } from '../stores/auth';

interface Props {
  children: ReactNode;
  initialized: boolean;
}

/**
 * Gates the authenticated part of the app.
 *
 * An unauthenticated visitor is sent to the login form with their intended
 * destination remembered, so signing in lands them where they were going
 * instead of on the dashboard.
 */
export function ProtectedRoute({ children, initialized }: Props) {
  const user = useAuth((s) => s.user);
  const accessToken = useAuth((s) => s.accessToken);
  const location = useLocation();

  if (!initialized) {
    return <Navigate to="/bootstrap" replace />;
  }
  if (!user || !accessToken) {
    return <Navigate to="/login" replace state={{ from: location.pathname }} />;
  }
  return <>{children}</>;
}
