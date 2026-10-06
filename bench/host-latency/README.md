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
The included ARM64 captures explicitly bypassed the quiet-host gate at the user's
request; process-load evidence is retained and those are shared-host measurements.

[Published measurement evidence](pr-evidence/README.md) contains both architecture
tables, original-main comparisons, final incremental comparisons, raw CSVs,
fixture/source/binary pins, and host-load evidence. Compressed captures avoid
putting exploratory Go source snapshots in the benchmark module. The local
`measurements/` archive is a separate module to keep `go test ./...` from treating
source snapshots as standalone packages.

The latest measured session round trips are AMD64 56.35/66.95/92.61ns and
ARM64 57.67/66.07/78.00ns for typed/HostCall/Caller respectively. AMD64 predates
the final shared single-result-copy change; that change has not been timed there.
Wasmtime remains about13ns on AMD64 and11.5ns on ARM64. Parity is unmet.

## Design notes

- [Wasmtime and transition audit](transition-audit.md)
- [V8 host calls](v8-host-calls.md)
- [ARM64 register contract](arm64-bridge-register-contract.md)
- [Go stack workspace investigation](go-stack-workspace-design.md)
