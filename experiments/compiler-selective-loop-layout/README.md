# Selective layout source experiment (unqualified)

Base c884863d; retains prior poll64/padding experiments, disabled by their
selectors as appropriate. No measurements or test execution at this checkpoint.

`wago_experimental_scalar_loop_entry` permits startup selector
`WAGO_AMD64_SCALAR_LOOP_ENTRY=off|0|8|16|24` (defaultoff). At ordinary
interruptible opBlockPlain loop entry only, replace existing alignment with up
to31 NOP bytes BEFORE controlSite and original poll. Backedges target poll, not
padding. No synthetic vector fallback/fast-loop alignment is changed; no poll
removed or moved. Selector targets function-local offset modulo32, not a blanket
promise about absolute native addresses after linking. Final-image fixture tests
explicitly verify actual target and unchanged backward poll edge.

`wago_experimental_linear_sum_outlined_poll` permits startup selector
`WAGO_AMD64_SUM_POLL_OUTLINE=0|1` (default0). It affects only sparse proven-range
cooperative sum grouping (poll64 policy). TEST remaining,60 branches on zero to
a cold spill/poll/reload block after group, remainder, combine and fallback
code; normal success jumps over that block. It rejoins the original CMP/JAE,
so flags from poll never control iteration. All four accumulators remain live
through the same save/restore; no extra resident register/slot, no bound change.
First/final/tail/fallback polls retain semantic order. The group poll's static
position is now after the tail; deterministic tests remap logical group/tail
site indices without removing any assertion. Final native fixture asserts
mask/JE, adjacent hot CMP/JAE, cold location and return before compare.

Both tags absent preserve old production branches. Tagged selectors off also
preserve old behavior. Hypotheses: once-per-entry alignment may avoid repeated
blanket-poll NOP work; outlining shrinks common grouped-loop footprint at cost
of an extra branch on the rare poll. Neither is a measured gain.

## Targeted qualification and build blocker

At combined source03bd80bd, targeted Go sparse lazy/budget/dense caller parity,
layout final-image targets, sum delayed/count/values/fallback/sparse4GiB passed
in default mode and outlined mode with header offsets0/8/16/24. No full suite.

One official matched TinyGo adapter build was attempted with default stack and
all experiment tags, selectors off. The disk guard terminated the entire build
process group after temporary free space fell below512MiB (sampled minimum
508334080bytes). No binary exists; no semantic smokes were run. This is a
capacity blocker, not a compiler/test failure. Existing prepared receipt and
source are retained; no caches/files deleted. After authorized capacity relief,
resume exact receipt build command rather than preparing a duplicate source.

## Resumed build and semantic smokes

User-authorized Go cache clearing restored temporary headroom. Resumed the
exact prepared command at evidence-only successor26026e06; receipt preserves
03bd80bd predecessor and restart reason. One adapter built successfully,
SHA256 a4fbe1f464900ead5f3d4337c3ab95c95e2873c750c842850c44c8e99d229a32.
Default-stack/tasks/conservative/opt2, all eight experiment tags. No repeated
targeted tests or broad suite. Four exact semantic smokes (sum,bitmap,GEMM,
yyjson) passed with defaults and outline1+scalar offsets0/8/16/24:8 paired
records per mode,40 total, all hashes/unique workload pairs verified.
Padding0 and mapped override unset throughout. No performance/RSS claim.

## Bounded selective entry mode (source-only)

New `WAGO_AMD64_SCALAR_LOOP_ENTRY=selective` under the existing explicit tag.
Only ordinary interruptible zero-param/result loops without EH/custom handling
or diagnostic poll padding qualify. Predict the existing AlignLoop header;
select only oldmod32=16 whose actual RSI encoding places guest start at30.
The poll byte count is generated once with the actual encoder and tested against
emitInterruptCheck, not a hardcoded generic length. Existing compact32 policy
is unchanged. Incremental padding is capped at64bytes/function (four selected
headers); exhausted/unknown cases retain old policy. Scalar budget is reset by
existing fn{} initialization. No new decoder scan. Final native images remain
the authority for absolute placement after linking.

Mode0 and selective use existing multi-byte AlignLoop32; other explicit offset
modes remain diagnostic. Codegenstats records selected count, emitted padding
bytes, incremental bytes, and policy/shape/budget skips without map allocation
in non-diagnostic builds. Focused source tests cover all32 prepad positions,
actual poll length/backedge target, four-header cap, and fallback exclusions.
Existing sum cadence/interruption tests remain unchanged. No tests/build yet.
