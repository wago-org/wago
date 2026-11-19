# Portable AMD64 Linux A/A host check, v2

This is a separate protocol, **not** a replication of the earlier A/A runs.
Tiny, fib_rec and PCRE2 bytes are unchanged; checked-in synthetic many_funcs
(300 small functions, oracle 464) replaces recovered CoreMark. No CoreMark
artifact is included or used. Do not pool historical and v2 data.

Prerequisites: trusted local Git snapshots at the exact revisions in
PROVENANCE.json, Go 1.27.1 linux/amd64, prepopulated trusted Go module cache,
Python 3.10+, taskset, readable procfs, and an operator-selected allowed CPU.
No installer, pull, automatic download, CPU policy change or license
acceptance is performed. Git commit IDs are required; remote availability
of the exact commit objects is not assumed. The recipe fails closed if any
required source, module checksum or tracked artifact is absent.

Review source before any execution. The files in licenses/ retain check-in
notices for Wago, Wasmbench and PCRE2. The three synthetic blobs come from
Wago's included WAT sources; PCRE2's exact tracked Wasm uses the provenance
and notice above. Keep the notices; building locally avoids republishing
the adapter. This kit contains no executable or Wasm file.

```sh
sha256sum -c SHA256SUMS
python3 build_inputs.py --wago WAGO --wasmbench WASMBENCH --go GO_BINARY \
  --gomodcache PREPOPULATED_CACHE --gocache LOCAL_BUILD_CACHE \
  --new-output-directory NEW_BUILD --build-local-v2
python3 prepare.py prepare --binary NEW_BUILD/adapter --binary-sha256 PRINTED_BUILD_SHA \
  --corpus NEW_BUILD/corpus --go GO_BINARY --expected-go go1.27.1 \
  --cpu CPU --manifest NEW_HOST_MANIFEST.json
python3 prepare.py check --manifest NEW_HOST_MANIFEST.json --manifest-sha256 PRINTED_MANIFEST_SHA
# Four correctness checks only; do not use elapsed values:
python3 smoke.py --manifest NEW_HOST_MANIFEST.json --manifest-sha256 PRINTED_MANIFEST_SHA \
  --new-receipt NEW_SMOKE.jsonl --execute-four-smoke-only
```

Only after separate authorization for the exact host and full protocol:
```sh
python3 run.py --manifest NEW_HOST_MANIFEST.json --manifest-sha256 PRINTED_MANIFEST_SHA \
  --new-output-directory UNUSED_OUTPUT --execute-local-aa384-v2
python3 analyze.py --run-directory UNUSED_OUTPUT
```

The source-built adapter need not match a historical binary hash. Pin each
actual local build SHA and use that one binary and fixed inode in both arms.
No fallback adapter, input substitution, wider benchmark or automatic retry.

384 first-Compile observations: four workloads, rewrite/no-rewrite, three
epochs, four four-child blocks per workload/stratum/epoch. ABBA and BAAB each
occur twice per cell/epoch. Block-order seed 902173, fixed 5 s between epochs.
All 20 flags OFF, GOMAXPROCS=1. Rewrite writes/truncates identical bytes at
the same inode; both strata hash the slot before every child. Fresh process,
same prepare/oracle/phase barriers, no warmup or discarded rows.

Rewrite is primary: A/B ratio of arithmetic means of per-workload medians.
Each case and aggregate must meet point within 2% and 95% paired block
interval wholly inside [0.97,1.03]. No-rewrite uses the same criteria and
cannot rescue rewrite. 5000 block draws (seed 992271), retaining all four
children, no workload resampling. Procfs diagnostics are descriptive outside
the timer and do not remove or adjust observations. Preserve partial records;
no trimming, extension, or reroll. A pass is limited to the exact run.
