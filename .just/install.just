set working-directory := '..'

# Run the installer from this checkout.
[default]
manager:
    go run ./cli/wago-installer install

# Atomically install this checkout as the global canary manager and runtime.
runtime:
    #!/usr/bin/env bash
    set -euo pipefail
    root="${WAGO_HOME:-$HOME/.wago}"
    runtime_root="$root"
    [[ -z "${WAGO_HOME:-}" ]] || runtime_root="$root/data"
    runtime_dir="$runtime_root/versions/canary/standard/normal"
    manager_dir="$root/bin"
    runtime="$runtime_dir/wago-runtime$(go env GOEXE)"
    manager="$manager_dir/wago$(go env GOEXE)"
    build="canary-local-$(git rev-parse --short HEAD)"
    mkdir -p "$runtime_dir" "$manager_dir"
    staged_runtime="$runtime_dir/.wago-runtime-local-$$"
    staged_manager="$manager_dir/.wago-local-$$"
    trap 'rm -f "$staged_runtime" "$staged_manager"' EXIT
    CGO_ENABLED=0 go build -tags wago_runtime -ldflags '-s -w -X main.version=canary' -o "$staged_runtime" ./cli/wago
    CGO_ENABLED=0 go build -ldflags '-s -w -X main.version=canary' -o "$staged_manager" ./cli/wago
    chmod 755 "$staged_runtime" "$staged_manager"
    mv -f "$staged_runtime" "$runtime"
    mv -f "$staged_manager" "$manager"
    trap - EXIT
    echo "installed $build at $manager and $runtime"

# Install this repository's git hooks.
hooks:
    scripts/install-hooks.sh
