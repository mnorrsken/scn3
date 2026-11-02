#!/bin/sh
# Builds dist/scn3-<arch>.AppImage from build/bin/scn3. The WebKitGTK libraries
# are not bundled: they must be installed on the system.
set -eu
arch=$(uname -m)
tools=$(mktemp -d)
url=https://github.com/linuxdeploy/linuxdeploy/releases/download/continuous
curl -fsSL -o "$tools/linuxdeploy" "$url/linuxdeploy-$arch.AppImage"
chmod +x "$tools/linuxdeploy"
appdir=$(mktemp -d)/AppDir
mkdir -p dist
# Runners and containers often have no FUSE.
export APPIMAGE_EXTRACT_AND_RUN=1
LDAI_OUTPUT="dist/scn3-$arch.AppImage" "$tools/linuxdeploy" \
	--appdir "$appdir" \
	--executable build/bin/scn3 \
	--desktop-file packaging/linux/scn3.desktop \
	--icon-file packaging/linux/scn3.png \
	--exclude-library 'libwebkit2gtk*' \
	--exclude-library 'libjavascriptcoregtk*' \
	--exclude-library 'libgtk-3*' \
	--exclude-library 'libgdk-3*' \
	--output appimage
