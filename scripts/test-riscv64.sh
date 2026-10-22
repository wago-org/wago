#!/usr/bin/env bash
set -euo pipefail

repo=$(cd -- "$(dirname -- "$0")/.." && pwd)
cd "$repo"
if [[ ${WAGO_RV64_MISE_READY:-0} != 1 ]]; then
  exec mise exec -- env WAGO_RV64_MISE_READY=1 bash "$0" "$@"
fi
command -v qemu-riscv64 >/dev/null

tags=${WAGO_RV64_TEST_TAGS:-}
export GOOS=linux GOARCH=riscv64 GORISCV64=rva20u64 CGO_ENABLED=0
export GOMAXPROCS=${GOMAXPROCS:-2}
export WAGO_RV64_CLANG=${WAGO_RV64_CLANG:-$(command -v clang)}
fixtures="$repo/.tools/rv64-fixtures"
export WAGO_RV64_SPEC_JSON_DIR="$fixtures/simd"
export WAGO_RV64_RELAXED_SPEC_JSON_DIR="$fixtures/relaxed-simd"
for profile in numeric float memory control multimemory memory64 fac call; do
  variable="WAGO_RV64_${profile^^}_SPEC_JSON_DIR"
  export "$variable=$fixtures/$profile"
done

if [[ ${1:-} == --ci ]]; then
  mode=${2:-}
  case "$mode" in
    smoke|supported|public|artifacts|guards) ;;
    *) echo 'Usage: scripts/test-riscv64.sh --ci {smoke|supported|public|artifacts|guards}' >&2; exit 2 ;;
  esac
  export WAGO_RV64_TEST_RUNNER=qemu
  exec scripts/ci-riscv64.sh "$mode"
elif [[ ${1:-} == --spec ]]; then
  version=${2:-}
  case "$version" in
    1|2|3) ;;
    *) echo 'Usage: scripts/test-riscv64.sh --spec {1|2|3}' >&2; exit 2 ;;
  esac
  shift 2
  export WAGO_SPECTEST_DIR="$repo/tests/conformance/spec-v$version"
  export WAGO_SPEC_VERSION="$version.0"
  export WAGO_SPEC_REQUIRE_ZERO_GAPS=1
  if [[ $version == 1 ]]; then
    export WAGO_SPEC_ALL_FILES=1
  fi
  export WAGO_WAST2JSON
  WAGO_WAST2JSON=$(scripts/bootstrap-wabt.sh --print-path)
  export WAGO_WABT_VERSION=1.0.41
  if [[ $version == 3 ]]; then
    export WAGO_SPEC_INTERPRETER WAGO_SPEC_INTERPRETER_REVISION
    WAGO_SPEC_INTERPRETER=$(scripts/bootstrap-spec-interpreter.sh --print-path)
    WAGO_SPEC_INTERPRETER_REVISION=$(scripts/bootstrap-spec-interpreter.sh --print-revision)
  fi
  set -- -run '^TestSpecSuiteExec$' ./src/wago "$@"
elif [[ $# == 0 ]]; then
  set -- ./src/core/encoder/riscv64 ./src/core/runtime/riscv64 \
    ./src/core/compiler/backend/railshot/riscv64 ./src/wago
fi

exec go test -exec "$repo/scripts/qemu-riscv64.sh" -tags="$tags" \
  -count=1 -parallel=2 -timeout="${WAGO_RV64_TEST_TIMEOUT:-15m}" "$@"
