#!/usr/bin/env sh
# Emit the "Build size" section fragment for the CI card: the size of the
# size-minimized TinyGo release CLI (`make build-release`), with a delta vs
# CARD_BASELINE_REF (e.g. origin/main) built in a throwaway worktree. Fragment
# format matches the other producers: line 1 is the <summary>, the rest is the
# body. Degrades to a placeholder when TinyGo is unavailable.
set -eu

report="${SIZE_REPORT:-ci-card/size.md}"
baseline_ref="${CARD_BASELINE_REF:-}"

root=$(git rev-parse --show-toplevel) || {
	printf 'wago: not inside a git repository\n' >&2
	exit 1
}
cd "$root"

placeholder() {
	printf 'Build size — _%s_\n\n_No data._\n' "$1" >"$report"
	printf 'Build size — %s\n' "$1"
	exit 0
}

command -v tinygo >/dev/null 2>&1 || placeholder "TinyGo not installed"

# build_size <dir>: build the release CLI in dir and echo its byte size (empty on
# failure). The binary is written as <dir>/wago by `make build-release`.
build_size() {
	( cd "$1" && make build-release >/dev/null 2>&1 && wc -c < wago ) 2>/dev/null | tr -d ' '
}

human() {
	awk -v b="$1" 'BEGIN { if (b>=1048576) printf "%.2f MB", b/1048576; else printf "%.0f KB", b/1024 }'
}

cur=$(build_size "$root"); cur=${cur:-0}
[ "$cur" -gt 0 ] || placeholder "release build failed"

have_base=0
if [ -n "$baseline_ref" ] && git rev-parse --verify -q "$baseline_ref^{commit}" >/dev/null 2>&1; then
	wt=$(mktemp -d)
	if git worktree add --detach -q "$wt" "$baseline_ref" 2>/dev/null; then
		base=$(build_size "$wt"); base=${base:-0}
		[ "$base" -gt 0 ] && have_base=1
	fi
	git worktree remove --force "$wt" 2>/dev/null || true
fi

summary="Build size: $(human "$cur") (TinyGo release CLI)"
if [ "$have_base" = 1 ]; then
	d=$((cur - base))
	dkb=$(awk -v d="$d" 'BEGIN { printf (d==0 ? "—" : "%+.1f KB"), d/1024 }')
	[ "$dkb" != "—" ] && summary="$summary (Δ ${dkb} vs main)"
	body=$(printf '| Binary | Size | Δ vs main |\n|---|---|---|\n| wago (TinyGo release, stripped) | %s | %s |\n' "$(human "$cur")" "$dkb")
else
	body=$(printf '| Binary | Size |\n|---|---|\n| wago (TinyGo release, stripped) | %s |\n' "$(human "$cur")")
fi

printf '%s\n%s\n' "$summary" "$body" >"$report"
printf '%s\n' "$summary"
