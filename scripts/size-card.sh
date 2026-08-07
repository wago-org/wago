#!/usr/bin/env bash
# Build the supported release profiles, enforce target-specific byte budgets,
# and emit both the CI-card fragment and machine-readable symbol attribution.
set -euo pipefail

report="${SIZE_REPORT:-ci-card/size.md}"
baseline_ref="${CARD_BASELINE_REF:-}"
target_os="${SIZE_GOOS:-linux}"
target_arch="${SIZE_GOARCH:-amd64}"

root=$(git rev-parse --show-toplevel) || {
  printf 'wago: not inside a git repository\n' >&2
  exit 1
}
cd "$root"
mkdir -p "$(dirname "$report")"

profile_tsv="${SIZE_PROFILE_REPORT:-$(dirname "$report")/size-profiles.tsv}"
symbol_tsv="${SIZE_SYMBOL_REPORT:-$(dirname "$report")/size-symbols.tsv}"
budgets="$root/scripts/release-size-budgets.tsv"
build_tmp=$(mktemp -d)
baseline_tmp=""
cleanup() {
  if [[ -n "$baseline_tmp" ]]; then
    git worktree remove --force "$baseline_tmp" >/dev/null 2>&1 || true
  fi
  rm -rf -- "$build_tmp"
}
trap cleanup EXIT

human() {
  awk -v b="$1" 'BEGIN { if (b>=1048576) printf "%.2f MiB", b/1048576; else printf "%.0f KiB", b/1024 }'
}

budget_for() {
  awk -F '\t' -v name="$1" '$1 == name { print $2; exit }' "$budgets"
}

# profile_spec prints: name, tags, package, toolchain.
profile_specs() {
  printf '%s|%s|%s|%s\n' \
    manager '' ./cli/wago go \
    runtime-standard wago_runtime ./cli/wago go \
    runtime-minimal wago_runtime,wago_minimal ./cli/wago go \
    runtime-minimal-tiny wago_runtime,wago_lean,wago_minimal ./cli/wago tinygo
}

build_profile() {
  local dir="$1" name="$2" tags="$3" package="$4" toolchain="$5" output="$6" symbols="${7:-false}"
  local -a args
  if [[ "$toolchain" == go ]]; then
    local ldflags='-s -w -X main.version=0.0.0'
    if [[ "$symbols" == true ]]; then
      ldflags='-w -X main.version=0.0.0'
    fi
    args=(build -buildvcs=false -trimpath -ldflags="$ldflags")
    if [[ -n "$tags" ]]; then
      args+=(-tags "$tags")
    fi
    (cd "$dir" && CGO_ENABLED=0 GOOS="$target_os" GOARCH="$target_arch" go "${args[@]}" -o "$output" "$package")
    return
  fi
  command -v tinygo >/dev/null 2>&1 || return 2
  args=(build -scheduler=tasks -no-debug -opt=z -gc=conservative -ldflags '-X main.version=0.0.0')
  if [[ -n "$tags" ]]; then
    args+=(-tags "$tags")
  fi
  (cd "$dir" && GOOS="$target_os" GOARCH="$target_arch" tinygo "${args[@]}" -o "$output" "$package")
  if [[ "$target_os" == linux ]]; then
    if command -v strip >/dev/null 2>&1 && strip --help 2>&1 | grep -q -- '--strip-section-headers'; then
      strip -s --strip-section-headers --remove-section=.eh_frame --remove-section=.eh_frame_hdr --remove-section=.comment "$output"
    elif command -v llvm-strip >/dev/null 2>&1; then
      llvm-strip --strip-sections "$output"
    elif [[ -x /opt/homebrew/opt/llvm/bin/llvm-strip ]]; then
      /opt/homebrew/opt/llvm/bin/llvm-strip --strip-sections "$output"
    fi
  fi
}

printf 'profile\ttarget\tbytes\tbudget_bytes\tdelta_bytes\n' >"$profile_tsv"
printf 'profile\trank\tbytes\ttype\tsymbol\n' >"$symbol_tsv"

have_baseline=false
if [[ -n "$baseline_ref" ]] && git rev-parse --verify -q "$baseline_ref^{commit}" >/dev/null; then
  baseline_tmp=$(mktemp -d)
  if git worktree add --detach -q "$baseline_tmp" "$baseline_ref"; then
    have_baseline=true
  fi
fi

rows=""
failures=0
profiles=0
while IFS='|' read -r name tags package toolchain; do
  current="$build_tmp/$name"
  if ! build_profile "$root" "$name" "$tags" "$package" "$toolchain" "$current"; then
    if [[ "$toolchain" == tinygo && ! -x "$(command -v tinygo 2>/dev/null || true)" ]]; then
      rows+="| $name | unavailable | — | — |\n"
      continue
    fi
    printf 'wago: failed to build release size profile %s\n' "$name" >&2
    exit 1
  fi
  bytes=$(wc -c <"$current" | tr -d ' ')
  budget=$(budget_for "$name")
  [[ -n "$budget" ]] || { printf 'wago: missing size budget for %s\n' "$name" >&2; exit 1; }
  delta=""
  if [[ "$have_baseline" == true ]]; then
    baseline="$build_tmp/base-$name"
    if build_profile "$baseline_tmp" "$name" "$tags" "$package" "$toolchain" "$baseline"; then
      base_bytes=$(wc -c <"$baseline" | tr -d ' ')
      delta=$((bytes - base_bytes))
    fi
  fi
  printf '%s\t%s/%s\t%s\t%s\t%s\n' "$name" "$target_os" "$target_arch" "$bytes" "$budget" "$delta" >>"$profile_tsv"
  budget_text="$(human "$budget")"
  headroom=$((budget - bytes))
  delta_text="—"
  if [[ -n "$delta" ]]; then
    delta_text=$(awk -v d="$delta" 'BEGIN { printf (d==0 ? "—" : "%+.1f KiB"), d/1024 }')
  fi
  rows+="| $name | $(human "$bytes") | $delta_text | $budget_text ($(human "$headroom") free) |\n"
  profiles=$((profiles + 1))
  if (( bytes > budget )); then
    failures=$((failures + 1))
  fi

  if [[ "$toolchain" == go ]]; then
    attributed="$build_tmp/$name-symbols"
    build_profile "$root" "$name" "$tags" "$package" "$toolchain" "$attributed" true
    rank=0
    while read -r _address symbol_bytes symbol_type symbol_name; do
      rank=$((rank + 1))
      printf '%s\t%s\t%s\t%s\t%s\n' "$name" "$rank" "$symbol_bytes" "$symbol_type" "$symbol_name" >>"$symbol_tsv"
    done < <(go tool nm -size -sort size "$attributed" | awk 'NR <= 25')
  fi
done < <(profile_specs)

summary="Build sizes: $profiles profiles within budget"
if (( failures != 0 )); then
  summary="Build sizes: $failures of $profiles profiles exceed budget"
fi
{
  printf '%s\n\n' "$summary"
  printf '| Profile | Size | Delta vs main | Budget |\n'
  printf '|---|---:|---:|---:|\n'
  printf '%b' "$rows"
  # Backticks are Markdown literals; target substitution is through printf's %s.
  # shellcheck disable=SC2016
  printf '\nTarget: `%s/%s`; stripped, `-trimpath`, `-buildvcs=false`. Top-symbol data: `size-symbols.tsv`.\n' "$target_os" "$target_arch"
} >"$report"
printf '%s\n' "$summary"

if (( failures != 0 )); then
  exit 1
fi
