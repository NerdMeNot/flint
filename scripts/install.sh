#!/usr/bin/env sh
# Flint installer: fetches the latest release binary for this platform.
#
#   curl -fsSL https://raw.githubusercontent.com/NerdMeNot/flint/main/scripts/install.sh | sh
#
# Installs `flint` (control plane + CLI). For machines that run CI steps,
# install `flint-agent` instead:  ... | sh -s -- flint-agent
set -eu

BIN="${1:-flint}"
REPO="NerdMeNot/flint"

OS="$(uname -s | tr '[:upper:]' '[:lower:]')"
ARCH="$(uname -m)"
case "$ARCH" in
  x86_64) ARCH=amd64 ;;
  aarch64 | arm64) ARCH=arm64 ;;
  *) echo "unsupported architecture: $ARCH" >&2; exit 1 ;;
esac

if [ "$BIN" = "flint-agent" ] && [ "$OS" != "linux" ]; then
  echo "flint-agent runs on linux machines only" >&2
  exit 1
fi

TAG="$(curl -fsSL "https://api.github.com/repos/$REPO/releases/latest" | sed -n 's/.*"tag_name": *"\([^"]*\)".*/\1/p' | head -1)"
[ -n "$TAG" ] || { echo "could not resolve the latest release" >&2; exit 1; }
VERSION="${TAG#v}"

URL="https://github.com/$REPO/releases/download/$TAG/${BIN}_${VERSION}_${OS}_${ARCH}.tar.gz"
DEST="/usr/local/bin"
[ -w "$DEST" ] || DEST="$HOME/.local/bin"
mkdir -p "$DEST"

TMP="$(mktemp -d)"
trap 'rm -rf "$TMP"' EXIT
echo "→ $BIN $TAG ($OS/$ARCH)"
curl -fsSL "$URL" | tar -xz -C "$TMP"
install -m 0755 "$TMP/$BIN" "$DEST/$BIN"

echo "✔ installed $DEST/$BIN"
"$DEST/$BIN" --version
