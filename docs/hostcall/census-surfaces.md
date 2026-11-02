**Census verdict:** at Wago `90f2b57da`, there is no bundled production WASI implementation. WASI Preview 1 used by the corpus is an external, precisely pinned provider. Wago itself supplies three conditional `wago:abi` functions. None of these complete bindings currently satisfies the reference design’s DIRECT admission contract.

I read the full brief and Slipstream design, including safety contract v2. All inspection was read-only. No children, edits, builds, tests, benchmarks, timings, branches, PRs or CI actions were performed. The excluded GC campaign was untouched.

**Inventory boundaries and counts**

| Surface | Functions | Current route | Admission from this audit |
|---|---:|---|---|
| Wago production `wago:abi` | 3 | Internal slot callbacks | 3 BOUNDARY |
| External pinned WASI Preview 1 | 46 | `CallerHostCallFunc` plus provider state wrapper | 46 BOUNDARY |
| Corpus Emscripten `env` adapter | 38 | Typed callbacks and HostCall views | Test-only; proposed 31 DIRECT, 7 SPECIALIZED |
| Corpus a-Shell extensions | 4 | Typed callbacks and Caller views | Test-only; proposed 2 DIRECT, 2 SPECIALIZED |
| Corpus PHP socket extensions | 5 | Caller views returning denial | Test-only; proposed 5 DIRECT |
| Corpus `micropython_wasm` | 2 | Caller views returning constants | Test-only; proposed 2 DIRECT |
| Conformance `spectest` | 7 | HostCall no-ops | Test-only; proposed 7 DIRECT |

Thus the production-plus-pinned-Preview-1 census is **49 functions: SPECIALIZED 0 / DIRECT 0 / BOUNDARY 49 admitted today**. The 56 explicitly enumerated compatibility/conformance functions are separate test surfaces, not production built-ins.

“Proposed” classifications below identify semantic opportunities, **not completed safety proofs**.

Source evidence:

- `git ls-tree -r 90f2b57da`: no WASI implementation or bundled host-provider subtree.
- `go.mod`: only `golang.org/x/sys`; no production WASI dependency.
- `bench/go.mod:10`: `github.com/wago-org/wasi v0.3.1-0.20260916034125-6a6684d2ecd2`.
- `bench/suite/command_runtime_unix_test.go:25`: constructs `p1.Imports`.
- Windows counterpart rejects non-core command runtimes.
- `providers.schema.json:310`: WASI is a provider-catalog **example**, not an installed implementation.
- `src/wago/reserved_modules_test.go:7`: reserved namespace names do not establish implementations.
- `src/wago/plugin_plan.go:811`: installs `wago:abi` only when needed.

The cached external provider was inspected at:

`/home/dev/go/pkg/mod/github.com/wago-org/wasi@v0.3.1-0.20260916034125-6a6684d2ecd2`

Its catalog also offers Preview 2. That is an external component-provider surface, not additional bundled Wago core imports or Preview 1 functions; it is excluded from the 49 count.

**Current call path and code-evident costs**

All Wago anchors below refer to `git show 90f2b57da:<path>`, regardless of current checkout contents.

| Stage | Anchor | Code-evident work |
|---|---|---|
| Registration | `src/wago/imports.go:77`, `HostFunc` | Signature-form inspection, declaration storage and duplicate checks; registration work, not per-call cost |
| Binding | `src/wago/hostcall.go:1867`, `bindSyncHostImport`; `:2110`, `buildSyncHosts` | Resolves representation, signature, scalar/reference class and exact descriptor once |
| Native call | `src/core/compiler/backend/railshot/amd64/call.go:1272`, `callHostSync` | Signature-sized argument transport into the control frame and result transport back; surrounding spill/root obligations depend on compiled route |
| Generic park | `src/core/runtime/hostcall_amd64.go:12–94` | Saves seven 64-bit register/SP fields, publishes control pointer and pending trap, unwinds to Go; later restores/resumes |
| Generic marshalling | `src/core/runtime/engine_host.go:304`, `callWithHostLoop` | For P parameters/R results: P control-to-buffer reads, R buffer clears, R buffer-to-control writes |
| Typed scalar portal | Same function, scalar branch | Up to two argument-word reads and one result-word write; avoids generic slice-buffer loop when handled |
| Borrowed view | `src/wago/hostcall_compact_slots.go:17`; `host_execution.go:84` | Compact slice descriptors and direct borrowed views; no mandatory payload copy |
| General dispatch | `src/wago/host_execution.go:270`, `dispatch`; `:350`, `dispatchActive` | Resolves actual owner, roots GC arguments as applicable, suspends collector/native leases, invokes callback and restores ownership/context |
| Foreign owner | `host_execution.go:329`, `dispatchForeignControl` | Additional saved control-prefix copy: 56 bytes on amd64; architecture-specific saved prefix on arm64 |
| Callback representation | `src/wago/hostcall.go:1794–1865` | Typed function call or type-switch to HostCall/Caller/slot callback; no general `reflect.Call` |
| Alternative crossing | `src/core/runtime/inline_host.go:15` | Existing bounded bridge can replace park/return/resume with a live Go frame, while retaining dispatcher and lease semantics |

**Do not claim reflection removal or universal slice-copy elimination.** Both are already absent from eligible existing lanes. “Direct typed lane” in Wago means specialized callback transport; it does **not** certify the callback as reference-design DIRECT.

The pinned Preview 1 bindings have these shape counts:

| P → R | Count |
|---|---:|
| 0 → 1 | 1 |
| 1 → 0 | 1 |
| 1 → 1 | 4 |
| 2 → 1 | 14 |
| 3 → 1 | 8 |
| 4 → 1 | 6 |
| 5 → 1 | 6 |
| 6 → 1 | 3 |
| 7 → 1 | 2 |
| 9 → 1 | 1 |
| **Total** | **46** |

For example, generic `fd_write` transport is P=4/R=1, whereas `path_open` is P=9/R=1. These counts describe the generic loop, not an assertion that every invocation uses it.

**Complete Preview 1 inventory**

External-provider anchors use `internal/core/`. Every row currently follows:

`bindings → callback() → CallerHostCallFunc → stateFor → handler`

- `core.go:245–248`: Caller-to-slot adapter.
- `core.go:339–354`: state wrapper, deferred unlock, shallow plugin copy and handler call.
- `fs.go:170–200`: guard mutex, caller resolution, state lookup, possible `makeFS(true)` and state-map insertion, then state mutex.

That wrapper can block and initialize resources even when the handler merely returns a constant. It is the decisive common BOUNDARY prerequisite.

| Function | Body anchor | Semantic effects / possible later class |
|---|---|---|
| `fd_write` | `core.go:611` | Rights, iovec allocation, arbitrary writer callbacks, partial I/O — BOUNDARY |
| `fd_read` | `core.go:663` | Rights, iovec allocation, arbitrary reader callbacks, memory mutation — BOUNDARY |
| `fd_close` | `core.go:715` | Descriptor/resource lifetime — BOUNDARY |
| `fd_seek` | `core.go:736` | Descriptor seek — BOUNDARY |
| `fd_fdstat_get` | `core.go:766` | Memory output; file-backed branch calls `Stat`; stream-only branch could be SPECIALIZED |
| `fd_prestat_get` | `core.go:795` | Descriptor/preopen validation, fixed eight-byte output — SPECIALIZED prospect |
| `fd_prestat_dir_name` | `core.go:812` | Ordered descriptor/capacity checks, name copy — bounded SPECIALIZED prospect |
| `proc_exit` | `core.go:832` | `HostExit` panic/unwind — DIRECT prospect only with equivalent exit protocol |
| `args_sizes_get` | `core.go:836` | String-length scan and ordered output writes — cached SPECIALIZED prospect |
| `args_get` | `core.go:840` | Pointer/string loop, incremental writes — bounded SPECIALIZED prospect |
| `environ_sizes_get` | `core.go:844` | String-length scan and ordered output writes — cached SPECIALIZED prospect |
| `environ_get` | `core.go:848` | Pointer/string loop, incremental writes — bounded SPECIALIZED prospect |
| `clock_time_get` | `core.go:883` | Configurable clock callback, memory output — BOUNDARY |
| `clock_res_get` | `core.go:900` | Configurable clock callback, memory output — BOUNDARY |
| `random_get` | `core.go:917` | Configurable reader/entropy, partial writes — BOUNDARY |
| `sched_yield` | `fs.go:1231` | Exact current body returns success — DIRECT prospect |
| `fd_advise` | `fs.go:390` | Descriptor/right/advice validation only — DIRECT prospect |
| `fd_allocate` | `fs.go:401` | File allocation/size operation — BOUNDARY |
| `fd_datasync` | `fs.go:419` | File synchronization — BOUNDARY |
| `fd_sync` | `fs.go:422` | File synchronization — BOUNDARY |
| `fd_fdstat_set_flags` | `fs.go:434` | Descriptor flags/platform handling — BOUNDARY |
| `fd_fdstat_set_rights` | `fs.go:466` | Rights-subset validation and mutation — DIRECT prospect |
| `fd_filestat_get` | `fs.go:478` | File metadata and memory output — BOUNDARY |
| `fd_filestat_set_size` | `fs.go:497` | File truncation — BOUNDARY |
| `fd_filestat_set_times` | `fs.go:549` | Timestamp/platform operation — BOUNDARY |
| `fd_pread` | `fs.go:578` | Positioned I/O/iovecs — BOUNDARY |
| `fd_pwrite` | `fs.go:579` | Positioned I/O/iovecs — BOUNDARY |
| `fd_readdir` | `fs.go:658` | Directory iterator, allocation, copying and lifetime — BOUNDARY |
| `fd_renumber` | `fs.go:775` | Descriptor replacement/resource lifetime — BOUNDARY |
| `fd_tell` | `fs.go:803` | Descriptor seek/query — BOUNDARY |
| `path_create_directory` | `fs.go:822` | Capability-relative filesystem mutation — BOUNDARY |
| `path_filestat_get` | `fs.go:846` | Capability-relative metadata — BOUNDARY |
| `path_filestat_set_times` | `fs.go:877` | Capability-relative timestamp mutation — BOUNDARY |
| `path_link` | `fs.go:945` | Filesystem linking — BOUNDARY |
| `path_open` | `fs.go:995` | Path validation, rights, resource creation — BOUNDARY |
| `path_readlink` | `fs.go:1086` | Filesystem read and copying — BOUNDARY |
| `path_remove_directory` | `fs.go:1129` | Filesystem mutation — BOUNDARY |
| `path_rename` | `fs.go:1166` | Filesystem mutation — BOUNDARY |
| `path_symlink` | `fs.go:1201` | Filesystem mutation — BOUNDARY |
| `path_unlink_file` | `fs.go:1135` | Filesystem mutation — BOUNDARY |
| `poll_oneoff` | `poll.go:33` | Subscription processing, clocks/readiness/wait — BOUNDARY |
| `proc_raise` | `fs.go:1232` | Exact current body returns NOTSUP — DIRECT prospect |
| `sock_accept` | `fs.go:1234` | Exact current body returns NOTSUP — DIRECT prospect |
| `sock_recv` | `fs.go:1235` | Descriptor validation, then NOTSOCK — DIRECT prospect |
| `sock_send` | `fs.go:1236` | Descriptor validation, then NOTSOCK — DIRECT prospect |
| `sock_shutdown` | `fs.go:1237` | Descriptor validation, then NOTSOCK — DIRECT prospect |

Additional code-evident costs:

- `fs.go:353`, `iovecs`: creates a `[][]byte` metadata slice, validates each extent and appends payload views. **It does not copy payload bytes.**
- `core.go:853`, `writeCounts`: scans every string; writes count before attempting size output.
- `core.go:866`, `writeStrings`: writes each pointer before checking/copying its string. Earlier writes can remain on later failure.
- `core.go:611`, `fdWrite`: performs writer calls before validating/storing `nwritten`.

Consequently, a fast path that prevalidates all outputs and returns early can change existing observable writes or external effects. Differential tests must compare these states; normative changes require a separate explicit baseline correction.

**Other Wago production functions**

`src/wago/instruction_runtime.go:153–196` supplies the entire fixed `wago:abi` catalog:

| Function | Signature | Effects | Class |
|---|---|---|---|
| `result.get` | `(i32,i32)→i32` | Validates pack/index; wide value allocates a cloned value and new handle | BOUNDARY |
| `result.drop` | `(i32)→()` | Validates and deletes Go-map pack entry; error formatting/panic | BOUNDARY |
| `value.drop` | `(i32)→()` | Validates and deletes Go-map value entry; error formatting/panic | BOUNDARY |

A narrow-value success branch of `result.get` is a possible later SPECIALIZED case, but accessing these Go maps directly from guest machine code is **not admitted**. The arbitrary custom-instruction catalog is plugin-defined, not a finite bundled function catalog; its default is BOUNDARY.

**Compatibility/conformance inventory**

These are explicitly test-only definitions. Their proposed classes apply solely to their exact implementations, never to matching user-supplied names.

`bench/suite/command_emscripten_test.go:19–103`, namespace `env`, **38 functions**:

- **DIRECT prospects, 31:** `emscripten_resize_heap`, `setTempRet0`, `__sys_open`, `__sys_fcntl64`, `__sys_ioctl`, `__sys_chmod`, `__sys_umask`, `__sys_fchmod`, `__sys_lstat64`, `__sys_stat64`, `__sys_rename`, `__sys_readlink`, `__sys_fchown32`, `__sys_chown32`, `__clock_gettime`, `__sys_unlink`, `emscripten_fd_seek`, `splice`, `__sys_fstatat64`, `__sys_openat`, `__sys_prlimit64`, `__sys_fadvise64_64`, `__sys_fchdir`, `__sys_chdir`, `emscripten_get_heap_max`, `__sys_ugetrlimit`, `__sys_getdents64`, `time`, `clock`, `difftime`, `strftime`.
- **SPECIALIZED prospects, 7:** `exit`, `abort`, `__assert_fail` require exact exit/trap adapters; `emscripten_memcpy_big` requires a bounded overlap-safe copy and pre-effect fallback; `__sys_fstat64`, `localtime_r`, `gettimeofday` require checked memory adapters. Trap paths that currently format Go errors retain boundary fallback.

`bench/suite/command_runtime_unix_test.go`:

- `wasi_snapshot_preview1.ashell_getcwd`, `ashell_getenv`: SPECIALIZED prospects; ordered checked memory writes.
- `ashell_chdir`, `ashell_system`: DIRECT denial prospects.
- `sock_open`, `sock_bind`, `sock_connect`, `sock_listen`, `sock_setsockopt`: five DIRECT denial prospects, installed only for the PHP corpus adapter.
- `micropython_wasm.host_result_cap`, `host_call`: two DIRECT constant-result prospects.

`src/wago/spectest_exec_test.go:1691`:

- `spectest.print`, `print_i32`, `print_i64`, `print_f32`, `print_f64`, `print_i32_f32`, `print_f64_f64`: seven exact no-op DIRECT prospects.
- Its four globals, memory and two tables are not functions.

These fixtures provide useful differential controls, but specializing them does not constitute production WASI specialization.

**Admission decisions and NOT VIABLE reasons**

1. **Name-based WASI lowering: NOT VIABLE.** Wago permits ordinary providers under those names. Eligibility must authenticate the resolved implementation, exact signature, gate and owner. Existing type/transport tags do not authenticate semantic effects.
2. **Calling arbitrary Go callbacks on the guest stack: NOT VIABLE.** Small numeric signatures can allocate, block, panic or reenter. Retain existing Go-stack bridges.
3. **Bypassing WASI `stateFor`: NOT VIABLE on the present provider contract.** It changes locking, lazy state creation, closed-state errors, caller authorization and possibly first-call filesystem effects.
4. **Treating constant WASI bodies as DIRECT today: NOT VIABLE.** Classify the complete binding, including state wrapper and plugin gate.
5. **Deferred events replacing synchronous imports: NOT VIABLE.** `I32HostEvent` explicitly delivers after native invocation; it has different ordering and failure semantics.
6. **Public eligibility API: design finding only.** The external provider currently cannot silently mint trusted core lowering metadata. An interoperable certification mechanism needs a deliberate design; a callback pointer or catalog digest alone is insufficient.
7. **Bounded copies: feasible design candidates.** Preserve current memory descriptor, aliasing and incremental-write behavior; guard before effects, retain original arguments, fall back once. Long copies remain BOUNDARY without a polling/progress proof.
8. **Typed transport improvements: feasible while remaining BOUNDARY.** A focused adapter may remove dispatch/marshalling while retaining all state, ownership and Go-stack transitions. This does not qualify as eliminating the semantic boundary.

**Sibling cross-check**

- Slipstream source at `bd640ddf0`, `src/wasi.goose:51`: `proc_raise` invokes `wasi_raise`; `sched_yield` issues syscall 24. Wago’s pinned external bodies instead return NOTSUP/success. **Neither sibling classification transfers.**
- Slipstream’s vector route is different from this provider’s `[][]byte` construction and Go `io.Reader`/`io.Writer` callbacks.
- Starshine was inspected at `b28144a7c50f1dc360825f7f68220022101b9921`.
  - `src/internal/xfs/fs_wasm.mbt`: byte-reader/append names describe Starshine’s imported ABI, not a Wago built-in implementation.
  - `tools/wago-host/main.go:108,138`: bulk request allocates pending response storage; response copies and consumes it.
  - `:164`: legacy filesystem imports are explicit traps in that runner.
  - `:227–230`: bulk CLI and time callbacks belong to Starshine’s runner, not Wago’s bundled surface.

No sibling evidence establishes a Wago bug or passed admission proof.

**Required correctness gates for later implementation**

No gates were executed in this research task.

For each candidate, require forced-generic versus forced-fast differential execution with **route-hit evidence**, comparing results, raw slots, traps/exits, memory and provider state. Cover exact-identity rejection, wrong signature, custom-provider substitution, direct/reference/table/tail/reexport calls, foreign owners, closure/revocation, reentry, grow relocation, shared memory, poisoned scalar high bits, aliases, zero/end-boundary extents, overflow and combined validation failures. Keep Wago’s actual slot ABI; do not transplant Slipstream’s 16-byte carrier/result-span machinery.

Focused existing suites include:

- `src/wago`: host-call API, capacity, hardening, grow, cross-instance dispatch, exact funcref ownership, callback lifetime, GC suspension and reentry suites.
- `src/core/runtime`: hostcall/layout/architecture tests.
- `bench/suite`: `TestImportLifecycleIndependentCommands`, `TestImportLifecycleSetupFailure`, corpus/catalog and relevant command tests.
- External provider: `internal/core`, `p1`, `register`; particularly state-lock isolation, runtime argv, preopen lifecycle, rights/close/renumber, partial I/O and poll cancellation tests. These are **additional provider gates**, not packages included automatically by Wago’s root `./...`.

Pinned repository package gates are specified exactly in `.just/test.just:13–21`:

```sh
go build ./...
go build -tags wago_runtime ./cli/...
go test -count=1 ./...
go test -tags=wago_regalloccheck -count=1 ./...
go test -count=1 -tags wago_runtime ./cli/...
go test -count=1 -tags wago_regalloccheck,wago_runtime ./cli/...
```

Also run the checked installer module and checked bench module with the recipe’s exact exclusions. Local rules additionally require **wagodebug** coverage; CI’s unit recipe does not supply that third mode automatically.

The exact CI race gate, `.github/workflows/ci.yml:303`, is:

```sh
go test -race -count=1 ./src/wago ./src/core/runtime ./tests/integration/runtimeconcurrency
```

CI further requires the applicable six OS/architecture platform cells, guard-page tests, register-allocation code identity, scalar diagnostics, profiling tests, concurrency, corpus/application shards, Core v1/v2/v3 and SIMD conformance, fuzzing and TinyGo runtime/compiler coverage. Preserve the planner’s expected-job policy. **Do not execute the workflow’s benchmark command under this correctness-only authorization.**

**Research head:** Wago `90f2b57dafa7` (requested abbreviated base `90f2b57da`). No candidate or report commit was created because this assignment explicitly required read-only work.
