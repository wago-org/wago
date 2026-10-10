#!/bin/sh
set -eu
export WAGO_SHARED_SCALAR=0 WAGO_PARITY_CORPUS=/Users/work/Code/Web/wasm.fyi/corpora/applications
export WAGO_PARITY_CACHE=/Users/work/Code/Web/wasm.fyi/.wasmbench/corpus-cache WAGO_PARITY_CORE_ID=wago/linked_list/sum
go build -tags wago_runtime,wago_profile,wago_guardpage -o /tmp/wago-constant-shift-prof ./cli/wago
artifact=$(python3 -c 'import json,os;from pathlib import Path;p=Path(os.environ["WAGO_PARITY_CACHE"]);w=next(w for w in json.loads((p/"manifest.json").read_text())["workloads"] if w["id"]=="wago/linked_list/sum");print(p/w["artifact"])')
for variant in before after; do
 binary=/tmp/wago-store-probe-prof
 if [ "$variant" = after ]; then binary=/tmp/wago-constant-shift-prof; fi
 "$binary" profile record --module "$artifact" --export sum --args 4096 --want 8386560 --bounds signals --mode prepared --duration 3s --backend samply --samply /opt/homebrew/bin/samply --rate 1000 --include-code --source-maps --out "experiments/arm64-parity/profile-linked-shift-$variant" > "experiments/arm64-parity/profile-linked-shift-$variant.txt" 2>&1
 /tmp/wago-profile-tools/bin/python experiments/arm64-parity/sampled_assembly.py "experiments/arm64-parity/profile-linked-shift-$variant" > "experiments/arm64-parity/linked-shift-$variant-sampled-assembly.txt"
done
control=src/core/compiler/backend/railshot/arm64/control.go
cp "$control" /tmp/constant-shift-control-original.go
trap 'cp /tmp/constant-shift-control-original.go "$control"' EXIT INT TERM
python3 - <<'EDIT'
from pathlib import Path
p=Path('src/core/compiler/backend/railshot/arm64/control.go');s=p.read_text()
needle='\tf.alignCode(loopAlign)\n\tif loopAlign >= 4 && !f.interruptible && f.nLocals <= pollFreeLoopPhaseMaxLocals {'
replacement='\tif loopAlign == 4 && !f.interruptible && f.nLocals <= pollFreeLoopPhaseMaxLocals {\n\t\tf.alignCode(6)\n\t\treturn\n\t}\n'+needle
assert needle in s
s=s.replace(needle,replacement);p.write_text(s)
Path('experiments/arm64-parity/constant-shift-layout64-trial.go.txt').write_text(replacement)
EDIT
go test -tags wago_guardpage -c -o /tmp/parity-constant-shift-layout64.test ./experiments/arm64-parity
cp /tmp/constant-shift-control-original.go "$control"
WAGO_PARITY_CODE_DIR=/tmp/constant-shift-layout64 /tmp/parity-constant-shift-layout64.test -test.run=^$ -test.bench='BenchmarkParity/.*/Exec$' -test.benchtime=1x > experiments/arm64-parity/constant-shift-layout64-oracles.txt 2>&1
/tmp/parity-constant-shift-layout64.test -test.run='^TestCachedCoreOracles$' > experiments/arm64-parity/constant-shift-layout64-core-oracles.txt 2>&1
index=0
for variant in plain aligned aligned plain; do
 binary=/tmp/parity-constant-shift-core-after.test
 if [ "$variant" = aligned ]; then binary=/tmp/parity-constant-shift-layout64.test; fi
 "$binary" -test.run=^$ -test.bench='BenchmarkCachedSimple/(Compile|Exec)$' -test.benchtime=300ms -test.count=2 > "experiments/arm64-parity/constant-shift-layout64-linked-$index-$variant.txt" 2>&1
 "$binary" -test.run=^$ -test.bench='BenchmarkParity/(audio-adpcm|audio-fir|vision-components|compiler-register-allocation|search-aho-corasick|video-dct)/(Compile|Exec)$' -test.benchtime=200ms -test.count=2 > "experiments/arm64-parity/constant-shift-layout64-$index-$variant.txt" 2>&1
 index=$((index+1))
done
