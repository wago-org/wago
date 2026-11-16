# Separate nursery policy sensitivity

One unchanged `wago_gc_direct_refstore` binary runs with nursery sizes of 64 KiB,
512 KiB and 2 MiB, holding each old semispace at 8 MiB (two spaces). This direct
prototype has no survivor spaces: all nursery survivors promote immediately.
A larger nursery changes survival/promotion frequency as well as reserved memory;
this is not an isolated instruction-speed comparison or a universal heap-policy
recommendation. No defaults are changed.

Build the probe with `go test -p 1 -c -tags wago_gc_direct_refstore -o PROBE ./src/wago`,
then run in a quiet window:

```sh
python3 run.py PROBE --output-dir RESULTS --audit
python3 run.py PROBE --output-dir RESULTS
```

The same 3-warmup/5-timed-call protocol and 3 fresh-process rounds are used for all
sizes. Exact allocation/checksum checks remain; report actual minor/full counts
and Linux `/proc` memory alongside times. Larger sizes can defer full collections,
so a short-window result does not establish long-run full-GC cost or V8 parity.
