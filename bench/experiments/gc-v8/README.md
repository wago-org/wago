# Experimental WasmGC/V8 comparison

This experiment does not establish parity with V8. Compare each workload, not a
geometric mean that hides allocation regressions behind optimized i31 loops.

## Provenance

Eight GC artifacts are generated from MIT-licensed `JairusSW/wasm.fyi` revision
`90b09c610eb506256f52191db396ab684395d8e1`, using
`corpora/features/generator.mjs` and `scripts/lib/feature-performance.mjs`.
See `LICENSE.corpus`; WAT trailing whitespace is normalized. The exact six public GC performance artifact hashes match
https://wasm.fyi/gc. The scalar-baseline and scalar-copy-loop controls are also
included. `gc-tree-survival.wat` is an original Wago experiment: a globally rooted
2047-node tree must survive construction and checksum traversal of 32 temporary
trees; the independently calculated result is 2047 * 33 = 67551.

The performance generator retains distinct objects. The older unwrapped binaries
in wasm.fyi's checked-in corpus do not have the same retention contract. Do not
substitute them. `cases.json` pins WAT/Wasm digests and exact checksums.

Regenerate binaries with official `wasm-tools 1.260.0`:

```sh
for f in gc-*.wat; do wasm-tools parse "$f" -o "${f%.wat}.wasm"; done
```

## Reproduce

From this directory build a runner against this checkout, outside `go.work`:

```sh
GOWORK=off go build -o wago-candidate .
```

Build the same `main.go` with a separate modfile whose `replace` selects the
baseline checkout, with the same Go version. Keep the two engine binaries.

```sh
python3 run.py --baseline /path/wago-baseline --candidate /path/wago-candidate --out results.json
# From the repository root, independent untimed collection and digest audit:
go test ./src/wago -run '^TestGCV8ExperimentCollection$' -v
```

The runner requests default GC settings and disables `dead-gc-new` using the
public optimization API. This disables allocation elision/scalar replacement,
not native allocation lowering. Other compiler optimizations remain active; this
is not a claim that all proof-based optimization is disabled. `GC_PROOFS=off` is
set by `run.py`; standalone runner calls must set it explicitly. The optional
`GC_MODE=throughput` selects a 1 MiB nursery and 64 MiB throughput heap; do not
mix that configuration into default results without labeling it.

Node uses `--no-liftoff` to select optimizing compilation. Each fresh process
compiles and instantiates outside timing, warms 100 calls, then collects nine
batches of 32 invocations. The same instance persists, with collection enabled
and no forced collection between calls. Every call verifies its result. Five
launches rotate baseline/candidate/V8 order. Record exact Node/V8 and Go versions,
engine revisions and binary hashes with the results.

These are invocation times, **not GC pause times**. Timers include host crossings
and oracle checks, which dominate tiny probes. Analyze medians per process and
per workload, retain failed runs, and report variability. Use the same machine
without concurrent tests or benchmarks. Shared cloud scheduling/frequency is
uncontrolled; repeat on controlled native hardware before general claims.

The audit requires actual minor/full collections for allocation and traversal
cases, catching silent collection-disabled fallback. It does not establish
major-collection pause behavior, peak/live memory, arbitrary graph correctness,
or full WasmGC conformance. Run existing GC correctness/stress/conformance suites
separately. Measure memory and collector pauses in a separate instrumentation
pass. V8 parity requires meeting the agreed threshold on **each** admitted
workload, not cherry-picked microbenchmarks.
