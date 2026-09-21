# Focused reproduction

Run these commands with Bash, Python 3, Go, Git, and benchstat installed. The scripts find the checkout from their own location. Work and output paths can contain spaces; keep them quoted. Work directories must be new or empty and outside the Go module cache. A build uses isolated Wago/provider worktrees and records the source changes, patches, dependency graph, flags, toolchain and binary hashes in build.json.

```sh
bash docs/performance/setup-cleanup/continuation/reproduce-provider.sh '/tmp/wago reproduction/build'
python3 docs/performance/setup-cleanup/continuation/run-wasi-pairs.py \
  --binaries '/tmp/wago reproduction/build' \
  --output '/tmp/wago reproduction/timing' --samples 20
python3 docs/performance/setup-cleanup/continuation/summarize-wasi.py \
  '/tmp/wago reproduction/timing' --output '/tmp/wago reproduction/summary'
```

For explicit binaries use `--baseline PATH --candidate PATH --build-metadata PATH` instead of `--binaries`. The manifest hashes must match both executables. `--cases smoke --samples 2` is a bounded installation check, not a performance comparison. The default cases cover full commands and controls, lifecycle phases, direct host calls and provider registration/reused-provider lifecycles. Resource observations are now a separate experiment; the timing runner does not silently run interventions or modify published resource files.

Each run has a UUID and records the fixed sample count, balanced AB/BA order, source/build identity, exact process commands, settings and host conditions. Each process writes a separate file. Failed, missing, duplicate or changed samples prevent summary generation. A new run cannot append to an existing output directory. Summary outputs also require an empty directory. No resume mode is provided.

Profiles are separate from timing comparisons:

```sh
bash docs/performance/setup-cleanup/diagnose-command.sh \
  --binary '/tmp/wago reproduction/build/wasi-candidate.test' \
  --output '/tmp/wago reproduction/command profiles'
bash docs/performance/setup-cleanup/diagnose-memory-workers.sh \
  --binary '/tmp/wago reproduction/build/wasi-candidate.test' \
  --output '/tmp/wago reproduction/memory worker profiles' \
  --gomaxprocs 16 --callers 16 --workers 0 --cpus 0-15
```

The worker API is public full compile. The selector anchors every path component; the caller count defaults to the configured GOMAXPROCS. The diagnostic supports callers 1, 4, or GOMAXPROCS. Each CPU and allocation profile must produce exactly the selected leaf with its requested count, passed phase count, worker limits, caller count and GOMAXPROCS. Limits are not observed simultaneous activity. `--workers-only`, `--cpu-time 1s`, and `--allocation-iterations 100` give a bounded profile smoke check. Profiles include process-wide setup and calibration; use call paths, not the timer alone, to attribute work. These scripts no longer launch the old broad matrices or incomplete strace command as a side effect.

Defaults preserve Linux guarded memory, signal bounds checks, GOMAXPROCS=16, GOGC=100, GOMEMLIMIT=off, empty GODEBUG and CPUs 0-15. Use `--gomaxprocs` and `--cpus` explicitly when hardware differs, and do not combine those samples with the published environment. An empty `--cpus ''` disables taskset. The build can select another provider base with `--provider-base COMMIT`, or a local source repository with `--provider-repository PATH`. `--comparison snapshot` holds that provider revision fixed and restores only the original Wago imports.go for the baseline.

Tests: `python3 docs/performance/setup-cleanup/test_measurement_scripts.py`. Published data remain historical records. The old append-based runner and invalid worker-profile filters must not be used to generate new evidence.

For a later provider experiment, `--production-patch PATH` selects an explicit patch against `--provider-base`. The default still uses the saved historical construction patch. See the [direct-dispatch reproduction](direct-dispatch/README.md).
