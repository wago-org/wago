# Matched queue and native-allocation experiment

Read `summary.md` for per-workload results and limitations. All 27 timed launches
passed artifact/checksum, exact allocation-count, and real-collection checks.
The three source/codegen variants are:

1. `0d4e364` plus identical probe assertion patch (`61e22e0`), `wago_gc_direct_reads`.
2. `26e2980` plus identical probe assertion patch (`da9bbfb`), `wago_gc_direct_reads`.
3. The same after-queue source, `wago_gc_direct_alloc`.

The queue comparison therefore holds compiler mode fixed; allocator comparison
holds collector implementation fixed. This is **not** a V8 acceptance run, and
these short results do not establish significance for the approximately1%
array/tree allocation-control deltas. Configured nursery/old capacity, invocation
parameters, Go version and process threading are matched. All timing-region GC
counts match across variants for each workload.

To repeat, check out the stated before/after sources with the identical probe
patch. From each worktree build with `go test -p 1 -c -tags TAG -o OUTPUT ./src/wago`.
Use output filenames `reads_before_queue.test`, `reads_after_queue.test`, and
`alloc_after_queue.test` in one directory, then run:

```sh
python3 run.py --before-root BEFORE_WORKTREE --after-root AFTER_WORKTREE \
  --binary-dir BINARIES --output-dir RESULTS --audit
python3 run.py --before-root BEFORE_WORKTREE --after-root AFTER_WORKTREE \
  --binary-dir BINARIES --output-dir RESULTS
```

Run only when other compiler/test/timing jobs on the machine are stopped.
`audit.json` contains untimed three-call correctness/counter/memory probes.
`results.json` retains all process output, exact binary and artifact SHA256s,
source revisions, configuration, counters, and corrected Linux `/proc` memory
metrics. No inherited `ru_maxrss` value is used for the summary.
