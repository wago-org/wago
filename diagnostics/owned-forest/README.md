# Owned-forest boundary diagnostics

These patches preserve test-only investigation, not a compiler migration.
Apply stage0.patch to b5ab03f9. The miss-overlay and transition patches are
separate alternatives on top of Stage 0, not patches to apply together.

## Stage 0 and one-shot miss classification

Stage 0 (41d94a8c) passed eight synthetic fixtures, and all 145 modules
matched native output and serialized metadata. Its structural oracle
nevertheless found 111 missing-child observations at GP allocation
boundaries. The parity result does not waive those misses.

One untimed follow-up (overlay frozen at a811b99; classification 777d2fd)
examined only the 14 previously divergent modules. It reproduced all 111
observations and the original counter maps; all 14 native and serialized
metadata comparisons matched. Of the 111 observations, 101 came from a
folded memory comparison and 10 from a folded masked EQZ. Each fold emits
CMP or TEST and retires its child block before allocating a destination
for SETcc. At the allocator boundary the parent still carries an
expression-shaped tag, but its result is temporarily represented by the
condition flags. The stale child edge seen by the projection is not a
future live operand. This classifies the observed oracle misses; it is
not evidence of a legacy compiler correctness bug. Counts do not
independently establish all absent descendants or local ownership leases.

## Flags-result transition model

The source-reviewed transition patch af939c02 passed four focused tests
with 18 subcases and no skips, plus native and serialized metadata
comparisons for all 145 modules. The strict oracle reproduced the original
111 divergences exactly by module; the transition-aware oracle found zero.
There were 132 pending begins and 132 ends, with zero invalid ends.

The test model represents the pending i32 result with
an explicit flags token, scoped to the lowering that owns it, rather
than weakening the missing-child oracle. It retains checks for genuinely
live missing children and fences observations and root roles. It does
not cover fused interiors or all fixed-register and arena boundaries. It
does not migrate the operand forest, change native lowering, or establish a
performance improvement. Provenance of the virtual postorder before
reset remains unknown; the checkpoint makes no claim about it.

No timing, promotion, or changes to the active compiler defaults.

## Bounded actual-emitter fixture

Apply actual-fixture.patch to af939c02 (Stage 0 plus the transition patch,
without the miss overlay). Final fixture source 58430361 passed six
top-level tests and eight rejection subcases with the
wago_owned_forest_fixture test tag and experimental flags off. The first
test build failed because of a signed-immediate cast in the fixture; the
focused retry passed after that test-only fix.

The checks cover masked native lowering and execution, GP pressure with
actual call-wrapper preparation and a synthetic clobber, and scalar
agreement helpers. The memory case checks native code and trap PC but
does not execute an out-of-bounds access. Tests did not execute a real
call or opBlock path and do not establish full compiler coverage.
Unsupported paths reject explicitly. Prefix skips remain unported.

Measured structure sizes on AMD64 were 64 bytes for elem, 120 for stack,
192 for the cursor and 104 for the forest. These are fixture sizes, not
evidence of node shrinkage, net cost reduction, or a speed gain. No
corpus or timing run was performed for this fixture.

## Focused closure extension

The bundled actual-fixture patch now captures 05cfbad6; the preceding
58430361 result refers to the earlier checkpoint. Attempt three passed
10 top-level tests and 8 rejection subcases with zero skips. Exact native
comparisons and runtime results matched for local snapshot (29), effect
snapshot (119), actual zero-argument internal call (42) and actual
slot-profile block/br/end (44). The latter deliberately uses
`regMerge=false`; this is not general control coverage.

The first two attempts remain failed: the first omitted local metadata;
the second treated the default RAX as pinned and used a scratch register
that clobbered a captured load. Both issues were exposed on the legacy
side first and resolved by correcting fixtures, not expectations.

The source-reviewed instrumentation and register-merge rejection changes
were 58d5844e and 56871e26. The following are separate work units, not
subtractions or estimates of savings:

| Case | List link assignments | List visits | Forest root stores | Forest cursor writes | Forest visits |
|---|---:|---:|---:|---:|---:|
| Local snapshot | 52 | 5 | 11 | 14 | 5 |
| Effect snapshot | 36 | 3 | 8 | 8 | 3 |
| Actual call | 28 | 1 | 9 | 2 | 1 |
| Actual block | 40 | — | 15 | 0 | 0 |

List link counts are reached push/erase only; snapshot and copy accounting
is incomplete. At this checkpoint the measured sizes were elem 64,
stack 128, cursor 200 and forest 104 bytes. Memory checks cover trap site
and native parity, not executing an out-of-bounds access. The effect
fixture had zero spills; two spills in the separate pressure fixture do
not prove reentrant effect traversal under allocation pressure. Prefix
skips are still unported. No net cost, throughput, or migration claim.

## Focused safety closure

The bundled fixture now captures e887bc79. Attempt two passed 12
top-level tests and eight rejection subcases. A nested effect traversal
under load pressure selected the deepest victim, spilled twice, and
resumed the remaining loads. The executed trap fixture used actual
emitTrapStubs and Engine.Call. In both arms it ran a valid access at
address 0, trapped on out-of-bounds address 65535 at function 3, PC 37,
then successfully repeated the valid access. Native code matched exactly.

The first attempt at 77cb6845 failed the trap-PC check. The fixture had
omitted the absent module-global lease sentinel; e887bc79 initializes it.
This was a test fixture correction, not a production semantic fix.
These cases extend the earlier focused coverage, not the full corpus.
No timing, net-cost, node-shrinkage, or promotion claim.
