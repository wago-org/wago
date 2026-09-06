# Runtime resource model

Wago classifies limits by their technical purpose. A valid Wasm module must not
be rejected only because it is unusual.

## Limit classes

### Safety invariant

A safety invariant prevents memory corruption or invalid native execution. It is
not a tenant quota.

Current examples:

- native stack fence and frame-size validation;
- integer and allocation-size overflow checks;
- address-width and platform mapping checks;
- Linux signal-handler code and memory registries;
- the guard-page reservation registry.

The fixed process registries stay bounded because signal handlers cannot allocate
or perform unbounded work. See `docs/native-memory-limits.md`.

### Resource quota

A resource quota limits measurable host use. Zero means unbounded unless an API
states otherwise. Quota failures use `ResourceLimitError` and identify a scope,
resource, requested amount, and limit.

Implemented runtime configuration quotas include:

- `WithMemoryLimitPages`: live pages per memory, checked at instantiation and by
  `memory.grow`; zero adds no quota;
- `WithMaxInstanceMetadataBytes`: validated off-heap metadata bytes per instance;
- `WithMaxCompiledMetadataBytes`: owned execution-snapshot metadata, checked
  before cloning; zero selects the 256 MiB default. Low-level callers use
  `InstantiateOptions.MaxCompiledMetadataBytes`. Counts include destination
  copies of aliased slices and allocation-rounding allowances;
- `WithMaxModuleBytes`: input Wasm bytes per compilation;
- `WithMaxNativeCodeBytes`: generated native code bytes per module;
- instance count, aggregate memory, and native mapping limits already exposed by
  the runtime configuration.

Declared Memory64 maxima are metadata. They are not charged as allocated bytes.

### Implementation limit

An implementation limit is a current internal representation boundary. It is not
a permission denial and does not enter resource-usage telemetry. It uses
`ErrImplementationLimit` or an `ImplementationLimitError` where the error crosses
the public API.

Hard implementation limits that remain after this change:

| Limit | Class | Code | Reason and removal work |
|---|---|---|---|
| Memory64 executable minimum/reservation: 65,535 pages | Implementation limit | `src/core/compiler/frontend/frontend.go`, `src/wago/api.go` | The current execution cache and reservation path use a bounded 32-bit page representation. Remove it with platform-specific sparse 64-bit reservations and full growth accounting. |
| Observable Table64 capacity: 16,384 entries | Implementation limit | `src/core/compiler/frontend/frontend.go` | The current table backend uses contiguous descriptors. Remove it with dynamic or sparse Table64 storage and growth. |
| Wrapper-ABI proper-tail scratch: 16 slots | Implementation limit | `src/core/runtime/abi/basedata.go`, AMD64/ARM64 tail lowering | The scratch bank is embedded in fixed basedata. Remove it with an instance-owned spill area that survives context switches without growing the native stack. |
| Mixed staged `ref.test` table count: 3 | Unsupported staged shape | `src/wago/gc_ref_test_table.go` | The staged product assigns distinct anyref, funcref, and externref behavior by table position. General support needs type-driven per-table ownership metadata. |
| GC-aware suspended host activations: 8; inline GC args/results: 7/2 | Implementation limit | `src/wago/gc_host_activation_*.go`, `src/wago/reference_store.go` | Exact parked-frame discovery still uses fixed activation records. General support needs dynamic stable activation/root areas with architecture-specific unwind validation. |
| AMD64 GC wrapper recovery envelope: 512 bytes | Safety invariant for the current scanner | `src/wago/gc_host_activation_linux_amd64.go` | The scanner must prove the return PC and frame shape. Widen only with matching unwind metadata. |
| GC liveness working arena: 64 MiB | Compile resource safety bound | `src/wago/gc_frame_liveness.go` | This bounds temporary bitmap/CFG memory. A future `MaxCompileWorkingBytes` allocator/accounting layer should replace the fixed budget. |
| Synchronous host-call slot count: 65,535 per direction | Representation invariant | `src/core/runtime/hostcall_layout.go` | The parked control frame encodes each slot count in 16 bits. |
| Function parameters plus locals: 65,535 | Representation invariant | `src/core/compiler/wasm/validate.go` | Compiler metadata uses the WebAssembly implementation's current uint16-compatible index range. Native frame safety is checked independently. |

### Unsupported feature or shape

Valid Wasm that Wago cannot execute reports `ErrUnsupported`. Examples include a
proposal disabled in `RuntimeConfig` and staged products that do not yet have the
required ownership or native-root behavior.

Unsupported and implementation-limit errors must not wrap
`ErrPermissionDenied`.

## Fast paths and overflow paths

The following fixed capacities are now fast paths, not semantic limits:

- the 1 MiB instance-arena value is only the one-entry mmap cache threshold;
- the first 64 synchronous host-call slots keep the existing control-frame
  layout, while wider calls use the checked extension;
- the first 64 public GC result and argument roots stay inline, with reusable
  dynamic overflow storage;
- `array.new_fixed` keeps ordinary helper lowering and uses an off-stack spill
  path for wider constructors;
- exact GC root vectors are variable-sized and may contain more than 1,024 live
  roots when the native frame is otherwise safe.

## Compile accounting gaps

`MaxModuleBytes` and `MaxNativeCodeBytes` are exact. Wago does not yet expose
`MaxCompileWorkingBytes`: compiler allocations use ordinary Go allocation and
worker-local scratch without a common charged allocator. Adding an inaccurate
counter would give false assurance. The existing 64 MiB GC-liveness arena and
function-worker policy remain explicit temporary controls until compilation uses
a shared accounting interface.

The executable `.wago` codec is version 2. Version-1 executable artifacts are
rejected because their generated `memory.grow` code does not enforce the
per-instance directory-resident runtime page quota.

Compilation cancellation remains tied to APIs that already accept
`context.Context`; no per-instruction fuel check is added to generated hot paths.

## Invocation duration

`Policy.MaxInvokeDuration` is deprecated. A nonzero value reports
`ErrUnsupported` instead of a permission failure. Use the context-aware call API
with a deadline. Linux uses asynchronous native interruption; other supported
products use their existing safe checkpoints.
