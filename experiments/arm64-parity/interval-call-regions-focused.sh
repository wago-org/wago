#!/bin/sh
set -eu
export WAGO_SHARED_SCALAR=0 WAGO_ARM64_EXPERIMENT_INTERVAL_CALL_REGIONS=0
export WAGO_PARITY_CACHE=/Users/work/Code/Web/wasm.fyi/.wasmbench/corpus-cache WAGO_PARITY_CORPUS=/Users/work/Code/Web/wasm.fyi/corpora/applications
rg -q '^PASS$' experiments/arm64-parity/interval-call-regions-core.txt
rg -q '^PASS$' experiments/arm64-parity/interval-call-regions-core-explicit.txt
test -s experiments/arm64-parity/interval-call-regions-native-changes.txt
index=0
for state in 1 0 0 1; do
 for id in wago/fastfloat/decimal-parse wago/pcre2/compile-match; do
  export WAGO_PARITY_CORE_ID="$id"
  name=$(printf '%s' "$id" | tr / _)
  WAGO_ARM64_EXPERIMENT_INTERVAL_CALL_REGIONS="$state" /tmp/parity-interval-call-regions.test -test.run=^$ -test.bench='BenchmarkCachedContract/(Compile|Exec)$' -test.benchtime=600ms -test.count=3 > "experiments/arm64-parity/interval-call-regions-$name-$index-$state.txt" 2>&1
 done
 index=$((index+1))
done
go build -tags wago_guardpage -o /tmp/paired-interval-call-regions ./experiments/arm64-parity/paired
/tmp/paired-interval-call-regions -core-compile -corpus "$WAGO_PARITY_CACHE" -option interval-call-regions -workloads decimal-parse,compile-match -phase compile -rounds 10 -budget 250ms > experiments/arm64-parity/interval-call-regions-compile-pairs.jsonl 2>&1
go build -tags wago_runtime,wago_profile,wago_guardpage -o /tmp/wago-interval-call-regions-prof ./cli/wago
artifact=$(python3 -c 'import json,os;from pathlib import Path;p=Path(os.environ["WAGO_PARITY_CACHE"]);w=next(w for w in json.loads((p/"manifest.json").read_text())["workloads"] if w["id"]=="wago/fastfloat/decimal-parse");print(p/w["artifact"])')
WAGO_ARM64_EXPERIMENT_INTERVAL_CALL_REGIONS=1 /tmp/wago-interval-call-regions-prof profile record --module "$artifact" --export fastfloat_run --want 17556528949095414306 --bounds signals --mode prepared --duration 3s --backend samply --samply /opt/homebrew/bin/samply --rate 1000 --include-code --source-maps --out experiments/arm64-parity/profile-interval-call-regions-fastfloat > experiments/arm64-parity/profile-interval-call-regions-fastfloat.txt 2>&1
/tmp/wago-profile-tools/bin/python experiments/arm64-parity/sampled_assembly.py experiments/arm64-parity/profile-interval-call-regions-fastfloat > experiments/arm64-parity/interval-call-regions-fastfloat-sampled-assembly.txt
