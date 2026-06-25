#!/bin/sh
set -eu

repo="github.com/wago-org/wago"
cmd="$repo/cli/wago"
version="${WAGO_VERSION:-latest}"
bin_dir="${WAGO_BIN_DIR:-}"
dry_run="${WAGO_DRY_RUN:-0}"

say() {
	printf '%s\n' "$*"
}

die() {
	printf 'wago: %s\n' "$*" >&2
	exit 1
}

need() {
	command -v "$1" >/dev/null 2>&1 || die "missing required command: $1"
}

go_env() {
	go env "$1" 2>/dev/null || true
}

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

os=$(uname -s 2>/dev/null || true)
arch=$(uname -m 2>/dev/null || true)

[ "$os" = "Linux" ] || die "unsupported OS: $os (wago currently targets linux/amd64)"
case "$arch" in
	x86_64|amd64) ;;
	*) die "unsupported architecture: $arch (wago currently targets linux/amd64)" ;;
esac

need go
go_version_ok || die "Go 1.22 or newer is required"

if [ -z "$bin_dir" ]; then
	gobin=$(go_env GOBIN)
	if [ -n "$gobin" ]; then
		bin_dir=$gobin
	else
		gopath=$(go_env GOPATH)
		[ -n "$gopath" ] || gopath="$HOME/go"
		bin_dir="$gopath/bin"
	fi
fi

pkg="$cmd@$version"

say "installing wago from $pkg"
say "target: $bin_dir/wago"

if [ "$dry_run" = "1" ]; then
	say "dry run: GOBIN=$bin_dir go install $pkg"
	exit 0
fi

mkdir -p "$bin_dir"
GOBIN="$bin_dir" go install "$pkg"

if [ -x "$bin_dir/wago" ]; then
	"$bin_dir/wago" version
else
	say "installed $bin_dir/wago"
fi

case ":$PATH:" in
	*":$bin_dir:"*) ;;
	*) say "add $bin_dir to PATH to run: wago" ;;
esac
