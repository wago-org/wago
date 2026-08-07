# Project Forge: concurrent native Wasm and threads/atomics

> **Status: proposed implementation plan (2026-08-06).**
> [ROADMAP.md](../ROADMAP.md) remains authoritative for scheduling and feature
> status. This document defines the architecture, staged product boundary,
> implementation order, and acceptance gates for the roadmap's threads and
> atomics bigger bet.

## 1. Outcome

Build a bounded first threads product in which multiple Wago instances execute
native Wasm concurrently against one shared linear memory, with classic
WebAssembly atomic loads, stores, read-modify-write operations,
compare-exchange, fences, and eventually wait/notify on amd64 and arm64.

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
- shared memory32 with an exact declared maximum;
- distinct concurrently executing instances;
- numeric globals, parameters, results, and functions; and
- classic scalar atomic memory instructions.

Initially reject:

- shared-everything threads and GC object atomics;
- exception handling and WasmGC;
- host imports and cross-instance function calls;
- simultaneous calls into the same instance;
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

