# Wasmtime-Go reference

This isolated module pins Wasmtime-Go v46.0.1 (upstream commit `dc8b29ba8cb3e219277d92ce9f977d8b0a698efc`). Its bundled C API libraries use Wasmtime v46.0.1. The existing Rust reference pins v46.0.0; those results are a separate comparison with a patch-version difference.

Build from the repository root:

```sh
(cd bench/host-latency/wasmtime-go && GOWORK=off GOFLAGS=-mod=readonly go build -o /tmp/wasmtime-go-host .)
(cd bench && go build -o /tmp/wago-host ./host-latency)
cd bench/host-latency
/tmp/wago-host --api=prepared --work=262144 --batch-min=16 --yield-atomic 1 65536
/tmp/wasmtime-go-host --work=262144 --batch-min=16 --yield-atomic 1 65536
```

Both harnesses use the same `yield.wasm`, an i32 increment and a checked atomic increment in every callback. Compilation, instantiation and export resolution happen before timing. Each result is checked; callback counts are checked after each sample. There are 16 warmups and five samples. Repeats are `max(work / loop_count, batch_min)`; defaults remain 4194304 and 64. Count 1 measures Call latency, the complete Go → Wasm → Go callback → Wasm → Go round trip. Count 65536 amortizes entry over the Wasm loop and includes loop overhead and the callback atomic increment.

CSV kinds are `typed` (`WrapFunc` with int32 input/output), `call` (explicit `NewFunc` with Val slices), and `caller` (`WrapFunc` with a Caller argument). NewFunc reuses a numeric result slot synchronously. These correspond to useful API choices, but differ from Wago's borrowed HostCall slots and Caller interfaces. The explicit Wasmtime callback always receives a Caller even when unused. This harness does not exercise concurrent or reentrant calls.

The Go binding has additional work beyond the Rust typed API. [`Func.Call`](https://github.com/bytecodealliance/wasmtime-go/blob/dc8b29ba8cb3e219277d92ce9f977d8b0a698efc/func.go) queries the function type and parameters on each invocation, marshals through C helpers and calls the checked C API. `WrapFunc` stores a reflect.Value and the callback trampoline invokes `reflect.Call`; `NewFunc` avoids that reflection but builds parameter Val slices and validates results. Both callback paths construct a Caller and invalidate it when the callback returns. [`getDataInStore`](https://github.com/bytecodealliance/wasmtime-go/blob/dc8b29ba8cb3e219277d92ce9f977d8b0a698efc/store.go) uses a C context lookup and a locked Go registry. These source findings explain mechanisms present in the binding; this benchmark does not attribute a precise number of nanoseconds to any one mechanism.

Measurements are retained in the parent README and `measurements/*/go-binding-reference`. They compare public Go embedding APIs, and do not establish parity with the Rust Wasmtime reference.
