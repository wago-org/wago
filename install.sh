#!/bin/sh
# wago installer.
#
#   curl -fsSL https://wago.sh/install.sh | sh
#
# wago is private during development, so this builds from source over SSH: you
# need read access to git@github.com:wago-org/wago and Go 1.22+. Everyone else
# should wait for the public v0.1.0 release — the same command will then install
# a prebuilt binary with no access required.
#
# Environment:
#   WAGO_VERSION   git ref to build: branch, tag, or commit (default: main)
#   WAGO_BIN_DIR   install directory (default: $HOME/.local/bin)
#   WAGO_DRY_RUN   set to 1 to print what would happen and exit
#   NO_COLOR       set to disable colored output
set -eu

repo_ssh="git@github.com:wago-org/wago"
version="${WAGO_VERSION:-main}"
bin_dir="${WAGO_BIN_DIR:-$HOME/.local/bin}"
dry_run="${WAGO_DRY_RUN:-0}"

# Accept GitHub's host key non-interactively; ssh still prompts for a key
# passphrase on /dev/tty if the agent doesn't hold it.
export GIT_SSH_COMMAND="${GIT_SSH_COMMAND:-ssh -o StrictHostKeyChecking=accept-new}"

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

# Print the "you don't have access yet" notice and exit non-zero.
no_access() {
	printf '\n%s✗ wago is private during development, and your SSH key has no read access.%s\n\n' "$pink" "$reset"
	info "If you should have access:"
	info "  • add your SSH key to GitHub — https://github.com/settings/keys"
	info "  • confirm you're a member of the wago-org/wago repo"
	printf '\n  %sOtherwise, hang tight — wago goes public with %s%sv0.1.0%s%s.%s\n' "$dim" "$reset" "$lilac" "$reset" "$dim" "$reset"
	printf '  %sThe same command will then install a prebuilt binary, no access needed.%s\n\n' "$dim" "$reset"
	exit 1
}

banner

have git || die "git is required to install wago"

# Gate on read access to the private repo.
step "checking your access to wago ${dim}(private)${reset}"
git ls-remote "$repo_ssh" >/dev/null 2>&1 || no_access
ok "access confirmed"

# Source build needs the Go toolchain.
have go || die "Go 1.22+ is required to build wago from source"
go_version_ok || die "Go 1.22 or newer is required"

if [ "$dry_run" = "1" ]; then
	info "dry run: clone $repo_ssh@$version + the wasi plugin, then go build ./cli/wago -> $bin_dir/wago"
	exit 0
fi

tmp=$(mktemp -d 2>/dev/null || mktemp -d -t wago)
trap 'rm -rf "$tmp"' EXIT

# Shallow-clone the requested ref. --branch handles branches and tags; a raw
# commit sha falls back to a full clone + checkout.
step "cloning wago ${bold}$version${reset}"
if ! git clone --depth 1 --branch "$version" "$repo_ssh" "$tmp/src" 2>/dev/null; then
	git clone "$repo_ssh" "$tmp/src" >/dev/null 2>&1 || die "could not clone $repo_ssh"
	git -C "$tmp/src" checkout -q "$version" 2>/dev/null || die "no such version: $version"
fi

# wago bundles the WASI plugin by default. Clone it as the sibling the module's
# `replace github.com/wago-org/wasi => ../wasi` expects, then blank-import its
# register package so it's compiled in. (wago-org/wasi is private too, so this
# clones over SSH with the same key that cloned wago.)
step "fetching the wasi plugin"
git clone --depth 1 "git@github.com:wago-org/wasi" "$tmp/wasi" >/dev/null 2>&1 \
	|| die "could not clone the wasi plugin (need access to wago-org/wasi)"
printf 'package main\n\nimport _ "github.com/wago-org/wasi/register"\n' > "$tmp/src/cli/wago/zz_wago_plugins.go"

# stdlib plus the local wasi plugin, so this builds offline (no module downloads).
stamp=$(git -C "$tmp/src" describe --tags --always 2>/dev/null || echo "$version")
step "building wago ${dim}($stamp)${reset}"
( cd "$tmp/src" && go build -trimpath -ldflags "-X main.version=$stamp" -o "$tmp/wago" ./cli/wago ) \
	|| die "build failed"

mkdir -p "$bin_dir"
mv "$tmp/wago" "$bin_dir/wago"
ok "installed $bin_dir/wago"

"$bin_dir/wago" --version || true
case ":$PATH:" in
	*":$bin_dir:"*) ;;
	*) info "add $bin_dir to your PATH to run: wago" ;;
esac
