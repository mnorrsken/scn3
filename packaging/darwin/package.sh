#!/bin/sh
# Turns build/bin/scn3.app into dist/scn3-<version>.dmg.
# Signs when SIGN_IDENTITY is set and notarises when APPLE_ID, APPLE_PASSWORD
# and APPLE_TEAM_ID are set too. Without them it makes an unsigned dmg.
set -eu
version=${1:?usage: package.sh <version>}
app=build/bin/scn3.app
dmg=dist/scn3-$version.dmg
mkdir -p dist

if [ -n "${SIGN_IDENTITY:-}" ]; then
	codesign --force --deep --options runtime --timestamp --sign "$SIGN_IDENTITY" "$app"
fi

stage=$(mktemp -d)
cp -R "$app" "$stage/"
ln -s /Applications "$stage/Applications"
rm -f "$dmg"
hdiutil create -quiet -volname scn3 -srcfolder "$stage" -ov -format UDZO "$dmg"
rm -rf "$stage"

if [ -n "${SIGN_IDENTITY:-}" ]; then
	codesign --force --timestamp --sign "$SIGN_IDENTITY" "$dmg"
	if [ -n "${APPLE_ID:-}" ]; then
		xcrun notarytool submit "$dmg" --wait \
			--apple-id "$APPLE_ID" --password "$APPLE_PASSWORD" --team-id "$APPLE_TEAM_ID"
		xcrun stapler staple "$dmg"
	fi
fi
