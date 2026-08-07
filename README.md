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

Requirements: Go 1.23+ (and Node 20+ once the frontend lands in M3).

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

Available today (both unauthenticated):

| Method | Path | Description |
|---|---|---|
| `GET` | `/api/v1/health` | Liveness probe — `{"status":"ok","uptime":"1m2s"}` |
| `GET` | `/api/v1/version` | Build metadata of the running binary |

The full surface is specified in [`PLAN.md`](PLAN.md#6-api-yüzeyi) and will be
published as `docs/openapi.yaml`.

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
