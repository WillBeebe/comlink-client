#!/usr/bin/env bash
# Internal TestFlight using the existing Mothers/Ada Apple signing identity.
set -euo pipefail
ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
MESH="${ROOT}/../mymesh"
ADA="${ROOT}/../../labs/ada-runtime"
for file in "${MESH}/.local/asc.env" "${ADA}/.local/asc.env" "${ROOT}/.local/asc.env"; do
  if [[ -f "$file" ]]; then source "$file"; fi
done
: "${ASC_KEY_ID:?Existing App Store Connect API key required}"
: "${ASC_ISSUER_ID:?Existing App Store Connect issuer required}"
TEAM=LJCC46HF4H
export ASC_KEY_ID ASC_ISSUER_ID
ASC_KEY_PATH="${ASC_KEY_PATH:-}"
if [[ ! -f "$ASC_KEY_PATH" ]]; then
  for key in "${MESH}/.local/AuthKey_${ASC_KEY_ID}.p8" "${ADA}/.local/AuthKey_${ASC_KEY_ID}.p8" "${HOME}/.appstoreconnect/private_keys/AuthKey_${ASC_KEY_ID}.p8"; do
    if [[ -f "$key" ]]; then ASC_KEY_PATH="$key"; break; fi
  done
fi
[[ -f "$ASC_KEY_PATH" ]] || { echo 'Existing ASC key file not found' >&2; exit 1; }
DIST_KC="${COMLINK_DIST_KEYCHAIN:-${ADA}/.local/signing/ada-dist.keychain-db}"
[[ -f "$DIST_KC" ]] || DIST_KC="${MESH}/.local/signing/mymesh-dist.keychain-db"
DIST_PASSWORD="$(tr -d '\n' < "$(dirname "$DIST_KC")/keychain-password")"
VERSION="${COMLINK_TF_VERSION:-0.1.0}"
TRACK="${ROOT}/.local/testflight-build"
BUILD="${COMLINK_TF_BUILD:-1}"
if [[ -z "${COMLINK_TF_BUILD:-}" && -f "$TRACK" ]]; then BUILD="$(( $(cat "$TRACK") + 1 ))"; fi
OUT="${ROOT}/.local/testflight/${VERSION}-${BUILD}"
mkdir -p "$OUT"
if [[ "${COMLINK_TF_UPLOAD_ONLY:-0}" != 1 ]]; then
# Never erase a previous archive/IPA, or reuse an uncertain uploaded build.
[[ ! -e "$OUT/Comlink.xcarchive" ]] || { echo "Archive already exists: $OUT; choose COMLINK_TF_BUILD" >&2; exit 1; }
SSL_CERT_FILE="$(python3 -c 'import certifi;print(certifi.where())')" python3 "${ROOT}/scripts/ios/ensure-app-store-profile.py"
"${ROOT}/scripts/build-ios.sh"
# Snapshot and restore the actual user's keychain list, rather than replacing it.
security list-keychains -d user > "$OUT/keychain-search-before.txt"
restore() {
  python3 - "$OUT/keychain-search-before.txt" <<'PY'
import shlex,subprocess,sys
paths=shlex.split(open(sys.argv[1]).read())
subprocess.run(['security','list-keychains','-d','user','-s',*paths],check=True)
PY
}
trap restore EXIT
security unlock-keychain -p "$DIST_PASSWORD" "$DIST_KC"
security set-key-partition-list -S apple-tool:,apple:,codesign: -s -k "$DIST_PASSWORD" "$DIST_KC" >/dev/null 2>&1
python3 - "$OUT/keychain-search-before.txt" "$DIST_KC" <<'PY'
import shlex,subprocess,sys
paths=shlex.split(open(sys.argv[1]).read())
if sys.argv[2] not in paths: paths.append(sys.argv[2])
subprocess.run(['security','list-keychains','-d','user','-s',*paths],check=True)
PY
xcodebuild -project "${ROOT}/ios/Comlink.xcodeproj" -scheme Comlink -configuration Release \
  -destination 'generic/platform=iOS' -archivePath "$OUT/Comlink.xcarchive" \
  -derivedDataPath "${ROOT}/.local/TestFlightDerivedData" DEVELOPMENT_TEAM="$TEAM" \
  MARKETING_VERSION="$VERSION" CURRENT_PROJECT_VERSION="$BUILD" archive
xcodebuild -exportArchive -archivePath "$OUT/Comlink.xcarchive" \
  -exportOptionsPlist "${ROOT}/ios/Config/ExportOptions.plist" -exportPath "$OUT/export"
restore
trap - EXIT
fi
if [[ "${COMLINK_TF_PREPARE_ONLY:-0}" == 1 ]]; then
  echo "Signed IPA ready: $OUT/export/Comlink.ipa"
  exit 0
fi
# Validate an existing prepared IPA before uploading it.
python3 - "$OUT/export/Comlink.ipa" "$VERSION" "$BUILD" <<'PYVALIDATE'
import plistlib,sys,zipfile
with zipfile.ZipFile(sys.argv[1]) as z:
    info=plistlib.loads(z.read('Payload/Comlink.app/Info.plist'))
if (info['CFBundleIdentifier'],info['CFBundleShortVersionString'],info['CFBundleVersion']) != ('io.containerlabs.comlink',sys.argv[2],sys.argv[3]):
    raise SystemExit('IPA bundle/version differs from requested upload')
PYVALIDATE
export API_PRIVATE_KEYS_DIR="$(dirname "$ASC_KEY_PATH")"
# Reserve the number before uploading: a transport error can have an ambiguous result.
printf '%s\n' "$BUILD" > "$TRACK"
xcrun altool --upload-app -f "$OUT/export/Comlink.ipa" --type ios \
  --api-key "$ASC_KEY_ID" --api-issuer "$ASC_ISSUER_ID" > "$OUT/upload.log" 2>&1
# Some altool versions return zero even after printing UPLOAD FAILED.
if ! grep -q 'UPLOAD SUCCEEDED' "$OUT/upload.log"; then
  echo "Upload not confirmed; inspect private log: $OUT/upload.log" >&2
  exit 1
fi
echo "Uploaded Comlink $VERSION ($BUILD); verify processing and internal TestFlight availability."
