# Native AMD64 scalar host leaves

Source `d4a5d2ff` adds verified scalar host-import lowering on AMD64. On Hub's Ryzen 7 7800X3D, ordinary typed callbacks now outperform pinned SIMD w2c2-clang on the compact-import, thread-host-identity, and minimal host-loop contracts. The opaque owned Go callback retains Go execution and remains far from parity. The full parity goal is incomplete.

Two real SDK orders, using Go 1.27.1 and Clang 18.1.3 on CPU 7, produced:

| Contract | Wago | w2c2-clang | Wago / peer | Change from 334732e1 |
|---|---:|---:|---:|---:|
| Compact ordinary imports | 757–758 us | 1.122 ms | 0.675 | −97.5% |
| Threads ordinary host identity | 438–466 us | 624–625 us | 0.700–0.747 | −92.7 to −93.1% |
| Threads opaque Go identity | 6.375–6.395 ms | 624–625 us | 10.20–10.24 | −0.5 to −1.0% |
| Minimal host loop, per iteration | 1.60–1.77 ns | 2.14 ns | 0.746–0.828 | −72.5 to −75.2% |

The separate public API comparison ran three alternating pairs on CPU 7 with the same fixtures and toolchain. All 18 observations passed their result oracle and allocated zero bytes. Artifact hashes, callback binding, workload contracts, source revision, and native platform are recorded in the adjacent JSON. The SDK audit checked 16 captures, 64 phases, 15 samples per phase, and all latency, RSS, and code statuses.

The new decoder copies at most 128 bytes of register arithmetic from an exact Go i32 function entry. It rejects calls, branches, memory access, stack/frame access, undefined register inputs, flags-dependent instructions, and FP operations. Wrappers preserve VM registers, keep instance-owned executable mappings under existing quiescence, and retain the ordinary host route for unsupported callbacks. Compiler register preservation remains independent of scheduler admission; disabling bounded entry preserves guest code.

Targeted native tests passed 60 checks with no skips. Broader native host/shared-memory/reentry/panic/cancellation/cross-instance/prepared-entry/codec tests passed 2,428 checks, with 19 skips. Five unrelated failures were reproduced on the unchanged baseline: two existing Compiled-size expectations and three table64 fixtures rejected by Hub's installed wat2wasm. Those five were excluded from the second broad run. The shared admission/alias and scheduler controls also passed on ARM64.

Compile RSS was effectively unchanged in these small cases. Compile latency improved for compact imports; the opaque case measured +2.5 to +3.9%, which needs the full native corpus audit before a broader compile-cost conclusion. Instantiate/first-call and code observations are retained in JSON.

These are private native AMD64 captures. They have not been ingested into the local Apple-platform website or production. The local website's complete retained `334732e1` ARM64 refresh is separate. A full 266-case native baseline/current/peer audit is the next check; these four contracts do not establish corpus-wide parity.
