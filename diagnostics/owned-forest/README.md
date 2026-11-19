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

## Focused scalar-module batch

The cumulative fixture now captures c9b12d78. Attempt two passed 14
top-level tests and 36 subtests, with no skips, retaining the safety
fixtures. Across 56 compile attempts there were 36 supported legacy
executions, 16 existing shared-scalar bypass executions, two list-only
inline control executions, and two rejected candidate inline attempts.
All 26 native comparisons matched. Bypasses do not count as forest
activation; the inline-selected ownership path remains unsupported.

Natural non-inline argument-call compilation reached two legacy bodies
and 529 opcode observations and returned 57, with exact native parity.
There is no separate call activation counter. The effectful IF reached
17 opcode observations and matched native code and both branch results.
The retained safety checks cover reentrant borrowed-load spills and
executed bounds-trap recovery.

The first attempt remains a failure: pure IF cases did not reach the
expected legacy dispatch, the inline call exceeded the supported scope,
and the interval eviction test did not activate interval state. The
sole-root transfer change (8870afb2) preserves pending local bookkeeping;
fb04ae5e preserves existing shared-scalar engine priority and counts that
path as a bypass rather than forcing forest activation. Prefix policies
remain unported. This is focused correctness evidence, not full corpus
coverage, timing, storage reduction, or a net work claim.

## One-pass public Compile corpus census

One serial untimed run at c9b12d78 retained 290 public Compile calls with
one worker and all experimental flags off. The baseline passed all 145
native and serialized-metadata comparisons. The candidate completed
48 of 145 modules, with exact native and serialized parity for all 48.
The gate failed; no retry or Wasm runtime execution was performed.

Of 97 candidate failures, the frozen raw classification was 33 scope
rejections and 64 unexpected errors. A separate read-only audit attributed
33 of the unexpected errors to an explicit specialized-state guard omitted
from the allowlist. The remaining 31 were unresolved recovered nil-pointer
backend panics. The audit does not revise the raw classification or turn
the failed run into a pass.

The 48 completed modules contained 49 legacy/forest bodies, out of 519 on
the baseline corpus, and one shared-scalar bypass. They dispatched 15,254
of the 357,073 observed baseline opcodes (4.27%). Results from partially
compiled failed modules are excluded from completed-module coverage.

Separate observations on the 48 completed matched modules:

- List: 85,780 reached link assignments; 3,540 shared-cursor visits;
  3 spill-prefix visits; 1,075 effect visits.
- Forest: 18,156 instrumented root stores; 7,389 cursor-frame writes;
  2,892 cursor visits; 172 growth events; 10,754 retirements; 13 spills;
  5 pending-flags transitions.

These units are heterogeneous and incomplete, and cannot be subtracted
into a savings estimate. Existing prefix policies and snapshot/copy
coverage remain incomplete. The observed cursor depth peaked at 7 and
retained root capacity at 32 pointers; elem measured 64 bytes. These
are not allocation, RSS, speed, or node-shrink measurements. Later
fixes and focused tests are not part of this census.

## Untested selector-read checkpoint

Apply selector-reads-untested.patch on top of c9b12d78 (the cumulative
actual fixture). It archives source-reviewed 72d31b26 and tests
0af32c65 and 2ea18fc1. The source changes route paired-division and
repeated-local selector reads through forest roots. Patch checks passed;
test execution was held, so no focused run occurred.

The original 31 recovered nil-pointer panics remain unresolved by
execution evidence. A source-only audit correlated 18 with division and
13 with arithmetic paths; those associations are not proven causes.
The prior failed corpus census and its raw classifications stand.
This snapshot provides no new corpus, runtime, or performance result.

## DROP source checkpoint

Apply drop-focused.patch on top of 2ea18fc1. Source a20407fc routes pure
dropped trees through explicit forest ownership, and f878b8f0 adds
five independent valid scalar cases. The source change passed review;
the focused gate passed on clean f878b8f0: 20 ordinary valid-input
execution images and 10 exact native pairs. Each candidate used one
legacy forest body, dispatched 8–14 opcodes and recorded positive root
stores. No shared-scalar bypasses or rejections occurred. No exceptional
inputs, crash reproductions, or corpus checks were run.

On a20407fc, the ordinary existing TestRepeatedLocalEarlySelection and
TestDivRemConstPairValues suites passed two top-level tests and 66
subtests without skips. This was not an owned-forest selector or DROP
runtime gate. The 31 original corpus panics remain unresolved by
dynamic evidence. No new corpus or performance result is claimed.

## Focused constant-identity retirement

Apply identity-focused.patch on top of f878b8f0. Source ea6719f4
handles the three identity-fold paths where discardSimple retires
the second root; 5e1316ca adds independent ordinary-input tests.
At clean 5e1316ca, six direct folds passed in both arms. The decoded
gate covered 24 valid execution images and 72 runtime calls, with
12 exact native pairs. The 12 candidate images each used an actual
legacy forest body and dispatched 150 opcodes in total.

The direct invariant tests cover local-reference storage. Other
storage-form cleanup was source-reviewed but not individually
exercised by those tests. No exceptional-input, crash, or corpus gate
ran; the 31 original corpus panics remain unverified. This is focused
correctness evidence, not a coverage expansion or speed claim.

## Focused scalar branch tables

Apply brtable-focused.patch on top of 5e1316ca. It archives 3149f705,
including implementation commits 61f3ee94 and df6deac0. The gate
passed 20 ordinary valid-input images and 112 runtime calls, with
10 exact native pairs. All 10 candidate images used the forest
without fallback. Same-compile counters showed chain dispatch
(jump 0, compact 0), dense delta (1, 0), and compact IDs (1, 1)
in both arms and both interruption modes.

Coverage includes i32 single-result nested blocks with retained
prefix and payload snapshots, repeated and default-shared labels,
default-only dispatch, ordinary out-of-range default, and a finite
zero-arity loop/block case. No separate register-merge activation was
observed. This does not verify i64, multivalue, floating point,
exception handling, function targets, or the 31 original corpus
panics. No exceptional-input or corpus run, timing or net savings
claim, or default change.

## Focused direct register-ABI call lanes

Apply direct-call-focused.patch on top of 3149f705. It archives
implementation 275111a6 and tests at clean 87b15a63. The ordinary
valid-input gate passed 12 execution images and 36 runtime calls,
with six exact native pairs. Every image recorded one actual
register-ABI call. The natural local-result i64 fusion activated
once in both arms and both interruption modes; the other cases
recorded no such fusion. Each candidate image reached two legacy
forest bodies with no bypass, inline gap, or rejection.

The gate covers direct void and single-i64 return lanes, not
wrapper, inline, tail, host, or wider ABI cases. Serialized metadata
was not compared. No exceptional-input, corpus, original 31-panic,
or timing check ran. No performance or default change is claimed.

## Focused straight-line integer inlining

Apply inline-focused.patch on top of 87b15a63. It archives
implementation e3f1d2f7 and independent tests at clean 52e609f2.
The ordinary valid-input gate passed 20 execution images and
60 runtime calls, with 10 exact native pairs and zero candidate
rejections. Same-image counters confirmed one add-constant splice,
one add-parameter splice, two parameter-forward splices, two
scratch-local splices with no forwarding, and one store-leaf
splice in their respective cases. The scratch-local case checked
a fresh zero value before each splice's write. All callers used
legacy forest; shared-scalar callee bodies were counted separately.

Native byte parity does not establish serialized metadata parity.
No recursive, control, tuple, tail, wrapper, exceptional-input,
corpus, original 31-panic, or timing coverage is claimed. The
partial work counters do not establish net savings. Defaults
remain unchanged.

## Focused scalar interval ownership

Apply interval-focused.patch on top of 52e609f2. The archive captures
b8347700, including interval changes 6ee5a9a3, 8d89f425, 2bc45e30
and 969b6275 and tests 473fb306 and b8347700. The first attempt did
not pass: three fixtures selected default shared scalar rather than
interval lowering. The call and paired carrier checks passed.

The second attempt used observable in-bounds stores and loads to
reach interval lowering without optimizer overrides. It passed 16
ordinary execution images and eight exact native pairs, with results
211, 234, 240 and 241. Interval-region counters activated in every
image. Straight-line images recorded 20 final transfers each and
a maximum of 11 active entries. The IF and call control counters
activated once per image and reached at most two active entries.
The non-inline register-ABI call was confirmed.

The paired carrier check emits moves only; no two-load flush or
direct stale-leaf/current-home case was tested. No broader exceptional
input, corpus, timing, or performance claim. Defaults are unchanged.

## Focused ordinary wrapper calls

Apply wrapper-focused.patch on top of b8347700. The focused gate at
clean 3383092b passed eight ordinary images and 24 runtime calls.
Four pairs matched exact native code and encoder-exported JSON
metadata. Every image recorded one actual wrapper call; candidates
reached two legacy bodies, with no alternate ABI or rejection.

Encoder-exported JSON parity is not public artifact codec parity.
This focused case does not establish broad wrapper, corpus, timing,
or prior-defect coverage. Compiler defaults remain unchanged.

## Censored 114-module compile census

One untimed run at 3383092b made 228 public Compile calls. The same
31 held modules were excluded from both arms before loading. The
baseline matched native output and public codec metadata on all 114
included modules. The candidate completed 77 modules with actual
forest dispatch, all matching native code and public codec metadata
exactly (75 forest-only, two mixed, none shared-only). The other
37 were explicit scope gaps; no unexpected or accounting errors
were observed within this subset. This is not a 145-module gate,
a repair verification for the held 31, or a runtime or timing run.

First blockers: opcode 0x2b (14), 0xfc (10), 0x44 (4), 0x11 (3),
and 0xb8 (1); selected inline ownership (2), specialized function
state (2), and adjacent div/rem selector (1). These are first
reported blockers, not exhaustive diagnoses of the modules.

Matched completions dispatched 31,358 opcodes in 80 legacy bodies
and included 301 shared-scalar bodies in each arm. Baseline counters
recorded 184,092 list link writes, 23,805 cursor visits, 2,739 effect
visits, and 18 spill-prefix visits. Forest counters recorded 38,784
root stores, 60,812 cursor writes, 22,597 cursor visits, 23,074
retirements, 297 grows, 165 spills, and six flags transitions.
These are incomplete and different units, not a net work estimate.
Candidate matched maxima: root capacity 64 pointers (512 bytes),
34 live roots, and cursor depth 7; measured struct sizes were elem
64, cursor 200, and forest 104 bytes.

## Focused ordinary bulk memory

Apply bulk-focused.patch on top of 3383092b. The gate at 81380949
passed 28 images and 84 valid calls, with 14 exact native and
encoder-exported JSON pairs and full-memory alias checks. Observed
selectors were copy unroll, exact-chunk copy (39 bytes), fill unroll,
vector fill, and fill-to-copy forwarding. Fixed-address copy and copy
append did not activate.

Scope was in-bounds memory.copy and memory.fill only (FC 10 and 11).
General floating point remains fenced; the vector path uses scratch
only. No other FC operations, exceptional input, held modules, full
corpus, public artifact codec parity, or timing are claimed.

## Focused private scalar floating point

Apply fp-focused.patch on top of 81380949. At ffa9cd4f the
ordinary gate passed 48 images, 96 calls, and 24 exact native
and encoder-exported JSON pairs. It recorded 16 float local
sinks, 16 deferred compares for branches, and 16 branch fusions.
Three paired direct emission-only checks covered eligible XMM
victim selection, a nested XMM/GP effect walk, and recursive XMM
spilling initiated from GP allocation. These do not establish
spill counts in compiled modules.

This is a private scalar FP lane with integer or void ABI
boundaries. Inline admission checks for FP intermediates before
binding; no reduction in preflight or cursor overhead has been
measured. Encoder JSON is not public artifact codec parity.
No exceptional-input, held-input, corpus, or timing gate ran.
The earlier censored result remains 77 of 114, not an updated
coverage estimate. Compiler defaults are unchanged.

## Focused private FP block results (Stage 2-A)

Apply fp-block-focused.patch on top of ffa9cd4f. At 81c24eed the
ordinary gate passed 32 execution images and 128 valid calls, with
16 exact native and encoder-exported JSON pairs. The emitted
agreements were 24 FP register merges and eight slot agreements
with merge requested and XMM11 pinned. That observation does not
establish that the pin was the sole cause of slot selection.

The ABI remained integer-only. Coverage was private explicit single
f32/f64 BLOCK and IF results, ordinary br/br_if, and qNaN/signed-zero
bit carriers. No FP loop, br_table, type-indexed block, FP call or
function ABI coverage is claimed. Encoder JSON is not public artifact
codec parity. No held-input, exceptional-input, corpus or timing
run occurred. The censored coverage figure remains 77 of 114;
compiler defaults remain unchanged.

## Focused FP exported ABI (Stage 2-B)

Apply fp-abi-focused.patch on top of 81c24eed. At ff082492 the
ordinary gate passed 48 images and 192 calls, with 24 exact native
and encoder-exported JSON pairs. All images entered the exported
adapter through Engine.Call, with a distinct InternalEntry and
positive HostAdapterBytes. Observed emitted agreements were 16 single
results in XMM0, 24 single results reloaded from canonical slots,
and eight void cases.

The checks cover two or four all-FP parameters and at most one
result, including f32 low-word inputs with dirty upper bits, finite
values, qNaN, signed zero, and an old parameter alias. Five through
eight FP parameters remain unverified. No typed prepared-direct,
plain non-register-ABI wrapper, mixed or internal call claim.
Encoder JSON is not public artifact codec parity. No legacy defect,
exceptional-input, held-input, corpus, or timing result is claimed.
The censored result remains 77 of 114 and defaults are unchanged.

## Focused scalar FP and bulk-memory composition

Apply fp-bulk-focused.patch on top of ff082492. The focused gate
at 054dc7f8 passed 32 images and 96 ordinary in-bounds calls with
16 exact native and encoder-exported JSON pairs. Eight images
activated exact-chunk copy and eight activated vector fill. The
fixtures checked whole memory after copies in both overlap
directions and disjoint copies, plus raw captured FP prefixes.

The gate does not establish dynamic scratch use with live XMM
values, dirty FP home rereads, spilling under pressure, public
artifact codec parity, or corpus or timing results. The prior
censored coverage remains 77 of 114, and defaults did not change.

## Composed FP censored compile census

One compile-only run at 054dc7f8 made 228 public Compile calls. Both arms
excluded the same 31 held modules before reading workload blobs; both
enabled the experimental FP lanes, with forest ownership alone varied.
All 114 baseline modules matched. The candidate completed 83 modules
with actual forest dispatch; all 83 matched native code and public
serialized metadata exactly. There were 30 explicit scope rejections
and one unexpected backend error, so the gate exited 1 and did not
qualify. The unexpected libtommath adjacent-div/rem result is retained
as classified in the original run.

The 83 completions included 115 legacy bodies, 303 shared-scalar bodies,
and 52,409 opcodes. The 31 abandoned modules had 70 attempts, 39
completed functions, 31 rejections, and 24,555 opcodes; these are not
completed-module coverage.

On the matched modules the list recorded 316,004 link writes and 55,053
cursor visits. The forest recorded 66,620 root stores, 136,974 cursor
writes, 52,823 cursor visits, 39,880 retirements, 323 grows, and 195
spills. These are distinct, incomplete units and do not establish
net savings. Completed forest root capacity peaked at 64 pointers
(512 bytes), live roots at 34, and cursor depth at 7.

No guest execution, timing, held-input repairs, or default promotion.
This is a separate censored result, not a replacement for the earlier
77-of-114 result or a full 145-module qualification.

Summary SHA-256:
`a339f1d13798f96cd1699e49ee26bba50159809c659b22336bb9a0666af48edd`.
Retained rows SHA-256:
`b86fe1026753b159ae95f1fe250ef05fcba5fa0760581193e2e3ec104f590fd1`.
