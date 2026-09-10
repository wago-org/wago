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
install_version=$version
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
		curl -fsSL --retry 2 --connect-timeout 10 "$url" -o "$target" 2>/dev/null
	elif command -v wget >/dev/null 2>&1; then
		wget -q "$url" -O "$target"
	else
		return 1
	fi
}

release_tag_from_json() {
	channel=$1
	awk -v channel="$channel" '
		/"tag_name"[[:space:]]*:/ {
			line = $0
			sub(/^.*"tag_name"[[:space:]]*:[[:space:]]*"/, "", line)
			sub(/".*$/, "", line)
			tag = line
		}
		/"draft"[[:space:]]*:[[:space:]]*true/ { tag = "" }
		/"published_at"[[:space:]]*:/ {
			line = $0
			sub(/^.*"published_at"[[:space:]]*:[[:space:]]*"/, "", line)
			sub(/".*$/, "", line)
			matches = (channel == "official" && tag ~ /^v[0-9]+\.[0-9]+\.[0-9]+$/) || \
				(channel == "beta" && tag ~ /^v[0-9]+\.[0-9]+\.[0-9]+-beta\.[0-9]+$/) || \
				(channel == "canary" && tag ~ /^v[0-9]+\.[0-9]+\.[0-9]+-canary\.g[0-9a-f]{7}$/)
			if (matches && (best == "" || line > best)) {
				best = line
				best_tag = tag
			}
			tag = ""
		}
		END { if (best_tag != "") print best_tag }
	' "$2"
}

release_count_from_json() {
	awk '{ count += gsub(/"tag_name"[[:space:]]*:/, "&") } END { print count + 0 }' "$1"
}

release_tag_from_pages() {
	wanted_channel=$1
	page=1
	while [ "$page" -le 10 ]; do
		page_file="$tmp/releases-$page.json"
		download "$release_api?per_page=100&page=$page" "$page_file" || break
		tag=$(release_tag_from_json "$wanted_channel" "$page_file")
		if [ -n "$tag" ]; then
			printf '%s\n' "$tag"
			return 0
		fi
		count=$(release_count_from_json "$page_file")
		[ "$count" -ge 100 ] || break
		page=$((page + 1))
	done
	return 1
}

resolve_release() {
	tags=""
	case "$version" in
		latest)
			download "$release_api/latest" "$tmp/release.json" || return 1
			tag=$(sed -n 's/^[[:space:]]*"tag_name"[[:space:]]*:[[:space:]]*"\([^"]*\)".*/\1/p' "$tmp/release.json" | head -1)
			tags=$tag
			;;
		v*-canary.g???????)
			install_version=$version
			tags=$(release_tag_from_pages beta) || return 1
			;;
		v*) tags=$version ;;
		*)
			case "$version" in
				beta)
					tags=$(release_tag_from_pages beta) || return 1
					;;
				canary|canary@*)
					install_version=$version
					tags=$(release_tag_from_pages beta) || return 1
					;;
				main)
					if download "$release_api/latest" "$tmp/release.json"; then
						tag=$(release_tag_from_json official "$tmp/release.json")
						[ -z "$tag" ] || tags=$tag
					fi
					beta_tag=$(release_tag_from_pages beta || true)
					[ -z "$beta_tag" ] || tags="$tags $beta_tag"
					;;
			esac
			;;
	esac
	[ -n "${tags# }" ] || return 1
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
	if WAGO_VERSION="$install_version" WAGO_PATH_REFRESH_FILE="$tmp/path-refresh" "$installer" install "$@"; then
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
downloaded=""
for tag in $tags; do
	url="$release_download_base/download/$tag/$asset"
	rm -f "$tmp/installer" "$tmp/installer.sha256"
	if ! download "$url" "$tmp/installer" || ! download "$url.sha256" "$tmp/installer.sha256"; then
		continue
	fi
	if ! verify_checksum "$tmp/installer" "$tmp/installer.sha256"; then
		die "the downloaded installer could not be verified; try again when the release service is available"
	fi
	downloaded=1
	break
done
if [ -z "$downloaded" ]; then
	die "the installer is unavailable; check your internet connection and try again"
fi
chmod +x "$tmp/installer"
run_installer "$tmp/installer" "$@"
start_refreshed_shell
