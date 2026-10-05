# Experimental Linux/RV64 integer multivalue checkpoint

This stage extends the accepted `4898b7a1` checkpoint with zero/multiple integer
results, indexed block/if/loop signatures, and complete result-vector transfers
for direct calls, returns and branches. Every admitted function/type remains
integer-only; unsupported module shapes still fail before executable publication.
This is genuine fixed-width RV64 lowering, without another architecture's code.

## Calling and control

Signatures permit zero through 256 parameters/results, subject to the existing
256-local and 512-operand bounds. The integer walker handles signed 33-bit block
types, including indices whose encodings need multiple bytes. Loop labels carry
the loop's parameters; block/if/function labels carry results. Branch vectors
move forward to lower canonical homes, preserving overlapping source ranges.
Conditional branches leave untaken operands untouched, and branch-table payload
copies preserve the selector register. All vector work counts against admission's
bounded instruction/transfer budget, including unreachable code.

Compiler instances use `NewCallFrameLayout`, with a fixed context header at
`A0-8`, an argument range at A0, and a separate result range at A1. The context
header no longer depends on module parameter capacity. Both ranges live in an
owned off-heap mapping; void modules reserve a valid result sentinel. Existing
standalone snippets keep the legacy `NewCallFrame` layout. The compiler always
constructs the new layout, and the two conventions must not be interchanged.

Direct callees consume their arguments into guarded frame homes before nesting.
Calls reuse the bounded arena, check traps before consuming result cells, and
load each result with LD or LWU according to width. i32 cells are zero-normalized
before full eight-byte result writes. Large frame offsets use X31 without
clobbering the result pointer held in X30. The entire foreign frame is checked
before SP adjustment or frame writes, as in the earlier checkpoint.

`InvokeInto` takes caller-owned argument/result slices, checks exact arity before
guest effects, and copies outputs only after success. No Go result pointer enters
guest code. Guest traps leave caller outputs unchanged; guest effects preceding
a trap retain ordinary Wasm semantics. Existing context/lease/frame/engine/code
locks remain held through output copying. Warm integer multivalue invocation
allocates zero Go objects in the focused QEMU regression.

## Review and qualification

Independent compiler review checked loop/result distinctions, overlapping
transfers, selector preservation, normalization, scratch lifetimes and admission.
Independent runtime review checked owned headers/ranges, preflight, success-only
copying, locks, leases and cleanup. Both accepted; suggested regressions were
added for no-else indexed if, differing loop vector counts, branch-table targets
at different frame depths, aliased caller slices, void sentinels and cleanup retry.

Focused and full experimental package runs passed under QEMU with optional
Zba/Zbb/Zbs/Zicond/vector extensions disabled, in ordinary and checked builds.
The same integer/control fixtures passed on the established AMD64 target. Host
race checks, host/RV64 vet and host static analysis also passed. Logs are retained
outside the checkout under `../evidence/multivalue-*`; disposable executable test
copies reside in `/tmp/wago-rv64-evidence-binaries` after root disk exhaustion.
QEMU evidence establishes correctness only; native cache/performance qualification
and the official complete feature corpus remain outstanding.

## Production boundary

This layout is transitional. Production wrapper/context/trap and metadata
integration must precede public exposure or production table descriptors. It
does not implement imports, indirect/cross-instance calls, tables/references,
FP/host/SIMD, GC/EH/threads, cancellation or artifact APIs. The
[full parity tracker](riscv64-parity-tracker.md) records those remaining obligations.
No native performance claim, push, PR publication, remote CI dispatch or merge
is part of this checkpoint.
