#!/bin/sh
set -eu

revision=9d36019973201a19f9c9ebb0f10828b2fe2374aa
repo=$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)
suite="$repo/tests/conformance/spec-v3"
source_dir="$suite/interpreter"
root="$repo/.tools/spec-interpreter-$revision"
bin="$root/wasm"
stamp="$root/source-revision"
provenance="$root/build-provenance"

actual_revision() {
	[ -e "$suite/.git" ] || return 0
	git -C "$suite" rev-parse HEAD 2>/dev/null || true
}

require_source() {
	actual=$(actual_revision)
	if [ "$actual" != "$revision" ]; then
		echo "bootstrap-spec-interpreter: tests/conformance/spec-v3 revision ${actual:-unavailable}, want $revision" >&2
		echo "bootstrap-spec-interpreter: initialize the pinned submodule with: git submodule update --init tests/conformance/spec-v3" >&2
		exit 1
	fi
	[ -f "$source_dir/dune-project" ] || {
		echo "bootstrap-spec-interpreter: pinned interpreter source is unavailable under $source_dir" >&2
		exit 1
	}
}

verify() {
	[ -x "$bin" ] || return 1
	[ -f "$stamp" ] || return 1
	[ "$(cat "$stamp")" = "$revision" ] || return 1
	[ "$("$bin" -v --help 2>/dev/null | sed -n '1p')" = "wasm 3.0.0 reference interpreter" ] || return 1
	[ -f "$provenance" ] || return 1
	[ "$(cat "$provenance")" = "$(expected_provenance)" ] || return 1
}

sha256_file() {
	if command -v sha256sum >/dev/null 2>&1; then
		sha256sum "$1" | awk '{ print $1 }'
	else
		shasum -a 256 "$1" | awk '{ print $1 }'
	fi
}

toolchain_versions() {
	command -v ocamlc >/dev/null 2>&1 || return 1
	command -v dune >/dev/null 2>&1 || return 1
	command -v menhir >/dev/null 2>&1 || return 1
	ocamlc -version
	dune --version
	menhir --version 2>&1 | sed -n '1p'
}

expected_provenance() {
	versions=$(toolchain_versions) || return 1
	ocaml=$(printf '%s\n' "$versions" | sed -n '1p')
	dune=$(printf '%s\n' "$versions" | sed -n '2p')
	menhir=$(printf '%s\n' "$versions" | sed -n '3p')
	printf 'revision=%s\nbootstrap_sha256=%s\nocaml=%s\ndune=%s\nmenhir=%s\nbinary_sha256=%s' \
		"$revision" "$(sha256_file "$0")" "$ocaml" "$dune" "$menhir" "$(sha256_file "$bin")"
}

build() {
	command -v dune >/dev/null 2>&1 || {
		echo "bootstrap-spec-interpreter: dune is required to build the official Release 3 interpreter" >&2
		exit 1
	}
	command -v menhir >/dev/null 2>&1 || {
		echo "bootstrap-spec-interpreter: menhir is required to build the official Release 3 interpreter" >&2
		exit 1
	}
	# --print-path is consumed by command substitution in CI; keep build chatter
	# on stderr so stdout remains the single machine-readable path.
	make -C "$source_dir" wasm >&2
	[ -x "$source_dir/wasm" ] || {
		echo "bootstrap-spec-interpreter: build did not produce $source_dir/wasm" >&2
		exit 1
	}
	tmp=$(mktemp -d "$repo/.tools/.spec-interpreter.XXXXXX")
	trap 'rm -rf "$tmp"' EXIT HUP INT TERM
	cp "$source_dir/wasm" "$tmp/wasm"
	chmod 755 "$tmp/wasm"
	printf '%s\n' "$revision" >"$tmp/source-revision"
	# Stamp the source, recipe, native toolchain, and actual interpreter bytes.
	# The stamp is checked on every cache hit before the tool can be used.
	old_root="$root"
	root="$tmp"
	bin="$tmp/wasm"
	expected_provenance >"$tmp/build-provenance"
	root="$old_root"
	bin="$root/wasm"
	rm -rf "$root"
	mv "$tmp" "$root"
	trap - EXIT HUP INT TERM
	verify || {
		echo "bootstrap-spec-interpreter: installed interpreter failed verification" >&2
		exit 1
	}
}

case "${1:-}" in
	--print-revision)
		# Reporting the declared source pin must not require an initialized
		# submodule; callers use this mode to decide what to initialize.
		printf '%s\n' "$revision"
		exit 0
		;;
	""|--print-path|--verify)
		;;
	*)
		echo "usage: $0 [--print-path|--print-revision|--verify]" >&2
		exit 2
		;;
esac

require_source

if ! verify; then
	mkdir -p "$repo/.tools"
	build
fi

case "${1:-}" in
	""|--print-path)
		printf '%s\n' "$bin"
		;;
	--verify)
		printf 'WebAssembly/spec interpreter %s (%s)\n' "$revision" "$bin"
		;;
esac
