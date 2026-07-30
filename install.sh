#!/bin/sh
# wago installer.
#
#   curl -fsSL https://wago.sh/install.sh | sh
#
# The bootstrap installer builds the CLI and Standard runtime from the public
# repository. It requires git and Go 1.22+.
#
# Environment:
#   WAGO_VERSION   git ref to build: branch, tag, or commit (default: main)
#   WAGO_BIN_DIR   install directory (default: $HOME/.local/bin)
#   WAGO_DRY_RUN   set to 1 to print what would happen and exit
#   NO_COLOR       set to disable colored output
set -eu

repo_url="https://github.com/wago-org/wago.git"
version="${WAGO_VERSION:-main}"
bin_dir="${WAGO_BIN_DIR:-$HOME/.local/bin}"
# The wago source is kept here so `wago pkg add` can build plugins while wago is
# unpublished — the CLI looks for it at ~/.wago/src (see wagoModuleDir).
src_dir="${WAGO_SRC_DIR:-$HOME/.wago/src}"
dry_run="${WAGO_DRY_RUN:-0}"

if [ -n "${WAGO_HOME:-}" ]; then
	wago_data="$WAGO_HOME/data"
	wago_config="$WAGO_HOME/config"
elif [ "$(uname -s)" = "Darwin" ]; then
	wago_data="$HOME/.wago"
	wago_config="$HOME/.wago/config"
else
	wago_data="${XDG_DATA_HOME:-$HOME/.local/share}/wago"
	wago_config="${XDG_CONFIG_HOME:-$HOME/.config}/wago"
fi
runner_dir="$wago_data/versions/$version/standard"

# --- CLI-style output ------------------------------------------------------
is_tty=0
if [ -t 1 ] && [ "${TERM:-dumb}" != "dumb" ]; then
	is_tty=1
fi

if [ -z "${NO_COLOR:-}" ] && [ "$is_tty" = "1" ]; then
	e=$(printf '\033')
	cyan="${e}[36m"
	red="${e}[31m"
	dim="${e}[2m"
	bold="${e}[1m"
	reset="${e}[0m"
else
	cyan="" red="" dim="" bold="" reset=""
fi

spinner_pid=""
spinner_label=""
tmp=""

stop_spinner() {
	if [ -n "$spinner_pid" ]; then
		kill "$spinner_pid" >/dev/null 2>&1 || true
		wait "$spinner_pid" 2>/dev/null || true
		spinner_pid=""
	fi
}

progress_begin() {
	spinner_label=$*
	stop_spinner
	if [ "$is_tty" = "1" ]; then
		(
			trap 'exit 0' HUP INT TERM
			while :; do
				for frame in ⠋ ⠙ ⠹ ⠸ ⠼ ⠴ ⠦ ⠧ ⠇ ⠏; do
					printf '\r\033[2K%s%s%s %s' "$dim" "$frame" "$reset" "$spinner_label"
					sleep 0.08
				done
			done
		) &
		spinner_pid=$!
	else
		printf '… %s\n' "$spinner_label"
	fi
}

progress_done() {
	stop_spinner
	if [ "$is_tty" = "1" ]; then
		printf '\r\033[2K%s✓%s %s' "$cyan" "$reset" "$*"
	else
		printf '%s✓%s %s\n' "$cyan" "$reset" "$*"
	fi
}

progress_finish() {
	stop_spinner
	if [ "$is_tty" = "1" ]; then
		printf '\r\033[2K'
	fi
	printf '%s✓%s %s\n' "$cyan" "$reset" "$*"
}

progress_fail() {
	stop_spinner
	if [ "$is_tty" = "1" ]; then
		printf '\r\033[2K'
	fi
	printf '%s✗%s %s\n' "$red" "$reset" "$*" >&2
}

detail() { printf '  %s%-12s%s %s\n' "$dim" "$1" "$reset" "$2"; }
die() {
	stop_spinner
	printf '%swago:%s %s\n' "$red" "$reset" "$*" >&2
	exit 1
}

have() { command -v "$1" >/dev/null 2>&1; }

display_path() {
	case "$1" in
		"$HOME") printf '~' ;;
		"$HOME"/*) printf '~/%s' "${1#"$HOME"/}" ;;
		*) printf '%s' "$1" ;;
	esac
}

cleanup() {
	stop_spinner
	if [ -n "$tmp" ] && [ -d "$tmp" ]; then
		rm -rf "$tmp"
	fi
}
trap cleanup EXIT
trap 'exit 130' HUP INT TERM

go_version_ok() {
	v=$(go env GOVERSION 2>/dev/null || go version | awk '{print $3}')
	v=${v#go}
	major=${v%%.*}
	rest=${v#*.}
	minor=${rest%%[!0-9]*}
	case "$major:$minor" in
		*[!0-9:]*|:|*:|"") return 1 ;;
	esac
	[ "$major" -gt 1 ] || { [ "$major" -eq 1 ] && [ "$minor" -ge 22 ]; }
}

printf '%sSetting Up%s\n' "$bold" "$reset"

if [ "$dry_run" = "1" ]; then
	detail "version" "$version"
	detail "profile" "standard"
	detail "manager" "$(display_path "$bin_dir/wago")"
	detail "runtime" "$(display_path "$runner_dir/wago-runtime")"
	detail "source" "$(display_path "$src_dir")"
	printf '%sNo changes made.%s\n' "$dim" "$reset"
	exit 0
fi

have git || die "git is required to install wago"

# Source build needs the Go toolchain.
progress_begin "checking Go toolchain"
if have go && go_version_ok; then
	progress_done "Go toolchain ready"
else
	progress_fail "Go 1.22 or newer is required"
	die "install Go 1.22+ and run the installer again"
fi

tmp=$(mktemp -d 2>/dev/null || mktemp -d -t wago)

# Shallow-clone the requested ref. --branch handles branches and tags; a raw
# commit sha falls back to a full clone + checkout.
progress_begin "fetching Wago source"
if git clone --depth 1 --branch "$version" "$repo_url" "$tmp/src" >"$tmp/git.log" 2>&1; then
	progress_done "fetched Wago source"
else
	rm -rf "$tmp/src"
	if git clone "$repo_url" "$tmp/src" >>"$tmp/git.log" 2>&1 &&
		git -C "$tmp/src" checkout -q "$version" >>"$tmp/git.log" 2>&1; then
		progress_done "fetched Wago source"
	else
		progress_fail "source fetch failed"
		tail -n 20 "$tmp/git.log" >&2 || true
		die "could not fetch $repo_url at $version"
	fi
fi

# No plugins are bundled: wago builds plugin-free (stdlib-only, so this builds
# offline with no module downloads).
stamp=$(git -C "$tmp/src" describe --tags --always 2>/dev/null || echo "$version")
progress_begin "building Wago manager"
if (cd "$tmp/src" &&
	CGO_ENABLED=0 go build -trimpath -tags wago_manager \
		-ldflags "-s -w -X main.version=$stamp" -o "$tmp/wago" ./cli/wago) >"$tmp/manager.log" 2>&1; then
	progress_done "built Wago manager"
else
	progress_fail "manager build failed"
	tail -n 20 "$tmp/manager.log" >&2 || true
	die "could not build Wago manager"
fi

progress_begin "building Standard runtime"
if (cd "$tmp/src" &&
	CGO_ENABLED=0 go build -trimpath \
		-ldflags "-s -w -X main.version=$stamp" -o "$tmp/wago-runtime" ./cli/wago) >"$tmp/runtime.log" 2>&1; then
	progress_done "built Standard runtime"
else
	progress_fail "runtime build failed"
	tail -n 20 "$tmp/runtime.log" >&2 || true
	die "could not build Standard runtime"
fi

runner_dir="$wago_data/versions/$stamp/standard"

progress_begin "installing Wago"
if mkdir -p "$bin_dir" "$runner_dir" "$wago_config" &&
	mv "$tmp/wago" "$bin_dir/wago" &&
	mv "$tmp/wago-runtime" "$runner_dir/wago-runtime" &&
	printf '%s\n' "$stamp" >"$wago_config/active-version" &&
	printf '%s\n' "standard" >"$wago_config/active-profile"; then
	progress_done "installed CLI and Standard runtime"
else
	progress_fail "installation failed"
	die "could not install Wago"
fi

# Keep the source so `wago pkg add <module> && wago pkg build` can compile a
# custom binary with plugins (wago is unpublished, so builds need it; the CLI
# finds it at ~/.wago/src). Swapped in only after a successful build.
progress_begin "saving Wago source"
source_backup="$tmp/source-backup"
if mkdir -p "$(dirname "$src_dir")" &&
	{ [ ! -e "$src_dir" ] || mv "$src_dir" "$source_backup"; } &&
	mv "$tmp/src" "$src_dir"; then
	progress_done "saved Wago source"
else
	[ ! -e "$source_backup" ] || mv "$source_backup" "$src_dir" 2>/dev/null || true
	progress_fail "could not save Wago source"
	die "installation is usable, but its source could not be saved"
fi

progress_begin "verifying installation"
if "$bin_dir/wago" --version >/dev/null 2>&1; then
	progress_done "verified installation"
else
	progress_fail "verification failed"
	die "the installed Wago command did not start"
fi

progress_finish "Installed Wago $stamp (standard)"
detail "manager" "$(display_path "$bin_dir/wago")"
detail "runtime" "$(display_path "$runner_dir/wago-runtime")"

case ":$PATH:" in
	*":$bin_dir:"*) ;;
	*)
		printf '\n%sNext step%s\n' "$bold" "$reset"
		printf '  Add %s to PATH, then run %swago%s.\n' "$(display_path "$bin_dir")" "$cyan" "$reset"
		;;
esac
