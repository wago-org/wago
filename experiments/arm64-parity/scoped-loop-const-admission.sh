#!/bin/sh
set -eu
export WAGO_CONST_PROBE=/Users/work/Code/Web/wasm.fyi/.wasmbench/corpus-cache/artifacts/6eb172b5da57c4d0a0ff194def7179f022847d09345fd975ba6dcdd827bd7265.wasm
WAGO_ARM64_EXPERIMENT_SCOPED_LOOP_CONST=1 go test -tags wago_codegenstats,wago_regalloccheck,wago_guardpage ./src/core/compiler/backend/railshot/arm64 -run "^TestScopedConstCorpusLoops$" -v > experiments/arm64-parity/scoped-loop-const-admission.txt 2>&1
