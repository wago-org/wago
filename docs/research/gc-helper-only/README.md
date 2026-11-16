# Helper-only compiler correctness bridge

Experimental build tag: `wago_gc_helper_only` (AMD64 only). Default code generation
is unchanged. This is not a performance optimization or a completed moving collector.
Object-layout operations go through checked Go helpers; native object resolver and
constructor entry points fail closed if accidentally reached. Primitive reference
null/equality/i31 operations remain native. Native loop/whole-body, fresh-constructor,
and dead-allocation reductions are disabled.

A live compiler receipt is copied into private compiled metadata only without custom
instructions. `hasGCHelperOnlyReceipt` is a necessary admission condition for a future
moving collector, not sufficient module-scope admission. The receipt is not serialized;
loading an artifact cannot manufacture it, including in a tagged build. Other backend
compilers leave it false. An adapter must reject artifacts without the receipt.

## Qualification

Go 1.27.1, Linux AMD64. Default backend and Wago GC suites passed (14.99s runtime).
Tagged Wago GC suite passed (66.73s) with the explicit native-only exclusions in this
directory. Tagged backend passed (0.085s) with the same list. These are **not** claims
that the unfiltered tagged suite passes. Exclusions cover native code-shape/admission
expectations and deliberate corruption of native handle/descriptor metadata that
checked Go helpers do not consume. No Wasm semantic or official spec test is excluded.
The disassembly space-load test is mode-aware and positively verifies zero native
handle-space byte loads across its seven helper-only access/cast fixtures.

Reproduction (from repository root, with pinned spec-v3 corpus installed):

```sh
skip="^($(paste -sd '|' docs/research/gc-helper-only/native-only-exclusions.txt))$"
go test -tags wago_gc_helper_only ./src/core/compiler/backend/railshot/amd64 ./src/wago -run 'GC|Gc' -skip "$skip" -count=1
go test -tags 'wago_gc_helper_only wago_regalloccheck' ./src/wago -run TestGCHelperOnly -count=1
```

Actual parked callback tests rewrite native roots to a distinct valid object before
resuming. Both Throughput and Tiny collectors pass local, caller-local plus operand
alias across internal calls, and owned mutable-global variants. Metadata serialization
also proves the private receipt does not affect artifact bytes. This does not yet prove
moving collection inside a constructor rewrites every initializer scratch copy; the
moving adapter must add that test. Owned runtime domains enumerate writable global
cells; nonowned instances are not covered by this bridge's global rewrite guarantee.
