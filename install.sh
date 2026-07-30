#!/bin/sh
# wago installer.
#
#   curl -fsSL https://wago.sh/install.sh | sh
#
# The bootstrap installer builds the CLI and Standard runtime from the public
# repository. It requires Go 1.22+; Git is preferred, with a zip fallback.
#
# Environment:
#   WAGO_VERSION   git ref to build: branch, tag, or commit (default: main)
#   WAGO_BIN_DIR   install directory (default: $HOME/.wago/bin)
#   WAGO_DRY_RUN   set to 1 to print what would happen and exit
#   WAGO_NO_MODIFY_PATH set to 1 to never offer to edit shell startup files
#   NO_COLOR       set to disable colored output
set -eu

repo_url="${WAGO_REPO_URL:-https://github.com/wago-org/wago.git}"
version="${WAGO_VERSION:-main}"
archive_url="${WAGO_ARCHIVE_URL:-https://api.github.com/repos/wago-org/wago/zipball/$version}"
bin_dir="${WAGO_BIN_DIR:-$HOME/.wago/bin}"
# The wago source is kept here so `wago pkg add` can build plugins while wago is
# unpublished — the CLI looks for it at ~/.wago/src (see wagoModuleDir).
src_dir="${WAGO_SRC_DIR:-$HOME/.wago/src}"
dry_run="${WAGO_DRY_RUN:-0}"
no_modify_path="${WAGO_NO_MODIFY_PATH:-0}"

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

progress_retry() {
	stop_spinner
	if [ "$is_tty" = "1" ]; then
		printf '\r\033[2K'
	fi
	printf '%s→%s %s\n' "$dim" "$reset" "$*"
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

shell_config_file() {
	case "$1" in
		zsh)
			printf '%s/.zshrc' "${ZDOTDIR:-$HOME}"
			;;
		bash)
			if [ "$(uname -s)" = "Darwin" ]; then
				if [ -e "$HOME/.bash_profile" ] || [ ! -e "$HOME/.bashrc" ]; then
					printf '%s/.bash_profile' "$HOME"
				else
					printf '%s/.bashrc' "$HOME"
				fi
			else
				printf '%s/.bashrc' "$HOME"
			fi
			;;
		fish)
			printf '%s/fish/config.fish' "${XDG_CONFIG_HOME:-$HOME/.config}"
			;;
		nu)
			printf '%s/nushell/env.nu' "${XDG_CONFIG_HOME:-$HOME/.config}"
			;;
		*)
			return 1
			;;
	esac
}

path_option_add() {
	shell_name=$1
	[ "$shell_name" = "$current_shell" ] || have "$shell_name" || return 0
	case "$path_shells" in
		*"
$shell_name|"*|"$shell_name|"*) return 0 ;;
	esac
	config_file=$(shell_config_file "$shell_name") || return 0
	path_option_count=$((path_option_count + 1))
	path_shells="${path_shells}${shell_name}|${config_file}
"
}

shell_single_quote() {
	printf '%s' "$1" | sed "s/'/'\\\\''/g"
}

add_path_to_config() {
	shell_name=$1
	config_file=$2
	marker="# Wago PATH: $bin_dir"
	if [ -f "$config_file" ] && grep -F "$marker" "$config_file" >/dev/null 2>&1; then
		printf '%s✓%s Wago is already configured in %s\n' "$cyan" "$reset" "$(display_path "$config_file")"
		return 0
	fi
	if ! mkdir -p "$(dirname "$config_file")"; then
		return 1
	fi
	quoted_bin=$(shell_single_quote "$bin_dir")
	if ! {
		[ ! -s "$config_file" ] || printf '\n'
		printf '%s\n' "$marker"
		case "$shell_name" in
			fish) printf "fish_add_path --path '%s'\n" "$quoted_bin" ;;
			nu) printf "\$env.PATH = (\$env.PATH | prepend '%s')\n" "$quoted_bin" ;;
			*) printf "export PATH='%s':\"\$PATH\"\n" "$quoted_bin" ;;
		esac
	} >>"$config_file"; then
		return 1
	fi
	printf '%s✓%s Added Wago to PATH in %s\n' "$cyan" "$reset" "$(display_path "$config_file")"
	printf '%sOpen a new shell to use wago.%s\n' "$dim" "$reset"
}

offer_path_setup() {
	case ":$PATH:" in
		*":$bin_dir:"*) return 0 ;;
	esac
	if [ "$no_modify_path" = "1" ]; then
		return 1
	fi
	if [ "$is_tty" != "1" ] && [ "${WAGO_INTERNAL_PATH_SETUP_ONLY:-0}" != "1" ]; then
		return 1
	fi
	install_tty="${WAGO_INSTALL_TTY:-/dev/tty}"
	if [ ! -r "$install_tty" ]; then
		return 1
	fi

	current_shell=${SHELL##*/}
	path_shells=""
	path_option_count=0
	path_option_add "$current_shell"
	for shell_name in zsh bash fish nu; do
		path_option_add "$shell_name"
	done
	if [ "$path_option_count" -eq 0 ]; then
		return 1
	fi

	printf '\n%sAdd Wago to your PATH?%s\n' "$bold" "$reset"
	option_index=0
	printf '%s' "$path_shells" | while IFS='|' read -r shell_name config_file; do
		[ -n "$shell_name" ] || continue
		option_index=$((option_index + 1))
		current=""
		[ "$shell_name" != "$current_shell" ] || current=" ${dim}current${reset}"
		printf '  %d) %-8s %s%s\n' "$option_index" "$shell_name" "$(display_path "$config_file")" "$current"
	done
	skip_option=$((path_option_count + 1))
	printf '  %d) Not now\n' "$skip_option"
	printf 'Choose [1-%d] (default 1): ' "$skip_option"
	choice=""
	IFS= read -r choice <"$install_tty" || true
	[ -n "$choice" ] || choice=1
	case "$choice" in
		*[!0-9]*|"") return 1 ;;
	esac
	if [ "$choice" -lt 1 ] || [ "$choice" -gt "$path_option_count" ]; then
		return 1
	fi
	selected=$(printf '%s' "$path_shells" | sed -n "${choice}p")
	shell_name=${selected%%|*}
	config_file=${selected#*|}
	add_path_to_config "$shell_name" "$config_file"
}

run_with_timeout() {
	timeout_seconds=$1
	shift
	"$@" &
	command_pid=$!
	(
		sleep "$timeout_seconds"
		kill -TERM "$command_pid" >/dev/null 2>&1 || exit 0
		sleep 1
		kill -KILL "$command_pid" >/dev/null 2>&1 || true
	) &
	timer_pid=$!
	status=0
	wait "$command_pid" || status=$?
	kill "$timer_pid" >/dev/null 2>&1 || true
	wait "$timer_pid" 2>/dev/null || true
	return "$status"
}

verify_installation() {
	verify_timeout="${WAGO_VERIFY_TIMEOUT:-10}"
	run_with_timeout "$verify_timeout" "$bin_dir/wago" self --help >/dev/null 2>&1
}

fetch_source_with_git() {
	have git || return 1
	if git clone --depth 1 --branch "$version" "$repo_url" "$tmp/src" >"$tmp/git.log" 2>&1; then
		return 0
	fi
	rm -rf "$tmp/src"
	if git clone "$repo_url" "$tmp/src" >>"$tmp/git.log" 2>&1 &&
		git -C "$tmp/src" checkout -q "$version" >>"$tmp/git.log" 2>&1; then
		return 0
	fi
	rm -rf "$tmp/src"
	return 1
}

download_source_archive() {
	archive="$tmp/wago-source.zip"
	if have curl; then
		curl -fsSL -o "$archive" "$archive_url" >"$tmp/archive.log" 2>&1
	elif have wget; then
		wget -qO "$archive" "$archive_url" >"$tmp/archive.log" 2>&1
	else
		printf 'neither curl nor wget is installed\n' >"$tmp/archive.log"
		return 1
	fi
}

unpack_source_archive() {
	archive="$tmp/wago-source.zip"
	unpack_dir="$tmp/archive"
	rm -rf "$unpack_dir"
	mkdir -p "$unpack_dir"
	if have unzip; then
		unzip -q "$archive" -d "$unpack_dir" >>"$tmp/archive.log" 2>&1
	elif have python3; then
		python3 - "$archive" "$unpack_dir" >>"$tmp/archive.log" 2>&1 <<'PY'
import sys
import zipfile

with zipfile.ZipFile(sys.argv[1]) as source:
    source.extractall(sys.argv[2])
PY
	else
		printf 'neither unzip nor python3 is installed\n' >>"$tmp/archive.log"
		return 1
	fi
	set -- "$unpack_dir"/*
	if [ "$#" -ne 1 ] || [ ! -d "$1" ] || [ ! -f "$1/go.mod" ]; then
		printf 'source archive did not contain one Wago source directory\n' >>"$tmp/archive.log"
		return 1
	fi
	mv "$1" "$tmp/src"
}

fetch_wago_source() {
	source_method=""
	progress_begin "fetching Wago source with git"
	if fetch_source_with_git; then
		source_method=git
		progress_done "fetched Wago source with git"
		return 0
	fi

	progress_retry "git fetch failed; trying source archive"
	progress_begin "downloading Wago source archive"
	if download_source_archive && unpack_source_archive; then
		source_method=archive
		progress_done "downloaded and unpacked Wago source archive"
		return 0
	fi

	progress_fail "source fetch failed"
	[ ! -f "$tmp/git.log" ] || tail -n 12 "$tmp/git.log" >&2
	[ ! -f "$tmp/archive.log" ] || tail -n 12 "$tmp/archive.log" >&2
	return 1
}

cleanup() {
	stop_spinner
	if [ -n "$tmp" ] && [ -d "$tmp" ]; then
		rm -rf "$tmp"
	fi
}
trap cleanup EXIT
trap 'exit 130' HUP INT TERM

if [ "${WAGO_INTERNAL_PATH_SETUP_ONLY:-0}" = "1" ]; then
	if ! offer_path_setup; then
		printf 'Add %s to PATH to use wago.\n' "$(display_path "$bin_dir")"
	fi
	exit 0
fi

if [ "${WAGO_INTERNAL_VERIFY_ONLY:-0}" = "1" ]; then
	verify_installation
	exit $?
fi

if [ "${WAGO_INTERNAL_FETCH_ONLY:-0}" = "1" ]; then
	tmp=$(mktemp -d 2>/dev/null || mktemp -d -t wago)
	fetch_wago_source
	printf 'source=%s\n' "$source_method"
	exit 0
fi

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

# Source build needs the Go toolchain.
progress_begin "checking Go toolchain"
if have go && go_version_ok; then
	progress_done "Go toolchain ready"
else
	progress_fail "Go 1.22 or newer is required"
	die "install Go 1.22+ and run the installer again"
fi

tmp=$(mktemp -d 2>/dev/null || mktemp -d -t wago)

# Prefer Git so installed source retains repository metadata. If Git is missing
# or the requested ref cannot be cloned, use GitHub's zip archive instead.
if ! fetch_wago_source; then
	die "could not fetch $repo_url at $version with git or $archive_url"
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
if verify_installation; then
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
		if ! offer_path_setup; then
			printf '\n%sNext step%s\n' "$bold" "$reset"
			printf '  Add %s to PATH, then run %swago%s.\n' "$(display_path "$bin_dir")" "$cyan" "$reset"
		fi
		;;
esac
