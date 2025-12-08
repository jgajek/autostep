#!/usr/bin/env bash
set -euo pipefail

ROOT="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")/.." && pwd)"
VERSION_FILE="$ROOT/build/version.txt"
DIST_DIR="$ROOT/dist"

function bump_patch() {
  local v="$1"
  if [[ "$v" =~ ^([0-9]+)\.([0-9]+)\.([0-9]+)$ ]]; then
    local major="${BASH_REMATCH[1]}"
    local minor="${BASH_REMATCH[2]}"
    local patch="${BASH_REMATCH[3]}"
    patch=$((patch + 1))
    echo "${major}.${minor}.${patch}"
  else
    echo "0.1.0"
  fi
}

if [[ -f "$VERSION_FILE" ]]; then
  CURRENT_VERSION="$(tr -d '\r\n' < "$VERSION_FILE")"
else
  CURRENT_VERSION="0.1.0"
fi

NEW_VERSION="$(bump_patch "$CURRENT_VERSION")"
echo "$NEW_VERSION" > "$VERSION_FILE"

echo "Building autostep version $NEW_VERSION"

mkdir -p "$DIST_DIR"
STAGE_DIR="$DIST_DIR/autostep-$NEW_VERSION"
rm -rf "$STAGE_DIR"
mkdir -p "$STAGE_DIR"

COMMIT="$(git rev-parse --short HEAD 2>/dev/null || echo unknown)"
BUILDDATE="$(date -Iseconds)"

GOOS=windows GOARCH=amd64 go build -ldflags "-X main.version=$NEW_VERSION -X main.commit=$COMMIT -X main.buildDate=$BUILDDATE" -o "$STAGE_DIR/autostep.exe" ./cmd/autostep

cp "$ROOT/manifest.json" "$STAGE_DIR/"
cp -r "$ROOT/workflows" "$STAGE_DIR/workflows"
cp -r "$ROOT/artifacts" "$STAGE_DIR/artifacts"

# Copy WiX build assets and version file
mkdir -p "$STAGE_DIR/build"
cp "$VERSION_FILE" "$STAGE_DIR/build/version.txt"
cp "$ROOT/build/README-msi-src.txt" "$STAGE_DIR/build/README-msi-src.txt"
mkdir -p "$STAGE_DIR/build/wix"
cp "$ROOT/build/wix/Product.wxs" "$STAGE_DIR/build/wix/Product.wxs"
cp "$ROOT/build/wix/build.ps1" "$STAGE_DIR/build/wix/build.ps1"

pushd "$DIST_DIR" >/dev/null
ZIP_NAME="autostep-msi-src-$NEW_VERSION.zip"
rm -f "$ZIP_NAME"
zip -r "$ZIP_NAME" "autostep-$NEW_VERSION" >/dev/null
popd >/dev/null

echo "Packaged: $DIST_DIR/$ZIP_NAME"
echo "Next: copy the zip to Windows, unzip, and run: pwsh build/wix/build.ps1"

