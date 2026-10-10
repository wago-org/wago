#!/bin/sh
set -eu
export WAGO_SHARED_SCALAR=0 WAGO_PARITY_CACHE=/Users/work/Code/Web/wasm.fyi/.wasmbench/corpus-cache
for id in wago/fastfloat/decimal-parse wago/pcre2/compile-match wago/xxhash/xxh64-boundaries; do
 export WAGO_PARITY_CORE_ID="$id"
 name=$(printf '%s' "$id" | tr / _)
 /tmp/parity-constant-shift-core-after.test -test.run=^$ -test.bench='BenchmarkCachedSimple/(Compile|Exec)$' -test.benchtime=300ms -test.count=3 > "experiments/arm64-parity/upstream-focused-$name.txt" 2>&1
done
artifact=$(python3 -c 'import json,os;from pathlib import Path;p=Path(os.environ["WAGO_PARITY_CACHE"]);w=next(w for w in json.loads((p/"manifest.json").read_text())["workloads"] if w["id"]=="wago/fastfloat/decimal-parse");print(p/w["artifact"])')
/tmp/wago-store-probe-prof profile record --module "$artifact" --export fastfloat_run --want 17556528949095414306 --bounds signals --mode prepared --duration 3s --backend samply --samply /opt/homebrew/bin/samply --rate 1000 --include-code --source-maps --out experiments/arm64-parity/profile-upstream-fastfloat > experiments/arm64-parity/profile-upstream-fastfloat.txt 2>&1
/tmp/wago-profile-tools/bin/python experiments/arm64-parity/sampled_assembly.py experiments/arm64-parity/profile-upstream-fastfloat > experiments/arm64-parity/upstream-fastfloat-sampled-assembly.txt
