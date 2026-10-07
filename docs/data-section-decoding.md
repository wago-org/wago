# Data-section vector allocation

The byte-backed decoder reserves active/passive data records at the section's
checked segment count. Active constant-expression offsets live directly in
`Data.Mode.Offset.BodyBytes`; a duplicate positional offset directory is no
longer allocated.
Every encoded data segment needs at least two bytes (flags and payload length),
so a count greater than half the remaining section bytes is rejected before
vector allocation. Syntax, constant-expression and payload checks still run for
every segment.

The existing conservative `reserveDecodedSlice[Data]` admission charge is
unchanged. This optimization does not expand the accepted metadata budget.
Sufficiently padded malformed input can allocate its full budget-admitted
vectors before an early syntax error, rather than starting with 1,024 records;
those allocations remain bounded by the same budget. Impossible/truncated
counts and over-budget declarations are rejected before allocation.

The old bounded initial capacity caused repeated pointer-rich growth buffers for
large valid sections. On a pinned esbuild input with 100,000 data segments,
phase diagnostics on Linux/amd64 Go 1.27.1 showed decode peak RSS fall from
73.3 MB to 43.3 MB and backend-end peak RSS from 120.9 MB to 97.0 MB. Publication
then became the high-water stage, at 118.1 MB in this exact-vector-only diagnostic
run. Instrumentation records cumulative kernel high-water RSS and current heap
occupancy at boundaries; it does not force collection or change GC policy. Phase
instrumentation is experimental, separate from production and timing samples.

With the separately proposed compact execution snapshot, three alternating cold
pairs measured 121.55 MB to 111.80 MB peak RSS, 144.37 MB to 93.22 MB total Go
allocations, and 92.69 MB to 87.10 MB post-GC retained heap versus the reviewed
operand-checkpoint candidate. These are distinct metrics, not a 50% peak claim.
Public module fields, borrowed payload bytes and normal validation remain
unchanged; no API or artifact migration is needed.

The subsequent offset-directory removal reduced another 2.40 MB of esbuild
allocation traffic (93.223 MB to 90.823 MB with compact snapshots), while the
three-pair peak RSS and post-GC retained heap medians remained unchanged.
The directory was transient, so this is not an additional peak-memory claim.

Byte-backed validation now reads the current public data offset bytes. Replacing
or reordering `Module.Data` after decoding is therefore validated against that
current representation, rather than an earlier positional offset summary. Normal
decoded module values, borrowing and active/passive/global/extended/memory64
offset behavior remain unchanged.
