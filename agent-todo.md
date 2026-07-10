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
  close. Imported reference globals and externref globals remain fail-closed.
- [x] Decouple the canonical function-descriptor arena from table presence.
  Tables retain the existing direct descriptor path, while table-free modules
  allocate exactly `(function count + 1) * 32` arena bytes only when an executable
  body or global initializer uses `ref.func`; scalar and null-only modules allocate
  no descriptor arena. Compiled codec version 19 preserves this structural need
  for table-free `.wago` modules while all reference-global metadata remains
  rejected on marshal/load and snapshots.
- [ ] Broaden public funcref tokens to host descriptors and remaining imported
  global/cross-instance boundaries; these remain fail-closed.
- [x] Measure the token foundation: scalar, null, local egress, imported egress,
  and same-runtime round trips remain 0 B/op and 0 allocs/op. Stable medians are
  16.23, 20.59, 28.42, 43.26, and 35.39 ns/op respectively. Warmed Runtime
  instantiation remains 1,224 B/op and 7 allocs/op. Instance size is 776 bytes
  (+32 from `e54f9556`); `referenceStore` is 48 bytes (+8 for the bounded live-
  instance registry map header), while standalone scalar/null-only instances
  keep the private store lazy.
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
- [ ] Executable `externref` support.
- [ ] Multiple tables are partial. Multiple local funcref tables now execute for
  definitions, active elements, every indexed table operation, cross-table
  copy/init, and nonzero-table `call_indirect`. Multiple imported tables,
  imported+local combinations, nonzero table exports, and externref tables remain.
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
- [ ] WebAssembly 2.0 conformance gate with no feature-related skips. With WABT
  1.0.36 available, the July 10, 2026 execution run reports 1,494 passed / 106
  skipped modules and 47,733 passed / 0 failed / 515 skipped assertions. Gap
  reasons are compile-rejected=26, instantiate-rejected=80,
  module-unavailable=424, absent-export=0, reference-argument=36,
  reference-result=55, and reference-global=0. `table_copy.wast` is fully green
  at 52/0/0 modules and 1,675/0/0 assertions; `table_init.wast` is fully green at
  35/0/0 modules and 677/0/0 assertions.

The remaining documentation closeout is primarily final feature reporting,
README alignment, and the eventual zero-skip support claim.

## Implementation Order

### P0 — Pin and Wire the Official WebAssembly 2.0 Suite

- [x] Add a separately pinned official WebAssembly 2.0 testsuite revision rather
  than replacing the pre-reference-types WebAssembly 1.0 conformance baseline.
- [x] Update the validation and execution harnesses for the 2.0 core-suite
  layout.
- [x] Install or provision `wast2json` in CI for the 2.0 job.
- [ ] Make valid modules rejected as unsupported fail the 2.0 job.
- [ ] Make invalid modules accepted by the decoder/validator fail the job.
- [ ] Add reference-valued assertion argument and result support.
  - [x] Encode, invoke, and assert null `funcref` arguments/results as token zero.
  - [ ] Add non-null funcref identity and externref values after their ownership
    models are implemented.
- [ ] Stop treating reference arguments, reference results, or reference globals
  as out-of-scope skips in `src/wago/spectest_exec_test.go`.
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
- [ ] Validate `funcref` and `externref` in function params/results, locals,
  globals, block signatures, typed `select`, tables, and element segments.
- [ ] Validate multiple-table indexes for `call_indirect`, active elements, and
  all table instructions.
- [ ] Validate element-segment and table reference-type compatibility.
- [ ] Validate `ref.null`, `ref.func`, and `ref.is_null` in every WebAssembly 2.0
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
  - [ ] Enable reference values at reflection-free host-call boundaries when P3
    and P5 make funcref/externref execution available.
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
- [ ] Permit `funcref` in function parameters, results, locals, and block
  parameters/results in the frontend support pass.
- [ ] Carry funcref through direct calls, recursion, multi-value returns,
  branches, typed `select`, and spills as a 64-bit JIT value.
- [ ] Carry funcref through cross-instance calls and synchronous host imports.
- [ ] Return and accept runtime-owned non-null funcref tokens through `Invoke`
  and typed `Call` without exposing descriptor addresses.
  - [x] Issue stable opaque tokens for local `ref.func` descriptors, validate
    same-store ingress, retain producer resources, and give standalone
    `Instantiate` a lazy private-store policy.
  - [x] Translate same-runtime cross-instance imported funcrefs through exact
    `InstanceExport`, descriptor-range, and `refSlot` canonicalization checks.
  - [ ] Translate host funcrefs and broader cross-instance/global boundaries;
    keep them fail-closed until their owners and close ordering are proven.
- [ ] Zero-initialize funcref locals.
- [ ] Audit every scalar/non-`v128` assumption in call marshalling, result
  handling, codecs, and snapshots.
- [ ] Preserve descriptor identity for `ref.is_null` and any supported identity
  operations.

The backend already maps reference values to a 64-bit machine type in several
places. Reuse that representation rather than adding a parallel register class.

### P4 — Funcref Globals and Lifetime

- [x] Add 8-byte module-local funcref global cells with immutable/mutable JIT
  access and exported typed host access.
- [ ] Support imported and cross-instance funcref global objects; local exported
  globals are executable, while the shared-object ownership model is pending.
- [x] Support `ref.null` global initializers.
- [x] Support valid non-null `ref.func` global initializers.
- [ ] Support imported immutable `global.get` initializers where the 2.0 rules
  permit them.
- [ ] Add host constructors and accessors for funcref globals.
- [ ] Keep funcref globals out of numeric-only optimizations unless explicitly
  proven safe.
- [x] Define local-cell funcref ownership so a token stored from another
  same-runtime instance retains the producer after logical close.
- [x] Ensure store-owned funcref tokens retained by local globals also retain the
  required code mapping and home instance context.
- [ ] Extend the same proof to imported/shared global objects and host-created
  funcref globals.

Do not expose the current pointer into an instance descriptor arena as the public
funcref identity.

### P5 — Externref Store and Host ABI

Implement externrefs as handles, not pointers in mmap-backed Wasm storage.

- [ ] Reserve handle zero for null.
- [ ] Add generation checking to detect stale handles.
- [ ] Add a runtime/store-owned Go table mapping handles to embedder objects.
- [ ] Keep native code limited to copying/testing the 64-bit handle.
- [ ] Translate handles only at public API and host-call boundaries.
- [ ] Define whether standalone `Instantiate` creates a private store.
- [ ] Share one store among instances created by the same `Runtime`.
- [ ] Reject or explicitly bridge externrefs passed between incompatible stores.
- [ ] Retain registered externrefs for the store lifetime unless a sound,
  measured reclamation scheme is implemented.
- [ ] Release the store and its Go roots on `Runtime.Close`.
- [ ] Cover host functions that accept, return, and round-trip externrefs.

Avoid a process-global unbounded cache. A per-runtime/store table makes the
lifetime and memory bound explicit.

### P6 — Externref Globals and Tables

- [ ] Add externref globals using 8-byte handle cells.
- [ ] Support null externref constant expressions.
- [ ] Add imported/exported/mutable externref globals and host accessors.
- [ ] Add externref tables with 8-byte entries rather than reusing the 32-byte
  funcref call-descriptor layout.
- [ ] Support externref `table.get`, `table.set`, `table.size`, `table.grow`, and
  `table.fill`.
- [ ] Support compatible `table.copy`, `table.init`, and `elem.drop` behavior.
- [ ] Preserve null and opaque identity across locals, calls, globals, tables,
  imports, and exports.
- [ ] Require a compatible externref store when sharing an externref table across
  instances.

### P7 — Generalize Element Metadata

Replace funcref-table-0-specific metadata with typed, table-indexed element
metadata.

- [x] Store the destination table index for active segments.
- [ ] Store the segment reference type.
- [ ] Represent active, passive, and declarative modes explicitly.
- [ ] Represent `ref.null` and `ref.func` element expressions without conflating
  null with an ordinary function index.
- [ ] Support typed externref segments; WebAssembly 2.0 module expressions can
  initialize them with null references.
- [ ] Keep per-instance drop state for passive segments.
- [ ] Preserve correct instantiation-time bounds traps and all-or-nothing
  initialization behavior.
- [ ] Update `table.init`, `table.copy`, active initialization, validation,
  footprint accounting, and serialization.

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

- [ ] Replace the remaining table-0 compatibility fields and single table import
  fields with complete per-table import/export metadata. Local tables 1..N now
  use compact per-table metadata while table 0 deliberately retains its legacy fields.
- [ ] Replace the remaining table-0 export/shared handle with indexed table
  handles/descriptors. Local nonzero descriptors are arena-owned and directory-addressed.
- [x] Retain the existing basedata table-0 pointer for immediate table index 0.
- [x] Add a basedata table-directory pointer for nonzero indexes. It reuses the
  former unused descriptor-count slot, so basedata remains 128 bytes.
- [x] Compile table index 0 to the current direct load sequence.
- [x] Compile nonzero constant indexes to a directory lookup.
- [x] Remove `readSingleTableIndex` and `readTablePairIndexes` restrictions.
- [x] Support indexed `table.get`, `table.set`, `table.size`, `table.grow`,
  `table.fill`, `table.copy`, and `table.init` for local funcref tables.
- [x] Support nonzero-table `call_indirect` with the correct element type and
  signature checks for local funcref tables.
- [x] Support active element segments targeting any local declared table.
- [ ] Support combinations of imported and locally defined tables.
- [ ] Resolve table exports by name instead of treating the name as advisory.
- [ ] Update host-created tables to carry element type, entry stride, limits,
  ownership, and externref-store identity.
- [x] Update table policy limits to account for all currently executable tables;
  `MaxTableEntries` is enforced independently for each local table.
- [ ] Update instantiation-arena footprint checks for heterogeneous table entry
  sizes. Multiple funcref descriptors and the compact directory are bounded now;
  8-byte externref entries remain pending.

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
- [ ] Keep reference-type subfeatures behind `CoreFeatureReferenceTypes` until
  the complete 2.0 subset is executable.
- [ ] Decide whether `SupportedFeatures` should report partial families; prefer
  not to claim complete reference-types support while valid 2.0 modules are
  rejected.
- [ ] Update `FEATURES.md` to mark table bulk operations and passive elements as
  implemented for table 0, while clearly listing externref and multiple-table
  gaps.
- [ ] Update `ROADMAP.md` and `README.md` so multi-value semantics are not called
  incomplete solely because the optimized ABI is pending.
- [ ] Document reference token/store lifetime and cross-runtime restrictions.
- [ ] Publish exact WebAssembly 2.0 conformance counts when complete.

### P11 — Conformance and Performance Gate

- [ ] Run the complete official WebAssembly 2.0 decode/validation corpus.
- [ ] Run all applicable execution assertions with reference arguments/results
  enabled.
- [ ] Require zero feature-related module and assertion skips.
- [ ] Add focused tests for:
  - [ ] undeclared `ref.func` rejection;
  - [ ] funcref identity and null behavior;
  - [ ] externref host round trips;
  - [ ] reference locals, globals, params, results, and multi-value returns;
  - [ ] multiple local/imported/exported tables;
  - [ ] cross-table copy and overlap semantics;
  - [ ] nonzero-table `call_indirect`;
  - [ ] instantiation bounds traps;
  - [ ] cross-instance reference ownership and close ordering;
  - [ ] stale externref-handle rejection.
- [ ] Benchmark and report before/after numbers for:
  - [ ] table-0 `call_indirect`;
  - [ ] table-0 get/set/grow/fill/copy/init;
  - [ ] ordinary scalar direct calls;
  - [ ] compile latency;
  - [ ] instantiation latency;
  - [ ] zero-table and one-table instance footprint;
  - [ ] funcref versus externref table bytes per entry;
  - [ ] host calls with and without reference values.

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
