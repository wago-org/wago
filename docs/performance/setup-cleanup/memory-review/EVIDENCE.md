# Evidence and exact reproduction

The [archive manifest](archives.json) gives SHA-256 checksums, run IDs and completed record counts. Archives contain the original per-process output and JSON, exact commands, binary/source hashes, dependency graphs, run order, timestamps, runtime settings and host observations. They are new evidence; the original published raw samples were not modified. Extract each archive into a new directory; its top-level directory retains the run name. Run the summary on that selected directory. Never combine historical, current, confirmation or rejected-candidate samples.

The [original-number recheck](historical-recheck.json), [machine settings](environment.json), [memory tables](RESULTS.md), [test commands and logs](checks/commands.sh), and [check status](checks/status.txt) are separate from timing data. `analysis/` contains benchstat, pprof and disassembly output. Trailing whitespace is removed from these derived text reports and copied check logs; the archived paired raw data and executable/source patches retain their original bytes. Full numeric counter summaries are in `runs/summaries.tar.gz`; raw data remain the authority. Boot/runtime startup measurements include all imported package initialization before the test starts; the rate-1 profile and inittrace are enabled in the child environment before initialization.

## Builds

These commands were run from the isolated Wago PR checkout, with the paths shown below. `/tmp/wago-setup-cleanup-pr-uvyppyeh/wasi-publish` was a separate local provider checkout. Substitute a local clone or the remote URL when reproducing. The scripts resolve their own checkout, and neither require `/home/jtenner/Projects/wago` nor read binaries from a fixed directory. Work and run directories must be new or empty.

```sh
bash docs/performance/setup-cleanup/continuation/reproduce-provider.sh \
  '/tmp/wago memory review/historical-v2' \
  --provider-repository '/tmp/wago-setup-cleanup-pr-uvyppyeh/wasi-publish' \
  --provider-base 6a6684d2ecd2be2d17e5792733d1d0e03b2f2c0e
bash docs/performance/setup-cleanup/continuation/reproduce-provider.sh \
  '/tmp/wago memory review/current' \
  --provider-repository '/tmp/wago-setup-cleanup-pr-uvyppyeh/wasi-publish' \
  --provider-base aef440ccb4368b87f122038cf31295bba1b4bd46
bash docs/performance/setup-cleanup/continuation/reproduce-provider.sh \
  '/tmp/wago memory review/snapshot' --comparison snapshot \
  --provider-repository '/tmp/wago-setup-cleanup-pr-uvyppyeh/wasi-publish' \
  --provider-base a6169cc6ebf86b5d2a98bd009e672dac02be1542
```

The exact measured build identities are in `builds/historical-v2/build.json`, `builds/current/build.json`, and `builds/snapshot/build.json`. To rebuild the exact measurements, use the recorded Wago revision and matching measured diagnostic source (`builds/measured-memory-diagnostic.go.txt`). The latest diagnostic also closes owners on failure and has a separate final smoke build. Do not silently label a latest-head rebuild as the original measurement binary. Older manifests show `go env` from the invoking source checkout; the actual build uses the per-label `.work` file shown by its dependency paths. The corrected builder records each invocation's actual environment and saves the source delta as well.

The provider patch is still [the saved production patch](../continuation/patches/wasi-construction.patch), based on the recorded provider revision. Tests also apply [the lifecycle tests](../continuation/patches/wasi-construction-tests.patch) and [the Go 1.22 correction](../continuation/patches/wasi-construction-go122-tests.patch) where the base lacks them. No Wago dependency pin was updated. A local comparison of this patch is not a published dependency upgrade.

## Process matrices

```sh
python3 docs/performance/setup-cleanup/memory-review/run-memory.py \
  --binaries '/tmp/wago memory review/current' \
  --output '/tmp/wago memory review/current-memory' --samples 20
python3 docs/performance/setup-cleanup/memory-review/summarize.py \
  '/tmp/wago memory review/current-memory' \
  --output '/tmp/wago memory review/current-memory-summary.json'
python3 docs/performance/setup-cleanup/continuation/run-wasi-pairs.py \
  --binaries '/tmp/wago memory review/current' \
  --output '/tmp/wago memory review/current-timing' --samples 20
python3 docs/performance/setup-cleanup/continuation/summarize-wasi.py \
  '/tmp/wago memory review/current-timing' \
  --output '/tmp/wago memory review/current-timing-summary'
```

The default fixed-work matrix is three modules × two APIs × three interventions × 20 pairs = 720 fresh processes. The following selections use the same commands with the stated binary/output directory and additional options:

| Run directory | Build | Additional selections |
| --- | --- | --- |
| historical-memory-session1, historical-memory-session2 | historical-v2 | full default matrix, separate sessions |
| snapshot-memory | snapshot | full default matrix |
| historical-full-context | historical-v2 | `--legacy --apis raw --interventions scavenge` |
| historical-exact | historical-v2 | old narrower corpus; preserved, invalid as exact replay; do not use this runner defect again |
| current-100commands | current | `--commands 100 --interventions normal` |
| current-epochs | current | `--epochs 4 --interventions normal` |
| current-tinyxml2-confirmation | current | `--modules tinyxml2 --apis raw` |
| current-profiles | current | `--samples 1 --interventions gc --profile` |
| current-mapping-diagnostic | current | `--samples 1 --modules tinyxml2 --apis raw --interventions scavenge --mapping-snapshot` |
| snapshot-timing | snapshot | timing runner: `--cases focused phases` |
| host-confirmation | current | timing runner: `--cases host-owned` |
| caller-timing | caller-experiment | timing runner: `--cases host-owned` |
| caller-verification | caller-experiment | timing runner: all default cases |

The rejected caller experiment holds the table patch in both builds. Its candidate overlays only the [private argument patch](builds/rejected-caller.patch). It uses current Wago diagnostic source and the same provider/dependency graph; its baseline binary is current's candidate. Build hashes and the overlay command are in `builds/caller-experiment/build.json`. This patch failed its single-call control and is **not** a retained production change. It has no claimed multi-toolchain correctness/memory acceptance.

For pprof, use the matching binary and saved `.heap` file, for example:

```sh
go tool pprof -top -nodefraction=0 -sample_index=inuse_space \
  -focus=github.com/wago-org/wasi \
  '/tmp/wago memory review/current/wasi-candidate.test' \
  '/tmp/wago memory review/current-profiles/001-candidate-tinyxml2-raw-gc.heap'
```

Use `alloc_space`, `alloc_objects` and `inuse_objects` separately. Allocation profiling at rate one and GC/init tracing alter execution and memory; do not merge these samples with normal runs. Detailed mapping snapshots also use their own run.

## Script smoke checks

The saved `worker-profiles` archive includes both the real CPU and allocation profile runs for the corrected public full pipeline selector. The exact flags and resolved limits are in its run.json. Lightweight fixture tests also use a checkout/work directory with spaces, GOMAXPROCS/callers 8, missing binaries, process failures, empty filters, existing output, bad checkpoint/configuration records and correct balanced paired counts.

The final smoke build used the same builder with work directory `/tmp/wago memory review/final smoke build` and the current provider base. It ran `--cases smoke --samples 2` through the paired timing runner and `--samples 1 --commands 2` through the full memory matrix. These are pass/fail smoke checks, not timing or memory effect estimates.
