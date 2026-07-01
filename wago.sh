#!/bin/sh
# wago installer — fetches a prebuilt binary from GitHub Releases.
#
#   curl -fsSL https://wago.sh/install.sh | sh
#
# Environment:
#   WAGO_VERSION      release tag to install: "latest" (default), "nightly",
#                     or an explicit tag like "v0.1.0". "latest" falls back to
#                     the nightly build until a stable release exists.
#   WAGO_BIN_DIR      install directory (default: $HOME/.local/bin)
#   WAGO_FROM_SOURCE  set to 1 to build from source with `go install` instead
#   WAGO_DRY_RUN      set to 1 to print what would happen and exit
#   NO_COLOR          set to disable colored output
set -eu

repo="wago-org/wago"
asset="wago-linux-amd64"
version="${WAGO_VERSION:-latest}"
bin_dir="${WAGO_BIN_DIR:-$HOME/.local/bin}"
from_source="${WAGO_FROM_SOURCE:-0}"
dry_run="${WAGO_DRY_RUN:-0}"

# --- pretty output (the wago.sh "sparkle" palette) -------------------------
if [ -z "${NO_COLOR:-}" ] && [ -t 1 ] && [ "${TERM:-dumb}" != "dumb" ]; then
	e=$(printf '\033')
	lilac="${e}[38;2;195;168;255m"
	green="${e}[38;2;116;224;173m"
	pink="${e}[38;2;255;158;196m"
	dim="${e}[38;2;125;114;176m"
	bold="${e}[1m"
	reset="${e}[0m"
else
	lilac="" green="" pink="" dim="" bold="" reset=""
fi

banner() {
	printf '%s' "$lilac"
	printf '  ╦ ╦ ╔═╗ ╔═╗ ╔═╗\n'
	printf '  ║║║ ╠═╣ ║ ╦ ║ ║\n'
	printf '  ╚╩╝ ╩ ╩ ╚═╝ ╚═╝%s\n' "$reset"
	printf '  %sa pure-Go WebAssembly JIT%s\n\n' "$dim" "$reset"
}

step() { printf '%s▸%s %s\n' "$lilac" "$reset" "$*"; }
ok() { printf '  %s✓%s %s\n' "$green" "$reset" "$*"; }
info() { printf '  %s%s%s\n' "$dim" "$*" "$reset"; }
die() {
	printf '%s✗ wago:%s %s\n' "$pink" "$reset" "$*" >&2
	exit 1
}

have() { command -v "$1" >/dev/null 2>&1; }

# --- download helpers ------------------------------------------------------
if have curl; then
	fetch() { curl -fsSL "$1"; }
	download() { curl -fsSL -o "$2" "$1"; }
elif have wget; then
	fetch() { wget -qO- "$1"; }
	download() { wget -qO "$2" "$1"; }
else
	fetch() { die "need curl or wget"; }
	download() { die "need curl or wget"; }
fi

# Resolve WAGO_VERSION to a concrete release tag.
resolve_tag() {
	case "$version" in
	nightly) printf 'nightly' ;;
	latest | "")
		# Newest stable release, or the rolling nightly if there are none yet.
		tag=$(fetch "https://api.github.com/repos/$repo/releases/latest" 2>/dev/null |
			sed -n 's/.*"tag_name"[[:space:]]*:[[:space:]]*"\([^"]*\)".*/\1/p' | head -n1)
		[ -n "$tag" ] && printf '%s' "$tag" || printf 'nightly'
		;;
	*) printf '%s' "$version" ;;
	esac
}

install_prebuilt() {
	tag=$(resolve_tag)
	base="https://github.com/$repo/releases/download/$tag"

	step "installing wago ${bold}$tag${reset} (linux/amd64)"
	if [ "$dry_run" = "1" ]; then
		info "dry run: download $base/$asset -> $bin_dir/wago"
		exit 0
	fi

	tmp=$(mktemp -d 2>/dev/null || mktemp -d -t wago)
	trap 'rm -rf "$tmp"' EXIT

	download "$base/$asset" "$tmp/wago" ||
		die "could not download $base/$asset (try WAGO_FROM_SOURCE=1 to build from source)"

	# Verify the checksum when sha256sum and the .sha256 asset are both available.
	if have sha256sum && download "$base/$asset.sha256" "$tmp/wago.sha256" 2>/dev/null; then
		expected=$(awk '{print $1}' "$tmp/wago.sha256")
		actual=$(sha256sum "$tmp/wago" | awk '{print $1}')
		[ "$expected" = "$actual" ] || die "checksum mismatch (expected $expected, got $actual)"
		ok "checksum verified"
	fi

	mkdir -p "$bin_dir"
	chmod +x "$tmp/wago"
	mv "$tmp/wago" "$bin_dir/wago" || die "could not install to $bin_dir (set WAGO_BIN_DIR to a writable path)"
	ok "installed $bin_dir/wago"
}

install_from_source() {
	have go || die "Go 1.22+ is required to build from source (install Go, or use a prebuilt binary on linux/amd64)"
	pkg="github.com/$repo/cli/wago@$version"
	step "building wago from ${bold}$pkg${reset}"
	if [ "$dry_run" = "1" ]; then
		info "dry run: GOBIN=$bin_dir go install $pkg"
		exit 0
	fi
	mkdir -p "$bin_dir"
	GOBIN="$bin_dir" go install "$pkg"
	ok "installed $bin_dir/wago"
}

banner

os=$(uname -s 2>/dev/null || echo unknown)
arch=$(uname -m 2>/dev/null || echo unknown)

# Prebuilt binaries are linux/amd64 only; everything else builds from source.
if [ "$from_source" != "1" ] && [ "$os" = "Linux" ] && { [ "$arch" = "x86_64" ] || [ "$arch" = "amd64" ]; }; then
	install_prebuilt
else
	[ "$from_source" = "1" ] || info "no prebuilt binary for $os/$arch — building from source"
	install_from_source
fi

# Confirm the install and nudge about PATH.
if [ -x "$bin_dir/wago" ]; then
	"$bin_dir/wago" version || true
fi
case ":$PATH:" in
*":$bin_dir:"*) ;;
*) info "add $bin_dir to your PATH to run: wago" ;;
esac
