# Native sampling permission and copy profiling attribution

Source `ff925be7`, production unchanged. Both diagnostic tests passed.

## Permission

One disabled self-thread software CPU-clock event successfully opened and closed
using existing x/sys. Kernel and hypervisor execution were excluded. The event
was never enabled or mmaped; no samples, signals, installation, escalation or
security-setting changes occurred. This establishes permission for this basic
open, not successful sampled event delivery or ring-buffer correctness.

A bounded next diagnostic may request IP/time/period records through an mmap
ring on the same locked OS thread and join records to a contemporaneous code
journal. It still needs independent sample/clock/loss validation. No sampler has
been implemented or run in this step.

## Copy parity cause

Both copy artifacts show the same observed decision deltas:

| Compiler decision | Function-only profile | Source-map profile |
|---|---:|---:|
| gc-direct-loop-hoist | 3 | 0 |
| gc-direct-loop-reuse | 3 | 0 |
| gc-direct-loop-no-plan | 4 | 7 |
| gc-direct-weighted-sum | 1 | 0 |
| br-pair-fold | 8 | 9 |

Function-only metadata produces exactly the plain 4,719 native bytes; both SHA256
and full byte equality are checked. Enabling source maps produces 4,591 bytes,
with the first byte difference at offset 33. Raw code and differing common-extent
offsets are retained. Native-size reduction is NOT a performance improvement.

The observed removed hoists match the explicit RecordSources exclusion in
`gc_loop_resolver.go:planGCLoopResolverModePair`, called by
`gc_direct_loop.go:hoistDirectLoop`. Weighted lowering in
`gc_compact_weighted_enabled.go:tryCompactWeightedLatch` requires the resulting
trusted lease, explaining the associated lost weighted-sum decision. Branch-fold
change is also preserved; no claim that every changed byte has one sole cause.
No admission guards or production compiler code were changed.

Function-only metadata is therefore a byte-neutral fallback for these two
artifacts in this same profiling-enabled build. It supplies native function/range
identity, not Wasm-operation hotness. Exact cross-build parity without wago_profile
remains unqualified. No guest calls or native samples were collected here.
