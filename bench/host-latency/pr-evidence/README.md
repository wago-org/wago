# Host call measurement evidence

Current comparison tables use current main9196bdff and the final PR runtime
source, identical harness/fixture/Go1.27.1/API, checked atomic callback counts
and checked output. Call latency means full reserved-session round trip.

The historical named tar.gz captures remain for audit. They are not current
main-versus-PR-head measurements and are not reused in the tables below.
Each has raw CSVs, stderr, commands/pins, fixtures and host-load metadata;
adjacent summaries include every API/callback/count shape and unfavorable
samples. manifest.json pins originals and published archives. Unrelated
process arguments were removed from public snapshots; process names,
PID/PPID/CPU, affinity/ticks remain. Full snapshots remain local.

Validation logs include native merged ARM64 units/race, register checks and
Caller cancellation-signal regression tests. Full external TestStaged
conformance and fresh AMD64 execution remain outstanding.

**Deltas**

Call latency is the full checked round trip for a reserved `PreparedSession.Invoke2`, in ns, with one atomic-count-checked callback and checked guest results. Both versions use the identical harness, fixture, Go toolchain and API; compilation and instantiation are outside timing. Nothing is subtracted. Raw evidence also covers ordinary/prepared APIs and batches.

These tables compare current main **`9196bdff`** directly with the final PR runtime source measured at **`6f8a0605`**. The following evidence-only commit leaves the runtime source tree unchanged (`fb51a5679d43806b0e27f777350ff10fefe751a7`). Historical and incremental comparisons are excluded.

### AMD64 — Ryzen 7800X3D

| Callback | Main Call latency | PR head Call latency | Delta |
|---|---:|---:|---:|
| Typed | Pending idle Hub | Pending idle Hub | — |
| HostCall | Pending idle Hub | Pending idle Hub | — |
| Caller | Pending idle Hub | Pending idle Hub | — |

### ARM64 — Apple M4 Max

| Callback | Main Call latency | PR head Call latency | Delta |
|---|---:|---:|---:|
| Typed | 151.647 | 62.762 | -58.61% |
| HostCall | 205.082 | 65.894 | -67.87% |
| Caller | 273.572 | 79.577 | -70.91% |

ARM64: Go1.27.1, eight alternating paired blocks, 40 samples per callback/version. Every API/callback/count shape improved in all eight blocks; the table above reports reserved sessions. The shared laptop was authorized by the user; host-load snapshots are retained. Wasmtime reference round trip was11.467ns typed/11.449ns Caller, with2.78%/3.24% before/after drift. No entry cost is subtracted. **Wasmtime parity remains unmet.**

AMD64 is awaiting idle Hub; no older AMD64 sample is presented as PR-head timing. [Exact-head pins, summaries and raw capture](bench/host-latency/pr-evidence/README.md) are published. CI is pending; native Linux ARM64 signal-cancellation and full external conformance are not established by Darwin tests.
