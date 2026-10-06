# V8 Wasm → host source investigation

Inspected V8 revision `00075520df30f6d891cd9cf5f9f497f624a2efaa` from `https://github.com/v8/v8.git`, shallow sparse clone at `/tmp/wago-host-latency-v8`, on 2026-10-04. This is a pinned source investigation, not a V8 latency measurement. No V8 build or benchmark was run. Files outside the sparse checkout were read with `git show HEAD:<path>`.

The useful architectural pattern is a typed Wasm call into an already selected adapter, with an explicit implicit-context argument. Generic marshaling is a startup/fallback tier. V8 owns both its Wasm and JS stack maps; its restricted fast C API is a separate contract, not evidence that an arbitrary Go callback can skip Go runtime entry.

## Source findings

### Binding happens before the hot call

Instantiation resolves the import kind and canonical signature. Cross-instance Wasm imports get a Wasm target directly; Fast API imports get a dedicated compiled wrapper; ordinary JS and C API imports obtain a wrapper from the import-wrapper cache. JS formal arity is part of that decision. [Instantiation](https://github.com/v8/v8/blob/00075520df30f6d891cd9cf5f9f497f624a2efaa/src/wasm/module-instantiate.cc#L1700-L1789).

The shared cache key contains import call kind, canonical signature, expected JS arity, and suspend mode. Cache lookup either returns an existing wrapper, chooses a generic/invalid-signature builtin, or compiles a new wrapper. A Fast API wrapper can be specific to the instantiation and is explicitly not shared. [Key](https://github.com/v8/v8/blob/00075520df30f6d891cd9cf5f9f497f624a2efaa/src/wasm/wasm-import-wrapper-cache.h#L16-L39), [lookup and compilation](https://github.com/v8/v8/blob/00075520df30f6d891cd9cf5f9f497f624a2efaa/src/wasm/wasm-import-wrapper-cache.cc#L47-L92), [special wrapper](https://github.com/v8/v8/blob/00075520df30f6d891cd9cf5f9f497f624a2efaa/src/wasm/wasm-import-wrapper-cache.cc#L154-L162).

Binding stores a wrapper handle plus a `WasmImportData` object in the imports dispatch table. Generated Wasm code loads the target and implicit argument from that table and emits a Wasm indirect-function call. This is a per-import adapter rather than a universal host dispatcher that resolves import names each time. [Binding](https://github.com/v8/v8/blob/00075520df30f6d891cd9cf5f9f497f624a2efaa/src/wasm/wasm-objects.cc#L1321-L1334), [target/context loads](https://github.com/v8/v8/blob/00075520df30f6d891cd9cf5f9f497f624a2efaa/src/wasm/turboshaft-graph-interface-inl.h#L65-L103), [emitted call](https://github.com/v8/v8/blob/00075520df30f6d891cd9cf5f9f497f624a2efaa/src/wasm/turboshaft-graph-interface.cc#L2724-L2735).

### Generic Wasm → JS tiers into a signature-specific wrapper

The generic wrapper is allowed for compatible JSFunction imports on listed supported architectures, with no suspension and the generic-wrapper flag enabled. It switches to the central stack if needed, publishes the signature before a possible GC, decrements a wrapper budget, allocates a zeroed argument array, walks parameter types, converts values, and invokes `CallVarargs`. Numeric conversion and reference conversion use separate passes. [Eligibility](https://github.com/v8/v8/blob/00075520df30f6d891cd9cf5f9f497f624a2efaa/src/wasm/wasm-objects.cc#L2723-L2739), [generic wrapper](https://github.com/v8/v8/blob/00075520df30f6d891cd9cf5f9f497f624a2efaa/src/builtins/wasm-to-js.tq#L59-L197).

When its budget reaches zero, runtime resolves the ultimate callable and calls `GetCompiled` with the specialized cache key; the source explicitly says that operation updates the code-pointer-table entry in place. Hot calls can therefore reach specialized code without repeatedly rebuilding the wrapper selection. [Tier-up](https://github.com/v8/v8/blob/00075520df30f6d891cd9cf5f9f497f624a2efaa/src/runtime/runtime-wasm.cc#L559-L595).

The compiled wrapper declares each incoming Wasm parameter with the representation dictated by the fixed signature. It constructs argument nodes at compilation time, fills missing formal parameters with `undefined`, and emits either a known JSFunction call descriptor or a general callable builtin. Its single-result conversion is emitted for the fixed return type; multiple results use iterable-to-fixed-array conversion. Unlike the generic builtin, this builder does not emit a runtime parameter-type traversal and generic argument array for the ordinary JSFunction path. [Compiled parameters and argument layout](https://github.com/v8/v8/blob/00075520df30f6d891cd9cf5f9f497f624a2efaa/src/compiler/turboshaft/wasm-wrappers-inl.h#L387-L440), [known/general call](https://github.com/v8/v8/blob/00075520df30f6d891cd9cf5f9f497f624a2efaa/src/compiler/turboshaft/wasm-wrappers-inl.h#L485-L542), [results](https://github.com/v8/v8/blob/00075520df30f6d891cd9cf5f9f497f624a2efaa/src/compiler/turboshaft/wasm-wrappers-inl.h#L555-L585).

Specialization does not erase JS semantics: i32 becomes Number, i64 becomes BigInt, and floating-point arguments become Number through conversion helpers. Return conversion likewise follows the expected Wasm type. [Numeric conversions](https://github.com/v8/v8/blob/00075520df30f6d891cd9cf5f9f497f624a2efaa/src/compiler/turboshaft/wasm-wrappers-inl.h#L109-L125), [result conversions](https://github.com/v8/v8/blob/00075520df30f6d891cd9cf5f9f497f624a2efaa/src/compiler/turboshaft/wasm-wrappers-inl.h#L555-L578).

### GC, stack switching, exceptions, and reentry are architectural obligations

The generic wrapper's signature spill identifies incoming reference slots until values have been transferred into its managed argument array. It clears the signature before invoking JS. The frame walker reads that signature and stops scanning incoming arguments once it is cleared. Compiled Wasm/Wasm-to-JS frames use code-manager safepoint information; the compiler records tagged stack slots from reference maps. These are concrete GC metadata contracts, not just keeping the adapter function alive. [Generic roots](https://github.com/v8/v8/blob/00075520df30f6d891cd9cf5f9f497f624a2efaa/src/builtins/wasm-to-js.tq#L62-L66), [end of incoming-root lifetime](https://github.com/v8/v8/blob/00075520df30f6d891cd9cf5f9f497f624a2efaa/src/builtins/wasm-to-js.tq#L189-L197), [frame scanning](https://github.com/v8/v8/blob/00075520df30f6d891cd9cf5f9f497f624a2efaa/src/execution/frames.cc#L1939-L1965), [safepoint lookup](https://github.com/v8/v8/blob/00075520df30f6d891cd9cf5f9f497f624a2efaa/src/execution/frames.cc#L1802-L1806), [tagged-slot recording](https://github.com/v8/v8/blob/00075520df30f6d891cd9cf5f9f497f624a2efaa/src/compiler/backend/code-generator.cc#L612-L630).

Central-stack switching has an explicit fast skip: read `is_on_central_stack_flag` and only switch when false. The switch saves source FP and target SP in stack metadata, changes stack limits, and changes SP; the normal return restores these only when a switch occurred. Windows/hardware-sandbox configurations use C helpers for parts of that work. [Switch and skip](https://github.com/v8/v8/blob/00075520df30f6d891cd9cf5f9f497f624a2efaa/src/wasm/turboshaft-graph-interface-inl.h#L189-L256), [restore](https://github.com/v8/v8/blob/00075520df30f6d891cd9cf5f9f497f624a2efaa/src/wasm/turboshaft-graph-interface-inl.h#L259-L298).

Ordinary JS call descriptors explicitly permit throwing. Suspension has additional checks rejecting active JS frames. Exception unwind separately restores stack flags and limits when its target is on a secondary stack; it cannot rely exclusively on the wrapper's normal return. After a Wasm call, generated code reloads cached memory because calls may mutate instance fields. These source behaviors are relevant to host reentry and memory growth. [Throwing call](https://github.com/v8/v8/blob/00075520df30f6d891cd9cf5f9f497f624a2efaa/src/compiler/turboshaft/wasm-wrappers-inl.h#L458-L496), [unwind restoration](https://github.com/v8/v8/blob/00075520df30f6d891cd9cf5f9f497f624a2efaa/src/execution/isolate.cc#L2949-L2959), [memory reload](https://github.com/v8/v8/blob/00075520df30f6d891cd9cf5f9f497f624a2efaa/src/wasm/turboshaft-graph-interface.cc#L8661-L8671).

### C API and Fast API are different paths

The Wasm C API wrapper allocates a stack values area sized for the larger of packed parameters/results, writes typed arguments, records C-entry FP, and invokes a C ABI function taking embedder data and that area. A nonzero return is rethrown as an explicit-context exception; success loads typed results. This wrapper includes a TODO warning that reference arguments need GC-safe treatment rather than raw pointers. It is not a typed direct callback with no marshaling. [C API wrapper](https://github.com/v8/v8/blob/00075520df30f6d891cd9cf5f9f497f624a2efaa/src/compiler/turboshaft/wasm-wrappers-inl.h#L652-L745).

That C call reaches `FuncData::v8_callback`, which establishes isolate and handle scopes, creates parameter/result `vec<Val>` arrays, decodes the packed buffer by type, calls the user's callback, returns a trap pointer on error, and copies results back. A wrapper-only instruction count would omit substantial embedding-API overhead. [C API callback](https://github.com/v8/v8/blob/00075520df30f6d891cd9cf5f9f497f624a2efaa/src/wasm/c-api.cc#L1912-L1992).

Fast API is more restricted: its public contract forbids JS heap allocation and triggering JS execution. Fallback-support callbacks can request a slow callback and must avoid duplicating visible side effects before that request. The direct Wasm import wrapper is additionally gated by `wasm_unsafe_fast_api_wrapper`, first-overload matching, and signature support; its builder checks the unsafe flag and assumes conversion cannot fail. [Public contract](https://github.com/v8/v8/blob/00075520df30f6d891cd9cf5f9f497f624a2efaa/include/v8-fast-api-calls.h#L24-L54), [selection restriction](https://github.com/v8/v8/blob/00075520df30f6d891cd9cf5f9f497f624a2efaa/src/wasm/module-instantiate.cc#L342-L353), [wrapper assumption](https://github.com/v8/v8/blob/00075520df30f6d891cd9cf5f9f497f624a2efaa/src/compiler/turboshaft/wasm-wrappers-inl.h#L861-L866), [typed argument call](https://github.com/v8/v8/blob/00075520df30f6d891cd9cf5f9f497f624a2efaa/src/compiler/turboshaft/wasm-wrappers-inl.h#L889-L930).

There is also an optimized well-known Fast API import route. Instantiation records the native target/signature and callback data. Generated Wasm code can emit the C ABI call directly, conditionally switch stacks, reload memory, check the isolate exception, and use the ordinary import call on conversion failure. This route is distinct from the unsafe wrapper above. [Binding](https://github.com/v8/v8/blob/00075520df30f6d891cd9cf5f9f497f624a2efaa/src/wasm/module-instantiate.cc#L488-L532), [direct call and fallback](https://github.com/v8/v8/blob/00075520df30f6d891cd9cf5f9f497f624a2efaa/src/wasm/turboshaft-graph-interface.cc#L2150-L2224).

## Wago inferences and candidate work

These are implementation hypotheses, not V8 latency findings.

Wago already selects specialized scalar portals before entering the guest (`src/wago/hostcall.go`, `callNativeSyncWithTrapContextUnprepared`) and has a live normal-Go bridge (`src/core/runtime/inline_host.go`). Repeating that work as another cached Go closure is not a new architectural improvement. Its bounded Caller path (`src/wago/host_execution.go`, `dispatchBoundedHostView`) also deliberately preserves native lease release/reacquisition, invocation identity, monotonic generation, and scope cleanup. Those costs must be assessed against the actual Go contract, not removed because V8's restricted C callback omits them.

The most useful next architectural experiment is an ABI-specialized adapter that transfers scalar arguments directly into the supported Go ABI and returns scalar results directly, while preserving a normal Go frame, callback roots, panic cleanup, and lease/migration checks. Compare it with the existing four-word fixed portal using matched stateful callbacks and both full round-trip and batched measurements. Prototype one signature before broadening; a direct C function pointer is not an ordinary Go func value.

A broader follow-up is per-import adapter metadata/target selection, analogous to V8's target-plus-context dispatch entry. That could extend the live-Go bridge beyond its sole-import admission without adding a general import-kind switch on every callback. Immutable type/ABI layout can be prepared at bind time; revocable execution ownership, memory/context versions, cancellation, and Caller validity must remain live checks. Measure multiple imports and reference/memory cases separately rather than treating a bounded numeric microbenchmark as general parity.

The source trace supplies no claim that V8 beats Wasmtime or Wago by any particular number. Any eventual comparison needs a pinned V8 executable, equivalent callback effects and value semantics, warmed wrapper tier, and explicit accounting for JS conversion versus Go/C API entry.

## Follow-up: bounded multiple Go imports

The later `multi-import-separated` experiment applies the per-import metadata
idea to Wago's existing live Go bridge. Instantiation prepares an immutable
raw-slot signature table for 2–64 ordinary numeric Go imports. Each host transfer
checks the active control frame, unsigned import index and exact selected shape
before borrowing slots or invoking a callback. The adapter then chooses the
bound typed/HostCall/Caller callback while preserving normal Go stack growth,
GC roots, panic cleanup, cancellation, native ownership and Caller generations.

A separate warm numeric driver and assembly owner keep the established
single-import bridge unchanged. Multi-import layout is allocated lazily behind
one appended sidecar pointer. Paired controls motivated early multi-import
admission on ARM64 and admission after the existing single-import cache on
AMD64. This extends the bounded numeric contract; it does not remove Go's
runtime boundaries or establish V8/Wasmtime parity. Measurements, control
regressions and validation are recorded in the host-latency README and both
`multi-import-separated` evidence directories. No V8 runtime latency was measured.

The bounded borrowed-view follow-up applies the same static target plus rooted
context shape to HostCall on both architectures and Caller on ARM64. AMD64
Caller retains its method adapter after the context variant regressed round
trips. Final batches improve modestly, with mixed round-trip results; see the
[view context measurements](README.md#static-target-and-rooted-context-for-borrowed-views).
This remains a source-inspired Wago experiment, not a V8 latency measurement.

The ARM64 compiler follow-up classifies ordinary Go imports at instantiation
with a tagged dispatch word. Bounded numeric guest functions can then call the
existing host protocol directly, skipping the generic cross-instance wrapper.
Wasm imports retain the wrapper route, and cached artifacts use untagged
metadata for compatibility. This applies V8's binding-time adapter selection
pattern while retaining Wago's Go runtime and Caller contracts. Reversed paired
measurements improve all three callback kinds across the tested APIs; see
[direct Go import measurements](README.md#direct-go-imports-in-bounded-arm64-functions).
This change is ARM64-only and does not establish Wasmtime parity or a V8 latency
result.

The subsequent AMD64 implementation also selects the direct Go protocol from
the bound dispatch word. Final same-executable enabled/disabled comparisons in
both orders and a separate preceding-binary comparison improve all measured
cases. Earlier multiple-import regressions and image/layout uncertainty remain
in the evidence. See [AMD64 direct import measurements](README.md#direct-go-imports-on-amd64).
The scope remains bounded numeric guest bodies with normal Go runtime and
Caller behavior; Wasmtime parity remains unfinished.
