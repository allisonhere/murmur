#!/usr/bin/env bash
#
# Installs Murmur by building it from source with the local Go toolchain.
# Murmur has no prebuilt releases: this always compiles for the host.
#
#   curl -fsSL https://raw.githubusercontent.com/allisonhere/murmur/main/install.sh | bash
#   ./install.sh                       # from inside a clone, builds in place
#   ./install.sh --prefix /opt/bin
#   MURMUR_REF=v0.2.0 ./install.sh     # build a specific tag/branch/commit
#
set -eu

REPO_URL="${MURMUR_REPO_URL:-https://github.com/allisonhere/murmur.git}"
REF="${MURMUR_REF:-main}"
INSTALL_DIR="${MURMUR_INSTALL_DIR:-}"
MIN_GO_MAJOR=1
MIN_GO_MINOR=25

err()  { printf 'install.sh: %s\n' "$*" >&2; exit 1; }
info() { printf '\033[1;34m==>\033[0m %s\n' "$*"; }
warn() { printf '\033[1;33mwarning:\033[0m %s\n' "$*" >&2; }

usage() {
  cat <<'EOF'
Usage: install.sh [--prefix DIR] [--ref REF]

  --prefix DIR   Install the binary into DIR (default: ~/.local/bin, falling
                 back to /usr/local/bin via sudo if that does not exist or
                 is not writable).
  --ref REF      Git branch, tag, or commit to build when fetching a fresh
                 checkout (default: main). Ignored when run from inside an
                 existing Murmur clone, which is built as-is.
  -h, --help     Show this help.

Environment variables MURMUR_INSTALL_DIR, MURMUR_REF and MURMUR_REPO_URL are
equivalent to --prefix, --ref and the source repository.
EOF
}

while [ $# -gt 0 ]; do
  case "$1" in
    --prefix) INSTALL_DIR="${2:?--prefix needs a value}"; shift 2 ;;
    --prefix=*) INSTALL_DIR="${1#*=}"; shift ;;
    --ref) REF="${2:?--ref needs a value}"; shift 2 ;;
    --ref=*) REF="${1#*=}"; shift ;;
    -h|--help) usage; exit 0 ;;
    *) err "unknown argument: $1 (see --help)" ;;
  esac
done

need() { command -v "$1" >/dev/null 2>&1 || err "'$1' is required but was not found in PATH."; }

need go

go_version="$(go env GOVERSION 2>/dev/null || true)"
go_version="${go_version#go}"
go_major="${go_version%%.*}"
go_minor="${go_version#*.}"
go_minor="${go_minor%%.*}"
case "$go_major" in ''|*[!0-9]*) err "could not determine the Go version from 'go env GOVERSION'." ;; esac
if [ "$go_major" -lt "$MIN_GO_MAJOR" ] || { [ "$go_major" -eq "$MIN_GO_MAJOR" ] && [ "$go_minor" -lt "$MIN_GO_MINOR" ]; }; then
  err "Murmur needs Go $MIN_GO_MAJOR.$MIN_GO_MINOR or newer, found go$go_version."
fi

CLEANUP_DIR=""
BUILD_DIR=""
cleanup() {
  [ -n "$CLEANUP_DIR" ] && rm -rf "$CLEANUP_DIR"
  [ -n "$BUILD_DIR" ] && rm -rf "$BUILD_DIR"
}
trap cleanup EXIT INT TERM

# Build in place if we're already sitting inside a Murmur checkout; otherwise
# fetch one. This is what lets `curl | bash` and `./install.sh` from a clone
# both work without the script needing to locate itself.
SRC_DIR=""
if [ -f go.mod ] && grep -q 'module .*murmur' go.mod 2>/dev/null; then
  SRC_DIR="$(pwd)"
fi

if [ -z "$SRC_DIR" ]; then
  need git
  CLEANUP_DIR="$(mktemp -d)"
  SRC_DIR="$CLEANUP_DIR/murmur"
  info "Cloning $REPO_URL @ $REF..."
  if ! git clone --quiet --depth 1 --branch "$REF" "$REPO_URL" "$SRC_DIR" 2>/dev/null; then
    git clone --quiet "$REPO_URL" "$SRC_DIR"
    git -C "$SRC_DIR" checkout --quiet "$REF"
  fi
else
  info "Building existing checkout at $SRC_DIR"
fi

version="$(git -C "$SRC_DIR" describe --tags --always --dirty 2>/dev/null || echo dev)"
module="$(cd "$SRC_DIR" && go list -m)"

BUILD_DIR="$(mktemp -d)"
info "Building murmur $version..."
(
  cd "$SRC_DIR"
  CGO_ENABLED=0 go build -trimpath -ldflags "-s -w -X ${module}/cmd.Version=${version}" -o "$BUILD_DIR/murmur" .
)

# Pick an install directory: an explicit --prefix wins; otherwise prefer
# ~/.local/bin and fall back to /usr/local/bin via sudo.
use_sudo=""
if [ -z "$INSTALL_DIR" ]; then
  default_dir="$HOME/.local/bin"
  mkdir -p "$default_dir" 2>/dev/null || true
  if [ -d "$default_dir" ] && [ -w "$default_dir" ]; then
    INSTALL_DIR="$default_dir"
  else
    INSTALL_DIR="/usr/local/bin"
    [ -w "$INSTALL_DIR" ] || use_sudo="1"
  fi
else
  if [ ! -d "$INSTALL_DIR" ]; then
    mkdir -p "$INSTALL_DIR" 2>/dev/null || use_sudo="1"
  fi
  [ -d "$INSTALL_DIR" ] && [ ! -w "$INSTALL_DIR" ] && use_sudo="1"
fi

info "Installing to $INSTALL_DIR/murmur..."
if [ -n "$use_sudo" ]; then
  need sudo
  sudo mkdir -p "$INSTALL_DIR"
  sudo install -m 0755 "$BUILD_DIR/murmur" "$INSTALL_DIR/murmur"
else
  install -m 0755 "$BUILD_DIR/murmur" "$INSTALL_DIR/murmur"
fi

case ":$PATH:" in
  *":$INSTALL_DIR:"*) ;;
  *) warn "$INSTALL_DIR is not on your PATH."
     printf '  Add this to your shell profile:\n    export PATH="%s:$PATH"\n' "$INSTALL_DIR" >&2 ;;
esac

installed_version="$("$INSTALL_DIR/murmur" version 2>/dev/null || echo "murmur (unknown version)")"
info "Installed ${installed_version} -> $INSTALL_DIR/murmur"
