#!/usr/bin/env bash
#
# Installs Iskele as a systemd service.
#
# Idempotent: running it again upgrades the binary and leaves the config, the
# database and the secret key alone. Nothing here overwrites a file an operator
# may have edited — a config that already exists is kept, and the new default
# is written beside it as .new so the differences can be read.
#
#   sudo ./install.sh                 # install or upgrade from ./iskeled
#   sudo ./install.sh --binary path   # install a binary from somewhere else
#   sudo ./install.sh --no-start      # install without starting the service
#
set -euo pipefail

USER_NAME="iskele"
GROUP_NAME="iskele"
BIN_DIR="/usr/local/bin"
CONFIG_DIR="/etc/iskele"
DATA_DIR="/var/lib/iskele"
UNIT_DIR="/etc/systemd/system"
UNIT_NAME="iskeled.service"

SOURCE_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
BINARY=""
START=1

say()  { printf '\033[1;34m==>\033[0m %s\n' "$*"; }
warn() { printf '\033[1;33m warning:\033[0m %s\n' "$*" >&2; }
die()  { printf '\033[1;31m error:\033[0m %s\n' "$*" >&2; exit 1; }

usage() {
    sed -n '3,12p' "${BASH_SOURCE[0]}" | sed 's/^# \{0,1\}//'
    exit 0
}

while [ $# -gt 0 ]; do
    case "$1" in
        --binary)   BINARY="${2:-}"; shift 2 ;;
        --no-start) START=0; shift ;;
        -h|--help)  usage ;;
        *)          die "unknown option: $1 (try --help)" ;;
    esac
done

# --- Preconditions ------------------------------------------------------

[ "$(id -u)" -eq 0 ] || die "this script must run as root (try: sudo $0)"

# systemctl being on PATH is not the same as systemd running: containers ship
# the binary without the init system, and the difference only surfaces as a
# bus error halfway through an install.
command -v systemctl >/dev/null 2>&1 || die "systemctl not found; this installer targets systemd hosts"
[ -d /run/systemd/system ] || die "systemd is not the init system here; install iskeled by hand or use a systemd host"

if [ -z "$BINARY" ]; then
    for candidate in "$SOURCE_DIR/iskeled" "$SOURCE_DIR/../iskeled" "$SOURCE_DIR/../bin/iskeled"; do
        if [ -f "$candidate" ]; then BINARY="$candidate"; break; fi
    done
fi
[ -n "$BINARY" ] || die "no iskeled binary found; build one with 'make build' or pass --binary"
[ -f "$BINARY" ] || die "no such file: $BINARY"

# Refusing here beats installing a binary for the wrong architecture and
# leaving the operator with a unit that exits 203 on every restart.
if ! "$BINARY" --version >/dev/null 2>&1; then
    die "$BINARY does not run on this host (wrong architecture?)"
fi
VERSION="$("$BINARY" --version 2>/dev/null | head -1)"

UNIT_SOURCE="$SOURCE_DIR/$UNIT_NAME"
CONFIG_SOURCE="$SOURCE_DIR/config.example.yaml"
[ -f "$UNIT_SOURCE" ]   || die "missing $UNIT_SOURCE"
[ -f "$CONFIG_SOURCE" ] || die "missing $CONFIG_SOURCE"

say "installing $VERSION"

# --- User and group -----------------------------------------------------

if ! getent group "$GROUP_NAME" >/dev/null; then
    say "creating group $GROUP_NAME"
    groupadd --system "$GROUP_NAME"
fi

if ! getent passwd "$USER_NAME" >/dev/null; then
    say "creating user $USER_NAME"
    useradd --system --gid "$GROUP_NAME" --home-dir "$DATA_DIR" \
            --no-create-home --shell /usr/sbin/nologin \
            --comment "Iskele Docker panel" "$USER_NAME"
fi

# The Docker socket is the service's entire privilege, and it is equivalent to
# root on this host. Without the group iskeled starts but every engine route
# answers DOCKER_UNAVAILABLE, so this is a warning rather than a failure.
if getent group docker >/dev/null; then
    if id -nG "$USER_NAME" | tr ' ' '\n' | grep -qx docker; then
        :
    else
        say "adding $USER_NAME to the docker group"
        usermod -aG docker "$USER_NAME"
    fi
else
    warn "there is no 'docker' group on this host; iskeled will start but cannot reach the engine"
fi

# --- Directories --------------------------------------------------------

say "creating directories"
# 2770, not 0750: iskeled runs as $USER_NAME and creates $CONFIG_DIR/secret.key
# itself on first start, so the group needs write on the directory or the
# daemon exits with "create key file: permission denied" before it ever
# listens. The unit's ReadWritePaths=/etc/iskele expresses the same intent, but
# that governs systemd's mount namespace and cannot grant a POSIX bit. setgid
# keeps anything created here in the iskele group.
#
# This does let a compromised daemon replace config.yaml. That costs nothing it
# does not already have: its whole privilege is the Docker socket, which is
# root on this host.
install -d -o root       -g "$GROUP_NAME" -m 2770 "$CONFIG_DIR"
install -d -o "$USER_NAME" -g "$GROUP_NAME" -m 0750 "$DATA_DIR"
# Custom catalog entries are read from here; the directory not existing is
# fine, but creating it saves the operator a step.
install -d -o root -g "$GROUP_NAME" -m 0750 "$CONFIG_DIR/templates"

# --- Binary -------------------------------------------------------------

say "installing $BIN_DIR/iskeled"
# To a temporary name first, then rename: install(1) truncates in place, and a
# running daemon would be reading the file it is being written over.
install -o root -g root -m 0755 "$BINARY" "$BIN_DIR/.iskeled.new"
mv -f "$BIN_DIR/.iskeled.new" "$BIN_DIR/iskeled"

# --- Config -------------------------------------------------------------

if [ -f "$CONFIG_DIR/config.yaml" ]; then
    say "keeping the existing $CONFIG_DIR/config.yaml"
    if ! cmp -s "$CONFIG_SOURCE" "$CONFIG_DIR/config.yaml.new" 2>/dev/null; then
        install -o root -g "$GROUP_NAME" -m 0640 "$CONFIG_SOURCE" "$CONFIG_DIR/config.yaml.new"
        say "wrote the new defaults to $CONFIG_DIR/config.yaml.new for comparison"
    fi
else
    say "writing $CONFIG_DIR/config.yaml"
    install -o root -g "$GROUP_NAME" -m 0640 "$CONFIG_SOURCE" "$CONFIG_DIR/config.yaml"
fi

# The secret key is created by iskeled on first start, with mode 0600. It is
# never touched here: overwriting it would make every stored registry password
# and every issued token unreadable.
if [ -f "$CONFIG_DIR/secret.key" ]; then
    say "keeping the existing secret key"
fi

# --- Unit ---------------------------------------------------------------

say "installing $UNIT_DIR/$UNIT_NAME"
install -o root -g root -m 0644 "$UNIT_SOURCE" "$UNIT_DIR/$UNIT_NAME"
systemctl daemon-reload

# --- Start --------------------------------------------------------------

if [ "$START" -eq 0 ]; then
    say "not starting the service (--no-start)"
elif systemctl is-active --quiet "$UNIT_NAME"; then
    say "restarting iskeled"
    systemctl restart "$UNIT_NAME"
else
    say "enabling and starting iskeled"
    systemctl enable --now "$UNIT_NAME"
fi

# --- Report -------------------------------------------------------------

echo
if [ "$START" -eq 1 ] && systemctl is-active --quiet "$UNIT_NAME"; then
    # `|| LISTEN=""`, because pipefail makes a grep that matches nothing fail
    # the assignment, and errexit would end the script here — after a complete
    # install, with no report and a non-zero exit.
    LISTEN="$(grep -E '^listen:' "$CONFIG_DIR/config.yaml" 2>/dev/null | head -1 | sed 's/^listen: *//; s/"//g')" || LISTEN=""
    LISTEN="${LISTEN:-127.0.0.1:8377}"
    PORT="${LISTEN##*:}"
    say "iskeled is running on $LISTEN"
    cat <<EOF

Next: open the panel and create the first admin account. Until you do, the API
stays closed — the bootstrap endpoint is the only thing that answers.

  http://$LISTEN
EOF

    # The default bind is loopback, so a fresh install is not reachable from
    # the machine the operator is sitting at — which is the first thing they
    # run into. An SSH tunnel is the one way in that needs nothing installed.
    case "$LISTEN" in
        127.0.0.1:*|localhost:*|"[::1]:"*)
            HOST_ADDR="$(hostname -I 2>/dev/null | awk '{print $1}')" || HOST_ADDR=""
            cat <<EOF

That address is loopback. From your workstation, tunnel to it and open
http://127.0.0.1:$PORT there:

  ssh -L $PORT:127.0.0.1:$PORT ${SUDO_USER:-<user>}@${HOST_ADDR:-<this-host>}
EOF
            ;;
    esac

    cat <<EOF

Anyone who can reach this panel can start a privileged container and read the
whole host. Keep 'listen' on 127.0.0.1 and terminate TLS in front of it.

  systemctl status iskeled
  journalctl -u iskeled -f

Docs and issues: https://github.com/ibrahimhates/iskele
If Iskele is useful here, a star helps other people find it.
EOF
else
    say "installed. Start it with: systemctl enable --now $UNIT_NAME"
fi
