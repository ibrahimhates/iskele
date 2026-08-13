# Security model

[`SECURITY.md`](../SECURITY.md) is the summary and the reporting process. This
is the detail: where the trust boundaries are, what enforces each one, and what
happens when one fails.

## The premise

The Docker Engine API is a root shell. `docker run -v /:/host --privileged`
reads and writes the whole machine, and no amount of care inside Iskele changes
that. So the question is never "can this API be made safe" — it is **who gets
to reach it, and what is written down about what they did.**

Everything below follows from that.

## Boundaries

### 1. Unconfigured → configured

Before the first admin exists, every route but the auth endpoints answers
`409 NOT_INITIALIZED`. `POST /auth/bootstrap` creates the first admin and then
refuses forever.

*Why:* an installation that is up but unconfigured must not hand Docker to
whoever reaches the port first. The window between `systemctl start` and the
operator opening a browser is real, and on a public address it is the whole
attack.

*Fails closed:* the check is a middleware on the protected subtree, not a flag
each handler consults.

### 2. Anonymous → authenticated

A `Bearer` credential: a JWT access token, or an API token
(`isk_<prefix>_<secret>`).

- Passwords: argon2id, 64 MiB, 3 passes, 2 lanes. Roughly 50–100 ms per
  attempt on a modest board — affordable per login, expensive per guess.
- A login for a missing account verifies against a dummy hash of the same
  shape, so it costs the same as a real one. Timing does not enumerate
  accounts.
- A per-IP lockout after repeated failures, counting *every* failure — wrong
  password, disabled account, wrong second factor.
- API tokens are stored as SHA-256 hashes with a lookup prefix. The database
  does not contain a usable token.

*Fails closed:* an unparseable or expired credential is anonymous, and
anonymous reaches nothing but the open endpoints.

### 3. One factor → two

Optional TOTP per account, RFC 6238. Secrets are AES-256-GCM encrypted at rest.

- Enrollment stores the secret **disabled**, so an abandoned setup leaves the
  account exactly as it was.
- Turning it off requires a current code: an unattended browser must not be
  enough to remove the factor that protects against unattended browsers.
- An admin can clear somebody else's after a lost device — which ends that
  account's sessions, since they were opened with the factor being removed.
- With no secret key configured, an account with two-factor enabled **cannot
  sign in at all**. Losing the key must not become a way past the factor.

`TOTP_REQUIRED` is deliberately distinguishable from a wrong password: the form
has to know to ask, and it tells an attacker who already guessed the password
nothing new. A *wrong* code is indistinguishable from a wrong password and
counts against the lockout.

### 4. Authenticated → permitted

Roles resolve to permissions; routes require permissions.

| Permission | viewer | operator | admin |
|---|:--:|:--:|:--:|
| `read` | ✅ | ✅ | ✅ |
| `operate` | | ✅ | ✅ |
| `create` | | ✅ | ✅ |
| `delete` | | ✅ | ✅ |
| `build` | | | ✅ |
| `prune` | | | ✅ |
| `privileged` | | | ✅ |
| `admin` | | | ✅ |

*Fails closed:* an unrecognised role carries no permissions. A role added to
the database by hand, or one from an older version, reaches nothing.

`build` is admin-only because a build reads host directories. `prune` is
admin-only because it deletes things nobody named individually.

### 5. Container → host: the path whitelist

Every host path a container could touch is checked against `allowed_paths` by
one guard, used in five places: the creation wizard, compose bind sources, the
directory browser, build contexts and template mounts.

- **Symlinks resolved first.** A link inside an allowed root can point
  anywhere; comparing the unresolved path would be no check at all.
- **Compared by component.** `/srv-other` does not pass a `/srv` root — a
  prefix comparison would let it.
- **Empty list refuses everything.** A misconfiguration denies rather than
  permits.

One guard rather than five is the point: a second implementation is a second
place to get it wrong, and the one that is wrong is the one an attacker uses.

Named volumes and tmpfs mounts touch no host path and are not subject to it.

### 6. Container → host: privileged options

`privileged`, `cap_add`, `devices`, `security_opt`, `sysctls` and
`network: host` require the `privileged` permission. Each is, in some
configuration, a route from container to host root.

Dropping capabilities is *not* gated — it narrows the container.

This gate applies wherever a container is defined: the wizard, a compose file,
or a catalog template. A template is a form, not a script; what it renders goes
through the same checks as anything an operator typed.

### 7. Database → plaintext

Registry passwords and TOTP secrets are AES-256-GCM encrypted under a key in
`secret_key_file`, outside the database. iskeled refuses to start if that file
is readable by anyone else.

A stolen database gives up neither. A stolen database *and* key gives up both —
they are a pair, and backing them up to the same place undoes the separation.

### 8. Browser → API: cross-origin

- A CSRF guard on state-changing requests.
- The WebSocket handshake is rejected unless `Origin` matches `Host`. That is
  the only cross-origin defense a WebSocket has; the same-origin policy does
  not cover it the way it covers `fetch`.
- Streaming endpoints take a single-use 60-second ticket instead of a header,
  because a browser cannot set one on a WebSocket or `EventSource`. The ticket
  is consumed on arrival whether or not the permission check that follows
  passes, so a rejected ticket cannot be retried against another endpoint.

### 9. Action → record

Every mutation is recorded: actor, action, resource, result, IP, user agent,
and a detail object. **Including the ones that were refused** — "who tried to
remove this and could not" is exactly what the log is asked later.

Nothing in the API edits or deletes a record. Retention removes them by age
and nothing else. An audit log an admin can rewrite is not an audit log.

Values that look like credentials are masked before the record is written.

## Deliberate non-goals

**A malicious admin.** An admin can deploy a privileged container. That is the
product working; the trail records it.

**A hardened multi-tenant panel.** One host, one team, roles to keep an
operator from doing an admin's job by accident. Not a boundary between mutually
distrusting parties.

**Surviving a flood.** Rate limits slow credential guessing. They are not DoS
protection.

**Protecting against local root.** The secret key is protected by file
permissions. Someone who is already root on the host has it.

## When something fails

**A wrong `allowed_paths`** (say `/`) removes boundary 5 entirely. Any operator
can then mount the host filesystem. This is the single highest-value line in
the config.

**A leaked secret key** exposes stored registry passwords and lets tokens be
forged. Rotating it means re-entering every registry credential and
re-enrolling every second factor.

**A leaked access token** is usable until it expires — 15 minutes by default;
they are not revocable individually. Refresh tokens *are* revoked, on password
reset, disable and delete.

**Docker unreachable** is not a security event. The daemon keeps serving and
engine routes answer `503 DOCKER_UNAVAILABLE`. Losing Docker should not also
cost you the panel that would tell you so.

## What is verified by tests

The whole RBAC matrix, route by route. The path guard, including the symlink
and prefix cases. The privileged gate, including through compose and templates.
Bootstrap running exactly once. Login timing not distinguishing a missing
account. Session revocation on password reset and disable. TOTP against RFC
6238's own vectors. The audit trail having no write endpoints. That an
unrecognised role reaches nothing.

These run on every push, with no Docker daemon and no network.
