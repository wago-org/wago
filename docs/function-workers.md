# Function workers

Wago can validate and compile independent WebAssembly functions concurrently.
The feature is opt-in: the default remains one worker so library users get the
smallest and most predictable memory footprint.

## Go API

Configure the policy on `RuntimeConfig`:

```go
cfg := wago.NewRuntimeConfig().WithFunctionWorkers(0) // adaptive
compiled, err := cfg.Compile(wasmBytes)
```

The policy values are:

| Value | Behavior |
|---:|---|
| `0` | Adaptive: serial for small modules, up to four workers for larger modules. |
| `1` | Serial validation and code generation; this is the default. |
| `N > 1` | Force a maximum of N workers. |

The effective count is always capped by `GOMAXPROCS` and the number of locally
defined functions. `FunctionWorkers()` reports the configured policy, not the
effective count for a particular module.

`WithCompileWorkers` and `CompileWorkers` remain as deprecated source-compatible
aliases. New code should use `WithFunctionWorkers` and `FunctionWorkers`, because
the policy covers validation as well as native code generation.

## CLI

Both compilation through `run` and validation-only workflows accept the same
flag forms:

```sh
wago run -p module.wasm                 # adaptive
wago run -p8 module.wasm                # force at most 8
wago run -p 8 module.wasm
wago run --parallel=8 module.wasm

wago validate -p module.wasm
wago validate -p4 module.wasm
```

Flags after the module passed to `wago run` remain guest arguments and are not
consumed by Wago.

## Adaptive policy

Adaptive mode uses a bounded work score:

```text
score = total function-body bytes + 64 * local function count
score < 16 KiB: serial
otherwise:       at most 4 workers
```

This keeps tiny modules on the allocation-minimal serial path. Four workers are
the adaptive ceiling because eight workers improve latency further on the
largest modules but consume more CPU and transient memory without consistently
improving end-to-end or multi-module throughput.

## Execution model

Module declarations are validated serially before workers start. This includes
types, imports, tables, memories, globals, constant expressions, exports,
elements, data segments, and the start declaration. Only function bodies fan
out.

Each validation worker owns its operand/control stacks, byte reader, and decoded
immediate scratch. The module's resolved type cache is populated and frozen
before parallel validation, so workers only read shared state. If multiple
functions are invalid, Wago reports the lowest function index, matching serial
validation regardless of completion order.

Code generation follows the same bounded-worker policy. Module-wide analyses
finish first; workers compile functions into private arenas; final machine code
is joined in original function order. Generated output and serialized `.wago`
artifacts are independent of worker count.

Modules whose code generation is deferred until import linking still benefit
from parallel validation during initial compilation. Their retained worker
policy is also reused for link-time code generation.

## Performance and memory

On the benchmark machine documented in
[the validation experiment](parallel-function-validation-report.md), four-worker
function validation reduced validation latency by 58-72% on representative
medium and large modules, and by 34% on the 301-function `many_funcs` fixture.
The full public compile pipeline improved by another 8-24% compared with
parallel code generation alone.

Parallel workers add bounded transient allocations for worker-local stacks and
scratch state. Keep the default serial policy for memory-constrained or
multi-module-throughput services unless measurements justify parallelism. Use
adaptive mode for CLI/build-style workloads where one-module startup latency is
the priority.

Detailed measurements and reproduction commands:

- [parallel function compilation experiment](parallel-function-compilation-report.md)
- [parallel function validation experiment](parallel-function-validation-report.md)
