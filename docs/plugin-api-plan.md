# Final-product Wago extension API plan

The final product should feel like this:

```go
rt := wago.NewRuntime()

rt.Use(timer.Ext())
rt.Use(metrics.Ext())
rt.Use(process.Ext())
rt.Use(mailbox.Ext())
rt.Use(http.Ext(http.WithClient(client)))

mod, err := rt.Compile(wasmBytes)
if err != nil {
    return err
}

class, err := rt.Class(mod, wago.ClassOptions{
    Pool: wago.PoolOptions{
        MinInstances: 1024,
        MaxInstances: 100_000,
        Reset:        wago.ResetMemorySnapshot,
    },
})
if err != nil {
    return err
}

pid, err := rt.Spawn(ctx, class, wago.SpawnOptions{
    Entry: "main",
})
```

And for a simple user who does not care about actors:

```go
rt := wago.NewRuntime()
rt.Use(timer.Ext())

mod, _ := rt.Compile(wasmBytes)
inst, _ := rt.Instantiate(mod)

out, err := inst.Invoke(ctx, "run", wago.I32(123))
```

That is the whole vibe: **simple first, absurdly powerful underneath**. The API should not punish normal people for not wanting to become runtime architects before lunch.

---

# Core design principle

Wago extensions should be built around one idea:

```text
Extensions register capabilities into a runtime.
The runtime owns orchestration.
Modules consume those capabilities through imports, hooks, policies, and optionally compiler integration.
```

Do **not** make extensions directly mutate random runtime internals. That feels powerful until someone’s logging plugin redefines memory growth. Humanity has suffered enough.

---

# The four-layer model

```text
Layer 1: Runtime extensions
  Host imports, hooks, resources, capabilities, policies.

Layer 2: Module extensions
  Wasm transforms, validation rules, required capabilities, metadata.

Layer 3: Process extensions
  Actors, mailboxes, timers, supervision, distributed routing.

Layer 4: Compiler extensions
  Intrinsics, backend selection, instrumentation, trusted codegen.
```

Most users only touch layer 1.

Most extension authors touch layer 1 and maybe layer 2.

Only Wago/core trusted packages touch layer 4.

---

# Why this fits Wago

Wago already has the right bones:

* `Compile` is separate from `Instantiate`, so extensions can hook compile-time and runtime separately.
* `InstantiateWithOptions` already accepts imports and GC config, which is exactly where extension-provided host APIs belong.
* Host imports already have a sync path that can access the calling instance’s memory through `HostModule.Memory()`, which is what serious extensions need.
* Compiled code is already cached/refcounted, so class/pool/process semantics can share native code instead of duplicating executable mappings like animals.

So the extension API should wrap and organize what Wago already wants to be.

---

# Final public API shape

## Runtime

```go
type Runtime struct {
    // internal
}

func NewRuntime(opts ...RuntimeOption) *Runtime

func (rt *Runtime) Use(ext Extension, opts ...UseOption) error
func (rt *Runtime) Compile(wasmBytes []byte, opts ...CompileOption) (*Module, error)
func (rt *Runtime) Load(blob []byte, opts ...LoadOption) (*Module, error)

func (rt *Runtime) Instantiate(ctx context.Context, mod *Module, opts ...InstantiateOption) (*Instance, error)
func (rt *Runtime) Class(mod *Module, opts ClassOptions) (*Class, error)

func (rt *Runtime) Close() error
func (rt *Runtime) Extensions() []ExtensionInfo
func (rt *Runtime) Capabilities() []Capability
```

`Runtime` is the high-level entry point. Existing package-level APIs can remain as low-level convenience.

```go
c, err := wago.Compile(wasmBytes)
in, err := wago.Instantiate(c, imports)
```

That should stay. The extension runtime is an additive higher layer.

---

## Module

```go
type Module struct {
    // wraps *Compiled plus extension metadata
}

func (m *Module) Compiled() *Compiled
func (m *Module) Exports() []string
func (m *Module) Imports() []ImportSpec
func (m *Module) RequiredCapabilities() []Capability
func (m *Module) Metadata() ModuleMetadata
func (m *Module) Close() error
```

`Module` is the runtime-aware wrapper over `Compiled`.

It should know:

```text
compiled code
declared imports
required capabilities
extension manifest
transform history
debug metadata
```

---

## Instance

```go
type Instance struct {
    // wraps current low-level *wago.Instance
}

func (in *Instance) Invoke(ctx context.Context, export string, args ...Value) ([]Value, error)
func (in *Instance) Memory() *Memory
func (in *Instance) Global(name string) (Value, error)
func (in *Instance) SetGlobal(name string, value Value) error
func (in *Instance) Close() error

func (in *Instance) Raw() *wago.Instance
```

Use `context.Context` in the high-level API. This gives timers, cancellation, deadlines, tracing, process scheduling, and host import cancellation somewhere sane to live. Wild concept, I know.

---

## Class

A `Class` is the deployable unit for massive instance pools and actor processes.

```go
type Class struct {
    // module + pool + policy + extension state
}

type ClassOptions struct {
    Name   string
    Pool   PoolOptions
    Policy Policy
    Config ProcessConfig
}

func (rt *Runtime) Class(mod *Module, opts ClassOptions) (*Class, error)

func (c *Class) Instantiate(ctx context.Context) (*Instance, error)
func (c *Class) Acquire(ctx context.Context) (*Lease, error)
func (c *Class) Close() error
```

A class is basically:

```text
compiled module
plus import environment
plus policy
plus pool
plus reset strategy
```

This is the foundation for “compile once, spawn a galaxy.”

---

# Extension interface

This is the most important part.

I would **not** use a giant interface with 400 methods. That is not complete. That is a cry for help.

Use one simple interface:

```go
type Extension interface {
    Info() ExtensionInfo
    Register(reg *Registry) error
}
```

That’s it.

Everything else happens through the registry.

```go
type ExtensionInfo struct {
    ID          string
    Name        string
    Version     string    // extension version (semver)
    Description string
    Stability   Stability

    // Provenance.
    Homepage   string   // project or docs URL
    Repository string   // source repo, e.g. https://github.com/acme/wago-redis
    License    string   // SPDX identifier, e.g. "Apache-2.0"
    Authors    []string // "Name <email>" entries
    Keywords   []string // discovery tags

    // Compat records supported wago versions, platforms, and TinyGo support.
    Compat Compatibility
}

type Compatibility struct {
    // Engines maps an engine/toolchain name to a semver constraint, npm-style.
    // Well-known keys: "wago" (runtime version, enforced at Use time), "tinygo"
    // (TinyGo support), "go" (minimum Go toolchain). Constraints are full semver
    // 2.0.0 ranges (src/core/semver): comparators, ^, ~, x-ranges, hyphen, ||, *.
    Engines   map[string]string
    Platforms []string // supported GOOS/GOARCH pairs, e.g. "linux/amd64"; empty = any
}
```

`wago plugin inspect <name>` renders all of this (with `--json` for the full
machine-readable config); `wago plugin list` shows a compatibility hint. Example:

```go
type TimerExtension struct{}

func (TimerExtension) Info() wago.ExtensionInfo {
    return wago.ExtensionInfo{
        ID:          "wago.timer",
        Name:        "Timer",
        Version:     "1.0.0",
        Description: "Monotonic and wall-clock time for Wasm guests.",
        Stability:   wago.Stable,
        Homepage:    "https://github.com/wago-org/wago",
        License:     "Apache-2.0",
        Authors:     []string{"The wago authors"},
        Keywords:    []string{"time", "clock"},
        Compat:      wago.Compatibility{Engines: map[string]string{"wago": ">=0.1.0", "tinygo": "*"}, Platforms: []string{"linux/amd64"}},
    }
}

func (TimerExtension) Register(reg *wago.Registry) error {
    reg.Capability(wago.CapTimer)

    reg.ImportModule("wago_timer").
        Func("now_unix_ms", timerNowUnixMS).
        Results(wago.I64)

    reg.ImportModule("wago_timer").
        Func("now_monotonic_ns", timerNowMonotonicNS).
        Results(wago.I64)

    return nil
}
```

This is lovable because extension authors learn **one** method: `Register`.

---

# Registry API

The registry is the extension builder surface.

```go
type Registry struct {
    // internal
}

func (r *Registry) Capability(cap Capability, opts ...CapabilityOption)
func (r *Registry) ImportModule(name string) *ImportModuleBuilder
func (r *Registry) Hooks() *HookRegistry
func (r *Registry) Resources() *ResourceRegistry
func (r *Registry) Policies() *PolicyRegistry
func (r *Registry) Memory() *MemoryRegistry
func (r *Registry) Processes() *ProcessRegistry
func (r *Registry) Compiler() *CompilerRegistry
```

This keeps the API discoverable:

```go
reg.ImportModule(...)
reg.Hooks().BeforeInvoke(...)
reg.Resources().Kind(...)
reg.Compiler().Intrinsic(...)
```

No mystery subinterface bingo.

---

# Import registration

## Host function builder

```go
type ImportModuleBuilder struct {
    // internal
}

func (m *ImportModuleBuilder) Func(name string, fn any) *ImportFuncBuilder
```

Then:

```go
reg.ImportModule("wago_timer").
    Func("now_unix_ms", func() int64 {
        return time.Now().UnixMilli()
    }).
    Results(wago.I64)
```

For memory-accessing functions:

```go
reg.ImportModule("wago_env").
    Func("log", func(mod wago.HostModule, ptr uint32, len uint32) int32 {
        msg := mod.Memory()[ptr : ptr+len]
        log.Printf("%s", msg)
        return 0
    }).
    Params(wago.I32, wago.I32).
    Results(wago.I32)
```

This maps naturally to Wago’s existing sync host import model.

## Supported host signatures

Allow both:

```go
func(a int32, b int32) int32
func(mod wago.HostModule, ptr uint32, len uint32) int32
```

And low-level:

```go
type SyncHostFunc func(m HostModule, params, results []uint64)
```

So advanced authors can avoid reflection and allocation.

---

# Import collision rules

This needs to be boring and deterministic.

```text
1. Extension import namespaces must be unique.
2. User imports cannot override reserved wago_* modules unless explicitly allowed.
3. Later extensions cannot override earlier extension imports by default.
4. Runtime options can allow controlled override for tests.
```

Reserved namespaces:

```text
wago_process
wago_mailbox
wago_timer
wago_metrics
wago_log
wago_fs
wago_net
wago_http
wago_kv
wago_crypto
wago_debug
wago_runtime
```

API:

```go
rt := wago.NewRuntime(
    wago.WithImportOverridePolicy(wago.NoExtensionOverrides),
)
```

Test-only escape hatch:

```go
rt := wago.NewRuntime(
    wago.WithImportOverridePolicy(wago.AllowTestOverrides),
)
```

---

# Hook system

Hooks should be complete, but not ridiculous.

## Runtime hooks

```go
type RuntimeHooks struct {
    OnRuntimeStart []func(*RuntimeContext) error
    OnRuntimeClose []func(*RuntimeContext)
}
```

## Compile hooks

```go
type CompileHooks struct {
    BeforeDecode    []func(*CompileContext, []byte) ([]byte, error)
    AfterDecode     []func(*CompileContext, *wasm.Module) error
    BeforeValidate  []func(*CompileContext, *wasm.Module) error
    AfterValidate   []func(*CompileContext, *wasm.Module) error
    BeforeCompile   []func(*CompileContext, *wasm.Module) error
    AfterCompile    []func(*CompileContext, *Module) error
}
```

Compile pipeline:

```text
raw wasm bytes
  -> BeforeDecode
decode
  -> AfterDecode
  -> transforms
  -> BeforeValidate
validate
  -> AfterValidate
  -> BeforeCompile
compile
  -> AfterCompile
```

The important rule:

```text
Any module transform must happen before final validation.
```

No plugin gets to smuggle invalid Wasm into codegen. We live in a society, allegedly.

## Instantiate hooks

```go
type InstantiateHooks struct {
    BeforeInstantiate []func(*InstantiateContext) error
    AfterInstantiate  []func(*InstantiateContext, *Instance) error
}
```

Use for:

```text
capability checks
memory plan selection
resource attachment
metrics
debug labels
initial snapshots
```

## Invoke hooks

```go
type InvokeHooks struct {
    BeforeInvoke []func(*InvokeContext) error
    AfterInvoke  []func(*InvokeContext, []Value, error)
}
```

Use for:

```text
metrics
tracing
fuel accounting
policy checks
panic/trap reporting
debug stepping later
```

## Instance lifecycle hooks

```go
type InstanceHooks struct {
    BeforeReset []func(*InstanceContext) error
    AfterReset  []func(*InstanceContext) error
    BeforeClose []func(*InstanceContext)
    AfterClose  []func(*InstanceContext)
}
```

Use for pools.

## Process hooks

```go
type ProcessHooks struct {
    OnSpawn          []func(*ProcessContext, PID)
    OnExit           []func(*ProcessContext, PID, ExitReason)
    OnMessageSend    []func(*MessageContext)
    OnMessageReceive []func(*MessageContext)
    OnLink           []func(*ProcessContext, PID, PID)
    OnMonitor        []func(*ProcessContext, PID, PID)
}
```

Use for actor/process extensions, distributed routing, debug visualizers, and supervisors.

---

# Context objects

Every hook gets a context object. No global sludge.

```go
type RuntimeContext struct {
    Runtime *Runtime
    Logger  Logger
}

type CompileContext struct {
    Runtime *Runtime
    ModuleID ModuleID
    Metadata map[string]any
}

type InstantiateContext struct {
    Runtime *Runtime
    Module  *Module
    Imports Imports
    Policy  Policy
    Metadata map[string]any
}

type InvokeContext struct {
    Runtime  *Runtime
    Instance *Instance
    Export   string
    Args     []Value
    Start    time.Time
    Metadata map[string]any
}
```

Hooks should be allowed to stash extension-local metadata.

```go
ctx.Metadata["wago.metrics.start"] = time.Now()
```

For a nicer API:

```go
ctx.Set(extID, key, value)
value, ok := ctx.Get(extID, key)
```

---

# Capabilities and policy

This is mandatory for a complete extension API.

## Capability

```go
type Capability string

const (
    CapTimerRead       Capability = "timer.read"
    CapProcessSpawn    Capability = "process.spawn"
    CapProcessKill     Capability = "process.kill"
    CapMailboxSend     Capability = "mailbox.send"
    CapMailboxReceive  Capability = "mailbox.receive"
    CapNetworkOutbound Capability = "net.outbound"
    CapFilesystemRead  Capability = "fs.read"
    CapFilesystemWrite Capability = "fs.write"
    CapHTTPClient      Capability = "http.client"
    CapKVRead          Capability = "kv.read"
    CapKVWrite         Capability = "kv.write"
    CapMetricsWrite    Capability = "metrics.write"
    CapCompilerCodegen Capability = "compiler.codegen"
)
```

## Policy

```go
type Policy struct {
    AllowedCapabilities []Capability
    DeniedCapabilities  []Capability

    MaxMemoryBytes      uint64
    MaxTableEntries     uint32
    MaxInstances        uint32
    MaxProcesses        uint32
    MaxMailboxMessages  uint32
    MaxMailboxBytes     uint64
    MaxInvokeDuration   time.Duration
}
```

Usage:

```go
inst, err := rt.Instantiate(ctx, mod,
    wago.WithPolicy(wago.Policy{
        AllowedCapabilities: []wago.Capability{
            wago.CapTimerRead,
            wago.CapMetricsWrite,
        },
        MaxMemoryBytes:    64 << 20,
        MaxInvokeDuration: 50 * time.Millisecond,
    }),
)
```

Extensions check through the runtime:

```go
if err := ctx.Require(wago.CapNetworkOutbound); err != nil {
    return err
}
```

This makes host APIs safe by default.

---

# Resource and handle system

Extensions need handles. PIDs, sockets, timers, files, KV connections, HTTP clients, subscriptions.

Do not pass Go pointers to Wasm. Use integer handles.

```go
type Handle uint64

type Resource interface {
    Close() error
}

type ResourceRegistry struct {
    // internal
}

func (r *ResourceRegistry) Kind(name string, opts ...ResourceKindOption)
```

Runtime owns handle tables:

```go
type HandleTable struct {
    // internal
}

func (h *HandleTable) Insert(kind string, value Resource) Handle
func (h *HandleTable) Get(handle Handle, kind string) (Resource, bool)
func (h *HandleTable) Close(handle Handle) error
```

Guest sees:

```wat
(func $timer_cancel (param i64) (result i32))
(func $file_close   (param i64) (result i32))
(func $pid_kill     (param i64 i32) (result i32))
```

Handle layout:

```text
u64 handle
upper bits: kind/generation
lower bits: slot index
```

Use generation counters so stale handles fail cleanly.

---

# Guest ABI

The guest ABI should be tiny and consistent.

## Values

```text
i32 for status codes
i64 for handles, PIDs, timestamps
ptr + len for guest buffers
ptr + cap for output buffers
```

## Return convention

Use status codes:

```text
0  = ok
1  = not found
2  = permission denied
3  = invalid handle
4  = buffer too small
5  = would block
6  = timed out
7  = trap/error
```

For variable-length output:

```wat
(import "wago_mailbox" "recv"
  (func $recv
    (param $buf_ptr i32)
    (param $buf_cap i32)
    (param $timeout_ms i64)
    (result i32))) ;; bytes read or negative error
```

Or more explicit:

```wat
(import "wago_mailbox" "recv"
  (func $recv
    (param $buf_ptr i32)
    (param $buf_cap i32)
    (param $out_len_ptr i32)
    (param $timeout_ms i64)
    (result i32)))
```

I’d pick explicit status + out pointer for anything nontrivial:

```text
status result
output length written to memory
```

It is less cute, but APIs that are too cute tend to become haunted.

---

# Guest bindings

Developer experience matters here.

Provide official bindings for:

```text
AssemblyScript
TinyGo
Rust
C
```

Example AssemblyScript:

```ts
import { nowMonotonicNs } from "@wago/timer";
import { send, recv } from "@wago/mailbox";

export function main(): void {
  let t = nowMonotonicNs();
}
```

TinyGo:

```go
package main

import "github.com/wago-org/guest/timer"

func main() {
    now := timer.NowMonotonicNS()
    _ = now
}
```

Rust:

```rust
let now = wago_timer::now_monotonic_ns();
```

Bindings should be generated from an extension manifest.

---

# Extension manifest

Each extension should be able to expose a manifest.

```go
type ExtensionManifest struct {
    Info         ExtensionInfo
    Imports      []ImportSpec
    Capabilities []CapabilitySpec
    Resources    []ResourceSpec
}
```

Import spec:

```go
type ImportSpec struct {
    Module  string
    Name    string
    Params  []ValType
    Results []ValType
    Capability Capability
    Docs    string
}
```

Use this for:

```text
docs generation
guest bindings
validation errors
debugging
CLI inspection
```

CLI:

```bash
wago ext list
wago ext inspect wago.timer
wago module imports app.wasm
wago module capabilities app.wasm
```

Nice error:

```text
module imports wago_http.fetch, but runtime has no extension providing module "wago_http"
hint: rt.Use(http.Ext(...))
```

That’s lovable. Slightly less lovable than a nap, but close.

---

# Built-in extension packages

Use boring package names.

```text
wago/ext/timer
wago/ext/log
wago/ext/metrics
wago/ext/process
wago/ext/mailbox
wago/ext/supervisor
wago/ext/http
wago/ext/fs
wago/ext/kv
wago/ext/crypto
wago/ext/debug
wago/ext/wasi
```

Example:

```go
import (
    "github.com/wago-org/wago"
    "github.com/wago-org/wago/ext/timer"
    "github.com/wago-org/wago/ext/process"
)
```

Not `grafts`, not `kits`, not `arcane_dragon_modules`. Save the poetry for tier names. APIs should be obvious.

---

# Built-in extension set

## `timer`

Imports:

```text
wago_timer.now_unix_ms() -> i64
wago_timer.now_monotonic_ns() -> i64
wago_timer.sleep_ms(ms: i64) -> i32
wago_timer.send_after_ms(pid: i64, ms: i64, ptr: i32, len: i32) -> i64
wago_timer.cancel(handle: i64) -> i32
```

## `log`

```text
wago_log.write(level: i32, ptr: i32, len: i32) -> i32
```

## `metrics`

```text
wago_metrics.counter_add(name_ptr, name_len, delta: i64) -> i32
wago_metrics.histogram_observe(name_ptr, name_len, value: f64) -> i32
```

## `process`

```text
wago_process.self() -> i64
wago_process.spawn(class_ptr, class_len, entry_ptr, entry_len, arg_ptr, arg_len) -> i64
wago_process.kill(pid: i64, reason: i32) -> i32
wago_process.link(pid: i64) -> i32
wago_process.unlink(pid: i64) -> i32
wago_process.monitor(pid: i64) -> i64
```

## `mailbox`

```text
wago_mailbox.send(pid: i64, ptr: i32, len: i32) -> i32
wago_mailbox.recv(buf_ptr: i32, buf_cap: i32, out_len_ptr: i32, timeout_ms: i64) -> i32
wago_mailbox.try_recv(buf_ptr: i32, buf_cap: i32, out_len_ptr: i32) -> i32
wago_mailbox.len() -> i32
```

## `supervisor`

Host-level first, guest-level later.

```go
sup := supervisor.New(wago.OneForOne,
    supervisor.Child("worker", workerClass),
)
rt.Use(sup)
```

## `http`

```text
wago_http.request(req_ptr, req_len, out_handle_ptr) -> i32
wago_http.read_body(handle, ptr, cap, out_len_ptr) -> i32
wago_http.close(handle) -> i32
```

## `kv`

```text
wago_kv.get(key_ptr, key_len, out_ptr, out_cap, out_len_ptr) -> i32
wago_kv.set(key_ptr, key_len, val_ptr, val_len) -> i32
wago_kv.delete(key_ptr, key_len) -> i32
```

## `debug`

```text
wago_debug.breakpoint(code: i32) -> i32
wago_debug.trace_event(ptr, len) -> i32
```

---

# Process model

This is the Lunatic-like layer.

## Runtime API

```go
type PID uint64

func (rt *Runtime) Spawn(ctx context.Context, class *Class, opts SpawnOptions) (PID, error)
func (rt *Runtime) Send(ctx context.Context, pid PID, msg []byte) error
func (rt *Runtime) Kill(ctx context.Context, pid PID, reason ExitReason) error
func (rt *Runtime) Monitor(ctx context.Context, pid PID) (<-chan ExitEvent, error)
```

## Spawn options

```go
type SpawnOptions struct {
    Entry string
    Args  []Value
    Init  []byte

    Name  string
    Policy Policy

    LinkToParent bool
}
```

## Process internals

```go
type Process struct {
    PID      PID
    Class    *Class
    Instance *Lease
    Mailbox  *Mailbox
    Policy   Policy
    Links    map[PID]struct{}
    Monitors map[PID]struct{}
}
```

Processes are not green threads. They are Wago instance leases with mailboxes and lifecycle. That is simpler, more honest, and much less likely to turn your stack management into a Victorian illness.

---

# Pool integration

A class owns a pool.

```go
type PoolOptions struct {
    MinInstances int
    MaxInstances int
    Reset        ResetPolicy

    MaxIdleTime time.Duration
    WarmStart   bool
}

type ResetPolicy int

const (
    ResetReinstantiate ResetPolicy = iota
    ResetMemorySnapshot
    ResetCopyOnWrite
)
```

Class usage:

```go
worker, err := rt.Class(mod, wago.ClassOptions{
    Name: "worker",
    Pool: wago.PoolOptions{
        MinInstances: 4096,
        MaxInstances: 100_000,
        Reset:        wago.ResetMemorySnapshot,
    },
})
```

This is where Wago can become nasty-fast.

---

# Module transforms

Extensions can transform Wasm before final validation.

```go
reg.Hooks().AfterDecode(func(ctx *CompileContext, m *wasm.Module) error {
    return instrumentModule(m)
})
```

Or a nicer builder:

```go
reg.Transform("inject-metrics", func(ctx *CompileContext, m *wasm.Module) error {
    return metrics.Inject(m)
})
```

Allowed transforms:

```text
add imports
rewrite imports
inject function calls
add custom sections
add wrappers
instrument memory accesses
insert fuel checks
```

Disallowed:

```text
skip validation
mutate compiled code after validation without revalidation
change runtime ABI
lie about signatures
```

---

# Compiler extension API

This should exist, but separate from normal extensions.

```go
type CompilerExtension interface {
    Info() ExtensionInfo
    RegisterCompiler(reg *CompilerRegistry) error
}
```

Register separately:

```go
rt.UseCompiler(simd.Codegen())
rt.UseCompiler(crypto.Intrinsics())
```

Not:

```go
rt.Use(randomDownloadedCompilerPlugin())
```

That way normal runtime plugins stay safe.

## Compiler registry

```go
type CompilerRegistry struct {
    // internal
}

func (r *CompilerRegistry) Backend(name string, backend codegen.Backend[*wasm.Module])
func (r *CompilerRegistry) Intrinsic(spec IntrinsicSpec)
func (r *CompilerRegistry) Instrumentation(pass InstrumentationPass)
func (r *CompilerRegistry) ObjectPass(pass ObjectPass)
```

## Intrinsics

This is the most important compiler extension feature.

```go
type IntrinsicSpec struct {
    ImportModule string
    ImportName   string
    Params       []ValType
    Results      []ValType
    Lower        IntrinsicLowering
}
```

Example:

```go
reg.Compiler().Intrinsic(wago.IntrinsicSpec{
    ImportModule: "wago_crypto",
    ImportName:   "blake3_chunk",
    Params:       []wago.ValType{wago.I32, wago.I32, wago.I32},
    Results:      []wago.ValType{wago.I32},
    Lower:        lowerBlake3ChunkAMD64,
})
```

Guest sees a normal import.

Without compiler extension:

```text
guest import -> host call
```

With compiler extension:

```text
guest import -> inlined native lowering
```

That is a lovely trick. Ergonomic, compatible, and fast.

## Backend selection

```go
rt := wago.NewRuntime(
    wago.WithBackend("railshot"),
)

rt.UseCompiler(mybackend.Ext())
```

Possible backends:

```text
railshot
debug interpreter
optimizing backend
gpu backend
aot backend
instrumented backend
```

Keep this trusted.

---

# Safety boundary

Normal extensions can:

```text
register imports
register hooks
register resources
declare capabilities
transform modules before validation
observe lifecycle events
enforce policy
```

Normal extensions cannot:

```text
patch machine code
change ABI layout
override trap handling
mutate register allocation
change stack layout
install arbitrary signal handlers
override reserved imports
skip validation
```

Compiler extensions can do some dangerous things, but only through trusted APIs.

---

# Error model

Use typed errors.

```go
var (
    ErrPermissionDenied = errors.New("wago: permission denied")
    ErrMissingImport    = errors.New("wago: missing import")
    ErrInvalidHandle    = errors.New("wago: invalid handle")
    ErrExtensionConflict = errors.New("wago: extension conflict")
)
```

Rich error:

```go
type ExtensionError struct {
    Extension string
    Operation string
    Err       error
}

func (e *ExtensionError) Error() string {
    return fmt.Sprintf("wago extension %s: %s: %v", e.Extension, e.Operation, e.Err)
}
```

Example message:

```text
wago extension wago.http: import registration failed:
module "wago_http" function "request" conflicts with extension "custom.http"
```

This matters. Bad errors are unpaid technical debt with a user interface.

---

# Developer experience checklist

## Good DX features

```text
rt.Use(ext)
one Register method
clear import collision errors
reserved namespace protection
extension manifest inspection
generated guest bindings
typed host functions
low-level SyncHostFunc escape hatch
test harness
CLI inspection
stable capability names
context-aware invoke API
```

## Test harness

```go
package myext_test

func TestExtension(t *testing.T) {
    rt := wagotest.NewRuntime(t)
    rt.Use(myext.Ext())

    wagotest.RequireImport(t, rt, "wago_myext", "do_thing")
    wagotest.RequireCapability(t, rt, myext.CapDoThing)
}
```

For guest tests:

```go
inst := wagotest.MustInstantiateWat(t, rt, `
(module
  (import "wago_timer" "now_unix_ms" (func $now (result i64)))
  (func (export "run") (result i64)
    call $now))
`)
```

## Debug inspection

```go
rt.Extensions()
rt.Capabilities()
mod.RequiredCapabilities()
mod.Imports()
```

CLI:

```bash
wago ext list
wago ext inspect wago.timer
wago inspect imports app.wasm
wago inspect capabilities app.wasm
```

---

# Configuration

Prefer typed Go options over generic maps.

```go
rt.Use(http.Ext(
    http.WithClient(client),
    http.WithAllowedHosts("api.example.com"),
    http.WithTimeout(2*time.Second),
))
```

Avoid this as the main Go API:

```go
rt.Use(http.Ext(), wago.Config{
    "timeout": "2s",
})
```

Stringly typed config is where bugs go to reproduce.

But still allow manifests/config files for CLI:

```toml
[extensions.timer]

[extensions.http]
allowed_hosts = ["api.example.com"]
timeout = "2s"
```

---

# Versioning and stability

```go
type Stability string

const (
    Experimental Stability = "experimental"
    Stable       Stability = "stable"
    Deprecated   Stability = "deprecated"
)
```

Runtime should reject incompatible extensions:

```go
if ext.Info().MinWago > currentVersion {
    return fmt.Errorf(...)
}
```

Extension IDs should be stable:

```text
wago.timer
wago.process
wago.mailbox
company.redis
company.auth
```

Import module names should be stable:

```text
wago_timer
wago_process
company_redis
```

---

# Suggested file/package layout

```text
src/wago/runtime.go
src/wago/extension.go
src/wago/registry.go
src/wago/hooks.go
src/wago/policy.go
src/wago/resource.go
src/wago/module_runtime.go
src/wago/class.go
src/wago/pool.go
src/wago/process.go

src/wago/ext/timer
src/wago/ext/log
src/wago/ext/metrics
src/wago/ext/process
src/wago/ext/mailbox
src/wago/ext/http
src/wago/ext/kv
src/wago/ext/wasi

src/wago/compilerext
```

Or if you want shorter import paths later:

```text
ext/timer
ext/process
ext/mailbox
```

But keeping them under `src/wago/ext/...` while private is fine.

> **Implemented layout (2026-07):** the built-in plugins ship under top-level
> `plugins/` — `github.com/wago-org/wago/plugins/{timer,log,metrics}` — each
> importing the root `github.com/wago-org/wago` facade, plus a `plugins/exttest`
> test helper. The process/mailbox/supervisor machinery lives in the core
> `wago` package rather than as separate plugin packages.

---

# Final API example: extension author

```go
package timer

type Extension struct {
    clock Clock
}

func Ext(opts ...Option) *Extension {
    e := &Extension{clock: realClock{}}
    for _, opt := range opts {
        opt(e)
    }
    return e
}

func (e *Extension) Info() wago.ExtensionInfo {
    return wago.ExtensionInfo{
        ID:          "wago.timer",
        Name:        "Timer",
        Version:     "1.0.0",
        Description: "Time functions for Wago guests.",
        MinWago:     "0.1.0",
        Stability:   wago.Stable,
    }
}

func (e *Extension) Register(reg *wago.Registry) error {
    reg.Capability(CapRead)

    reg.ImportModule("wago_timer").
        Func("now_unix_ms", func() int64 {
            return e.clock.Now().UnixMilli()
        }).
        Results(wago.I64).
        Capability(CapRead)

    reg.ImportModule("wago_timer").
        Func("now_monotonic_ns", func() int64 {
            return e.clock.MonotonicNS()
        }).
        Results(wago.I64).
        Capability(CapRead)

    return nil
}
```

User:

```go
rt := wago.NewRuntime()
rt.Use(timer.Ext())

mod, _ := rt.Compile(wasmBytes)
inst, _ := rt.Instantiate(context.Background(), mod)

out, err := inst.Invoke(context.Background(), "run")
```

---

# Final API example: process + mailbox

```go
rt := wago.NewRuntime()

rt.Use(timer.Ext())
rt.Use(process.Ext())
rt.Use(mailbox.Ext())
rt.Use(metrics.Ext())

mod, err := rt.Compile(workerWasm)
if err != nil {
    return err
}

worker, err := rt.Class(mod, wago.ClassOptions{
    Name: "worker",
    Pool: wago.PoolOptions{
        MinInstances: 256,
        MaxInstances: 20_000,
        Reset:        wago.ResetMemorySnapshot,
    },
    Policy: wago.Policy{
        AllowedCapabilities: []wago.Capability{
            process.CapSpawn,
            mailbox.CapSend,
            mailbox.CapReceive,
            timer.CapRead,
        },
        MaxMemoryBytes:     32 << 20,
        MaxMailboxMessages: 1024,
    },
})
if err != nil {
    return err
}

pid, err := rt.Spawn(ctx, worker, wago.SpawnOptions{
    Entry: "main",
})
if err != nil {
    return err
}

err = rt.Send(ctx, pid, []byte("hello"))
```

Guest:

```ts
import { self } from "@wago/process";
import { recv, send } from "@wago/mailbox";

export function main(): void {
  let me = self();
  let msg = recv();
}
```

---

# “Simple, lovable, complete” evaluation

## Simple

A user learns:

```go
rt := wago.NewRuntime()
rt.Use(ext)
mod := rt.Compile(...)
inst := rt.Instantiate(...)
inst.Invoke(...)
```

An extension author learns:

```go
type Extension interface {
    Info() ExtensionInfo
    Register(*Registry) error
}
```

That is simple.

## Lovable

Lovable parts:

```text
typed host functions
one Register method
good errors
guest bindings
CLI inspection
reserved namespaces
test helpers
safe defaults
context-aware invoke
```

This is the difference between “technically extensible” and “people will actually use it without muttering.”

## Complete

It supports:

```text
host APIs
runtime hooks
module transforms
policies
capabilities
resources/handles
processes
mailboxes
timers
supervision
pooling
compiler intrinsics
backend replacement
observability
distributed later
```

That is complete without dumping every dangerous internal into the public API like a yard sale.

---

# Implementation phases

## Phase 1: Extension spine

```text
Runtime
Extension
Registry
Import registration
Basic hooks
```

## Phase 2: Runtime wrappers

```text
Module
Instance
context-aware Invoke
extension-aware Instantiate
good errors
manifest inspection
```

## Phase 3: Built-ins

```text
timer
log
metrics
```

## Phase 4: Pool/class

```text
Class
Pool
Lease
Reset policies
```

## Phase 5: Process/mailbox

```text
PID
Spawn
Send
Recv
Kill
Monitor
Link
```

## Phase 6: Policy/resources

```text
Capabilities
Handle table
Resource cleanup
Per-instance/process limits
```

## Phase 7: Supervisors

```text
one-for-one
one-for-all
restart windows
exit reasons
```

## Phase 8: Compiler extensions

```text
intrinsics
backend selection
instrumentation hooks
```

## Phase 9: Distributed

```text
node IDs
remote PIDs
spawn remote
send remote
routing
backpressure
```

---

# The final shape, condensed

```go
type Extension interface {
    Info() ExtensionInfo
    Register(*Registry) error
}

type Registry struct{}

func (r *Registry) ImportModule(name string) *ImportModuleBuilder
func (r *Registry) Hooks() *HookRegistry
func (r *Registry) Capability(cap Capability, opts ...CapabilityOption)
func (r *Registry) Resources() *ResourceRegistry
func (r *Registry) Compiler() *CompilerRegistry
```

Runtime:

```go
type Runtime struct{}

func NewRuntime(opts ...RuntimeOption) *Runtime
func (rt *Runtime) Use(ext Extension, opts ...UseOption) error
func (rt *Runtime) UseCompiler(ext CompilerExtension) error
func (rt *Runtime) Compile([]byte, ...CompileOption) (*Module, error)
func (rt *Runtime) Instantiate(context.Context, *Module, ...InstantiateOption) (*Instance, error)
func (rt *Runtime) Class(*Module, ClassOptions) (*Class, error)
func (rt *Runtime) Spawn(context.Context, *Class, SpawnOptions) (PID, error)
```

That is the product.

Small enough to understand.
Nice enough to use.
Powerful enough to grow into Lunatic-style actors, pools, distributed processes, and compiler intrinsics without detonating the API later.

Tiny spine. Big dragon. Unfortunately coherent.
