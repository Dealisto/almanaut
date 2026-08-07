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
ALMANAUT_AGENT_TOKEN is safer: tokens on the command line are visible to all
users in ps(1) and may be logged in shell history.

Neither is needed when /etc/almanaut-agent/config.toml already exists.
EOF
}

while [ $# -gt 0 ]; do
	case "$1" in
	--server)
		if [ $# -lt 2 ]; then
			echo "install.sh: --server requires an argument" >&2
			usage >&2
			exit 1
		fi
		SERVER_URL="$2"
		shift 2
		;;
	--token)
		if [ $# -lt 2 ]; then
			echo "install.sh: --token requires an argument" >&2
			usage >&2
			exit 1
		fi
		TOKEN="$2"
		shift 2
		;;
	-h | --help) usage; exit 0 ;;
	*) echo "install.sh: unknown argument: $1" >&2; usage >&2; exit 1 ;;
	esac
done

# Validate that SERVER_URL and TOKEN contain no dangerous characters that could
# corrupt the TOML config or enable shell injection. Both are provided by the
# operator or deployment tooling, and a malformed value is an error, not a
# reason to fail silently.
validate_config_value() {
	local name="$1"
	local value="$2"
	if echo "$value" | grep -q '"'; then
		echo "install.sh: $name contains a double-quote, which would corrupt the TOML config" >&2
		exit 1
	fi
	if echo "$value" | grep -q '\\'; then
		echo "install.sh: $name contains a backslash, which the agent's config parser rejects" >&2
		exit 1
	fi
	# Detect newlines using case: in POSIX case patterns, * matches across
	# newlines, so *"<newline>"* matches any value containing one, triggering
	# the error branch.
	case "$value" in
	*"
"*)
		echo "install.sh: $name contains a newline" >&2
		exit 1
		;;
	esac
}
if [ -n "$SERVER_URL" ]; then
	validate_config_value "SERVER_URL" "$SERVER_URL"
fi
if [ -n "$TOKEN" ]; then
	validate_config_value "TOKEN" "$TOKEN"
fi

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

if [ ! -f ./systemd/almanaut-agent.service ]; then
	echo "install.sh: ./systemd/almanaut-agent.service not found; run this from the extracted archive" >&2
	exit 1
fi

if [ ! -f ./systemd/almanaut-agent.timer ]; then
	echo "install.sh: ./systemd/almanaut-agent.timer not found; run this from the extracted archive" >&2
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
	# between creation and chmod. Use printf %s to emit values literally, without
	# shell expansion — a token containing $(…) or backticks must not execute as root.
	(
		umask 077
		printf 'server_url = "%s"\ntoken = "%s"\n' "$SERVER_URL" "$TOKEN" >"$CONFIG_FILE"
	)
	# Explicit chmod: belt-and-braces alongside umask 077. The umask ensures the
	# file is created at 0600, and this confirms it.
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
