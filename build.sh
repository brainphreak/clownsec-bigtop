#!/bin/sh
# Builds Clownsec Big Top for Windows, macOS and Linux into dist/, the version in every file name
# (and shown in the server's window and setup page) so a copy says which build it is:
#   ClownsecBigTop-<v>.exe (+ -windows.zip), clownsec-bigtop-<v>-macos-arm64 / -macos-intel .command (+ .zip),
#   clownsec-bigtop-<v>-linux-amd64, clownsec-bigtop-<v>-x86_64.AppImage, SHA256SUMS.txt
# The AppImage needs mksquashfs (squashfs-tools; on macOS: brew install squashfs) and downloads
# the AppImage runtime once into .cache/. The updater checks every download against SHA256SUMS.txt.
set -e
cd "$(dirname "$0")"
export GOCACHE="$PWD/.gocache" CGO_ENABLED=0
VERSION=${VERSION:-$(date +%y%m%d.%H%M)}
# symbols kept (no -s -w): antivirus software treats stripped Go binaries as suspicious
LDFLAGS="-X main.version=$VERSION"
# The live moflex encoder (Old 3DS MP4/MKV streaming) is built into every Big Top: Mobipeg-MOFLEX by
# Movie Bot + the clownsec patch, cross-built by clownsec-moflex-encoder/tools/build_encoder_cross.sh.
# Each platform's build embeds its own (internal/media/embed_*.go); a missing one stops the build.
ENC=${MOFLEX_ENCODERS:-$HOME/moflex-encoder/dist/encoders}
for f in darwin_arm64/moflex-ffmpeg darwin_amd64/moflex-ffmpeg linux_amd64/moflex-ffmpeg windows_amd64/moflex-ffmpeg.exe; do
    [ -s "$ENC/$f" ] || { echo "missing encoder: $ENC/$f (set MOFLEX_ENCODERS)"; exit 1; }
done
rm -rf internal/media/encoder && mkdir -p internal/media/encoder && cp -R "$ENC/." internal/media/encoder/
TAGS="-tags moflexenc"
rm -rf dist && mkdir -p dist
# Windows: a tray app (-H windowsgui: no console window; -console brings one back)
GOOS=windows GOARCH=amd64 go build $TAGS -trimpath -ldflags "$LDFLAGS -H windowsgui" -o "dist/ClownsecBigTop-$VERSION.exe" .
# macOS: .command, so Finder opens it in Terminal on a double-click (without an extension it takes the
# version's dot for one and calls the file a document). Zipped too: unzipping keeps it executable,
# a browser download doesn't. The plain-named copies are for the updaters in 261114.0325/.0329.
for a in arm64:arm64 amd64:intel; do
    GOOS=darwin GOARCH=${a%%:*} go build $TAGS -trimpath -ldflags "$LDFLAGS" -o "dist/clownsec-bigtop-$VERSION-macos-${a##*:}.command" .
    cp "dist/clownsec-bigtop-$VERSION-macos-${a##*:}.command" "dist/clownsec-bigtop-$VERSION-macos-${a##*:}"
    (cd dist && zip -q "clownsec-bigtop-$VERSION-macos-${a##*:}.zip" "clownsec-bigtop-$VERSION-macos-${a##*:}.command")
done
GOOS=linux   GOARCH=amd64 go build $TAGS -trimpath -ldflags "$LDFLAGS" -o "dist/clownsec-bigtop-$VERSION-linux-amd64" .
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
