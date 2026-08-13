#!/bin/sh
# Runs after the files are removed.
#
# The database, the secret key and the config stay. They hold accounts, stack
# definitions and the audit trail, and a package manager deleting them because
# somebody removed the package would be destroying data nobody asked it to
# touch. `purge` on Debian removes the config; the data directory is left for
# the operator either way.
set -e

if command -v systemctl >/dev/null 2>&1 && [ -d /run/systemd/system ]; then
    systemctl daemon-reload >/dev/null 2>&1 || true
    systemctl reset-failed iskeled.service >/dev/null 2>&1 || true
fi

case "$1" in
    0|remove|purge)
        cat <<'MSG'
Iskele is removed. Its data is not:

  /var/lib/iskele   accounts, stacks, build history, audit trail
  /etc/iskele       config, secret key, custom templates

Delete them by hand if you are done with this installation. Containers the
panel created are still running: they belong to Docker.
MSG
        ;;
esac
