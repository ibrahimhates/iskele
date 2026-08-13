import { create } from 'zustand';
import { persist } from 'zustand/middleware';

import { connectAuth } from '../api/client';
import type { Session, User, Permission } from '../api/types';

interface AuthState {
  accessToken: string | null;
  refreshToken: string | null;
  user: User | null;
  signIn: (session: Session) => void;
  signOut: () => void;
  setUser: (user: User) => void;
  can: (permission: Permission) => boolean;
}

/**
 * Auth state, persisted so a page reload does not sign the operator out.
 *
 * Only the refresh token and the user survive a reload; the access token is
 * short-lived and re-derived from the refresh token on start-up. localStorage
 * is readable by any script on the origin, which is why the CSP forbids
 * third-party scripts entirely.
 */
export const useAuth = create<AuthState>()(
  persist(
    (set, get) => ({
      accessToken: null,
      refreshToken: null,
      user: null,

      signIn: (session) =>
        set({
          accessToken: session.access_token,
          refreshToken: session.refresh_token,
          user: session.user,
        }),

      signOut: () => set({ accessToken: null, refreshToken: null, user: null }),

      setUser: (user) => set({ user }),

      can: (permission) => get().user?.permissions?.includes(permission) ?? false,
    }),
    {
      name: 'iskele.auth',
      partialize: (state) => ({
        refreshToken: state.refreshToken,
        user: state.user,
        accessToken: state.accessToken,
      }),
    },
  ),
);

// The API client cannot import the store directly without a cycle, so it is
// handed the accessors it needs.
connectAuth({
  getAccessToken: () => useAuth.getState().accessToken,
  getRefreshToken: () => useAuth.getState().refreshToken,
  onRefreshed: (session) => useAuth.getState().signIn(session),
  onSignedOut: () => useAuth.getState().signOut(),
});
