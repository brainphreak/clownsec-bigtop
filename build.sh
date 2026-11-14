#!/bin/sh
# Builds Clownsec Big Top for Windows, macOS and Linux into dist/, the version in every file name
# (and shown in the server's window and setup page) so a copy says which build it is.
# A repo-local build cache keeps this independent of the machine's Go setup.
set -e
cd "$(dirname "$0")"
export GOCACHE="$PWD/.gocache" CGO_ENABLED=0
VERSION=${VERSION:-$(date +%y%m%d.%H%M)}
LDFLAGS="-s -w -X main.version=$VERSION"
rm -rf dist && mkdir -p dist
GOOS=windows GOARCH=amd64 go build -trimpath -ldflags "$LDFLAGS" -o "dist/ClownsecBigTop-$VERSION.exe" .
GOOS=darwin  GOARCH=arm64 go build -trimpath -ldflags "$LDFLAGS" -o "dist/clownsec-bigtop-$VERSION-macos-arm64" .
GOOS=darwin  GOARCH=amd64 go build -trimpath -ldflags "$LDFLAGS" -o "dist/clownsec-bigtop-$VERSION-macos-intel" .
GOOS=linux   GOARCH=amd64 go build -trimpath -ldflags "$LDFLAGS" -o "dist/clownsec-bigtop-$VERSION-linux-amd64" .
echo "built $VERSION:"; ls -la dist
