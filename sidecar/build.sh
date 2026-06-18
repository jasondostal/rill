#!/bin/bash
# Build RillSidecar.app - a native SwiftUI menu-bar binary. No Xcode required
# (SwiftPM + Command Line Tools). Assembles the .app bundle, ad-hoc signs it,
# and seeds host/token from .env on first build.
set -euo pipefail

DIR="$(cd "$(dirname "$0")" && pwd)"
cd "$DIR"
APP="RillSidecar.app"
BUNDLE_ID="com.jasondostal.rill-sidecar"
CONF="${1:-release}"

echo "[build] swift build ($CONF)"
swift build -c "$CONF"
BIN="$(swift build -c "$CONF" --show-bin-path 2>/dev/null)/RillSidecar"

echo "[build] assembling ${APP}"
rm -rf "$APP"
mkdir -p "$APP/Contents/MacOS" "$APP/Contents/Resources"
cp "$BIN" "$APP/Contents/MacOS/RillSidecar"

cat > "$APP/Contents/Info.plist" <<PLIST
<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
  <key>CFBundleName</key><string>Rill Sidecar</string>
  <key>CFBundleDisplayName</key><string>Rill Sidecar</string>
  <key>CFBundleIdentifier</key><string>${BUNDLE_ID}</string>
  <key>CFBundleVersion</key><string>0.1.0</string>
  <key>CFBundleShortVersionString</key><string>0.1.0</string>
  <key>CFBundlePackageType</key><string>APPL</string>
  <key>CFBundleExecutable</key><string>RillSidecar</string>
  <key>LSMinimumSystemVersion</key><string>13.0</string>
  <key>LSUIElement</key><true/>
  <key>NSHighResolutionCapable</key><true/>
</dict>
</plist>
PLIST

echo "[build] ad-hoc codesign"
codesign --force --deep --sign - "$APP" 2>/dev/null || echo "[build] codesign skipped"

# Seed host + token from .env into the app prefs domain (only if unset, so manual
# edits in Settings win). Token at rest in the prefs plist mirrors the .env it
# came from. No Keychain -> no ACL prompts, no blocking SecItem on the launch path.
if [ -f .env ]; then
  set -a; . ./.env; set +a
  if [ -n "${RILL_HOST:-}" ] && [ -z "$(defaults read "$BUNDLE_ID" rill.host 2>/dev/null || true)" ]; then
    defaults write "$BUNDLE_ID" rill.host "$RILL_HOST"
  fi
  if [ -n "${RILL_TOKEN:-}" ] && [ -z "$(defaults read "$BUNDLE_ID" rill.token 2>/dev/null || true)" ]; then
    defaults write "$BUNDLE_ID" rill.token "$RILL_TOKEN"
    echo "[build] seeded host + token from .env"
  fi
fi

echo "[build] done: ${DIR}/${APP}"
echo "[build] run: open \"${DIR}/${APP}\"  (droplet appears in the menu bar)"
