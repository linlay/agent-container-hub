#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "$SCRIPT_DIR/.." && pwd)"
RELEASE_ASSETS_DIR="$SCRIPT_DIR/release-assets"
APP_NAME="agent-container-hub"

die() {
  echo "[release] $*" >&2
  exit 1
}

VERSION="${VERSION:-$(cat "$REPO_ROOT/VERSION" 2>/dev/null || echo "dev")}"
[[ "$VERSION" =~ ^v[0-9]+\.[0-9]+\.[0-9]+$ ]] || die "VERSION must match vX.Y.Z (got: $VERSION)"

if [[ -z "${ARCH:-}" ]]; then
  case "$(uname -m)" in
    x86_64|amd64) ARCH=amd64 ;;
    arm64|aarch64) ARCH=arm64 ;;
    *) die "cannot detect ARCH from $(uname -m); pass ARCH=amd64|arm64" ;;
  esac
fi

case "$ARCH" in
  amd64|arm64) ;;
  *) die "ARCH must be amd64 or arm64 (got: $ARCH)" ;;
esac

command -v go >/dev/null 2>&1 || die "go is required"
command -v tar >/dev/null 2>&1 || die "tar is required"

cd "$REPO_ROOT"

BUNDLE_NAME="${APP_NAME}-${VERSION}-linux-${ARCH}"
BUNDLE_TAR="$REPO_ROOT/dist/release/${BUNDLE_NAME}.tar.gz"

echo "[release] VERSION=$VERSION ARCH=$ARCH"

TMP_DIR="$(mktemp -d "${TMPDIR:-/tmp}/agent-container-hub-release.XXXXXX")"
trap 'rm -rf "$TMP_DIR"' EXIT

BUNDLE_ROOT="$TMP_DIR/$APP_NAME"
mkdir -p \
  "$BUNDLE_ROOT/configs" \
  "$BUNDLE_ROOT/data/rootfs" \
  "$BUNDLE_ROOT/data/builds" \
  "$BUNDLE_ROOT/systemd"

echo "[release] building binary..."
CGO_ENABLED=0 GOOS=linux GOARCH="$ARCH" \
  go build \
  -ldflags "-X main.buildVersion=$VERSION" \
  -o "$BUNDLE_ROOT/$APP_NAME" \
  ./cmd/agent-container-hub

echo "[release] assembling bundle..."
cp "$REPO_ROOT/VERSION" "$BUNDLE_ROOT/VERSION"
cp "$REPO_ROOT/.env.example" "$BUNDLE_ROOT/.env.example"
cp "$RELEASE_ASSETS_DIR/start.sh" "$BUNDLE_ROOT/start.sh"
cp "$RELEASE_ASSETS_DIR/stop.sh" "$BUNDLE_ROOT/stop.sh"
cp "$RELEASE_ASSETS_DIR/README.txt" "$BUNDLE_ROOT/README.txt"
cp "$RELEASE_ASSETS_DIR/systemd/agent-container-hub.service" "$BUNDLE_ROOT/systemd/agent-container-hub.service"

tar --exclude='.DS_Store' -C "$REPO_ROOT/configs" -cf - environments | tar -C "$BUNDLE_ROOT/configs" -xf -

chmod +x "$BUNDLE_ROOT/$APP_NAME" "$BUNDLE_ROOT/start.sh" "$BUNDLE_ROOT/stop.sh"

mkdir -p "$(dirname "$BUNDLE_TAR")"
tar -czf "$BUNDLE_TAR" -C "$TMP_DIR" "$APP_NAME"

echo "[release] done: $BUNDLE_TAR"
