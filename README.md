# Iskele

Native Docker management panel for a single Linux host — one static binary, no
container, no runtime dependencies.

Iskele runs on the host as a systemd service (`iskeled`) and talks to the Docker
Engine API over `/var/run/docker.sock`. It manages containers, images, volumes,
networks and Compose stacks from a web UI that is embedded in the binary itself.

> **Status: under construction.** The project is being built milestone by
> milestone — see [`PROGRESS.md`](PROGRESS.md) for what works today and
> [`PLAN.md`](PLAN.md) for where it is going. There is no usable release yet.

---

## Security notice — read this first

**Access to the Docker socket is equivalent to root on the host.** Anyone who
can reach Iskele's API can start a privileged container that mounts the host
filesystem. Treat the panel as a root shell:

- Keep `listen` on `127.0.0.1` and publish it through a reverse proxy that adds
  TLS, or enable the built-in TLS listener. Iskele warns at startup when it is
  bound to a non-loopback address.
- Run it as the dedicated `iskele` system user (a member of the `docker` group),
  never as root.
- Keep `allowed_paths` as narrow as possible: it is the whitelist that bind
  mounts and build contexts cannot escape.
- `secret_key_file` (default `/etc/iskele/secret.key`) holds the master key for
  stored secrets and token signing. iskeled creates it with mode `0600` and
  **refuses to start** if it is readable by anyone else.

See [`SECURITY.md`](SECURITY.md) for the full threat model *(added in M9)*.

---

## Planned features

| Area | What it does |
|---|---|
| Containers | List, inspect, start/stop/restart/pause/kill/rename, bulk actions, redeploy |
| Live streams | Log streaming, `exec` console (xterm.js), live CPU/memory/IO stats |
| Create wizard | Every `docker run` option, with a live command + API payload preview |
| Builds | Build from a Dockerfile on the host with streamed logs, cancel, history |
| Compose | Parse and run Compose stacks natively, Monaco editor, diff before apply |
| App catalog | One-click deploy templates (redis, postgres, traefik, gitea, …) |
| Images, volumes, networks | Full CRUD, prune, private registries with encrypted credentials |
| Dashboard | Container/host metrics, live Docker events, audit log |
| Users | Roles (admin/operator/viewer), API tokens, optional TOTP 2FA |

---

## Building from source

Requirements: Go 1.25+ (and Node 20+ once the frontend lands in M3).

```sh
make build      # -> bin/iskeled
make test       # go test -race ./...
make check      # gofmt + vet + test
make help       # list every target
```

Run it locally against your own Docker socket:

```sh
make run        # binds 127.0.0.1:8377, data dir ./.data, debug logging
curl -s http://127.0.0.1:8377/api/v1/health
```

---

## Configuration

Values are resolved with the precedence **flag > environment variable > YAML
file > built-in default**. The config file defaults to `/etc/iskele/config.yaml`;
a fully commented example lives in
[`deploy/config.example.yaml`](deploy/config.example.yaml).

```yaml
listen: "127.0.0.1:8377"
docker_host: "unix:///var/run/docker.sock"
data_dir: "/var/lib/iskele"
secret_key_file: "/etc/iskele/secret.key"
allowed_paths:
  - "/opt/stacks"
  - "/srv"
log_level: "info"
log_format: "auto"
tls:
  enabled: false
  cert_file: ""
  key_file: ""
session:
  access_ttl: "15m"
  refresh_ttl: "168h"
```

Run `iskeled --help` for the full flag and environment variable list.

---

## API

All endpoints live under `/api/v1`. Errors use a single envelope:

```json
{ "error": { "code": "CONTAINER_NOT_FOUND", "message": "no such container: abc", "details": {} } }
```

Every route requires a `Bearer` credential — a JWT access token from
`/auth/login`, or an API token (`isk_<prefix>_<secret>`) — except the six
listed as *open* below.

| Method | Path | Auth | Description |
|---|---|---|---|
| `GET` | `/health` | open | Liveness probe — `{"status":"ok","uptime":"1m2s"}` |
| `GET` | `/version` | open | Build metadata of the running binary |
| `GET` | `/auth/status` | open | Has the installation been set up yet? |
| `POST` | `/auth/bootstrap` | open | Create the first admin account (works once) |
| `POST` | `/auth/login` | open | Sign in |
| `POST` | `/auth/refresh` | open | Rotate the refresh token for a new pair |
| `POST` | `/auth/logout` | any | Revoke the current session |
| `GET` | `/auth/me` | any | The caller's identity and permissions |
| `GET` | `/system/ping` | read | Is the Docker daemon reachable? Always 200 |
| `GET` | `/system/info` | read | Docker engine and host summary |
| `GET` | `/system/df` | read | Disk usage (`docker system df`) |
| `GET` | `/containers` | read | List containers — `all`, `size`, `label`, `status`, `name` |
| `GET` | `/containers/{id}` | read | Container detail |
| `GET` | `/containers/{id}/inspect` | read | The engine's raw inspect payload, verbatim |
| `POST` | `/containers/{id}/start` | operate | Start |
| `POST` | `/containers/{id}/stop` | operate | Stop — optional `timeout` |
| `POST` | `/containers/{id}/restart` | operate | Restart — optional `timeout` |
| `DELETE` | `/containers/{id}` | delete | Remove — `force`, `volumes` |
| `GET` | `/images` | read | List images — `all`, `dangling`, `label` |
| `GET` | `/volumes` | read | List volumes |
| `GET` | `/networks` | read | List networks |

Collection endpoints return `{"items": [...], "total": N}`; `items` is never
`null`. The full specification is [`docs/openapi.yaml`](docs/openapi.yaml), and
the planned surface is in [`PLAN.md`](PLAN.md#6-api-yüzeyi).

### Getting started

```sh
# 1. Is it set up yet?
curl -s localhost:8377/api/v1/auth/status
# {"initialized":false}

# 2. Create the first admin (works exactly once)
curl -s -X POST localhost:8377/api/v1/auth/bootstrap \
  -H 'Content-Type: application/json' \
  -d '{"username":"admin","password":"a-long-enough-Passphrase-1"}'

# 3. Use the access token it returns
curl -s localhost:8377/api/v1/containers -H "Authorization: Bearer $TOKEN"
```

Until that first account exists, **every** route except the auth endpoints
answers `409 NOT_INITIALIZED` — an installation that is running but not yet
configured does not expose Docker to whoever reaches the port first.

### Roles

| | viewer | operator | admin |
|---|:--:|:--:|:--:|
| Read (list, inspect, logs, stats) | ✅ | ✅ | ✅ |
| Start / stop / restart, create, remove | | ✅ | ✅ |
| Build, prune, privileged options | | | ✅ |
| Users, settings, registries, audit log | | | ✅ |

Routes require *permissions*, not roles, and an unrecognised role carries no
permissions at all — the check fails closed.

**When Docker is down**, iskeled still starts and serves: `/health` keeps
answering and every engine-backed route returns `503 DOCKER_UNAVAILABLE` with
the endpoint it tried and the most likely fix. Losing the daemon should not
also cost you the panel that would tell you so.

---

## Project documents

| File | Purpose |
|---|---|
| [`PLAN.md`](PLAN.md) | Architecture, data model, API surface, milestone plan |
| [`PROGRESS.md`](PROGRESS.md) | Live milestone and task status |
| [`DECISIONS.md`](DECISIONS.md) | Design decisions and assumptions (ADR style) |
| [`ACCEPTANCE.md`](ACCEPTANCE.md) | Acceptance criteria for v0.1.0 |

## License

[Apache-2.0](LICENSE)
