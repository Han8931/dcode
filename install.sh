#!/bin/sh
# install.sh — build and install the `dcode` (meari-dcode) binary.
#
# Usage:
#   ./install.sh                 # build and install to ~/.local/bin
#   PREFIX=/usr/local ./install.sh   # install to /usr/local/bin (may need sudo)
#   ./install.sh --prefix ~/bin  # install the binary directly into ~/bin
#
# The multi-language repo map uses Tree-sitter (a C library), so a C compiler
# must be available (cgo). macOS Xcode Command Line Tools or gcc/clang on Linux
# are enough. This script never touches your source tree beyond building here.

set -eu

# ---- resolve options --------------------------------------------------------
# BINDIR is where the `dcode` binary lands. With --prefix DIR we install into
# DIR directly; otherwise into $PREFIX/bin (PREFIX defaults to ~/.local).
PREFIX="${PREFIX:-$HOME/.local}"
BINDIR=""

while [ $# -gt 0 ]; do
	case "$1" in
		--prefix)
			[ $# -ge 2 ] || { echo "error: --prefix needs a directory" >&2; exit 2; }
			BINDIR="$2"; shift 2 ;;
		--prefix=*)
			BINDIR="${1#--prefix=}"; shift ;;
		-h|--help)
			sed -n '2,11p' "$0" | sed 's/^# \{0,1\}//'; exit 0 ;;
		*)
			echo "error: unknown argument: $1" >&2; exit 2 ;;
	esac
done
[ -n "$BINDIR" ] || BINDIR="$PREFIX/bin"

# Run from the repo root (the directory holding this script), so `go build`
# sees go.mod regardless of where the user invoked us from.
SCRIPT_DIR=$(CDPATH= cd -- "$(dirname -- "$0")" && pwd)
cd "$SCRIPT_DIR"

# ---- prerequisite checks ----------------------------------------------------
if ! command -v go >/dev/null 2>&1; then
	echo "error: Go is not installed or not on your PATH." >&2
	echo "       Install Go 1.26+ from https://go.dev/dl/ and re-run." >&2
	exit 1
fi

# cgo needs a C compiler; probe the one Go will actually use.
CC_BIN=$(go env CC 2>/dev/null || echo cc)
if ! command -v "$CC_BIN" >/dev/null 2>&1; then
	echo "error: no C compiler ($CC_BIN) found — required for the Tree-sitter repo map." >&2
	echo "       macOS: xcode-select --install    Linux: install gcc or clang." >&2
	exit 1
fi

echo "==> Go:       $(go version | awk '{print $3}')"
echo "==> Compiler: $CC_BIN"
echo "==> Building dcode..."

# ---- build ------------------------------------------------------------------
mkdir -p "$BINDIR"
CGO_ENABLED=1 go build -o "$BINDIR/dcode" .

echo "==> Installed: $BINDIR/dcode"

# ---- PATH hint --------------------------------------------------------------
case ":$PATH:" in
	*":$BINDIR:"*)
		echo "==> Done. Run: dcode" ;;
	*)
		echo
		echo "note: $BINDIR is not on your PATH. Add it, e.g.:"
		echo "      echo 'export PATH=\"$BINDIR:\$PATH\"' >> ~/.zshrc && source ~/.zshrc"
		echo "      then run: dcode" ;;
esac
