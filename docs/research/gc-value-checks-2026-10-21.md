# Per-value checked reference scratch

## Behavior

Use a small compiler table to reuse successful final-struct reference checks.
The table holds eight values at a time. Local copies share a value identity;
assignment gives the destination a new identity. Owned stack/register values
keep their old identity. Repeated reference fields have child identities, but
a checked parent never proves that a child reference is valid.

Each selected producer saves the checked object address and the smaller of
the handle and header extents in private, non-root frame homes. A later read
of that value and final type loads the address directly. A larger later read
checks the saved extent at its original instruction. There is no runtime
table search. The first read keeps all original physical and type checks.
Successful test/first-field prefixes can also produce a checked address.

Calls, allocation, writes, control edges, and unknown operations clear facts.
Inline bodies, custom instructions, and modules with exception handling do
not use this table. This first version does not reuse array bounds checks or
retain facts across loops or calls. Existing root masks and safepoints stay
in force. The native GC ABI and artifact format do not change.

## Storage and planning

A linear source scan finds producers with later uses. This scan only decides
whether a home store may pay off; the emitter's current value/effect facts
authorize every reuse. The scan caps each working map at 256 entries and
selected producers at 4,096. State belongs to one function and compiler
worker, not the whole module. The runtime frame grows by at most 16 eight-byte
homes in an admitted function. Cached raw addresses are never GC roots.

Compiler-only cache homes have fixed offsets outside source-local slot
packing. A review found that recording these homes as source locals disabled
packing. A regression test reproduced the recorder overflow; using fixed
offsets fixes it while keeping address encoding diagnostics.

## Evidence

The first three balanced full Dewdrop pairs use the same compiler Wasm,
request, isolated host patch, affinity (CPUs 12 and 14), and GOMAXPROCS=2.
Each run has fresh native and result caches. The control is the combined
local GC stack at `40cfab8d8ec32ca3eff1cd178fa746cb2eeb82b0`.

Control times: 36.8491, 35.8619, 35.3928 seconds. Candidate times: 36.1797,
36.8873, 35.0051 seconds. Medians are **35.8619 and 36.1797 seconds**.
The 0.89% difference is within overlapping results; **there is no clear full
compile speed gain**. Median process peak RSS is 1,140,900 versus 1,140,700
KiB; no memory gain is claimed. All output Wasm/cache hashes match and main
returns 42. The known V8 reference remains four seconds and was not rerun.

The focused 8,192-iteration read benchmark has medians 135,233 and 79,859.5
ns/op: **40.95% faster**. This is a focused result, not the full compiler gain.
Those original runs precede the slot-packing fix. Final source/code identity
and qualification results are recorded with the linked evidence.

Tests cover copied and replaced locals, old stack snapshots, interleaved
values, reference fields, failed type tests, later larger fields, writes,
branches, register pressure, forced collection, tiny heaps, saved artifact
reload, and corrupt handle/header/child metadata. A larger field must still
trap when either saved size is too small. A small read must still succeed.

The first full-suite attempt failed because this new worktree had no pinned
WebAssembly test corpus. Its diagnostic log is retained. Reusing the existing
pinned corpus fixes the test setup without skipping tests or changing pins.

Raw timings, final code identity, test logs, scripts, and review results are
under `docs/research/data/gc-value-checks-2026-10-21/` in this change.

A later Daybreak Blue review found two fused reference-field result paths and
a final interval-local transfer that did not preserve value IDs. They now
propagate those IDs. The interval transfer has a regression test that failed
with ID 0 instead of 7 before the fix. Inline read regions still cannot
authorize cache reuse; preserving an identity does not extend its lifetime.

The final review also found ordinary typed struct reads losing child IDs.
A new typed-child admission test reproduced zero reuse; after propagating the
loaded child's ID it saves one full check. Both the source planner and emitter
now keep a touched-local list. Region resets visit only used local IDs, rather
than clearing every declared local at every barrier. Large sparse-local
benchmarks changed from 66,913 to 6.896 ns per emitter reset and from 3,703,690
to 41,181 ns per 1,024-region planning pass. These measure compiler analysis,
not GC execution. Regression tests and raw logs are retained.

Daybreak Blue's final narrow review found no concrete issue in the direct-child
identity or touched-local reset fixes, and no safety, ABI, or root defect.
The review was read-only; test results come from the separate measured runs.

## Exact final-source qualification

The final source removes 4,429 static repeated resolutions in 1,006 of the
7,012 compiler functions. Native code is 25,936,107 bytes versus 25,622,571
bytes (+1.22%). Its SHA-256 is
`a05803adec9bf1a686c03be958c8cc77e21f4a09211538da26e878d386ac75a6`,
identical to the first measured candidate despite the later coverage and
compile-analysis fixes. Exact-root admission stays unchanged: 25,794
safepoints, 50,438 callsites, maximum 77 roots, 1,803,076 metadata bytes.

A diagnostic compile takes 0.477 seconds for the control and 0.521 seconds
for the final candidate. These include telemetry and are not full-job latency
or production process memory figures. The diagnostic executable builds took
25 seconds; this is Go test/link work, not Wago module compilation.

Fresh balanced pairs use a newly built exact-source host (build 7.629 seconds),
the same pinned workload and normal diagnostic-free options. Control times
are 34.0751, 33.8384, 33.7399 seconds; candidate times are 33.0077, 32.8877,
32.7437 seconds. Medians are **33.8384 to 32.8877 seconds (2.81% faster)**.
Each candidate beats its paired control. Earlier pairs overlapped, so retain
that limit rather than treating the final result as a larger general claim.

Median whole-process peak RSS rises from 1,096,388 to
1,141,100 KiB: **+43.66 MiB (4.08%)**.
This is the measured cost of the final version; no memory saving is claimed.
All six output Wasm/cache hashes match, validation passes, main returns 42,
and all six artifacts retain exact-root admission. The four-second target
remains unmet (the final median is 8.22 times that reference).

Validation: the full combined normal and checked suites and checked benchmark
suite pass. The full public suite passes. After the final review fixes, both
combined and public AMD64/runtime package suites pass again, with checked and
profile coverage. Earlier missing-corpus and wrong-working-folder diagnostic
failures remain in the evidence. The final Daybreak Blue review reports no
concrete issue. Inline, branch, call, mutation, and independent child-check
limits stated above still apply.

Final focused benchmark rebuilds also pass: median 135,399.5 to
79,198.0 ns per 8,192-read loop (**41.51% faster**).
Both variants reload the same final fixture and verify the result. This remains
a focused benchmark; the full-job result is the separate 2.81% result above.
