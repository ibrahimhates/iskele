#!/usr/bin/env bash
#
# Removes the Iskele service.
#
# By default it stops the daemon and removes the unit and the binary, and it
# keeps everything an operator would miss: the config, the database and the
# secret key. Reinstalling then picks up exactly where it left off.
#
#   sudo ./uninstall.sh            # remove the service, keep the data
#   sudo ./uninstall.sh --purge    # also delete config, database and the key
#   sudo ./uninstall.sh --purge --yes   # no confirmation prompt
#
set -euo pipefail

USER_NAME="iskele"
GROUP_NAME="iskele"
BIN_DIR="/usr/local/bin"
CONFIG_DIR="/etc/iskele"
DATA_DIR="/var/lib/iskele"
UNIT_DIR="/etc/systemd/system"
UNIT_NAME="iskeled.service"

PURGE=0
ASSUME_YES=0

say()  { printf '\033[1;34m==>\033[0m %s\n' "$*"; }
warn() { printf '\033[1;33m warning:\033[0m %s\n' "$*" >&2; }
die()  { printf '\033[1;31m error:\033[0m %s\n' "$*" >&2; exit 1; }

usage() {
    sed -n '3,11p' "${BASH_SOURCE[0]}" | sed 's/^# \{0,1\}//'
    exit 0
}

while [ $# -gt 0 ]; do
    case "$1" in
        --purge)   PURGE=1; shift ;;
        -y|--yes)  ASSUME_YES=1; shift ;;
        -h|--help) usage ;;
        *)         die "unknown option: $1 (try --help)" ;;
    esac
done

[ "$(id -u)" -eq 0 ] || die "this script must run as root (try: sudo $0)"

# --- Confirm the destructive path ---------------------------------------

if [ "$PURGE" -eq 1 ] && [ "$ASSUME_YES" -eq 0 ]; then
    cat <<EOF
--purge deletes:

  $CONFIG_DIR   (config, secret key, custom templates)
  $DATA_DIR     (database: accounts, stacks, build history, audit trail)

The secret key cannot be recovered. Without it every stored registry password
is unreadable, so a later reinstall starts from an empty installation.

Containers, images and volumes on this host are NOT touched: they belong to
Docker, not to Iskele.

EOF
    printf 'Type the word "purge" to continue: '
    read -r reply
    [ "$reply" = "purge" ] || die "aborted"
fi

# --- Service ------------------------------------------------------------

if systemctl list-unit-files "$UNIT_NAME" >/dev/null 2>&1 && \
   [ -f "$UNIT_DIR/$UNIT_NAME" ]; then
    if systemctl is-active --quiet "$UNIT_NAME"; then
        say "stopping iskeled"
        systemctl stop "$UNIT_NAME"
    fi
    if systemctl is-enabled --quiet "$UNIT_NAME" 2>/dev/null; then
        say "disabling iskeled"
        systemctl disable "$UNIT_NAME" >/dev/null 2>&1 || true
    fi
    say "removing $UNIT_DIR/$UNIT_NAME"
    rm -f "$UNIT_DIR/$UNIT_NAME"
    systemctl daemon-reload
    systemctl reset-failed "$UNIT_NAME" >/dev/null 2>&1 || true
else
    say "no $UNIT_NAME installed"
fi

# --- Binary -------------------------------------------------------------

if [ -f "$BIN_DIR/iskeled" ]; then
    say "removing $BIN_DIR/iskeled"
    rm -f "$BIN_DIR/iskeled"
fi

# --- Data ---------------------------------------------------------------

if [ "$PURGE" -eq 1 ]; then
    for dir in "$CONFIG_DIR" "$DATA_DIR"; do
        if [ -d "$dir" ]; then
            say "removing $dir"
            rm -rf "$dir"
        fi
    done

    if getent passwd "$USER_NAME" >/dev/null; then
        say "removing user $USER_NAME"
        userdel "$USER_NAME" 2>/dev/null || warn "could not remove the user $USER_NAME"
    fi
    if getent group "$GROUP_NAME" >/dev/null; then
        groupdel "$GROUP_NAME" 2>/dev/null || true
    fi
else
    say "keeping $CONFIG_DIR and $DATA_DIR (use --purge to delete them)"
fi

echo
say "done"
if [ "$PURGE" -eq 0 ]; then
    cat <<EOF

The config, the database and the secret key are still on this host. Reinstall
and the panel comes back with its accounts and stacks intact.

Containers this panel created are still running: they belong to Docker.
EOF
fi
