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

Qualification and measurements are pending. Planned evidence: one binary, all six
artifact/checksum/allocation audits, repeated rotating fresh processes, unchanged
100 warmups and 9 batches of 32 calls. Record process-level medians, collection
counts, memory and failures; do not pool dependent batches as repetitions.
