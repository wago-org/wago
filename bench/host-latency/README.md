# Host call latency

Measure full checked Go → Wasm → Go callback → Wasm → Go round trips, and
separately amortized callback batches. Export lookup, compilation and instantiation
are outside timing. The callback increments an atomic counter; every invocation
checks results and each sample checks the callback count. No entry-cost subtraction.

The public Go API is unchanged. Compiler-certified bounded integer modules use
rooted normal Go callback frames, private native contexts, and typed/view prepared
owners. FP/SIMD modules, shared resources, unsupported signatures and decoded
artifacts retain their normal bridges. Caller lifetime, reentry, panic cleanup,
stack growth, GC and revocation checks remain active.

## Run

From this directory, use `./run.sh --yield-atomic 1 65536` with Go and Rust installed.
The locked Rust reference uses Wasmtime46. `main.go` also accepts
`--api=instance`, `--api=prepared`, or `--api=session`, plus `--work=N` and
`--batch-min=N`. Run a compiled binary in a directory containing `yield.wasm`;
the Rust reference materializes it from `yield.wat`.

For paired comparisons, build the identical harness against each source version
and run `quiet_capture.py --help`. `summarize_guarded.py` verifies CSV digests and
retains all callback/API/count shapes, paired ratios, spread and reference drift.

Current-main-versus-PR-head tables for AMD64 and ARM64 are in the PR description.
Raw captures, source and binary pins, and host-load evidence are retained locally.
Local exploration archives are ignored. Wasmtime parity is unmet.
