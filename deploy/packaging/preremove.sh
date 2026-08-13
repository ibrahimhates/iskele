#!/bin/sh
# Runs before the files are removed.
set -e

# $1 is the remaining version count on rpm and the action word on deb. On an
# upgrade the service is restarted by the postinstall, so it is only stopped
# when the package is going away for good.
case "$1" in
    0|remove|purge)
        if command -v systemctl >/dev/null 2>&1 && [ -d /run/systemd/system ]; then
            systemctl stop iskeled.service >/dev/null 2>&1 || true
            systemctl disable iskeled.service >/dev/null 2>&1 || true
        fi
        ;;
esac
