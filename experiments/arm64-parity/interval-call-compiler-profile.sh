#!/bin/sh
set -eu
export WAGO_SHARED_SCALAR=0 WAGO_ARM64_EXPERIMENT_INTERVAL_CALL_REGIONS=1
export WAGO_PARITY_CACHE=/Users/work/Code/Web/wasm.fyi/.wasmbench/corpus-cache WAGO_PARITY_CORE_ID=wago/fastfloat/decimal-parse
rg -q '^PASS$' experiments/arm64-parity/interval-call-packed-core.txt
for variant in next-use packed; do
 binary="/tmp/parity-interval-call-$variant.test"
 profile="experiments/arm64-parity/interval-call-compiler-$variant.pprof"
 "$binary" -test.run=^$ -test.bench='BenchmarkCachedContract/Compile$' -test.benchtime=3s -test.cpuprofile="$profile" > "experiments/arm64-parity/interval-call-compiler-$variant.txt" 2>&1
 go tool pprof -top -nodecount=25 "$binary" "$profile" > "experiments/arm64-parity/interval-call-compiler-$variant-top.txt" 2>&1
done
go build -tags wago_runtime,wago_profile,wago_guardpage -o /tmp/wago-interval-call-packed-prof ./cli/wago
artifact=$(python3 -c 'import json,os;from pathlib import Path;p=Path(os.environ["WAGO_PARITY_CACHE"]);w=next(w for w in json.loads((p/"manifest.json").read_text())["workloads"] if w["id"]=="wago/fastfloat/decimal-parse");print(p/w["artifact"])')
/tmp/wago-interval-call-packed-prof profile record --module "$artifact" --export fastfloat_run --want 17556528949095414306 --bounds signals --mode prepared --duration 3s --backend samply --samply /opt/homebrew/bin/samply --rate 1000 --include-code --source-maps --out experiments/arm64-parity/profile-interval-call-next-use-fastfloat > experiments/arm64-parity/profile-interval-call-next-use-fastfloat.txt 2>&1
/tmp/wago-profile-tools/bin/python experiments/arm64-parity/sampled_assembly.py experiments/arm64-parity/profile-interval-call-next-use-fastfloat > experiments/arm64-parity/interval-call-next-use-fastfloat-sampled-assembly.txt
