# Tests

Package-local Go tests stay beside the implementation they exercise. This
directory contains repository-level conformance, integration, fuzz, corpus,
fixture, and support code:

```text
tests/
  conformance/   pinned WebAssembly spec suites and Wasmtime Core 3 ports
  corpus/        regression artifacts with provenance ledgers
  fuzz/          reusable fuzz workers and oracles
  integration/   cross-package and CI-policy tests
  fixtures/      small purpose-built test modules
  support/       shared Go test helpers
  scripts/       shell-level integration checks
  tools/         corpus and documentation maintenance commands
```

Use `just` as the public entry point. Run `just --list test` or
`just --list test spec` to explore:

```sh
just test                         # unit/integration tests + every benchmark corpus workload
CORPUS=quick just test            # faster representative local gate
just test corpus algorithms
just test corpus tag:polybench
just test corpus all              # every curated executable workload
just test spec v1                 # one pinned spec version
just test spec                    # all pinned spec versions
just test fuzz 30s                # bounded fuzzing gates
just test all                     # all of the above
```

The runtime benchmark corpus is repository-level data in `corpus/`; it is not
duplicated under tests. Regression artifacts remain under `tests/corpus`
because they are narrow bug reproductions, not performance workloads.

## AMD64 loop-boundary regression checks

`src/wago/loop_boundary_*test.go` keeps the #817 experiment's useful
checks: arithmetic and trap oracles, live stack/register pressure, native
producer placement, and compile/prepared-call benchmarks. It tests zero, one,
and many iterations, conditional entry, aliases, constants, subnormal values,
signed zeros, infinities and NaNs. No additional eager-flush optimization was
retained: the measured candidate emitted identical native code in all nine
fixtures and had no significant timing improvement on the tested AMD CPU.

The native check reads instruction addresses and direct backedge targets in
GNU objdump output, for both SSE2 and modern encodings. Its deliberately
inside-loop producer has the same multiply count as the pre-loop producer;
the observer must distinguish their placement. This is a bounded single-loop
check, not a general native control-flow analyzer. With `wago_codegenstats`, it
also verifies the established compiler path and actual allocator spills in the
register-pressure fixture.

```sh
go test -tags=wago_regalloccheck,wago_codegenstats ./src/wago -run '^TestLoopBoundary'
go test ./src/wago -run '^$' -bench '^BenchmarkLoopBoundary' -benchmem
```

These tests use the existing WABT helper and skip when `wat2wasm` is unavailable;
native placement additionally requires GNU `objdump` or `gobjdump` and skips
when only LLVM objdump is available. WAT assembly and guest
execution are excluded from the compile benchmark; setup and compilation are
excluded from the prepared-call benchmark. No wall-clock threshold is asserted.
Native ARM64 and admitted memory-region/shared-compiler loops remain outside
this coverage.

## Synchronous and deferred host-event boundaries

`host_event_boundary_test.go` uses a host-owned imported scalar global to
observe the public callback contract. The guest writes phase 1, emits event 11,
reads the global, writes phase 2, emits event 22, and writes phase 3 before
return. Synchronous callbacks observe phases 1 and 2; their mutation is visible
to the next guest instruction. Deferred callbacks both observe phase 3 and do
not change that guest result. The callbacks use public numeric global accessors
and retain no borrowed guest storage.

The tests also check that a guest trap discards pending events, a replayed
callback failure stops later events after guest work finishes, and a repeated
prepared invocation has no leftover events. The same observer rejects a local
adapter that delivers the first event early, reversed events, and a dropped
event. Existing `host_event_test.go` tests own buffer overflow, exact signatures,
mixed-mode rejection, and cross-instance restrictions.

Verbose output records Wasm and loaded-code hashes, Go/OS/architecture, explicit
bounds mode, required AMD64 features, callback mode, and retained event-log
bytes. With `wago_codegenstats`, an independent diagnostic compile must reproduce
the loaded bytes and report the established compiler path. These tests support
ordinary Go on native AMD64/ARM64; TinyGo and precompiled-only builds are
excluded. Cross-compilation does not count as executed delivery coverage.

`BenchmarkHostEventBoundary` measures repeated prepared calls with 1, 16, and
1,024 events, checks exact counts and sums, and reports allocations and retained
log bytes. It omits the phase observer's global reads. Synchronous latency and
deferred throughput are different contracts, not interchangeable optimizations.
No production state, API, callback buffer, or instrumentation is added.
