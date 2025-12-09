#!/usr/bin/env bash
set -euo pipefail

ROOT="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")/.." && pwd)"
VERSION_FILE="$ROOT/build/version.txt"
DIST_DIR="$ROOT/dist"
MSI_DIR="$DIST_DIR/msi-src"
MSI_OUT="$DIST_DIR/msi"

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

mkdir -p "$MSI_DIR"
mkdir -p "$MSI_OUT"
STAGE_DIR="$MSI_DIR/autostep-$NEW_VERSION"
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

pushd "$MSI_DIR" >/dev/null
ZIP_NAME="autostep-msi-src-$NEW_VERSION.zip"
rm -f "$ZIP_NAME"
zip -r "$ZIP_NAME" "autostep-$NEW_VERSION" >/dev/null
popd >/dev/null

echo "Packaged: $MSI_DIR/$ZIP_NAME"
echo "Next (Windows): copy the zip to a Windows-backed path (e.g., /mnt/c/Users/<You>/Downloads), unzip, and run: pwsh -File build/wix/build.ps1"

# Attempt to build MSI automatically via Windows toolchain even when repo is on WSL-only path.
PWSH_CMD="pwsh.exe"
if ! command -v "$PWSH_CMD" >/dev/null 2>&1; then
  # Try to find pwsh under Program Files paths
  PWSH_CANDIDATES=$(ls /mnt/c/Program\ Files*/PowerShell/*/pwsh.exe 2>/dev/null || true)
  for c in $PWSH_CANDIDATES; do
    PWSH_CMD="$c"
    break
  done
fi

WIX_FOUND=0
WIN_WIX_PATH_DEFAULT="C:\\Program Files\\WiX Toolset v6.0\\bin\\wix.exe"

# Auto-discover wix.exe under /mnt/c/Program Files*/WiX Toolset*/bin
if command -v "$PWSH_CMD" >/dev/null 2>&1; then
  # First try PATH
  if "$PWSH_CMD" -NoProfile -Command "Get-Command wix.exe" >/dev/null 2>&1; then
    WIX_FOUND=1
  else
    # Try provided override
    if [[ -n "${WIN_WIX_PATH:-}" ]] && "$PWSH_CMD" -NoProfile -Command "Test-Path '$WIN_WIX_PATH'" >/dev/null 2>&1; then
      WIX_FOUND=1
    else
      # Discover via mounted paths
      CANDIDATES=$(ls /mnt/c/Program\ Files*/WiX\ Toolset*/bin/wix.exe 2>/dev/null || true)
      for c in $CANDIDATES; do
        WIN_WIX_PATH="$(wslpath -w "$c")"
        if "$PWSH_CMD" -NoProfile -Command "Test-Path '$WIN_WIX_PATH'" >/dev/null 2>&1; then
          WIX_FOUND=1
          break
        fi
      done
      if [[ "$WIX_FOUND" -eq 0 ]]; then
        WIN_WIX_PATH="$WIN_WIX_PATH_DEFAULT"
        if "$PWSH_CMD" -NoProfile -Command "Test-Path '$WIN_WIX_PATH'" >/dev/null 2>&1; then
          WIX_FOUND=1
        fi
      fi
    fi
  fi
fi

if command -v "$PWSH_CMD" >/dev/null 2>&1 && [[ "$WIX_FOUND" -eq 1 ]]; then
  echo "Detected pwsh.exe and wix.exe; attempting Windows-side MSI build..."
  WIN_TEMP="$("$PWSH_CMD" -NoProfile -Command "[IO.Path]::GetTempPath()" | tr -d '\r')"
  WIN_ZIP="$WIN_TEMP\\autostep-msi-src-$NEW_VERSION.zip"
  WIN_DEST="$WIN_TEMP\\autostep-$NEW_VERSION"
  WIN_MSI="$WIN_DEST\\build\\wix\\dist\\autostep-$NEW_VERSION.msi"

  ZIP_WSL="$MSI_DIR/$ZIP_NAME"
  ZIP_WIN="$(wslpath -w "$ZIP_WSL")"

  # Copy zip to Windows temp
  cp "$ZIP_WSL" "$(wslpath -u "$WIN_TEMP")/autostep-msi-src-$NEW_VERSION.zip"

  "$PWSH_CMD" -NoProfile -ExecutionPolicy Bypass -Command "
    \$zip = '$WIN_ZIP';
    \$dest = '$WIN_DEST';
    if (Test-Path \$dest) { Remove-Item \$dest -Recurse -Force -ErrorAction SilentlyContinue }
    Expand-Archive -Path \$zip -DestinationPath \$dest -Force
    Set-Location \$dest
    pwsh -NoProfile -ExecutionPolicy Bypass -File .\build\wix\build.ps1 -Version $NEW_VERSION
  " || echo "Windows-side MSI build failed."

  # Copy built MSI back if present
  if [ -f "$(wslpath -u "$WIN_MSI")" ]; then
    cp "$(wslpath -u "$WIN_MSI")" "$MSI_OUT/"
    echo "MSI copied to: $MSI_OUT/autostep-$NEW_VERSION.msi"
  else
    echo "MSI not found at expected path: $WIN_MSI"
  fi

  # Cleanup Windows temp artifacts (best effort).
  "$PWSH_CMD" -NoProfile -ExecutionPolicy Bypass -Command "
    if (Test-Path '$WIN_ZIP') { Remove-Item '$WIN_ZIP' -Force -ErrorAction SilentlyContinue }
    if (Test-Path '$WIN_DEST') { Remove-Item '$WIN_DEST' -Recurse -Force -ErrorAction SilentlyContinue }
  " >/dev/null 2>&1 || true
else
  echo "Skipping automatic MSI build (pwsh.exe and/or wix.exe not available)."
fi

