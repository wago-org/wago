# Host call measurement evidence

Current comparison tables use current main9196bdff and the final PR runtime
source, identical harness/fixture/Go1.27.1/API, checked atomic callback counts
and checked output. Call latency means full reserved-session round trip.

The current AMD64 and ARM64 captures contain raw CSVs, stderr, fixture hashes,
commands and host-load metadata; the summary includes every API/callback/count
shape and unfavorable samples. manifest.json pins the raw local files and
published archives. Unrelated process arguments are removed from public load
snapshots; process names, PID/PPID/CPU and available affinity/ticks remain.
Full snapshots and historical experiments are preserved locally.

Native merged ARM64 units, focused race3, register checks and Caller
cancellation-signal tests passed. Full external TestStaged conformance and
native Linux ARM64 signal-cancellation validation remain outstanding.

**Deltas**

Call latency is the full checked round trip for a reserved `PreparedSession.Invoke2`, in ns, with one atomic-count-checked callback and checked guest results. Both versions use the identical harness, fixture, Go toolchain and API; compilation and instantiation are outside timing. Nothing is subtracted. Raw evidence also covers ordinary/prepared APIs and batches.

These tables compare current main **`9196bdff`** directly with the final PR runtime source: ARM64 measured at **`6f8a0605`**, AMD64 at **`fff4db02`**. Subsequent changes publish evidence only. After cleanup, the rebuilt ARM64 benchmark is **byte-for-byte identical** to the measured PR candidate (SHA256 recorded in the evidence). Historical and incremental comparisons are excluded.

### AMD64 — Ryzen 7800X3D

| Callback | Main Call latency | PR head Call latency | Delta |
|---|---:|---:|---:|
| Typed | 188.067 | 55.483 | -70.50% |
| HostCall | 268.072 | 66.250 | -75.29% |
| Caller | 432.676 | 90.838 | -79.01% |

### ARM64 — Apple M4 Max

| Callback | Main Call latency | PR head Call latency | Delta |
|---|---:|---:|---:|
| Typed | 151.647 | 62.762 | -58.61% |
| HostCall | 205.082 | 65.894 | -67.87% |
| Caller | 273.572 | 79.577 | -70.91% |

ARM64: Go1.27.1, eight alternating paired blocks, 40 samples per callback/version. Every API/callback/count shape improved in all eight blocks; the table above reports reserved sessions. The shared laptop was authorized by the user; host-load snapshots are retained. Wasmtime reference round trip was11.467ns typed/11.449ns Caller, with2.78%/3.24% before/after drift. No entry cost is subtracted. **Wasmtime parity remains unmet.**

AMD64: native Linux, Go1.27.1, CPU3, ASLR disabled, GOMAXPROCS1, ten-second quiet preflight and continuous interference guard; eight alternating paired blocks and40 samples per callback/version. All API/callback/count shapes improved in all eight blocks. Wasmtime reference round trip was13.148ns typed/13.046ns Caller, with5.72%/4.64% before/after drift; zero measured VM steal. The large main/head gains are consistent across every pair, while reference timing is less stable. Native head race3 and Linux interrupt-owner token race3 passed. [Exact-head pins, summaries and raw capture](bench/host-latency/pr-evidence/README.md) are published. CI is pending; native Linux ARM64 signal-cancellation and full external conformance are not established by Darwin tests.
