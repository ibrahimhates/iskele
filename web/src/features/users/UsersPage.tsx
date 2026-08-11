import { useState } from 'react';
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import { useTranslation } from 'react-i18next';
import {
  KeyRound,
  ShieldCheck,
  ShieldOff,
  Trash2,
  UserPlus,
  Users as UsersIcon,
} from 'lucide-react';

import { users as usersApi } from '../../api/endpoints';
import type { Role, User } from '../../api/types';
import { PageHeader } from '../../components/PageHeader';
import { ConfirmDialog } from '../../components/ConfirmDialog';
import { EmptyState } from '../../components/EmptyState';
import { ErrorPanel } from '../../components/ErrorPanel';
import { Spinner } from '../../components/Spinner';
import { Field, Toggle } from '../create/fields';
import { formatRelative } from '../../lib/format';
import { useAuth } from '../../stores/auth';

const ROLES: Role[] = ['admin', 'operator', 'viewer'];

/**
 * Account administration.
 *
 * Everything here either grants access to this panel or takes it away, which
 * is why the whole page is behind the admin permission and why the server —
 * not this form — is what refuses a change that would leave the panel with no
 * admin left.
 */
export function UsersPage() {
  const { t } = useTranslation();
  const queryClient = useQueryClient();
  const me = useAuth((s) => s.user);

  const [adding, setAdding] = useState(false);
  const [pendingDelete, setPendingDelete] = useState<User | null>(null);
  const [pendingReset, setPendingReset] = useState<User | null>(null);
  const [actionError, setActionError] = useState<unknown>(null);

  const query = useQuery({ queryKey: ['users'], queryFn: usersApi.list });

  const refresh = () => queryClient.invalidateQueries({ queryKey: ['users'] });

  const update = useMutation({
    mutationFn: ({ id, ...input }: { id: string; role?: Role; disabled?: boolean }) =>
      usersApi.update(id, input),
    onSuccess: () => {
      setActionError(null);
      void refresh();
    },
    onError: setActionError,
  });

  const remove = useMutation({
    mutationFn: (id: string) => usersApi.remove(id),
    onSuccess: () => {
      setActionError(null);
      setPendingDelete(null);
      void refresh();
    },
    onError: (err) => {
      setActionError(err);
      setPendingDelete(null);
    },
  });

  const resetTOTP = useMutation({
    mutationFn: (id: string) => usersApi.resetTOTP(id),
    onSuccess: () => {
      setActionError(null);
      setPendingReset(null);
      void refresh();
    },
    onError: (err) => {
      setActionError(err);
      setPendingReset(null);
    },
  });

  if (query.isLoading) {
    return (
      <div className="flex justify-center py-16">
        <Spinner />
      </div>
    );
  }
  if (query.error) {
    return <ErrorPanel error={query.error} onRetry={() => void query.refetch()} />;
  }

  const items = query.data?.items ?? [];

  return (
    <>
      <PageHeader
        title={t('users.title')}
        description={t('users.count', { count: items.length })}
        actions={
          <button type="button" className="btn-primary" onClick={() => setAdding(true)}>
            <UserPlus size={14} aria-hidden />
            {t('users.add')}
          </button>
        }
      />

      {actionError != null && (
        <div className="mb-4">
          <ErrorPanel error={actionError} />
        </div>
      )}

      {items.length === 0 ? (
        <EmptyState
          icon={<UsersIcon size={32} aria-hidden />}
          title={t('users.none')}
          description={t('users.none_hint')}
        />
      ) : (
        <div className="card overflow-x-auto">
          <table className="w-full text-sm">
            <thead>
              <tr className="border-b border-border text-left text-xs text-muted">
                <th className="px-4 py-2 font-medium">{t('common.name')}</th>
                <th className="px-4 py-2 font-medium">{t('users.role')}</th>
                <th className="px-4 py-2 font-medium">{t('users.two_factor')}</th>
                <th className="px-4 py-2 font-medium">{t('users.last_login')}</th>
                <th className="px-4 py-2 font-medium">{t('users.enabled')}</th>
                <th className="px-4 py-2 font-medium text-right">{t('common.actions')}</th>
              </tr>
            </thead>
            <tbody>
              {items.map((user) => {
                const isSelf = user.id === me?.id;
                return (
                  <tr key={user.id} className="border-b border-border/50 last:border-0">
                    <td className="px-4 py-2">
                      <span className="font-medium">{user.username}</span>
                      {isSelf && (
                        <span className="ml-2 badge bg-elevated text-muted">{t('users.you')}</span>
                      )}
                    </td>

                    <td className="px-4 py-2">
                      <select
                        className="input h-8 w-32 py-0 text-xs"
                        value={user.role}
                        aria-label={t('users.role')}
                        disabled={update.isPending}
                        onChange={(e) =>
                          update.mutate({ id: user.id, role: e.target.value as Role })
                        }
                      >
                        {ROLES.map((role) => (
                          <option key={role} value={role}>
                            {t(`users.role_${role}`)}
                          </option>
                        ))}
                      </select>
                    </td>

                    <td className="px-4 py-2">
                      {user.totp_enabled ? (
                        <span className="flex items-center gap-1.5 text-xs text-ok">
                          <ShieldCheck size={14} aria-hidden />
                          {t('users.two_factor_on')}
                        </span>
                      ) : (
                        <span className="flex items-center gap-1.5 text-xs text-muted">
                          <ShieldOff size={14} aria-hidden />
                          {t('users.two_factor_off')}
                        </span>
                      )}
                    </td>

                    <td className="px-4 py-2 text-xs text-muted">
                      {user.last_login_at ? formatRelative(user.last_login_at) : t('users.never')}
                    </td>

                    <td className="px-4 py-2">
                      <Toggle
                        label=""
                        checked={!user.disabled}
                        onChange={(next) => update.mutate({ id: user.id, disabled: !next })}
                      />
                    </td>

                    <td className="px-4 py-2">
                      <div className="flex items-center justify-end gap-1">
                        <ResetPasswordButton
                          user={user}
                          onDone={() => setActionError(null)}
                          onError={setActionError}
                        />
                        {/* Clearing a factor is for a lost device; one's own
                            is turned off from settings, with a code. */}
                        {user.totp_enabled && !isSelf && (
                          <button
                            type="button"
                            className="btn-ghost"
                            title={t('users.reset_totp')}
                            aria-label={t('users.reset_totp')}
                            onClick={() => setPendingReset(user)}
                          >
                            <ShieldOff size={14} aria-hidden />
                          </button>
                        )}
                        {!isSelf && (
                          <button
                            type="button"
                            className="btn-ghost text-danger"
                            title={t('common.delete')}
                            aria-label={t('common.delete')}
                            onClick={() => setPendingDelete(user)}
                          >
                            <Trash2 size={14} aria-hidden />
                          </button>
                        )}
                      </div>
                    </td>
                  </tr>
                );
              })}
            </tbody>
          </table>
        </div>
      )}

      {adding && (
        <AddUserDialog
          onClose={() => setAdding(false)}
          onCreated={() => {
            setAdding(false);
            void refresh();
          }}
        />
      )}

      {pendingDelete && (
        <ConfirmDialog
          open
          title={t('users.delete_title')}
          description={t('users.delete_message', { username: pendingDelete.username })}
          // Deleting an account is irreversible and its sessions go with it,
          // so the operator retypes the name they mean.
          confirmText={pendingDelete.username}
          confirmLabel={t('common.delete')}
          destructive
          busy={remove.isPending}
          onCancel={() => setPendingDelete(null)}
          onConfirm={() => remove.mutate(pendingDelete.id)}
        />
      )}

      {pendingReset && (
        <ConfirmDialog
          open
          title={t('users.reset_totp')}
          description={t('users.reset_totp_message', { username: pendingReset.username })}
          confirmLabel={t('users.reset_totp')}
          destructive
          busy={resetTOTP.isPending}
          onCancel={() => setPendingReset(null)}
          onConfirm={() => resetTOTP.mutate(pendingReset.id)}
        />
      )}
    </>
  );
}

/** Sets a new password for one account, in place. */
function ResetPasswordButton({
  user,
  onDone,
  onError,
}: {
  user: User;
  onDone: () => void;
  onError: (error: unknown) => void;
}) {
  const { t } = useTranslation();
  const [open, setOpen] = useState(false);
  const [password, setPassword] = useState('');

  const reset = useMutation({
    mutationFn: () => usersApi.update(user.id, { password }),
    onSuccess: () => {
      setOpen(false);
      setPassword('');
      onDone();
    },
    onError,
  });

  if (!open) {
    return (
      <button
        type="button"
        className="btn-ghost"
        title={t('users.reset_password')}
        aria-label={t('users.reset_password')}
        onClick={() => setOpen(true)}
      >
        <KeyRound size={14} aria-hidden />
      </button>
    );
  }

  return (
    <form
      className="flex items-center gap-1"
      onSubmit={(e) => {
        e.preventDefault();
        reset.mutate();
      }}
    >
      <input
        className="input h-8 w-44 py-0 text-xs"
        type="password"
        value={password}
        autoComplete="new-password"
        placeholder={t('users.new_password')}
        aria-label={t('users.new_password')}
        onChange={(e) => setPassword(e.target.value)}
      />
      <button type="submit" className="btn-primary h-8 px-2 text-xs" disabled={reset.isPending}>
        {t('common.save')}
      </button>
      <button
        type="button"
        className="btn-ghost h-8 px-2 text-xs"
        onClick={() => {
          setOpen(false);
          setPassword('');
        }}
      >
        {t('common.cancel')}
      </button>
    </form>
  );
}

/** The new-account form. */
function AddUserDialog({ onClose, onCreated }: { onClose: () => void; onCreated: () => void }) {
  const { t } = useTranslation();
  const [username, setUsername] = useState('');
  const [password, setPassword] = useState('');
  const [role, setRole] = useState<Role>('viewer');

  const create = useMutation({
    mutationFn: () => usersApi.create({ username: username.trim(), password, role }),
    onSuccess: onCreated,
  });

  return (
    <div className="fixed inset-0 z-40 flex items-center justify-center bg-black/50 p-4">
      <div className="card w-full max-w-md p-4">
        <h2 className="mb-3 text-sm font-semibold">{t('users.add')}</h2>

        {create.error != null && (
          <div className="mb-3">
            <ErrorPanel error={create.error} />
          </div>
        )}

        <form
          className="space-y-3"
          onSubmit={(e) => {
            e.preventDefault();
            create.mutate();
          }}
        >
          <Field label={t('auth.username')} htmlFor="new-username">
            <input
              id="new-username"
              className="input"
              value={username}
              autoComplete="off"
              spellCheck={false}
              onChange={(e) => setUsername(e.target.value)}
            />
          </Field>

          <Field label={t('auth.password')} htmlFor="new-password" hint={t('users.password_hint')}>
            <input
              id="new-password"
              className="input"
              type="password"
              value={password}
              autoComplete="new-password"
              onChange={(e) => setPassword(e.target.value)}
            />
          </Field>

          <Field label={t('users.role')} htmlFor="new-role" hint={t(`users.role_${role}_hint`)}>
            <select
              id="new-role"
              className="input"
              value={role}
              onChange={(e) => setRole(e.target.value as Role)}
            >
              {ROLES.map((option) => (
                <option key={option} value={option}>
                  {t(`users.role_${option}`)}
                </option>
              ))}
            </select>
          </Field>

          <div className="flex justify-end gap-2 pt-1">
            <button type="button" className="btn-default" onClick={onClose}>
              {t('common.cancel')}
            </button>
            <button type="submit" className="btn-primary" disabled={create.isPending}>
              {create.isPending ? <Spinner className="h-4 w-4" /> : t('common.create')}
            </button>
          </div>
        </form>
      </div>
    </div>
  );
}
