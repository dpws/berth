#!/bin/sh
#
# Installs berth, a tmux session manager for coding agents.
#
#   curl -fsSL https://raw.githubusercontent.com/dpws/berth/main/install.sh | sh
#
# Environment:
#   VERSION           tag to install, e.g. v0.2.0 (default: latest release)
#   BERTH_INSTALL_DIR where to put the binary (default: ~/.local/bin)
#   BERTH_CLIPD       set to 1 to also install the clipboard agent
#   BERTH_DRY_RUN     set to 1 to print what would happen and stop
#
# Nothing is installed outside the target directory, and no sudo is used
# unless you point BERTH_INSTALL_DIR somewhere that needs it.

set -eu

REPO="dpws/berth"
: "${VERSION:=latest}"
: "${BERTH_INSTALL_DIR:=$HOME/.local/bin}"
: "${BERTH_CLIPD:=0}"
: "${BERTH_DRY_RUN:=0}"

# ---------------------------------------------------------------- output

if [ -t 1 ]; then
	cyan=$(printf '\033[36m') green=$(printf '\033[32m')
	yellow=$(printf '\033[33m') red=$(printf '\033[31m') off=$(printf '\033[0m')
else
	cyan= green= yellow= red= off=
fi

step() { printf '%s==>%s %s\n' "$cyan" "$off" "$1"; }
ok() { printf '    %s%s%s\n' "$green" "$1" "$off"; }
warn() { printf '    %s%s%s\n' "$yellow" "$1" "$off"; }
die() {
	printf '%serror:%s %s\n' "$red" "$off" "$1" >&2
	exit 1
}

# ---------------------------------------------------------------- platform

detect_os() {
	case "$(uname -s)" in
	Linux) echo linux ;;
	Darwin) echo darwin ;;
	MINGW* | MSYS* | CYGWIN* | Windows_NT)
		die "berth needs a pty and tmux, so it does not run on Windows.
Install it on the machine you SSH into. For pasting images from this
machine's clipboard, see berth-clipd:
  https://github.com/$REPO/tree/main/cmd/berth-clipd"
		;;
	*) die "unsupported operating system: $(uname -s)" ;;
	esac
}

detect_arch() {
	case "$(uname -m)" in
	x86_64 | amd64) echo amd64 ;;
	aarch64 | arm64) echo arm64 ;;
	armv7l | armv6l) echo arm ;;
	*) die "unsupported architecture: $(uname -m)" ;;
	esac
}

# ---------------------------------------------------------------- fetching

if command -v curl >/dev/null 2>&1; then
	fetch() { curl -fsSL "$1"; }
	fetch_to() { curl -fsSL "$1" -o "$2"; }
elif command -v wget >/dev/null 2>&1; then
	fetch() { wget -qO- "$1"; }
	fetch_to() { wget -qO "$2" "$1"; }
else
	die "neither curl nor wget is installed"
fi

latest_version() {
	# Ask the API rather than following /releases/latest, so the failure mode
	# when there are no releases yet is a clear message.
	tag=$(fetch "https://api.github.com/repos/$REPO/releases/latest" 2>/dev/null |
		sed -n 's/.*"tag_name": *"\([^"]*\)".*/\1/p' | head -1)
	[ -n "$tag" ] || die "no published release found for $REPO.
Install from source instead:
  go install github.com/$REPO@latest"
	echo "$tag"
}

sha256_of() {
	if command -v sha256sum >/dev/null 2>&1; then
		sha256sum "$1" | cut -d' ' -f1
	elif command -v shasum >/dev/null 2>&1; then
		shasum -a 256 "$1" | cut -d' ' -f1
	else
		echo ""
	fi
}

# ---------------------------------------------------------------- install

OS=$(detect_os)
ARCH=$(detect_arch)

step "Installing berth"
ok "platform: ${OS}/${ARCH}"

if [ "$VERSION" = "latest" ]; then
	VERSION=$(latest_version)
fi
ok "version:  $VERSION"
ok "target:   $BERTH_INSTALL_DIR"

BASE="https://github.com/$REPO/releases/download/$VERSION"
ARCHIVE="berth_${VERSION}_${OS}_${ARCH}.tar.gz"

if [ "$BERTH_DRY_RUN" = "1" ]; then
	echo
	echo "would download: $BASE/$ARCHIVE"
	echo "would verify:   $BASE/checksums.txt"
	echo "would install:  $BERTH_INSTALL_DIR/berth"
	exit 0
fi

tmp=$(mktemp -d)
trap 'rm -rf "$tmp"' EXIT INT TERM

step "Downloading"
fetch_to "$BASE/$ARCHIVE" "$tmp/$ARCHIVE" 2>/dev/null ||
	die "no build for ${OS}/${ARCH} in $VERSION.
Build from source instead:
  go install github.com/$REPO@latest"
ok "$ARCHIVE"

step "Verifying"
if fetch_to "$BASE/checksums.txt" "$tmp/checksums.txt" 2>/dev/null; then
	# sha256sum may write names with a leading "./", so compare basenames
	# rather than whole fields.
	want=$(awk -v want="$ARCHIVE" '
		{ name = $2; sub(/^\.\//, "", name); if (name == want) { print $1; exit } }
	' "$tmp/checksums.txt")
	got=$(sha256_of "$tmp/$ARCHIVE")
	if [ -z "$got" ]; then
		warn "no sha256 tool found, skipping checksum"
	elif [ -z "$want" ]; then
		die "$ARCHIVE is not listed in checksums.txt.
The download cannot be verified, so it is not being installed."
	elif [ "$want" != "$got" ]; then
		die "checksum mismatch for $ARCHIVE
  expected $want
  got      $got
Do not use this download."
	else
		ok "sha256 ok"
	fi
else
	warn "no checksums.txt published for $VERSION"
fi

step "Installing"
tar -xzf "$tmp/$ARCHIVE" -C "$tmp"
mkdir -p "$BERTH_INSTALL_DIR" 2>/dev/null ||
	die "cannot create $BERTH_INSTALL_DIR (set BERTH_INSTALL_DIR, or re-run with sudo)"

install_one() {
	[ -f "$tmp/$1" ] || return 0
	install -m 0755 "$tmp/$1" "$BERTH_INSTALL_DIR/$1" 2>/dev/null ||
		die "cannot write to $BERTH_INSTALL_DIR (set BERTH_INSTALL_DIR, or re-run with sudo)"
	ok "installed $BERTH_INSTALL_DIR/$1"
}

install_one berth
if [ "$BERTH_CLIPD" = "1" ]; then
	install_one berth-clipd
fi

# ---------------------------------------------------------------- report

echo
if ! command -v tmux >/dev/null 2>&1; then
	warn "tmux is not installed - berth cannot run without it"
	warn "  Debian/Ubuntu: sudo apt install tmux"
	warn "  macOS:         brew install tmux"
fi

case ":$PATH:" in
*":$BERTH_INSTALL_DIR:"*) ok "$BERTH_INSTALL_DIR is on your PATH" ;;
*)
	warn "$BERTH_INSTALL_DIR is not on your PATH yet. Add it:"
	warn "  export PATH=\"$BERTH_INSTALL_DIR:\$PATH\""
	;;
esac

echo
printf '%sRun %sberth%s to start.%s\n' "$green" "$cyan" "$green" "$off"
