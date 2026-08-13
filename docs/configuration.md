# Configuration

Values are resolved with the precedence **flag > environment variable > YAML
file > built-in default**, then validated as a whole. A configuration that
cannot work is refused at startup with every problem listed at once, rather
than the first one found.

The file defaults to `/etc/iskele/config.yaml`. Point somewhere else with
`--config` or `ISKELE_CONFIG`. A missing file is not an error — the defaults
are a working configuration.

Not everything is here. Settings an admin changes while the daemon runs — audit
retention, the bind-mount warning — live in the database and are edited from
the settings page. The split is deliberate: what is in this file are
startup-time security boundaries, and an admin who could widen `allowed_paths`
from a browser would be one request away from mounting the whole filesystem
into a container.

## Reference

| YAML | Flag | Environment | Default |
|---|---|---|---|
| — | `--config` | `ISKELE_CONFIG` | `/etc/iskele/config.yaml` |
| `listen` | `--listen` | `ISKELE_LISTEN` | `127.0.0.1:8377` |
| `docker_host` | `--docker-host` | `ISKELE_DOCKER_HOST` | `unix:///var/run/docker.sock` |
| `data_dir` | `--data-dir` | `ISKELE_DATA_DIR` | `/var/lib/iskele` |
| `secret_key_file` | `--secret-key-file` | `ISKELE_SECRET_KEY_FILE` | `/etc/iskele/secret.key` |
| `allowed_paths` | `--allowed-paths` | `ISKELE_ALLOWED_PATHS` | `/opt/stacks`, `/srv` |
| `template_dir` | — | — | `/etc/iskele/templates` |
| `log_level` | `--log-level` | `ISKELE_LOG_LEVEL` | `info` |
| `log_format` | `--log-format` | `ISKELE_LOG_FORMAT` | `auto` |
| `tls.enabled` | `--tls` | `ISKELE_TLS_ENABLED` | `false` |
| `tls.cert_file` | `--tls-cert` | `ISKELE_TLS_CERT_FILE` | — |
| `tls.key_file` | `--tls-key` | `ISKELE_TLS_KEY_FILE` | — |
| `session.access_ttl` | — | `ISKELE_ACCESS_TTL` | `15m` |
| `session.refresh_ttl` | — | `ISKELE_REFRESH_TTL` | `168h` |

`--version` prints the build metadata and exits. `--help` lists the flags.

## The ones worth thinking about

### `listen`

`127.0.0.1:8377` by default, and it should stay there. iskeled logs a warning
at startup when it is bound to a non-loopback address, because at that point
the only thing between the internet and a root-equivalent API is a password
form.

Put a TLS reverse proxy in front instead — see
[`deploy/reverse-proxy/`](../deploy/reverse-proxy/).

### `allowed_paths`

The whitelist that bind mounts and build contexts cannot escape. It is checked
in five places — the creation wizard, compose bind sources, the directory
browser, build contexts and template mounts — by one guard, so they cannot
disagree.

```yaml
allowed_paths:
  - "/opt/stacks"
  - "/srv"
```

- Symlinks are resolved before the comparison, because a link inside an allowed
  root can point anywhere.
- Paths are compared by component, so `/srv-other` does not pass a `/srv` root.
- **An empty list refuses every bind mount.** A misconfiguration fails closed.
- `/` in this list defeats the whole mechanism. If you find yourself wanting
  it, you want a narrower path.

Named volumes and tmpfs mounts touch no host path, so the whitelist does not
apply to them.

### `secret_key_file`

The master key. It encrypts registry passwords and TOTP secrets, and it derives
the JWT signing key. iskeled creates it with mode `0600` on first start and
**refuses to start if anyone else can read it**.

Back it up together with the database. The database without the key gives up
neither its stored passwords nor its second factors; the key without the
database is useless. Losing the key means re-entering every registry
credential and re-enrolling every second factor.

### `docker_host`

`unix:///var/run/docker.sock` for the local daemon. A `tcp://` endpoint works
for a remote one — use TLS if you do, since the Docker API over plain TCP is an
unauthenticated root shell on that machine.

There is no multi-host switcher; one installation manages one endpoint.

### `data_dir`

The SQLite database, archived build logs (`builds/`) and stack working copies
(`stacks/`). It is the directory that fills up, and the one the dashboard's
disk gauge watches.

### `session.access_ttl` and `session.refresh_ttl`

Access tokens are short-lived and not revocable; refresh tokens are long-lived
and rotate on every use. Shortening the access TTL narrows the window a stolen
token is useful for, at the cost of more refresh round-trips. `access_ttl` must
be shorter than `refresh_ttl`, and startup refuses the reverse.

### `log_format`

`auto` writes text on a terminal and JSON otherwise — which is what you want
under systemd, where the journal parses the fields.

## Runtime settings

These are in the database, changed from **Settings** in the panel, and take
effect without a restart.

| Setting | Default | What it does |
|---|---|---|
| Audit log retention | `0` (keep everything) | Entries older than this many days are removed by the daily sweep. Read fresh on every sweep, so a change applies at the next tick. |
| Bind mount warning | on | Warns in the creation wizard whenever a host directory is mounted. A nudge, not a control — `allowed_paths` is what decides. |

## Examples

**Behind a reverse proxy** (the recommended layout):

```yaml
listen: "127.0.0.1:8377"
allowed_paths:
  - "/opt/stacks"
log_format: "json"
```

**Built-in TLS**, when you would rather not run a proxy:

```yaml
listen: "0.0.0.0:8377"
tls:
  enabled: true
  cert_file: "/etc/iskele/tls/fullchain.pem"
  key_file: "/etc/iskele/tls/privkey.pem"
```

Nothing renews those for you.

**Development**, against your own socket:

```sh
iskeled --listen 127.0.0.1:8377 --data-dir ./.data \
        --secret-key-file ./.data/secret.key \
        --allowed-paths "$PWD/stacks" --log-level debug
```

## Checking a configuration

```sh
iskeled --config /etc/iskele/config.yaml --version   # does it parse and validate?
systemctl status iskeled
journalctl -u iskeled -f
```

A refused configuration lists every problem at once:

```
iskeled: invalid configuration:
  - data_dir "data" must be an absolute path
  - secret_key_file "etc/secret.key" must be an absolute path
```
