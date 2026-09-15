# Wazy instantiation research (2026-09-15)

## Bottom line

Wazy really is fast at fresh-instance setup. On this Apple M4 Max, Wazy v0.3.0 instantiated and closed Wago's 88-byte `fib.wasm` in a median **480.6 ns/op** (range 463.5-506.7, 10 x 1 s), versus about **511 ns/op** for the final uncommitted Wago optimization worktree. Wazy remains roughly **6% lower latency**, despite allocating **1,784 B / 17 allocs** versus Wago's **1,168 B / 3 allocs**. The useful lesson is path length and lifecycle simplicity, not merely allocation count.

On Wazy's substantially larger 37,587-byte TinyGo `case.wasm`, its own public-API benchmark measured a median **2.015 us/op**, 3.2 KB/op, 27 allocs/op; its pinned wazero comparison measured **17.41 us/op**, 139.6 KB/op, 73 allocs/op (Wazy 8.64x faster). After sharing Wago's immutable host thunks and deferring its host dispatcher, the Wago worktree measured the same guest at a median **3.29 us/op**, 2,176 B/op, 13 allocs/op. That Wago/Wazy ratio remains directional because Wago used generic host stubs while Wazy used its WASI/env host modules.

## Source and revisions

- Official repository: [`samyfodil/wazy`](https://github.com/samyfodil/wazy).
- Inspected and benchmarked revision: **`df7d6962b5fb0c3efbb13fd48409e6631c46b428`**, exactly the annotated **`v0.3.0`** tag and current `main` at inspection time.
- Go/toolchain: `go1.26.5 darwin/arm64`; host: Apple M4 Max, Darwin 25.6.0.
- Wago comparison worktree: `/private/tmp/wago-instantiate-latency`, base/head **`2dd978ccda6fd7955683cc926987b9243cd2bb6e` plus uncommitted instantiate optimizations**. The older clean primary checkout at `43e18b07ffebd4887b11daa8e9cc71657c6fd392` measured 632.7 ns/op median, 1,600 B/op, 7 allocs/op on the same fixture.

## What Wazy actually does

The public lifecycle is conventional: `InstantiateModule` builds a per-instance system context, resolves store-local type IDs if required, calls `Store.Instantiate`, attaches optional close ownership, then invokes configured start functions. See [`runtime.go:411-500`](https://github.com/samyfodil/wazy/blob/df7d6962b5fb0c3efbb13fd48409e6631c46b428/runtime.go#L411-L500).

The store then creates a fresh `ModuleInstance`, exact-sized slices for tables/memories/globals/tags, a fresh native `moduleEngine`, resolves imports, builds tables/globals/tags/memory, applies elements/data, finalizes the native VM context, and optionally runs the Wasm start function. There is **no whole-instance pool or reset-to-snapshot shortcut**. See [`internal/wasm/store.go:419-523`](https://github.com/samyfodil/wazy/blob/df7d6962b5fb0c3efbb13fd48409e6631c46b428/internal/wasm/store.go#L419-L523).

The native engine reuses immutable compiled artifacts. Per instance, `NewModuleEngine` does a compiled-module cache lookup, allocates only the imported-function slice and a single aligned opaque VM-context byte block, then `DoneInstantiation` writes the already-computed offsets/pointers into that block. See [`internal/engine/native/engine.go:976-1006`](https://github.com/samyfodil/wazy/blob/df7d6962b5fb0c3efbb13fd48409e6631c46b428/internal/engine/native/engine.go#L976-L1006) and [`internal/engine/native/module_engine.go:110-191`](https://github.com/samyfodil/wazy/blob/df7d6962b5fb0c3efbb13fd48409e6631c46b428/internal/engine/native/module_engine.go#L110-L191).

Several pieces of work are deliberately moved to compile/decode time and shared read-only: compiled code and VM-context offsets, store-local type IDs, imports grouped by module, the ordered export slice, memory definitions, and dense funcref slots. The dense funcref table avoids an O(n^2) scan while preserving stable addresses required by bare `uintptr` funcrefs. See [`internal/engine/native/module_engine.go:382-420`](https://github.com/samyfodil/wazy/blob/df7d6962b5fb0c3efbb13fd48409e6631c46b428/internal/engine/native/module_engine.go#L382-L420).

Wazy still registers every instance, including anonymous ones, in a doubly-linked store list under a mutex; close removes it under the same mutex. Anonymous instances skip only the name map. See [`internal/wasm/store_module_list.go:8-89`](https://github.com/samyfodil/wazy/blob/df7d6962b5fb0c3efbb13fd48409e6631c46b428/internal/wasm/store_module_list.go#L8-L89).

Close is intentionally simple: one embedded atomic word claims closure and stores the exit code; the owner removes the instance from the store, closes system resources, and recycles eligible memory. There is no completion channel/state object on the ordinary path. See [`internal/wasm/module_instance.go:114-240`](https://github.com/samyfodil/wazy/blob/df7d6962b5fb0c3efbb13fd48409e6631c46b428/internal/wasm/module_instance.go#L114-L240).

## Caching, pooling, and ownership trade-offs

- **Compiled artifacts:** a runtime/engine owns a map of compiled modules with reference counts; `CompiledModule.Close` releases one reference, while instances may continue running. This separates immutable code lifetime from instance lifetime. See [`internal/wasm/engine.go:18-55`](https://github.com/samyfodil/wazy/blob/df7d6962b5fb0c3efbb13fd48409e6631c46b428/internal/wasm/engine.go#L18-L55).
- **Linear memory only, not instances:** ordinary non-shared, non-custom-allocator buffers go through process-wide exact-capacity `sync.Pool` buckets. Acquire clears only the previously exposed prefix; untouched reserve pages remain known-zero. Shared and custom-allocator memories are excluded. See [`internal/wasm/memory_pool.go:23-109`](https://github.com/samyfodil/wazy/blob/df7d6962b5fb0c3efbb13fd48409e6631c46b428/internal/wasm/memory_pool.go#L23-L109).
- **Safe cross-instance ownership:** an owner-closed bit plus importer refcount delays recycling until the owner and every importing instance have closed; buffer claiming happens under the memory mutex and clears the stale owner's references before pooling. This is correctness/security-critical because a premature pool return would alias guest memory across tenants. See [`internal/wasm/module_instance.go:178-230`](https://github.com/samyfodil/wazy/blob/df7d6962b5fb0c3efbb13fd48409e6631c46b428/internal/wasm/module_instance.go#L178-L230).
- **Learned capacity:** each decoded memory retains an atomic high-water page count. A later instance begins with the largest capacity a prior instance needed, avoiding repeated grow/reallocate/copy and making exact-capacity pool buckets hit. The stated trade-off is that one unusually large instance can make later instances reserve more virtual memory, bounded by the configured maximum; untouched pages remain lazily backed. See [`internal/wasm/memory.go:149-228`](https://github.com/samyfodil/wazy/blob/df7d6962b5fb0c3efbb13fd48409e6631c46b428/internal/wasm/memory.go#L149-L228).
- **Pool cost is workload-dependent:** on the tiny Wago fib fixture, disabling Wazy's memory pool produced the same 1,784 B / 17 allocs and a 463.1 ns median versus 480.6 ns with it. That separately-run ~3.6% difference is too small/noisy to call a win, but it confirms that Wazy's tiny-module advantage is not its memory pool. Wazy's own optimization notes likewise record a real-consumer case where an exact-capacity mismatch caused a 0% hit rate until high-water sizing fixed it.

## Direct benchmark evidence

Wazy ships a directly relevant benchmark: compile once, then repeatedly call public `InstantiateModule` and `Close`, anonymous name, `_start` skipped. Its two runtime arms use identical guest bytes. See [`benchmarks/vs-wazero/bench_test.go:72-84`](https://github.com/samyfodil/wazy/blob/df7d6962b5fb0c3efbb13fd48409e6631c46b428/benchmarks/vs-wazero/bench_test.go#L72-L84), [`wazy_test.go:126-153`](https://github.com/samyfodil/wazy/blob/df7d6962b5fb0c3efbb13fd48409e6631c46b428/benchmarks/vs-wazero/wazy_test.go#L126-L153), and [`wazero_test.go:119-142`](https://github.com/samyfodil/wazy/blob/df7d6962b5fb0c3efbb13fd48409e6631c46b428/benchmarks/vs-wazero/wazero_test.go#L119-L142).

Commands run:

```sh
# Wazy's official case.wasm benchmark, both Wazy and pinned wazero.
cd /private/tmp/wazy-research.gFQJAo/benchmarks/vs-wazero
GOCACHE=/private/tmp/wazy-research-gocache \
GOMODCACHE=/private/tmp/wazy-research-gomodcache \
go test -run '^$' -bench '^BenchmarkInstantiate$' \
  -benchmem -benchtime=1s -count=10 .

# Exact Wago fib.wasm through Wazy v0.3.0 public APIs.
cd /private/tmp/wazy-matched-instantiate
GOCACHE=/private/tmp/wazy-match-gocache \
GOMODCACHE=/private/tmp/wazy-research-gomodcache \
go test -run '^$' -bench '^BenchmarkInstantiateWazyWagoFib$' \
  -benchmem -benchtime=1s -count=10 .

# Current Wago optimization worktree, its existing public lifecycle benchmark.
cd /private/tmp/wago-instantiate-latency/bench/suite
GOCACHE=/private/tmp/wago-wazy-branch-gocache \
go test -run '^$' -bench '^BenchmarkInstantiate_wago$' \
  -benchmem -benchtime=1s -count=10 .

# Same Wazy case.wasm through the current Wago worktree.
GOCACHE=/private/tmp/wago-wazy-branch-gocache \
go test -run '^$' -bench '^BenchmarkInstantiateWazyCaseWago$' \
  -benchmem -benchtime=1s -count=10 .
```

| Guest / runtime | Median | 10-sample range | B/op | allocs/op |
|---|---:|---:|---:|---:|
| Wago fib (88 B), Wazy v0.3.0 | 480.6 ns | 463.5-506.7 ns | 1,784 | 17 |
| Wago fib (88 B), Wago optimization worktree | ~511 ns | 497.7-517.5 ns | 1,168 | 3 |
| Wazy case (37,587 B), Wazy v0.3.0 | 2.015 us | 1.855-2.671 us | ~3,245 | 27 |
| Wazy case, current Wago optimization worktree | 3.29 us | 3.236-3.423 us | 2,176 | 13 |
| Wazy case, pinned wazero | 17.41 us | 16.96-18.98 us | ~139,648 | 73 |

The fib Wazy and Wago rows are the closest lifecycle match: same bytes, compile once outside timing, fresh instance plus close each operation, no start invocation, public APIs. They were run in separate benchmark binaries, so the percentage should be confirmed once more in the final in-repo three-runtime harness before publishing it.

The resulting Wago optimization has a larger effect on imported modules in Wago's existing matched-revision benchmarks:

| Wago benchmark | `2dd978cc` median | Optimized median | Change | B/op | allocs/op |
|---|---:|---:|---:|---:|---:|
| `BenchmarkInstantiateHostFuncDirect` | 4.515 us | 1.184 us | -73.8% | 2,952 -> 1,960 | 23 -> 10 |
| `BenchmarkInstantiateTableHostFuncThunk` | 4.740 us | 1.342 us | -71.7% | 2,952 -> 1,960 | 23 -> 10 |
| `BenchmarkRuntimeInstantiateSmallScalar` | 1.213 us | 1.185 us | -2.3% | 1,720 -> 1,512 | 10 -> 8 |

## Actionable ideas for Wago

1. **Optimize elapsed instructions, not allocation count.** Wago is already at 3 allocations versus Wazy's 17 yet is ~9% slower on tiny fib. Treat further allocation reductions as secondary unless a profile names GC/allocator cost. The primary target should be the ordinary unmanaged `Instantiate+Close` control-flow length.
2. **Preserve Wago's richer semantics behind cold branches.** Wazy's embedded atomic close flag is much simpler than Wago's reentrant close, wait-for-completion, hooks, managed ownership, interruption, reference lifetime, and terminal-finalizer machinery. Do not delete those guarantees. Instead, keep a provably equivalent fast path for the overwhelmingly common case: unmanaged, no hooks/plugins, no active invocation, no imported lifetime edges. Ideally it should avoid creating close completion state/channels and skip generic finalizer orchestration.
3. **Finish moving immutable setup into `Compiled`.** Wazy's per-instance native engine mostly allocates a flat VM-context block and fills precomputed offsets. The current Wago worktree's shared host-thunk work is aligned with this. Continue auditing instance construction for signature/layout scans, maps, closures, or thunk generation that depend only on the compiled module and sync mode.
4. **Use size-proportional regressions as the oracle.** Tiny fib leaves only tens of nanoseconds between current Wago and Wazy, while the 37 KB TinyGo guest still leaves ~1.28 us. That scaling points toward per-import/per-function/per-export construction and teardown, not just a fixed close-state tax. Keep both fixtures: tiny fib for fixed overhead and Wazy case for module-shape overhead.
5. **Borrow Wazy's high-water idea only where Wago still reallocates.** Wago already has stronger arena/linear-memory reuse and fewer bytes/op on both compared fixtures. Do not replace it with Wazy's global exact-capacity `sync.Pool`. The transferable idea is the per-compiled-memory learned capacity after guest growth, but only if a Wago profile/counter proves repeated grow-and-copy on fresh instances.
6. **Do not pool whole `Instance` objects yet.** Wazy demonstrates that sub-microsecond instantiate does not require it. Whole-instance pooling would greatly complicate stale handles, imported ownership, reference tokens, plugins, and concurrency. Exhaust compile-time precomputation and the semantically-simple fast path first.

## Verification caveats

- Benchmarks were run locally, not under CPU affinity; ranges are included instead of claiming formal significance.
- Wazy's own `case.wasm` Wago arm uses Wago host stubs while Wazy's arm has its own WASI/env instances. Compilation is outside the timed region, but import-resolution objects differ.
- The Wago worktree contains uncommitted changes and can move; its measurements are a current checkpoint, not a release claim.
