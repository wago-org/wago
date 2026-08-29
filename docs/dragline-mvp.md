# Dragline MVP

Dragline is Wago's independent optimizing compiler pipeline. The MVP proves the
full product boundary: explicit engine selection, strict rejection without
fallback, a compact SSA IR, deterministic register allocation, native code
emission, runtime execution, artifact persistence, and cache isolation.

## Using it

From Go, select Dragline on an immutable runtime configuration:

```go
cfg := wago.NewRuntimeConfig().
    WithCompiler(wago.CompilerDragline).
    WithTarget(wago.TargetCompatibility)
compiled, err := cfg.Compile(wasmBytes)
```

From the CLI, use any of these equivalent forms:

```sh
wago config --experimental
wago run --backend dragline module.wasm
wago run --dragline module.wasm
wago build --dragline module.wasm
wago run --dragline --target native module.wasm
wago run --dragline --objective speed module.wasm
wago run --dragline --compiler-fallback railshot module.wasm
```

Enable “Dragline compiler” in the experimental preview before selecting it from
the CLI. The non-interactive equivalent is
`wago config --enable dragline --experimental`. Railshot remains the default;
`--railshot` selects it explicitly. Conflicting backend flags are usage errors.
Dragline remains strict unless `--compiler-fallback railshot` is supplied; that
explicit policy retries the whole original module only after a typed unsupported
feature or recoverable compiler-resource-limit error and records Railshot as the
artifact's actual compiler. Resource-limit errors report the bounded structure,
required capacity, and configured limit; without explicit fallback they remain
strict Dragline failures.

## Supported subset

Dragline compiles byte-backed scalar functions on `amd64` and `arm64`. Its
currently admitted subset includes:

- `i32` and `i64` parameters, locals, globals, constants, and at most one result;
- scalar parameter lists up to the structured-function local limit; the private
  ABI uses eight argument registers plus a canonical argument vector;
- `local.get`, `local.set`, `local.tee`, `global.get`, `global.set`, `drop`, and
  `select`;
- integer arithmetic, division/remainder, bitwise, shifts/rotates, bit counts,
  and `eqz`;
- scalar `f32` and `f64` constants, arithmetic, rounding, square root, minimum,
  maximum, and the MVP numeric conversions exercised by the ISA corpus;
- scalar `i32`, `i64`, `f32`, and `f64` memory loads/stores with explicit bounds
  checks, plus proof-bounded masked-address fast paths;
- same-module direct calls, function imports, and single-table indirect calls,
  including bounds, null, signature, host-return, and cross-instance traps; and
- empty, scalar, or type-indexed multi-value `block`, `loop`, and `if` control
  with `else`, `br`, `br_if`, and `br_table`; and
- source-ordered multi-result functions and calls, with four private result
  GPRs plus a caller-owned overflow vector.

Modules with tags, multiple memories/tables, reference types, SIMD instructions
outside the separately documented bounded corpus subset, or instructions
outside the bounded scalar subset return a
`*wago.DraglineUnsupportedError`.
Dragline never retries such a module with Railshot.

The differential ISA gate admits 180 exports in 17 generated modules. It adds
every MVP scalar integer and floating comparison, every MVP scalar conversion,
`f32` and narrow signed/unsigned memory operations, narrow stores,
`memory.size`, and `memory.grow` to the original arithmetic, control, call,
variable, and full-width memory rows, plus nine post-MVP `memory.copy` and
`memory.fill` rows and all five scalar sign-extension instructions.
`TestDraglineMVPISAInventory` fixes the
module/export counts and structural compositions so the claim cannot drift
silently. The full manifest trip counts match Railshot on ARM64; AMD64 lowering
and execution tests pass under Rosetta. Bulk memory remains post-MVP even though
the two calibrated operations are admitted. Reference types remain outside this
bounded subset. Core SIMD is post-MVP and has a separate exact corpus gate; it
does not enlarge the 180-row Wasm 1.0 performance claim.

## Implementation boundary

`src/core/compiler` owns the engine router and stable runtime ABI revision.
Railshot and Dragline are siblings below `src/core/compiler/backend`; CI policy
tests prevent either backend from importing the other. Dragline lowers directly
to dense RailSSA and RailMach tables. Both compatibility and native modes may
consume the complete RailMach product after target selection, three schedule
candidates, schedule-aware allocation, late SSA exit, post-RA planning, and
ABI/frame analysis. A source-derived opcode profitability policy retains the
established scalar emitter for loop shapes where it remains stronger;
compatibility still forbids native-only feature assumptions. Both paths emit
the existing host-wrapper ABI plus a private scalar register entry.

Native targets record a canonical host CPU model and a stable tuning family in
their target fingerprint. For example, an Apple M4 Max is identified as
`apple-m4-max` and tuned as the `apple-m4` family. Unknown CPUs retain their
canonical model while using the architecture-generic tuning family, so cache
identity stays exact without claiming unmeasured CPU-family costs.

The compiler engine is recorded in `Compiled`, serialized in the existing
artifact feature word, and included in automatic artifact-cache keys. Old and
zero-value metadata continues to identify Railshot.

The shared compiler boundary also defines a stable target fingerprint, a
versioned backend-neutral profile keyed to original Wasm function indexes and
instruction offsets, and a strict single-function replay format. Opt-in
Dragline metrics attribute lowering and emission time, IR/native/frame sizes,
relocations, tracked peak-live compiler storage, production semantic graph
size, distinct RailSSA/RailMach instruction counts, proved bounds-check
elisions, and native RailMach finalization with its winning schedule, exact
candidate/combination/live-segment counts, selected immediate folds, copy and
dependency counts, spill debt, ABI class, and clobbers plus discharged semantic obligations and
the count of verified post-RA rewrites actually emitted to each function.
Every native bounds certificate consumed by the emitter is revalidated through
a bounded demand-proof query and counted. When a post-RA rewrite is realized,
metrics also re-emit a diagnostic baseline with only those rewrites disabled
and report the exact signed byte saving; positive values are smaller final code
and negative values are growth. This diagnostic pass runs after `emit_nanos` is
captured and is excluded from compiler peak-live accounting.
The current metrics contract is version 16. Railshot's
existing exact counters can be projected into target-neutral quality-debt rows
without changing engine routing.

Railshot can also collect exact per-instance native function-entry counts with
`WithRailshotProfiling(true)`. Collection is off by default, allocates one
bounded off-heap `uint64` per original Wasm function only at instantiation, and
adds one atomic increment at each local function entry. Profiling builds disable
Railshot inlining so nested original-Wasm calls remain visible. The instance API
atomically observes or drains counts into a source-hash-bound profile:

```go
cfg := wago.NewRuntimeConfig().WithRailshotProfiling(true)
profile, err := instance.SnapshotRailshotProfile(wago.CompilerProfileSteady, true)
```

The instrumentation flag and source hash survive `.wago` serialization. The
versioned runtime context transfers each instance's counter pointer across
cross-instance calls, preventing observations from leaking into the caller's
slab. The slab is capped at 1,048,576 entries (8 MiB), released with the
instance, and uses atomic increments/snapshots on AMD64 and ARM64.

`PlanCompilerTier` turns a validated profile plus the original direct-call
graph into a deterministic bounded set of hot roots and call-cluster members.
The policy requires a positive threshold and function cap, limits cluster
expansion to eight direct-call edges, excludes imports from compilation, and
marks plans truncated rather than growing without bound.

For native specialization, `PlanCompilerNativeClones` additionally requires a
per-function native opportunity, projected gain, and original body size. It
ranks roots by measured heat and gain, admits each root's complete transitive
local direct-call closure atomically, and applies hard function-count and body-
byte limits. An imported or indirect call remains a runtime dispatch boundary.
A closure that does not fit is skipped and marks the plan truncated.

The experimental runtime boundary is enabled on the initial Railshot compile
with `WithTiering(true)`, which also enables its entry profile. A tierable
instance publishes architecture-specific stable wrapper thunks backed by one
bounded off-heap entry slot per local function. Compile the source-identical
Dragline module independently, then install it once:

```go
railshot, err := wago.NewRuntimeConfig().WithTiering(true).Compile(wasmBytes)
dragline, err := wago.NewRuntimeConfig().
    WithCompiler(wago.CompilerDragline).
    Compile(wasmBytes)
instance, err := wago.Instantiate(railshot, wago.InstantiateOptions{})
err = instance.InstallDragline(dragline)
```

To publish only the bounded hot set without retaining native bodies for cold
functions, compile and install the exact native-clone plan:

```go
plan, err := wago.PlanCompilerNativeClones(
    profile, importedFunctions, directCalls, opportunities, policy,
)
clone, err := wago.CompileNativeClone(cfg, wasmBytes, plan)
err = instance.InstallDraglineTier(clone, plan)
```

The clone emits only selected local bodies; unselected entry rows are zero and
occupy no native body space. It retains source-indexed module metadata so code,
trap, safepoint, and exact GC-root identities remain compatible with the base
instance. It is deliberately non-standalone and non-serializable, and the
installer requires exactly the same sorted selection. Function indexes remain
in the original Wasm index space; compilation rejects a local direct call that
would escape the selected closure. Installation rejects imports, duplicates,
unsorted rows, empty plans, and out-of-range indexes before publishing any slot.
A full independently compiled Dragline candidate remains supported for whole-
module installation.

Prepared exports, public calls, function-reference wrappers, and
cross-instance import dispatch retain the same thunk address across
installation. Publication atomically replaces entry slots, while the instance
retains the compatibility image and one candidate image until close so an
already-running Railshot frame can finish. The compact path avoids duplicating
cold native bodies. Installation is deliberately one-shot to bound retained code and
requires exact source identity, function layout, and bounds mode. Modules with
native GC root maps publish an immutable code-and-root-map generation before
any entry can target it. Safepoint helpers, nested host activations, and native
frame-chain walking resolve the exact generation from return PCs, including
old Railshot frames that finish after installation. The source identity and tierable flag survive `.wago`
serialization. Native ARM64 and Rosetta AMD64 tests cover direct, prepared,
compact, GC-root-bearing, concurrent, artifact-reloaded, and cross-instance
installation; the 180-row ISA
gate passes before and after installation. A four-round, 100 ms alternating
post-install diagnostic passes the prior 166/166 scalar inventory in both target modes: compatibility's
narrowest raw/paired win is 5.59%/5.83%, and native's is 5.41%/5.32%. Run it
with `go run ./cmd/draglinecompare -tiered -rounds=4 -benchtime=100ms` from
`bench` (add `-target=native` for the native-target pass).

The master-plan function artifact contract and bounded process-shared cache are
also available as an opt-in Go API. Production Dragline emitters can reuse
relocatable function bodies while module finalization still produces the same
whole-module `.wago` artifact. The cache is off by default and is always bounded
by the caller's byte charge:

```go
cache := wago.NewFunctionArtifactCache(8 << 20)
cfg = cfg.WithFunctionArtifactCache(cache)
```

The same bounded cache can be persisted explicitly without enabling a daemon:

```go
_, err := cache.SnapshotTo(writer)
_, err = cache.RestoreFrom(reader)
```

Snapshots are versioned and deterministic. Restore validates every artifact and
the complete byte charge before atomically replacing live entries; malformed,
trailing, duplicate, or over-budget input leaves the cache unchanged.

For process isolation and cache sharing across independent Wago clients, build
the optional daemon and give it a private Unix socket. A cache snapshot is only
loaded at startup and atomically published after graceful shutdown:

```sh
go build -o draglined ./cli/draglined
./draglined \
  -address /tmp/wago-draglined.sock \
  -cache-file /tmp/wago-draglined.cache \
  -cache-bytes $((256 << 20))
```

The daemon prints its bound address after it is ready. Its versioned protocol
uses complete source and response digests, strict JSON options, hard source and
artifact limits, bounded concurrent module compilation, and one reusable
function cache. Clients validate each returned `.wago` artifact locally:

```go
client, err := compilerdaemon.Dial(ctx, "unix", "/tmp/wago-draglined.sock")
compiled, err := client.Compile(ctx, compilerdaemon.CompileOptions{
    Target: wago.TargetCompatibility,
    Objective: wago.OptimizeSpeed,
    Core: 1,
}, wasmBytes)
```

Unix sockets are changed to mode `0600`. TCP is rejected unless it resolves to
loopback and the daemon is started with
`-allow-unauthenticated-loopback`; that switch does not add authentication.
Malformed Wasm is a per-request error and leaves the connection reusable, while
protocol corruption closes it. Persistent cache files use mode `0600`; invalid,
over-budget, non-regular, or symlink snapshots fail startup rather than silently
discarding compiler state.

The key separates non-code module structure, each function's local declarations
and body, and the deterministic transitive direct-callee closure that can alter
IPRA allocation. Editing one unrelated function preserves its cached artifact;
changing a leaf invalidates that leaf and every transitive caller while leaving
unrelated functions warm. Both direct and transitive cases are checked against
cold finalized output on ARM64 and AMD64.

Cached RailMach and structured-stack bodies carry ordered original-Wasm source
mappings and exact direct trap-stub metadata. The latter records native offset,
Wasm byte offset, and trap code for unreachable, bounds, indirect-call,
division, overflow, and conversion traps; cache validation checks every flat
slab before reuse. Direct, imported, indirect, and guarded calls record their
machine return PCs as zero-root safepoints. Safepoint ranges must pack the flat
root slab exactly in native-offset order: gaps, overlaps, out-of-bounds ranges,
and trailing unreferenced roots reject before cache publication or restore.
MVP values are scalar, so the root slab is intentionally empty rather than
inferred from machine code.

To capture the current per-function measurement document directly from a Wasm
file, run:

```sh
cd bench
go run ./cmd/draglinemetrics -out metrics.json -replay failure.json module.wasm
```

The replay file is written only for a function-specific compilation failure.

External compiler compile-time gates use a strict version-2 command manifest
and retain raw alternating-round measurements. The built-in `dragline` adapter
still runs as a fresh child of the harness, so its process accounting has the
same isolation as external commands:

```sh
cd bench
go run ./cmd/compilerharness \
  -config external-compilers.json -rounds 6 -out external.json corpus/tiny.wasm
```

Version-3 rows record process wall time, child user/system/total CPU time, peak
RSS, artifact size, executable SHA-256, tool version, and exact Wasm hash. Process
startup is included, so these raw CLI results are an end-to-end harness and must
not be presented as in-process compiler latency. The checked-in LLVM entry is optional because the local
Wasmer build does not contain LLVM; release qualification requires a real
`wasmer-llvm` command (or an equivalent manifest entry), not relabeling another
backend.

The current six-round ARM64 report covers all 17 admitted ISA modules. Against
Wasmtime 46.0.1 Cranelift, compatibility/native Dragline have geometric-mean
wall ratios of 0.917x/0.899x and peak-RSS ratios of 0.437x/0.437x. Their worst
per-module wall ratios are 1.144x/1.124x and worst RSS ratios are
0.467x/0.471x. See `dragline-external-compiler-arm64-2026-08-28.md` and its
adjacent `.jsonl` raw report.

The current neutral optimized-Wasm report covers 29 non-SIMD application and
kernel modules with the manifest's forced eight-worker Dragline policy. Against
the same Cranelift build, compatibility/native Dragline have geometric-mean wall
ratios of 0.983x/0.966x, CPU ratios of 0.563x/0.556x, and peak-RSS ratios of
0.537x/0.538x. Every module stays below the 3x wall and 1.5x RSS ceilings; the
worst wall ratios are 2.554x/2.554x and worst RSS ratios are 0.855x/0.839x. See
`dragline-external-neutral-arm64-2026-08-28.md` and its adjacent `.jsonl`.

Dragline consumes Wago's resolved function-worker policy. Independent
call-graph components wait for their callee levels, recursive SCCs stay on one
worker, and final assembly retains deterministic callee-first code layout. With
an explicitly parallel module policy, machine functions of at least 1,024
instructions may evaluate the three already-independent schedule candidates
concurrently. Default/one-worker compilation retains the serialized
minimum-memory path. Serial and eight-worker `wasm3` artifacts are byte-identical.

On Linux, the paired ISA benchmark can also open a `perf_event_open` group on
the locked execution thread and report multiplex-scaled user-space counters:

```sh
cd bench
WAGO_HARDWARE_COUNTERS=1 go test -run '^$' \
  -bench '^BenchmarkRailshotDraglineISAExec$/^isa_cmp_i32[.]eq$/^(railshot|dragline)$' \
  -benchtime=500ms -count=1
```

Available rows include cycles, instructions, branches, branch misses, L1I/L1D
and LLC load misses, and frontend/backend stalled cycles per operation. Cycles
and instructions are mandatory; unsupported optional PMU events are omitted.
Kernel and hypervisor activity is excluded, the goroutine is locked to its OS
thread for the measured loop, and the report scales counts by the kernel's
enabled/running times when events are multiplexed. Non-Linux hosts fail with an
explicit unsupported error rather than returning zeros; timing-only benchmarks
remain unchanged unless `WAGO_HARDWARE_COUNTERS=1` is set.

The current six-round `tiny.wasm` run fingerprints Wasmtime 46.0.1 and emits a
stable 50,688-byte Cranelift artifact. Excluding the cold first process, wall
samples range from 4.23 to 4.59 ms with approximately 14.9–15.0 MB peak RSS.
The configured `wasmer-llvm` command is absent on this host, so no LLVM result is
claimed.

## Footprint and performance status

The allocator uses storage proportional to one function's value count and a
fixed register set; it has no global cache or goroutines. Lowering and emission
consume one function at a time and reuse bounded per-compilation scratch, so
temporary IR storage is bounded by the largest function rather than the whole
module. Execution of the admitted ISA exports reports 0 B/op and 0 allocs/op
for both compilers. Six-round, 500 ms serialized alternating runs report all
180 ARM64 Dragline medians below Railshot in both compatibility and native
modes. In the retained post-change August 28, 2026 rerun, compatibility's
narrowest raw and paired wins are 5.21% and 5.19% at `isa_cmp_f32.eq`.
Native's narrowest raw and paired wins are 5.35% and 5.32% at
`isa_cmp_f32.ne`. The narrowest compatibility/native bulk-memory rows are
`copy_fwd_4096` at 5.61%/5.59% and 5.72%/5.73% raw/paired. The complete six
alternating rounds are retained in
`dragline-isa-arm64-compat-2026-08-28.txt` and
`dragline-isa-arm64-native-2026-08-28.txt` beside this document.
`memory.size` is 86.83% lower and `memory.grow(0)` 53.80% lower natively.

The compile benchmark reports latency, heap allocations, and native size per
module. After routing the newly admitted weak loop families through RailMach,
seven of the original 15 scalar modules compile faster than Railshot and seven allocate fewer
bytes on Apple M4 Max. Fourteen produce smaller native code; the expanded
narrow-memory module is 27,580 versus 14,932 bytes. Bounded first-function
native-expansion observation plus bounded SSA/prepass reservations cut
`isa_mem_narrow` from about 278.0 KB/670 allocations to 195.8 KB/620 without
changing its code bytes, while small
call-heavy modules retain their prior allocation profile. Reusable SSA, CFG,
scheduler, allocator, verifier, and physical-occupancy
scratch plus exact machine/semantic slab sizing and value-count reservations
reduced `isa_cmp_f32` from about 6.45 ms/419 KB/4,780 allocations to about
208.6 KB/400 allocations without changing strict backend decisions or its
4,504-byte output. Exact-on-growth slabs remove capacity overshoot, dead
pressure arrays are gone, use counts saturate in one byte, and float-heavy
functions index only their integer facts. The largest measured row is now
`isa_cmp_f64` at about 214.4 KB/400 versus Railshot's 113.7 KB/121. Post-RA
realization scratch is allocated only for rewrite
families present in the function rather than for every supported family, and
the integer-fact table stores one verifier-authoritative constant value rather
than retaining a duplicate. The address-fold pass reuses selection-verifier
use-count storage, the dependency DAG gives both reusable slabs one
function-bounded initial reservation, and the structured prepass reserves from
its byte bound to avoid growth copies. ARM64 power-rotation specialization results are
checked-in deterministic data rather than a package-initialization map walk;
allocation profiling confirms this removes about 31 MB of transient process
startup allocation without changing any generated function body.
This is explicit compiler-footprint debt, not a completed release gate. See
`dragline-plan-status.md` for the full phase ledger and qualification boundary.

This clears the admitted Wasm 1.0 scalar ISA execution gate on native ARM64 in
both target modes. Compiler footprint, peak-live memory, broader application
corpora, and native AMD64 hardware measurements
remain required before broader rollout. The exact whole-function recurrence
recognizers are prototype optimizations, not substitutes for the master plan's
general RailSSA, RailMach, selection, proof, scheduling, and allocation phases.

The opt-in curated application gate currently executes 30 of 36 modules and
bit-compares their results with Railshot. Six additional host-dependent large
artifacts (`regexmatch`, `wasm3`, Lua, SQLite, Ruby, and esbuild) compile
successfully, so all 36 available artifacts are admitted. The former three SIMD
rejections now pass a normal focused differential test covering V128 locals,
calls, structured results, selects, memory, shuffles, lane operations, integer
vector arithmetic, and reductions. After bounded vector operand caching,
call-free vector/scalar local pinning, and copy-on-write vector-local aliases,
the exact six-sample ARM64 median remains 1.85x to 8.35x slower than Railshot
(Apple M4 Max, 500 ms/sample), so this is correctness coverage rather than a
SIMD performance claim. Run
`WAGO_DRAGLINE_CORPUS_COVERAGE=1 go test -run TestDraglineCorpusCoverage -v`
from `bench`.

Corpus-by-corpus optimization now also specializes the `dispatch` module's
private, immutable, densely initialized local table. When the caller and every
table target have the exact proved integer signature, and each of at most 16
targets is a supported side-effect-free binary operation, ARM64 emits a bounded
selector switch directly in the prepared entry. It retains the unsigned table
bounds trap while removing the runtime table, null, signature, home, context,
and indirect-call sequence. The caller fell from 708 to 168 native bytes and
from a 32-byte frame to no frame. Seven serialized alternating 300 ms samples
measured 23.223 ns/op for Dragline and 23.369 ns/op for Railshot on Apple M4
Max, or 0.994x Railshot latency. The runtime test executes both table targets,
the out-of-bounds trap, and successful reuse after trap recovery.

The `branches` module's eight-instruction integer `br_table` leaf now uses the
same direct prepared integer entry as straight-line integer leaves. This removes
the generic prepared-call ingress without changing the backend CFG or default
table edge. Seven serialized alternating 300 ms samples measured 20.687 ns/op
for Dragline and 20.705 ns/op for Railshot on Apple M4 Max, or 0.999x Railshot
latency. Focused runtime coverage executes every explicit arm and the default
arm through the direct entry.

The same ARM64 entry is now available to bounded call-free, one-argument pure
integer/control leaves of at most 48 RailMach instructions. This covers the
focused SWAR `pack` and `parse4` exports and `fib_iter` without granting direct
entry to memory, global, reference, or call-dependent code. Redundant ingress
moves are omitted when allocation already places the parameter in `X0`.
Nine alternating 500 ms `pack` samples measured 20.008 ns/op for Dragline and
20.105 ns/op for Railshot (0.995x); seven alternating 300 ms `fib_iter` samples
measured 26.347 ns/op and 27.458 ns/op respectively (0.960x). `runN` improved
from 1.180x to 1.153x Railshot but remains a loop-code-quality target rather
than an entry-overhead problem.

The exact function-tail unsigned 64x64 multiply-high expansion emitted by
AssemblyScript/XJB is verified byte-for-byte before ARM64 replaces it with one
`UMULH`. The bounded matcher requires two `i64` parameters, one `i64` result,
the two declared scratch locals, every local index, mask, shift, and the final
function end; near or embedded expressions remain on normal RailMach lowering.
The `mulhi` entry now accepts its two integer parameters directly and fell from
116 to 56 native bytes. Nine alternating 500 ms samples measured 19.992 ns/op
for Dragline and 20.072 ns/op for Railshot on Apple M4 Max, or 0.996x Railshot
latency. Runtime coverage compares the rewrite with `bits.Mul64` across zero,
unit, all-ones, and mixed high-bit operands.

The same strict MVP coverage command also exercises the in-progress Phase 2
builder independently of production emission. The current pinned run builds and
verifies 3,184 function CFGs containing 9,662 compact blocks, 115 demanded local
block parameters, and 232 demanded operand-stack block parameters across all
782 emitted modules, with zero MVP rejections and zero post-MVP exclusions.
Administrative locals disappear from the resulting source-ordered semantic
graph, which contains 8,324 compact 24-byte instructions and 5,228 flat
arguments. The same pass verifies 1,981 effectful and 979 trapping instruction
records with source-ordered semantic obligations.
Bodies of at least 16 KiB use a bounded compact region-local event form, and
local SSA derives live-out sets rather than retaining a redundant dense matrix.
That keeps the original 14 MiB nominal transient ceiling while admitting
Ruby's 1,082,172-cell largest function. Demand liveness now uses a precomputed
instruction-to-block index and a visited-value worklist, so cyclic block
parameters terminate and the curated gate completes in roughly six seconds.
The deterministic semantic dump and bounded pure-integer/control evaluator are
covered separately, including conditional results, loop-carried locals, and
indexed/default `br_table` exits.
These numbers establish structural coverage. Production emission still walks
the compact stack record, but a reusable `EmissionPlanner` now hides its
CFG/SSA/simplifier scratch behind source-indexed verified decisions. Both target
emitters consume the first such decision: directly constant structured loads
and stores omit their bounds check only when a minimum-memory certificate proves the
complete access. Full block-argument RailSSA emission remains open.

The Phase 3 builder lowers that graph into separate AMD64 and ARM64
RailMach SSA. Each target verifies the same 6,489 compact 24-byte machine
instructions, 4,285 banked operands, and 500 weighted local/stack edge
transfers. AMD64 shift/division and private-call fixed-register requirements are
explicit. In both compatibility and native modes, verifier-gated scalar
functions may traverse
the complete selector, three schedule candidates, schedule-aware allocator,
late SSA exit, post-RA plan, ABI/frame analysis, and dedicated AMD64/ARM64
finalizer. The finalizer covers integer and floating-point arithmetic,
comparisons and conversions, scalar memory, memory size/grow, globals, select,
structured branches, returns, traps, and ordered `br_table` dispatch. Dense
spill slots, constant rematerialization, register/spill edge copies, and cycles
through a reserved temporary are emitted after a pre-emission safety check.
Execution is covered on native ARM64 and Rosetta
AMD64. Verified nested loops and loop-contained `if` result merges execute through
RailMach when its source-derived opcode policy selects them. Values live from
outside a loop are extended through every backedge before allocation, and the
independent allocator verifier rejects a truncated loop-live interval. The
established Dragline emitter remains a measured profitability choice for
arithmetic-only loop shapes; it is not a structured-control correctness fallback.
Both ARM64 target modes pass the 180/180 release gate. Module-local,
imported, and table-dispatched calls now finalize, and integer values live
across calls use allocator-managed callee-save frames. A canonical argument
vector keeps mixed Stack/RailMach callees ABI-compatible, and indirect
bounds/null/signature traps are covered on both targets. AMD64 fixed shift and
division repairs are emitted, including preservation of an allocated value live
across the shift-count RCX repair and the divisor-in-RAX case.
AMD64 imported and indirect calls use a bounded fixed frame area to preserve
call-live XMM values. Forced 21-value integer spills and 28-value floating
rematerialization execute through RailMach on both targets. The remaining
physical rewrites remain integration gates.

The initial verifier-backed `RALinearQ` shadow allocation also runs across every
admitted function. With conservative register sets it uses 301 aggregate spill
slots on each target and no function exceeds a 32-byte frame; fixed-use repair
records 58 AMD64 and 42 ARM64 moves. Late SSA exit keeps all 500 weighted edge
affinities through allocation, coalesces 87 per target, and resolves the
remaining parallel bundles into 476 AMD64 and 460 ARM64 physical moves,
including five exercised cycles per target. Cycle values and spill-to-spill
transfers use distinct reserved bank temporaries, so a memory copy cannot
overwrite a value preserved for a parallel-copy cycle. These are construction diagnostics,
not whole-corpus generated-code quality claims. Register, spill,
spill-to-spill, rematerialization, and cycle bundles plus AMD64 shift/divide
fixed-use repairs now feed the acyclic production finalizers. Predecessor-end,
successor-entry, and split-edge placements are emitted at their physical program
points. A complete bundle can move to a successor that is no hotter, and the
verifier independently checks placement legality, bundle consistency, and copy
motion debt. The strict corpus has no naturally profitable motion; a focused
weighted-edge test exercises two successor-entry copies.

The bounded Phase 5 analysis overlay is also corpus-clean. Without mutating the
verified graph it derives 254 aliases and 2,335 integer constants, simplifies
117 branches, marks 119 blocks and 765 semantic instructions dead, and discharges 323
obligations. Constant addresses produce 285 minimum-memory bounds certificates;
all 285 are replayed through the fuel-bounded demand-proof verifier. Bitwise
known-zero/known-one facts also derive unsigned ranges for acyclic masked
addresses and masked affine loop-carried chains. Fact verification replays both
instruction transfers and loop-parameter facts. The certificate verifier
re-derives the effective maximum from the semantic operand, offset, and access
width. Production consumes the analysis through one `EmissionPlanner` seam:
acyclic native functions reuse their existing RailSSA plan, while structured
loops use a compact independently verified source proof rather than building a
second CFG. That path elides 16 checks in each of the seven `isa_mem` functions
(112 total), performs zero warm planner allocations, and keeps the module peak
at 12,947 compiler-owned bytes. The general ARM64 and AMD64 fallback emitters
both shrink the proved masked-loop shape; the corpus-specific ISA loop emitter
already emitted the smaller form, so its native byte count is unchanged.
Non-bitwise loop ranges, non-adjacent forwarding, general check rewriting, and broader
production use remain unfinished.

The alias set includes local and unique-predecessor pure GVN from a reusable
bounded hash table. Its verifier replays the bounded dominance chain, exact
opcode/auxiliary data/resolved operands, and the absence of effects, traps, and obligations. Native RailMach
redirects operands to the canonical value and emits redundant definitions as
zero bytes; a native execution test covers repeated integer constants and
expressions. Full dominator-tree GVN and general graph compaction remain unfinished.

The Phase 6 planning overlay reports separate static pressure by block and
finds 273 safe cheap-operation sink candidates, 3,099 constant/extension/affine
rematerialization recipes, and 13 simple loop-header induction updates. The
largest admitted static block reaches 28 GPR and 10 FPR values. Its pressure
calculation is a linear event sweep with reusable per-function scratch; the warm
focused path performs zero allocations. The production pressure schedule now
commits a deterministic nonconflicting subset: one sink per consumer, no
chained adjacency conflict, delayed until the consumer's other dependencies are
ready, then independently verified as an adjacent pair. The strict corpus
commits 219 sinks per target candidate (438 total), and the pressure schedule
wins six functions per target without adding allocation debt.
Four edge-only inductions per target candidate are placed immediately before
their backedge terminator and verified for exact adjacency; updates with a
semantic consumer are retained in dependency order. The 53 affine recipes are
priced through the selected target rule, and all 106 target decisions beat the
modeled spill reload. Bounded loop-depth weights also drive cold-use separation
for rematerializable values at a four-to-one temperature boundary; the strict
corpus now has 24 mixed-temperature uses after cross-block simplification.
Constant, extension, and target-profitable affine uses commit per operand, are
excluded from the hot interval, and rematerialize in consumer-local scratch in
both finalizers.
The verifier-gated LICM path requires one outside preheader, no effects or
traps, operands defined outside the loop, loop-confined uses, and no increase
to the observed register-bank peak. One strict-corpus instruction satisfies the
complete rule and is reassigned to its preheader in each target schedule; an
explicit instruction-to-emission-block map is shared by scheduling, allocation,
and both finalizers.

Sparse simplification also removes trivial local and stack block arguments at
a bounded fixed point when every non-self incoming value resolves identically.
The verifier independently replays those inputs and checks the exact reduction
count consumed by pressure shaping. The strict corpus has no natural reduction;
a focused unchanged loop-carried-local test exercises and tampers with the path.

The stage-4 `RAGreedyP` allocator begins from the complete linear result,
then performs verified nonconflicting promotion, weighted eviction when useful,
callee-saved placement for call-crossing ranges, fixed-use repair rebuilding,
spillset construction, and stack-slot recoloring. Preservation-aware promotion
charges only the first use of each saved physical register. Regional placement
creates hot-loop and call-separated register fragments for spilled SSA values;
free-register fragments need one reload, while saturated regions save and
restore an independently verified inactive victim through a bounded slot. Both
finalizers emit those transitions, and version-16 metrics expose the resulting
allocation diagnostics. On this corpus and the current generous register sets,
it promotes 408 ranges per target, 138 genuinely call-crossing, charges 274
preservation units, and leaves no abstract spills or weighted debt.

The initial RailSpec contract and consumer-aware selector also verify every
admitted semantic operation. The versioned JSON declares target masks, operand
forms, verification state, and native-byte/latency/uop costs; `go generate`
validates the contract and reproducibly emits the dense checked-in table, while
check mode rejects drift. AMD64 selects 95 immediate forms, 112 fixed
shift/divide forms, 516 folded addresses, 80 compare/branch flag forms, and 691
shallow combinations; ARM64 selects 68 immediates, 516 addresses, 80 flag forms,
and 664 combinations. Production consumes proved address folds, adjacent
single-use integer load folds, immediate forms, LEA choices, and direct
compare/branch flags. Checked-in target, immediate, wrap, displacement,
single-use, initialized-memory, and bounds-trap tests close the bounded Phase 8
contract; measured CPU-family overrides and wider/floating folds remain later
policy extensions.

The Phase 9 shadow dependency graph records 3,214 AMD64 and 3,211 ARM64 data,
effect/trap, fixed-register, and fusion dependencies. Every function verifies
three sequential topological candidates: source-stable, latency/fusion
priority, and pressure priority. Each candidate now runs through scheduled
`RAGreedyP` and late SSA exit, and a deterministic score compares actual spill,
copy-cycle, physical-copy, copy-motion, fixed-repair, and broken-fusion debt before rebuilding
only the winner for post-RA planning. Both targets choose source-stable for
2,678 functions and pressure for six; the current simple latency costs win no
functions. The AMD64 scheduler resolves a selected comparison's actual sole
conditional consumer, holds the comparison until the control tail, and
independently verifies all 55 strict-corpus commitments as adjacent. Production
debt above four weighted spill units, excessive copies, or copy cycles triggers
one preservation-biased pass over the same serialized candidates; attempt two
and low debt are hard stops, and version-16 metrics expose the attempt count.
The spill-free strict corpus requests none, while forced-pressure execution
tests exercise two attempts on both targets. Calibrated processor resources,
deeper critical paths, and reload-latency post-RA motion remain open.

The bounded call-graph prepass computes deterministic SCCs, compiles acyclic
callees first, and leaves recursive SCCs conservative. Post-allocation ABI
analysis derives exact banked clobbers including fixed-use, result, and
transitive call writes; production caller allocation consumes completed callee
contracts and can retain a live value in an actually unclobbered volatile
register. Version-16 metrics count those refinements while function rows remain
source-indexed. Correcting the logical call position prevents results from
being classified as crossing their own calls. Current strict shadow coverage
refines 808 calls per target. Deterministic frame composition includes spills,
roots, callee saves, call/result areas, and runtime storage. Production
finalizers save and restore selected callee GPR/FPR regions and own dense spill
slots. The current strict-corpus maximum composed frame is 880 bytes on AMD64
and 928 bytes on ARM64. Both finalizers reserve the largest canonical outgoing
argument/result vector once in that frame and reuse it for direct, imported,
and indirect calls, eliminating per-call stack-vector adjustments; ARM64 keeps
the link-register save as a separate 16-byte call boundary. Allocator-selected
FPR preservation for imported and indirect platform calls follows the vector.
Direct, host-imported, and table-indirect call-live execution is covered on both
targets. Profile-hot recursive SCCs now receive one bounded refined pass after
all conservative member contracts are available. The component-wide caller mask
is independently replayed before publication; a refined member is retained only
when it stays within that contract and is no worse by complete schedule score.
Cold or mixed-emitter recursion remains conservative. Verifier-gated
per-register shrink wrapping moves a save/restore pair into an explicitly
observed zero-count, non-loop region of at most eight blocks. The region is a
single-entry/single-exit chain: every block after the entry has one predecessor,
every block before the restore has one successor, and every baseline interval,
regional fragment, and fixed-register write for that physical register stays
inside the chain before the restore. Side entries, side exits, and loop headers
reject the transformation. Initial-local writes remain at the function
boundary, frame slots remain stable, and native ARM64 plus Rosetta AMD64 tests
execute both hot and cold paths through a real two-block pressure case.
Version-16 metrics count realized pairs. The
private contract passes up to four source-ordered results in GPRs and places
overflow results in a caller-owned vector. Both finalizers implement direct,
imported, indirect, public-wrapper, and cache-restored mixed-result paths;
focused six-result and type-indexed multi-value control tests execute on native
ARM64 and Rosetta AMD64 without changing scalar production behavior.
Managed-reference result roots remain open.

The bounded post-RA planner consumes the shadow selection, schedule,
allocation, and copy debt. Its eight-instruction maximum scan identifies AMD64
LEA/fixed-divide/fusion/memory repairs and ARM64 compare/branch, same-base pair,
forward, pre-index, and rename opportunities, with target legality verified before any rewrite can be
committed. Pair and forwarding candidates additionally require actual schedule
adjacency. Verified AMD64 integer-add and representable immediate-subtraction
candidates emit LEA; register subtraction and the signed-displacement overflow
near miss remain unfused. Fifty-five strict-corpus integer compare/branch pairs
per target consume flags directly; AMD64 removes `setcc`/`movzx` and `test`,
while ARM64 removes `CSET` and the boolean recompare. Two AMD64 full-width
integer loads fold into their sole ALU consumer while retaining the original
bounds trap. ARM64 emits legal full-width integer and scalar-floating `LDP`
load pairs after two ordered source-specific bounds checks and promotes an
immediately following exact same-address load from its store value. Store pairs
cannot preserve the first Wasm store when only the second access traps, and
AArch64 has no byte/halfword pair form, so stores and narrow adjacent accesses
use the post-index path.
On both targets, a verifier-approved one-use integer comparison can keep its
condition in EFLAGS or NZCV across non-clobbering constant materializations;
the finalizer then omits both boolean materialization and its later comparison.
ARM64 scalar memory operations with nonzero offsets from 1 through 255 use a
signed-unscaled pre-index load/store on the reserved native-address scratch,
while preserving the original bounds check and Wasm address value.
Adjacent same-base scalar accesses with a signed-imm9 offset delta use a real
post-index first access and carry that scratch address into the second. The
second access performs its original bounds check with a distinct scratch before
using the carried address, preserving trap order and any first-store side
effect.
Each increments the version-16 production rewrite counter and has execution
coverage. Broader physical renaming and post-MVP MOPS remain open.

The Phase 12 specialization overlay consumes only explicit runtime facts. It
records same-instance direct calls, declared host effect contracts, preserved
bounds certificates, and indirect targets only when a valid profile has at least
10 observations and a 90% target share. Strict coverage records 808
same-instance calls and preserves all 294 bounds certificates. Focused tests
cover host effects, threshold rejection, counter overflow, and certificate
verification; the corpus supplies no host contracts or profile observations.
Backend-neutral exact-type, identity, nullability, freshness, generation,
pointer-free, and known-array-length facts now live at the shared compiler
codegen seam rather than under Railshot. RailSSA accepts an optional sparse list
of those facts, tied to one source-ordered semantic result. The list must be
strictly ordered; independent verification checks the result exists, is a
reference, and that an unpublished-fresh fact is non-null, has a bounded nonzero
identity, and describes a struct or array. Exact-type and fresh-object decisions
are distinct specialization entries, and version-16 metrics expose both counts.
No production Dragline GC producer exists yet, so these specialization counts
remain zero for the scalar corpus and do not authorize barrier removal or GC
instruction lowering. Reference-bearing signatures, locals, globals, block
results, and calls do use the compact typed SSA path. A separately verified
backwards dataflow plan publishes only collector references live at each
potentially collecting call, colors disjoint lifetimes onto reusable bounded
root slots, and distinguishes call operands from values that must be reloaded
after collection. Both native finalizers store those roots before the call,
reload live-after values, and publish frame-relative offsets plus temporary
wrapper stack adjustment through version-2 function artifacts and the runtime
compiler output. Wago validates the flat callsite/root slabs before admitting
them to its existing native frame walker. `funcref`, `externref`, and exception
references are excluded from collector maps.
The production native planner now builds and verifies this same plan against
the exact function index and immutable input profile. A dominant in-module
local indirect target emits a canonical-funcref-identity guard and private
direct-call relocation; mutation or a profile miss falls through to the full
bounds, null, signature, home, and context-switch path. Native ARM64 and Rosetta
AMD64 execution covers the guard hit, a same-signature fallback, and the bounds
trap, while version-16 metrics count realization. Host contracts are configured
by exact `(module, name)` import identity, snapshotted into immutable function-
import order, recorded in replay artifacts, and hashed only into cache entries
for functions that consume them. Declared effects replace conservative imported-
call metadata before simplification and scheduling; the dependency DAG orders
same-domain accesses while allowing independent heap domains around calls that
do not declare global barriers. Unlisted imports remain conservative. Profile edge counts keyed to
original Wasm offsets also drive the verified ExtTSP-like block order used by
both production finalizers; a hot-false-edge conditional executes correctly on
ARM64 and AMD64. Broader snapshot guards and deoptimization, cloning, the
production GC fact producer/consumer, GC instruction lowering, allocation
elimination, and helper safepoints remain open.
