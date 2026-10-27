# Bounded trapping guard address reuse, 2026-10-21

Experiment only. No push, PR, or installation. New tree based on public PR948
`2c53a783828d7ae3ca2d3fe92946a249d6fdf278`. Previous cast-array fusion and type-only
experiments are unchanged.

Common dependencies, kept separate from this optimization:

- PR1006 `021d36398d2fc78091a3f22045aadbbeb62c6583`, cherry-picked as `23eb23189`:
  preserve pinned global values on existing final-resolver cold exits.
- Numeric leaf inlining `a59763770ad243b13e8c85488f6f3554b4bf71f3`, cherry-picked as
  `c6db3230a`: use existing actual inline admission and consume exact source masks.
  The original PR948 base disables all inlining with exact GC roots, so it cannot
  admit the target span without this already implemented dependency.

The red fixture requires one full same-final-struct resolution followed by one
raw-address reuse through an actually selected integer comparison inline and a
then arm that can only trap. The second field extent must fit the first checked
extent. No source call, control-flow, or general pure-if policy is broadened.
The cache must use a currently free existing certificate register and end at the
second load. Tests also require fallback for stores, local writes, both live arms,
a larger extent, a different type, a noninlined helper, disabled inlining, and a
scan budget excess. Runtime cases retain original null/cast/explicit-trap order,
dirty globals, codec round trip, forced collection, and a returned compact root.

Evidence and exact commands are in `trapping-guard-2026-10-21-data/`. No benchmark
will run during the parent's functional or timing lanes.

## Baseline red evidence

Fixture assembly: 0.010724 s. `go test -p 2 -tags=wago_codegenstats -run
'^TestGCTrappingGuard' -count=1 -v ./src/wago`: 24.343260 s, expected failure.
Runtime/codec/forced-GC tests pass. Admission fails only at four required sites:
`read`, `nullable`, `roots`, `dirty` each has zero reuse, two native resolvers and
one actually selected inline. Exact source-mask counts and all negative cases
pass. Baseline `read`: 555 code bytes, 72 frame bytes, 2 pinned locals, 0 spills.

## Initial implementation diagnostics

The first draft tripped its explicit address-lease invariant before runtime.
Normal/stats build 24.546793 s and checked/stats diagnostic build 28.600524 s.
The normal command accidentally named `regalloccheck`; that unsupported tag does
not enable the checker. Only the second command used `wago_regalloccheck`.
Both logs remain. The invariant reported `valid=true`, register RBP, pin mask
zero after the numeric inline. The first resolver's operand flush had cleared a
pin reserved before any certificate existed. The corrected draft establishes
both the certificate and pin only after the first full resolver returns, checks
that the chosen register is still free, then holds it through the planned prefix.
No fact is restored after an ordinary call or an allocator-visible unpin.

The expanded fixture uses a heap-allocated cell as the returned compact root,
not an immediate i31. With collection on every allocation, the retained second
field must still resolve to value17 after another allocation. Packed signed and
unsigned second reads and same-field integer reuse are explicit positive cases.
Floating-point and SIMD second fields are currently excluded. The comparison
must be exactly two i32 parameters, two ordered local reads, one comparison, end;
no added locals, control, helper, import, or division is admitted.

The prefix uses the ordinary inline call emitter, if emitter and else emitter.
It does not modify dispatcher invalidation or pure-if eligibility. At the else
edge, the first successful field access dominates the only returning path. A
runtime compiler assertion checks the cache register's lease after each emitted
prefix instruction. The certificate is released immediately after the second
field load, before later source code. Its register is never an operand/root and
no new frame slot is allocated. Existing exact source-call masks are advanced
once by the ordinary call emitter. PR1006 supplies first-resolver cold restores.

## First corrected checked result

`green-v3` passes in 23.280488 s (checked + stats). The normal read and nullable
read each use one resolver, down from two. Native code falls from 555 to531 bytes;
frame stays72 bytes and spills stay0. The actual address register is RBP. The
pressure case has seven local pins and does not reuse. All fallback controls,
exact source call-mask counts, null/cast/unreachable order, truncated first
extent and dirty-global checks pass. Packed signed/unsigned and same-field reads
pass. All runtime tests include codec reload and throughput/Tiny profiles.

The stats revealed that `struct.new; drop` in the root test was optimized to a
dead allocation. The strengthened fixture stores that allocation into an
observable global instead. Its next checked run must prove this result before
forced movement coverage is claimed. `green-v3` is not that proof.

## Planned matched micro

The control overlay restores the exact common production base `c6db3230a` in
three touched files and removes the new prefix emitter, retaining identical final
tests and fixture. No runtime experiment switch is added. `prepare-control.py`
reconstructs these inputs and records hashes. Use normal binaries, CPU15,
GOMAXPROCS1, 200ms per case, three ABBA blocks, no discarded samples. `loop` does
8192 guest calls through the positive wrapper and checks the heap cell value.
`same-field` is one small entry. `negative-loop` has two returning arms,
`noninline-loop` has an actual comparison call, and `pressure` is one entry with
seven local pins. Benchmark construction and binary builds happen before the
quiet timing lane. Parent owns full compiler qualification and independent
Daybreak Blue review before any publication.

The block-signature scan checks indexed signature arity before allocating any
frame-type slice. Rejected large signatures cannot expand the 64-byte/32-op
lookahead budget. Added an indexed zero-parameter/single-result positive and
parameterized/multiple-result negative controls. Matching is limited to16 attempts
per function, and never resets that budget after a successful use.

## Strengthened root proof

`green-v4` ran in29.913719 s and exposed a test API mistake: source return-PC
callsites and allocation safepoints are different streams. Runtime, codec,
observable allocation, admission and cold-trap tests passed; only the new count
assertion used the wrong stream. `green-v5` passes in25.946965 s, including the
existing numeric-inline tests. It requires exactly one allocation safepoint,
containing only the saved second-field reference (local2), with no dead-new
rewrite. The original comparison source mask remains counted exactly once;
its native callsite is absent with inlining and present without it.

Before matched builds, added explicit runtime checks of all three8192-iteration
benchmark loops, dirty high i32 argument bits, and later-larger-extent trap order.
The first normal control build caught a test table using `uint32` instead of
`TrapCode` (3.343386 s); its diagnostic is retained and the type is corrected.
No production change was needed for either test correction.

## Final focused checks and binaries

Final checked+stats `green-v6`: 21.900872 s PASS, including all guard admissions,
root-mask checks, actual allocation root contents, nullable/nonnullable first
traps, later larger-field trap ordering, both integer high-bit inputs, packed
reads, seven-pin fallback, observable collection, codec reload and existing
numeric-inline regressions. The narrow source-plan adapter is integer-only;
GC code uses the physical allocator checks plus the explicit address-lease
assertions, exact-root tests and runtime tests described here.

Normal control/candidate binaries built in22.510250/24.751760 s. Both run the
same final runtime tests, including all benchmark loop outputs, in0.006748 and
0.007255 s. No benchmark timing has run at this commit. Production changes are
only `call.go` (shared actual inline selection), `compile.go` (one bounded scan
counter in existing padding), `gc_struct.go` (one dispatch hook), and new
`gc_trapping_guard.go` (the complete bounded prefix plan/emitter). Existing
resolver physical checks and cold trap restores are unchanged.


## Matched micro result

Production commit `46a9e4e9909cdf18bafd6b57acf1f3dbed6d6d0d` was held fixed.
The normal binaries above ran on isolated CPU15 with GOMAXPROCS1, 200ms per case,
in three ABBA blocks. All 12 processes passed in14.925363 s; all60 samples are
retained in `micro-samples.json` and the12 raw logs. No sample was discarded.
`qualification.json` records production patch and binary hashes and platform;
`source.json` records the actual overlaid source hashes.

| Case | Unit | Control median ns | Candidate median ns | Change |
| --- | --- | ---: | ---: | ---: |
| loop | 8192 guest calls | 83186 | 65747 | -20.96% |
| same-field | one small entry | 238.75 | 231.95 | -2.85% |
| negative-loop | 8192 guest calls, two returning arms | 83768 | 83604 | -0.20% |
| noninline-loop | 8192 guest calls, actual comparison call | 88090.5 | 87885 | -0.23% |
| pressure | one entry, seven actual local pins | 241.85 | 244.05 | +0.91% |

The loop gain is clear in this fixture: control82–86us versus candidate65–67us.
The fallback pressure samples overlap (control240.6–263.5ns, candidate243.3–256.9ns).
All samples report0 allocs/op; some loop samples report1–2B/op, retained verbatim.
These measurements support a full compiler comparison; they do not establish
whole-job speed. Root owns that comparison, full suites and independent Daybreak
Blue review. No publication or install was performed here.

`static/prepare.py` and `static/capture_test.go.txt` provide a diagnostic overlay for
actual admissions on a chosen combined checkout. They use ordinary public
`Compile` with V3 and telemetry, so default exact-root/helper/inline policies
apply. The overlay captures per-function guard counts, all rewrite counts,
actual local/global pin counts, frame bytes, spills and code bytes, along with
whole-module native bytes, compile time and exact-root admission. It asserts at
least one real admission; it does not silently accept an absent optimization.
The helper is source only at this commit and has not been run. Usage:

```sh
python3 docs/research/trapping-guard-2026-10-21-data/static/prepare.py /absolute/combined/wago
```

From that checkout, set `WAGO_REAL_COMPILER_WASM` and `WAGO_REAL_COMPILER_STATS`,
then time `go test -p 2 -tags=wago_codegenstats -overlay=/absolute/path/to/static/overlay.json -run=^TestRealTrappingGuardStaticAdmission$ -count=1 -v ./src/wago`.
Generated overlay inputs must stay local; the candidate contains no diagnostic
capture hook. This read-only count is separate from performance timing.


## Diagnostic source packaging

Saved diagnostic test source uses `capture_test.go.txt`; the preparation script
creates `hook.go.txt`. The overlay still maps these files to virtual `.go` paths
under `src/wago`. This prevents repository-wide Go package discovery from
compiling research evidence as a separate package. No tracked `.go` file remains
under `docs/research` in this checkout. Runtime source, tests, WAT and Wasm
fixtures are unchanged; no build or test was run for this packaging correction.

Saved control and diagnostic Go source uses `.go.txt`; reproduction overlays
retain virtual `.go` names without adding packages to the repository.
