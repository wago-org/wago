# SSE2 fill matched experiment

Source `aaf25f6`, runtime `861bbe1`. Same-source best ABI4 loop/copy baseline versus added SSE2 fill, with V8 as end-to-end reference. Primary Eden is 2 MiB; 512 KiB is a separate control. Both old semispaces remain 8 MiB. All 12 audits and 36 timing launches passed. Timed protocol stays 100 warmups, 9 batches of 32 calls, three rotating rounds. Audit-only warmups are 128 at 2 MiB. No CPU affinity changes were made mid-experiment.

## primary

| Case | Engine | Median ms | Process medians ms | Ratio to V8 | Timed minor/full | Peak RSS MiB |
|---|---|---:|---|---:|---|---:|
| array-fill | baseline | 9.3965 | 9.8223, 9.3965, 9.1840 | 1.286x | 2901/125 | 38.95 |
| array-fill | candidate | 9.0436 | 9.5879, 9.0436, 8.5918 | 1.237x | 2901/125 | 39.66 |
| array-fill | v8 | 7.3095 | 7.3095, 7.3380, 7.2157 | 1.000x | unobserved | 105.27 |
| array-allocation-access | baseline | 7.7416 | 8.3208, 7.7416, 7.7156 | 1.340x | 2901/125 | 39.36 |
| array-allocation-access | candidate | 7.7707 | 7.7707, 8.0014, 7.7522 | 1.345x | 2901/125 | 38.88 |
| array-allocation-access | v8 | 5.7755 | 5.7755, 5.6137, 6.2350 | 1.000x | unobserved | 105.16 |

## control

| Case | Engine | Median ms | Process medians ms | Ratio to V8 | Timed minor/full | Peak RSS MiB |
|---|---|---:|---|---:|---|---:|
| array-fill | baseline | 16.6108 | 16.9384, 16.5840, 16.6108 | 2.384x | 11776/512 | 38.51 |
| array-fill | candidate | 16.3498 | 17.0537, 15.5808, 16.3498 | 2.346x | 11776/512 | 38.53 |
| array-fill | v8 | 6.9682 | 6.9682, 7.1829, 6.9437 | 1.000x | unobserved | 105.25 |
| array-allocation-access | baseline | 15.0649 | 15.6211, 14.5894, 15.0649 | 2.482x | 11776/512 | 38.43 |
| array-allocation-access | candidate | 15.4722 | 13.6690, 15.4722, 15.8610 | 2.549x | 11776/512 | 38.58 |
| array-allocation-access | v8 | 6.0694 | 6.0694, 5.8671, 6.1396 | 1.000x | unobserved | 105.21 |

## Interpretation

At 2 MiB, fill is lower in all three paired rounds, with a 3.8% median reduction; the array-allocation control overlaps and changes only +0.4% in median. At 512 KiB, fill improves 1.6% in median but rounds are mixed, while the control is noisy. This is modest evidence on a shared host, not a strong universal gain. Candidate fill remains 1.237x V8 at the primary policy and misses the <=1.15x gate. Do not promote a default based solely on this small effect. A later explicitly affinity-controlled repeat could reduce migration noise, but cannot eliminate shared-host variability.

## Separate compile footprint

| Case | Variant | Median us/op | Native bytes | Go B/op | Allocs/op |
|---|---|---:|---:|---:|---:|
| array-fill | baseline | 135.865 | 2479 | 59979 | 233 |
| array-fill | candidate | 129.483 | 2543 | 59979 | 233 |
| array-allocation-access | baseline | 144.785 | 1967 | 56074 | 223 |
| array-allocation-access | candidate | 131.599 | 1967 | 56074 | 223 |

Fill adds 64 native code bytes (2479 to 2543) without added Go compile allocations/bytes; the array control is unchanged. Compile timings are reported, not treated as evidence of a compiler speedup.

V8 uses --no-liftoff, Node 24.22.1 / V8 13.6.233.17-node.58. Exact artifact hashes, checksums, object allocation counts and actual Wago collections were verified. V8 internal allocation/elision and collection counters remain unobserved; memory budgets differ. This is an opt-in end-to-end subset comparison, not isolated collector efficiency or an all-six-workload target result. Profiling and worker builds were excluded from the window; host frequency/scheduling remain uncontrolled.
