#!/usr/bin/env bash
# Sign, notarize, staple, and package the macOS build for direct distribution.
#
# Prerequisites (one-time):
#   - "Developer ID Application" certificate in the login Keychain
#   - notarytool credentials stored:  xcrun notarytool store-credentials agentx-notary ...
#
# Usage: scripts/sign-notarize-macos.sh build/bin/agentx.app

set -euo pipefail

APP_PATH="${1:?Usage: sign-notarize-macos.sh path/to/AgentX.app}"
IDENTITY="Developer ID Application: LARAMS HOSPITALITY LLP (375TMA46GZ)"
PROFILE="agentx-notary"
SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
ENTITLEMENTS="$SCRIPT_DIR/../build/darwin/entitlements.plist"
OUT_ZIP="AgentX-macOS.zip"

echo "==> Signing $APP_PATH"
codesign --force --deep --timestamp --options runtime \
  --entitlements "$ENTITLEMENTS" \
  --sign "$IDENTITY" "$APP_PATH"

echo "==> Verifying signature"
codesign --verify --deep --strict --verbose=2 "$APP_PATH"

echo "==> Submitting for notarization (this can take a few minutes)"
ditto -c -k --keepParent "$APP_PATH" notarize-upload.zip
xcrun notarytool submit notarize-upload.zip --keychain-profile "$PROFILE" --wait
rm -f notarize-upload.zip

echo "==> Stapling notarization ticket"
xcrun stapler staple "$APP_PATH"

echo "==> Packaging $OUT_ZIP"
rm -f "$OUT_ZIP"
ditto -c -k --keepParent "$APP_PATH" "$OUT_ZIP"

echo "==> Done: $OUT_ZIP (ready for GitHub release)"
