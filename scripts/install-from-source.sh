#!/usr/bin/env bash
#
# Builds and installs berth from source.
#
#   ./scripts/install.sh                 # install to ~/.local/bin
#   ./scripts/install.sh --prefix /usr/local   # needs sudo
#   ./scripts/install.sh --with-clipd    # also install the clipboard agent here
#   ./scripts/install.sh --uninstall
#
# This is a convenience wrapper around `make install`, for machines without
# make or for people who would rather run one script.

set -euo pipefail

PREFIX="${PREFIX:-$HOME/.local}"
WITH_CLIPD=0
UNINSTALL=0

usage() {
	sed -n '3,12p' "$0" | sed 's/^# \{0,1\}//'
	exit "${1:-0}"
}

while [ $# -gt 0 ]; do
	case "$1" in
	--prefix)
		PREFIX="${2:?--prefix needs a directory}"
		shift 2
		;;
	--prefix=*)
		PREFIX="${1#*=}"
		shift
		;;
	--with-clipd)
		WITH_CLIPD=1
		shift
		;;
	--uninstall)
		UNINSTALL=1
		shift
		;;
	-h | --help) usage 0 ;;
	*)
		echo "unknown option: $1" >&2
		usage 1
		;;
	esac
done

BINDIR="$PREFIX/bin"
REPO="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"

step() { printf '\033[36m==>\033[0m %s\n' "$1"; }
ok() { printf '    \033[32m%s\033[0m\n' "$1"; }
die() {
	printf '\033[31merror:\033[0m %s\n' "$1" >&2
	exit 1
}

if [ "$UNINSTALL" -eq 1 ]; then
	step "Removing berth"
	for binary in berth berth-clipd; do
		if [ -e "$BINDIR/$binary" ]; then
			rm -f "$BINDIR/$binary"
			ok "removed $BINDIR/$binary"
		fi
	done
	echo
	echo "Sessions are untouched - they are ordinary tmux sessions."
	echo "Config and cached images, if you want them gone too:"
	echo "  rm -rf ${XDG_CONFIG_HOME:-$HOME/.config}/berth ${XDG_CACHE_HOME:-$HOME/.cache}/berth"
	exit 0
fi

# ---------------------------------------------------------------- checks

command -v go >/dev/null || die "go is not on PATH (needs Go 1.24 or newer)"
command -v tmux >/dev/null || die "tmux is not on PATH - berth cannot run without it"

step "Checking versions"
ok "$(go version)"
ok "$(tmux -V)"

# ---------------------------------------------------------------- build

cd "$REPO"
VERSION="$(git describe --tags --always --dirty 2>/dev/null || echo dev)"

step "Building berth $VERSION"
go build -ldflags "-X main.version=$VERSION" -o berth .
ok "built ./berth"

if [ "$WITH_CLIPD" -eq 1 ]; then
	step "Building berth-clipd"
	mkdir -p dist
	go build -ldflags "-X main.version=$VERSION" -o dist/berth-clipd ./cmd/berth-clipd
	ok "built ./dist/berth-clipd"
fi

# ---------------------------------------------------------------- install

step "Installing to $BINDIR"
if ! mkdir -p "$BINDIR" 2>/dev/null; then
	die "cannot create $BINDIR - for a system prefix, re-run with sudo"
fi
if ! install -m 0755 berth "$BINDIR/berth" 2>/dev/null; then
	die "cannot write to $BINDIR - for a system prefix, re-run with sudo"
fi
ok "installed $BINDIR/berth"

if [ "$WITH_CLIPD" -eq 1 ]; then
	install -m 0755 dist/berth-clipd "$BINDIR/berth-clipd"
	ok "installed $BINDIR/berth-clipd"
fi

# ---------------------------------------------------------------- report

echo
case ":$PATH:" in
*":$BINDIR:"*) ok "$BINDIR is on your PATH" ;;
*)
	printf '\033[33m    %s is not on your PATH yet.\033[0m\n' "$BINDIR"
	echo "    Add it to your shell profile:"
	echo "      export PATH=\"$BINDIR:\$PATH\""
	;;
esac

echo
echo "Run 'berth' to start, or 'berth ls' to list sessions."
if [ "$WITH_CLIPD" -eq 0 ]; then
	echo
	echo "To paste images from another machine's clipboard, build the agent for it:"
	echo "  make clipd-windows    # then install.ps1 over there"
	echo "  make clipd-darwin"
fi
