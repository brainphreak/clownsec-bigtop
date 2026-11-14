#!/bin/sh
# Builds Clownsec Big Top for Windows, macOS and Linux into dist/, the version in every file name
# (and shown in the server's window and setup page) so a copy says which build it is:
#   ClownsecBigTop-<v>.exe (+ -windows.zip), clownsec-bigtop-<v>-macos-arm64 / -macos-intel,
#   clownsec-bigtop-<v>-linux-amd64, clownsec-bigtop-<v>-x86_64.AppImage, SHA256SUMS.txt
# The AppImage needs mksquashfs (squashfs-tools; on macOS: brew install squashfs) and downloads
# the AppImage runtime once into .cache/. The updater checks every download against SHA256SUMS.txt.
set -e
cd "$(dirname "$0")"
export GOCACHE="$PWD/.gocache" CGO_ENABLED=0
VERSION=${VERSION:-$(date +%y%m%d.%H%M)}
# symbols kept (no -s -w): antivirus software treats stripped Go binaries as suspicious
LDFLAGS="-X main.version=$VERSION"
rm -rf dist && mkdir -p dist
GOOS=windows GOARCH=amd64 go build -trimpath -ldflags "$LDFLAGS" -o "dist/ClownsecBigTop-$VERSION.exe" .
GOOS=darwin  GOARCH=arm64 go build -trimpath -ldflags "$LDFLAGS" -o "dist/clownsec-bigtop-$VERSION-macos-arm64" .
GOOS=darwin  GOARCH=amd64 go build -trimpath -ldflags "$LDFLAGS" -o "dist/clownsec-bigtop-$VERSION-macos-intel" .
GOOS=linux   GOARCH=amd64 go build -trimpath -ldflags "$LDFLAGS" -o "dist/clownsec-bigtop-$VERSION-linux-amd64" .
# the Windows build zipped too: browsers and chat apps are less quick to block a zipped .exe
(cd dist && zip -q "ClownsecBigTop-$VERSION-windows.zip" "ClownsecBigTop-$VERSION.exe")

# ---- the Linux AppImage: the AppImage runtime + a squashfs of the app ----
if command -v mksquashfs >/dev/null 2>&1; then
    mkdir -p .cache
    RT=.cache/runtime-x86_64
    [ -s "$RT" ] || curl -fsSL -o "$RT" https://github.com/AppImage/type2-runtime/releases/download/continuous/runtime-x86_64
    APPDIR=.cache/AppDir
    rm -rf "$APPDIR" && mkdir -p "$APPDIR/usr/bin"
    cp "dist/clownsec-bigtop-$VERSION-linux-amd64" "$APPDIR/usr/bin/clownsec-bigtop"
    ln -s usr/bin/clownsec-bigtop "$APPDIR/AppRun"
    cp assets/clownsec-bigtop.png "$APPDIR/clownsec-bigtop.png"
    ln -s clownsec-bigtop.png "$APPDIR/.DirIcon"
    cat > "$APPDIR/clownsec-bigtop.desktop" <<DESK
[Desktop Entry]
Type=Application
Name=Clownsec Big Top
Comment=Stream your moflex library to the Clownsec 3DS video player
Exec=clownsec-bigtop
Icon=clownsec-bigtop
Categories=AudioVideo;Video;Network;
Terminal=false
X-AppImage-Version=$VERSION
DESK
    rm -f .cache/app.squashfs
    mksquashfs "$APPDIR" .cache/app.squashfs -root-owned -noappend -comp gzip -quiet
    cat "$RT" .cache/app.squashfs > "dist/clownsec-bigtop-$VERSION-x86_64.AppImage"
    chmod +x "dist/clownsec-bigtop-$VERSION-x86_64.AppImage"
else
    echo "(no mksquashfs: skipping the AppImage)"
fi

# checksums: the updater won't install a download that doesn't match
(cd dist && for f in *; do [ "$f" = SHA256SUMS.txt ] || shasum -a 256 "$f"; done > SHA256SUMS.txt)
echo "built $VERSION:"; ls -la dist
