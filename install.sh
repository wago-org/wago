#!/bin/sh
# wago installer.
#
#   curl -fsSL https://wago.sh/install.sh | sh
#
# The bootstrap installer builds the runtime-independent manager from the public
# repository. It requires Go 1.22+; Git is preferred, with a zip fallback.
#
# Environment:
#   WAGO_VERSION   git ref to build: branch, tag, or commit (default: main)
#   WAGO_BIN_DIR   install directory (default: $HOME/.wago/bin)
#   WAGO_REINSTALL_MODE full, partial, or minimal for an existing install
#   WAGO_DRY_RUN   set to 1 to print what would happen and exit
#   WAGO_NO_MODIFY_PATH set to 1 to never offer to edit shell startup files
#   WAGO_NO_COMPLETIONS set to 1 to skip shell completion setup
#   NO_COLOR       set to disable colored output
set -eu

repo_url="${WAGO_REPO_URL:-https://github.com/wago-org/wago.git}"
version="${WAGO_VERSION:-main}"
archive_url="${WAGO_ARCHIVE_URL:-https://api.github.com/repos/wago-org/wago/zipball/$version}"
bin_dir_explicit=0
[ -z "${WAGO_BIN_DIR:-}" ] || bin_dir_explicit=1
bin_dir="${WAGO_BIN_DIR:-$HOME/.wago/bin}"
# The wago source is kept here so `wago pkg add` can build plugins while wago is
# unpublished — the CLI looks for it at ~/.wago/src (see wagoModuleDir).
src_dir="${WAGO_SRC_DIR:-$HOME/.wago/src}"
dry_run="${WAGO_DRY_RUN:-0}"
no_modify_path="${WAGO_NO_MODIFY_PATH:-0}"
no_completions="${WAGO_NO_COMPLETIONS:-0}"

if [ -n "${WAGO_HOME:-}" ]; then
	wago_data="$WAGO_HOME/data"
	wago_config="$WAGO_HOME/config"
	wago_cache="$WAGO_HOME/cache"
elif [ "$(uname -s)" = "Darwin" ]; then
	wago_data="$HOME/.wago"
	wago_config="$HOME/.wago/config"
	wago_cache="$HOME/.wago/cache"
else
	wago_data="${XDG_DATA_HOME:-$HOME/.local/share}/wago"
	wago_config="${XDG_CONFIG_HOME:-$HOME/.config}/wago"
	wago_cache="${XDG_CACHE_HOME:-$HOME/.cache}/wago"
fi
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
radio_saved_stty=""
radio_tty=""
radio_painted=0
radio_lines=0
install_input_open=0

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

detail() { printf '  %s%-8s%s %s\n' "$dim" "$1" "$reset" "$2"; }
welcome() { printf "%sWelcome to Wago! Let's get you set up.%s\n\n" "$bold" "$reset"; }
report_install_dir() { printf 'Installing to: %s\n\n' "$(display_path "$bin_dir")"; }
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

restore_radio_terminal() {
	if [ -n "$radio_saved_stty" ] && [ -n "$radio_tty" ]; then
		stty "$radio_saved_stty" <"$radio_tty" >/dev/null 2>&1 || true
	fi
	radio_saved_stty=""
	radio_tty=""
}

clear_radio() {
	if [ "$radio_painted" = "1" ] && [ "$is_tty" = "1" ]; then
		printf '\033[%sA\033[J' "$radio_lines"
	fi
	radio_painted=0
	radio_lines=0
}

render_radio() {
	printf '%s%s%s\n' "$bold" "$radio_title" "$reset"
	radio_label_width=$(printf '%s\n' "$radio_items" | awk -F '|' '
		length($1) > width { width = length($1) }
		END { print width + 0 }
	')
	radio_row=0
	printf '%s\n' "$radio_items" | while IFS='|' read -r radio_label radio_desc radio_item_value radio_status; do
		[ -n "$radio_label" ] || continue
		radio_row=$((radio_row + 1))
		radio_cursor_mark="  "
		radio_choice_mark="○"
		if [ "$radio_row" -eq "$radio_cursor" ]; then
			radio_cursor_mark="${cyan}› ${reset}"
			radio_choice_mark="${cyan}◉${reset}"
		fi
		radio_label_style=""
		if [ -n "$radio_status" ]; then
			radio_label_style=$bold
		fi
		printf '%s%s %s%-*s%s' "$radio_cursor_mark" "$radio_choice_mark" \
			"$radio_label_style" "$radio_label_width" "$radio_label" "$reset"
		if [ -n "$radio_desc" ]; then
			printf '  %s%s%s' "$dim" "$radio_desc" "$reset"
		fi
		if [ -n "$radio_status" ]; then
			printf '  %s(%s)%s' "$dim" "$radio_status" "$reset"
		fi
		printf '\n'
	done
	printf '%s↑/↓ move · enter/→ select · esc cancel%s\n' "$dim" "$reset"
	radio_lines=$((radio_count + 2))
	radio_painted=1
}

finish_radio() {
	restore_radio_terminal
	clear_radio
}

read_install_line() {
	install_line=""
	if [ -c "$install_tty" ]; then
		IFS= read -r install_line <"$install_tty" || true
		return 0
	fi
	if [ "$install_input_open" = "0" ]; then
		exec 9<"$install_tty"
		install_input_open=1
	fi
	IFS= read -r install_line <&9 || true
}

radio_selected_value() {
	radio_selected=$(printf '%s\n' "$radio_items" | sed -n "${radio_cursor}p")
	radio_selected_label=$(printf '%s\n' "$radio_selected" | awk -F '|' '{ print $1 }')
	radio_value=$(printf '%s\n' "$radio_selected" | awk -F '|' '{ print $3 }')
}

radio_select_scripted() {
	render_radio
	read_install_line
	radio_input=$install_line
	case "$radio_input" in
		""|enter|right) ;;
		esc|escape|q|Q) return 1 ;;
		*[!0-9]*) return 1 ;;
		*) radio_cursor=$radio_input ;;
	esac
	if [ "$radio_cursor" -lt 1 ] || [ "$radio_cursor" -gt "$radio_count" ]; then
		return 1
	fi
	radio_selected_value
	return 0
}

radio_select() {
	radio_title=$1
	radio_items=$2
	radio_cursor=$3
	radio_value=""
	radio_count=$(printf '%s\n' "$radio_items" | awk 'NF { count++ } END { print count + 0 }')
	[ "$radio_count" -gt 0 ] || return 1

	if [ ! -c "$install_tty" ]; then
		radio_select_scripted
		return $?
	fi

	radio_tty=$install_tty
	radio_saved_stty=$(stty -g <"$radio_tty" 2>/dev/null) || {
		radio_saved_stty=""
		radio_tty=""
		return 1
	}
	# Change input handling only. `stty raw` also disables output newline
	# translation, which makes every repaint staircase to the right on macOS.
	if ! stty -echo -icanon min 1 time 0 <"$radio_tty" 2>/dev/null; then
		restore_radio_terminal
		return 1
	fi
	render_radio
	radio_escape=$(printf '\033')
	radio_return=$(printf '\r')
	while :; do
		radio_key=$(dd if="$radio_tty" bs=1 count=1 2>/dev/null || true)
		case "$radio_key" in
			""|"$radio_return"|l|L|">")
				radio_selected_value
				finish_radio
				return 0
				;;
			k|K)
				if [ "$radio_cursor" -gt 1 ]; then
					radio_cursor=$((radio_cursor - 1))
				fi
				;;
			j|J)
				if [ "$radio_cursor" -lt "$radio_count" ]; then
					radio_cursor=$((radio_cursor + 1))
				fi
				;;
			q|Q)
				finish_radio
				return 1
				;;
			"$radio_escape")
				stty min 0 time 1 <"$radio_tty" 2>/dev/null || true
				radio_second=$(dd if="$radio_tty" bs=1 count=1 2>/dev/null || true)
				radio_third=""
				if [ "$radio_second" = "[" ]; then
					radio_third=$(dd if="$radio_tty" bs=1 count=1 2>/dev/null || true)
				fi
				stty min 1 time 0 <"$radio_tty" 2>/dev/null || true
				case "$radio_third" in
					A)
						if [ "$radio_cursor" -gt 1 ]; then
							radio_cursor=$((radio_cursor - 1))
						fi
						;;
					B)
						if [ "$radio_cursor" -lt "$radio_count" ]; then
							radio_cursor=$((radio_cursor + 1))
						fi
						;;
					C)
						radio_selected_value
						finish_radio
						return 0
						;;
					"")
						finish_radio
						return 1
						;;
				esac
				;;
		esac
		clear_radio
		render_radio
	done
}

resolve_custom_path() {
	case "$1" in
		"~") printf '%s' "$HOME" ;;
		"~/"*) printf '%s/%s' "$HOME" "${1#\~/}" ;;
		/*) printf '%s' "$1" ;;
		*) printf '%s/%s' "$(pwd)" "$1" ;;
	esac
}

refresh_path_preview() {
	path_preview=""
	case "$path_editor_input" in
		"")
			path_parent=""
			path_prefix=""
			;;
		"~")
			path_parent=$HOME
			path_prefix=""
			;;
		*/)
			path_parent=$(resolve_custom_path "${path_editor_input%/}")
			path_prefix=""
			;;
		*/*)
			path_typed_parent=${path_editor_input%/*}
			path_prefix=${path_editor_input##*/}
			[ -n "$path_typed_parent" ] || path_typed_parent="/"
			path_parent=$(resolve_custom_path "$path_typed_parent")
			;;
		*)
			path_parent=$(pwd)
			path_prefix=$path_editor_input
			;;
	esac
	if [ -n "$path_parent" ] && [ -d "$path_parent" ]; then
			path_preview=$(
				find "$path_parent" -mindepth 1 -maxdepth 1 -type d -print 2>/dev/null |
					awk -v prefix="$path_prefix" '
						{
							name = $0
							sub(/^.*\//, "", name)
							if (substr(name, 1, length(prefix)) == prefix) {
								print $0
							}
						}
					' |
					while IFS= read -r path_candidate; do
						printf '%s/\n' "$(display_path "$path_candidate")"
					done |
				LC_ALL=C sort |
				head -5
		)
	fi
	path_preview_count=$(printf '%s\n' "$path_preview" | awk 'NF { count++ } END { print count + 0 }')
	if [ "$path_preview_count" -eq 0 ]; then
		path_preview_cursor=0
	elif [ "$path_preview_cursor" -lt 1 ] || [ "$path_preview_cursor" -gt "$path_preview_count" ]; then
		path_preview_cursor=1
	fi
}

render_path_editor() {
	refresh_path_preview
	printf '%sWhere should Wago be installed?%s\n' "$bold" "$reset"
	if [ "$path_editor_focus" -eq 1 ]; then
		printf '%s› %s◉%s %s\n' "$cyan" "$cyan" "$reset" "$(display_path "$bin_dir")"
	else
		printf '  ○ %s\n' "$(display_path "$bin_dir")"
	fi
	if [ "$path_editor_focus" -eq 2 ]; then
		printf '%s› %s◉%s ' "$cyan" "$cyan" "$reset"
		if [ -n "$path_editor_input" ]; then
			printf '%s' "$path_editor_input"
		else
			printf '%sType a directory%s' "$dim" "$reset"
		fi
		printf '\n'
	else
		printf '  ○ Custom\n'
	fi
	path_preview_row=0
	printf '%s\n' "$path_preview" | while IFS= read -r path_suggestion; do
		[ -n "$path_suggestion" ] || continue
		path_preview_row=$((path_preview_row + 1))
		if [ "$path_preview_row" -eq "$path_preview_cursor" ]; then
			printf '    %s› %s%s\n' "$cyan" "$path_suggestion" "$reset"
		else
			printf '      %s%s%s\n' "$dim" "$path_suggestion" "$reset"
		fi
	done
	if [ "$path_editor_focus" -eq 1 ]; then
		printf '%s↑/↓ move · enter/→ select · esc cancel%s\n' "$dim" "$reset"
	else
		printf '%s↑/↓ suggestions · type path · tab/→ complete · ← parent · enter select · esc cancel%s\n' "$dim" "$reset"
	fi
	radio_lines=$((path_preview_count + 4))
	radio_painted=1
}

complete_path_preview() {
	[ "$path_preview_cursor" -gt 0 ] || return 0
	path_completion=$(printf '%s\n' "$path_preview" | sed -n "${path_preview_cursor}p")
	[ -n "$path_completion" ] || return 0
	path_editor_input=$path_completion
	path_preview_cursor=1
}

path_editor_parent() {
	path_parent_input=${path_editor_input%/}
	case "$path_parent_input" in
		"")
			path_editor_focus=1
			;;
		"/")
			path_editor_input="/"
			;;
		"~")
			path_editor_input="~/"
			;;
		*/*)
			path_editor_input=${path_parent_input%/*}
			if [ -z "$path_editor_input" ]; then
				path_editor_input="/"
			elif [ "$path_editor_input" = "~" ]; then
				path_editor_input="~/"
			else
				path_editor_input="${path_editor_input}/"
			fi
			;;
		*)
			path_editor_input=""
			;;
	esac
	path_preview_cursor=1
}

path_editor_scripted() {
	render_path_editor
	read_install_line
	case "$install_line" in
		""|1|enter|right)
			custom_path_value=$(display_path "$bin_dir")
			return 0
			;;
		2|down|custom)
			path_editor_focus=2
			clear_radio
			render_path_editor
			read_install_line
			;;
		esc|escape|q|Q) return 1 ;;
	esac
	[ -n "$install_line" ] || return 1
	custom_path_value=$install_line
	return 0
}

path_editor() {
	path_editor_input=""
	path_editor_focus=1
	path_preview_cursor=1
	custom_path_value=""
	if [ ! -c "$install_tty" ]; then
		path_editor_scripted
		return $?
	fi

	radio_tty=$install_tty
	radio_saved_stty=$(stty -g <"$radio_tty" 2>/dev/null) || {
		radio_saved_stty=""
		radio_tty=""
		return 1
	}
	if ! stty -echo -icanon min 1 time 0 <"$radio_tty" 2>/dev/null; then
		restore_radio_terminal
		return 1
	fi
	render_path_editor
	path_escape=$(printf '\033')
	path_return=$(printf '\r')
	path_tab=$(printf '\t')
	path_backspace=$(printf '\b')
	path_delete=$(printf '\177')
	while :; do
		path_key=$(dd if="$radio_tty" bs=1 count=1 2>/dev/null || true)
		case "$path_key" in
			""|"$path_return")
				if [ "$path_editor_focus" -eq 1 ]; then
					custom_path_value=$(display_path "$bin_dir")
					finish_radio
					return 0
				elif [ -n "$path_editor_input" ]; then
					custom_path_value=$path_editor_input
					finish_radio
					return 0
				fi
				;;
			"$path_tab")
				if [ "$path_editor_focus" -eq 2 ]; then
					complete_path_preview
				fi
				;;
			"$path_backspace"|"$path_delete")
				if [ "$path_editor_focus" -eq 2 ] && [ -n "$path_editor_input" ]; then
					path_editor_input=${path_editor_input%?}
					path_preview_cursor=1
				fi
				;;
			"$path_escape")
				stty min 0 time 1 <"$radio_tty" 2>/dev/null || true
				path_second=$(dd if="$radio_tty" bs=1 count=1 2>/dev/null || true)
				path_third=""
				if [ "$path_second" = "[" ]; then
					path_third=$(dd if="$radio_tty" bs=1 count=1 2>/dev/null || true)
				fi
				stty min 1 time 0 <"$radio_tty" 2>/dev/null || true
				case "$path_third" in
					A)
						if [ "$path_editor_focus" -eq 2 ] && [ "$path_preview_cursor" -gt 1 ]; then
							path_preview_cursor=$((path_preview_cursor - 1))
						elif [ "$path_editor_focus" -eq 2 ]; then
							path_editor_focus=1
						fi
						;;
					B)
						if [ "$path_editor_focus" -eq 1 ]; then
							path_editor_focus=2
						elif [ "$path_preview_cursor" -lt "$path_preview_count" ]; then
							path_preview_cursor=$((path_preview_cursor + 1))
						fi
						;;
					C)
						if [ "$path_editor_focus" -eq 1 ]; then
							custom_path_value=$(display_path "$bin_dir")
							finish_radio
							return 0
						else
							complete_path_preview
						fi
						;;
					D)
						if [ "$path_editor_focus" -eq 2 ]; then
							path_editor_parent
						fi
						;;
					"")
						finish_radio
						return 1
						;;
				esac
				;;
			*)
				if [ "$path_editor_focus" -eq 2 ]; then
					case "$path_key" in
						[[:print:]])
							path_editor_input="${path_editor_input}${path_key}"
							path_preview_cursor=1
							;;
					esac
				fi
				;;
		esac
		clear_radio
		render_path_editor
	done
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
	path_desc=$(display_path "$config_file")
	path_status=""
	if [ "$shell_name" = "$current_shell" ]; then
		path_status=current
	fi
	path_radio_items="${path_radio_items}${shell_name}|${path_desc}|${shell_name}|${path_status}
"
}

shell_single_quote() {
	printf '%s' "$1" | sed "s/'/'\\\\''/g"
}

print_wago_ready() {
	config_file=$1
	printf '\n%sWago is ready!%s\n' "$bold" "$reset"
	if have wago; then
		printf 'Install a version with:\n  %swago version install%s\n' "$cyan" "$reset"
		return
	fi
	printf 'Open a new shell, or run:\n  %ssource %s && wago version install%s\n' \
		"$cyan" "$(display_path "$config_file")" "$reset"
}

add_path_to_config() {
	shell_name=$1
	config_file=$2
	marker="# Wago PATH: $bin_dir"
	if [ -f "$config_file" ] && grep -F "$marker" "$config_file" >/dev/null 2>&1; then
		printf '%s✓%s PATH already configured\n' "$cyan" "$reset"
		[ "${WAGO_INTERNAL_PATH_SETUP_ONLY:-0}" != "1" ] || print_wago_ready "$config_file"
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
	printf '%s✓%s Added Wago to PATH\n' "$cyan" "$reset"
	[ "${WAGO_INTERNAL_PATH_SETUP_ONLY:-0}" != "1" ] || print_wago_ready "$config_file"
}

offer_path_setup() {
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
	path_radio_items=""
	path_option_count=0
	path_option_add "$current_shell"
	for shell_name in zsh bash fish nu; do
		path_option_add "$shell_name"
	done
	if [ "$path_option_count" -eq 0 ]; then
		return 1
	fi

	path_radio_items="${path_radio_items}Not now||none"
	if ! radio_select "Add Wago to your PATH?" "$path_radio_items" 1; then
		return 1
	fi
	if [ "$radio_value" = "none" ]; then
		printf 'PATH setup: skipped\n\n'
		return 1
	fi
	selected=$(printf '%s' "$path_shells" | sed -n "/^${radio_value}|/p")
	shell_name=${selected%%|*}
	config_file=${selected#*|}
	configured_shell=$shell_name
	configured_file=$config_file
	printf 'Adding to PATH: %s\n\n' "$(display_path "$config_file")"
	add_path_to_config "$shell_name" "$config_file"
}

offer_completion_setup() {
	[ "$no_completions" != "1" ] || return 1
	case "${configured_shell:-}" in zsh|bash|fish) ;; *) return 1 ;; esac
	completion_items='Yes|Enable command completion|yes
No||no'
	if ! radio_select "Enable Wago completions for $configured_shell?" "$completion_items" 1; then
		return 1
	fi
	if [ "$radio_value" != "yes" ]; then
		printf 'Completions: skipped\n\n'
		return 1
	fi
	if "$bin_dir/wago" config completions "$configured_shell" --install --rc "$configured_file" >/dev/null; then
		printf '%s✓%s Enabled Wago completions for %s\n' "$cyan" "$reset" "$configured_shell"
		return 0
	fi
	printf '%s!%s Could not enable Wago completions\n' "$dim" "$reset"
	return 1
}

choose_install_dir() {
	[ "$bin_dir_explicit" = "0" ] || return 0
	if [ "$is_tty" != "1" ] && [ "${WAGO_INTERNAL_INSTALL_DIR_ONLY:-0}" != "1" ]; then
		return 0
	fi
	install_tty="${WAGO_INSTALL_TTY:-/dev/tty}"
	if [ ! -r "$install_tty" ]; then
		return 0
	fi

	if path_editor; then
		answer=$custom_path_value
	else
		return 1
	fi
	if [ "$answer" != "$(display_path "$bin_dir")" ]; then
		case "$answer" in
			"~") bin_dir=$HOME ;;
			"~/"*) bin_dir="$HOME/${answer#\~/}" ;;
			/*) bin_dir=$answer ;;
			*) bin_dir="$(pwd)/$answer" ;;
		esac
		while [ "$bin_dir" != "/" ] && [ "${bin_dir%/}" != "$bin_dir" ]; do
			bin_dir=${bin_dir%/}
		done
	fi
	return 0
}

choose_reinstall_mode() {
	reinstall_mode=minimal
	[ -e "$bin_dir/wago" ] || return 0
	if [ -n "${WAGO_REINSTALL_MODE:-}" ]; then
		case "$WAGO_REINSTALL_MODE" in
			full|partial|minimal) reinstall_mode=$WAGO_REINSTALL_MODE ;;
			*) die "WAGO_REINSTALL_MODE must be full, partial, or minimal" ;;
		esac
		return 0
	fi
	if [ "$is_tty" != "1" ] && [ "${WAGO_INTERNAL_REINSTALL_CHECK_ONLY:-0}" != "1" ]; then
		return 0
	fi
	install_tty="${WAGO_INSTALL_TTY:-/dev/tty}"
	if [ ! -r "$install_tty" ]; then
		return 0
	fi

	printf '\n%sWago is already installed at %s.%s\n' \
		"$bold" "$(display_path "$bin_dir/wago")" "$reset"
	reinstall_items='Full|Reset everything, including plugins and settings|full
Partial|Reset Wago but keep global plugins for reinstall|partial
Minimal|Replace binaries and keep existing state|minimal'
	if ! radio_select "How should it be reinstalled?" "$reinstall_items" 3; then
		return 1
	fi
	reinstall_mode=$radio_value
	printf 'Reinstall mode: %s\n\n' "$radio_selected_label"
}

remove_install_path() {
	path=$1
	[ ! -e "$path" ] || {
		case "$path" in
			""|"."|"/"|"$HOME") return 1 ;;
		esac
		rm -rf "$path"
	}
}

remove_installer_path_block() {
	config_file=$1
	[ -f "$config_file" ] || return 0
	path_config_index=$((path_config_index + 1))
	config_tmp="$tmp/path-config-$path_config_index"
	if ! awk '
		{
			lines[++count] = $0
		}
		END {
			out = 0
			for (i = 1; i <= count; i++) {
				if (lines[i] ~ /^# Wago PATH: /) {
					if (out > 0 && output[out] == "") {
						out--
					}
					if (i < count) {
						nextline = lines[i + 1]
						if (nextline ~ /^export PATH=/) {
							i++
						} else if (nextline ~ /^fish_add_path --path /) {
							i++
						} else if (nextline ~ /^\$env\.PATH = \(\$env\.PATH \| prepend /) {
							i++
						}
					}
					continue
				}
				output[++out] = lines[i]
			}
			for (i = 1; i <= out; i++) {
				print output[i]
			}
		}
	' "$config_file" >"$config_tmp"; then
		return 1
	fi
	if ! cat "$config_tmp" >"$config_file"; then
		return 1
	fi
	rm -f "$config_tmp"
}

remove_installer_path_blocks() {
	path_config_index=0
	remove_installer_path_block "${ZDOTDIR:-$HOME}/.zshrc" &&
		remove_installer_path_block "$HOME/.bashrc" &&
		remove_installer_path_block "$HOME/.bash_profile" &&
		remove_installer_path_block "${XDG_CONFIG_HOME:-$HOME/.config}/fish/config.fish" &&
		remove_installer_path_block "${XDG_CONFIG_HOME:-$HOME/.config}/nushell/env.nu"
}

remove_full_wago_roots() {
	remove_install_path "$HOME/.wago" || return 1
	if [ -n "${WAGO_HOME:-}" ]; then
		remove_install_path "$WAGO_HOME" || return 1
	fi
}

apply_reinstall_cleanup() {
	case "$reinstall_mode" in
		full)
			remove_install_path "$wago_data" &&
				remove_install_path "$wago_config" &&
				remove_install_path "$wago_cache" &&
				remove_install_path "$src_dir" &&
				rm -f "$bin_dir/wago" &&
				remove_full_wago_roots &&
				remove_installer_path_blocks
			;;
		partial)
			remove_install_path "$wago_data/versions" &&
				remove_install_path "$wago_config" &&
				remove_install_path "$wago_cache" &&
				remove_install_path "$src_dir" &&
				rm -f "$bin_dir/wago" &&
				remove_installer_path_blocks
			;;
		minimal)
			return 0
			;;
		*)
			return 1
			;;
	esac
}

report_reinstall_cleanup() {
	case "$reinstall_mode" in
		full) printf 'mode=full plugins=removed\n' ;;
		partial) printf 'mode=partial plugins=preserved\n' ;;
		minimal) printf 'mode=minimal state=preserved\n' ;;
	esac
}

cancel_reinstall() {
	printf 'Cancelled.\n'
	exit 0
}

confirm_reinstall() {
	if choose_reinstall_mode; then
		return 0
	fi
	cancel_reinstall
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
	restore_radio_terminal
	clear_radio
	if [ "$install_input_open" = "1" ]; then
		exec 9<&-
		install_input_open=0
	fi
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

if [ "${WAGO_INTERNAL_REINSTALL_CHECK_ONLY:-0}" = "1" ]; then
	confirm_reinstall
	report_reinstall_cleanup
	exit 0
fi

if [ -n "${WAGO_INTERNAL_REINSTALL_CLEANUP_ONLY:-}" ]; then
	tmp=$(mktemp -d 2>/dev/null || mktemp -d -t wago)
	reinstall_mode=$WAGO_INTERNAL_REINSTALL_CLEANUP_ONLY
	apply_reinstall_cleanup
	report_reinstall_cleanup
	exit 0
fi

if [ -n "${WAGO_INTERNAL_PATH_PREVIEW_ONLY:-}" ]; then
	path_editor_input=$WAGO_INTERNAL_PATH_PREVIEW_ONLY
	path_preview_cursor=1
	refresh_path_preview
	printf '%s\n' "$path_preview"
	exit 0
fi

if [ -n "${WAGO_INTERNAL_PATH_PARENT_ONLY:-}" ]; then
	path_editor_input=$WAGO_INTERNAL_PATH_PARENT_ONLY
	path_editor_focus=2
	path_editor_parent
	printf '%s\n' "$path_editor_input"
	exit 0
fi

if [ "${WAGO_INTERNAL_INSTALL_DIR_ONLY:-0}" = "1" ]; then
	welcome
	if ! choose_install_dir; then
		printf 'Cancelled.\n'
		exit 0
	fi
	report_install_dir
	printf 'bin=%s\n' "$(display_path "$bin_dir")"
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

welcome

if ! choose_install_dir; then
	printf 'Cancelled.\n'
	exit 0
fi
report_install_dir

if [ "$dry_run" = "1" ]; then
	detail "version" "$version"
	detail "command" "$(display_path "$bin_dir/wago")"
	detail "source" "$(display_path "$src_dir")"
	printf '%sNo changes made.%s\n' "$dim" "$reset"
	exit 0
fi

confirm_reinstall

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
progress_begin "building Wago"
if (cd "$tmp/src" &&
	CGO_ENABLED=0 go build -trimpath \
		-ldflags "-s -w -X main.version=$stamp" -o "$tmp/wago" ./cli/wago) >"$tmp/manager.log" 2>&1; then
	progress_done "built Wago"
else
	progress_fail "Wago build failed"
	tail -n 20 "$tmp/manager.log" >&2 || true
	die "could not build Wago"
fi

if [ "$reinstall_mode" != "minimal" ]; then
	progress_begin "cleaning existing Wago installation"
	if apply_reinstall_cleanup; then
		progress_done "cleaned existing Wago installation ($reinstall_mode)"
	else
		progress_fail "could not clean existing Wago installation"
		die "reinstall cleanup failed"
	fi
fi

progress_begin "installing Wago"
if mkdir -p "$bin_dir" &&
	mv "$tmp/wago" "$bin_dir/wago"; then
	progress_done "installed Wago"
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

progress_finish "Installed Wago $stamp"
detail "Command" "$(display_path "$bin_dir/wago")"

printf '\n'
if ! offer_path_setup; then
	case ":$PATH:" in
		*":$bin_dir:"*) ;;
		*)
			printf '\n%sNext step%s\n' "$bold" "$reset"
			printf '  Add %s to PATH, then run %swago%s.\n' "$(display_path "$bin_dir")" "$cyan" "$reset"
			;;
	esac
else
	offer_completion_setup || true
	print_wago_ready "$configured_file"
fi
