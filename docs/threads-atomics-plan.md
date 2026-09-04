# Project Forge: concurrent native Wasm and threads/atomics

> **Status: bounded first product implemented (2026-08-06).**
> [ROADMAP.md](../ROADMAP.md) remains authoritative for scheduling and feature
> status. Sections 1–9 retain the architecture and acceptance gates that drove
> the implementation; section 10 records the resulting product and evidence.

## 1. Outcome

Build a bounded first threads product in which multiple Wago instances execute
native Wasm concurrently against one shared linear memory, with classic
WebAssembly atomic loads, stores, read-modify-write operations,
compare-exchange, fences, and wait/notify on amd64 and arm64.

The first product is deliberately narrower than every combination Wago can
eventually support. It must provide real parallel native execution, not merely
correct atomic instruction results while the runtime serializes all Wasm calls.

## 2. The actual first blocker

The decoder and validator already understand shared memories and the `0xfe`
atomic instruction family. `wago.NewSharedMemory` already provides one backing
that compatible instances may import concurrently. The missing center is native
execution.

`src/wago/instance_native_context.go` currently guards every native activation
with one process-wide mutex. This is intentional: Wago's WARP-derived ABI keeps
mutable execution control in basedata at negative offsets from the linear-memory
base, and shared-memory tenants rebind that state before entry. Without the
mutex, two tenants could overwrite each other's trap state, stack fence,
arguments, results, import context, GC view, or memory directory.

Lowering atomic machine instructions without fixing that ownership model would
pass isolated semantic tests while leaving all Wasm execution serialized. The
first architectural task is therefore to separate private execution context
from shared linear-memory backing for an exact class of threaded modules.

## 3. Design rule: a deep native-entry module

Keep the external interface unchanged: callers continue to use `Memory`,
`Instantiate`, and `Call`. Native entry owns the complexity behind its existing
seam.

For ordinary modules, preserve the current process-wide serialized path and its
generated code. For eligible threaded modules, native entry must:

- give every instance private basedata, trap state, argument/result storage,
  stack fence, import context, and memory-directory storage;
- share only the declared shared linear-memory pages;
- route shared memory zero through an instance-owned directory entry rather
  than treating the shared backing's negative offsets as instance state;
- use an instance-local execution lease, allowing distinct instances to enter
  native code concurrently; and
- keep at most one public native activation per individual instance until its
  mutable call scratch is made invocation-local.

This is an internal seam. Do not add public scheduler, executor, thread, or
waiter interfaces merely to make the implementation testable. Tests should use
real instances and real shared memory through the same interface as callers.

## 4. First executable boundary

The initial threaded product admits only:

- explicit bounds checks;
- one memory32; shared memory must be an exact-maximum import, while unshared
  memory may be local or imported;
- distinct concurrently executing instances, with same-instance calls
  serialized around reusable invocation state;
- numeric globals, parameters, results, and functions, excluding mutable
  global imports; and
- classic scalar atomic memory instructions.

Initially reject:

- shared-everything threads and GC object atomics;
- exception handling and WasmGC;
- host imports and cross-instance function calls;
- mutable global imports;
- memory64 and multi-memory;
- signal-backed guard execution;
- snapshots and domain snapshots;
- shared-memory growth, until its publication and directory-cache rules are
  explicitly synchronized; and
- wait/notify until the bounded waiter implementation exists.

Every unsupported combination must reject before Railshot begins emitting
code. The boundary may expand only with focused execution, lifecycle, codec,
and platform tests.

## 5. Implementation sequence

### Phase 1: baseline and failing vertical test

Work in an isolated main-based worktree. Record current compile, instantiate,
entry, code-size, allocation, and footprint baselines before editing.

Add a failing end-to-end test that:

1. creates one `NewSharedMemory`;
2. compiles an exact bounded atomic module;
3. instantiates several copies against that memory;
4. starts calls simultaneously from separate goroutines;
5. performs repeated `i32.atomic.rmw.add`; and
6. proves native overlap and the exact final counter value.

The test must distinguish true overlap from interleaved but serialized calls.

### Phase 2: private control and shared backing

Add an internal compiled execution-mode bit derived from exact module facts.
Do not infer safety at instantiation from incomplete metadata.

For the threaded mode:

- allocate a private control `JobMemory` or equivalent stable basedata region
  for each instance;
- retain the imported shared `Memory` only as linear-memory backing;
- create an immutable instance-owned memory-zero directory entry;
- lower all shared memory-zero addresses and size checks through that entry;
- bind only private control fields at native entry;
- bypass the process-wide execution mutex while holding an instance-local
  lease; and
- make close wait for the instance's active invocation before releasing private
  control or detaching the shared backing.

The ordinary path must not gain an extra conditional in generated code. The
choice belongs at compile/instantiate time.

Checkpoint:

- two eligible instances visibly overlap in native execution;
- ordinary modules remain serialized and generate unchanged code;
- one instance cannot race its own argument, result, or trap scratch;
- producer/consumer close ordering remains correct; and
- shared memory cannot unmap while any importer is active.

### Phase 3: architecture-neutral atomic description

Reuse the frontend's decoded atomic kinds and memargs, but keep Railshot direct:
do not introduce an instruction IR. Add a small architecture-neutral
description containing only the facts both backends require:

- operation;
- access width;
- result width;
- natural alignment;
- memory index and offset; and
- load, store, RMW, compare-exchange, fence, wait, or notify class.

Both backends must consume the same description. Invalid subopcodes, alignment,
and memory forms remain frontend errors rather than backend fallthrough.

### Phase 4: native atomic lowering

Add `emitFE` dispatch to amd64 and arm64 Railshot.

Implement:

- `atomic.fence`;
- 8-, 16-, 32-, and 64-bit atomic loads;
- 8-, 16-, 32-, and 64-bit atomic stores;
- `rmw.add`, `sub`, `and`, `or`, `xor`, and `xchg`;
- compare-exchange; and
- correct narrow-result zero extension.

Every operation must:

- compute the effective address with overflow checks;
- trap on non-natural alignment;
- complete the full bounds check before any write;
- preserve memory on a trapping write or RMW;
- obey the proposal's ordering contract; and
- remain a direct native-code hot path with no Go helper.

Land `i32.atomic.rmw.add` on both architectures first, then fill the matrix.
This establishes one shared backend contract before either implementation grows
independently.

AMD64 should use the appropriate locked instructions and bounded CAS loops.
ARM64 should use the supported baseline exclusive-load/store sequences; LSE may
be added later only behind an explicit CPU gate.

### Phase 5: bounded wait/notify

Attach waiter state lazily to the shared-memory backing, never to one importing
instance. Conceptually, the internal interface is:

```go
Wait32(context, offset, expected, timeout) result
Wait64(context, offset, expected, timeout) result
Notify(offset, count) notified
```

The implementation must hide waiter identity, parking, timeout races,
cancellation, notification ordering, memory close, and address reuse.

Requirements:

- no goroutine per memory;
- no permanent entry per historically touched address;
- storage proportional to active waiters;
- removal after the final waiter leaves;
- compare-before-park atomic with waiter registration;
- no missed notification between comparison and sleep;
- notify isolation by shared-memory identity and byte offset;
- bounded notification counts;
- close and cancellation wake parked calls; and
- no waiter may observe an unmapped backing.

Generated Wasm may reach wait/notify through the existing synchronous helper
machinery. Atomic loads, stores, RMW, and compare-exchange must never take that
helper path.

If the waiter design becomes unbounded or compromises the native atomic path,
finish and publish the non-wait atomic product first and retain this phase as the
next focused change.

### Phase 6: feature and artifact productization

After native concurrency and atomic execution are proven:

- add `CoreFeatureThreads`;
- admit shared memories and `0xfe` operations through the frontend feature set;
- record the feature during byte-backed required-feature scanning;
- include it in runtime compilation and artifact-cache identity;
- persist exact shared-memory and required-feature metadata in `.wago`, with a
  codec version bump if the wire contract changes;
- expose it in deterministic module inspection; and
- report exact unsupported platform or feature combinations.

Threads remain optional and must not be added to `CoreFeaturesV3`.
`SupportedFeatures` should advertise them only on a platform/backend whose
native execution and conformance gates pass.

## 6. Verification

### Instruction semantics

Cover every width and operation, including:

- zero and all-ones operands;
- signed-looking bit patterns;
- wrapping arithmetic;
- compare-exchange success and failure;
- narrow result extension;
- unaligned addresses;
- effective-address overflow;
- end-of-memory accesses; and
- unchanged memory after every trapping write/RMW form.

### Concurrency and lifecycle

Exercise:

- shared counters;
- spin and ticket locks;
- compare-exchange contention;
- a producer/consumer ring;
- barriers;
- wait/notify success, mismatch, timeout, cancellation, and count limits;
- memory close during active calls and waits;
- importer rollback;
- independent instance close order; and
- repeated instantiate/close cycles.

Run Go race-focused tests around all Go-owned lifecycle and waiter state. Native
memory races must be tested through deterministic atomic invariants and stress
loops because the Go race detector does not instrument generated machine code.

### Proposal and regression corpora

- Execute the checked-in threads proposal fixtures under
  `tests/spec/proposals/threads`.
- Execute and exactly account for the checked-in atomic regression modules.
- Remove broad `unsupported atomic` matching as operations are admitted.
- Preserve malformed, invalid, unlinkable, accepted, and still-gated counts
  separately.
- Keep existing explicit- and signal-bounds suites green even while the new
  product is explicit-only.

### Cross-platform gates

- Native amd64 and arm64 execution for the admitted matrix.
- Cross-compilation for all six release targets.
- Native CI on every platform that advertises the feature.
- Clear compile-time rejection elsewhere.

## 7. Performance and footprint gates

Record raw values for:

- uncontended atomic load, store, RMW, and compare-exchange;
- two-, four-, and eight-contender throughput;
- wait/notify latency;
- ordinary and threaded native-entry latency;
- compile time and allocations;
- warmed execution allocations;
- generated code size;
- `Compiled`, `Instance`, `Memory`, and waiter-sidecar sizes; and
- RSS/PSS where concurrent shared-memory savings are material.

Acceptance:

- ordinary module code bytes remain unchanged;
- ordinary entry latency remains within measurement noise;
- atomic load/store/RMW/CAS uses direct native code;
- warmed native atomic loops allocate zero bytes;
- waiter storage is bounded by active waiters;
- contention tests return exact results;
- eligible distinct instances do not acquire the process-wide native execution
  mutex; and
- any optional fast path earns its compile-time, code-size, and footprint cost
  with measured results.

## 8. Stop conditions

- Do not publicly advertise threads while native calls remain process-wide
  serialized.
- Do not remove the global execution lease for ordinary modules as part of this
  project.
- Do not introduce a universal second context register unless the restricted
  directory-routed design is proven insufficient and the wider ABI cost is
  measured.
- Do not admit shared-memory growth until size publication and concurrent
  directory refresh are exact.
- Do not retain an unbounded waiter map or one goroutine per waiter/memory.
- Do not let a semantic test silently substitute serialized calls for actual
  concurrency.
- Do not broaden into WASI thread creation, component-model threading, stack
  switching, or shared-everything GC atomics in this phase.

## 9. Suggested atomic history

1. `runtime: isolate threaded instance control from shared memory`
2. `compiler: admit bounded shared-memory atomic modules`
3. `amd64: lower classic WebAssembly atomic operations`
4. `arm64: lower classic WebAssembly atomic operations`
5. `runtime: add bounded atomic wait and notify`
6. `wago: productize threads and atomic execution`
7. `tests: execute the threads proposal corpus`
8. `docs: record the threads boundary and performance`

No public feature commit should land before the native-overlap test, atomic
corpus, lifecycle tests, and both advertised backends agree.

## 10. Implemented product and evidence

### Enabling it

Threads are experimental and intentionally absent from `CoreFeaturesV3`.
Callers opt in, select explicit bounds, and provide one exact-size shared
memory import:

```go
config := wago.NewRuntimeConfig().
	WithCoreFeatures(wago.CoreFeaturesV2 | wago.CoreFeatureThreads).
	WithBoundsChecks(wago.BoundsChecksExplicit)

compiled, err := wago.Compile(config, wasmBytes)
memory, err := wago.NewSharedMemory(1, 1)
instance, err := wago.Instantiate(compiled, wago.Imports{"env.memory": memory})
```

The admitted module boundary is one memory32. Shared memory must be an imported
memory with an exact maximum; unshared memory may be local or imported. Modules
may contain numeric functions and globals but no mutable global imports, no
function imports, no tables, tags, or segments, and memory operations are
limited to the classic atomic family.
Memory growth, memory64, multi-memory, signal bounds, WasmGC, exceptions,
snapshots, and shared-everything GC remain rejected before native code
generation. Same-instance concurrent entry is accepted but serialized. The
feature is advertised only on Linux and macOS amd64/arm64.

Distinct eligible instances have private basedata and execution leases, so they
can execute native Wasm concurrently against the same backing. Ordinary modules
retain the process-wide execution lease and produce byte-identical code whether
or not Threads is present in the configured feature set.

### Native implementation

Both Railshot backends consume one architecture-neutral atomic descriptor.
AMD64 lowers the direct path with locked operations, `XCHG`, `MFENCE`, and CAS;
ARM64 uses acquire/release loads and stores, `DMB`, and exclusive-load/store
loops. Loads, stores, add/sub/and/or/xor/exchange RMWs, compare-exchange, and
fence remain direct native code. Address overflow, bounds, and natural
alignment are checked before mutation.

Wait32, wait64, and notify use the synchronous helper boundary. Their shared
memory sidecar is allocated lazily, keyed by exact memory identity and offset,
stores only active waiters, and disappears after the last waiter leaves. It
does not create a goroutine per waiter or memory. Timeout, cancellation,
notification, instance close, and memory close converge on the same bounded
removal path. On unshared memory, notify checks alignment and bounds and returns
zero; wait checks alignment and then traps with `TrapExpectedSharedMemory`
before reading memory.

The compiled artifact persists the exact Threads requirement and
wait-helper admission. Direct-only atomic artifacts do not acquire helper
admission, and artifacts with an unknown format version are rejected rather
than reinterpreted.

### Verification record

Local execution on an Apple M4 Max passed native Darwin/arm64 and Rosetta
Darwin/amd64. The checked-in official `atomic.0.wasm` bodies produced 63 setup
actions, 149 exact returns, and 45 exact traps. The official wait/notify module
produced three exact returns and six bounds/alignment traps. The product also
passes the full operation/width matrix, end-of-memory and effective-address
overflow checks for every write/RMW form, contention invariants, true-overlap
rendezvous, waiter lifecycle tests, and 250 instantiate/invoke/close cycles.

Race-focused waiter and lifecycle tests pass under `go test -race`. Tests that
hold generated native code in a deliberate spin while a Go controller publishes a
shared-memory release must reserve at least two `GOMAXPROCS`: generated code does
not yield its P to Go's scheduler, so a one-P test process cannot run the controller.
Every such rendezvous must also use bounded phase waits with state diagnostics rather
than relying on the package timeout. Test binaries cross-compile for linux, darwin,
and windows on amd64 and arm64; Windows
does not advertise Threads, and this local run did not provide native Linux or
Windows execution.

Relevant local commands included:

```sh
go test ./src/core/encoder/arm64 \
  ./src/core/compiler/backend/railshot/shared \
  ./src/core/compiler/backend/railshot/arm64 \
  ./src/core/runtime ./src/wago -count=1
go test -race ./src/wago -run 'Threads.*(Wait|Close|Contention|Barrier)'
```

### Performance and footprint

Measurements below are raw `go test -bench` ranges on Apple M4 Max,
Darwin/arm64, with a 200 ms benchtime and three samples:

| Operation | Time | Generated code | Allocations |
|---|---:|---:|---:|
| atomic load | 88.02–95.03 ns/op | 576 B | 0 |
| atomic store | 88.89–89.86 ns/op | 576 B | 0 |
| `rmw.add` | 90.11–93.28 ns/op | 304 B | 0 |
| compare-exchange | 90.16–94.03 ns/op | 328 B | 0 |
| wait mismatch | 249.9–261.6 ns/op | 996 B | 200 B/op, 5 allocs |
| notify with no waiters | 247.7–252.8 ns/op | 996 B | 200 B/op, 5 allocs |
| parked wait/notify round trip | 4.188–4.291 µs/op | — | 1129–1130 B/op, 21 allocs |

The round-trip benchmark includes its caller goroutine and coordination. A
warmed direct atomic call has a hard zero-allocation test. Ordinary entry was
79.31–81.55 ns/op versus 89.70–91.77 ns/op for threaded atomic-load entry, both
at zero allocations. Compiling the wait/notify fixture took 15.2–17.8 µs and
53,891–53,892 B across 140 allocations.

Static Go object sizes were 680 B for `Compiled`, 832 B for `Instance`, 16 B for
`Memory`, 24 B for `memoryState`, 16 B for the active waiter sidecar, and 40 B
per active waiter.

The review fix that serializes the reusable same-instance `Invoke` scratch adds
one 8 B mutex to the threaded-only memory-directory sidecar. A six-sample,
300 ms A/B on an Apple M4 Max measured atomic load at 90.09 -> 93.49 ns/op
(1.038x), store at 88.03 -> 90.76 ns/op (1.031x), `rmw.add` at 90.57 ->
93.85 ns/op (1.036x), compare-exchange at 91.33 -> 94.15 ns/op (1.031x),
wait mismatch at 259.52 -> 263.52 ns/op (1.015x), and empty notify at 266.25 ->
267.87 ns/op (1.006x). The isolated threaded-entry result was 91.02 ->
95.86 ns/op (1.053x). All direct cases retained zero allocations; ordinary
entry did not acquire the new mutex.
