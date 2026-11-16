# Fixed-total arena policy sensitivity

Test-only `GC_BRIDGE_OLD` selects each old semispace in the promote-all direct
bridge probe. Default remains 8 MiB; existing young default remains 64 KiB.
The planned comparison explicitly sets young/each-old to 2/8, 4/7, and 6/6 MiB.
All reserve 18 MiB usable arena plus the 16-byte null guard. No production policy
or compiler option changes. Old overrides reject non-direct/age2 modes, zero,
unaligned values, and arena geometry exceeding uint32 offsets.

The receipt checks actual initial DirectView nursery and old boundary/arena
extent against requested config. It emits actual sizes and start/dirty table
lengths, with a separately labeled derived fixed metadata footprint.

For R = 16 + young + 2*old, directgc.FixedBytes is:

    R + 15 + 2*floor(R/8) + 8*ceil(floor(R/16)/64)
      + 4*floor(R/16) + 4*min(1024, floor(old/16))

Each proposed split reserves 28,463,151 bytes including scalar metadata. Tests
compare the formula to actual core FixedBytes and runtime view geometry.
Descriptors, Go object/slice headers, runtime roots and host allocation/RSS are
not included. Smaller old spaces may increase full collections or fail live-set
capacity; a larger nursery also changes oversize allocation and promotion age.
This is not evidence of equal memory use versus V8 or isolated GC efficiency.

Qualification and measurements are complete; see `evidence/summary.md`. Evidence: one binary, all six
artifact/checksum/allocation audits, repeated rotating fresh processes, unchanged
100 warmups and 9 batches of 32 calls. Record process-level medians, collection
counts, memory and failures; do not pool dependent batches as repetitions.

Reproduce using the seven tags in `evidence/full.json`, build one test binary,
then run `python3 run.py --repo REPO --binary BINARY --output AUDIT --audit`
and repeat without `--audit` into a separate output directory. Audits use 512
warmup calls uniformly; timed processes retain 100 warmups and 9×32 calls.
Keep other local jobs paused and preserve failures. `summarize.py` regenerates
the committed summary from evidence next to the script.

## Old-live-set capacity control (normal and race qualified)

`GC_EQUAL_CAPACITY=1 go test ./experiments/directgc -run '^TestEqualBudgetOldCapacity$' -count=1 -v`
checks 27 independent heaps: all three splits crossed with exact live-byte targets
6, 7 and 8 MiB, each at -16, equal, and +16 bytes. A 64-slot reference-array root
retains scalar byte arrays of at most 256 KiB stride; the final child makes the
requested total precise. Every child payload carries checked index/offset IDs.
Each child is attached through an old-to-young edge and explicitly minor-collected.

When live bytes first exceed each-old capacity, Minor and Full must both report
ErrOOM without changing roots or old cursor; all payloads remain readable.
Dropping only the newest edge must permit Full and move the surviving graph.
Successful graphs also undergo two moving Full collections. Finally dropping all
roots must reclaim old storage and allow allocation plus Minor again. Record the
first failed attempted live size separately from the requested larger graph,
which cannot be built once capacity is reached. No runtime timing or V8 claim;
this core control does not replace runtime GC semantic qualification.

Capacity results: see `capacity-evidence/summary.md` and the 27 receipts per mode.
