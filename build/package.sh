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
PWSH_CMD="${PWSH_CMD:-}"
if [[ -z "$PWSH_CMD" ]]; then
  for c in /mnt/c/Program\ Files*/PowerShell/*/pwsh.exe; do
    if [[ -e "$c" ]]; then
      PWSH_CMD="$c"
      break
    fi
  done
fi

WIN_WIX_PATH_DEFAULT="C:\\Program Files\\WiX Toolset v6.0\\bin\\wix.exe"
WIX_FOUND=0

if [[ -n "$PWSH_CMD" && -x "$PWSH_CMD" ]]; then
  # Use override if provided
  if [[ -n "${WIN_WIX_PATH:-}" ]] && "$PWSH_CMD" -NoProfile -Command "Test-Path '$WIN_WIX_PATH'" >/dev/null 2>&1; then
    WIX_FOUND=1
  fi
  # Try mounted Program Files locations
  if [[ "$WIX_FOUND" -eq 0 ]]; then
    for c in /mnt/c/Program\ Files*/WiX\ Toolset*/bin/wix.exe; do
      if [[ -e "$c" ]]; then
        WIN_WIX_PATH="$(wslpath -w "$c")"
        if "$PWSH_CMD" -NoProfile -Command "Test-Path '$WIN_WIX_PATH'" >/dev/null 2>&1; then
          WIX_FOUND=1
          break
        fi
      fi
    done
  fi
  # Try default fallback
  if [[ "$WIX_FOUND" -eq 0 ]]; then
    WIN_WIX_PATH="$WIN_WIX_PATH_DEFAULT"
    if "$PWSH_CMD" -NoProfile -Command "Test-Path '$WIN_WIX_PATH'" >/dev/null 2>&1; then
      WIX_FOUND=1
    fi
  fi
fi

if [[ -n "$PWSH_CMD" && -x "$PWSH_CMD" && "$WIX_FOUND" -eq 1 ]]; then
  echo "Detected pwsh.exe and wix.exe; attempting Windows-side MSI build..."
  WIN_TEMP="$("$PWSH_CMD" -NoProfile -Command "[IO.Path]::GetTempPath()" | tr -d '\r')"
  WIN_TEMP_WSL="$(wslpath -u "$WIN_TEMP")"
  WIN_DEST="$WIN_TEMP\\autostep-$NEW_VERSION"
  WIN_DEST_WSL="$(wslpath -u "$WIN_DEST")"

  ZIP_WSL="$MSI_DIR/$ZIP_NAME"

  # Prepare destination and unzip on Windows-backed path
  mkdir -p "$WIN_DEST_WSL"
  unzip -o "$ZIP_WSL" -d "$WIN_TEMP_WSL" >/dev/null

  "$PWSH_CMD" -NoProfile -ExecutionPolicy Bypass -Command "
    \$dest = '$WIN_DEST';
    \$wixPath = '$WIN_WIX_PATH';
    if (Test-Path \$wixPath) {
      \$wixDir = Split-Path \$wixPath;
      \$env:PATH = \"\$wixDir;\$env:PATH\";
      Write-Host \"Using WiX at: \$wixPath\";
    }
    \$buildScript = Join-Path \$dest 'build\\wix\\build.ps1'
    Write-Host \"Using build script at: \$buildScript\"
    if (Test-Path \$buildScript) {
      pwsh -NoProfile -ExecutionPolicy Bypass -File \$buildScript -Version $NEW_VERSION
    } else {
      Write-Error \"Build script not found: \$buildScript\"
      if (Test-Path \$dest) { Get-ChildItem -Recurse \$dest | Select-Object FullName }
      exit 1
    }
  " || echo "Windows-side MSI build failed."

  # Copy built MSI back if present
  WIN_MSI_PATH="$WIN_DEST_WSL/build/wix/dist/autostep-$NEW_VERSION.msi"
  if [ -f "$WIN_MSI_PATH" ]; then
    cp "$WIN_MSI_PATH" "$MSI_OUT/"
    echo "MSI copied to: $MSI_OUT/autostep-$NEW_VERSION.msi"
  else
    echo "MSI not found at expected path: $WIN_MSI_PATH"
  fi

  # Cleanup Windows temp artifacts (best effort).
  "$PWSH_CMD" -NoProfile -ExecutionPolicy Bypass -Command "
    \$temp = [IO.Path]::GetTempPath();
    \$zip = Join-Path \$temp 'autostep-msi-src-$NEW_VERSION.zip';
    \$dest = Join-Path \$temp 'autostep-$NEW_VERSION';
    if (Test-Path \$zip) { Remove-Item \$zip -Force -ErrorAction SilentlyContinue }
    if (Test-Path \$dest) { Remove-Item \$dest -Recurse -Force -ErrorAction SilentlyContinue }
  " >/dev/null 2>&1 || true
else
  echo "Skipping automatic MSI build (pwsh.exe and/or wix.exe not available)."
fi

