#!/usr/bin/env bash
set -euo pipefail

BINARY="ysf-reflector"
OUTPUT_DIR="dist"
MODULE="github.com/siteworxpro/ysf-reflector-go"
GPG_KEY="ron@siteworxpro.com"
VERSION=$(git describe --tags --abbrev=0 2>/dev/null || echo "dev")
LDFLAGS="-X main.Version=${VERSION}"

sign() {
  local file="$1"
  gpg --batch --yes --local-user "$GPG_KEY" --detach-sign --armor "$file"
  echo "  signed         -> ${file}.asc"
}

mkdir -p "$OUTPUT_DIR"

echo "Building $BINARY..."

GOOS=darwin  GOARCH=amd64 go build -ldflags "$LDFLAGS" -o "$OUTPUT_DIR/${BINARY}-darwin-amd64"  "$MODULE"
echo "  darwin/amd64  -> $OUTPUT_DIR/${BINARY}-darwin-amd64"
sign "$OUTPUT_DIR/${BINARY}-darwin-amd64"

GOOS=darwin  GOARCH=arm64 go build -ldflags "$LDFLAGS" -o "$OUTPUT_DIR/${BINARY}-darwin-arm64"  "$MODULE"
echo "  darwin/arm64  -> $OUTPUT_DIR/${BINARY}-darwin-arm64"
sign "$OUTPUT_DIR/${BINARY}-darwin-arm64"

GOOS=linux   GOARCH=amd64 go build -ldflags "$LDFLAGS" -o "$OUTPUT_DIR/${BINARY}-linux-amd64"   "$MODULE"
echo "  linux/amd64   -> $OUTPUT_DIR/${BINARY}-linux-amd64"
sign "$OUTPUT_DIR/${BINARY}-linux-amd64"

echo "Done."
