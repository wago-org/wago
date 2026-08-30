# Dewdrop blockers #551 and #552

Date: 2026-08-30

## Scope

This change addresses two Dewdrop blockers:

- configurable native execution stack capacity;
- direct AMD64 lowering for non-final defined `ref.cast` and `ref.test`.

## Native stack capacity

The runtime keeps the historical 4 MiB default and the fixed 256 KiB fence
margin. `RuntimeConfig.WithNativeStackBytes` and `wago run --native-stack`
select 512 KiB through 1 GiB. Capacities must be 16-byte aligned.

The one-slot engine cache now matches capacities exactly. A mismatched cached
mapping is unmapped before a new engine is allocated. Synchronous host re-entry
uses the active instance capacity, so parked outer activations and nested calls
retain separate equal-capacity stacks.

A focused function with 4,095 i64 locals and bounded recursion traps at depth 120
on the 4 MiB default and completes on an 8 MiB stack. This test keeps the
existing prologue fence and trap path unchanged.

Memory cost is explicit: each live or cached engine retains the selected mapping
instead of a fixed 4 MiB mapping. The cache remains one slot and is not
partitioned or unbounded.

## Native defined-type checks

Collector subtype forests already use packed DFS intervals. The unreleased
native GC ABI remains at version 1 and now includes:

- an immutable interval backing pointer;
- an interval count.

The added collector-view prefix cost is 16 bytes. The interval table still costs
8 bytes per canonical type. Runtime-domain growth quiesces the domain invocation
lease before `Collector.AddTypes` republishes the pointer and count. Generated
code reloads both fields for each
check and does not retain an object pointer across a call, helper, allocation,
or safepoint.

AMD64 defined struct/array checks now:

1. apply exact null semantics;
2. reject tagged i31 values;
3. map the module-local target to its canonical collector type;
4. validate handle range, liveness, heap space, backing range, and object extent;
5. read the object's canonical type ID;
6. use canonical equality for exact casts or DFS interval containment otherwise;
7. return the original compact reference, a zero/one test result, or a trap.

Focused coverage includes successful open-supertype checks, unrelated types,
null variants, exact casts, a 32-type chain, shifted module-local canonical IDs,
nursery and promoted objects, full collection, Tiny stress collection, and a
large array object. The existing official recursive-group and linked-domain
suites continue to cover those graph and provider boundaries.

## Measurement

Host: AMD Ryzen 7 8845HS, linux/amd64. Command:

```sh
go test ./src/wago -run '^$' \
  -bench 'BenchmarkGCRef(Test|Cast)NonFinalInstruction$' \
  -benchtime=200ms -count=2
```

Results:

| Benchmark | ns/op range | B/op | allocs/op |
|---|---:|---:|---:|
| non-final `ref.cast` | 3.541-3.587 | 0 | 0 |
| non-final `ref.test` | 3.654-3.655 | 0 | 0 |

A separate final/non-final cast run measured final casts at 3.505-3.543 ns/op
and non-final casts at 3.503-3.568 ns/op. With `-tags wago_gcstats`, 1,000
steady-state non-final casts and tests each report zero synchronous GC helper
calls.

## Release-size effect

A local reproducible `scripts/size-card.sh` run with Go 1.24.4 and TinyGo 0.41.1
measured deltas against `origin/main` of 0 bytes for manager, +20,480 bytes for
Standard runtime, +12,288 bytes for Minimal runtime, and +6,000 bytes for the
TinyGo Minimal runtime. The existing Standard budget still passes in canonical
CI. The Minimal and TinyGo Minimal ceilings increase by 20,000 and 8,000 bytes,
respectively; this keeps the budget change bounded to the measured CLI/runtime
configuration and native subtype-check implementation.
