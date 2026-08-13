# Iskele

[![Release](https://img.shields.io/github/v/release/ibrahimhates/iskele?logo=github&color=success)](https://github.com/ibrahimhates/iskele/releases/latest)
[![CI](https://github.com/ibrahimhates/iskele/actions/workflows/ci.yml/badge.svg)](https://github.com/ibrahimhates/iskele/actions/workflows/ci.yml)
[![Go](https://img.shields.io/badge/go-1.25-00ADD8?logo=go&logoColor=white)](go.mod)
[![License](https://img.shields.io/badge/license-Apache--2.0-blue)](LICENSE)
[![Stars](https://img.shields.io/github/stars/ibrahimhates/iskele?logo=github)](https://github.com/ibrahimhates/iskele/stargazers)

Native Docker management panel for a single Linux host — one static binary, no
container, no runtime dependencies.

Iskele runs on the host as a systemd service (`iskeled`) and talks to the Docker
Engine API over `/var/run/docker.sock`. It manages containers, images, volumes,
networks and Compose stacks from a web UI that is embedded in the binary itself.

> **Status: v0.1.2.** Every planned feature is implemented and tested. What has
> not happened yet is long-running use on many different hosts — see
> [Known limitations](CHANGELOG.md#known-limitations) and
> [`PROGRESS.md`](PROGRESS.md).

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

See [`SECURITY.md`](SECURITY.md) for the full threat model, including what
Iskele deliberately does **not** defend against.

---

## Requirements

- **Linux with systemd.** The installer refuses to run anywhere else rather
  than leaving a half-configured host behind.
- **Docker Engine, installed first.** Iskele manages a Docker daemon; it does
  not install one. Order matters: `install.sh` puts the `iskele` user in the
  `docker` group, and it can only do that if the group already exists.
- **amd64, arm64 or armv7.** armv6 and below are not built — the floor is a
  Raspberry Pi 2.

If Docker is missing:

```sh
curl -fsSL https://get.docker.com | sudo sh
```

If you installed Iskele first and Docker afterwards, the panel will run but
every engine route answers `503 DOCKER_UNAVAILABLE`. Repair it with:

```sh
sudo usermod -aG docker iskele
sudo systemctl restart iskeled
```

---

## Install

### 1. Pick your architecture

`uname -m` prints the kernel's name for it, which is not always the name in the
file:

| `uname -m` | Download |
|---|---|
| `x86_64` | `amd64` — same architecture, two names |
| `aarch64` | `arm64` |
| `armv7l` | `armv7` |

### 2. Download and verify

```sh
VER=0.1.2
ARCH=amd64        # from the table above

base=https://github.com/ibrahimhates/iskele/releases/download/v$VER
curl -fsSLO $base/iskele_${VER}_linux_${ARCH}.tar.gz
curl -fsSLO $base/iskele_${VER}_checksums.txt

sha256sum -c --ignore-missing iskele_${VER}_checksums.txt
```

This panel is a root shell on the host it runs on. Checking the digest costs
one command; skipping it means trusting the network you downloaded over.

### 3. Install

```sh
tar xzf iskele_${VER}_linux_${ARCH}.tar.gz
sudo ./deploy/install.sh
```

The installer creates the `iskele` system user, adds it to the `docker` group,
lays out `/etc/iskele` and `/var/lib/iskele`, installs the systemd unit and
starts it. Running it again upgrades the binary and leaves your config,
database and secret key alone.

There are `.deb` and `.rpm` packages too. They install everything but do not
start the service — a package manager starting a root-equivalent panel because
somebody typed `apt install` is making a decision that is not its to make:

```sh
sudo dpkg -i iskele_0.1.2_linux_amd64.deb      # or: rpm -i …_amd64.rpm
sudo systemctl enable --now iskeled
```

### 4. Check that it came up

```sh
systemctl status iskeled
journalctl -u iskeled -f
```

---

## Reaching the panel

Iskele binds `127.0.0.1:8377`. That is deliberate — until you decide how the
panel should be published, it is reachable only from the host itself. Three
ways to change that, in the order we would pick them.

### An SSH tunnel — nothing to install, nothing to expose

```sh
ssh -L 8377:127.0.0.1:8377 you@your-host
```

Then open `http://127.0.0.1:8377` on your own machine. `install.sh` prints this
line with your host's address already filled in. It is the right answer for
occasional administration, and it stays right forever if you never need more.

Create the first admin account here. Until you do, every route answers
`409 NOT_INITIALIZED`.

### A TLS proxy in front — the answer for anything permanent

Keep `listen` on loopback and terminate TLS in whatever the host already runs.
Iskele ships no proxy configuration on purpose: the machine that needs one is
already running nginx, Caddy or Traefik, and its operator knows that proxy
better than a file we could ship. Two requirements it does have — pass through
**WebSocket upgrades** and **unbuffered SSE**, or logs, stats and the terminal
will hang.

### Binding to the LAN — convenient, and a real decision

Editing `listen` in `/etc/iskele/config.yaml` puts the panel on the network:

```yaml
listen: "192.168.1.50:8377"     # the host's own address, not 0.0.0.0
```

```sh
sudo systemctl restart iskeled
ss -tlnp | grep 8377            # confirm what it is bound to

sudo ufw allow from 192.168.1.0/24 to any port 8377 proto tcp
# or: sudo firewall-cmd --add-port=8377/tcp --permanent && sudo firewall-cmd --reload
```

Naming the interface rather than `0.0.0.0` keeps the panel off any other
network the host is on — a public address, a VPN, a tunnel. It also means a
DHCP lease change stops the service from starting, so give the host a static
address first.

Understand what you are accepting: over plain HTTP the password and session
token cross the network in the clear, and whoever reads them gets a root shell
on this host. On a home LAN that may be a fair trade. On a shared office or
campus network it is not. The built-in TLS listener closes that gap without a
proxy — a self-signed certificate is enough, and the browser warning is a
one-time click:

```yaml
tls:
  enabled: true
  cert_file: "/etc/iskele/cert.pem"
  key_file: "/etc/iskele/key.pem"
```

iskeled logs a warning at startup on every non-loopback bind — with TLS or
without, recording which it was. That warning is not noise.

---

## Uninstall

```sh
sudo ./deploy/uninstall.sh           # keeps the database and the secret key
sudo ./deploy/uninstall.sh --purge   # deletes them
```

---

## Features

| Area | What it does |
|---|---|
| Containers | List, inspect, start/stop/restart/pause/kill/rename, bulk actions, redeploy |
| Create wizard | Every `docker run` option, with a live command + API payload preview |
| Live streams | Log streaming, `exec` console (xterm.js), live CPU/memory/IO stats |
| App catalog | One-click deploy templates (redis, postgres, traefik, gitea, …) |
| Images, volumes, networks | Full CRUD, prune, private registries with encrypted credentials |
| Dashboard | Container/host metrics, live Docker events, audit log |
| Users | Roles (admin/operator/viewer), API tokens, optional TOTP 2FA |
| Compose stacks | Editor, host file or git; dependency-ordered deploys, diff, discovery of CLI-started projects |
| Audit | Every mutation recorded, filtered and exported as CSV or JSON |

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

### Screenshots

<!-- Replace these with real captures before announcing the release. Each
     should be a 1440px-wide PNG in docs/images/, taken in dark mode against
     a host with a few containers running. -->

| | |
|---|---|
| _Dashboard_ — counts, host metrics, live activity | _Containers_ — bulk actions and per-row live stats |
| _Container detail_ — logs, charts, console | _Create_ — the ten-tab wizard with its live preview |
| _Stacks_ — the compose editor and a deploy in progress | _Audit_ — filters and export |

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
| `GET` | `/system/host` | read | Host CPU/RAM/disk, daemon uptime. Always 200, even with Docker down |
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
| `POST` | `/containers/prune` | prune | Remove every stopped container |
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
| `GET` | `/templates` | read | The app catalog, with categories and any malformed custom entries |
| `GET` | `/templates/{id}` | read | One template's questions |
| `POST` | `/templates/{id}/deploy` | create | Deploy it; every bad answer is returned at once |
| `POST` | `/templates/secret` | create | Generate a password server-side |
| `GET` | `/users` | admin | Accounts |
| `POST` | `/users` | admin | Create one |
| `PUT` | `/users/{id}` | admin | Change role, password or disabled state |
| `DELETE` | `/users/{id}` | admin | Delete; ends its sessions |
| `DELETE` | `/users/{id}/totp` | admin | Clear somebody else's second factor |
| `POST` | `/auth/totp/{setup,verify,disable}` | any | Two-factor, for the caller's own account |
| `GET` | `/audit` | admin | The audit trail, filtered and paged |
| `GET` | `/audit/facets` | admin | The distinct actors, actions and resource types on record |
| `GET` | `/audit/export` | admin | The same, as a CSV or JSON download |
| `GET` | `/settings` | admin | Runtime settings and this installation's fixed facts |
| `PUT` | `/settings` | admin | Change retention or the bind-mount warning |

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
| [`CHANGELOG.md`](CHANGELOG.md) | What shipped, and the known limitations |
| [`SECURITY.md`](SECURITY.md) | Threat model and how to report a vulnerability |
| [`CONTRIBUTING.md`](CONTRIBUTING.md) | Development setup and house style |

### Reference

| File | Purpose |
|---|---|
| [`docs/openapi.yaml`](docs/openapi.yaml) | The API, in full |
| [`docs/architecture.md`](docs/architecture.md) | How the pieces fit and why |
| [`docs/configuration.md`](docs/configuration.md) | Every setting, flag and environment variable |
| [`docs/security-model.md`](docs/security-model.md) | The trust boundaries, in detail |
| [`docs/development.md`](docs/development.md) | Working on Iskele, and cutting a release |
| [`docs/compose-support.md`](docs/compose-support.md) | Which compose fields are supported |
| [`docs/template-schema.md`](docs/template-schema.md) | Writing a catalog template |

---

## Star the project

Iskele is built and maintained in the open. If it earns a place on your host,
a star makes it findable for the next person looking for a panel that is not a
container:

**<https://github.com/ibrahimhates/iskele>**

Bug reports and pull requests are welcome — [`CONTRIBUTING.md`](CONTRIBUTING.md)
has the development setup. Security issues go through
[`SECURITY.md`](SECURITY.md) rather than the public issue tracker.

## License

[Apache-2.0](LICENSE)
