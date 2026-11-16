# Matched ABI3 native reference-store comparison

Both binaries use the same source, direct-offset collector, remembered-parent
queue, native scalar allocation and reads. Only the reference-array store tag
changes. The probe holds 64 KiB Eden / 8 MiB per old semispace fixed, disables
allocation elision, checks exact artifact/checksum and allocation counts, and
records actual collection counts and Linux `/proc` memory data.

Build from the same source with `go test -p 1 -c -tags TAG -o OUTPUT ./src/wago`:

- `wago_gc_direct_alloc` → `native_alloc.test`
- `wago_gc_direct_refstore` → `native_refstore.test`

Then, while other compiler/test jobs are idle:

```sh
python3 run.py --binary-dir BINARIES --output-dir RESULTS --audit
python3 run.py --binary-dir BINARIES --output-dir RESULTS
```

The short exploratory protocol is 3 warmup calls followed by 5 timed calls in
three alternating fresh processes per workload/variant. It is not a V8 acceptance
run. Nursery sensitivity is a different experiment and must not be mixed into
this comparison. Source preparation alone supplies no performance conclusion.

Recorded result: all 18 timed launches passed, with identical allocation and
collection counts. Struct time was consistently lower with native reference
stores (median75.66→22.87ms). Array and tree contained large opposing outliers;
the array median difference is inconclusive and the tree control supplies no
reliable gain claim. Full samples, provenance and memory data are under evidence.
