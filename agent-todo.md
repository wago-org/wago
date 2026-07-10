# Agent Todo

## WebAssembly 2.0 Completion Roadmap

Goal: complete WebAssembly 2.0 semantics without regressing the table-0,
call, compile, instantiation, or memory-footprint hot paths.

WebAssembly 2.0 scope for this roadmap:

- sign-extension operations;
- non-trapping float-to-int conversions;
- multi-value functions and blocks;
- reference types (`funcref` and `externref`);
- typed `select`;
- table operations and multiple tables;
- bulk memory/table operations; and
- core SIMD.

Tail calls, typed function references, GC, exceptions, memory64, and
multi-memory are not required for WebAssembly 2.0 completion.

## Current State

- [x] Sign-extension operations.
- [x] Non-trapping float-to-int conversions.
- [x] Multi-value semantics. The optimized multi-result register ABI remains a
  performance task, not a WebAssembly 2.0 semantics blocker.
- [x] Typed `select` for currently executable value types.
- [x] Bulk linear-memory operations and passive data segments.
- [x] Table operations on table 0: `table.get`, `table.set`, `table.size`,
  `table.grow`, `table.fill`, `table.copy`, `table.init`, and `elem.drop`.
  Min-only local funcref tables that execute `table.grow` or export the table
  reserve a bounded 64-entry growth window; fixed-use min-only tables retain
  their minimum-sized descriptor footprint.
- [x] Passive and declarative funcref element handling for table 0.
- [x] Core SIMD for the documented linux/amd64 baseline.
- [x] Funcref table initializer expressions, including non-null `ref.func`.
- [x] Nullable funcref parameters/results, zero-initialized locals, direct calls,
  block results, `ref.null`, and `ref.is_null`; the Release 2 execution harness
  now executes null funcref arguments/results instead of counting them as gaps.
- [x] Public local `ref.func` results use stable runtime/private-store-owned
  opaque tokens through raw `Invoke`, typed `Call`, and `invokeLocal`. Same-store
  tokens translate back only after exact validation; forged, cross-runtime, and
  cross-private-store tokens fail before native entry. Token retention keeps the
  producer's descriptor arena, code mapping, and home context alive after its
  logical `Instance.Close` until store teardown.
- [x] Canonicalize same-runtime `InstanceExport` funcrefs: imported `ref.func`
  and canonical local descriptors returned by `table.get` reuse the producer's
  opaque identity, retain the true producer, and survive producer logical close.
  Cross-runtime/private-store imports, corrupted `refSlot` metadata, and host
  imports fail closed without issuing tokens.
- [x] Execute module-local immutable/mutable `funcref` globals as 8-byte cells.
  `ref.null func` initializes zero and structural `ref.func` initializers resolve
  to the instance's canonical descriptor after code mapping; JIT
  `global.get`/`global.set`, exported invocation, `GlobalValue`, and
  `SetGlobalValue` translate non-null values only through the exact reference
  store. Raw global access cannot expose descriptor addresses, forged tokens fail
  before storage, and a stored token retains its true producer after logical
  close. Imported/shared funcref globals now use the same exact compatible-store
  owner and producer-retention model.
- [x] Decouple the canonical function-descriptor arena from table presence.
  Tables retain the existing direct descriptor path, while table-free modules
  allocate exactly `(function count + 1) * 32` arena bytes only when an executable
  body or global initializer uses `ref.func`; scalar and null-only modules allocate
  no descriptor arena. Compiled codec version 19 preserves this structural need
  for table-free `.wago` modules while all reference-global metadata remains
  rejected on marshal/load and snapshots.
- [x] Broaden public funcref tokens to explicitly owned host descriptors.
  `Runtime.NewHostFuncRef` binds one exact signature/store owner, canonicalizes
  the same owner across importing instances, retains the first callable thunk and
  home descriptor, dispatches indirect calls through the active same-store
  caller, and enforces importer/token/runtime close ordering. Raw `HostFunc`
  descriptor egress, cross-runtime owners, forged tokens, and corrupted `refSlot`
  metadata remain fail-closed.
- [x] Measure explicit host funcrefs. Pinned three-second medians are 38.83 ns/op
  for stable owned-descriptor egress and 121.0 ns/op for a same-store indirect
  host call through the public token, both 0 B/op and 0 allocs/op. Warmed
  funcref-ingress caller instantiation is 1,283 ns/op, 1,296 B/op, and 10
  allocs/op; it alone installs the exact 328-byte off-heap sync control frame.
  Warmed owned-host-funcref instantiation is 9,974 ns/op, 2,528 B/op, and 22
  allocs/op because it creates the explicit per-instance executable thunk.
  `HostFuncRef` is 112 Go bytes plus one bounded 24-byte store dispatch slot;
  `Instance`, `Compiled`, `Global`, `Table`, and `referenceStore` remain 776,
  632, 40, 64, and 88 bytes. DecodeValidate, scalar compile, scalar Invoke,
  fixed table-0 indirect, scalar instantiate, and fixed-table instantiate
  medians are 118.184 us/op, 9.525 us/op, 16.26 ns/op, 18.30 ns/op, 1,063 ns/op,
  and 1,191 ns/op with unchanged allocation counts on the ordinary paths.
- [x] Measure the token foundation: scalar, null, local egress, imported egress,
  and same-runtime round trips remain 0 B/op and 0 allocs/op. Stable medians are
  16.23, 20.59, 28.42, 43.26, and 35.39 ns/op respectively. Warmed Runtime
  instantiation remains 1,224 B/op and 7 allocs/op. Instance size is 776 bytes
  (+32 from `e54f9556`). The funcref foundation used a 48-byte
  `referenceStore`; executable externref adds 40 bytes for a keyed generation
  seed and lazy slot slice, bringing it to 88 bytes while standalone
  scalar/null-only instances keep the private store lazy.
- [x] Measure bounded min-only table growth: successful null `table.grow` has a
  stable median of 22.77 ns/op with 0 B/op and 0 allocs/op. Warmed Runtime
  instantiation medians are 1,001 ns/op for a fixed-use min-only table and
  1,010 ns/op for the 64-entry growth-capable shape, both 1,224 B/op and 7
  allocs/op; the detached `08476b11` baseline measured 998.8 and 1,006 ns/op.
  The growth reserve adds 2,048 off-heap descriptor bytes for a min=0 funcref
  table and no Go allocation; fixed-use min-only capacity is unchanged.
- [x] Enforce the WebAssembly 2.0 declared-function-reference rule for
  `ref.func` on both AST and byte-backed validators. Function exports,
  global/table initializer expressions, and legacy/expression element segments
  declare their referenced functions; a start function or function body alone
  does not. The official `ref_func.wast` validation slice passes 3 modules and
  3 invalid assertions with no failures or skips.
- [x] Admit active data and active/declarative element indexes to
  `memory.init`/`data.drop` and `table.init`/`elem.drop` while starting those
  runtime slots dropped. The seven formerly rejected valid Release 2 modules
  now pass both validators; zero-length operations succeed, nonzero source
  ranges trap, repeated drops are valid, and active initialization still takes
  effect. Descriptor arrays preserve original segment indexes but are allocated
  only through passive or instruction-addressed slots, at 16 arena bytes each.
- [x] Validate `br_table` targets against their common available branch payload
  instead of requiring the label types themselves to be identical. The bottom
  payload after `unreachable` now matches the equal-arity `f32` and `f64` labels
  in `unreached-valid.wast:49`, while reachable heterogeneous numeric payloads
  and label-arity mismatches remain rejected. Both AST and byte-backed validators
  pass the focused official site. The complete valid-module gate is now
  1,600 passed / 0 failed / 0 skipped.
- [x] Measure the branch-table validation correction against detached
  `11af9a0f`: paired pinned-CPU medians are 115.817 vs 116.178 us/op for
  DecodeValidate, 9.113 vs 9.262 us/op for scalar compile, 17.56 vs 16.06 ns/op
  for scalar Invoke, and 1,004 vs 959.5 ns/op for warmed scalar Runtime
  instantiation. Allocation counts are unchanged: DecodeValidate remains 365
  allocs/op, compile 62 allocs/op, Invoke 0 B/op and 0 allocs/op, and
  instantiation 1,224 B/op and 7 allocs/op. The small compile/validation shifts
  are within the run's observed noise and add no runtime hot-path work.
- [x] Reject multiple memories strictly because multi-memory is outside the
  WebAssembly 2.0 target and remains a documented non-goal. Validation counts
  imported plus locally defined memories and rejects any total above one before
  frontend support filtering, while one imported or one local memory remains
  valid. The five official invalid assertions at `imports.wast:483/487/491` and
  `memory.wast:10/11` now fail with a clear unsupported-feature error through
  both AST and byte-backed validation. The complete invalid/malformed gate is
  now 2,848 passed / 32 failed / 1,077 skipped; valid modules remain
  1,600 passed / 0 failed / 0 skipped.
- [x] Measure strict memory cardinality against detached `cd58b0d5` with pinned-
  CPU, three-second samples. Medians are 116.980 vs 117.161 us/op for
  DecodeValidate, 10.254 vs 10.050 us/op for scalar compile, 16.40 vs 16.86
  ns/op for scalar Invoke, and 986 vs 1,046 ns/op for warmed scalar Runtime
  instantiation. Allocation counts are unchanged: DecodeValidate is 365
  allocs/op, compile is 26,880 B/op and 62 allocs/op, Invoke is 0 B/op and 0
  allocs/op, and instantiation is 1,224 B/op and 7 allocs/op. The validator adds
  only a module-bounded memory-count check; the runtime binaries and layouts are
  unchanged, and the noisy runtime shifts are retained as watchpoints rather
  than attributed to this frontend-only change.
- [x] Restrict implicit/untyped `select` to numeric and vector operands while
  preserving typed `funcref`/`externref` select and stack-polymorphic bottom.
  Both AST and byte-backed validators now reject the official
  `select.wast:340` implicit externref case. Local tests also prove valid
  implicit numeric/vector forms, valid typed references, and that bottom cannot
  hide a known reference operand. The invalid/malformed gate is now 2,849
  passed / 31 failed / 1,077 skipped; valid modules remain 1,600 / 0 / 0.
- [x] Measure the implicit-select correction against detached `49c4bd6a` with
  pinned-CPU, three-second samples. Medians are 117.418 vs 115.697 us/op for
  DecodeValidate, 10.867 vs 9.651 us/op for scalar compile, 16.55 vs 16.59
  ns/op for scalar Invoke, and 1,007 vs 956.8 ns/op for warmed scalar Runtime
  instantiation. Allocation counts are unchanged: DecodeValidate remains 365
  allocs/op at 51,353-51,354 B/op in this run, compile remains 26,880 B/op and
  62 allocs/op, Invoke remains 0 B/op and 0 allocs/op, and instantiation remains
  1,224 B/op and 7 allocs/op. The change is validator-only; runtime code and
  layouts are unchanged, and the timing spread is retained as a watchpoint.
- [x] Reject malformed data-count binaries during decode. The decoder now checks
  the declared count against the final data-section length and records
  `memory.init`/`data.drop` while it already walks byte-backed function bodies,
  rejecting a missing data-count section without materializing instructions or
  rescanning bodies. The official `binary.wast:1185/1195/1205/1227` and
  `custom.wast:123` sites pass through both public decode APIs. The invalid/
  malformed gate is now 2,854 passed / 26 failed / 1,077 skipped; the remaining
  failures are 20 `binary.wast` and six `binary-leb128.wast` cases.
- [x] Measure the data-count decode correction against detached `47efb9b3` with
  reverse-order pinned-CPU, three-second samples. Medians are 123.528 vs
  121.220 us/op for DecodeValidate and 10.383 vs 10.199 us/op for scalar
  compile, with allocations unchanged at 51,354 B/op and 365 allocs/op plus
  26,880 B/op and 62 allocs/op respectively. Scalar Invoke measured 16.68 vs
  18.12 ns/op at 0 B/op and 0 allocs/op, while warmed scalar Runtime
  instantiation measured 973.7 vs 1,012 ns/op at 1,224 B/op and 7 allocs/op.
  Runtime code and layouts are unchanged; the runtime timing shifts and one
  outlier in each direction remain noise watchpoints rather than attributed
  regressions.
- [x] Reject non-literal reserved immediates for `memory.size` and
  `memory.grow`. Both AST and byte-backed decode paths now require exactly one
  `0x00` byte, rejecting nonzero indexes and two- through five-byte LEB zero
  encodings before validation while preserving truncated-immediate offsets and
  code-section spans. The ten official `binary.wast` sites at lines 857, 877,
  897, 916, 935, 955, 974, 993, 1011, and 1029 now pass through both public
  decode APIs. The invalid/malformed gate is 2,864 passed / 16 failed / 1,077
  skipped; remaining
  failures are ten `binary.wast` and six `binary-leb128.wast` cases.
- [x] Measure the reserved-zero correction against detached `fa5dce7f` with
  paired pinned-CPU, three-second samples. Medians are 117.033 vs 115.814 us/op
  for DecodeValidate and 9.929 vs 9.878 us/op for scalar compile. Allocation
  counts are unchanged at 51,353-51,354 B/op and 365 allocs/op plus 26,880 B/op
  and 62 allocs/op respectively. Scalar Invoke measured 16.69 vs 17.38 ns/op at
  0 B/op and 0 allocs/op, while warmed scalar Runtime instantiation measured
  986.3 vs 970.0 ns/op at 1,224 B/op and 7 allocs/op. Runtime code and layouts
  are unchanged; the small runtime timing shifts and isolated benchmark
  outliers remain noise watchpoints rather than attributed regressions.
- [x] Decode memarg offsets at the effective sole memory's address width.
  Memory32 and the conservative no-/multi-memory paths use u32 LEB decoding,
  while one local or imported memory64 uses u64. Valid non-minimal encodings
  within the selected width remain accepted. The twelve official malformed
  sites at `binary.wast:483/540/620/639/733/752` and
  `binary-leb128.wast:405/462/731/750/844/863` now fail during both public
  decode paths; direct raw-body validation uses the same policy. The invalid/
  malformed gate is now 2,876 passed / 4 failed / 1,077 skipped. Remaining
  failures are `binary.wast:48/1082/1098/1563`.
- [x] Refine the memarg-width context so it is passed through the existing body
  walks rather than stored in `reader`. Against detached `95763a49`, paired
  pinned-CPU three-second medians are 114.709 vs 115.308 us/op for
  DecodeValidate, 9.720 vs 9.360 us/op for scalar compile, 17.97 vs 16.48 ns/op
  for scalar Invoke, and 1,037 vs 948.8 ns/op for warmed scalar Runtime
  instantiation. Allocation counts remain 51,354 B/op and 365 allocs/op for
  DecodeValidate, 26,880 B/op and 62 allocs/op for compile, 0 B/op and 0
  allocs/op for Invoke, and 1,224 B/op and 7 allocs/op for instantiation. The
  first implementation's transient +64 DecodeValidate and +32 compile B/op
  reader-size increase was removed before commit; runtime code/layout remains
  unchanged, and timing spread is retained as noise watchpoints.
- [x] Reject code bodies whose individually valid u32 local runs aggregate above
  2^32-1 during decode, without expanding locals. The exact declared-local
  boundary and zero-count runs remain valid binary encodings; function
  parameters remain a separate validation index-space concern. Both public
  decode APIs now reject `binary.wast:1082/1098` with code-section diagnostics,
  and the direct code-section plus AST test paths use the same bounded sum. The
  invalid/malformed gate is now 2,878 passed / 2 failed / 1,077 skipped;
  remaining failures are `binary.wast:48/1563`.
- [x] Measure aggregate local-count decoding against baseline `16d2aabc` with
  pinned-CPU three-second samples. Medians are 142.062 vs 136.097 us/op for
  DecodeValidate, 13.776 vs 12.941 us/op for scalar compile, 21.35 vs 17.65
  ns/op for scalar Invoke, and 1,319 vs 1,001 ns/op for warmed scalar Runtime
  instantiation. Allocation counts remain unchanged at 51,354 B/op and 365
  allocs/op for DecodeValidate, 26,880 B/op and 62 allocs/op for compile, 0 B/op
  and 0 allocs/op for Invoke, and 1,224 B/op and 7 allocs/op for instantiation.
  The broad timing movement, including runtime-only paths untouched by this
  decoder change, is retained as scheduler-noise watchpoints rather than claimed
  improvement; runtime code and layouts are unchanged.
- [x] Reject shared memory encodings without an explicit maximum during binary
  decode. The common memory-type decoder now rejects memory32 flag 2 and
  memory64 flag 6 after reading the minimum, preserving malformed-LEB priority,
  while flags 3/7 and every unshared limits form remain accepted for imports and
  definitions. Both public decoders reject `binary.wast:1563` with memory-section
  diagnostics; AST/public local tests cover imported/local memory32/memory64,
  and programmatically constructed modules retain validation-time
  `ErrInvalidSharedMemory`. The invalid/malformed gate is now 2,879 passed / 1
  failed / 1,077 skipped; the sole remaining failure is `binary.wast:48`.
- [x] Measure the shared-memory decode correction against red baseline
  `8b528fb6` with pinned-CPU three-second samples. Medians are 115.139 vs
  121.116 us/op for DecodeValidate, 9.665 vs 9.696 us/op for scalar compile,
  16.47 vs 16.61 ns/op for scalar Invoke, and 988.6 vs 1,053 ns/op for warmed
  scalar Runtime instantiation. Allocation counts are unchanged: DecodeValidate
  remains 51,354 B/op and 365 allocs/op, compile remains 26,880 B/op and 62
  allocs/op, Invoke remains 0 B/op and 0 allocs/op, and instantiation remains
  1,224 B/op and 7 allocs/op. The decoder adds only bounded flag checks; runtime
  code and layouts are unchanged, and broad timing movement including the
  untouched instantiation path is retained as scheduler-noise watchpoints.
- [x] Reject reserved core section id 14 during binary decode. The former
  stringrefs-proposal section path had no executable frontend consumer, so the
  default `DecodeModule` and `DecodeModuleByteBacked` APIs now follow the
  WebAssembly 2.0 core namespace and reject ids above 13 with preserved section
  diagnostics. The AST oracle is aligned, while programmatically constructed
  `Module.StringRefs` values retain their independent validator coverage. The
  official `binary.wast:48` malformed assertion now passes, bringing the full
  validation gate to 1,600 passed / 0 failed / 0 skipped modules and 2,880
  passed / 0 failed / 1,077 skipped assertions.
- [x] Measure the section-id correction against red baseline `9606f093` with
  pinned-CPU three-second samples. Medians are 115.242 vs 117.130 us/op for
  DecodeValidate, 11.366 vs 9.658 us/op for scalar compile, 17.51 vs 17.77
  ns/op for scalar Invoke, and 1,099 vs 1,060 ns/op for warmed scalar Runtime
  instantiation. Allocation counts remain 365 allocs/op at 51,353-51,354 B/op
  for DecodeValidate, 26,880 B/op and 62 allocs/op for compile, 0 B/op and 0
  allocs/op for Invoke, and 1,224 B/op and 7 allocs/op for instantiation. The
  decoder removes one unsupported section entry and no runtime code or layout;
  timing spread is retained as scheduler-noise watchpoints, not an attributed
  performance change.
- [x] Measure local funcref globals against red baseline `713bb939` with pinned-
  CPU three-second samples. DecodeValidate medians are 116.247 vs 117.876 us/op,
  scalar compile 9.466 vs 9.764 us/op, scalar Invoke 16.44 vs 16.48 ns/op, and
  warmed scalar Runtime instantiation 967.7 vs 1,005 ns/op. Allocations remain
  unchanged at 51,354 B/op and 365 allocs/op, 26,880 B/op and 62 allocs/op,
  0 B/op and 0 allocs/op, and 1,224 B/op and 7 allocs/op respectively. The new
  null funcref global set/get Invoke path measures 21.49 ns/op with 0 B/op and
  0 allocs/op; its two-global warmed instantiation measures 1,044 ns/op,
  1,320 B/op, and 9 allocs/op. `Instance` and basedata layouts are unchanged;
  broad timing movement in untouched scalar paths remains scheduler-noise
  watchpoints rather than an attributed regression.
- [x] Measure structural `ref.func` globals against red baseline `d543e598`.
  Pinned-CPU three-second medians were 148.384 vs 120.815 us/op for
  DecodeValidate, 16.114 vs 9.737 us/op for scalar compile, 19.06 vs 16.20 ns/op
  for scalar Invoke, and 1,201 vs 964.7 ns/op for warmed scalar Runtime
  instantiation, with allocation counts unchanged. The new no-table global egress
  path measures 28.74 ns/op at 0 B/op and 0 allocs/op; warmed no-table `ref.func`
  global instantiation measures 1,082 ns/op, 1,280 B/op, and 9 allocs/op with an
  exact 128-byte off-heap descriptor arena for three functions. Null-only global
  instantiation remains 1,320 B/op and 9 allocs/op with no descriptor arena.
  Table-grow and fixed-table reverse-order watchpoints moved 23.17 to 23.73 ns/op
  and 991.3 to 1,026 ns/op respectively with allocations unchanged; those small
  shifts affect untouched steady-state code/layout and remain noise watchpoints.
- [x] Execute and measure the first multiple-local-table slice. The complete
  official `table_copy.wast` and `table_init.wast` files now pass 52 modules /
  1,675 assertions and 35 modules / 677 assertions respectively, unlocking 69
  modules and 1,339 assertions in the full execution gate. Table 0 keeps its
  direct basedata load; nonzero indexes use an arena-backed pointer directory.
  Reusing the former unused descriptor-count basedata slot keeps basedata at 128
  bytes, and reconstructing active destinations from that directory keeps warmed
  one- and two-table Runtime instantiation at 1,224 B/op and 7 allocs/op. A
  capacity-one second funcref table adds exactly 56 off-heap arena bytes: 40 for
  its descriptor and 16 for the two-entry directory. Pinned medians against red
  baseline `af93836f` were 120.959 vs 126.061 us/op for DecodeValidate, 12.244 vs
  10.200 us/op for scalar compile, 18.43 vs 16.45 ns/op for scalar Invoke, 20.32
  vs 19.12 ns/op for table-0 `call_indirect`, 23.36 vs 24.94 ns/op for null
  table-grow, 1,093 vs 959.5 ns/op for scalar instantiation, and 1,059 vs 1,040
  ns/op for fixed-table instantiation, with allocation counts unchanged. Timing
  moved in both directions across repeated/reverse-order runs, including untouched
  paths, so the spread remains a scheduler/frequency watchpoint rather than an
  attributed regression. New two-table table-0/table-1 indirect medians are 18.62
  and 18.63 ns/op at 0 B/op/0 allocs; warmed two-table instantiation is 1,092
  ns/op, 1,224 B/op, and 7 allocs/op.
- [ ] Full first-class `funcref` support.
- [x] Execute the first externref store/host-ABI slice. Signatures,
  params/results, zero-initialized locals, blocks/branches, typed `select`,
  `ref.null extern`, and `ref.is_null` carry generation-checked per-store handles.
  `Runtime`, `Instance`, and reflection-free host callbacks can register/resolve
  embedder objects; forged, stale, cross-runtime, cross-private-store, and forged
  host-result tokens fail before native entry/re-entry.
- [x] Measure the externref foundation: null/non-null Invoke and synchronous host
  round trips have pinned medians of 21.52, 33.54, and 132.4 ns/op, all 0 B/op
  and 0 allocs/op; scalar synchronous host calls measure 108.3 ns/op. Warmed
  externref-control versus scalar Runtime instantiation is 1,018 vs 1,013 ns/op,
  both 1,224 B/op and 7 allocs/op. Each object slot is 24 bytes plus amortized
  slice backing; `Instance` remains 776 bytes and `referenceStore` is 88 bytes.
  DecodeValidate, scalar compile, scalar Invoke, fixed table-0 indirect, and
  scalar instantiation medians are 120.004 us/op, 10.872 us/op, 16.61 ns/op,
  19.29 ns/op, and 1,013 ns/op versus documented `16a78af5` medians of 128.205
  us/op, 12.826 us/op, 18.49 ns/op, 20.65 ns/op, and 1,231 ns/op. Allocations are
  unchanged; broad movement remains scheduler/frequency noise rather than an
  attributed gain.
- [x] Execute module-local immutable/mutable `externref` globals as 8-byte handle
  cells. `ref.null extern` initializes zero; JIT `global.get`/`global.set`, exported
  invocation, `GlobalValue`, and `SetGlobalValue` preserve same-store non-null
  identity while rejecting forged and cross-store tokens before storage. Raw
  global access stays fail-closed, runtime/private-store teardown releases roots,
  and `.wago` plus snapshots reject reference-global metadata. Imported/shared
  externref globals now use an exact store-bound owner model.
- [x] Measure local externref globals: pinned medians are 24.28 ns/op for null and
  33.45 ns/op for non-null set/get Invoke round trips, both 0 B/op and 0 allocs/op.
  Warmed two-global Runtime instantiation is 1,104 ns/op, 1,320 B/op, and 9
  allocs/op. `Instance` remains 776 bytes, `referenceStore` remains 88 bytes, and
  each global cell is exactly 8 off-heap bytes. DecodeValidate, scalar compile,
  scalar Invoke, fixed table-0 indirect, and scalar instantiation medians are
  120.486 us/op, 10.624 us/op, 17.68 ns/op, 18.85 ns/op, and 1,031 ns/op with
  allocation counts unchanged; timing movement versus the prior documented run
  remains scheduler/frequency noise rather than an attributed regression.
- [x] Execute module-local externref tables with exact typed descriptors and
  8-byte entries. `table.get`, `table.set`, `table.size`, `table.grow`, and
  `table.fill` work at table 0 and nonzero indexes in heterogeneous modules;
  native code only copies handles, externref-only tables allocate no funcref
  descriptor arena, and min-only growth has a bounded 1,024-entry reserve.
  Externref elements/copy/init and codec-v19 persistence remain rejected.
- [x] Measure local externref tables: null/non-null set/get Invoke medians are
  21.52/33.52 ns/op at 0 B/op and 0 allocs/op; fixed capacity-one warmed
  instantiation is 1,013 ns/op, 1,224 B/op, and 7 allocs/op. A capacity-four
  descriptor is exactly 40 off-heap bytes; the min-only reserve is 8,192 entry
  bytes. `Compiled`, `Instance`, `Table`, and `tableDef` remain 632, 776, 64,
  and 40 bytes. DecodeValidate, scalar compile, scalar Invoke, funcref table-0
  indirect, and scalar instantiate medians are 116.676 us/op, 10.001 us/op,
  16.25 ns/op, 18.65 ns/op, and 1,015 ns/op with unchanged allocations.
- [x] Share imported/runtime-owned externref tables and local exports/re-exports
  only through an exact compatible reference store. `Runtime.NewExternRefTable`
  creates typed 8-byte storage; imported aliases preserve get/set/size/grow/fill
  state, exact limits/type/store checks run before instantiation, host table close
  rejects live importers, local owners are retained until consumers detach, and
  Runtime.Close keeps roots until the last instance and store-owned table close.
  The same-size `Table.owner` pointer preserves the 64-byte public handle.
- [x] Measure shared externref tables: warmed imported externref instantiation is
  1,379 ns/op, 1,840 B/op, and 9 allocs/op versus 1,416 ns/op with the same
  allocations for the funcref imported-table control. Cached local externref
  export lookup is 25.19 ns/op at 0 B/op and 0 allocs/op. DecodeValidate, scalar
  compile, scalar Invoke, funcref table-0 indirect, scalar instantiate, and local
  externref-table instantiate medians are 118.701 us/op, 11.409 us/op, 16.28
  ns/op, 18.51 ns/op, 984.7 ns/op, and 1,021 ns/op with unchanged allocation
  counts. `Compiled`, `Instance`, `Table`, `tableDef`, and `referenceStore` remain
  632, 776, 64, 40, and 88 bytes.
- [x] Share imported/local-exported funcref and externref globals through an exact
  typed owner. `Runtime.NewExternRefGlobal` creates a store-bound 8-byte host
  cell; aliases attach one lifetime root, local exports/re-exports preserve exact
  identity, imported immutable `global.get` initializers copy valid references,
  host close rejects live importers, and producer/store resources release once
  consumers detach. Cross-runtime/private-store, forged token/descriptor, type,
  and mutability mismatches reject before native-visible storage.
- [x] Measure shared reference globals: warmed imported externref-global
  instantiation is 2,010 ns/op, 1,960 B/op, and 14 allocs/op versus 1,938 ns/op,
  1,968 B/op, and 14 allocs/op for the imported numeric-global control. Cached
  host externref `GetValue` is 8.464 ns/op at 0 B/op/0 allocs. Numeric, null
  funcref, null externref, and non-null externref global round trips are 16.90,
  23.52, 21.30, and 34.08 ns/op, all allocation-free. DecodeValidate, scalar
  compile, scalar Invoke, fixed table-0 indirect, scalar instantiate, local
  externref-table instantiate, imported funcref-table instantiate, and imported
  externref-table instantiate medians are 298.261 us/op, 27.844 us/op, 22.32
  ns/op, 22.09 ns/op, 1,202 ns/op, 1,025 ns/op, 1,381 ns/op, and 1,406 ns/op;
  allocations remain unchanged. `Global`, `Compiled`, `Instance`, and
  `referenceStore` remain 40, 632, 776, and 88 bytes. Broad timing movement on
  untouched paths is retained as scheduler/frequency noise rather than an
  attributed regression.
- [x] Execute typed externref elements and bulk table operations. `ElemInit` now
  carries exact reference type, explicit active/passive/declarative mode, and
  structural `RefInit` values so null never aliases a function index. Active null
  segments initialize local, nonzero, imported, and shared externref tables;
  passive `table.init`, `elem.drop`, and 8-byte `table.copy` preserve handle
  identity, overlap semantics, zero-length boundaries, all-or-nothing per-segment
  bounds, and declaration-order failed-instantiation store effects. Externref
  failures add no producer root, while existing funcref retention is unchanged.
- [x] Admit the final five compile-rejected Release 2 modules. No-table
  `elem.drop` gets bounded per-instance descriptor state, and inert unexported
  tables whose declared spare capacity cannot fit the arena are represented at
  their minimum because no grow/export surface can observe that capacity.
  `bulk.wast` is fully green at 13 modules / 104 assertions, `elem.wast` at
  29 / 37, and `table.wast` at 9 modules.
- [x] Measure typed elements: pinned three-second medians are 91.99/26.25 ns/op
  for four-entry funcref table-0 copy/init and 69.78/19.58 ns/op for four-entry
  externref copy/init, all 0 B/op and 0 allocs/op. Warmed passive-externref-element
  instantiation is 1,168 ns/op, 1,216 B/op, and 6 allocs/op; four passive nulls
  add exactly one 16-byte descriptor plus 32 payload bytes. DecodeValidate,
  scalar compile, scalar Invoke, fixed table-0 indirect, scalar instantiate, and
  fixed externref-table instantiate medians are 117.768 us/op, 11.444 us/op,
  16.92 ns/op, 18.46 ns/op, 1,061 ns/op, and 1,090 ns/op with allocation counts
  unchanged. `Global`, `Table`, `Compiled`, `Instance`, `tableDef`, and
  `referenceStore` remain 40, 64, 632, 776, 40, and 88 bytes.
- [x] Full executable `externref` support across signatures, locals, control,
  host calls, globals, tables, elements, and bulk table operations.
- [x] Multiple funcref and externref tables execute across local and imported
  definitions. Imported descriptors occupy table indexes 0..N-1, local
  descriptors follow in the bounded directory, and active elements, every
  indexed table operation, cross-table copy/init, nonzero-table `call_indirect`,
  exact named exports/re-exports, duplicate imported aliases, per-import limits,
  and failed-instance ownership are covered with exact type/store checks.
- [x] The Release 2 `table_grow.wast` min-only funcref growth assertions now
  pass: growth from 10 to 20 returns the old size and leaves every new slot null.
- [x] Release 2 instantiation store effects persist in declaration order across
  later active-segment bounds failures and start traps. Imported tables retain a
  failed local-funcref producer only while one of its descriptor identities
  remains in the finite table; overwrite and table-close tests prove stale roots
  are released and retention stays capacity-bounded.
- [x] Imported function re-exports report their declared signature, forward raw
  `Invoke` and typed `Call` through the original `InstanceExport`, preserve traps
  and producer state, and can be linked again without creating a relay owner.
  Host-import re-export handles remain fail-closed because they lack an explicit
  instance/code lifetime owner. The producer must remain open until its importing
  and re-exporting instances close.
- [x] Cache imported-function export resolution in the existing fixed Invoke
  cache: forwarding improved from a 194.1 ns/op median with 80 B/op and 3
  allocs/op to 29.96 ns/op with 0 B/op and 0 allocs/op, without increasing the
  776-byte `Instance` footprint.
- [x] Measure the shared-table lifetime fix: against detached `4d613c9b` medians,
  scalar compile is 8.660 vs 8.549 us/op, scalar Invoke is 16.36 vs 16.29 ns/op,
  warmed scalar Runtime instantiate is 963.5 vs 942.7 ns/op, fixed min-only table
  instantiate is 1,024 vs 990.9 ns/op, and imported-table instantiate is 1,304
  vs 1,297 ns/op. Allocation counts are unchanged: Invoke remains 0 B/op and 0
  allocs/op, scalar/fixed instantiation remains 1,224 B/op and 7 allocs/op, and
  imported-table instantiation remains 1,840 B/op and 9 allocs/op. Instance size
  remains 776 bytes; the small timing deltas are within observed run noise.
- [x] Resolve local table exports by their exact declared names and indexes.
  Nonzero descriptors are reconstructed only from the bounded runtime-owned
  directory, and each exported table gets one lazy 64-byte ownership handle.
  Repeated table-0/table-1 lookups measure 11.01/12.92 ns/op at 0 B/op and 0
  allocs/op. Export metadata is not representable in codec version 19, so marshal
  rejects it; loaded v19 table modules expose an exactly empty table-export set
  instead of reviving the former advisory table-0 fallback. Min-only growth
  reserve is now per exported table rather than applied to every local table.
  Against green baseline `c856b282`, pinned medians are 142.809 vs 115.873 us/op
  for DecodeValidate, 16.196 vs 9.625 us/op for scalar compile, 21.24 vs 16.73
  ns/op for scalar Invoke, 21.85 vs 19.07 ns/op for table-0 `call_indirect`,
  1,257 vs 973.9 ns/op for scalar instantiation, 1,163 vs 1,019 ns/op for fixed-
  table instantiation, and 1,095 vs 1,091 ns/op for two-table instantiation.
  Allocations remain unchanged at 51,354 B/op and 365 allocs/op, 26,880 B/op and
  62 allocs/op, 0/0 for both Invoke paths, and 1,224 B/op plus 7 allocs/op for
  every instantiation shape. Two-table-with-exports instantiation measures 1,120
  ns/op with the same 1,224 B/op and 7 allocs/op; broad timing movement remains a
  scheduler/frequency watchpoint rather than an attributed gain.
- [x] Execute and measure one imported funcref table 0 followed by local tables.
  The imported descriptor remains in the direct basedata slot, local table 1..N
  use the bounded directory, and a capacity-one local table adds exactly 56
  importer-arena bytes (40 descriptor plus a 16-byte two-table directory).
  Imported and local active elements, cross-table copy, indexed `call_indirect`,
  exact exports/re-exports, limit/policy checks, shared-memory rejection, failed-
  instantiation retention, and consumer-before-owner close ordering are covered.
  That bounded slice still rejected multiple imported tables clearly; codec
  version 19 continues to reject every unencoded multi-table shape. Pinned
  medians are 20.37 ns/op for imported table-0 indirect dispatch, 18.47 ns/op for
  local table-1 dispatch, and 1,332
  ns/op for warmed imported+local shape instantiation; dispatch is 0 B/op and 0
  allocs/op, while instantiation is 1,840 B/op and 9 allocs/op, matching imported-
  only allocation counts. Against detached `02e75aeb`, DecodeValidate, scalar
  compile, scalar Invoke, table-0 indirect, scalar/fixed/two-local/imported
  instantiation medians are 120.588 vs 121.760 us/op, 9.568 vs 10.609 us/op,
  17.05 vs 17.50 ns/op, 18.33 vs 19.88 ns/op, 955.5 vs 1,140 ns/op, 1,082 vs
  1,089 ns/op, 1,150 vs 1,147 ns/op, and 1,382 vs 1,541 ns/op. Allocations are
  unchanged; broad timing movement remains scheduler/frequency noise rather than
  an attributed gain.
- [x] Execute and measure multiple imported funcref tables followed by local
  tables. Indexed import metadata reuses the already-required nonzero-table
  entries, preserving the 632-byte `Compiled`, 776-byte `Instance`, scalar
  compile at 26,880 B/op and 62 allocs/op, and codec-v19's sole-import round
  trip. Imported table 0 remains direct; imported table 1 and later indexes use
  the bounded directory. Exact imports/exports, distinct and duplicate handles,
  active elements, table operations, `call_indirect`, independent limits/policy,
  shared-memory rejection, alias-safe failed-instance retention, and close
  ordering are covered. A second imported table adds exactly a 16-byte two-entry
  directory; a capacity-one local table after two imports adds 48 bytes (40-byte
  descriptor plus 8-byte directory growth). Pinned medians are 21.46, 22.23, and
  20.05 ns/op for imported table 0, imported table 1, and local table 2 dispatch,
  all 0 B/op/0 allocs; warmed two-import-plus-local instantiation is 1,662 ns/op,
  1,840 B/op, and 9 allocs/op. Against detached `fc3bea91`, medians are 127.073
  vs 128.205 us/op for DecodeValidate, 11.983 vs 12.826 us/op for scalar compile,
  18.15 vs 18.49 ns/op for scalar Invoke, 20.61 vs 20.65 ns/op for fixed table-0
  indirect, and 1,208/1,237/1,495/1,579 vs 1,231/1,276/1,572/1,662 ns/op for
  scalar/fixed/imported/imported+local instantiation. Allocations are unchanged;
  timing movement is retained as scheduler/frequency noise rather than attributed
  gains.
- [ ] WebAssembly 2.0 conformance gate with no feature-related skips. With WABT
  1.0.36 available, the July 10, 2026 execution run reports 1,564 passed / 36
  skipped modules and 48,225 passed / 0 failed / 23 skipped assertions. Gap
  reasons are compile-rejected=0, instantiate-rejected=36,
  module-unavailable=23, absent-export=0, reference-argument=0,
  reference-result=0, and reference-global=0. `bulk.wast`, `elem.wast`, and
  `table.wast` are fully executable at 13/104, 29/37, and 9/0 modules/assertions.
  `table_get.wast` is now fully executable at 1 module / 10 assertions, including
  both non-null funcref expectations. `exports.wast` remains fully
  green at 56/9; `imports.wast` remains 41 passed / 13 skipped modules and 16
  passed / 18 skipped assertions. `table_copy.wast`, `table_init.wast`, and
  `ref_func.wast` remain fully green at 52/1,675, 35/677, and 3/10. Relative to
  1,559/41 modules and 48,221/27 assertions, typed elements unlock five modules
  and two assertions and eliminate every compile-rejected gap; explicit/non-null
  funcref closeout unlocks the final two reference-result assertions.

Remaining closeout work is semantic: host-created funcref globals, codec
evolution for persistent typed reference/table/element metadata, the 36 reasoned
instantiation gaps and 23 dependent unavailable assertions, and final zero-skip
feature reporting/docs.

The 36 instantiate-rejected modules are now pinned by exact source site and
bounded reason. Thirteen import missing the standard `spectest.print*` host
functions (`binary-leb128.wast:75/87/99`, `func_ptrs.wast:1`,
`imports.wast:26/97/107`, `linking.wast:22`, `names.wast:1095`,
`start.wast:80/86/92`, and `tokens.wast:35`). Twenty-two import the missing
file-scoped `spectest.memory` (`data.wast:39/52/67/78/101/116/135/145/150/155/
161/167/172` and `imports.wast:459/471/498/499/500/501/502/503/565`). The final
site, `imports.wast:588`, requires imported-memory re-export resolution. This
inventory is distinct from host funcref ownership and must not be collapsed into
a generic instantiation reason.

## Implementation Order

### P0 — Pin and Wire the Official WebAssembly 2.0 Suite

- [x] Add a separately pinned official WebAssembly 2.0 testsuite revision rather
  than replacing the pre-reference-types WebAssembly 1.0 conformance baseline.
- [x] Update the validation and execution harnesses for the 2.0 core-suite
  layout.
- [x] Install or provision `wast2json` in CI for the 2.0 job.
- [ ] Make valid modules rejected as unsupported fail the 2.0 job.
- [ ] Make invalid modules accepted by the decoder/validator fail the job.
- [x] Add reference-valued assertion argument and result support for every shape
  in the pinned Release 2 corpus.
  - [x] Encode, invoke, and assert null `funcref` arguments/results as token zero.
  - [x] Add null/non-null externref fixture identities through per-instance
    stores; `ref.extern N` arguments/results now execute.
  - [x] Match WABT's non-null funcref expectation as any nonzero opaque token;
    the two `table_get.wast` sites now execute without descriptor comparison.
- [x] Stop treating reference arguments, reference results, or reference globals
  as out-of-scope skips in `src/wago/spectest_exec_test.go`. Unknown future value
  shapes are harness failures rather than feature skips.
- [x] Record per-file module/assertion pass, fail, and skip counts.
- [x] Classify execution skips with bounded compile, instantiate, blocked-module,
  absent-export, reference-argument, reference-result, and reference-global
  reason counts, and expose those counts in the CI card.

Completion criterion: the harness reports every remaining WebAssembly 2.0 gap
explicitly instead of hiding it behind unsupported-module or reference-value
skips.

### P1 — WebAssembly 2.0 Validation Correctness

- [x] Enforce the declared-function-reference rule for `ref.func` on both
  validator paths, with focused official Release 2 fixture coverage.
- [x] Validate bulk data/table segment operands independently of active,
  passive, or declarative mode, while preserving index and reference-type checks.
- [x] Validate each `br_table` target against the common branch payload, including
  stack-polymorphic bottom, while preserving arity and reachable mismatch checks.
- [x] Validate `funcref` and `externref` in function params/results, locals,
  globals, block signatures, typed `select`, tables, and element segments.
- [x] Validate multiple-table indexes for `call_indirect`, active elements, and
  all table instructions.
- [x] Validate element-segment and table reference-type compatibility.
- [x] Validate `ref.null`, `ref.func`, and `ref.is_null` in every WebAssembly 2.0
  context.
- [x] Drive validation fixes from the official 2.0 invalid and malformed
  corpus. Multiple memories, implicit reference `select`, data-count
  consistency, reserved-zero memory immediates, memory32 memarg width,
  aggregate declared-local counts, shared-memory maxima, and reserved section
  ids are all rejected through the applicable decoder/validator paths. The
  complete gate is now 1,600 passed / 0 failed / 0 skipped modules and 2,880
  passed / 0 failed / 1,077 skipped assertions.

Keep decode and validation strict. Do not turn malformed structured sections or
invalid proposal encodings into best-effort parsing.

### P2 — Public Reference Types and Slot ABI

- [x] Add public `ValFuncRef` and `ValExternRef` value types.
- [x] Add opaque `FuncRef` and `ExternRef` public representations.
- [x] Add typed constructors/accessors for reference-valued `Value`s.
- [x] Define the low-level `uint64` representation as an opaque reference token,
  never a documented Go or native pointer.
- [ ] Update value-type encoding, `.wago` type metadata, signatures, reflection-
  free host calls, and typed `Call` validation.
  - [x] Preserve reference value types in public signatures and typed `Call`
    one-slot validation/result decoding.
  - [x] Add structural signature type codes and codec-version-19 table-free
    funcref-descriptor metadata while rejecting reference globals and live
    reference tokens on marshal/load.
  - [x] Enable externref values at reflection-free host-call boundaries.
  - [x] Enable opaque host funcref params/results. Host callbacks receive public
    tokens rather than descriptors; returned tokens resolve only through the exact
    store before native re-entry.
- [x] Define null construction and testing in the public API.

Suggested API direction:

```go
type FuncRef struct { /* opaque */ }
type ExternRef struct { /* opaque */ }

func ValueFuncRef(FuncRef) Value
func ValueExternRef(ExternRef) Value
func (Value) FuncRef() FuncRef
func (Value) ExternRef() ExternRef
```

### P3 — First-Class Funcref Execution

- [x] Execute the nullable funcref foundation through parameters/results,
  zero-initialized locals, direct calls, block results, `ref.null`, and
  `ref.is_null`, with exact typed `Call` values and feature gating.
- [x] Permit `funcref` in function parameters, results, locals, and block
  parameters/results in the frontend support pass.
- [x] Carry funcref through direct calls, recursion, multi-value returns,
  branches, typed `select`, and spills as a 64-bit JIT value.
- [x] Carry funcref through cross-instance calls and synchronous host imports.
- [ ] Return and accept runtime-owned non-null funcref tokens through `Invoke`
  and typed `Call` without exposing descriptor addresses.
  - [x] Issue stable opaque tokens for local `ref.func` descriptors, validate
    same-store ingress, retain producer resources, and give standalone
    `Instantiate` a lazy private-store policy.
  - [x] Translate same-runtime cross-instance imported funcrefs through exact
    `InstanceExport`, descriptor-range, and `refSlot` canonicalization checks.
  - [x] Translate explicitly owned host funcrefs across public/same-runtime
    boundaries with exact signature/store/descriptor validation and retained
    callable context. Raw unowned host descriptors remain fail-closed.
- [x] Zero-initialize funcref locals.
- [ ] Finish the remaining codec/snapshot/pool audit for reference ownership;
  call marshalling and result handling now preserve exact funcref/externref types.
- [x] Preserve descriptor identity for `ref.is_null` and supported identity
  operations.

The backend already maps reference values to a 64-bit machine type in several
places. Reuse that representation rather than adding a parallel register class.

### P4 — Funcref Globals and Lifetime

- [x] Add 8-byte module-local funcref global cells with immutable/mutable JIT
  access and exported typed host access.
- [x] Support imported and cross-instance funcref global objects with exact
  compatible-store ownership, alias deduplication, and producer retention.
- [x] Support `ref.null` global initializers.
- [x] Support valid non-null `ref.func` global initializers.
- [x] Support imported immutable `global.get` initializers where the 2.0 rules
  permit them, including funcref and externref identity.
- [ ] Add host constructors and accessors for funcref globals.
- [ ] Keep funcref globals out of numeric-only optimizations unless explicitly
  proven safe.
- [x] Define local-cell funcref ownership so a token stored from another
  same-runtime instance retains the producer after logical close.
- [x] Ensure store-owned funcref tokens retained by local globals also retain the
  required code mapping and home instance context.
- [x] Extend the same proof to imported/shared global objects.
- [ ] Add host-created funcref globals by reusing the now-explicit HostFuncRef/
  token owner proof; do not add a second descriptor registry or serialize owners.

Do not expose the current pointer into an instance descriptor arena as the public
funcref identity.

### P5 — Externref Store and Host ABI

Implement externrefs as handles, not pointers in mmap-backed Wasm storage.

- [x] Reserve handle zero for null.
- [x] Add generation checking to detect stale handles.
- [x] Add a runtime/store-owned Go table mapping handles to embedder objects.
- [x] Keep native code limited to copying/testing the 64-bit handle.
- [x] Translate handles only at public API and host-call boundaries.
- [x] Give standalone `Instantiate` a lazy private store on first non-null use.
- [x] Share one store among instances created by the same `Runtime`.
- [x] Reject externrefs passed between incompatible stores.
- [x] Retain registered externrefs for the store lifetime; no early reclamation.
- [x] Release Go roots on `Runtime.Close` after the last attached instance closes.
- [x] Cover host functions that accept, return, and round-trip externrefs.

Avoid a process-global unbounded cache. A per-runtime/store table makes the
lifetime and memory bound explicit.

### P6 — Externref Globals and Tables

- [x] Add module-local externref globals using 8-byte handle cells.
- [x] Support null externref constant expressions.
- [x] Add exported/mutable local externref globals and typed instance accessors.
- [x] Add imported/shared externref globals and host-created store-bound global objects.
- [x] Add module-local externref tables with 8-byte entries rather than reusing
  the 32-byte funcref call-descriptor layout.
- [x] Support local externref `table.get`, `table.set`, `table.size`,
  `table.grow`, and `table.fill` across heterogeneous table indexes.
- [x] Support compatible `table.copy`, `table.init`, and `elem.drop` behavior.
- [x] Preserve null and opaque identity across locals, calls, globals, tables,
  imports, and exports.
- [x] Require a compatible externref store when sharing an externref table across
  instances, including runtime-owned construction and local export/re-export.

### P7 — Generalize Element Metadata

Replace funcref-table-0-specific metadata with typed, table-indexed element
metadata.

- [x] Store the destination table index for active segments.
- [x] Store the segment reference type.
- [x] Represent active, passive, and declarative modes explicitly.
- [x] Represent `ref.null` and `ref.func` element expressions without conflating
  null with an ordinary function index.
- [x] Support typed externref segments; WebAssembly 2.0 module expressions can
  initialize them with null references.
- [x] Keep per-instance drop state for passive segments.
- [x] Preserve correct instantiation-time bounds traps and all-or-nothing
  initialization behavior.
- [x] Update `table.init`, `table.copy`, active initialization, validation, and
  footprint accounting. Codec v19 keeps its legacy funcref encoding and rejects
  externref/heterogeneous typed metadata until a deliberate version bump.

A possible metadata direction is:

```go
type ElemInit struct {
    TableIndex uint32
    RefType    ValType
    Mode       ElemMode
    Values     []RefInit
}
```

### P8 — Multiple Tables

Preserve the current table-0 fast path while adding a table directory.

- [x] Preserve the table-0 compatibility fields and codec-v19 sole-import path
  while storing complete additional import metadata in the already-required
  nonzero table entries. Runtime/module/spec linking sees every declaration in
  order without allocating indexed metadata for scalar or one-table modules.
- [x] Resolve every imported/exported table by exact index and name. Imported
  handles remain foreign-owned; local nonzero descriptors are arena-owned and
  directory-addressed.
- [x] Retain the existing basedata table-0 pointer for immediate table index 0.
- [x] Add a basedata table-directory pointer for nonzero indexes. It reuses the
  former unused descriptor-count slot, so basedata remains 128 bytes.
- [x] Compile table index 0 to the current direct load sequence.
- [x] Compile nonzero constant indexes to a directory lookup.
- [x] Remove `readSingleTableIndex` and `readTablePairIndexes` restrictions.
- [x] Support indexed `table.get`, `table.set`, `table.size`, `table.grow`,
  `table.fill`, `table.copy`, and `table.init` for imported/local funcref tables.
- [x] Support nonzero-table `call_indirect` with the correct element type and
  signature checks for imported/local funcref tables.
- [x] Support active element segments targeting any imported or local funcref table.
- [x] Support multiple imported funcref tables followed by locally defined
  tables, including distinct and aliased handles.
- [x] Resolve local table exports by exact name and index, including nonzero tables;
  imported-table re-exports use the same exact-name rule.
- [x] Update host-created tables to carry element type, entry stride, limits,
  ownership, and externref-store identity without growing the 64-byte `Table`.
- [x] Update table policy limits to account for all currently executable tables;
  `MaxTableEntries` is enforced independently for each local table.
- [x] Update instantiation-arena footprint checks for heterogeneous table entry
  sizes. Multiple funcref descriptors, 8-byte externref entries, and the compact
  directory are bounded without changing scalar or funcref table-0 metadata reads.

Preferred runtime shape:

- table 0 remains directly addressable through the existing basedata slot;
- a compact pointer directory handles tables 1..N;
- funcref tables use the current 32-byte call descriptors;
- externref tables use 8-byte handles.

### P9 — Codec, Snapshots, Pools, and Product Surface

- [ ] Bump the `.wago` codec version for reference types and per-table metadata.
- [ ] Serialize reference value types, table definitions/imports/exports,
  element metadata, and required feature bits.
- [ ] Continue to serialize only module structure and null/reference-function
  initializers, not live host externref objects.
- [ ] Explicitly reject snapshots containing live externrefs until an
  application-provided resolver is designed.
- [ ] Audit instance reset/pooling so tables, reference globals, passive element
  state, and externref-store bindings cannot leak between tenants.
- [ ] Audit cross-instance links and close ordering for reference ownership.
- [ ] Update module inspection APIs to report all tables and reference types.

### P10 — Feature Reporting and Documentation

- [x] Add `CoreFeatureSIMD` to `CoreFeaturesV2` so the public feature group matches
  the WebAssembly 2.0 release scope.
- [x] Keep every reference-type module surface behind
  `CoreFeatureReferenceTypes`; explicitly owned host descriptors execute while
  raw unowned descriptor egress remains fail-closed.
- [x] Keep `SupportedFeatures` as the build/host-admitted gate set rather than a
  zero-skip conformance claim. All valid Release 2 modules now compile; docs keep
  host-created funcref globals, codec, and instantiation gaps explicit.
- [x] Update `FEATURES.md` for complete funcref/externref table bulk operations
  and explicit host descriptor ownership while clearly listing the remaining
  host-global and codec gaps.
- [x] Update `ROADMAP.md` and `README.md` so multi-value semantics and typed
  externref element/table support match the implementation.
- [x] Document reference token/store lifetime and cross-runtime restrictions,
  including shared globals and tables.
- [ ] Publish exact WebAssembly 2.0 conformance counts when complete.

### P11 — Conformance and Performance Gate

- [x] Run the complete official WebAssembly 2.0 decode/validation corpus.
- [x] Run all currently executable assertions with reference arguments/results
  enabled and classify every remaining gap explicitly.
- [ ] Require zero feature-related module and assertion skips.
- [ ] Add focused tests for:
  - [x] undeclared `ref.func` rejection;
  - [x] funcref identity and null behavior;
  - [x] externref host round trips;
  - [x] reference locals, globals, params, results, and multi-value returns;
  - [x] multiple local/imported/exported tables;
  - [x] cross-table copy and overlap semantics;
  - [x] nonzero-table `call_indirect`;
  - [x] instantiation bounds traps;
  - [x] cross-instance reference ownership and close ordering;
  - [x] stale externref-handle rejection.
- [ ] Benchmark and report before/after numbers for:
  - [x] table-0 `call_indirect`;
  - [x] table-0 get/set/grow/fill/copy/init;
  - [x] ordinary scalar direct calls;
  - [x] compile latency;
  - [x] instantiation latency;
  - [x] zero-table and one-table instance footprint;
  - [x] funcref versus externref table bytes per entry;
  - [x] host calls with and without reference values.

## Definition of Done

Wago can claim WebAssembly 2.0 support when all of the following are true:

- [ ] Every Release 2.0 feature family is decoded, validated, executable, and
  feature-gated correctly.
- [ ] `funcref` and `externref` work in signatures, locals, globals, control flow,
  host calls, and tables.
- [ ] Multiple tables work for definitions, imports, exports, active elements,
  table operations, and `call_indirect`.
- [ ] The official WebAssembly 2.0 validation and execution corpus has no
  feature-related skips.
- [ ] `CoreFeaturesV2` and `SupportedFeatures` accurately describe the runtime.
- [ ] `.wago` loading rejects incompatible or unsupported reference metadata
  safely.
- [ ] Performance measurements show no unjustified regression to table-0,
  scalar-call, compile, instantiation, or footprint-sensitive paths.
- [ ] `FEATURES.md`, `ROADMAP.md`, `README.md`, and relevant developer docs match
  the implemented behavior.

## Engineering Constraints

- Keep malformed module rejection strict.
- Preserve the no-cgo runtime boundary.
- Do not place untracked Go pointers in native Wasm storage.
- Avoid process-global or otherwise unbounded reference caches.
- Preserve the table-0 and `call_indirect` hot paths unless measurements justify
  a regression.
- Keep table entry layouts type-specific to avoid wasting 32 bytes per externref.
- Add each feature as the smallest coherent, tested PR.
- Include benchmark and footprint numbers for runtime-layout or call-path
  changes.
