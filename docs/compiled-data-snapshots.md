# Active data in compiled execution snapshots

Public `Compiled.Data` remains an independent mutable `[]DataInit`. Compilation
and artifact loading freeze an execution snapshot before publication; subsequent
public edits do not affect instantiation, validation or serialization.

For at least 1,024 active segments with ordinary constant offsets and non-nil
payload slices, the immutable snapshot uses 16-byte pointer-free records:
32-bit memory index, offset, and start/end positions in one owned byte arena.
The public 72-byte descriptor remains unchanged on 64-bit targets. Global-derived
or extended-expression offsets, nil metadata, and payloads exceeding the compact
range retain the ordinary deep-copy representation. Empty active segments remain
present and retain their instantiation bounds checks.

Only the execution snapshot uses this directory. Internal consumers use
`activeDataCount`/`activeDataAt`; artifact encoding is unchanged. Mutable clones
expand it to the public representation. Every returned payload slice has clipped
capacity, and snapshot quotas account for the compact records, owner and payload
with the existing conservative rounding allowance. No API or artifact migration
is required.

A pinned Linux/amd64 experiment on Go 1.27.1 compiled esbuild with 100,000 active
segments and 5,546,669 payload bytes. Relative to the operand-arena checkpoint
candidate, compact descriptors reduced total allocation and post-GC retained
heap by 5.60 MB. Three alternating cold-process pairs showed unchanged peak RSS
(120.64 MB versus 120.71 MB). Separate phase diagnostics then identified
decoder growth and backend allocation as earlier high-water contributors.
After the separate exact data-vector decoder change, compact snapshots also
reduce the later publication peak; the combined cold comparison was
121.55 MB versus 111.80 MB.
These are retained-output savings, not a 50% compilation-peak claim. Raw
fresh-process RSS, allocation traffic and retained heap must remain separate
metrics; see `scripts/compilemem` for the measurement harness.

The extra private directory pointer uses space recovered by grouping an existing
private telemetry flag into padding. The amd64 `Compiled` footprint stays at
784 bytes; public fields and their meaning are unchanged.
