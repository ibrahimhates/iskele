# Security

## The one thing to understand first

**Access to the Docker socket is equivalent to root on the host.** This is not
a limitation of Iskele; it is what the Docker Engine API is. Anyone who can
reach Iskele's API with the `create` and `privileged` permissions can start a
container that mounts `/` and reads or writes anything on the machine.

So: **treat the panel as a root shell with a web interface.** Every control in
this document narrows who gets to that shell and records what they did with it.
None of them makes the panel safe to expose to the internet without
authentication in front of it.

## Reporting a vulnerability

Report privately through GitHub's [security advisory
form](https://github.com/ibrahimhates/iskele/security/advisories/new), or by
email to the address in the repository's profile. Please do not open a public
issue for a vulnerability.

Include the version (`iskeled --version`), what an attacker gains, and enough
detail to reproduce it. You will get an acknowledgement within a week. If a
fix is warranted it goes out as a patch release with an advisory naming you,
unless you would rather not be named.

This is a small project without a paid security team. There is no bounty, and
a fix may take longer than a funded project's would.

## Threat model

### What Iskele defends against

| Threat | Control |
|---|---|
| Someone reaching the port before setup | Every route answers `409 NOT_INITIALIZED` until the first admin exists. Only `/auth/bootstrap` works, and only once. |
| Password guessing | argon2id (64 MiB, 3 passes), a per-IP lockout after repeated failures, and a login that costs the same whether or not the account exists. |
| A stolen access token | 15-minute lifetime by default; refresh tokens rotate on every use, so a stolen one stops working as soon as the real user refreshes — and the theft leaves a trace. |
| A stolen refresh token | Rotation, revocation on password reset, and revocation when the account is disabled or deleted. |
| An operator escalating to host root | Bind mount sources must be inside `allowed_paths`; `privileged`, `cap_add`, `devices`, `security_opt`, `sysctls` and `network: host` need the `privileged` permission, which only an admin has. |
| A path escaping the whitelist | Symlinks are resolved before the comparison, and paths are compared by component, so `/srv-other` does not pass a `/srv` root. An unset whitelist refuses every bind mount. |
| A stolen database | Registry passwords and TOTP secrets are encrypted with AES-256-GCM under a key held outside the database, in a file iskeled refuses to start on if it is readable by anyone else. |
| Cross-site request forgery | A CSRF guard on state-changing requests, and a WebSocket handshake rejected unless `Origin` matches `Host` — the only cross-origin defense a WebSocket has. |
| A browser that cannot send an auth header | Streaming endpoints take a single-use 60-second ticket instead, consumed on arrival whether or not the permission check passes. |
| Not knowing what happened | Every mutation is recorded with actor, action, resource, result, IP and user agent — including the ones that were refused. Nothing in the API edits or deletes a record. |
| Secrets in the audit trail | Values that look like credentials are masked before the record is written. |

### What Iskele does not defend against

- **A malicious admin.** An admin can deploy a privileged container. That is
  the product working as designed; the audit trail records it, and nothing
  prevents it.
- **A compromised Docker daemon or a malicious image.** Iskele asks the engine
  to run what it is told to run.
- **The network path.** There is no built-in rate limiting on bandwidth, no
  IP allowlist, and no WAF. Put a reverse proxy in front of it.
- **A host that other people already have root on.** The secret key is
  protected by file permissions.
- **Denial of service.** Rate limits exist to slow credential guessing, not to
  survive a flood.

## Deployment checklist

1. **Keep `listen` on `127.0.0.1`** and publish through a reverse proxy that
   terminates TLS. iskeled warns at startup when it is bound to a non-loopback
   address. Examples: [`deploy/reverse-proxy/`](deploy/reverse-proxy/).
2. **Run as the `iskele` user**, a member of the `docker` group — never as
   root. `deploy/install.sh` does this, and the shipped unit file adds
   `ProtectSystem=strict`, `NoNewPrivileges`, an empty capability set and a
   syscall filter.
3. **Keep `allowed_paths` as narrow as the stacks you actually run.** It is the
   whitelist bind mounts and build contexts cannot escape. `/` in that list
   defeats every other control in this document.
4. **Protect `secret_key_file`** (default `/etc/iskele/secret.key`, mode
   `0600`). It decrypts stored registry passwords and TOTP secrets and signs
   every token. Back it up with the database, and lose neither separately —
   the database without the key is unreadable, and the key without the database
   is useless.
5. **Turn on two-factor** for every admin account. Settings → two-factor.
6. **Give people the smallest role that works.** A viewer can read everything
   and change nothing; an operator can run containers but cannot reach
   privileged options, accounts or the audit log.
7. **Set an audit retention** you can live with, and read the log occasionally.
   The default keeps everything.

## Cryptography

| Purpose | Choice |
|---|---|
| Password hashing | argon2id, 64 MiB, 3 passes, 2 lanes, 16-byte salt, 32-byte key |
| Stored secrets | AES-256-GCM with a random 12-byte nonce per value |
| Key derivation | The master key from `secret_key_file`, split per purpose |
| Token signing | HMAC-SHA256 (JWT), key derived from the master key |
| API tokens | 32 random bytes, stored as a SHA-256 hash with a lookup prefix |
| Two-factor | TOTP, RFC 6238: HMAC-SHA1, 6 digits, 30-second step, ±1 step tolerance |

TOTP uses SHA-1 because every authenticator app assumes it. HMAC-SHA1 is not
affected by the collision attacks that retired SHA-1 for signatures, and each
code is valid for half a minute.

## Supported versions

Until v1.0.0, only the latest release gets security fixes.

| Version | Supported |
|---|:--:|
| 0.1.x | ✅ |
| < 0.1 | ❌ |
