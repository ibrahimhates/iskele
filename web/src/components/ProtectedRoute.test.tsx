import { describe, expect, it, beforeEach } from 'vitest';
import { render, screen } from '@testing-library/react';
import { MemoryRouter, Route, Routes } from 'react-router-dom';

import { ProtectedRoute } from './ProtectedRoute';
import { useAuth } from '../stores/auth';
import type { Session } from '../api/types';

const session: Session = {
  access_token: 'access-1',
  token_type: 'Bearer',
  expires_at: '2099-01-01T00:00:00Z',
  refresh_token: 'refresh-1',
  refresh_expires_at: '2099-01-01T00:00:00Z',
  user: {
    id: 'u1',
    username: 'admin',
    role: 'admin',
    totp_enabled: false,
    disabled: false,
    created_at: '2026-01-01T00:00:00Z',
    permissions: ['read', 'operate'],
  },
};

/** Renders the guard at `/secret`, with the redirect targets as siblings. */
function renderAt(initialized: boolean) {
  return render(
    <MemoryRouter initialEntries={['/secret']}>
      <Routes>
        <Route
          path="/secret"
          element={
            <ProtectedRoute initialized={initialized}>
              <p>secret</p>
            </ProtectedRoute>
          }
        />
        <Route path="/login" element={<p>login form</p>} />
        <Route path="/bootstrap" element={<p>bootstrap form</p>} />
      </Routes>
    </MemoryRouter>,
  );
}

describe('ProtectedRoute', () => {
  beforeEach(() => {
    useAuth.getState().signOut();
  });

  it('sends an anonymous visitor to the login form', () => {
    renderAt(true);
    expect(screen.getByText('login form')).toBeInTheDocument();
    expect(screen.queryByText('secret')).not.toBeInTheDocument();
  });

  it('renders the page once signed in', () => {
    useAuth.getState().signIn(session);
    renderAt(true);
    expect(screen.getByText('secret')).toBeInTheDocument();
  });

  // The bootstrap check comes first: an installation with no admin account has
  // nothing to sign in to, so offering a login form would be a dead end.
  it('sends everyone to bootstrap while the installation is unset up', () => {
    useAuth.getState().signIn(session);
    renderAt(false);
    expect(screen.getByText('bootstrap form')).toBeInTheDocument();
  });

  // A persisted user with no access token is what a half-cleared localStorage
  // looks like; treating it as signed in would leave the app calling the API
  // without a credential.
  it('treats a user without an access token as signed out', () => {
    useAuth.getState().signIn(session);
    useAuth.setState({ accessToken: null });
    renderAt(true);
    expect(screen.getByText('login form')).toBeInTheDocument();
  });
});
