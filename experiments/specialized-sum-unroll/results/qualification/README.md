# Final qualification evidence

`build.json` fixes the source lineage and timing binary hashes. The branch was
not rebased, merged with main, or rewritten. Only the external timing source
snapshot includes the main update patch. It is not a Git worktree.

`external-manifest.json`, `additional-manifest.json`, `local-manifest.json` and
`component-manifest.json` retain source URLs, revisions and artifact hashes.
`scan-summary.json` summarizes the final decoded records. The `*-final.json.gz`
files contain the full final analysis; gzip uses a fixed timestamp. Earlier
scanner outputs remain available with no `-final` suffix. Broad reduction
candidates are hypotheses, not admission or execution proofs.

`loops/` contains manually inspected instruction ranges with explicit local
types. PCs are offsets in the function expression, excluding local declarations.
Function indexes include imported functions. `duckdb-f8687.json` supplies the
four-load main loop and the remainder setup for its tail.

`admission/` contains 19 modules × 3 modes, compiled with one worker and deferred code
mapping. There is one selected latch only in the synthetic positive control.
`admission-initial-failed/` retains the first diagnostic-tool failure: the tool
called `Close` on a nil code image after successful heap-only compilation. The
tool was corrected and all diagnostics repeated. This was not a compiler crash
or a timing sample. No result from the failed attempt supports an admission claim.

The five `yyjson-*` directories retain 20 alternating pairs each, all raw output,
launch order, environment, benchstat and paired summaries. The first same-binary
set is noisy and remains in the result. No sample was discarded. These are
negative controls, not evidence of application gains from the sum emitter.

The research limit was 256 MiB of pinned Git-hosted external artifacts, one
DuckDB package below 160 MiB, the existing local corpus, and seven bounded CDN
attempts. No guest application from a new download was executed. Only the
existing verified yyjson workload and native correctness fixtures were executed.
No new binaries, licenses of uncertain scope, or third-party source files were
copied into the corpus.

## Reproduction

From the repository root, with Go 1.27.1, Python 3 and wasm-tools 1.251.0:

```sh
python3 experiments/specialized-sum-unroll/fetch-qualification.py --cache /tmp/wago-qualification-artifacts --out /tmp/external-manifest.json
python3 experiments/specialized-sum-unroll/fetch-more-qualification.py --cache /tmp/wago-qualification-artifacts --out /tmp/additional-manifest.json
go build -o /tmp/wago-qualification-scan ./experiments/specialized-sum-unroll/scan
/tmp/wago-qualification-scan --manifest /tmp/external-manifest.json --dir corpus/workloads --out /tmp/instruction-scan.json
/tmp/wago-qualification-scan --manifest /tmp/additional-manifest.json --out /tmp/additional-scan.json
wasm-tools component unbundle /tmp/wago-qualification-artifacts/sightglass/benchmarks/cm-online-stats/cm-online-stats.wasm --threshold 0 --module-dir /tmp/wago-components/stats -o /tmp/stats-imports.component
wasm-tools component unbundle /tmp/wago-qualification-artifacts/sightglass/benchmarks/kotlin-richards/kotlin-richards.wasm --threshold 0 --module-dir /tmp/wago-components/kotlin -o /tmp/kotlin-imports.component
/tmp/wago-qualification-scan --dir /tmp/wago-components --out /tmp/component-scan.json
go test -tags=wago_codegenstats ./experiments/specialized-sum-unroll/scan
go build -tags=wago_codegenstats,wago_sumunroll -o /tmp/wago-qualification-admission ./experiments/specialized-sum-unroll/admission
/tmp/wago-qualification-admission --module /tmp/wago-qualification-artifacts/duckdb/package/dist/duckdb-mvp.wasm --variant D --out /tmp/duckdb-D.json
```

To reproduce the timing source, export commit 6a06dbe1 to a fresh ordinary
directory with `git archive`, then apply `updated-base.patch` there. Build
`bench/suite` both normally and with `-tags=wago_sumunroll`; use
`GOFLAGS=-buildvcs=false`. The exact `sample.py` command, working directory,
selectors and timing flags are in each saved `environment.json`. Use new output
directories and run the sets serially. The experiment selection is private to
the tagged test binary; ordinary Wago remains 4/4.
