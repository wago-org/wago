#!/usr/bin/env bash
set -euo pipefail

release_dir="${1:-.}"
expected_target="${2:-}"
cd "$release_dir"

if [[ -n "$expected_target" ]]; then
  managers=("wago-${expected_target}")
else
  managers=()
  while IFS= read -r manager; do
    managers[${#managers[@]}]="$manager"
  done < <(
      find . -maxdepth 1 -type f -name 'wago-*' \
        ! -name 'wago-runtime-*' ! -name '*.sha256' \
        -print | sed 's|^\./||' | sort
    )
fi

if [[ "${#managers[@]}" -eq 0 ]]; then
  echo "no Wago CLI assets found" >&2
  exit 1
fi

for manager in "${managers[@]}"; do
  target="${manager#wago-}"
  assets=(
    "$manager"
    "wago-runtime-standard-normal-${target}"
    "wago-runtime-lite-normal-${target}"
    "wago-runtime-minimal-normal-${target}"
  )
  tiny_assets=(
    "wago-runtime-standard-tiny-${target}"
    "wago-runtime-lite-tiny-${target}"
    "wago-runtime-minimal-tiny-${target}"
  )
  # TinyGo supports the subprocesses needed by Standard/Lite on Linux. Other
  # hosts still publish every Tiny profile their TinyGo port can build (notably
  # Minimal) without sacrificing the complete Normal set.
  if [[ "$target" == linux-* ]]; then
    assets+=("${tiny_assets[@]}")
  else
    for asset in "${tiny_assets[@]}"; do
      if [[ -s "$asset" ]]; then
        assets+=("$asset")
      fi
    done
  fi
  for asset in "${assets[@]}"; do
    if [[ ! -s "$asset" ]]; then
      echo "missing release asset: $asset" >&2
      exit 1
    fi
    if [[ ! -s "${asset}.sha256" ]]; then
      echo "missing release checksum: ${asset}.sha256" >&2
      exit 1
    fi
    if command -v sha256sum >/dev/null 2>&1; then
      sha256sum --check "${asset}.sha256"
    else
      shasum -a 256 --check "${asset}.sha256"
    fi
  done
done

printf 'verified %d platform set(s): CLI + complete Normal runtimes and supported Tiny runtimes\n' "${#managers[@]}"
