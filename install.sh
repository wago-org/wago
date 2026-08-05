#!/bin/sh
# Wago installer bootstrap.
#
#   curl -fsSL https://install.wago.sh | sh
#
# It also starts a refreshed shell when the native installer requests one.
set -eu

release_repo="${WAGO_RELEASE_REPO:-wago-org/wago}"
release_api="${WAGO_RELEASES_API_URL:-https://api.github.com/repos/$release_repo/releases}"
release_download_base="${WAGO_RELEASE_DOWNLOAD_BASE:-https://github.com/$release_repo/releases}"
version="${WAGO_VERSION:-main}"
tmp=""

die() {
	printf 'wago: %s\n' "$*" >&2
	exit 1
}

cleanup() {
	[ -z "$tmp" ] || rm -rf "$tmp"
}
trap cleanup EXIT HUP INT TERM

download() {
	url=$1
	target=$2
	if command -v curl >/dev/null 2>&1; then
		curl -fsSL --retry 2 --connect-timeout 10 "$url" -o "$target"
	elif command -v wget >/dev/null 2>&1; then
		wget -q "$url" -O "$target"
	else
		return 1
	fi
}

release_tag_from_json() {
	prefix=$1
	awk -v prefix="$prefix-" '
		/"tag_name"[[:space:]]*:/ {
			line = $0
			sub(/^.*"tag_name"[[:space:]]*:[[:space:]]*"/, "", line)
			sub(/".*$/, "", line)
			tag = line
		}
		/"published_at"[[:space:]]*:/ {
			line = $0
			sub(/^.*"published_at"[[:space:]]*:[[:space:]]*"/, "", line)
			sub(/".*$/, "", line)
			if (index(tag, prefix) == 1 && (best == "" || line > best)) {
				best = line
				best_tag = tag
			}
			tag = ""
		}
		END { if (best_tag != "") print best_tag }
	' "$2"
}

resolve_release() {
	case "$version" in
		latest)
			download "$release_api/latest" "$tmp/release.json" || return 1
			tag=$(sed -n 's/^[[:space:]]*"tag_name"[[:space:]]*:[[:space:]]*"\([^"]*\)".*/\1/p' "$tmp/release.json" | head -1)
			;;
		v*|canary-*|nightly-*) tag=$version ;;
		*)
			case "$version" in nightly) channel=nightly ;; *) channel=canary ;; esac
			download "$release_api?per_page=100" "$tmp/releases.json" || return 1
			tag=$(release_tag_from_json "$channel" "$tmp/releases.json")
			;;
	esac
	[ -n "${tag:-}" ] || return 1
}

target_name() {
	case "$(uname -s)" in
		Darwin) os=darwin ;;
		Linux) os=linux ;;
		*) return 1 ;;
	esac
	case "$(uname -m)" in
		x86_64|amd64) arch=amd64 ;;
		arm64|aarch64) arch=arm64 ;;
		*) return 1 ;;
	esac
	printf 'wago-installer-%s-%s' "$os" "$arch"
}

verify_checksum() {
	payload=$1
	checksum=$2
	expected=$(awk 'NR == 1 { print $1 }' "$checksum" | tr 'A-F' 'a-f')
	case "$expected" in ""|*[!0-9a-f]*) return 1 ;; esac
	[ "${#expected}" -eq 64 ] || return 1
	if command -v sha256sum >/dev/null 2>&1; then
		actual=$(sha256sum "$payload" | awk '{ print $1 }')
	elif command -v shasum >/dev/null 2>&1; then
		actual=$(shasum -a 256 "$payload" | awk '{ print $1 }')
	elif command -v openssl >/dev/null 2>&1; then
		actual=$(openssl dgst -sha256 "$payload" | awk '{ print $NF }')
	else
		return 1
	fi
	[ "$(printf '%s' "$actual" | tr 'A-F' 'a-f')" = "$expected" ]
}

run_installer() {
	installer=$1
	shift
	if WAGO_PATH_REFRESH_FILE="$tmp/path-refresh" "$installer" install "$@"; then
		return 0
	else
		status=$?
	fi
	if [ "$status" -eq 2 ]; then
		die "this installer release predates the native install flow; wait for the channel to update and try again"
	fi
	return "$status"
}

start_refreshed_shell() {
	[ -f "$tmp/path-refresh" ] || return 0
	shell=${SHELL:-/bin/sh}
	[ -x "$shell" ] || die "could not start a refreshed shell: $shell"
	cleanup
	tmp=""
	trap - EXIT HUP INT TERM
	exec "$shell" -i
}

tmp=$(mktemp -d 2>/dev/null || mktemp -d -t wago) || die "could not create a temporary directory"

if [ -n "${WAGO_INSTALLER:-}" ]; then
	[ -x "$WAGO_INSTALLER" ] || die "WAGO_INSTALLER is not executable: $WAGO_INSTALLER"
	run_installer "$WAGO_INSTALLER" "$@"
	start_refreshed_shell
	exit 0
fi

asset=$(target_name) || die "this operating system or architecture is not supported"
if ! resolve_release; then
	die "the installer is unavailable; check your internet connection and try again"
fi
url="$release_download_base/download/$tag/$asset"
if ! download "$url" "$tmp/installer" || ! download "$url.sha256" "$tmp/installer.sha256"; then
	die "the installer is unavailable; check your internet connection and try again"
fi
if ! verify_checksum "$tmp/installer" "$tmp/installer.sha256"; then
	die "the downloaded installer could not be verified; try again when the release service is available"
fi
chmod +x "$tmp/installer"
run_installer "$tmp/installer" "$@"
start_refreshed_shell
