#!/usr/bin/env bash
set -euo pipefail
root=$(git rev-parse --show-toplevel)
cd "$root"
mkdir -p .tmp/setup-cleanup
git show 95be283fa511db7b01d86ef72b6c58fbe1ab607a:src/wago/imports.go > .tmp/setup-cleanup/imports-baseline.go
python3 - <<'PY'
from pathlib import Path
import json
root = Path.cwd()
tmp = root / '.tmp/setup-cleanup'
(tmp / 'baseline-overlay.json').write_text(json.dumps({'Replace': {
    str(root / 'src/wago/imports.go'): str(tmp / 'imports-baseline.go'),
}}))
PY
go test -c -overlay .tmp/setup-cleanup/baseline-overlay.json -tags wago_guardpage -o .tmp/setup-cleanup/baseline-diagnostic.test ./bench/suite
go test -c -tags wago_guardpage -o .tmp/setup-cleanup/candidate-diagnostic.test ./bench/suite
cp .tmp/setup-cleanup/baseline-diagnostic.test .tmp/setup-cleanup/baseline-suite.test
cp .tmp/setup-cleanup/candidate-diagnostic.test .tmp/setup-cleanup/candidate-suite.test
go test -c -overlay .tmp/setup-cleanup/baseline-overlay.json -tags wago_guardpage -o .tmp/setup-cleanup/baseline-wago.test ./src/wago
go test -c -tags wago_guardpage -o .tmp/setup-cleanup/candidate-wago.test ./src/wago
