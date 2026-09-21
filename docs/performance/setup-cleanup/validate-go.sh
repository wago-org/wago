#!/usr/bin/env bash
set -euo pipefail
export PATH=/home/jtenner/.local/share/mise/installs/github-web-assembly-wabt/1.0.41/bin:/home/jtenner/.local/share/mise/installs/github-web-assembly-wabt/1.0.41:$PATH
export GOMAXPROCS=16
out=docs/performance/setup-cleanup
go test -count=1 -tags wago_guardpage ./src/core/runtime > "$out/test-runtime-guard.txt" 2>&1
go test -race -count=1 -tags wago_guardpage ./bench/suite -run '^Test(ImportLifecycle|MinimalWASICommand)' > "$out/test-command-isolation-race.txt" 2>&1
just test corpus all > "$out/test-corpus.txt" 2>&1
WAGO_BOUNDS=signals go test -count=1 -tags wago_guardpage ./bench/suite -run '^(TestCorpus|TestCorpusSemanticExec|TestApplicationCorpusRuns|TestCatalog.*|TestValidateCorpusModule.*|TestImportLifecycle.*|TestMinimalWASICommand)$' -args -wago.corpus=all > "$out/test-corpus-guard.txt" 2>&1
just lint > "$out/lint.txt" 2>&1
go vet ./bench/suite > "$out/vet-bench.txt" 2>&1
