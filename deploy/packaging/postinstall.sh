#!/bin/sh
# Runs after the files are unpacked, on install and on upgrade.
#
# It deliberately does not start the service. A package manager that starts a
# root-equivalent panel on a public port because somebody ran `apt install` is
# making a decision that is not its to make; the operator reads the config
# first and starts it when ready.
set -e

chown -R iskele:iskele /var/lib/iskele
chmod 0750 /var/lib/iskele

chown root:iskele /etc/iskele /etc/iskele/config.yaml 2>/dev/null || true
# 2770 so iskeled, which runs as the iskele user, can create
# /etc/iskele/secret.key on first start. At 0750 the daemon exits with
# "create key file: permission denied" before it listens.
chmod 2770 /etc/iskele 2>/dev/null || true
chmod 0640 /etc/iskele/config.yaml 2>/dev/null || true
[ -d /etc/iskele/templates ] && chown root:iskele /etc/iskele/templates && chmod 0750 /etc/iskele/templates

# The Docker socket is this service's entire privilege. Without the group the
# panel runs and every engine route answers DOCKER_UNAVAILABLE, so this is a
# note rather than a failure.
if getent group docker >/dev/null; then
    usermod -aG docker iskele 2>/dev/null || true
else
    echo "iskele: no 'docker' group on this host; the panel will start but cannot reach the engine" >&2
fi

if command -v systemctl >/dev/null 2>&1 && [ -d /run/systemd/system ]; then
    systemctl daemon-reload || true

    if systemctl is-active --quiet iskeled.service; then
        # An upgrade: the operator already chose to run it, so put the new
        # binary in service.
        systemctl restart iskeled.service || true
    else
        cat <<'MSG'

Iskele is installed but not started.

  1. Review /etc/iskele/config.yaml — especially `listen` and `allowed_paths`.
  2. systemctl enable --now iskeled
  3. Open the panel and create the first admin account.

Anyone who can reach this panel can start a privileged container and read the
whole host. Keep `listen` on 127.0.0.1 behind a TLS proxy; see
/usr/share/doc/iskele/reverse-proxy/.

MSG
    fi
fi
