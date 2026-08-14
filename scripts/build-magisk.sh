#!/usr/bin/env bash
set -euo pipefail

ROOT=$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)
VERSION=${VERSION:-dev}
VERSION_CODE=${VERSION_CODE:-1}
ARCH=${1:-arm64}
OUT_DIR=${OUT_DIR:-"$ROOT/dist"}

if [[ "$ARCH" != "arm64" ]]; then
  echo "unsupported architecture: $ARCH (only arm64 is currently packaged)" >&2
  exit 2
fi

for command_name in go zip; do
  command -v "$command_name" >/dev/null 2>&1 || {
    echo "missing build dependency: $command_name" >&2
    exit 1
  }
done

STAGE=$(mktemp -d)
trap 'rm -rf "$STAGE"' EXIT

cp -R "$ROOT/packaging/magisk/." "$STAGE/"
mkdir -p \
  "$STAGE/bin" \
  "$STAGE/resource/avatars" \
  "$STAGE/resource/db" \
  "$STAGE/resource/qr" \
  "$STAGE/resource/static" \
  "$STAGE/resource/templates" \
  "$OUT_DIR"

# Runtime resource directories may contain account databases and QR images.
# Package only the tracked application assets and leave runtime data empty.
cp -R "$ROOT/resource/static/." "$STAGE/resource/static/"
cp -R "$ROOT/resource/templates/." "$STAGE/resource/templates/"

CGO_ENABLED=0 GOOS=android GOARCH=arm64 \
  go build -trimpath -ldflags="-s -w" -o "$STAGE/bin/yyb-go" "$ROOT/cmd/yyb-go"

sed -i "s/^version=.*/version=$VERSION/" "$STAGE/module.prop"
sed -i "s/^versionCode=.*/versionCode=$VERSION_CODE/" "$STAGE/module.prop"
chmod 0755 "$STAGE"/*.sh "$STAGE/bin/yyb-go"

OUTPUT="$OUT_DIR/yyb-go-magisk-arm64-$VERSION.zip"
rm -f "$OUTPUT"
(cd "$STAGE" && zip -q -r "$OUTPUT" .)
echo "$OUTPUT"
