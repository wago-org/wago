# Published host-call latency evidence

The PR description reports matched original-main comparisons and separately
labels later retained-session measurements. Original main is0ef007c70581bf56155a4daf6fce8bda3f2c5ff1.
AMD main comparison uses Go1.22.2/CPU2 with other host workloads; latest AMD
retained capture uses Go1.27.1/CPU3, disabled ASLR and a quiet guard. ARM uses
Go1.27.1 on M4 Max and user-authorized shared-laptop captures. AMD latest timing
predates the final shared result-copy change; no current-head AMD timing claim.

Each tar.gz contains the complete selected capture's CSVs, stderr, fixture,
commands/pins and load evidence; adjacent summaries expose every callback/API/
count shape, paired ratios and reference drift. Capture labels: amd64-main-paired,
amd64-retained, arm64-main-paired, arm64-port-paired, arm64-result-paired,
arm64-result-reversed. Reversed capture has baseline=new and candidate=old.

Published process snapshots retain PID/PPID/CPU/executable, affinity and CPU ticks
where available. Unrelated process arguments are removed; manifest.json pins the
original local files and each published archive. Full original snapshots remain
local. Old AMD host-before.txt is omitted from publication with its hash retained.
CSV timing hashes and raw unfavorable timings are unchanged.

Validation logs are in validation/. Source/binary hashes for the final retained
copy change are adjacent; the capture pins describe the exact measured snapshots.
Wasmtime46 is locked by ../wasmtime/Cargo.lock. Main comparisons and incremental
measurements are separate experiments; no synthetic stitched delta is claimed.
No timing claim uses an interrupted capture. Wasmtime parity remains unmet.

### AMD64 — Ryzen 7800X3D

Matched original-main comparison (Go1.22.2, CPU2, October5; other host workloads remained active):

| Callback | Main Call latency | Measured candidate Call latency | Delta |
|---|---:|---:|---:|
| Typed | 278.892 | 101.589 | -63.57% |
| HostCall | 397.717 | 115.206 | -71.03% |
| Caller | 556.655 | 133.977 | -75.93% |

Latest retained session comparison (Go1.27.1, CPU3, ASLR disabled, quiet guard, eight alternating paired blocks, October6):

| Callback | Previous retained | Measured retained | Paired delta |
|---|---:|---:|---:|
| Typed | 57.628 | 56.350 | -2.18% |
| HostCall | 68.637 | 66.951 | -2.53% |
| Caller | 94.883 | 92.610 | -2.40% |

Matched Wasmtime session-equivalent reference: approximately **13ns**. These AMD64 numbers precede the final shared single-result-copy change; that change has not been timed on AMD64 because Hub is in use.

### ARM64 — Apple M4 Max

Matched original-main comparison (Go1.27.1, eight alternating paired blocks, October6):

| Callback | Main Call latency | Combined-port Call latency | Delta |
|---|---:|---:|---:|
| HostCall | 246.058 | 72.715 | -70.45% |
| Caller | 313.132 | 88.407 | -71.77% |
| Typed | 212.611 | 63.885 | -69.95% |

Latest retained session comparison, confirmed with reversed order (eight blocks each direction):

| Callback | Previous retained | Final measured retained | Paired delta, reversed run |
|---|---:|---:|---:|
| Typed | 61.293 | 57.673 | -4.54% |
| HostCall | 67.792 | 66.069 | -2.27% |
| Caller | 79.675 | 78.004 | -1.99% |

HostCall and Caller won all16 paired blocks; typed won11/16. ARM64 used the shared laptop at the user's request, with process-load evidence and reference drift retained. Typed reference drift was0.19% forward/0.57% reversed; Caller was0.34%/2.69%. Wasmtime remains approximately **11.5ns**; **parity is unmet**. Batches were mostly flat after the final copy change. Small public Caller regressions and rejected experiments are retained/disclosed rather than folded into the session win.
