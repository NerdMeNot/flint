#!/usr/bin/env sh
# Downloads the container runtime flint-agent supervises — containerd + runc —
# into the agent's bundle directory, so machines need nothing pre-installed.
#
# Run at machine provisioning time (cloud-init does this on elastic machines)
# or once on a static machine before starting the agent:
#
#   sudo scripts/bundle-runtime.sh [/var/lib/flint-agent/bin]
#
# The agent looks for <data-dir>/bin/containerd; when present it launches and
# supervises its own containerd (root/state under the agent data dir). When
# absent it falls back to the system containerd socket.
set -eu

DEST="${1:-/var/lib/flint-agent/bin}"
CONTAINERD_VERSION="${CONTAINERD_VERSION:-2.0.2}"
RUNC_VERSION="${RUNC_VERSION:-1.2.4}"

ARCH="$(uname -m)"
case "$ARCH" in
  x86_64) ARCH=amd64 ;;
  aarch64 | arm64) ARCH=arm64 ;;
  *) echo "unsupported architecture: $ARCH" >&2; exit 1 ;;
esac

if [ "$(uname -s)" != "Linux" ]; then
  echo "the flint-agent runtime bundle is linux-only" >&2
  exit 1
fi

mkdir -p "$DEST"
TMP="$(mktemp -d)"
trap 'rm -rf "$TMP"' EXIT

echo "→ containerd ${CONTAINERD_VERSION} (${ARCH})"
curl -fsSL -o "$TMP/containerd.tgz" \
  "https://github.com/containerd/containerd/releases/download/v${CONTAINERD_VERSION}/containerd-${CONTAINERD_VERSION}-linux-${ARCH}.tar.gz"
tar -xzf "$TMP/containerd.tgz" -C "$TMP"
install -m 0755 "$TMP"/bin/* "$DEST/"

echo "→ runc ${RUNC_VERSION} (${ARCH})"
curl -fsSL -o "$DEST/runc" \
  "https://github.com/opencontainers/runc/releases/download/v${RUNC_VERSION}/runc.${ARCH}"
chmod 0755 "$DEST/runc"

echo "✔ runtime bundle installed to $DEST"
"$DEST/containerd" --version
"$DEST/runc" --version | head -1
