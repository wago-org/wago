# Wazero test applicability ledger

Upstream: `tetratelabs/wazero` revision `236c2458ed22010150de76c5397eca2c89af3b4f` (July 14, 2026).

This ledger accounts for all **234** upstream Go test files at that revision. “Already covered” means a named wago suite exercises the same Wasm/runtime contract; “not applicable” is reserved for wazero implementation or product APIs that wago does not expose. Unsupported proposals retain explicit fail-closed tests instead of skips.

Primary ports:

- `src/core/compiler/wasm/wazero_leb128_port_test.go`
- `src/core/compiler/wasm/wazero_validation_port_test.go`
- `src/wago/wazero_wasm_api_port_test.go`
- `src/wago/wazero_i32_upper_bits_port_test.go`
- `src/wago/wazero_runtime_regressions_port_test.go`
- `src/wago/wazero_fuzzcases_port_test.go`
- `src/wago/wazero_many_values_port_test.go`
- `src/wago/wazero_stack_width_port_test.go`
- `src/wago/wazero_engine_fixtures_port_test.go`
- `src/wago/wazero_adversarial_runtime_port_test.go`
- `src/wago/wazero_extended_const_port_test.go`
- `src/wago/wazero_core_v2_port_test.go`
- `src/wago/wazero_proposal_corpus_port_test.go`
- `src/wago/wazero_concurrency_port_test.go`
- `src/core/compiler/wasm/wazero_extended_const_port_test.go`
- `src/core/compiler/wasm/wazero_core_v2_port_test.go`

## Subtest and corpus accounting

The file ledger below is not used as a substitute for subtest accounting:

- `internal/integration_test/engine/adhoc_test.go` registers 39 engine cases.
  Twenty-seven observable runtime/compiler contracts are ported or matched by
  named Wago suites: all huge-stack and many-value variants, the 40,000-function
  relocation layout plus arm64 BL boundary checks,
  imported mutable globals, overflow/extension/unreachable, recursive and
  indirect host entry, host memory, numeric host slots, close interruption and
  close-in-flight, repeated instantiation isolation, memory operations and
  recursive growth, reftype imports, call arity, module memory, two-level host
  panic integrity, table-owner/table-writer close retention, and huge-call-stack
  unwind. The remaining 12 cases are wazero listener/context-reflection,
  user-defined Go-primitive reflection, or experimental table-lookup APIs Wago
  does not expose; they are not counted as Wasm semantic coverage.
- Core v2 accounting is exact at 147 WAST files (90 top-level core files and 57
  SIMD files), with the pinned tree digest checked independently by decode and
  execution wrappers. Validation pins 1,600 modules, 2,880 binary assertions,
  and 1,077 malformed-text commands. Execution now accounts for all 48,331
  runtime assertions, including the 83 `assert_unlinkable` commands previously
  ignored. Those checks exposed 12 failures around memory-export names, memory
  import limits, and a resulting table-initialization side effect; exact export
  metadata plus link-time limit validation now make the expanded corpus green.
  This supersedes the historical 46-file selector boundary and prevents the old
  48,248-assertion subset from being reported as the full corpus.
- Extended-constant accounting is exact at 63 generated artifacts. Proposal
  fail-closed accounting is exact at 782 generated exception-handling,
  tail-call, threads, and typed-function-reference artifacts. The 42 supported
  negative-instantiation binaries are counted separately from ordinary modules
  instead of accepting arbitrary empty-import failures; all 12 self-contained
  element-segment negatives run with the required `spectest.table` binding and
  assert the intended out-of-bounds failure.
- Fuzz and engine binary manifests are exact at 71 and 23 fixtures,
  respectively; manifest drift fails tests instead of silently changing scope.

The close-safety fix keeps the ordinary invocation path allocation-free. On an
AMD Ryzen 7 8845HS, five runs of `BenchmarkInvokeAddOne` had a 37.31 ns/op median
versus 32.84 ns/op at the pinned `main` base (a 4.47 ns/op invocation-lease cost),
with 0 B/op and 0 allocs/op in both cases. Replacing the initial mutex-based lease
with one packed atomic state also kept `unsafe.Sizeof(Instance{})` unchanged at
776 bytes.

| Upstream test file | Disposition | Rationale / wago coverage |
|---|---|---|
| `api/features_test.go` | ported / already covered | ported/adapted in src/wago/wazero_wasm_api_port_test.go |
| `api/wasm_test.go` | ported / already covered | ported/adapted in src/wago/wazero_wasm_api_port_test.go |
| `builder_test.go` | not applicable | wazero builder/cache/config API has no matching wago API contract |
| `cache_example_test.go` | benchmark/example only | documentation or performance harness, not a correctness regression suite |
| `cache_test.go` | not applicable | wazero builder/cache/config API has no matching wago API contract |
| `cmd/wazero/wazero_test.go` | not applicable | tests wazero WASI/OS/CLI/library integration that wago does not implement in core |
| `config_example_test.go` | benchmark/example only | documentation or performance harness, not a correctness regression suite |
| `config_test.go` | not applicable | wazero builder/cache/config API has no matching wago API contract |
| `context_done_example_test.go` | benchmark/example only | documentation or performance harness, not a correctness regression suite |
| `example_test.go` | reviewed, no direct port | wazero-specific API or helper behavior with no independent wago semantic oracle |
| `examples/allocation/rust/greet_test.go` | benchmark/example only | documentation or performance harness, not a correctness regression suite |
| `examples/allocation/tinygo/greet_test.go` | benchmark/example only | documentation or performance harness, not a correctness regression suite |
| `examples/allocation/zig/greet_test.go` | benchmark/example only | documentation or performance harness, not a correctness regression suite |
| `examples/basic/add_test.go` | benchmark/example only | documentation or performance harness, not a correctness regression suite |
| `examples/cli/cli_test.go` | benchmark/example only | documentation or performance harness, not a correctness regression suite |
| `examples/concurrent-instantiation/main_test.go` | benchmark/example only | documentation or performance harness, not a correctness regression suite |
| `examples/import-go/age-calculator_test.go` | benchmark/example only | documentation or performance harness, not a correctness regression suite |
| `examples/multiple-results/multiple-results_test.go` | benchmark/example only | documentation or performance harness, not a correctness regression suite |
| `examples/multiple-runtimes/counter_test.go` | benchmark/example only | documentation or performance harness, not a correctness regression suite |
| `experimental/checkpoint_example_test.go` | benchmark/example only | documentation or performance harness, not a correctness regression suite |
| `experimental/checkpoint_test.go` | not applicable | wazero-specific experimental/import adapter API; no corresponding wago surface |
| `experimental/close_example_test.go` | benchmark/example only | documentation or performance harness, not a correctness regression suite |
| `experimental/close_test.go` | not applicable | wazero-specific experimental/import adapter API; no corresponding wago surface |
| `experimental/exceptions_test.go` | not applicable | wazero-specific experimental/import adapter API; no corresponding wago surface |
| `experimental/features_example_test.go` | benchmark/example only | documentation or performance harness, not a correctness regression suite |
| `experimental/importresolver_example_test.go` | benchmark/example only | documentation or performance harness, not a correctness regression suite |
| `experimental/importresolver_test.go` | not applicable | wazero-specific experimental/import adapter API; no corresponding wago surface |
| `experimental/listener_example_test.go` | benchmark/example only | documentation or performance harness, not a correctness regression suite |
| `experimental/listener_test.go` | not applicable | wazero-specific experimental/import adapter API; no corresponding wago surface |
| `experimental/logging/log_listener_example_test.go` | benchmark/example only | documentation or performance harness, not a correctness regression suite |
| `experimental/logging/log_listener_test.go` | not applicable | wazero-specific experimental/import adapter API; no corresponding wago surface |
| `experimental/sock/sock_test.go` | not applicable | tests wazero WASI/OS/CLI/library integration that wago does not implement in core |
| `experimental/sys/error_test.go` | not applicable | tests wazero WASI/OS/CLI/library integration that wago does not implement in core |
| `experimental/sysfs/config_example_test.go` | benchmark/example only | documentation or performance harness, not a correctness regression suite |
| `experimental/table/lookup_test.go` | not applicable | wazero-specific experimental/import adapter API; no corresponding wago surface |
| `experimental/wazerotest/wazerotest_test.go` | not applicable | wazero-specific experimental/import adapter API; no corresponding wago surface |
| `fsconfig_example_test.go` | benchmark/example only | documentation or performance harness, not a correctness regression suite |
| `fsconfig_test.go` | not applicable | wazero builder/cache/config API has no matching wago API contract |
| `imports/assemblyscript/assemblyscript_example_test.go` | benchmark/example only | documentation or performance harness, not a correctness regression suite |
| `imports/assemblyscript/assemblyscript_test.go` | not applicable | wazero-specific experimental/import adapter API; no corresponding wago surface |
| `imports/assemblyscript/example/assemblyscript_test.go` | not applicable | wazero-specific experimental/import adapter API; no corresponding wago surface |
| `imports/emscripten/emscripten_example_test.go` | benchmark/example only | documentation or performance harness, not a correctness regression suite |
| `imports/emscripten/emscripten_test.go` | not applicable | wazero-specific experimental/import adapter API; no corresponding wago surface |
| `imports/wasi_snapshot_preview1/args_test.go` | not applicable | tests wazero WASI/OS/CLI/library integration that wago does not implement in core |
| `imports/wasi_snapshot_preview1/clock_test.go` | not applicable | tests wazero WASI/OS/CLI/library integration that wago does not implement in core |
| `imports/wasi_snapshot_preview1/environ_test.go` | not applicable | tests wazero WASI/OS/CLI/library integration that wago does not implement in core |
| `imports/wasi_snapshot_preview1/example/cat_test.go` | not applicable | tests wazero WASI/OS/CLI/library integration that wago does not implement in core |
| `imports/wasi_snapshot_preview1/example_test.go` | not applicable | tests wazero WASI/OS/CLI/library integration that wago does not implement in core |
| `imports/wasi_snapshot_preview1/fs_test.go` | not applicable | tests wazero WASI/OS/CLI/library integration that wago does not implement in core |
| `imports/wasi_snapshot_preview1/fs_unit_test.go` | not applicable | tests wazero WASI/OS/CLI/library integration that wago does not implement in core |
| `imports/wasi_snapshot_preview1/poll_test.go` | not applicable | tests wazero WASI/OS/CLI/library integration that wago does not implement in core |
| `imports/wasi_snapshot_preview1/proc_test.go` | not applicable | tests wazero WASI/OS/CLI/library integration that wago does not implement in core |
| `imports/wasi_snapshot_preview1/random_test.go` | not applicable | tests wazero WASI/OS/CLI/library integration that wago does not implement in core |
| `imports/wasi_snapshot_preview1/sched_test.go` | not applicable | tests wazero WASI/OS/CLI/library integration that wago does not implement in core |
| `imports/wasi_snapshot_preview1/sock_test.go` | not applicable | tests wazero WASI/OS/CLI/library integration that wago does not implement in core |
| `imports/wasi_snapshot_preview1/sock_unit_test.go` | not applicable | tests wazero WASI/OS/CLI/library integration that wago does not implement in core |
| `imports/wasi_snapshot_preview1/usage_test.go` | not applicable | tests wazero WASI/OS/CLI/library integration that wago does not implement in core |
| `imports/wasi_snapshot_preview1/wasi_bench_test.go` | not applicable | tests wazero WASI/OS/CLI/library integration that wago does not implement in core |
| `imports/wasi_snapshot_preview1/wasi_snapshot_preview1_test.go` | not applicable | tests wazero WASI/OS/CLI/library integration that wago does not implement in core |
| `imports/wasi_snapshot_preview1/wasi_stdlib_test.go` | not applicable | tests wazero WASI/OS/CLI/library integration that wago does not implement in core |
| `imports/wasi_snapshot_preview1/wasi_stdlib_unix_test.go` | not applicable | tests wazero WASI/OS/CLI/library integration that wago does not implement in core |
| `imports/wasi_snapshot_preview1/wasi_test.go` | not applicable | tests wazero WASI/OS/CLI/library integration that wago does not implement in core |
| `internal/assemblyscript/logging/logging_test.go` | not applicable | wazero-specific experimental/import adapter API; no corresponding wago surface |
| `internal/descriptor/export_test.go` | not applicable | wazero implementation-private engine, cache, platform, or test-helper internals |
| `internal/descriptor/table_test.go` | not applicable | wazero implementation-private engine, cache, platform, or test-helper internals |
| `internal/emscripten/emscripten_test.go` | not applicable | wazero-specific experimental/import adapter API; no corresponding wago surface |
| `internal/engine/interpreter/compiler_test.go` | not applicable | wazero implementation-private engine, cache, platform, or test-helper internals |
| `internal/engine/interpreter/interpreter_test.go` | not applicable | wazero implementation-private engine, cache, platform, or test-helper internals |
| `internal/engine/interpreter/operations_test.go` | not applicable | wazero implementation-private engine, cache, platform, or test-helper internals |
| `internal/engine/interpreter/signature_test.go` | not applicable | wazero implementation-private engine, cache, platform, or test-helper internals |
| `internal/engine/interpreter/typed_func_refs_test.go` | not applicable | wazero implementation-private engine, cache, platform, or test-helper internals |
| `internal/engine/wazevo/backend/abi_test.go` | not applicable | wazero implementation-private engine, cache, platform, or test-helper internals |
| `internal/engine/wazevo/backend/backend_test.go` | not applicable | wazero implementation-private engine, cache, platform, or test-helper internals |
| `internal/engine/wazevo/backend/compiler_lower_test.go` | not applicable | wazero implementation-private engine, cache, platform, or test-helper internals |
| `internal/engine/wazevo/backend/go_call_test.go` | not applicable | wazero implementation-private engine, cache, platform, or test-helper internals |
| `internal/engine/wazevo/backend/isa/amd64/abi_entry_preamble_test.go` | not applicable | wazero implementation-private engine, cache, platform, or test-helper internals |
| `internal/engine/wazevo/backend/isa/amd64/abi_go_call_test.go` | not applicable | wazero implementation-private engine, cache, platform, or test-helper internals |
| `internal/engine/wazevo/backend/isa/amd64/instr_encoding_test.go` | not applicable | wazero implementation-private engine, cache, platform, or test-helper internals |
| `internal/engine/wazevo/backend/isa/amd64/instr_test.go` | not applicable | wazero implementation-private engine, cache, platform, or test-helper internals |
| `internal/engine/wazevo/backend/isa/amd64/lower_mem_test.go` | not applicable | wazero implementation-private engine, cache, platform, or test-helper internals |
| `internal/engine/wazevo/backend/isa/amd64/machine_pro_epi_logue_test.go` | not applicable | wazero implementation-private engine, cache, platform, or test-helper internals |
| `internal/engine/wazevo/backend/isa/amd64/machine_regalloc_test.go` | not applicable | wazero implementation-private engine, cache, platform, or test-helper internals |
| `internal/engine/wazevo/backend/isa/amd64/machine_test.go` | not applicable | wazero implementation-private engine, cache, platform, or test-helper internals |
| `internal/engine/wazevo/backend/isa/amd64/reg_test.go` | not applicable | wazero implementation-private engine, cache, platform, or test-helper internals |
| `internal/engine/wazevo/backend/isa/amd64/stack_test.go` | not applicable | wazero implementation-private engine, cache, platform, or test-helper internals |
| `internal/engine/wazevo/backend/isa/amd64/util_test.go` | not applicable | wazero implementation-private engine, cache, platform, or test-helper internals |
| `internal/engine/wazevo/backend/isa/arm64/abi_entry_preamble_test.go` | not applicable | wazero implementation-private engine, cache, platform, or test-helper internals |
| `internal/engine/wazevo/backend/isa/arm64/abi_go_call_test.go` | not applicable | wazero implementation-private engine, cache, platform, or test-helper internals |
| `internal/engine/wazevo/backend/isa/arm64/abi_test.go` | not applicable | wazero implementation-private engine, cache, platform, or test-helper internals |
| `internal/engine/wazevo/backend/isa/arm64/instr_encoding_test.go` | not applicable | wazero implementation-private engine, cache, platform, or test-helper internals |
| `internal/engine/wazevo/backend/isa/arm64/instr_test.go` | not applicable | wazero implementation-private engine, cache, platform, or test-helper internals |
| `internal/engine/wazevo/backend/isa/arm64/lower_constant_test.go` | not applicable | wazero implementation-private engine, cache, platform, or test-helper internals |
| `internal/engine/wazevo/backend/isa/arm64/lower_instr_operands_test.go` | not applicable | wazero implementation-private engine, cache, platform, or test-helper internals |
| `internal/engine/wazevo/backend/isa/arm64/lower_instr_test.go` | not applicable | wazero implementation-private engine, cache, platform, or test-helper internals |
| `internal/engine/wazevo/backend/isa/arm64/lower_mem_test.go` | not applicable | wazero implementation-private engine, cache, platform, or test-helper internals |
| `internal/engine/wazevo/backend/isa/arm64/machine_pro_epi_logue_test.go` | not applicable | wazero implementation-private engine, cache, platform, or test-helper internals |
| `internal/engine/wazevo/backend/isa/arm64/machine_regalloc_test.go` | not applicable | wazero implementation-private engine, cache, platform, or test-helper internals |
| `internal/engine/wazevo/backend/isa/arm64/machine_relocation_test.go` | not applicable | wazero implementation-private engine, cache, platform, or test-helper internals |
| `internal/engine/wazevo/backend/isa/arm64/machine_test.go` | not applicable | wazero implementation-private engine, cache, platform, or test-helper internals |
| `internal/engine/wazevo/backend/isa/arm64/reg_test.go` | not applicable | wazero implementation-private engine, cache, platform, or test-helper internals |
| `internal/engine/wazevo/backend/isa/arm64/unwind_stack_test.go` | not applicable | wazero implementation-private engine, cache, platform, or test-helper internals |
| `internal/engine/wazevo/backend/isa/arm64/util_test.go` | not applicable | wazero implementation-private engine, cache, platform, or test-helper internals |
| `internal/engine/wazevo/backend/machine_test.go` | not applicable | wazero implementation-private engine, cache, platform, or test-helper internals |
| `internal/engine/wazevo/backend/regalloc/api_test.go` | not applicable | wazero implementation-private engine, cache, platform, or test-helper internals |
| `internal/engine/wazevo/backend/regalloc/reg_test.go` | not applicable | wazero implementation-private engine, cache, platform, or test-helper internals |
| `internal/engine/wazevo/backend/regalloc/regalloc_test.go` | not applicable | wazero implementation-private engine, cache, platform, or test-helper internals |
| `internal/engine/wazevo/call_engine_test.go` | not applicable | wazero implementation-private engine, cache, platform, or test-helper internals |
| `internal/engine/wazevo/e2e_test.go` | not applicable | wazero implementation-private engine, cache, platform, or test-helper internals |
| `internal/engine/wazevo/engine_cache_test.go` | not applicable | wazero implementation-private engine, cache, platform, or test-helper internals |
| `internal/engine/wazevo/engine_test.go` | not applicable | wazero implementation-private engine, cache, platform, or test-helper internals |
| `internal/engine/wazevo/frontend/frontend_test.go` | not applicable | wazero implementation-private engine, cache, platform, or test-helper internals |
| `internal/engine/wazevo/frontend/lower_test.go` | not applicable | wazero implementation-private engine, cache, platform, or test-helper internals |
| `internal/engine/wazevo/hostmodule_test.go` | not applicable | wazero implementation-private engine, cache, platform, or test-helper internals |
| `internal/engine/wazevo/module_engine_test.go` | not applicable | wazero implementation-private engine, cache, platform, or test-helper internals |
| `internal/engine/wazevo/ssa/builder_test.go` | not applicable | wazero implementation-private engine, cache, platform, or test-helper internals |
| `internal/engine/wazevo/ssa/instructions_test.go` | not applicable | wazero implementation-private engine, cache, platform, or test-helper internals |
| `internal/engine/wazevo/ssa/pass_blk_layouts_test.go` | not applicable | wazero implementation-private engine, cache, platform, or test-helper internals |
| `internal/engine/wazevo/ssa/pass_cfg_test.go` | not applicable | wazero implementation-private engine, cache, platform, or test-helper internals |
| `internal/engine/wazevo/ssa/pass_test.go` | not applicable | wazero implementation-private engine, cache, platform, or test-helper internals |
| `internal/engine/wazevo/ssa/ssa_test.go` | not applicable | wazero implementation-private engine, cache, platform, or test-helper internals |
| `internal/engine/wazevo/ssa/vs_test.go` | not applicable | wazero implementation-private engine, cache, platform, or test-helper internals |
| `internal/engine/wazevo/wazevo_test.go` | not applicable | wazero implementation-private engine, cache, platform, or test-helper internals |
| `internal/engine/wazevo/wazevoapi/exitcode_test.go` | not applicable | wazero implementation-private engine, cache, platform, or test-helper internals |
| `internal/engine/wazevo/wazevoapi/offsetdata_test.go` | not applicable | wazero implementation-private engine, cache, platform, or test-helper internals |
| `internal/engine/wazevo/wazevoapi/pool_test.go` | not applicable | wazero implementation-private engine, cache, platform, or test-helper internals |
| `internal/filecache/file_cache_test.go` | not applicable | wazero implementation-private engine, cache, platform, or test-helper internals |
| `internal/integration_test/bench/bench_test.go` | benchmark/example only | documentation or performance harness, not a correctness regression suite |
| `internal/integration_test/bench/debug_bench_test.go` | benchmark/example only | documentation or performance harness, not a correctness regression suite |
| `internal/integration_test/bench/decoder_bench_test.go` | benchmark/example only | documentation or performance harness, not a correctness regression suite |
| `internal/integration_test/bench/hostfunc_bench_test.go` | benchmark/example only | documentation or performance harness, not a correctness regression suite |
| `internal/integration_test/bench/interface_bench_test.go` | benchmark/example only | documentation or performance harness, not a correctness regression suite |
| `internal/integration_test/bench/memory_bench_test.go` | benchmark/example only | documentation or performance harness, not a correctness regression suite |
| `internal/integration_test/engine/adhoc_test.go` | ported / already covered | concrete ports cover huge/many-value stacks, the bounded-memory 40,000-function relocation layout plus direct arm64 BL boundary tests, overflow, zero-extension, host memory, recursive entry and memory growth, reftype imports, memory growth/bounds, nested host panics, infinite-loop interruption, prepared/callback/importing/imported close-in-flight, and huge-call-stack unwind; listener/DWARF-only surfaces remain non-applicable |
| `internal/integration_test/engine/dwarf_test.go` | not applicable | Wago does not expose wazero's DWARF source-line stack-trace formatting surface |
| `internal/integration_test/engine/eh_hammer_test.go` | unsupported, fail-closed tested | all pinned exception-handling generated binaries are accounted for and valid proposal modules reject explicitly as unsupported |
| `internal/integration_test/engine/exceptions_test.go` | unsupported, fail-closed tested | all pinned exception-handling generated binaries plus 11 engine regressions reject explicitly as unsupported |
| `internal/integration_test/engine/hammer_test.go` | ported / already covered | concurrent compile/instantiate/execute ported; active caller close has exact interrupted-trap oracles and deferred physical release in the adversarial runtime port |
| `internal/integration_test/engine/i32_upper_bits_test.go` | ported / already covered | ported in src/wago/wazero_i32_upper_bits_port_test.go |
| `internal/integration_test/engine/memleak_test.go` | ported / already covered | deterministic repeated runtime/compile/instantiate/close stress includes host functions, externrefs, globals, tables, instances, and compiled modules; it asserts exact execution and a bounded post-GC retained-heap delta |
| `internal/integration_test/engine/tailcall_test.go` | unsupported, fail-closed tested | all pinned tail-call generated binaries are accounted for and valid proposal modules reject explicitly as unsupported |
| `internal/integration_test/engine/threads_test.go` | unsupported, fail-closed tested | all pinned threads/atomic generated binaries are accounted for and shared memory/atomic modules reject explicitly |
| `internal/integration_test/engine/typed_func_refs_test.go` | unsupported, fail-closed tested | all 558 pinned typed-function-reference binaries are accounted for; supported core reference modules compile and proposal-dependent modules reject explicitly |
| `internal/integration_test/engine/urem_regalloc_test.go` | ported / already covered | ported in src/wago/wazero_runtime_regressions_port_test.go |
| `internal/integration_test/filecache/filecache_test.go` | not applicable | wazero implementation-private engine, cache, platform, or test-helper internals |
| `internal/integration_test/fuzz/wazerolib/nodiff_test.go` | not applicable | wazero implementation-private engine, cache, platform, or test-helper internals |
| `internal/integration_test/fuzz/wazerolib/validate_test.go` | not applicable | wazero implementation-private engine, cache, platform, or test-helper internals |
| `internal/integration_test/fuzzcases/fuzzcases_test.go` | ported / already covered | ported with all 71 binary regressions and concrete oracles |
| `internal/integration_test/libsodium/bench_test.go` | not applicable | tests wazero WASI/OS/CLI/library integration that wago does not implement in core |
| `internal/integration_test/spectest/exception-handling/spec_test.go` | unsupported, fail-closed tested | all 40 pinned generated artifacts are accounted for; valid exception modules reject explicitly as unsupported |
| `internal/integration_test/spectest/extended-const/spec_test.go` | ported / already covered | all 63 pinned artifacts are checked in; 13 valid modules and 60 execution assertions pass, while 37 invalid and 4 malformed binaries reject and 3 WAT-only malformed cases are explicitly accounted |
| `internal/integration_test/spectest/spectest_test.go` | already covered | official pinned spec suites run directly in wago; duplicating wazero harness code adds no independent oracle |
| `internal/integration_test/spectest/tail-call/spec_test.go` | unsupported, fail-closed tested | all 45 pinned generated artifacts are accounted for; valid tail-call modules reject explicitly as unsupported |
| `internal/integration_test/spectest/threads/spec_test.go` | unsupported, fail-closed tested | all 55 pinned generated artifacts are accounted for; valid threads/atomic modules reject explicitly as unsupported |
| `internal/integration_test/spectest/typed-function-references/spec_test.go` | unsupported, fail-closed tested | all 642 pinned generated artifacts, including 558 binaries, are accounted for; proposal-dependent modules reject explicitly as unsupported |
| `internal/integration_test/spectest/v1/spec_test.go` | already covered | official pinned spec suites run directly in wago; duplicating wazero harness code adds no independent oracle |
| `internal/integration_test/spectest/v2/spec_test.go` | ported / already covered | the in-repository pinned Core v2 corpus manifest contains 147 WAST files and now runs automatically through validation and native execution when wast2json is available, with zero feature skips |
| `internal/integration_test/stdlibs/bench_test.go` | not applicable | tests wazero WASI/OS/CLI/library integration that wago does not implement in core |
| `internal/leb128/leb128_alloc_test.go` | ported / already covered | `TestWazeroPortLEB128NoAlloc` requires zero allocations for u32/u64/i32/i64 reader decoding |
| `internal/leb128/leb128_test.go` | ported / already covered | ported in src/core/compiler/wasm/wazero_leb128_port_test.go |
| `internal/logging/logging_test.go` | not applicable | wazero-specific experimental/import adapter API; no corresponding wago surface |
| `internal/moremath/moremath_test.go` | not applicable | wazero implementation-private engine, cache, platform, or test-helper internals |
| `internal/platform/mmap_linux_test.go` | not applicable | wazero implementation-private engine, cache, platform, or test-helper internals |
| `internal/platform/mmap_test.go` | not applicable | wazero implementation-private engine, cache, platform, or test-helper internals |
| `internal/platform/path_test.go` | not applicable | wazero implementation-private engine, cache, platform, or test-helper internals |
| `internal/platform/time_test.go` | not applicable | wazero implementation-private engine, cache, platform, or test-helper internals |
| `internal/sys/fs_test.go` | not applicable | wazero system-context implementation internals |
| `internal/sys/stdio_test.go` | not applicable | wazero system-context implementation internals |
| `internal/sys/sys_test.go` | not applicable | wazero system-context implementation internals |
| `internal/sysfs/adapter_test.go` | not applicable | tests wazero WASI/OS/CLI/library integration that wago does not implement in core |
| `internal/sysfs/bench_test.go` | not applicable | tests wazero WASI/OS/CLI/library integration that wago does not implement in core |
| `internal/sysfs/dir_test.go` | not applicable | tests wazero WASI/OS/CLI/library integration that wago does not implement in core |
| `internal/sysfs/dirfs_test.go` | not applicable | tests wazero WASI/OS/CLI/library integration that wago does not implement in core |
| `internal/sysfs/file_test.go` | not applicable | tests wazero WASI/OS/CLI/library integration that wago does not implement in core |
| `internal/sysfs/futimens_test.go` | not applicable | tests wazero WASI/OS/CLI/library integration that wago does not implement in core |
| `internal/sysfs/oflag_test.go` | not applicable | tests wazero WASI/OS/CLI/library integration that wago does not implement in core |
| `internal/sysfs/open_file_test.go` | not applicable | tests wazero WASI/OS/CLI/library integration that wago does not implement in core |
| `internal/sysfs/poll_test.go` | not applicable | tests wazero WASI/OS/CLI/library integration that wago does not implement in core |
| `internal/sysfs/poll_windows_test.go` | not applicable | tests wazero WASI/OS/CLI/library integration that wago does not implement in core |
| `internal/sysfs/readfs_test.go` | not applicable | tests wazero WASI/OS/CLI/library integration that wago does not implement in core |
| `internal/sysfs/rename_test.go` | not applicable | tests wazero WASI/OS/CLI/library integration that wago does not implement in core |
| `internal/sysfs/sock_test.go` | not applicable | tests wazero WASI/OS/CLI/library integration that wago does not implement in core |
| `internal/sysfs/stat_test.go` | not applicable | tests wazero WASI/OS/CLI/library integration that wago does not implement in core |
| `internal/sysfs/sysfs_test.go` | not applicable | tests wazero WASI/OS/CLI/library integration that wago does not implement in core |
| `internal/sysfs/unlink_test.go` | not applicable | tests wazero WASI/OS/CLI/library integration that wago does not implement in core |
| `internal/testing/binaryencoding/code_test.go` | not applicable | wazero implementation-private engine, cache, platform, or test-helper internals |
| `internal/testing/binaryencoding/element_test.go` | not applicable | wazero implementation-private engine, cache, platform, or test-helper internals |
| `internal/testing/binaryencoding/encoder_test.go` | not applicable | wazero implementation-private engine, cache, platform, or test-helper internals |
| `internal/testing/binaryencoding/export_test.go` | not applicable | wazero implementation-private engine, cache, platform, or test-helper internals |
| `internal/testing/binaryencoding/global_test.go` | not applicable | wazero implementation-private engine, cache, platform, or test-helper internals |
| `internal/testing/binaryencoding/import_test.go` | not applicable | wazero implementation-private engine, cache, platform, or test-helper internals |
| `internal/testing/binaryencoding/names_test.go` | not applicable | wazero implementation-private engine, cache, platform, or test-helper internals |
| `internal/testing/binaryencoding/section_test.go` | not applicable | wazero implementation-private engine, cache, platform, or test-helper internals |
| `internal/testing/binaryencoding/value_test.go` | not applicable | wazero implementation-private engine, cache, platform, or test-helper internals |
| `internal/testing/fs/fs_test.go` | not applicable | wazero implementation-private engine, cache, platform, or test-helper internals |
| `internal/testing/nodiff/nodiff_test.go` | not applicable | wazero implementation-private engine, cache, platform, or test-helper internals |
| `internal/testing/require/require_errno_test.go` | not applicable | wazero implementation-private engine, cache, platform, or test-helper internals |
| `internal/testing/require/require_test.go` | not applicable | wazero implementation-private engine, cache, platform, or test-helper internals |
| `internal/u32/u32_test.go` | not applicable | wazero implementation-private engine, cache, platform, or test-helper internals |
| `internal/u64/u64_test.go` | not applicable | wazero implementation-private engine, cache, platform, or test-helper internals |
| `internal/version/testdata/main_test.go` | not applicable | wazero implementation-private engine, cache, platform, or test-helper internals |
| `internal/wasip1/errno_test.go` | not applicable | tests wazero WASI/OS/CLI/library integration that wago does not implement in core |
| `internal/wasip1/logging/logging_test.go` | not applicable | tests wazero WASI/OS/CLI/library integration that wago does not implement in core |
| `internal/wasm/binary/const_expr_test.go` | already covered | decoder behavior is covered by wago wasm decode/edge/spectest tests and wazero validation ports |
| `internal/wasm/binary/data_test.go` | already covered | decoder behavior is covered by wago wasm decode/edge/spectest tests and wazero validation ports |
| `internal/wasm/binary/decoder_test.go` | already covered | decoder behavior is covered by wago wasm decode/edge/spectest tests and wazero validation ports |
| `internal/wasm/binary/element_test.go` | already covered | decoder behavior is covered by wago wasm decode/edge/spectest tests and wazero validation ports |
| `internal/wasm/binary/function_test.go` | already covered | decoder behavior is covered by wago wasm decode/edge/spectest tests and wazero validation ports |
| `internal/wasm/binary/import_test.go` | already covered | decoder behavior is covered by wago wasm decode/edge/spectest tests and wazero validation ports |
| `internal/wasm/binary/limits_test.go` | already covered | decoder behavior is covered by wago wasm decode/edge/spectest tests and wazero validation ports |
| `internal/wasm/binary/memory_test.go` | already covered | decoder behavior is covered by wago wasm decode/edge/spectest tests and wazero validation ports |
| `internal/wasm/binary/names_test.go` | already covered | decoder behavior is covered by wago wasm decode/edge/spectest tests and wazero validation ports |
| `internal/wasm/binary/section_test.go` | already covered | decoder behavior is covered by wago wasm decode/edge/spectest tests and wazero validation ports |
| `internal/wasm/binary/table_test.go` | already covered | decoder behavior is covered by wago wasm decode/edge/spectest tests and wazero validation ports |
| `internal/wasm/binary/value_test.go` | already covered | decoder behavior is covered by wago wasm decode/edge/spectest tests and wazero validation ports |
| `internal/wasm/counts_test.go` | already covered | module validation/runtime-model analogue covered by wago wasm and runtime tests; wazero store structs are implementation-specific |
| `internal/wasm/func_validation_test.go` | ported / already covered | ported/adapted in src/core/compiler/wasm/wazero_validation_port_test.go |
| `internal/wasm/function_definition_test.go` | already covered | module validation/runtime-model analogue covered by wago wasm and runtime tests; wazero store structs are implementation-specific |
| `internal/wasm/global_test.go` | already covered | module validation/runtime-model analogue covered by wago wasm and runtime tests; wazero store structs are implementation-specific |
| `internal/wasm/gofunc_test.go` | already covered | module validation/runtime-model analogue covered by wago wasm and runtime tests; wazero store structs are implementation-specific |
| `internal/wasm/host_test.go` | already covered | module validation/runtime-model analogue covered by wago wasm and runtime tests; wazero store structs are implementation-specific |
| `internal/wasm/memory_definition_test.go` | already covered | module validation/runtime-model analogue covered by wago wasm and runtime tests; wazero store structs are implementation-specific |
| `internal/wasm/memory_test.go` | already covered | module validation/runtime-model analogue covered by wago wasm and runtime tests; wazero store structs are implementation-specific |
| `internal/wasm/module_instance_lookup_test.go` | already covered | module validation/runtime-model analogue covered by wago wasm and runtime tests; wazero store structs are implementation-specific |
| `internal/wasm/module_instance_test.go` | already covered | module validation/runtime-model analogue covered by wago wasm and runtime tests; wazero store structs are implementation-specific |
| `internal/wasm/module_test.go` | already covered | module validation/runtime-model analogue covered by wago wasm and runtime tests; wazero store structs are implementation-specific |
| `internal/wasm/store_module_list_test.go` | already covered | module validation/runtime-model analogue covered by wago wasm and runtime tests; wazero store structs are implementation-specific |
| `internal/wasm/store_test.go` | already covered | module validation/runtime-model analogue covered by wago wasm and runtime tests; wazero store structs are implementation-specific |
| `internal/wasm/table_test.go` | already covered | module validation/runtime-model analogue covered by wago wasm and runtime tests; wazero store structs are implementation-specific |
| `internal/wasmdebug/debug_test.go` | not applicable | wazero DWARF/debug formatting implementation is not part of wago runtime semantics |
| `internal/wasmdebug/dwarf_test.go` | not applicable | wazero DWARF/debug formatting implementation is not part of wago runtime semantics |
| `runtime_test.go` | ported / already covered | applicable runtime/linking behaviors adapted, including current cross-runtime structural-type regression |
| `sys/error_test.go` | not applicable | tests wazero WASI/OS/CLI/library integration that wago does not implement in core |
| `sys/stat_export_test.go` | not applicable | tests wazero WASI/OS/CLI/library integration that wago does not implement in core |
| `sys/stat_test.go` | not applicable | tests wazero WASI/OS/CLI/library integration that wago does not implement in core |
