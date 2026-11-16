# Checked queued-scan reuse comparator

Build `wago_gc_compact_age2_queue_reuse` alongside the explicit age2 policy tag.
The default comparator remains unchanged. This changes only preflight scanning:
each queued object was already checked by visit (live start/range, complete
layout and capacity), before its mark and queue entry were published. There is
no heap mutation or external callback between enqueue and scan. Reusing that
validation for the queued object's own layout is therefore safe. Every child
edge still enters visit and receives its original validation. Remembered old
parents are not enqueued during a minor and retain full checks.

Copy/rewrite/forwarding, dirty rebuilding, geometry, allocation accounting and
policy are untouched. No extra state or unchecked-reference mode. Full and
minor capacity failures still occur before authoritative mutation.

Source prepared; independent review and qualification pending. Existing age2
OOM/cycle/alias/race tests remain applicable. Added malformed queued/remembered
type and child-edge rejection with unchanged heap snapshots. No speed claim.
