# Profiling Wago workloads

This is an experimental, opt-in developer tool. See the qualification record
before relying on a particular collector, target, or measurement capability.

`wagoprof` records a defined workload, retains the identities and lifetimes of
Wago's generated code, and joins sampled functions to Railshot's compiler
statistics. Collection and visualization remain with perf, pprof, and Samply.
Profiling builds expose `wago profile`; `bench/cmd/wagoprof` remains a standalone
wrapper around the same command implementation. Ordinary builds contain neither
the command nor its capture and reporting implementation.

See [qualification results](profiling-qualification.md) for tested targets,
remaining capabilities, and the release-size gate.

## Build and record

From the repository root, build the integrated CLI:

```sh
scripts/build-profiler.sh /tmp/wago --cli
/tmp/wago profile record --workload json-as --iterations 1000 --out /tmp/json-cli.wagoprof
/tmp/wago profile top /tmp/json-cli.wagoprof
/tmp/wago profile annotate /tmp/json-cli.wagoprof --function serializeN --json
```

This uses the engine compiled into the profiling binary. It does not select a
different installed runtime or resolve project plugin imports. The current workload
contract accepts the supported corpus presets or explicit module/export/ABI-slot
arguments and result oracles. Presets use the repository catalog; pass `--catalog`
when recording outside a checkout.

The standalone wrapper remains available:

```sh
scripts/build-profiler.sh /tmp/wagoprof
cd bench

# Portable metadata, exact workload checks, and elapsed phase timings.
/tmp/wagoprof record --workload json-as --iterations 1000 --out /tmp/json.wagoprof
/tmp/wagoprof top /tmp/json.wagoprof

# Linux: native CPU sampling, jitdump, perf injection, and pprof export.
/tmp/wagoprof record --backend perf --include-code --source-maps --workload json-as \
  --phase execute --duration 15s --rate 99 --out /tmp/json-perf.wagoprof
/tmp/wagoprof top /tmp/json-perf.wagoprof
/tmp/wagoprof annotate --function serializeN --assembly /tmp/json-perf.wagoprof
perf report -i /tmp/json-perf.wagoprof/perf.jit.data
# Open native PCs with any recorded static inline ancestry:
go tool pprof /tmp/json-perf.wagoprof/native.pprof

# Isolate decoding of a freshly produced artifact (Go CPU profile).
/tmp/wagoprof record --backend pprof --workload json-as --phase reload \
  --iterations 1 --out /tmp/json-reload.wagoprof

# macOS: Samply must be installed. This collects all phases and may include
# off-CPU observations; it is not an isolated execute-only CPU measurement.
/tmp/wagoprof record --backend samply --workload json-as --duration 15s \
  --out /tmp/json-samply.wagoprof
/tmp/wagoprof top /tmp/json-samply.wagoprof
```

The build script embeds the source revision and dirty status, including when Go
omits VCS settings for this nested module. Direct `go build -tags=wago_profile ./cli/wago` from the root or
`go build -tags=wago_profile ./cmd/wagoprof` from `bench/` works,
but missing build provenance is explicitly reported. Captures stay local. Output
directories must be new; the runner never replaces an earlier capture.

`--workload` reads the existing `corpus/catalog.json` benchmark contract: module
hash, initialization export, calls, arguments, and exact result oracle. JSON-AS
is a preset, not a separate workload implementation. `jsonprof` remains a
compatibility command using this runner and the same oracle.

For a custom module:

```sh
/tmp/wagoprof record --module add.wasm --export add --args 20,22 --want 42 \
  --iterations 100000 --mode prepared --out /tmp/add.wagoprof
```

Arguments and results are unsigned 64-bit ABI slots. Floating-point arguments
use their bit representations, and vectors occupy two slots. Lists are
comma-separated, optionally enclosed in one matching pair of square brackets;
malformed lists and values outside the unsigned 64-bit range fail validation.
Use `--want '[]'`
for a void result and `--init EXPORT` for optional initialization. An expected
result is mandatory. The current import environment provides a failing
`env.abort`; unresolved imports and command/WASI presets fail explicitly.
Arbitrary host environments can use the library interface below.

`--mode public` includes named `Instance.Invoke` lookup and public-call overhead.
`--mode prepared` uses a resolved `WasmFunc`, retaining normal invocation gates.
Neither mode claims to measure a pure guest kernel: every invocation includes
result validation. `--warmup` defaults to five complete workload iterations.
Choose either `--iterations N` or `--duration D`; explicit conflicting options
are errors. Duration is checked between complete iterations, and the bundle
records actual elapsed time and completed work, including any overshoot.

## Capture capabilities

| Backend | Measurement | Phase isolation | Native symbols | Guest stacks |
|---|---|---|---|---|
| `none` | Elapsed phases and static compiler data | No sampling | Metadata only | Unavailable |
| `perf` | Linux `cpu-clock:u` samples and event periods | Acknowledged enable/disable | Timestamped jitdump | Unqualified |
| `samply` | Native observations, potentially including off-CPU time | All process phases | Perf-map fallback | Unqualified |
| `perf-map` | Metadata for an externally started collector | Uncontrolled | Perf map | Unqualified |
| `pprof` | Go process CPU profile | `StartCPUProfile` / `StopCPUProfile` | Guest PCs not qualified | Unqualified |

Linux capture starts perf with events disabled. The child enables and disables
collection through the documented control/acknowledgement protocol. Small
collector-control and discovery-mapping costs can appear around the phase;
these are not guest instructions. Supported
phase selections are `compile` (decode + validate + native compilation),
`reload` (trusted artifact decoding), `instantiate` (including Wasm start),
`initialize`, `execute`, `close`, and `all`.
Warmup has its own recorded interval. `all` includes runner/exporter overhead.
Acknowledgement timeouts fail the capture. A failed child or expired safety
deadline terminates the collector; on Linux/macOS its private process group is
terminated too. Pipe shutdown is bounded, and the bundle remains incomplete.
`--phase=reload` first compiles the supplied Wasm, serializes it in a separate
`artifact-prepare` phase, closes the preparatory module, and records only decoding
with `LoadTrustedArtifact`. Executable mapping still occurs during instantiation.
`--reload-artifact` uses that same round trip with any capture phase, including
`execute`. Only artifacts produced by the current run are loaded. The manifest
records the artifact hash and byte size; it does not save the artifact implicitly.
Current artifacts omit compiler/source metadata, so loaded bodies remain unknown
and the manifest reports that limitation. Diffs reject mixed reloaded/direct
compilation captures. Phase timestamps use Unix nanoseconds; `elapsed_ns` and
execution duration use the monotonic clock. Blocking or nonterminating guest calls are not preempted by a
workload duration; the measurement deadline is checked between iterations.
The CLI's separate `--collection-timeout` (default 5 minutes) supervises setup,
warmup, invocation, and teardown in another process, including a guest call that
never returns. `--conversion-timeout` (default 2 minutes) bounds perf injection
and sample conversion together. These safety limits do not set the measurement
window; raise them explicitly for long experiments. Both effective limits appear
in the manifest. A deadline fails the capture and preserves available evidence;
it does not fabricate phase completion or completed work. Internal `capture`
re-execution and direct `profcapture.Run` calls rely on the supervising parent.

Every measured phase records its own `completed_work` and `work_unit`.
`top` normalizes execute samples by validated workload iterations, instantiation
by completed instantiations, initialization by completed initialization calls,
and compile/reload/close by their own completed operations. No-op initialization
has zero completed work. Old bundles without these counts show startup/teardown
totals only. `all` shows CPU totals and elapsed phase breakdowns; it does not
relabel startup amortization as execution cost. Text and JSON use the same rows,
static compiler metadata, denominator, and `--limit`; total sample/weight fields
still describe the full capture.

Jitdump records contain copied native bytes, so `--include-code` is required for
`perf`. Its load records use the same monotonic clock as perf. Retirement remains
in Wago's journal: jitdump has no per-function unload record, and its close record
is emitted only when the capture ends. Perf-map export rejects address reuse
rather than silently assigning a new generation's symbol to an old sample.

The macOS/arm64 qualification resolved JSON-AS guest functions with Samply
0.13.1. `top` uses only each observation's leaf symbol; it ignores unqualified
ancestor frames and does not reinterpret observations as CPU nanoseconds.
Linux/amd64 real perf 7.0.14 capture also passes: an execution-only JSON-AS
capture retained symbols after teardown, opened in `perf report` and pprof, and
joined sampled instructions to compiler output. Qualification used a container
with only `PERFMON` added; the host kept `perf_event_paranoid=4`. Wago does not
change host security settings. The default native sampling rate is 99 Hz; use
`--rate` to trade sampling resolution for overhead. See
[qualification results](profiling-qualification.md) for measured overhead and
the tested scope.

## Hotness and compiler output

`--unwind-maps` alone is an experimental metadata-only option. It retains sparse
compiler recovery rules in `images.json`; it does not capture raw stack bytes
or enable call-chain collection. Jitdump export converts available rules into
offline DWARF records. Current coverage is AMD64 fixed-frame bodies with no calls
or supported direct-local calls, including precise prologue and epilogue
transitions after compaction. Entry adapters, shared adapters, and shared return
tails retain their stack-state rules through layout changes. The
[adapter fixture](../profile/testdata/adapterunwind/README.md) qualifies sampled
body-to-adapter paths separately from deterministic instruction-boundary tests.
Host/indirect/tail calls, inlining,
GC/EH/plugin paths, jump-table data, cold traps, and ARM64 remain unqualified. Missing ranges and
unspecified caller registers are unknown. The manifest records requested and
available unwind maps separately; `qualified_guest_stacks` remains false.
Artifact reloads do not retain these rules. Ordinary builds remove the recorder
and remapping code along with the rest of profiling.

Linux/AMD64 can explicitly opt into bounded raw stack sampling with
`--stack-bytes=8192`, alongside `--unwind-maps --include-code`:

```sh
wago profile record --backend=perf --workload=json-as --duration=15s \
  --include-code --unwind-maps --stack-bytes=8192 --out=json-stacks.wagoprof
perf report -i json-stacks.wagoprof/perf.jit.data \
  --symfs "$(pwd)/json-stacks.wagoprof/symbols"
```

The byte limit must be a multiple of eight from 256 through 65,528; zero, the
default, collects no raw stack memory. This separate opt-in can retain stack
contents, including guest or host data. The limit bounds each sampled read;
bounded reads and unsupported transitions can truncate call chains. The manifest
records `raw_stack_bytes_limit`, `stack_collection`, and the relative
`jit_symbol_root`. `qualified_guest_stacks` remains false: the tested direct-call
and adapter paths do not establish complete mixed Go/Wasm stacks. Artifact
reloads are rejected for this mode because they do not retain unwind metadata.

Stack captures preserve generated JIT ELF files and the dump under
`symbols/tmp/jitted-wago-*/`. This retains the path spelling required by the
qualified perf/libdw implementation. The temporary originals are removed after
capture. You can move the whole bundle and reopen it with `--symfs` pointing to
its new `symbols` directory. Host executables and shared-library symbols are not
copied into this JIT symbol tree. Use the injected `perf.jit.data` for viewing;
raw `perf.data` retains the original temporary discovery paths.

`top`, `annotate`, `diff`, and `native.pprof` continue to use flat self attribution.
`annotate --assembly` supplies the bundle's symbol root to perf automatically. Diffs
require matching stack-collection settings and requested sampling rates so a
change in collector overhead is not silently treated as a compiler improvement.

`top` displays measured self samples and, for perf CPU-clock captures, estimated
CPU nanoseconds per completed workload iteration. Its static columns describe
emitted code bytes, frame bytes, spills, reloads, and bounds checks. These are
compiler counts, never dynamic operation counts or estimates of savings.
Unknown/non-guest samples remain visible instead of being assigned to a nearby
Wasm function.

`annotate --function NAME_OR_FULL_INDEX` prints the matching compiler decisions
and sampled native offsets. `--assembly` delegates disassembly to `perf annotate`
on the injected capture. Saved native bytes are optional outside jitdump.
With `--source-maps`, recorded PCs can also show their Wasm origin and static
inline call sites. Coverage remains sparse; see the source-map section below.

```sh
/tmp/wagoprof diff /tmp/baseline.wagoprof /tmp/candidate.wagoprof
```

Diff currently compares `execute` captures. It requires matching workload
contract, target, CPU model, OS version, logical
CPU count, invocation mode, phase, event, backend, and warmup. Compiler choices
may differ. Phase isolation, collector version, requested sampling rate, raw-stack
settings, and requested metadata collection must also match. It compares absolute
sampled CPU cost per iteration and separately displays elapsed time per iteration.
CPU totals include guest functions and their owned adapters, helpers, and
unknown/non-guest samples. Unknown time can include unresolved guest PCs as well
as host/runtime execution. No samples for a function means unobserved cost, not
proof that its execution was free. Captures with no CPU observations cannot
establish a CPU comparison.

`diff --json` emits a versioned object with completed iteration counts,
`elapsed_ns_per_iteration`, an optional `sampled_cpu` breakdown, `functions`, and
diagnostics. Each summary cost has `before`, `after`, and `delta` values; CPU and
elapsed nanoseconds remain separate. Backends without native CPU measurements
still report elapsed cost and omit `sampled_cpu`. When multiple sampled artifacts
implement the same logical function within one capture, their CPU costs add,
but ambiguous compiler counters are omitted with a diagnostic.

A comparison does not establish statistical significance from
one pair. Samply observations are rejected as CPU-cost inputs. Use repeated,
controlled captures before making performance claims. CPU affinity, power state,
and other system activity remain the experimenter's responsibility.

## Embedding the metadata journal

```go
session := wago.NewCodeProfile(wago.CodeProfileOptions{
    IncludeCode: true, // copies native code; choose explicitly
    MaxBytes:    64 << 20,
    MaxEvents:   65536,
})
defer session.Close()

cfg := wago.NewRuntimeConfig().WithCodeProfile(session)
compiled, err := cfg.Compile(wasmBytes)
// Handle err, then instantiate and run normally.
```

Only opted-in modules are observed. `session.Snapshot()` atomically returns
current images and a cursor; `session.Read(cursor)` returns subsequent events.
This provides ordered live attachment to a session without a gap between
enumeration and subscription. Check the returned status on every read. The
bounded journal retains retirement history until `Close`; budget exhaustion
increments an explicit loss count. A capture with lost metadata must not be
presented as complete. Long-running embeddings should drain and rotate sessions
at suitable workload boundaries; draining does not erase retained history.

Each image has a module hash, artifact/configuration hash, and unique mapping
ID. Function indexes include imported functions. Regions use finalized offsets
and emitted lengths, including entry adapters, guest bodies, shared cold trap
bodies, shared adapters, runtime helpers, literals, and padding. Omitted inlined
functions do not acquire fabricated native ranges. Both serial and parallel
finalization produce the same metadata contract. `Instance.CodeBase()` retains
its existing defensive-copy behavior.

Registration happens before an image can execute, once per real mapping. Shared
instances do not duplicate it. Retirement occurs after execution ownership ends,
just before returning the mapping address to the OS, so address reuse cannot
overtake retirement. Host thunks have separate registrations. The registry does
no I/O and calls no observers under the code-cache mutex. Retained bytes and
public snapshots are copies, never raw pointers into executable memory.

`Compiled.AttachCodeProfile(session)` attaches to current body and host-thunk
mappings, including mappings retained by live instances after `Compiled.Close`.
Attachment is ordered with creation and retirement; requested code bytes are
copied while the mapping is still owned. Profiling builds keep a directory of
live thunk mappings, removed at actual unmapping. Ordinary builds have no such
directory or tracking fields.

A load record with `preexisting: true` marks first observation during attachment,
not the original mapping creation time. It cannot symbolize historical samples
from before attachment. Artifacts and unobserved compilations omit diagnostic
metadata, so their bodies report an unknown region and unavailable original-module
identity rather than guessed function ranges. Source-map requests cannot recover
absent metadata; use recompilation for detailed regions and compiler counters.
Thunk regions remain exact. Consumers use `Snapshot` plus its cursor to continue
with subsequent events without an enumeration/subscription gap.

Attach before instantiation to capture initialization or enable boundary tracing.
Enabling tracing with existing instances is rejected, since in-flight activations
lack the necessary ancestry. Reattaching the same session is idempotent; a second
session cannot replace the first. Closing a session releases its retained
diagnostics without changing code ownership.

The ordinary runtime imports only the small metadata journal. File encoders,
symbol resolution, pprof production, and collector processes live in `profile/`
and `internal/profcapture` / `internal/profilecmd`. No Go signal handler, cgo bridge, protobuf runtime,
or guest entry/exit instrumentation is added.

## Bundle and quality contract

A capture directory contains:

- `manifest.json`: schema version, source provenance, workload/module hashes,
  effective configuration, host details, collector/event, requested rate,
  actual work, phase intervals, diagnostics, and metadata-loss status.
- `images.json`: ordered load/retirement history and immutable compiler metadata;
  native bytes only when explicitly requested.
- Linux perf: `jit-PID.dump`, `perf.data`, `perf.jit.data`, `samples.json`,
  `report.json`, `native.pprof`, and `collector.log`.
- Samply: `samply.json.gz`, `symbols.map`, and `collector.log`.
- Go CPU sampling: `cpu.pprof`.

For perf and Samply, successful workload execution leaves `complete: false` and
`collector_pending: true` until the parent has retained collector output and
finished any required conversion. Only then does it publish `complete: true`.
An interrupted parent therefore leaves a visibly unfinished capture. CPU reports
reject pending captures. Final manifest publication uses a temporary file and
atomic replacement on the supported Linux/macOS targets; publication errors
leave the previous incomplete state available.

Files are private to the capture directory. Guest arguments, guest memory,
source paths, and the process environment are not copied into the manifest.
Native bytes and raw system-profiler files can still contain sensitive diagnostic
information. Samples retain their event periods; the requested sampling frequency
is not asserted to be an observed fixed rate. Perf lost-event records fail sample
conversion. Samply's loss accounting and mixed-stack completeness are not
qualified. Export/write/close failures and incorrect guest results fail the run
and leave a failed manifest where output creation succeeded.

Perf conversion streams sample text into a parser capped at one million samples
and a 1 MiB input line, instead of buffering all command output first. Malformed
records or the sample limit stop the producer and suppress partial reports.
Injection and sample-conversion diagnostics retain at most 32 KiB; exceeding that
limit also stops conversion and reports truncation. Raw capture files remain in
the failed bundle for investigation with existing tools.

Saved-bundle readers share `profile.DefaultLimits`: 128 MiB per stored input
file, 128 MiB of decoded/decompressed JSON, 64 MiB of retained native code,
65,536 events, 8,192 image records, one million samples and metadata/table
entries, 100,000 report rows, and 250,000 distinct hot PCs. Inline depth is capped
at 128 and expanded inline entries have a separate one-million-entry ceiling.
Sample and image arrays decode incrementally and stop before retaining an excess
record. Image JSON also has a one-million structural-entry budget and nesting
depth 128, checked before allocating nested metadata arrays/maps. Samply JSON has a decompressed byte ceiling and nested sample/table arrays
enforce shared cardinality budgets as they decode, before aggregation. Raw perf inputs are checked
before conversion. Limit violations reject the report rather than display an
apparently complete subset. These are separate resource budgets, **not a 64 MiB
process-memory guarantee**: parsing, sorting, and report construction still use
additional bounded storage. Incremental aggregation and mapping-churn scaling
remain later work. The Go APIs accept explicit `Limits`; CLI readers use the
default policy.

If an external collector's child leaves a missing, truncated, or unsupported
manifest, the parent publishes a failed manifest with actual phases and completed
work explicitly unknown. Existing malformed bytes are preserved as
`manifest.child.invalid.json`; raw collector data and logs remain available for
diagnosis. Preservation and replacement errors also fail the capture. An
existing evidence file is never overwritten to make recovery appear successful.

Boundary timelines for host callbacks, nested guest activations, waits, and GC
helpers are a separate optional measurement layer. Phase and boundary durations are
elapsed wall time and must not be treated as host CPU time. GC root maps remain
unrelated to arbitrary-PC unwind qualification.

Format references: [Linux jitdump specification](https://github.com/torvalds/linux/blob/master/tools/perf/Documentation/jitdump-specification.txt),
[perf record control protocol](https://man7.org/linux/man-pages/man1/perf-record.1.html),
[Samply](https://github.com/mstange/samply), and
[pprof profile format](https://github.com/google/pprof/blob/main/proto/profile.proto).

### Build-time diagnostic removal

Mapping profiling requires `-tags=wago_profile`; the build script supplies it. An ordinary runtime build removes the journal, registration, retirement, and finalized-region collection paths. Requesting profiling in such a build fails configuration validation.

All ordinary builds, including embedding applications, compile out compiler counters and explain reports. Explicit `wago_profile`, `wago_codegenstats`, or `wago_gcstats` tags enable compiler diagnostics; requesting unavailable diagnostics returns an error. `wago_nodiagnostics` is no longer needed. GC cycle telemetry still requires `wago_gcstats`.

Run compiler-statistics tests with `go test -tags=wago_codegenstats ./src/core/compiler/backend/railshot/... ./src/wago`; CI runs these alongside ordinary-build tests. The ordinary suite skips assertions that require counters and tests that diagnostic requests are rejected. For the standalone compiler report, run `go run -tags=wago_codegenstats ./cmd/explain module.wasm` from `bench/`.

### Optional elapsed boundary timeline

Build with `scripts/build-profiler.sh`, then add `--timeline` to a capture:

```sh
/tmp/wagoprof record --catalog corpus/catalog.json --workload json-as --backend none --iterations 20 --timeline --out /tmp/json-timeline
/tmp/wagoprof timeline /tmp/json-timeline
```

The current coverage is synchronous native activations, host callbacks, internal GC and atomic-wait helpers, and callback-authorized `InvokeFromHost` re-entry. Named `Instance.Invoke`/`InvokeContext`, resolved `WasmFunc.Invoke`, typed `InvokeValues`, and `PreparedSession` calls are traced even without host transitions. Instance instantiation, start-function initialization, logical close, and physical release are recorded separately. Initialization spans identify whether the start function is guest code or a directly imported host function. A retained invocation or reference can delay physical release beyond logical close. Re-exported host imports receive callback spans even when they bypass native dispatch. The manifest records this coverage explicitly. Lifecycle spans are independent operations with instance identities, so overlapping lifecycle and invocation durations must not be summed as disjoint work. Boundary recording spans all workload phases, independently of the CPU collector's selected phase; initialization can therefore appear in the timeline.

Rows contain invocation, activation-parent, actual-instance, module, and import/helper identities. Inclusive elapsed time includes nested re-entry. Exclusive elapsed time subtracts the union of immediate child spans. These durations are not host CPU time or dynamically executed compiler counters. Guest activation time also includes runtime transition overhead.

Tracing uses the admitted generic invocation/dispatch paths for observed named and prepared calls; its overhead is not representative of an unobserved call. Native sampling without `--timeline` preserves ordinary dispatch. Boundaries are limited by `--max-spans` (default 65,536) and the session's shared metadata-byte budget. Capacity loss fails capture quality and suppresses exclusive duration estimates. `boundaries.json` preserves raw spans; `timeline.json` records the analyzed elapsed-time view, including incomplete spans and loss diagnostics.

Invocation spans begin after the serialization gate is acquired, use the actual runtime invocation identity, and include runtime transition overhead. Waiting to acquire that gate is not included. Callback-authorized re-entry has an enclosing span that includes target admission and a nested native activation where applicable.

Observed reserved sessions retain their normal reservation identity across calls and assign each call a distinct span identity. Their tracing path uses the existing general reservation admission instead of identity-free direct admission. Profile comparisons require matching timeline coverage so tracing overhead cannot silently be compared with an unobserved invocation path.

For library embedding, `CodeProfileOptions.TraceLifecycle` enables instance lifecycle recording and implies `TraceBoundaries`. The CLI enables both with `--timeline`. Instantiation recording begins after import-options normalization; module decode/compilation remains represented by the capture manifest phases.

### Sparse Wasm instruction locations

`--source-maps` requests compiler-recorded opcode lowering, deferred-expression, and trap/check locations. The mapping joins a final native range to a full Wasm function index and a byte offset measured from that function's local declarations. Function compaction and module adapter removal transform the directory before publication; native code bytes are unchanged. `wagoprof annotate` prints a Wasm location when a sampled PC falls inside a recorded range.

Coverage is recorded as `opcode-lowering-and-deferred-origins`. The bytecode driver scopes eager lowering, including calls and control flow, while deferred expressions and scalar memory loads retain their producer locations. Native instructions outside these scopes remain unmapped. This does not provide source-file lines, complete expression provenance, sampled call stacks, or unwind rules. Library applications opt in with `CodeProfileOptions.SourceMaps`; reloaded artifacts cannot reconstruct absent source metadata. Source directories are copied into the bounded session and survive mapping teardown.

Deferred expression nodes retain their original Wasm location separately from the compiler stack payload. Scoped emission records generated arithmetic, conversions, and supporting instructions; nested scopes split the parent range and restore its origin afterward. Operand folding attributes the combined instruction to the expression that emits it, without inventing separate native instructions for eliminated operations. Tentative native-code rollback discards the corresponding ranges. Check branches are captured before shared-trap lowering repurposes scratch records, then remapped through final compaction. Runtime trap payloads and native code bytes remain unchanged. Scalar load/store checks and explicit `unreachable` branches are also qualified, and specific check origins override broader expression ranges. Standalone deferred scalar memory loads retain their load opcode location even when a later opcode forces materialization. A fused instruction has one principal lowering origin: an ALU instruction with a folded memory operand belongs to the consuming expression, and a paired load belongs to the opcode that emits the pair. The map does not claim independent native instructions for eliminated operations. Prologues and synthetic code outside an opcode scope remain gaps; additional trap families are not independently qualified.


Recorded source ranges can carry static inline caller ancestry. `inline_parent`
indexes a per-image caller table whose entries contain full Wasm function indexes,
call-site byte offsets, and their own parent indexes. Compaction preserves these
identities and module publication relocates the table indexes. The manifest's
`static_inline_ancestry` flag reports whether any recorded range has an inline
caller. `annotate` and JSON hotspot output show that ancestry for resolved PCs.
This describes the compiler's inlining decisions; it does not reconstruct sampled
native callers or establish mixed Go/Wasm stack unwinding. Eliminated and otherwise
unmapped expressions still have no reported instruction location.


### Pprof export semantics

`native.pprof` retains each sampled native PC as one location. When that PC has
compiler-recorded inline ancestry, the location includes the logical Wasm leaf
and its static callers in pprof's inline order. Other native callers are absent;
this export does not qualify stack unwinding. A function's cumulative total can
therefore include known inlined callees, but not missing native call frames.

Function identities include the module, compiled artifact, and full Wasm index;
helpers additionally retain their region kind and offset. Wasm byte offsets use
`wasm_offset` labels, and `inline_call_sites` preserves caller byte offsets. They
are not source-file line numbers. `native_owner` retains the physical function
containing the sampled instruction. Missing names fall back to Wasm indexes.
With `--source-maps`, `compiler_site_kind` labels identify sampled explicit
operand spill/reload instructions or a linear-memory bounds branch. These labels
describe the emitted instruction; their sample weights are not dynamic spill or
check counts.
The writer rejects incompatible units, signed-value overflow, and inconsistent
sample totals instead of producing a truncated or misleading profile.

## Compiler sites at hot instructions

`--source-maps` also saves a sparse `compiler_sites` directory in each code image.
`annotate --json` includes all recorded `emitted_sites` for the selected physical
function, and sampled `native_pcs` carry their exact `compiler_site` when present.
Text annotations display the same kind beside the sample count and CPU weight.
Use pprof's `compiler_site_kind` labels to select samples at these sites.

Current kinds distinguish explicit GP, floating-point, and vector operand spills
and materialized reloads, plus custom-value spill stores and linear-memory bounds
branches. A bounds site covers the conditional failure branch; it does not claim
the surrounding address or predicate instructions. Folded stack operands,
pinned-local writebacks, and decisions without recorded sites stay unclassified.
The map is separate from existing function-level counters and need not have the
same totals. It describes final surviving emission sites, not dynamic operation
counts or a measured saving from a compiler optimization.

Sites follow tentative-code rollback, instruction shortening, frame compaction,
and module adapter layout changes. Eliminated sites disappear; neighboring sites
do not fill unknown gaps. Each site stays within one executable-region owner.
The journal copies the directory and charges it to the metadata budget. Artifact
reloads and late attachment cannot invent absent sites, and ordinary builds
remove the recorder along with all other profiling code.
