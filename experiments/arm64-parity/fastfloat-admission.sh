#!/bin/sh
set -eu
export WAGO_SHARED_SCALAR=0
artifact=$(python3 -c 'import json;from pathlib import Path;p=Path("/Users/work/Code/Web/wasm.fyi/.wasmbench/corpus-cache");w=next(w for w in json.loads((p/"manifest.json").read_text())["workloads"] if w["id"]=="wago/fastfloat/decimal-parse");print(p/w["artifact"])')
WAGO_CONST_PROBE="$artifact" go test -tags wago_codegenstats,wago_guardpage ./src/core/compiler/backend/railshot/arm64 -run '^TestCorpusConstProbe$' -v > experiments/arm64-parity/fastfloat-admission.txt 2>&1
