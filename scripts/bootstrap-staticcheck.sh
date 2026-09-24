#!/bin/sh
set -eu

version=2024.1.1
repo=$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)
root="$repo/.tools/staticcheck"
bin="$root/staticcheck"
stamp="$root/build-provenance"

sha256_file() {
	if command -v sha256sum >/dev/null 2>&1; then
		sha256sum "$1" | awk '{ print $1 }'
	else
		shasum -a 256 "$1" | awk '{ print $1 }'
	fi
}

expected_provenance() {
	printf 'module=honnef.co/go/tools/cmd/staticcheck@%s\ngo=%s\nbootstrap_sha256=%s\nbinary_sha256=%s' \
		"$version" "$(go version)" "$(sha256_file "$0")" "$(sha256_file "$bin")"
}

verify() {
	[ -x "$bin" ] || return 1
	case "$($bin -version 2>/dev/null)" in
		"staticcheck $version "*) ;;
		*) return 1 ;;
	esac
	[ -f "$stamp" ] || return 1
	[ "$(cat "$stamp")" = "$(expected_provenance)" ]
}

case "${1:-}" in
	""|--print-path|--verify) ;;
	*)
		echo "usage: $0 [--print-path|--verify]" >&2
		exit 2
		;;
esac

if ! verify; then
	mkdir -p "$repo/.tools"
	tmp=$(mktemp -d "$repo/.tools/.staticcheck.XXXXXX")
	trap 'rm -rf "$tmp"' EXIT HUP INT TERM
	GOBIN="$tmp" go install "honnef.co/go/tools/cmd/staticcheck@$version"
	[ -x "$tmp/staticcheck" ] || {
		echo "bootstrap-staticcheck: go install did not produce staticcheck $version" >&2
		exit 1
	}
	bin="$tmp/staticcheck"
	expected_provenance >"$tmp/build-provenance"
	bin="$root/staticcheck"
	rm -rf "$root"
	mv "$tmp" "$root"
	trap - EXIT HUP INT TERM
	verify || {
		echo "bootstrap-staticcheck: installed tool failed provenance verification" >&2
		exit 1
	}
fi

case "${1:-}" in
	""|--print-path) printf '%s\n' "$bin" ;;
	--verify) printf 'Staticcheck %s (%s)\n' "$version" "$bin" ;;
esac
