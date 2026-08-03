#!/bin/sh
set -eu

version=1.0.41
base_url="https://github.com/WebAssembly/wabt/releases/download/$version"
source_build=false

case "$(uname -s):$(uname -m)" in
	Linux:x86_64)
		platform="linux-x64"
		asset="wabt-$version-linux-x64.tar.gz"
		sha256="83f8122e924745fcd70636e3594bc01c4c47f2d4c8f3c63b5d70d3f83a482677"
		;;
	Linux:aarch64|Linux:arm64)
		platform="linux-arm64"
		asset="wabt-$version-linux-arm64.tar.gz"
		sha256="5e35416ee8725dc7cc0572e4392a8117cbf008b0e34c0db65c75506b0299cdbf"
		;;
	Darwin:arm64)
		platform="macos-arm64"
		asset="wabt-$version-macos-arm64.tar.gz"
		sha256="e5269d6bbe05dfeb179e4f21111b3a641d6ccaa38b0b21d472ae5c65f8c4ff5d"
		;;
	Darwin:x86_64)
		# Upstream does not publish a macOS x86_64 binary for 1.0.41. Build the
		# checksummed release source rather than substituting an unpinned tool.
		platform="macos-x64"
		asset="wabt-$version.tar.xz"
		sha256="ca9e69cc1de13b4633a3c74fd697319303b21108529d4f10960af4e1f4a65893"
		source_build=true
		;;
	*)
		echo "bootstrap-wabt: unsupported host $(uname -s)/$(uname -m); supported: linux/amd64, linux/arm64, darwin/amd64, darwin/arm64" >&2
		exit 1
		;;
esac

repo=$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)
root="$repo/.tools/wabt-$version-$platform"
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
	if command -v sha256sum >/dev/null 2>&1; then
		printf '%s  %s\n' "$sha256" "$archive" | sha256sum -c - >/dev/null
	else
		printf '%s  %s\n' "$sha256" "$archive" | shasum -a 256 -c - >/dev/null
	fi
	extracted="$tmp/wabt-$version"
	if [ "$source_build" = true ]; then
		tar -xJf "$archive" -C "$tmp"
		rm -rf "$root"
		cmake -S "$extracted" -B "$tmp/build" -DBUILD_TESTS=OFF -DCMAKE_BUILD_TYPE=Release -DCMAKE_INSTALL_PREFIX="$root"
		cmake --build "$tmp/build" --parallel
		cmake --install "$tmp/build"
	else
		tar -xzf "$archive" -C "$tmp"
		[ -x "$extracted/bin/wast2json" ] || {
			echo "bootstrap-wabt: archive $asset did not contain wabt-$version/bin/wast2json" >&2
			exit 1
		}
		rm -rf "$root"
		mv "$extracted" "$root"
	fi
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
