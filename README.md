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
| Create wizard | Every `docker run` option, with a live command + API payload preview |
| Live streams | Log streaming, `exec` console (xterm.js), live CPU/memory/IO stats |
| App catalog | One-click deploy templates (redis, postgres, traefik, gitea, …) |
| Images, volumes, networks | Full CRUD, prune, private registries with encrypted credentials |
| Dashboard | Container/host metrics, live Docker events, audit log |
| Users | Roles (admin/operator/viewer), API tokens, optional TOTP 2FA |

---

## Building from source

Requirements: Go 1.25+ and Node 20+.

```sh
make build      # frontend + binary -> bin/iskeled
make build-go   # binary only, no Node needed
make test       # go test -race
make web-test   # frontend test suite
make check      # gofmt + vet + test
make gen-api    # regenerate the TypeScript wire types from docs/openapi.yaml
make help       # list every target
```

`make build` compiles the frontend into `web/dist` and embeds it, so the
binary serves the UI with no external files. `make build-go` skips the
frontend entirely — useful when working on the backend on a machine without
Node. A binary built that way answers `/api/v1` normally and serves a short
page in place of the UI saying how to build the full one.

Working on the frontend with live reload:

```sh
make run        # terminal 1: the daemon on 127.0.0.1:8377
make web-dev    # terminal 2: Vite on :5173, proxying /api to the daemon
```

Run it locally against your own Docker socket:

```sh
make run        # binds 127.0.0.1:8377, data dir ./.data, debug logging
curl -s http://127.0.0.1:8377/api/v1/health
```

---

## Web UI

The panel is served from the binary at `/`, on the same port as the API. What
exists today:

- **Dashboard** — container counts, engine and host summary, disk usage.
- **Containers** — search, sort, multi-select with bulk actions, and live CPU
  and memory per row (one shared event stream, not one per container).
- **Container detail** — eight tabs: overview, live logs, charts, an
  interactive console (xterm.js), the raw inspect payload, environment, mounts
  and networks.
- **Create a container** — a ten-tab wizard covering every `docker run` option,
  with a live preview of the command and the API payload it becomes. Bind
  mounts outside `allowed_paths` and options needing the privileged permission
  are flagged before you submit, not after the server refuses.
- **Images** — pull with a per-layer progress bar, layer history, inspect, tag,
  remove, prune.
- **Stacks** — Compose files, parsed with the same library the `docker
  compose` CLI uses and then deployed over the engine socket, so no `docker
  compose` binary has to be installed. Write one in the editor, read one from
  this host, or clone a repository. Deploying streams its progress, leaves an
  unchanged service running, and says which service and field it would refuse
  before it touches anything. A compose project started with the CLI on the
  same host shows up in the list and can be adopted. What each compose field
  does is in [`docs/compose-support.md`](docs/compose-support.md).
- **Build** — build an image from a Dockerfile on the host. The context is
  picked with a browser that cannot leave `allowed_paths`, output streams live
  with the Dockerfile step it is on, and the build survives the tab: closing it
  stops the frames, not the work. History keeps who built what, from where, and
  the archived output to read back.
- **Volumes and networks** — create, remove, prune; attach and detach
  containers.
- **Tasks** — a drawer showing what is still running, with cancel. A pull keeps
  going when you navigate away; it belongs to the daemon, not the page.
- **Settings** — profile, theme, language, and private registry credentials
  (admin only).

Turkish and English, light and dark, both remembered across reloads.
Destructive actions ask you to type the container's name.

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
| `POST` | `/containers/{id}/pause` | operate | Freeze every process (cgroup freezer) |
| `POST` | `/containers/{id}/unpause` | operate | Resume a paused container |
| `POST` | `/containers/{id}/kill` | operate | Signal — `signal`, default `SIGKILL` |
| `POST` | `/containers/{id}/rename` | operate | Rename — `{"name": "..."}` |
| `POST` | `/containers/{id}/redeploy` | operate | Pull the image and recreate, rolling back on failure |
| `POST` | `/containers/batch` | operate | One action over many containers; `207` on partial failure |
| `POST` | `/containers` | create | Create a container from a full definition |
| `GET` | `/system/allowed-paths` | read | Host paths bind mounts may use |
| `DELETE` | `/containers/{id}` | delete | Remove — `force`, `volumes` |
| `POST` | `/auth/ws-ticket` | any | A single-use 60s ticket for the streaming endpoints |
| `GET` | `/containers/{id}/logs` | ticket | **WebSocket** — live logs (`tail`, `follow`, `timestamps`) |
| `GET` | `/containers/{id}/exec` | ticket | **WebSocket** — interactive shell (binary stdin, text resize) |
| `GET` | `/containers/{id}/stats` | ticket | **SSE** — one container's CPU/memory/IO, once a second |
| `GET` | `/containers/stats` | ticket | **SSE** — every running container over one connection |
| `GET` | `/system/events` | ticket | **SSE** — the Docker engine event stream |
| `GET` | `/images` | read | List images — `all`, `dangling`, `label` |
| `GET` | `/images/pull` | ticket | **SSE** — pull an image with per-layer progress |
| `POST` | `/images/prune` | prune | Remove untagged images (`all=true` for every unused one) |
| `POST` | `/images/{id}/tag` | operate | Add a reference to an image |
| `GET` | `/images/{id}/history` | read | The image's layers |
| `GET` | `/images/{id}/inspect` | read | Raw engine payload |
| `DELETE` | `/images/{id}` | delete | Remove — `force`, `noprune` |
| `GET` | `/fs/browse` | build | List a whitelisted host directory; no `path` returns the roots |
| `GET` | `/build` | ticket | **WebSocket** — build an image, with live output and step progress |
| `GET` | `/builds` | read | Build history — `status`, `limit` |
| `GET` | `/builds/{id}` | read | One build record |
| `GET` | `/builds/{id}/log` | read | The build's output verbatim, as `text/plain` |
| `POST` | `/builds/{id}/cancel` | build | Stop a running build |
| `GET` | `/stacks` | read | Compose stacks; `env` is withheld from listings |
| `POST` | `/stacks` | create | Record a stack — editor, file or git |
| `GET` | `/stacks/{id}` | read | One stack, with what the engine reports for it |
| `PUT` | `/stacks/{id}` | create | Replace its content; the name is fixed |
| `DELETE` | `/stacks/{id}` | delete | Forget the record; containers are left alone |
| `POST` | `/stacks/validate` | read | Check a compose file without saving it |
| `POST` | `/stacks/{id}/diff` | read | What saving and deploying would change |
| `GET` | `/stacks/{id}/up` | ticket | **SSE** — deploy, with live progress |
| `GET` | `/stacks/{id}/pull` | ticket | **SSE** — re-pull every image |
| `GET` | `/stacks/{id}/scale` | ticket | **SSE** — change one service's replica count |
| `GET` | `/stacks/{id}/logs` | ticket | **WebSocket** — every service, interleaved |
| `POST` | `/stacks/{id}/down` | delete | Stop and remove its containers |
| `POST` | `/stacks/{id}/{stop,start,restart}` | operate | In dependency order |
| `GET` | `/stacks/discovered` | read | Compose projects running here that are not stacks |
| `POST` | `/stacks/import` | create | Adopt one, without touching its containers |
| `GET` | `/volumes` | read | List volumes |
| `POST` | `/volumes` | create | Create a volume — driver and driver options |
| `GET` | `/volumes/{name}` | read | One volume, with usage when the engine has it |
| `POST` | `/volumes/prune` | prune | Remove every volume no container references |
| `DELETE` | `/volumes/{name}` | delete | Remove — `force` |
| `GET` | `/networks` | read | List networks |
| `POST` | `/networks` | create | Create — driver, subnet, gateway, internal |
| `GET` | `/networks/{id}` | read | One network, with its attached containers counted |
| `POST` | `/networks/{id}/connect` | operate | Attach a container, with aliases and a static IP |
| `POST` | `/networks/{id}/disconnect` | operate | Detach a container |
| `POST` | `/networks/prune` | prune | Remove user-defined networks with nothing attached |
| `DELETE` | `/networks/{id}` | delete | Remove |
| `GET` | `/registries` | admin | Private registry credentials (never the password) |
| `POST` | `/registries` | admin | Add one — the password is encrypted before storage |
| `PUT` | `/registries/{id}` | admin | Update — a blank password keeps the stored one |
| `DELETE` | `/registries/{id}` | admin | Remove |
| `GET` | `/tasks` | read | Long-running operations |
| `POST` | `/tasks/{id}/cancel` | operate | Stop one |

Collection endpoints return `{"items": [...], "total": N}`; `items` is never
`null`.

The streaming endpoints are the one exception to the `Bearer` rule. A browser
cannot set a header on a WebSocket handshake or an `EventSource` request, so
they take a ticket from `POST /auth/ws-ticket` as the `ticket` query parameter:
single-use, 60 seconds, consumed on arrival whether or not the permission check
that follows passes. They enforce the same permissions as everything else, and
the WebSocket handshake is rejected when `Origin` does not match `Host`. The full specification is [`docs/openapi.yaml`](docs/openapi.yaml), and
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

**Creating a container** is the one place where two policies stand between an
operator and the host, and both answer `403`:

- every **bind mount** source must be inside `allowed_paths`
  (`PATH_NOT_ALLOWED`). Named volumes and tmpfs mounts touch no host path, so
  the whitelist does not apply to them. The check resolves symlinks first,
  because a link inside an allowed root can point anywhere, and compares path
  components, so `/srv-other` does not pass a `/srv` root.
- `privileged`, `cap_add`, `devices`, `security_opt`, `sysctls` and
  `network: host` need the **privileged** permission. Each is, in some
  configuration, a route from container to host root. Dropping capabilities is
  not gated — it narrows the container.

With no `allowed_paths` configured, every bind mount is refused. A
misconfiguration fails closed.

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
