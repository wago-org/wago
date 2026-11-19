# Standalone identical-binary AMD64 Linux A/A diagnostic
No remote access, execution, candidate comparison, or scheduling is implied by
these files. Run only on an explicitly authorized host with a CPU selected by
its operator; no governor, kernel, SMT, cgroup, affinity-policy, or system
settings are modified. Only the child is pinned. Historical Ryzen 7800X3D
metadata is not machine access or stability evidence.

## Prerequisites and review
Python >=3.10 (stdlib, no `-O`), readable Linux procfs, util-linux taskset, a
local Go executable, and one trusted executable Linux x86-64 Wago adapter
supporting protocol v1 describe/prepare/compile with three phase barriers and
an independent execution oracle, plus close. No installer, build or network
is included. Supply the four exact artifact blobs named in inputs.json under
one directory. Use a writable, executable local filesystem and enough disk
for the binary copy, rows, journal, and gzip. Both A and B will execute the
same copied file at one inode. The historical Go 1.27.1 adapter SHA-256 is
91c65a975fada991ecefc9dc1d35252572677e2330df3218722647b39438deee;
another explicitly pinned compatible binary is a new host/binary condition,
not an exact reproduction. No binary or Wasm blobs are distributed here.

```sh
python3 prepare.py prepare --binary ADAPTER --binary-sha256 PINNED_SHA256 \
  --corpus CORPUS_DIR --go GO_BINARY --expected-go go1.27.1 \
  --cpu OPERATOR_SELECTED_CPU --manifest NEW_MANIFEST.json
python3 prepare.py check --manifest NEW_MANIFEST.json --manifest-sha256 PRINTED_SHA
# Only after explicit authorization for execution:
python3 run.py --manifest NEW_MANIFEST.json --manifest-sha256 PRINTED_SHA \
  --new-output-directory NEVER_USED_DIRECTORY --execute-local-aa384
python3 analyze.py --run-directory NEVER_USED_DIRECTORY
```

Review the manifest and immutable input package before release. The manifest
pins host architecture/kernel, selected and permitted affinity, Go/toolchain
version and adapter build metadata, input and kit hashes, source binary
device/inode, and selected executable hash. The controller exclusively creates
a fresh output directory; each child executes an independently hashed slot
whose device/inode remains fixed. A dry check does not run the adapter; any
protocol disagreement stops the executed run. Close handles automatically on
errors; if an adapter hangs, interrupt the run and keep its partial receipt.
Do not resume it or silently retry. Do not modify the source adapter or corpus
during a run. Exclusive run lock governs this output/slot, not unrelated host
activity; the operator must arrange exclusivity.

## Frozen design and decision
Exactly 384 first public Compile observations: four fixed contracts × rewrite
and no-rewrite × three epochs × four blocks per cell/epoch × four fresh
children. Seed 902173 shuffles blocks; two ABBA and two BAAB sequences per
cell/epoch; fixed five-second pauses only between epochs. Rewrite writes and
truncates the same bytes to the same slot; both strata hash the whole slot
before every launch. No-rewrite inherits the last slot write. Neither stratum
represents a guaranteed cold file cache. The Go runtime uses GOMAXPROCS=1 and
the same 20 OFF flags; no arm-specific environment.

Raw JSONL and journal are fsynced before proceeding. Compress only after
all 384 verified samples. Keep the first row, transitions and all outliers.
Primary outcome is rewrite A/B ratio of arithmetic means of per-workload
medians. Each case and aggregate must independently meet point within 2%
and paired 95% block-bootstrap interval wholly inside [0.97,1.03].
No-rewrite receives the same gate and never automatically replaces the
primary protocol. Bootstrap retains four-child blocks, 5000 draws, seed
992271; no workload resampling. Rewrite/no-rewrite ratio and Linux scheduler
or fault observations are descriptive only, and are collected outside the
adapter timer. Unavailable optional procfs counters are recorded. Time spent
observing barriers must not be subtracted from the adapter timer. A pass
covers this precise host/binary/workload/window only. No trimming, rerolls,
candidate comparisons, optimization or RSS qualification.
