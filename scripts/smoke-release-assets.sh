#!/usr/bin/env bash
set -euo pipefail

release_dir="${1:?release directory is required}"
target="${2:?release target is required}"
version="${3:?release version is required}"
repository_root=$(git rev-parse --show-toplevel)

[[ "$version" =~ ^v(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)$ ]] || {
  echo "invalid stable version: $version" >&2
  exit 1
}

manager="$release_dir/wago-$target"
installer="$release_dir/wago-installer-$target"
standard="$release_dir/wago-runtime-standard-normal-$target"
minimal="$release_dir/wago-runtime-minimal-normal-$target"
for asset in "$manager" "$installer" "$standard" "$minimal"; do
  [[ -s "$asset" && ! -L "$asset" ]] || {
    echo "missing smoke-test asset: $asset" >&2
    exit 1
  }
done

scratch=$(mktemp -d)
trap 'rm -rf "$scratch"' EXIT
export HOME="$scratch/home"
export WAGO_HOME="$scratch/wago"
export XDG_CONFIG_HOME="$scratch/config"
export XDG_CACHE_HOME="$scratch/cache"
export XDG_DATA_HOME="$scratch/data"
export NO_COLOR=1
version_pattern=${version//./\\.}

manager_version=$("$manager" --json --version)
grep -Eq '"managerVersion"[[:space:]]*:[[:space:]]*"'"$version_pattern"'"' <<<"$manager_version" || {
  echo "manager does not report release version $version" >&2
  exit 1
}
[[ "$("$installer" --version)" == "$version" ]] || {
  echo "installer does not report release version $version" >&2
  exit 1
}

fixture="$repository_root/tests/fixtures/wasm/fib.wasm"
for runtime in "$standard" "$minimal"; do
  runtime_version=$("$runtime" --json --version)
  grep -Eq '"release"[[:space:]]*:[[:space:]]*"'"$version_pattern"'"' <<<"$runtime_version" || {
    echo "$(basename "$runtime") does not report release version $version" >&2
    exit 1
  }
  output=$("$runtime" run --invoke fib "$fixture" 20)
  [[ "$output" == "fib(20) = 6765" ]] || {
    echo "$(basename "$runtime") raw wasm smoke output: $output" >&2
    exit 1
  }
done

artifact="$scratch/fib.wago"
"$standard" build --output "$artifact" "$fixture" >/dev/null
[[ -s "$artifact" ]] || {
  echo "release runtime did not produce a .wago artifact" >&2
  exit 1
}
output=$("$standard" run --invoke fib "$artifact" 20)
[[ "$output" == "fib(20) = 6765" ]] || {
  echo "release runtime .wago smoke output: $output" >&2
  exit 1
}

for runtime in "$release_dir"/wago-runtime-*-tiny-"$target"; do
  [[ -e "$runtime" ]] || continue
  runtime_version=$("$runtime" --json --version)
  grep -Eq '"release"[[:space:]]*:[[:space:]]*"'"$version_pattern"'"' <<<"$runtime_version" || {
    echo "$(basename "$runtime") does not report release version $version" >&2
    exit 1
  }
done

printf 'smoke-tested qualified release assets for %s at %s\n' "$target" "$version"
