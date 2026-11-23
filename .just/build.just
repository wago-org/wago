set working-directory := '..'

tinygo := env('TINYGO', 'tinygo')
tinygo_opt := env('TINYGO_OPT', 'z')
tinygo_stack_size := env('TINYGO_STACK_SIZE', '256KB')

# Build the runtime-independent manager with Go.
[default]
manager version=env('WAGO_VERSION', '0.0.0'):
    CGO_ENABLED=0 go build -ldflags '-s -w -X main.version={{ version }}' -o wago ./cli/wago

# Build a runtime: `just build runtime [standard|minimal] [go|tinygo]`.
runtime profile='standard' compiler='go' version=env('WAGO_VERSION', '0.0.0') scheduler=env('TINYGO_SCHEDULER', 'tasks'):
    #!/usr/bin/env bash
    set -euo pipefail
    case '{{ profile }}' in standard) tags=wago_runtime ;; minimal) tags=wago_runtime,wago_lean,wago_minimal ;; *) echo 'profile must be standard or minimal' >&2; exit 2 ;; esac
    case '{{ compiler }}' in
      go)
        CGO_ENABLED=0 go build -tags "$tags" -ldflags '-s -w -X main.version={{ version }}' -o 'wago-runtime-{{ profile }}-normal' ./cli/wago
        ;;
      tinygo)
        build_args=(build)
        if [[ "$(go env GOOS)/$(go env GOARCH)" == darwin/arm64 ]]; then
          build_args+=(-llvm-features=+lse)
        fi
        {{ tinygo }} "${build_args[@]}" -scheduler='{{ scheduler }}' -stack-size='{{ tinygo_stack_size }}' -no-debug -opt='{{ tinygo_opt }}' -gc=conservative -tags "$tags" -ldflags '-X main.version={{ version }}' -o 'wago-runtime-{{ profile }}-tiny' ./cli/wago
        ;;
      *) echo 'compiler must be go or tinygo' >&2; exit 2 ;;
    esac

# Build the manager and all supported runtime profiles.
release version=env('WAGO_VERSION', '0.0.0'):
    GOOS="$(go env GOOS)" GOARCH="$(go env GOARCH)" WAGO_VERSION='{{ version }}' scripts/build-release-assets.sh

# Build the diagnostic run-only Minimal/TinyGo runtime.
engine version=env('WAGO_VERSION', '0.0.0') scheduler=env('TINYGO_SCHEDULER', 'tasks'):
    #!/usr/bin/env bash
    set -euo pipefail
    build_args=(build)
    if [[ "$(go env GOOS)/$(go env GOARCH)" == darwin/arm64 ]]; then
      build_args+=(-llvm-features=+lse)
    fi
    {{ tinygo }} "${build_args[@]}" -scheduler='{{ scheduler }}' -stack-size='{{ tinygo_stack_size }}' -no-debug -opt=z -gc=conservative -tags wago_runtime,wago_lean,wago_minimal -ldflags '-X main.version={{ version }}' -o wago-engine ./cli/wago
    if [[ "$(go env GOOS)" == linux ]]; then
      strip -s --strip-section-headers --remove-section=.eh_frame --remove-section=.eh_frame_hdr --remove-section=.comment wago-engine
    fi
    echo "wago-engine {{ version }}: $(du -h wago-engine | cut -f1)"

# Build the debug Minimal/TinyGo runtime used by CI.
tinygo compiler=env('TINYGO', 'tinygo') scheduler=env('TINYGO_SCHEDULER', 'tasks'):
    {{ compiler }} build -scheduler='{{ scheduler }}' -stack-size='{{ tinygo_stack_size }}' -tags wago_runtime,wago_lean,wago_minimal -o wago-tinygo ./cli/wago
