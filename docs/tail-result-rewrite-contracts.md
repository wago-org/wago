# Proper-tail result rewrite contracts

Status: implemented August 15, 2026 for issue #436.

## Why this fence exists

A proper tail transfers its result contract to another function without returning
through the current Wasm frame. A later optimization that removes or rewrites
function results must therefore update the complete contract, not only the body
whose result appears unused.

The target-knowledge boundary is:

- `return_call` has one statically known function target;
- `return_call_indirect` has a dynamic target unless a private immutable local
  table proves its complete non-null target set;
- `return_call_ref` has a dynamic target unless the producer is mechanically
  known. The initial admitted proof is the exact adjacent `ref.func` shape;
  typed globals, imported references, mutable tables, locals, parameters, and
  computed references remain conservative.

Changing only a caller to a covariant wider reference result is safe when the
selected target/type contract is unchanged. Changing the selected indirect or
reference type is not safe merely because the transformed module validates: an
old dynamic target could begin trapping on its runtime signature check.

## Required workflow for signature-changing transforms

Any compiler pass that changes function result signatures while retaining
proper-tail sites must:

1. retain the validated pre-transform `*wasm.Module`;
2. construct the transformed module without mutating the retained source;
3. call `frontend.ValidateTailResultRewriteWithFeatures(before, after, features)`
   with the exact validation feature set used by the compile (the compatibility
   helper `ValidateTailResultRewrite` is sufficient only for default modules);
4. abandon the transform on any error; and
5. run ordinary module validation and backend differential execution tests.

`ValidateTailResultRewrite` performs ordinary validation itself and additionally
checks the target-knowledge rule above. Signature comparisons include complete
recursive/GC type graphs, not only their local type indexes. It rejects changes
to imported, exported, or start-function signatures; changes to local functions
that may escape through exported function-reference tables, globals, results, or
tags; parameter rewrites; tail-site retargeting; mutable or exported tables;
imported indirect targets; and dynamic `return_call_ref` producers.

The helper is intentionally off the ordinary compile hot path. The current
Railshot optimizations do not rewrite Wasm function signatures, so paying for a
second body scan on every compilation would provide no additional protection.
The first pass that introduces signature rewriting must use this fence at its
transform boundary.

## Tests and benchmarks

Focused checks:

```sh
go test ./src/core/compiler/frontend -run 'TailResult' -count=1
go test ./src/wago -run '^TestProperTail' -count=1
```

The frontend tests cover coordinated scalar, multivalue, SIMD, direct,
immutable-indirect, and immediate-reference elimination; partial transforms;
mutable tables; imported targets; typed-global references; recursive signature
graph changes; exported function-reference escape surfaces; caller-only reference
covariance; AST/byte-backed parity; and fuzz seeds.

The runtime matrix executes direct, indirect, and reference proper tails across
Balanced, Size, and Embedded objectives. Scalar, multivalue, SIMD,
typed-global `return_call_ref`, and million-transfer frame-discard coverage run
on amd64 and arm64 build products. The generic funcref-result egress matrix runs
on amd64, while the transform fence's reference and recursive-GC contract tests
remain architecture-independent.

Benchmarks:

```sh
go test ./src/core/compiler/frontend -run '^$' -bench 'TailResult' -benchmem
go test ./src/wago -run '^$' -bench '^BenchmarkProperTailResultContracts$' -benchmem
```

The frontend analysis/validation benchmarks are transform-development costs,
not ordinary compile-path costs. Runtime benchmarks must remain allocation-free
once the instance and export are warm.
