# Bounded final cast and array read

A final array cast followed by an index read and array.get used to resolve the
same object twice. The bounded fusion performs one checked resolution and reads
the element immediately. It keeps type, reference, span, null and bounds checks.
No pointer survives a call or safepoint.

This is a separate change from the held type-only experiment. The isolated
prototype starts at origin/main cd029409bd8985ab51d30e5f68714d13d00192fa.
The public follow-up is above PR948 head2c53a7838, which owns these paths.
Combined qualification preserves PR1006 cold pin restores in existing leaves.

The full compiler median falls from37.915011 to36.451915seconds (3.86% less
time) in three balanced serial Wago pairs. All output hashes, main42 and exact
root checks pass. No new V8 timing is run. See full-compiler-combined/summary.json
in the adjacent data directory. Full combined normal/checked suites pass147.157/139.056seconds; checked
benchmark-module suite27.179seconds. Public-branch feature tests pass normal
25.664s and checked+stats25.982s. Full commands/logs are saved under full-checks.
Daybreak Blue independently reviewed the change and reported no findings before
publication; see the adjacent daybreak-review.md for scope and test gaps.

First add strict runtime/codec/Throughput/Tiny/trap regressions and a structural
admission check. Benchmark cases use one Invoke per8192 reads, except pin-entry,
which uses one small entry with value-pinned global pressure. Negative cases add
nop or index arithmetic and must remain unfused. All errors remain diagnostic.

Setup attempt: /usr/bin/time does not exist; fixture compiler never ran. Use a
measured Python subprocess wrapper for later checks. No timing data discarded.

Red-v2: 0.530s, normal/codec/Throughput/Tiny semantic cases pass; twelve
positive admission expectations fail with zero fusions. Negative patterns stay
unfused. The pin-entry fixture has seven actual value pins. A separate one-
function cold-null regression fails because global5 becomes0 rather than6;
bounds, truncated-header extent, and successful read controls pass. This is the
old leaf's missing cold pin restore, already repaired separately in PR1006.
The new leaf must include that repair without changing unrelated old leaves.

Initial red24.039s also found an invalid fixture: an eqref array result needs an
explicit i31 cast before i31.get_u. Corrected fixture and original log retained.

Implementation: recognize a final indexed array cast followed by one i32 local
read or constant and same-type array.get/get_s/get_u. Inline/local-remap/custom
paths remain excluded. The same existing index emitter recovers lazy locals;
only a nontrapping index producer moves before the cast. Nonfinal types and
intervening nop/arithmetic/trapping-index patterns retain ordinary lowering.
All supported widths1/2/4/8/16 use a per-function noncollecting checked address
leaf. Return RAX is consumed by an immediate signed/unsigned/scalar/FP/vector or
zero-extending32-bit collector-reference load. Funcref/externref storage is not
admitted. Collector results receive their ordinary GC-root flag.

The leaf checks full reference high bits, null cast semantics, i31, canonical
final type, handle count, live space byte, backing span, handle size, logical
index, and accessed extent against both handle and header. u32 index scaling is
64-bit. Nullable null traps as array null; nonnullable null traps at the cast;
wrong type precedes index bounds. No address survives a call or safepoint.
Only actual R9-R11 value/local pins are saved, and all three cold exits restore
them before the shared trap tail writes dirty globals. Caller flush retires the
global-cell cache before marshalling. No additional broad invalidation.

Checked+stats v5 passed20.646s, covering codec, Throughput stress, Tiny, retained
reference across allocation, dirty-high-bit i32 arguments, every admitted width,
packed signs, cast-before-divide order, nonfinal exclusion, and direct null/i31/
bounds/truncated-header pin checks. Pin-entry has7value pins; generated saves
explicitly include R9/R10/R11. Both negative loops remain0fusions. Scalar and
reference hot loops each emit1resolver instead of2. Fixture ref read uses index0
with a nonzero adjacent compact reference, detecting an accidental64-bit load.

Diagnostic failures retained: first green build used nonexistent Reader.S32;
corrected to I32. Checked-v3 used the wrong expected text for division and tried
to inject a foreign-wide global through an entry that rejects noncompact roots
before execution. That case could not test the leaf, so the direct cold case was
removed; high-bit input rejection remains in the leaf and ordinary API boundary.
No production safety check was relaxed. Checked-v4 passed before test expansion.

## Matched micro qualification

Production commit b6e1e529884a717f31e00659f56ecff0d1bed178. Same final test source
and Wasm fixture in both normal binaries. Control replaces the three edited
production files with exact origin/main versions and removes the new leaf file
through Go's overlay; no common dependency or held type-only patch is included.
`control-overlay.json` and `prepare-control.py` reconstruct that source exactly.
The new leaf itself uses the already-qualified byte load for the space field;
combined qualification must compare against installed PR948's byte-load base.

Build commands and logs: matched-control-build21.122s, matched-candidate-build
22.869s. Normal candidate focused tests0.006sPASS; normal control semantic tests
0.007sPASS. Checked+stats v5 compilation/test20.646sPASS. All per-command logs,
including failed attempts, remain in the adjacent data directory.

CPU15, GOMAXPROCS1, Go1.27.1 linux/amd64,200ms/sample,3ABBA blocks (control,
candidate,candidate,control):72samples,12processes,17.986s total. Positive ranges
are completely separated. No samples discarded. All samples report0allocs/op;
scalar loop reports0–1B/op, all other cases0B/op. One operation is one Invoke,
containing8192guest iterations except pin-entry, which is one small entry.

| Case | Control median ns/op | Candidate median ns/op | Change |
| --- | ---: | ---: | ---: |
| scalar loop | 57053.5 | 28357.5 | -50.30% |
| collector-reference loop | 59166.5 | 32054 | -45.82% |
| global-pressure loop | 74845.5 | 44895 | -40.02% |
| actual seven-pin entry | 222.55 | 204.35 | -8.18% |
| nop negative | 57833.5 | 57538 | -0.51% |
| arithmetic-index negative | 57458.5 | 57661.5 | +0.35% |

The pressure loop mutates eight globals but has no value pins; the separate
pin-entry has seven actual value pins. The single-function cold test explicitly
checks the R9/R10/R11 push sequence and all eight committed globals after traps.
Normal semantic tests include collection between calls, forced allocation
collection, Tiny, and artifact reload. Full suites and full compiler timing are
owned by the parent integration lane; these micro numbers are not a full-job
speed claim. All heavy work stopped after the matrix; parent was notified.

Exact hashes are in micro-identity.json. Production patch SHA256:
e3465ec6ca067a5b7282bdcf7b7b416191c3e9e7bc0b65d6b6cb6127b4325fd1.
Control binary4356dcbfc34c36da9597b63553a4d27df21b333cfffac92a3a264cd7f0ae83e1.
Candidate binarybbf53b8fd605c7d096252356bc29b7b3cde1bed26bc79ad9c73354599ac7046f.
Fixturef662aa6d5938de93d2e285e92ea3c48bd967ad9f64d5fde98ee7ff698aad25af.

## Actual compiler admission

Ordinary public Compile with CoreFeaturesV3 and code-neutral compile telemetry
admits164fusions in156functions. Native code shrinks24,186,923→24,170,827bytes
(16,096bytes). Exact root metadata remains25,794safepoints,56,923callsites,
maximum77roots and1,983,045bytes. The diagnostic compile takes0.513seconds.
The new hot array_get_unchecked body is942bytes, with3native resolver calls and
one numeric inline; the old profiled body was1,180bytes with4resolvers.
The stats reader is diagnostic only and is not installed.
