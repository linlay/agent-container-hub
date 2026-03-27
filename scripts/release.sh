#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "$SCRIPT_DIR/.." && pwd)"
RELEASE_ASSETS_DIR="$SCRIPT_DIR/release-assets"
WINDOWS_RELEASE_SCRIPTS_DIR="$REPO_ROOT/release-scripts/windows"
APP_NAME="agent-container-hub"

die() {
  echo "[release] $*" >&2
  exit 1
}

detect_arch() {
  case "$(uname -m)" in
    x86_64|amd64) echo "amd64" ;;
    arm64|aarch64) echo "arm64" ;;
    *) die "cannot detect ARCH from $(uname -m); pass ARCH=amd64|arm64" ;;
  esac
}

detect_host_os() {
  case "$(uname -s)" in
    Linux) echo "linux" ;;
    Darwin) echo "darwin" ;;
    MINGW*|MSYS*|CYGWIN*) echo "windows" ;;
    *) die "cannot detect TARGET_OS from $(uname -s); pass TARGET_OS=linux|darwin|windows" ;;
  esac
}

VERSION="${VERSION:-$(cat "$REPO_ROOT/VERSION" 2>/dev/null || echo "dev")}"
[[ "$VERSION" =~ ^v[0-9]+\.[0-9]+\.[0-9]+$ ]] || die "VERSION must match vX.Y.Z (got: $VERSION)"

ARCH="${ARCH:-$(detect_arch)}"
case "$ARCH" in
  amd64|arm64) ;;
  *) die "ARCH must be amd64 or arm64 (got: $ARCH)" ;;
 esac

TARGET_OS="${TARGET_OS:-$(detect_host_os)}"
case "$TARGET_OS" in
  linux|darwin|windows) ;;
  *) die "TARGET_OS must be linux, darwin, or windows (got: $TARGET_OS)" ;;
 esac

command -v go >/dev/null 2>&1 || die "go is required"
command -v tar >/dev/null 2>&1 || die "tar is required"

cd "$REPO_ROOT"

BINARY_NAME="$APP_NAME"
if [[ "$TARGET_OS" == "windows" ]]; then
  BINARY_NAME="${BINARY_NAME}.exe"
fi

BUNDLE_NAME="${APP_NAME}-${VERSION}-${TARGET_OS}-${ARCH}"
BUNDLE_TAR="$REPO_ROOT/dist/release/${BUNDLE_NAME}.tar.gz"

echo "[release] VERSION=$VERSION TARGET_OS=$TARGET_OS ARCH=$ARCH"

TMP_DIR="$(mktemp -d "${TMPDIR:-/tmp}/agent-container-hub-release.XXXXXX")"
trap 'rm -rf "$TMP_DIR"' EXIT

BUNDLE_ROOT="$TMP_DIR/$APP_NAME"
mkdir -p \
  "$BUNDLE_ROOT/configs" \
  "$BUNDLE_ROOT/data/rootfs" \
  "$BUNDLE_ROOT/data/builds" \
  "$BUNDLE_ROOT/release-scripts/windows"

if [[ "$TARGET_OS" == "linux" ]]; then
  mkdir -p "$BUNDLE_ROOT/systemd"
fi

BINARY_NAME="$APP_NAME"
if [[ "$TARGET_OS" == "windows" ]]; then
  BINARY_NAME="${APP_NAME}.exe"
fi

echo "[release] building binary..."
CGO_ENABLED=0 GOOS="$TARGET_OS" GOARCH="$ARCH" \
  go build \
  -ldflags "-X main.buildVersion=$VERSION" \
  -o "$BUNDLE_ROOT/$BINARY_NAME" \
  ./cmd/agent-container-hub

echo "[release] assembling bundle..."
cp "$REPO_ROOT/.env.example" "$BUNDLE_ROOT/.env.example"
cp "$RELEASE_ASSETS_DIR/start.sh" "$BUNDLE_ROOT/start.sh"
cp "$RELEASE_ASSETS_DIR/stop.sh" "$BUNDLE_ROOT/stop.sh"
cp "$RELEASE_ASSETS_DIR/README.txt" "$BUNDLE_ROOT/README.txt"
cp "$WINDOWS_RELEASE_SCRIPTS_DIR/start.ps1" "$BUNDLE_ROOT/release-scripts/windows/start.ps1"
cp "$WINDOWS_RELEASE_SCRIPTS_DIR/stop.ps1" "$BUNDLE_ROOT/release-scripts/windows/stop.ps1"
cp "$WINDOWS_RELEASE_SCRIPTS_DIR/start.cmd" "$BUNDLE_ROOT/release-scripts/windows/start.cmd"
cp "$WINDOWS_RELEASE_SCRIPTS_DIR/stop.cmd" "$BUNDLE_ROOT/release-scripts/windows/stop.cmd"

if [[ "$TARGET_OS" == "linux" ]]; then
  cp "$RELEASE_ASSETS_DIR/systemd/agent-container-hub.service" "$BUNDLE_ROOT/systemd/agent-container-hub.service"
fi

tar --exclude='.DS_Store' -C "$REPO_ROOT/configs" -cf - environments | tar -C "$BUNDLE_ROOT/configs" -xf -

if [[ "$TARGET_OS" != "windows" ]]; then
  chmod +x "$BUNDLE_ROOT/$BINARY_NAME" "$BUNDLE_ROOT/start.sh" "$BUNDLE_ROOT/stop.sh"
fi

mkdir -p "$(dirname "$BUNDLE_TAR")"
tar -czf "$BUNDLE_TAR" -C "$TMP_DIR" "$APP_NAME"

echo "[release] done: $BUNDLE_TAR"
