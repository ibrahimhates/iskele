# Changelog

Notable changes to Iskele. The format follows
[Keep a Changelog](https://keepachangelog.com/en/1.1.0/), and this project uses
[Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

## [0.1.0] — 2026-08-13

First release. A single static binary that manages Docker on one Linux host,
serving its own web UI, running as a systemd service.

### Containers

- List with search, sort and multi-select; bulk start, stop, restart, pause,
  unpause, kill and remove, reporting per-container results rather than one
  verdict for the batch.
- Detail across eight tabs: overview, live logs, CPU/memory charts, an
  interactive console (xterm.js over a WebSocket), the raw inspect payload,
  environment, mounts and networks.
- Live CPU and memory for every row from one shared event stream — a stream
  per row stalls at six containers, which is the browser's per-origin limit.
- Redeploy: pull the image and recreate the container from its own definition,
  rolling back to the old one if the new one fails to start.
- A ten-tab creation wizard covering every `docker run` option, with a live
  preview of both the command and the API payload it becomes.

### Compose stacks

- Compose files are parsed with the same library the `docker compose` CLI uses
  and deployed over the engine socket, so no `docker compose` binary needs to
  be installed.
- Sources: an in-browser editor (Monaco, bundled), a file on this host, or a
  git repository.
- Deploys stream their progress in dependency order, leave an unchanged service
  running (config-hash labelling), and say which service and which field would
  be refused before anything is created.
- Diff before deploying; per-service logs interleaved over one WebSocket.
- Compose projects started with the CLI on the same host are discovered and can
  be adopted without touching their containers.
- Interpolation sees only the stack's own `.env`, never the daemon's
  environment. A variable that resolves to nothing is reported rather than
  silently emptied.

### Images, volumes, networks

- Pull with per-layer progress, layer history, inspect, tag, remove, prune.
- Volume and network CRUD, attach and detach, prune.
- Private registries with credentials encrypted at rest; the password is never
  returned by the API.

### Building

- Build an image from a Dockerfile on the host, with the context picked through
  a browser that cannot leave `allowed_paths`.
- Output streams live with the Dockerfile step it is on. Closing the tab stops
  the frames, not the build.
- History records who built what, from where, with the archived output.
- `.dockerignore` is honoured, including negation and `**/`.

### App catalog

- Twenty templates: redis, postgres, mysql, mariadb, mongodb, cloudflared,
  nginx, caddy, traefik, portainer_agent, uptime-kuma, n8n, vaultwarden, minio,
  rabbitmq, adminer, pgadmin, watchtower, gitea, wg-easy.
- Custom entries load from `/etc/iskele/templates`; one whose id matches a
  shipped template replaces it. A malformed file is reported on the catalog
  screen rather than swallowed.
- A template is a form, not a script: rendering produces an ordinary container
  definition that goes through the same path guard and privileged gate as the
  wizard.

### Dashboard

- Container, image, volume and network counts with `docker system df`.
- Host CPU, memory, swap and disk from gopsutil, plus host and daemon uptime.
  These do not come from Docker, so they keep working when Docker is down.
- A live activity feed off the engine's event stream — a container someone
  stopped over SSH appears the same as one stopped from the panel.

### Accounts and access

- Roles: viewer, operator, admin, resolved to permissions. An unrecognised role
  carries none; the check fails closed.
- User management: create, change role, reset password, disable, delete. A
  password reset or a disable ends that account's sessions.
- Optional TOTP two-factor, written against RFC 6238 and tested with its own
  vectors. Enrollment shows a QR code and the secret; turning it off requires a
  current code. An admin can clear somebody else's after a lost device.
- API tokens for headless use, stored as hashes.
- Audit log with actor, action, date and result filters, and CSV/JSON export.
  Read-only over the API: nothing edits or deletes a record.

### Security

- Bind mount sources and build contexts are confined to `allowed_paths`, with
  symlinks resolved and paths compared by component. An unset whitelist refuses
  every bind mount.
- `privileged`, `cap_add`, `devices`, `security_opt`, `sysctls` and
  `network: host` require the privileged permission, which only admins have.
- argon2id passwords, rotating refresh tokens, a per-IP login lockout, CSRF
  protection, and a WebSocket handshake refused when `Origin` does not match
  `Host`.
- Streaming endpoints authenticate with a single-use 60-second ticket, since a
  browser cannot set a header on a WebSocket or `EventSource` request.
- Registry passwords and TOTP secrets are encrypted with AES-256-GCM under a
  key iskeled refuses to start on if it is readable by anyone else.

### Operations

- systemd unit with `Type=notify`, a watchdog, `ProtectSystem=strict`, an empty
  capability set and a syscall filter.
- `install.sh` and `uninstall.sh --purge`; `.deb` and `.rpm` packages.
- Reverse proxy examples for nginx, Caddy and Traefik, each handling the
  WebSocket upgrade, unbuffered SSE and the long read timeouts that log streams
  and builds need.
- Prune tools for stopped containers, dangling images and unused volumes and
  networks, each stating the engine's actual rule before it runs.
- Configurable audit retention, swept daily.
- With Docker unreachable the daemon still starts and serves; engine routes
  answer `503 DOCKER_UNAVAILABLE` with the endpoint tried and the likely fix.

### Known limitations

- One Docker host per installation. Remote hosts work through `docker_host`,
  but there is no multi-endpoint switcher.
- Swarm is out of scope: `deploy.replicas` is read, the rest of `deploy:` is
  reported as unsupported rather than silently ignored.
- No file manager inside containers.
- `docker image save`/`load` is not exposed.
- The socket path and `allowed_paths` are changed by editing the config file
  and restarting, not from the settings page — they are startup-time security
  boundaries.

[Unreleased]: https://github.com/ibrahimhates/iskele/compare/v0.1.0...HEAD
[0.1.0]: https://github.com/ibrahimhates/iskele/releases/tag/v0.1.0
