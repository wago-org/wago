#!/bin/sh
set -eu
export WAGO_SHARED_SCALAR=0 WAGO_ARM64_EXPERIMENT_INTERVAL_CALL_REGIONS=0
go test -tags wago_codegenstats,wago_regalloccheck,wago_guardpage ./src/core/compiler/backend/railshot/arm64 ./src/core/compiler/optimization > experiments/arm64-parity/interval-call-regions-tests-retry.txt 2>&1
sh experiments/arm64-parity/interval-call-regions-qualify.sh
sh experiments/arm64-parity/interval-call-regions-focused.sh
