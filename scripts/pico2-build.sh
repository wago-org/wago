#!/bin/sh
# Build the Pico 2 firmware with the repository's native 32-bit boundary.
set -eu

root=$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)
tinygo_cmd=${TINYGO:-tinygo}
tinygo_root=${TINYGOROOT:-$($tinygo_cmd env TINYGOROOT)}
arch=arm
if [ "${1:-}" = arm ] || [ "${1:-}" = riscv ]; then
	arch=$1
	shift
fi
output=${1:-$root/build/wago-pico2-$arch.uf2}

case "$tinygo_root" in
	/*) ;;
	*)
		echo "TINYGOROOT must be an absolute directory" >&2
		exit 1
		;;
esac
if [ ! -d "$tinygo_root/targets" ]; then
	echo "TINYGOROOT does not contain TinyGo targets: $tinygo_root" >&2
	exit 1
fi

mkdir -p "$(dirname -- "$output")"
target=$(mktemp "${TMPDIR:-/tmp}/wago-pico2-$arch-target.XXXXXX.json")
trap 'rm -f "$target"' EXIT HUP INT TERM
case "$arch" in
	arm)
		base_target=pico2
		native_source=$root/firmware/pico2/native_arm.S
		;;
	riscv)
		base_target=pico2-riscv
		native_source=$root/firmware/pico2/native_riscv.S
		if [ ! -f "$tinygo_root/targets/$base_target.json" ]; then
			echo "TinyGo target $base_target is not installed in $tinygo_root" >&2
			exit 1
		fi
		;;
esac
native=$(realpath --relative-to="$tinygo_root" "$native_source")
printf '{\n  "inherits": ["%s"],\n  "extra-files": ["%s"]\n}\n' "$base_target" "$native" >"$target"

"$tinygo_cmd" build -target="$target" -o "$output" "$root/firmware/pico2"
