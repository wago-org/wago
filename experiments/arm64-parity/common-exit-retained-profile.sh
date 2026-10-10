#!/bin/sh
set -eu
export WAGO_SHARED_SCALAR=0
unset WAGO_ARM64_EXPERIMENT_COMMON_EXIT_COMPARE WAGO_ARM64_NO_COMMON_EXIT_COMPARE
go build -tags wago_runtime,wago_profile,wago_guardpage -o /tmp/wago-common-exit-retained-profile ./cli/wago
for spec in 'compiler-register-allocation 2048 1790173339' 'vision-dilation 128 633963730'; do
 set -- $spec
 /tmp/wago-common-exit-retained-profile profile record --module "/Users/work/Code/Web/wasm.fyi/corpora/applications/artifacts/$1.wasm" --export benchmark --args "$2" --want "$3" --bounds signals --mode prepared --duration 3s --backend samply --samply /opt/homebrew/bin/samply --rate 1000 --include-code --source-maps --out "experiments/arm64-parity/profile-common-exit-retained-$1" > "experiments/arm64-parity/profile-common-exit-retained-$1.txt" 2>&1
 /tmp/wago-profile-tools/bin/python experiments/arm64-parity/sampled_assembly.py "experiments/arm64-parity/profile-common-exit-retained-$1" > "experiments/arm64-parity/profile-common-exit-retained-$1-assembly.txt"
done
