# WasmGC `ref.cast` and `struct.get` instruction performance

Date: 2026-08-29

Host: Linux/amd64, AMD Ryzen 7 8845HS, `-cpu=1`, 500 ms benchmark time,
zero allocations per measured guest iteration.

## Method

The focused benchmarks execute `b.N` guest iterations in one host invocation.
Each GC instruction result is dropped after the instruction, which preserves the
trap-capable operation without adding a result-dependent `ref.is_null` or integer
sum. A separate control measures the shared loop and counter update.

The final `ref.cast` source is loaded from a mutable `eqref` global, so the cast
cannot be removed from initializer facts. The final `struct.get` reads one mutable
`i32` field from an object allocated before the guest loop. The non-final cases use
an open struct type and one proper final subtype.

Command:

```sh
go test ./src/wago -run '^$' \
  -bench '^BenchmarkGC(RefCastInstruction|StructGetInstruction|RefCastNonFinalInstruction|StructGetNonFinalInstruction|InstructionLoopControl)$' \
  -benchmem -benchtime=500ms -count=5 -cpu=1
```

## Baseline results

Five-sample medians from the first complete final/non-final run:

| Benchmark | Median |
|---|---:|
| loop control | 0.227 ns/op |
| final `ref.cast` | 2.666 ns/op |
| final scalar `struct.get` | 2.452 ns/op |
| non-final `ref.cast` | 79.64 ns/op |
| non-final scalar `struct.get` | 71.42 ns/op |

The final paths are checked native code. Explain output attributes one native GC
call to each final cast and one emitted compact-handle resolution to each final
get. The non-final paths use one synchronous helper transition per dynamic
instruction. This transition is the main measured gap: non-final operations are
about 29-30 times slower than the matching final operation in this fixture.

## Retained optimization

AMD64 structured reference facts already know an exact final subtype after a
constructor or equivalent proven local flow. Scalar `struct.get` previously
ignored that exact dynamic type when the instruction was declared through an open
supertype, so it always entered the synchronous helper.

The retained specialization checks all of these compile-time conditions:

- the declared access field has direct scalar storage;
- the receiver fact has one exact final type;
- the exact type is a declared subtype of the access type;
- both layouts have the same field offset and scalar representation; and
- the ordinary checked native resolver can validate the exact final runtime type
  and required object extent.

With `WAGO_AMD64_GC_REF_FACTS=1`, seven samples of
`BenchmarkGCStructGetNonFinalInstruction` changed from a 78.67 ns/op default
median to a 2.680 ns/op median. This is a 96.6% reduction for the focused case.
Explain output changes from two synchronous helper sites (constructor plus get) to
one constructor helper, one native resolution, and
`gc-nonfinal-struct-get-specialize=1`. Both forms remain at 0 B/op and 0
allocations/op in the warmed benchmark. Focused generated code grows from 749 to
906 bytes (**+157 bytes, +21.0%**) because the one-site module replaces a compact
helper call with the complete checked native resolver path.

The optimization remains behind the existing default-off `gc-ref-facts` policy.
Promoting that full policy still requires broad compile-memory and real-workload
qualification; this microbenchmark alone does not justify changing its default.

## Remaining opportunities

1. **General non-final native subtype checks.** Publish a bounded, versioned native
   subtype representation or equivalent shared stub input. This can remove the
   70-80 ns synchronous transition when no exact final receiver fact exists. It
   requires a native ABI change and exact codec/collector validation.
2. **Loop-invariant checked-object resolution.** The final `struct.get` fixture
   repeats full compact-handle, space, extent, and type validation each iteration.
   A conditional loop precheck could retain a raw resolved address only when the
   object local is invariant and the loop has no allocation, call, host re-entry,
   collection, or other relocation edge. Zero-iteration and trap timing require a
   guarded/versioned loop rather than unconditional hoisting.
3. **Loop-invariant casts.** The final cast source is invariant in this fixture,
   but moving a trapping cast before a zero-iteration loop is invalid. A guarded
   fast loop plus exact slow loop could perform one cast check when the loop will
   execute and no body edge can mutate or replace the source.
4. **Reference-global value pinning.** The final cast loop loads the global
   directory, cell, and compact reference each iteration. Call-free functions
   could pin eligible local reference globals after proving host mutation cannot
   occur during the invocation. This needs register-pressure and root/alias
   qualification before retention.

The existing shared-stub and straight-line resolver-reuse toggles did not show a
useful improvement for the one-site final fixtures. The larger opportunities are
therefore subtype-aware native checks and safe loop-invariant validation, not a
new one-site stub threshold.
