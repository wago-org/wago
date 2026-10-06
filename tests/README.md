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

## Rule-directed compiler recipes

`tests/support/ruleguide` supplies bounded typed recipes for three existing
rules: AMD64 `i32-bswap-tee`, portable `swar-mask-test`, and portable
`simd-shift-imm`. The architecture tests compile and execute 36 positive or
one-premise-broken recipes, plus 96 directed and 96 generic template samples
from seed 809. Each case uses 23 edge or random inputs and checks all semantic
result bits against a Go bit-vector model. Contexts cover a plain result, a
local update followed by a source overwrite, a branch result, and a result
kept live across a non-inlined guest call.

Checked diagnostic builds require the exact rule count and record actual
shared/established admission, native frame/spill/literal counts, source/native/
input hashes, Go version, target, feature masks, bounds mode, and completed
calls. Valid disabled scalar rules and a dynamic SIMD-count substitution must
fail the activation gate while retaining correct results. Ordinary builds
check numerical results and report selection as unavailable. ARM64 executes
the byte-swap sources but reports that rule's selection as unsupported.
AMD64 checks SSE2 and, when the host permits it, AVX2.

The yield benchmark gives both generators the same 96-candidate budget, typed
root templates, contexts, operand alphabet, and seed. The generic sampler can
produce every selected rule. It samples operand relations independently;
the directed sampler constrains the required relations. A useful case is a
**distinct source hash within that budget**, with actual rule activation and
two native calls checked against the model. The benchmark includes generation,
validation, compilation, mapping, execution, and duplicate removal. It reports
attempts, activations, distinct useful cases, and useful cases per second.
This is a bounded template comparison, not a yield claim for a general fuzzer.

```sh
go test -tags=wago_regalloccheck,wago_codegenstats ./src/core/compiler/backend/railshot/amd64 -run '^TestRuleDirectedRecipes$' -v
go test -tags=wago_codegenstats ./src/core/compiler/backend/railshot/amd64 -run '^$' -bench '^BenchmarkRuleDirectedYield' -benchmem
go test ./src/core/compiler/backend/railshot/amd64 -run '^$' -bench '^BenchmarkRuleDirectedPairs' -benchmem
```

The paired benchmarks report decode/validate/compile cost separately from
prepared native calls. Scalar pairs use existing rule kill switches. SIMD pairs
compare a constant count with an equal dynamic count. Setup is outside native
call timing. No wall-clock threshold is asserted. The support code adds no
production instrumentation or retained production memory.
