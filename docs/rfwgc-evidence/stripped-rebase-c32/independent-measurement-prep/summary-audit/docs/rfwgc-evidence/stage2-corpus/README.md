# Stage2 admitted-corpus evidence

Source under test: `9d7e4fdf7925265ee0c7db9246e66f0707ee2d76`.
Baseline: `6ceb862219e64480c0503836b19ee1e02b209cc7`.
Both primary revisions enable `gc-lifetime-analyze`: baseline analyzes only;
Stage2 also enters the sealed/replayed consumer. Baseline-off correctness logs
are additional controls, never the primary performance counterfactual.

The current catalog is the admitted corpus: **128 entries, 121 unique artifact
SHA-256 values, 21 semantic checks**. All artifacts are present and match their
pins. There are **0 missing/unbuildable catalog entries**. No build recipe was
needed. `candidates.json` is an acquisition queue and is excluded. All 63 command,
48 export and 17 semantic entries are attempted individually, independent of
website/quick profiles. There are 51 direct export slots and 21 semantic slots,
plus 63 command workloads: **135 runtime rows** across 128 entries.

`manifest.json` retains the exact catalog identity, artifacts, actual hashes and
oracle declarations. `per-module.json` joins each catalog entry to compiler,
inventory, three-mode oracle and runtime outcomes; `tables.md` is its compact
view. `summary.json` separates catalog-weighted and unique-hash totals.

## Static and dynamic interpretation

All 128 modules compile through the real public runtime compiler with opt-in
analysis and the consumer enabled. The archived compiler alone changes the
existing diagnostic print sink to JSON (`stats.go.txt`); it records the bounded
analysis result and finalized function peephole counts for
`gc-lifetime-checked-scalar-replace`. It uses `wago_codegenstats`, **not**
`wago_profile` (which bypasses the consumer). This evidence build is separate
from the ordinary timing binaries. There are no production policy changes.
Diagnostic proof counts and actual emitted consumer lowerings are separate.

`inventory_test.go.txt` independently walks **every function body** with the
module-aware instruction immediate classifier, without the lifetime diagnostic
4096-function/256-record budgets. It counts native-GC constructor instructions,
not arbitrary byte patterns or guest linear-memory malloc calls. A successful
complete walk supplies the exact constructor denominator. The bounded reports
still explicitly retain omitted functions and unknown analyzed denominators;
the exhaustive inventory does not retroactively make those diagnostics complete.

Zero lowered sites proves zero dynamic materialization removals in these compiled
modules. This is a static exclusion, **not measured dynamic counter telemetry**.
No publication count is inferred from LiveObjects; host Go allocations are not
guest allocation counts. The catalog's language runtimes can allocate in linear
memory without containing WasmGC constructors. Fixture improvements are not
extrapolated to this corpus, and zero qualifying sites is a valid result.

## Oracles and measurement

Disposable `git archive` trees consume the same current catalog, pinned artifacts,
inputs and existing `bench/suite` direct, semantic and command adapters. The
archived adapters explicitly propagate the lifetime option to direct execution,
commands **and semantic exact-oracle compilation**. Each adapter is run with
`1x` for correctness in ordinary, wagodebug and wago_regalloccheck builds before
timing. Those one-iteration outputs are **not admitted performance evidence**.

Repeated export execution excludes compilation and instantiation; a vector
semantic operation is its complete case-set. A **command operation** includes a
fresh instance, host/IO setup, command execution and teardown, excluding module
compilation. These units are reported distinctly, not combined into a corpus
speedup. Root benchmark lines without a workload child are empty dispatcher
measurements and are excluded from results.

Primary timing uses small per-catalog-workload revision pairs, rotating revision
order, six accepted samples per revision and actual timed runtime rows >=500ms
(600ms target). Existing batched execution normalization is preserved. Fixed
GOMAXPROCS=1; only each owned timing process is pinned, to an observed allowed
CPU. Three 2s admission windows require <=5% external-host **and** SMT-sibling
load; all monitored >=250ms in-run intervals must also satisfy <=5%. Rejected
outputs and all telemetry are retained. Five minutes without progress ends an
attempt for reassessment, without weakening thresholds. Builds/tests finish
before timing; no other process is killed or repinned.

A main-checkout advisory flock coordinates this run; `lease.jsonl` records its
acquisition/release. CPU-package sensors (k10temp/coretemp) are recorded, excluding
NVMe temperatures. Per owner clarification, temperature is telemetry, not an
additional 60C admission veto: the separate main-checkout AGENTS.md is outside
this worktree ancestry. Initial build-time temperatures are not timed samples.
The original contention method remains unchanged.

## Staged conformance is separate

`conformance-coverage.json` inventories **19 historical accounting snapshots,
103 source rows / 102 unique pinned WAST source files** (`ref_null` overlaps).
It reads immutable Git blobs at asserted official revision
`9d36019973201a19f9c9ebb0f10828b2fe2374aa` and hashes each source. Lexical source
counts are **1242 module forms including negative inputs**, **522 direct module
commands**, and separate assertion/action categories. They are not positive
executable modules, test-function counts, or freshly executed lifetime-on coverage.

The prior `stage2-suite/ordinary.log` contains converter errors for **20 distinct
pinned source paths**, across **18 top-level failed tests / 27 failure lines**.
The unavailable official Release3 interpreter requires dune/ocaml/ocamlc/menhir.
Historical expected counts are retained as historical only. This extension did
not rerun staged conformance with lifetime-on; some families previously passed.
It does not call all 102 sources blocked, all 522 commands unbuildable, or any
of the 128 catalog artifacts a conformance-toolchain gap.

## Reproduce

From this checkout, with its already available pinned artifacts and Go 1.27.1:

```sh
python3 docs/rfwgc-evidence/stage2-corpus/run.py
python3 docs/rfwgc-evidence/stage2-corpus/conformance.py
python3 docs/rfwgc-evidence/stage2-corpus/summarize.py
```

The source revisions are constants in `run.py`. All archives, binaries, GOTMPDIR
and private Go caches live in one TemporaryDirectory and are removed on exit;
only textual evidence and reproduction scripts remain. `refresh-semantic.py`
records a preparation repair used during this run; a clean reproduction applies
that adapter change directly before its first build. A failed initial inherited
GOROOT mismatch is retained in `preparation-toolchain-mismatch.txt`; final builds
explicitly select both the Go binary and GOROOT at 1.27.1.
