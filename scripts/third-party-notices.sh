#!/bin/sh
# Prints the license of every module built into scn3 and scan, on every OS we
# ship for, plus Go's own. MIT and BSD licenses require this text to ship with
# the binaries. Usage: scripts/third-party-notices.sh > THIRD_PARTY_NOTICES.txt
set -eu
modules=$(for os in darwin linux windows; do
	GOOS=$os CGO_ENABLED=1 go list -tags desktop,production,webkit2_41 -deps \
		-f '{{if .Module}}{{.Module.Path}} {{.Module.Version}}{{end}}' . ./cmd/...
done | grep -v '^github.com/mnorrsken/scn3 ' | sort -u)

echo "scn3 includes the following third-party software."
echo
print_license() {
	file=$(ls "$2" | grep -i -m1 -E '^(license|licence|copying)') || {
		echo "no license file found for $1" >&2
		exit 1
	}
	printf '%s\n%s\n\n' "================================================================" "$1"
	cat "$2/$file"
	echo
}
print_license "Go $(go env GOVERSION)" "$(go env GOROOT)"
echo "$modules" | while read -r path version; do
	print_license "$path $version" "$(go list -m -f '{{.Dir}}' "$path")"
done
