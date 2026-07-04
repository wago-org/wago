# Cross-runtime startup latency: full-process wasm → exec (2026-07-03)

End-to-end wall time for one real-world binary — `exec()` → load → compile →
instantiate → run the workload → exit — across nine runtime configurations, a
mix of interpreters and JITs. Reproduce with `skills/startup-latency-bench/`.

## The binary

**json-as** (the AssemblyScript SWAR JSON library already used in
`bench/corpus`), rebuilt standalone so every CLI can run it uniformly:

- 22 KB, **zero imports** (`env.abort` rebound to a local trap via
  `--use abort=`), wasm start section for runtime init.
- `_start` runs the real workload: 1000× `JSON.stringify` + 1000×
  `JSON.parse` of the session-status payload (~100 B).
- A **noop twin** exports the same functions (so the full JSON code is live and
  compile cost is identical) but `_start` does nothing. Startup = noop total;
  exec ≈ workload − noop. A positive delta also proves the runtime actually
  executed `_start`.

Measured with `hyperfine -N --warmup 5 --min-runs 30`, compilation caches
disabled for cold rows. Machine: AMD Ryzen 7 7800X3D (8 cores), linux/amd64.

## Results — cold start, full process (mean)

| Runtime | Type | Total | Startup (noop) | ~Exec |
|---|---|---:|---:|---:|
| wasm3 0.5.2 | interpreter | **5.0 ms** | 1.2 ms | 3.8 ms |
| **wago dev @ 0df7ea2** | single-pass JIT | **5.4 ms** | 5.0 ms | **0.4 ms** |
| wasmi 1.1.0 | interpreter (rewriting) | 7.1 ms | 1.5 ms | 5.6 ms |
| wasmtime 45.0.1 | Cranelift JIT | 8.0 ms | 7.3 ms | 0.7 ms |
| wasmer 7.1.0 singlepass | single-pass JIT | 8.6 ms | 7.7 ms | 0.9 ms |
| iwasm 2.4.3 (WAMR) | fast interpreter | 9.3 ms | 5.8 ms | 3.5 ms |
| wazero 1.12.0 | compiler | 10.8 ms | 8.8 ms | 2.0 ms |
| wasmer 7.1.0 cranelift | optimizing JIT | 21.3 ms | 21.1 ms | ~0.2 ms |
| wavm (LLVM 21.1.8) | LLVM JIT | 263 ms | 265 ms | ~0 |

Warm compilation caches (the wasmtime/wasmer CLI **default**): wasmtime
4.2 ms, wasmer 7.4 ms.

Bare process spawn (`--version`): wasm3/iwasm 0.45 ms · wasmi 0.6 ms ·
wago 0.8 ms · wazero 1.4 ms · wasmtime 2.4 ms · wasmer 6.3 ms.

## Findings

1. **wago is second overall and the fastest cold-starting JIT.** Only wasm3
   wins, and only at exactly this workload size: wago has the best exec time
   on the table (0.4 ms vs wasm3's 3.8 ms), so any heavier workload flips the
   order; the noop column shows interpreters win anything lighter.
2. **wago's 5.4 ms is almost entirely compile time.** `wago run` on tiny.wasm
   is 0.98 ms total (0.8 ms of it process spawn — leaner than wasmtime's
   2.4 ms and wasmer's 6.3 ms). The 22 KB module adds ~4 ms; the in-process
   stage bench attributes it: decode 76 µs, validate 0.6 ms, **compile
   2.0 ms**, instantiate 18 µs. That is ~7 MB/s single-threaded compile
   throughput — the known transient-Instruction-AST hotspot, not codegen.
   Cutting compile to Cranelift-per-byte territory puts wago at ~2.5 ms
   end-to-end, ahead of everything above **including warm-cache wasmtime**.
3. **wasmtime's cold 8 ms hides 16 ms of user time** — Cranelift compiles in
   parallel on all 8 cores; wago's number is one thread.
4. **wasmtime and wasmer cache compiled code by default**, so their real
   second-run behavior is the warm rows. A working `wago build` / `.wago` AOT
   path would be the equivalent lever.
5. wavm shows why LLVM JITs don't play in this space (260 ms to compile
   22 KB). iwasm's 5.8 ms "interpreter startup" is mostly eager memory setup,
   not parsing. The wasmer CLI carries 6.3 ms of fixed spawn overhead before
   any wasm work happens.
