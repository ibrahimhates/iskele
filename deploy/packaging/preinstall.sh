#!/bin/sh
# Runs before the package's files are unpacked.
#
# The user has to exist before the files land, because the postinstall gives
# them ownership of the data directory and systemd starts the unit as them.
set -e

if ! getent group iskele >/dev/null; then
    groupadd --system iskele
fi

if ! getent passwd iskele >/dev/null; then
    useradd --system --gid iskele --home-dir /var/lib/iskele \
            --no-create-home --shell /usr/sbin/nologin \
            --comment "Iskele Docker panel" iskele
fi
