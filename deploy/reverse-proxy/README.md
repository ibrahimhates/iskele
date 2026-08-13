# Reverse proxy examples

Iskele listens on `127.0.0.1:8377` by default and speaks plain HTTP. Putting a
proxy in front of it adds TLS and a public name; these three configurations do
the same job in nginx, Caddy and Traefik.

Everything here assumes the panel stays on loopback. If you change `listen` to
a public address, the proxy stops being the only way in and the rest of this is
decoration.

## The three things that are easy to get wrong

**WebSocket upgrade.** Container logs, the console and build output are
WebSockets; live stats, engine events and image pulls are Server-Sent Events.
A proxy that does not forward `Upgrade`/`Connection` breaks the first group,
and one that buffers responses breaks the second — the page loads, the logs
never arrive, and nothing in any log says why. Each example handles both.

**Read timeouts.** A log stream is idle whenever the container is quiet, and a
build can run for many minutes. A 60-second read timeout closes both. The
examples raise it.

**The origin check.** The WebSocket handshake is rejected unless `Origin`
matches `Host` — it is the only cross-origin defense available to a WebSocket,
which the same-origin policy does not cover. Forward `Host` unchanged. Every
example does; if yours does not, the console fails with a handshake error and
the browser console says the origin was refused.

## Files

| File | Notes |
|---|---|
| `nginx.conf` | A `server` block. Drop it in `sites-available` and symlink it. |
| `Caddyfile` | The shortest of the three; Caddy gets TLS on its own. |
| `traefik.yml` | Dynamic configuration, for a Traefik that is already running. |

## Certificates

Caddy obtains and renews certificates by itself. For nginx use certbot:

```
certbot --nginx -d panel.example.com
```

Traefik needs a certificate resolver configured in its static configuration;
the example references one called `letsencrypt`.

If you would rather not run a proxy at all, Iskele has a built-in TLS listener
— set `tls.enabled` with a certificate and key in `config.yaml`. It does not
renew anything for you, so a proxy is usually less work.
