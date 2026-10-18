#!/usr/bin/env bash
# Run scripts/ruri-embedding-server.py (Ruri v3 for ccr's vector search) in
# the background and start it automatically:
#
#   macOS                    a launchd LaunchAgent (starts at login)
#   Linux, WSL with systemd  a systemd user service (starts at login, or at
#                            boot with --linger)
#   WSL without systemd      a guarded start from the login shell's profile
#
# Usage:
#   ruri-embedding-service.sh install [--model M] [--host H] [--port P]
#                                     [--device D] [--linger] [--dry-run]
#   ruri-embedding-service.sh uninstall [--dry-run]
#   ruri-embedding-service.sh status
#
# --platform macos|systemd|wsl-profile overrides the detection.
#
# Needs uv (https://docs.astral.sh/uv/). The server script is copied to
# ${XDG_DATA_HOME:-~/.local/share}/ccr/, so the service does not depend on
# where this repository is checked out; without a checkout next to this
# script, it is downloaded from GitHub.

set -euo pipefail

NAME=ccr-ruri-embedding
LABEL=com.github.yteraoka.ccr.ruri-embedding
SERVER_URL=https://raw.githubusercontent.com/yteraoka/ccr/main/scripts/ruri-embedding-server.py

DATA_DIR=${XDG_DATA_HOME:-$HOME/.local/share}/ccr
STATE_DIR=${XDG_STATE_HOME:-$HOME/.local/state}/ccr
SERVER=$DATA_DIR/ruri-embedding-server.py
LAUNCHER=$DATA_DIR/start-ruri-embedding-server.sh

PLIST=$HOME/Library/LaunchAgents/$LABEL.plist
MAC_LOG=$HOME/Library/Logs/$NAME.log
UNIT=${XDG_CONFIG_HOME:-$HOME/.config}/systemd/user/$NAME.service
PROFILE_LOG=$STATE_DIR/ruri-embedding-server.log
PROFILE_PID=$STATE_DIR/ruri-embedding-server.pid
MARK_BEGIN="# >>> $NAME >>>"
MARK_END="# <<< $NAME <<<"

model=cl-nagoya/ruri-v3-310m
host=127.0.0.1
port=8080
device=
linger=false
dry_run=false
platform=

die() { echo "error: $*" >&2; exit 1; }
say() { echo "$*" >&2; }

usage() {
	sed -n '2,/^$/p' "$0" | sed 's/^# \{0,1\}//' >&2
	exit "${1:-2}"
}

detect_platform() {
	if [ -n "$platform" ]; then
		echo "$platform"
	elif [ "$(uname -s)" = Darwin ]; then
		echo macos
	elif [ -d /run/systemd/system ]; then
		echo systemd
	elif is_wsl; then
		echo wsl-profile
	else
		die "no systemd user session here; start the server some other way: uv run --script $SERVER"
	fi
}

is_wsl() {
	[ -n "${WSL_DISTRO_NAME:-}" ] || grep -qi microsoft /proc/version 2>/dev/null
}

# server_args prints the server's arguments, one per line.
server_args() {
	printf '%s\n' --model "$model" --host "$host" --port "$port"
	if [ -n "$device" ]; then
		printf '%s\n' --device "$device"
	fi
}

find_uv() {
	command -v uv 2>/dev/null && return
	for p in "$HOME/.local/bin/uv" "$HOME/.cargo/bin/uv" /opt/homebrew/bin/uv /usr/local/bin/uv; do
		if [ -x "$p" ]; then
			echo "$p"
			return
		fi
	done
	die "uv not found; install it first: curl -LsSf https://astral.sh/uv/install.sh | sh"
}

# write FILE: replace FILE with stdin (or show it, with --dry-run).
write() {
	if $dry_run; then
		say "--- $1"
		cat >&2
		return
	fi
	mkdir -p "$(dirname "$1")"
	cat >"$1.tmp.$$"
	mv "$1.tmp.$$" "$1"
}

run() {
	if $dry_run; then
		say "+ $*"
	else
		"$@"
	fi
}

install_server() {
	local here
	here=$(cd "$(dirname "$0")" 2>/dev/null && pwd || true)
	if $dry_run; then
		say "+ copy ruri-embedding-server.py to $SERVER"
		return
	fi
	mkdir -p "$DATA_DIR"
	if [ -n "$here" ] && [ -f "$here/ruri-embedding-server.py" ]; then
		cp "$here/ruri-embedding-server.py" "$SERVER.tmp.$$"
	else
		say "downloading $SERVER_URL"
		curl -fsSL "$SERVER_URL" -o "$SERVER.tmp.$$"
	fi
	mv "$SERVER.tmp.$$" "$SERVER"
	chmod 755 "$SERVER"
}

# prefetch installs the server's Python dependencies now, so a problem shows
# up here rather than in a log, and the first start is not minutes long.
prefetch() {
	$dry_run && return
	say "installing the server's dependencies with uv (the first time downloads PyTorch; this can take a while)..."
	"$uv" run --script "$SERVER" --help >/dev/null
}

xml_escape() {
	sed -e 's/&/\&amp;/g' -e 's/</\&lt;/g' -e 's/>/\&gt;/g' -e 's/"/\&quot;/g'
}

# systemd_quote quotes one ExecStart word: double quotes, with backslash,
# quote and % (a specifier) escaped.
systemd_quote() {
	local s=${1//\\/\\\\}
	s=${s//\"/\\\"}
	s=${s//%/%%}
	printf '"%s"' "$s"
}

install_macos() {
	local args a
	args="    <string>$(printf '%s' "$uv" | xml_escape)</string>
    <string>run</string>
    <string>--script</string>
    <string>$(printf '%s' "$SERVER" | xml_escape)</string>"
	while IFS= read -r a; do
		args="$args
    <string>$(printf '%s' "$a" | xml_escape)</string>"
	done < <(server_args)

	write "$PLIST" <<EOF
<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
  <key>Label</key>
  <string>$LABEL</string>
  <key>ProgramArguments</key>
  <array>
$args
  </array>
  <key>RunAtLoad</key>
  <true/>
  <key>KeepAlive</key>
  <dict>
    <key>SuccessfulExit</key>
    <false/>
  </dict>
  <key>ProcessType</key>
  <string>Background</string>
  <key>StandardOutPath</key>
  <string>$(printf '%s' "$MAC_LOG" | xml_escape)</string>
  <key>StandardErrorPath</key>
  <string>$(printf '%s' "$MAC_LOG" | xml_escape)</string>
</dict>
</plist>
EOF
	local domain
	domain=gui/$(id -u)
	run launchctl bootout "$domain/$LABEL" 2>/dev/null || true
	run launchctl bootstrap "$domain" "$PLIST"
	say "installed $PLIST (log: $MAC_LOG)"
}

uninstall_macos() {
	run launchctl bootout "gui/$(id -u)/$LABEL" 2>/dev/null || true
	run rm -f "$PLIST"
	say "removed $PLIST"
}

status_macos() {
	launchctl print "gui/$(id -u)/$LABEL" 2>/dev/null | grep -E '^\s*(state|pid|last exit code) =' ||
		say "not loaded"
	say "log: $MAC_LOG"
}

install_systemd() {
	local exec a
	exec="$(systemd_quote "$uv") run --script $(systemd_quote "$SERVER")"
	while IFS= read -r a; do
		exec="$exec $(systemd_quote "$a")"
	done < <(server_args)

	write "$UNIT" <<EOF
[Unit]
Description=Ruri v3 embedding server for ccr's vector search
Documentation=https://github.com/yteraoka/ccr

[Service]
ExecStart=$exec
Restart=on-failure
RestartSec=10

[Install]
WantedBy=default.target
EOF
	run systemctl --user daemon-reload
	run systemctl --user enable "$NAME.service"
	run systemctl --user restart "$NAME.service"
	if $linger; then
		run loginctl enable-linger "$(id -un)"
	fi
	say "installed $UNIT (log: journalctl --user -u $NAME)"
	if ! $linger; then
		say "it starts when you log in; add --linger to start it at boot instead"
	fi
}

uninstall_systemd() {
	run systemctl --user disable --now "$NAME.service" 2>/dev/null || true
	run rm -f "$UNIT"
	run systemctl --user daemon-reload
	say "removed $UNIT"
}

status_systemd() {
	systemctl --user --no-pager status "$NAME.service" || true
}

# profile_file is the login shell profile WSL's shell reads at start.
profile_file() {
	case "$(basename "${SHELL:-}")" in
	zsh) echo "$HOME/.zprofile" ;;
	bash)
		if [ -f "$HOME/.bash_profile" ]; then
			echo "$HOME/.bash_profile"
		else
			echo "$HOME/.profile"
		fi
		;;
	*) echo "$HOME/.profile" ;;
	esac
}

install_wsl_profile() {
	local cmd a
	cmd="$(printf '%q' "$uv") run --script $(printf '%q' "$SERVER")"
	while IFS= read -r a; do
		cmd="$cmd $(printf '%q' "$a")"
	done < <(server_args)

	write "$LAUNCHER" <<EOF
#!/usr/bin/env bash
# Starts ccr's Ruri v3 embedding server unless it is already running.
# Written by ruri-embedding-service.sh; called from $(profile_file).
pid=\$(cat $(printf '%q' "$PROFILE_PID") 2>/dev/null)
if [ -n "\$pid" ] && kill -0 "\$pid" 2>/dev/null; then
	exit 0
fi
mkdir -p $(printf '%q' "$STATE_DIR")
nohup $cmd >>$(printf '%q' "$PROFILE_LOG") 2>&1 </dev/null &
echo \$! >$(printf '%q' "$PROFILE_PID")
EOF
	run chmod 755 "$LAUNCHER"

	local profile
	profile=$(profile_file)
	if ! grep -qF "$MARK_BEGIN" "$profile" 2>/dev/null; then
		if $dry_run; then
			say "--- append to $profile"
			say "$MARK_BEGIN"
			say "[ -x $LAUNCHER ] && $LAUNCHER"
			say "$MARK_END"
		else
			# keep the block contiguous so uninstall takes out exactly it
			if [ -s "$profile" ] && [ -n "$(tail -c 1 "$profile")" ]; then
				echo >>"$profile"
			fi
			printf '%s\n[ -x %q ] && %q\n%s\n' "$MARK_BEGIN" "$LAUNCHER" "$LAUNCHER" "$MARK_END" >>"$profile"
		fi
	fi
	# a server started with the old options would otherwise keep running
	stop_profile_server
	run "$LAUNCHER"
	say "installed: $profile starts the server when you open a WSL shell (log: $PROFILE_LOG)"
	say "with systemd enabled in WSL ([boot] systemd=true in /etc/wsl.conf, then wsl --shutdown)"
	say "this script sets up a systemd user service instead, which also restarts it on failure"
}

uninstall_wsl_profile() {
	local profile pid
	profile=$(profile_file)
	if grep -qF "$MARK_BEGIN" "$profile" 2>/dev/null; then
		if $dry_run; then
			say "+ remove the $NAME block from $profile"
		else
			awk -v b="$MARK_BEGIN" -v e="$MARK_END" '
				$0 == b { skip = 1; next }
				$0 == e { skip = 0; next }
				!skip' "$profile" >"$profile.tmp.$$"
			mv "$profile.tmp.$$" "$profile"
		fi
	fi
	stop_profile_server
	run rm -f "$LAUNCHER" "$PROFILE_PID"
	say "removed the server's start from $profile"
}

# stop_profile_server stops the server the launcher started, waiting (a
# few seconds at most) for it to let go of its port, and forgets its pid so
# the launcher starts a new one.
stop_profile_server() {
	local pid
	pid=$(cat "$PROFILE_PID" 2>/dev/null || true)
	if [ -z "$pid" ] || ! kill -0 "$pid" 2>/dev/null; then
		return
	fi
	run kill "$pid"
	$dry_run && return
	for _ in 1 2 3 4 5 6 7 8 9 10; do
		kill -0 "$pid" 2>/dev/null || break
		case $(ps -o stat= -p "$pid" 2>/dev/null) in Z*) break ;; esac
		sleep 0.5
	done
	rm -f "$PROFILE_PID"
}

status_wsl_profile() {
	local pid
	pid=$(cat "$PROFILE_PID" 2>/dev/null || true)
	if [ -n "$pid" ] && kill -0 "$pid" 2>/dev/null; then
		say "running (pid $pid)"
	else
		say "not running"
	fi
	say "log: $PROFILE_LOG"
}

# check_endpoint asks the server for one embedding.
check_endpoint() {
	local url=http://$host:$port/v1/embeddings
	if curl -fsS -m 30 -H 'Content-Type: application/json' -d '{"input": ["テスト"]}' "$url" >/dev/null 2>&1; then
		say "$url: responding"
	else
		say "$url: not responding (still loading the model on a first start? see the log)"
	fi
}

main() {
	[ $# -ge 1 ] || usage
	local action=$1
	shift
	while [ $# -gt 0 ]; do
		case $1 in
		--model) model=${2:?--model needs a value}; shift ;;
		--host) host=${2:?--host needs a value}; shift ;;
		--port) port=${2:?--port needs a value}; shift ;;
		--device) device=${2:?--device needs a value}; shift ;;
		--platform) platform=${2:?--platform needs a value}; shift ;;
		--linger) linger=true ;;
		--dry-run) dry_run=true ;;
		-h | --help) usage 0 ;;
		*) die "unknown option: $1" ;;
		esac
		shift
	done
	local p
	p=$(detect_platform)
	case $p in
	macos | systemd | wsl-profile) ;;
	*) die "--platform must be macos, systemd or wsl-profile, not $p" ;;
	esac
	local fn=${p//-/_}

	case $action in
	install)
		uv=$(find_uv)
		install_server
		prefetch
		"install_$fn"
		if [ "$model" != cl-nagoya/ruri-v3-310m ] || [ "$host" != 127.0.0.1 ] || [ "$port" != 8080 ]; then
			say
			say "this is not ccr's default endpoint; set it in ~/.config/ccr/config.json:"
			say "  {\"history\": {\"embedding\": {\"url\": \"http://$host:$port/v1/embeddings\", \"model\": \"$model\"}}}"
		else
			say
			say "turn vector search on with {\"history\": {\"embedding\": {}}} in ~/.config/ccr/config.json"
		fi
		;;
	uninstall) "uninstall_$fn" ;;
	status)
		"status_$fn"
		check_endpoint
		;;
	*) usage ;;
	esac
}

main "$@"
