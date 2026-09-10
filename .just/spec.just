set working-directory := '..'

root := justfile_directory()
spec1 := root + '/tests/conformance/spec-v1'
spec2 := root + '/tests/conformance/spec-v2'
spec3 := root + '/tests/conformance/spec-v3'

# Run every pinned WebAssembly spec suite.
[default]
all: v1 v2 v3

# Run the WebAssembly 1.0 core suite.
v1:
    #!/usr/bin/env bash
    set -euo pipefail
    command -v wast2json >/dev/null 2>&1 || { echo 'wast2json (wabt) not on PATH; install wabt'; exit 1; }
    test -f '{{ spec1 }}/i32.wast' || git submodule update --init tests/conformance/spec-v1
    WAGO_SPECTEST_DIR='{{ spec1 }}' WAGO_SPEC_VERSION=1.0 go test -count=1 -run TestSpecSuiteExec -v ./src/wago/

# Run the pinned WebAssembly 2.0 core suite.
v2:
    #!/usr/bin/env bash
    set -euo pipefail
    command -v wast2json >/dev/null 2>&1 || { echo 'wast2json (wabt) not on PATH; install wabt'; exit 1; }
    test -f '{{ spec2 }}/test/core/i32.wast' || git submodule update --init tests/conformance/spec-v2
    go test -count=1 -run '^TestCoreV2Validation$' -v ./src/core/compiler/wasm/
    go test -count=1 -run '^TestCoreV2SpecExecution$' -v ./src/wago/

[private]
wabt:
    scripts/bootstrap-wabt.sh --verify

[private]
interpreter:
    scripts/bootstrap-spec-interpreter.sh --verify

# Run the pinned WebAssembly 3.0 core suite.
v3: wabt interpreter
    #!/usr/bin/env bash
    set -euo pipefail
    wast2json="$(scripts/bootstrap-wabt.sh --print-path)"
    interpreter="$(scripts/bootstrap-spec-interpreter.sh --print-path)"
    interpreter_revision="$(scripts/bootstrap-spec-interpreter.sh --print-revision)"
    test -f '{{ spec3 }}/test/core/i32.wast' || git submodule update --init tests/conformance/spec-v3
    WAGO_WAST2JSON="$wast2json" WAGO_WABT_VERSION=1.0.41 \
    WAGO_SPEC_INTERPRETER="$interpreter" WAGO_SPEC_INTERPRETER_REVISION="$interpreter_revision" \
    WAGO_SPECTEST_DIR='{{ spec3 }}' WAGO_SPEC_VERSION=3.0 \
      go test -count=1 -run TestSpecSuiteExec -v ./src/wago/

# Run WebAssembly 3.0 with signal-backed bounds.
v3-signals: wabt interpreter
    #!/usr/bin/env bash
    set -euo pipefail
    wast2json="$(scripts/bootstrap-wabt.sh --print-path)"
    interpreter="$(scripts/bootstrap-spec-interpreter.sh --print-path)"
    interpreter_revision="$(scripts/bootstrap-spec-interpreter.sh --print-revision)"
    test -f '{{ spec3 }}/test/core/i32.wast' || git submodule update --init tests/conformance/spec-v3
    WAGO_BOUNDS=signals WAGO_WAST2JSON="$wast2json" WAGO_WABT_VERSION=1.0.41 \
    WAGO_SPEC_INTERPRETER="$interpreter" WAGO_SPEC_INTERPRETER_REVISION="$interpreter_revision" \
    WAGO_SPECTEST_DIR='{{ spec3 }}' WAGO_SPEC_VERSION=3.0 \
      go test -tags wago_guardpage -count=1 -run TestSpecSuiteExec -v ./src/wago/

# Refresh the committed WebAssembly 3.0 failure baseline.
baseline:
    scripts/spec3-baseline.sh

# Run the official SIMD proposal execution suite.
simd:
    #!/usr/bin/env bash
    set -euo pipefail
    command -v wast2json >/dev/null 2>&1 || { echo 'wast2json (wabt) not on PATH; install wabt'; exit 1; }
    test -f '{{ spec1 }}/proposals/simd/simd_address.wast' || git submodule update --init tests/conformance/spec-v1
    WAGO_SPECTEST_DIR='{{ spec1 }}' WAGO_SPEC_VERSION=simd go test -count=1 -run TestSpecSuiteExec -v ./src/wago/
