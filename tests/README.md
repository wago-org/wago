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

## Footprint report omission controls

`TestNativeSizeRejectsOmittedAlignment` compiles small exported functions alone
and together on each native backend. The standalone images establish physical
function spans. The combined image establishes the intervening padding span.
The positive report must reconcile with those bytes. Omitting nonzero padding
from a report copy must fail the same gate. Reassigning padding to another
category must fail the boundary check even when the total remains correct.
Run it with `-tags=wago_codegenstats` on the target architecture.

`TestArtifactSizeRejectsOmittedMetadata` reads section lengths and the entry
vector span from serialized bytes. It rejects report copies with omitted
metadata, entries, or framing, plus an entry-to-import reassignment. It never
changes executable or serialized artifacts. `BenchmarkArtifactFootprintCount`
measures the existing count-only path with 0-byte, 64-KiB, and 8-MiB passive
payloads; these controls add no production state or allocation.

These are byte-attribution checks after compilation/serialization, not release
or leak tests. Executable payload, mapped capacity, retained compiler storage,
allocation volume, peak memory, and RSS remain separate quantities. Existing
ownership tests own release semantics. Native execution is reported separately
from cross-build success; these checks do not address deferred issue #801.
