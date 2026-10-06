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

These tables will compare current main (`9196bdff`) directly with the final PR runtime source after resolving main's merge conflicts. Historical and incremental comparisons have been removed. Fresh matched measurements are in progress.

### AMD64 — Ryzen 7800X3D

| Callback | Main Call latency | PR head Call latency | Delta |
|---|---:|---:|---:|
| Typed | Pending idle Hub | Pending idle Hub | — |
| HostCall | Pending idle Hub | Pending idle Hub | — |
| Caller | Pending idle Hub | Pending idle Hub | — |

### ARM64 — Apple M4 Max

| Callback | Main Call latency | PR head Call latency | Delta |
|---|---:|---:|---:|
| Typed | Measuring | Measuring | — |
| HostCall | Measuring | Measuring | — |
| Caller | Measuring | Measuring | — |

ARM64 measurements use the shared laptop at the user's request. Hub remains idle while the user plays CS2. No historical samples are presented as PR-head measurements. Wasmtime parity is not claimed.

