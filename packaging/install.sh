#!/bin/sh
# Install the almanaut inventory agent on this machine.
#
# Run from an extracted release archive:
#   sudo ./install.sh --server https://almanaut.lan --token alm_xxx
#
# Idempotent: re-running upgrades the binary and the units but never touches an
# existing config, because that file holds a token pasted by hand and silently
# blanking it during an upgrade would break reporting with no obvious cause.
set -eu

BIN_DIR=/usr/local/bin
CONFIG_DIR=/etc/almanaut-agent
CONFIG_FILE="$CONFIG_DIR/config.toml"
UNIT_DIR=/etc/systemd/system

SERVER_URL="${ALMANAUT_SERVER_URL:-}"
TOKEN="${ALMANAUT_AGENT_TOKEN:-}"

usage() {
	cat <<'EOF'
usage: install.sh [--server URL] [--token TOKEN]

  --server URL    almanaut base URL, e.g. https://almanaut.lan
  --token TOKEN   an agent-scoped API token from /account/tokens

Both may instead be supplied as ALMANAUT_SERVER_URL and ALMANAUT_AGENT_TOKEN.
Neither is needed when /etc/almanaut-agent/config.toml already exists.
EOF
}

while [ $# -gt 0 ]; do
	case "$1" in
	--server) SERVER_URL="${2:-}"; shift 2 ;;
	--token) TOKEN="${2:-}"; shift 2 ;;
	-h | --help) usage; exit 0 ;;
	*) echo "install.sh: unknown argument: $1" >&2; usage >&2; exit 1 ;;
	esac
done

if [ "$(id -u)" -ne 0 ]; then
	echo "install.sh: must run as root (try: sudo ./install.sh ...)" >&2
	exit 1
fi

if ! command -v systemctl >/dev/null 2>&1; then
	echo "install.sh: systemctl not found; this installer targets systemd hosts" >&2
	exit 1
fi

if [ ! -f ./almanaut-agent ]; then
	echo "install.sh: ./almanaut-agent not found; run this from the extracted archive" >&2
	exit 1
fi

install -m 0755 ./almanaut-agent "$BIN_DIR/almanaut-agent"

mkdir -p "$CONFIG_DIR"
chmod 700 "$CONFIG_DIR"

if [ -f "$CONFIG_FILE" ]; then
	echo "install.sh: keeping the existing $CONFIG_FILE"
else
	if [ -z "$SERVER_URL" ] || [ -z "$TOKEN" ]; then
		echo "install.sh: no existing config, so --server and --token are required" >&2
		usage >&2
		exit 1
	fi
	# Write with a restrictive umask so the token is never briefly world-readable
	# between creation and chmod.
	(
		umask 077
		cat >"$CONFIG_FILE" <<EOF
server_url = "$SERVER_URL"
token = "$TOKEN"
EOF
	)
	chmod 600 "$CONFIG_FILE"
	echo "install.sh: wrote $CONFIG_FILE"
fi

install -m 0644 ./systemd/almanaut-agent.service "$UNIT_DIR/almanaut-agent.service"
install -m 0644 ./systemd/almanaut-agent.timer "$UNIT_DIR/almanaut-agent.timer"

systemctl daemon-reload
systemctl enable --now almanaut-agent.timer

echo
echo "Installed. The agent reports hourly, starting 2 minutes after boot."
echo "Report once now:   sudo systemctl start almanaut-agent.service"
echo "See the result:    journalctl -u almanaut-agent.service -n 20"
echo "Next scheduled:    systemctl list-timers almanaut-agent.timer"
