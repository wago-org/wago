# Portable official-adapter reproduction sources

This is a reusable wrapper bundle around JairusSW/wasm-bench commit
`a017c56fef370a54ffa33ca49ad9b6827e127cc4`, not a claim that rebuilt binaries
match the earlier lost temporary binaries. Recorded evidence remains historical.
`original-wrapper-hashes.json` identifies the persisted reconstruction wrappers
before portability edits; `source-receipt.json` identifies prepared adapter sources.
Neither file proves identity with a lost earlier executable. No new measurements
or executable builds were performed while preparing this bundle.

## Prepare

Use Linux AMD64, Python 3, Git, taskset, Go 1.27.1 and TinyGo 0.42.0.
Place the selected Go and TinyGo tools on PATH; keep CPU affinity and background
load controlled. Run all following commands from this bundle directory.

```sh
git clone https://github.com/JairusSW/wasm-bench.git upstream
git -C upstream checkout a017c56fef370a54ffa33ca49ad9b6827e127cc4
python3 prepare.py --upstream upstream
python3 fetch-artifacts.py --contracts contracts145.json
```

The 145 execution contracts retain original workload/oracle/reset data and
contract hashes. Only outer artifact paths change to `artifacts/SHA.wasm`,
relative to the current directory. Download URLs are canonical immutable
`https://wasm.fyi/api/artifacts/SHA.wasm`; every byte stream must match its SHA.
Never substitute rebuilt modules or same-name artifacts when an address fails.
The JSON file hash changes due to portable paths; workload contract hashes do not.

The compatibility patch removes obsolete AOT modes, explicitly reports unsupported
TinyGo MemStats fields, and rejects unsupported TinyGo command fixture import
rather than weakening filesystem confinement. It preserves core execution
contracts, HostCall imports, initialization, reset, and oracle semantics.
Preparation verifies all prepared source hashes against the recorded receipt.

## Toolchain qualification

The measured TinyGo installation is NOT stock. In `src/internal/task/task_threads.go`,
the declaration for `tinygo_task_exit` has
`//go:linkname tinygo_task_exit tinygo_task_exit_thread`; in
`src/internal/task/task_threads.c`, the corresponding C function is named
`tinygo_task_exit_thread` rather than `tinygo_task_exit`. Use the same modification
for baseline and candidate. `toolchain-receipt.json` records SHA-256 of these
modified source files and the installed TinyGo executable, with installation-relative
paths. The compiler executable hash alone does not identify its runtime sources.
Do not silently compare patched and stock runtimes. No automatic toolchain patching
is performed here.

Pinned adapter dependencies: wasi v0.3.1, component-model v0.1.6, x/sys v0.44.0;
checksums are in `adapter-dependencies.sum`. The build wrapper selects a local
Wago checkout and the exact prepared adapter. It uses tasks scheduler,
conservative GC, opt=2, no-debug, GOMAXPROCS=2, and optional explicit stack size.
It records the Wago revision and executable hash. Caller-supplied Go/TinyGo paths
and cache settings replace private machine defaults; wrappers are therefore not
byte-identical to original build scripts. Outputs/receipts may contain your local
paths: review them before publication.

## Build and run

Use separate clean Wago checkouts. The original source baseline is local commit
74ab035a (published equivalent d51627cb8ea4f6692df6d3014280e55d5192a743);
the iterative source is on jairus/compiler-iterative-hints. Pin and report exact
checkout revisions instead of assuming a branch remains unchanged.

```sh
bash build-tinygo.sh /path/to/baseline "$PWD/bin/baseline" 256KB
bash build-tinygo.sh /path/to/candidate "$PWD/bin/candidate" default
python3 paired.py --contracts contracts145.json --baseline "$PWD/bin/baseline" --candidate "$PWD/bin/candidate" --output timing.jsonl --cpu 2 --control
python3 run_phases.py --contracts contracts145.json --baseline "$PWD/bin/baseline" --candidate "$PWD/bin/candidate" --output memory.jsonl --cpu 2 --trials 3
```

Choose a permitted CPU; CPU 2 is an example, not a portable availability guarantee.
A default-stack versus 256 KiB comparison changes BOTH source and stack settings.
Use equal stack settings to isolate the source change. Do not use a crashing
baseline or switch optimization flags to create an apparent speedup.
`paired.py` retains declared fresh-instance/stateless reset behavior and validates
sample oracles. `run_phases.py` measures absolute Linux VmHWM at the compiled
barrier before subsequent oracle execution; instrumentation is included. Its
barrier timings are diagnostics, not uninstrumented compilation latency.
Do not infer a 40% gain or a <=5% execution-regression pass from this bundle.
