# Untimed age2 workload traffic observations

Runtime bd4a4685, reviewed probe 98312138. SSE+refstruct+age2+age2_metrics only, explicit age2 true. Total young 2 MiB split active/inactive 1 MiB, survivor cap 512 KiB; two 8 MiB old semispaces. No weighted/shared/stride/cohort/shadow. This separate diagnostic binary adds scans and is NOT timing evidence.

All six 133-call runs pass SHA/checksum/exact allocations/actual GC and native/metric success reconciliation. Snapshots before warmup, after 128 and after 133 calls are preserved.

| Case | Successful minors | Fresh allocated bytes | First survivor/retained-out bytes | Age-one promoted bytes | Overflow promoted bytes |
|---|---:|---:|---:|---:|---:|
| struct-allocation-access | 709 | 697,176,832 | 46,328,896/46,328,896 | 0 | 0 |
| array-allocation-access | 4255 | 2,928,464,880 | 1,464,060,400/1,464,060,400 | 0 | 0 |
| array-fill | 4255 | 2,928,464,880 | 1,464,060,400/1,464,060,400 | 0 | 0 |
| array-copy-checksum | 2 | 2,096,784 | 288/288 | 0 | 0 |
| array-loop-copy-checksum | 2 | 2,096,784 | 288/288 | 0 | 0 |
| tree-survival | 141 | 143,454,272 | 4,428,208/4,428,208 | 266,112 | 0 |

Full successes, minor/full failures, old-direct allocation bytes/objects, remembered preflight parent/edge scans and remembered overflow slot visits are ZERO for every workload. No full live-byte observation exists; do not interpret absent full cycles as zero live-old survival.

## Effective fresh allocation room

| Case | Last retained prefix in bytes | Last fresh capacity bytes | Last fresh allocated bytes | Last first survivor bytes |
|---|---:|---:|---:|---:|
| struct-allocation-access | 65,344 | 983,232 | 983,232 | 65,344 |
| array-allocation-access | 344,080 | 704,496 | 688,160 | 344,080 |
| array-fill | 344,080 | 704,496 | 688,160 | 344,080 |
| array-copy-checksum | 144 | 1,048,432 | 1,048,320 | 144 |
| array-loop-copy-checksum | 144 | 1,048,432 | 1,048,320 | 144 |
| tree-survival | 33,264 | 1,015,312 | 1,015,312 | 33,264 |

Array/fill first survivors total about 50% of fresh allocated physical bytes in these successful events. The last retained 344,080-byte prefix leaves 704,496 bytes fresh capacity inside the 1 MiB active space, versus the promote-all 2 MiB active nursery. No cap-overflow promotion, age-one promotion or remembered scan work was observed for these two workloads. These counters narrow the explanation of the measured regression toward collection frequency and repeated young graph/copy work, but do not quantify each mechanism’s CPU cost or prove an optimization.

Tree has 266,112 bytes of actual age-one promotion, demonstrating that the changed collection schedule does not preserve the old shadow cohort identity. Prior promote-all shadow results must not be generalized into “no objects ever survive two age2 minors.”

FirstSurvivor includes retained plus overflow-promoted fresh objects and must not be added to retention/promotion as another independent traffic total. Capacity/prefix/remembered totals sum per-cycle observations, not distinct memory or unique object lifetimes. Fresh and old-direct allocations after the last successful collection are excluded from these traffic totals; full 133-call generic allocation totals still reconcile. Failed attempts would not consume intervals or update successful totals. No timing, V8 or default-enablement claim follows.
