set working-directory := '..'

# Install the manager built from this checkout's HEAD.
[default]
manager:
    #!/usr/bin/env bash
    set -euo pipefail
    stage=$(mktemp -d)
    trap 'rm -rf "$stage"' EXIT
    source_dir="${WAGO_MANAGER_SOURCE:-}"
    if [[ -z "$source_dir" ]]; then
        source_dir="$stage/source"
        mkdir -p "$source_dir"
        git archive HEAD | tar -xf - -C "$source_dir"
    fi
    source_sha=$(git rev-parse HEAD)
    install_version="${WAGO_VERSION:-canary@$source_sha}"
    manager_path="${WAGO_MANAGER_PATH:-}"
    if [[ -z "$manager_path" ]]; then
        manager_path="$stage/wago$(go env GOEXE)"
        (cd "$source_dir" && CGO_ENABLED=0 go build -trimpath -ldflags "-s -w -X main.version=$install_version" -o "$manager_path" ./cli/wago)
    fi
    WAGO_VERSION="$install_version" \
        WAGO_MANAGER_PATH="$manager_path" \
        WAGO_MANAGER_SOURCE="$source_dir" \
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
