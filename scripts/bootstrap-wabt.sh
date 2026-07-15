#!/bin/sh
set -eu

version=1.0.41
base_url="https://github.com/WebAssembly/wabt/releases/download/$version"

case "$(uname -s):$(uname -m)" in
	Linux:x86_64)
		asset="wabt-$version-linux-x64.tar.gz"
		sha256="83f8122e924745fcd70636e3594bc01c4c47f2d4c8f3c63b5d70d3f83a482677"
		;;
	Linux:aarch64|Linux:arm64)
		asset="wabt-$version-linux-arm64.tar.gz"
		sha256="5e35416ee8725dc7cc0572e4392a8117cbf008b0e34c0db65c75506b0299cdbf"
		;;
	Darwin:arm64)
		asset="wabt-$version-macos-arm64.tar.gz"
		sha256="e5269d6bbe05dfeb179e4f21111b3a641d6ccaa38b0b21d472ae5c65f8c4ff5d"
		;;
	*)
		echo "bootstrap-wabt: unsupported host $(uname -s)/$(uname -m); supported: linux/amd64, linux/arm64, darwin/arm64" >&2
		exit 1
		;;
esac

repo=$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)
root="$repo/.tools/wabt-$version-${asset#wabt-$version-}"
root=${root%.tar.gz}
bin="$root/bin/wast2json"

verify() {
	[ -x "$bin" ] || return 1
	[ "$($bin --version 2>/dev/null)" = "$version" ] || return 1
}

if ! verify; then
	mkdir -p "$repo/.tools"
	tmp=$(mktemp -d "$repo/.tools/.wabt-$version.XXXXXX")
	trap 'rm -rf "$tmp"' EXIT HUP INT TERM
	archive="$tmp/$asset"
	curl -fsSL "$base_url/$asset" -o "$archive"
	printf '%s  %s\n' "$sha256" "$archive" | sha256sum -c - >/dev/null
	tar -xzf "$archive" -C "$tmp"
	extracted="$tmp/wabt-$version"
	[ -x "$extracted/bin/wast2json" ] || {
		echo "bootstrap-wabt: archive $asset did not contain wabt-$version/bin/wast2json" >&2
		exit 1
	}
	rm -rf "$root"
	mv "$extracted" "$root"
	verify || {
		echo "bootstrap-wabt: installed wast2json did not report pinned version $version" >&2
		exit 1
	}
fi

case "${1:-}" in
	""|--print-path)
		printf '%s\n' "$bin"
		;;
	--verify)
		printf 'wast2json %s (%s)\n' "$version" "$bin"
		;;
	*)
		echo "usage: $0 [--print-path|--verify]" >&2
		exit 2
		;;
esac
