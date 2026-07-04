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
CNI_VERSION="${CNI_VERSION:-1.6.2}"

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

# containerd release binaries are glibc-linked: stock distro machines
# (AL2023, Ubuntu, Debian) work; musl systems (Alpine) do not.
if [ ! -e /lib/ld-linux-aarch64.so.1 ] && [ ! -e /lib64/ld-linux-x86-64.so.2 ] && [ ! -e /lib/ld-linux-armhf.so.3 ]; then
  echo "warning: no glibc dynamic loader found — containerd release binaries need glibc (Alpine/musl is unsupported)" >&2
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

# CNI plugins power per-step network namespaces for service containers
# (bridge + host-local IPAM + loopback). Installed under $DEST/cni. The
# bridge's outbound NAT shells out to iptables — stock machine images ship
# it; warn when it's missing so `services:` failures aren't a mystery.
command -v iptables >/dev/null 2>&1 || \
  echo "warning: iptables not found — steps with services: need it (apt/dnf install iptables)" >&2
echo "→ cni plugins ${CNI_VERSION} (${ARCH})"
mkdir -p "$DEST/cni"
curl -fsSL -o "$TMP/cni.tgz" \
  "https://github.com/containernetworking/plugins/releases/download/v${CNI_VERSION}/cni-plugins-linux-${ARCH}-v${CNI_VERSION}.tgz"
tar -xzf "$TMP/cni.tgz" -C "$DEST/cni" ./bridge ./host-local ./loopback ./portmap

echo "✔ runtime bundle installed to $DEST"
"$DEST/containerd" --version
"$DEST/runc" --version | head -1
