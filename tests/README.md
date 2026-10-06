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

## Wide host calls across nested state changes

`host_nested_state_test.go` combines 48 mixed i32/i64/f32/f64 parameters and
results with one supported `InvokeFromHost` re-entry. Same-instance and
different-instance cases grow memory once, write a new-page marker, change a
global, and replace an indirect-call target. Both normal nested return and
mutation followed by a nested trap must preserve every outer parameter and
result. The guest reports five separate state observations after the callback.
The test invokes one resolved `WasmFunc` twice with distinct input and output
values; retained results are copied before the next invocation. Memory views
are reacquired after nested execution. Scalar comparison ignores unspecified
high ABI-slot bits for i32/f32 and compares all semantic bits exactly.

The same observation gate rejects B's valid state when A is expected. This is
a wrong-valid-binding observer control, not native pointer corruption. Existing
`guest_storage_test.go` owns rejection of re-entry during a storage borrow and
expired borrowed views. No v128 callback or deferred-event re-entry is added.

Verbose output records Wasm and loaded-code hashes and execution provenance.
With `wago_codegenstats`, a separate diagnostic compile must reproduce loaded
bytes and report the established compiler for the wide `run` function. This
caller-aware route does not use the direct HostCall view portal or the fixed
typed scalar portal. Native AMD64 was executed; ARM64 cross-compilation alone
is not execution qualification. TinyGo and precompiled-only builds are excluded.

`BenchmarkHostNestedMixedState` measures repeated calls after the first memory
growth, including the observer and nested mutation. Compilation, instance setup,
and initial growth are excluded. Each instance retains at most two Wasm memory
pages. The benchmark reports allocations without asserting a timing threshold.
