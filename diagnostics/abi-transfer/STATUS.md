# Shared register argument transfer

Experimental baseline: 2dd082d4. No measured compiler or RSS improvement yet.

The GP, mixed-bank, and tail-call paths share argument capture and register
commit. Call target preservation, continuation barriers, collector metadata,
frame release and result handling stay in the original callers. Existing root
scratch is reused without a second linked-root traversal; only continuation
prefix types are classified. No signature cache or collector changes.

## Focused qualification

Source 8834f26c: 21 top-level ABI tests passed with
`wago_codegenstats,wago_regalloccheck` (0.052s), no skips. The normal build
passed (0.157s); its three diagnostics-dependent tail tests skipped and were
covered by the tagged run. This is not full CI.

Go 1.27.1 escape diagnostics exposed an initial regression: all six bounded
mixed/tail arrays moved to the heap. Removing pooled-slice retention from the
shared commit was insufficient. Returning capture plans by value, instead of
appending through an opaque plan pointer, removed all six array escape reports.
The shared commit plan and move callbacks do not escape. Growing GP scratch may
still allocate as before; this is not a claim of allocation-free compilation.

Independent native/full-metadata corpus parity passed for all 290 cases
(145 workloads, both interruptibility modes) against pristine 2dd082d4. Root/scratch, slot capture, fixed-register spills, indirect targets,
FP literal loading, traps, and tail-frame tests are focused correctness evidence,
not proof of the requested performance targets.


## Initial cold screen

Standard Go1.27.1, CPU2/GOMAXPROCS1, three fresh-process trials across145
workloads for two main controls and the candidate:1305 runtime oracles verified.
Arithmetic average of per-workload median first public Compile latency:
main A893.526us, main B892.445us, shared ABI899.110us. Candidate is0.69% slower
than mean main; A/A spread0.12%. No demonstrated compilation gain, no PR or
promotion. RSS and detailed execution latency were not measured for this
checkpoint. Correct output is not proof of an execution-speed improvement.
