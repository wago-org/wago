# Defined test and cast cold pin restoration

Astra's source audit found that the generic defined test/cast leaf saves actual
R9–R11 value pins but branches directly into the enclosing trap tail on physical
metadata failures. That tail writes dirty global pins, so scratch values can
replace globals. A dedicated red test covers test/cast normal and codec artifacts,
all three actual global pins, successful/mismatching/null outcomes and corrupt
handle count/space/extent. Corrupt fixture metadata is restored before teardown.
The new type-test/field fusion must not copy this cold exit pattern.

The initial red run took27.522s and demonstrates dirty-global corruption on
physical failures and wrong/null casts in both artifacts. An initial separate
HandleCount injection was ineffective because invocation refreshes that view;
its expected trap failure is retained in red.json. The final regression uses
stable mutated handle space/extent plus normal wrong/null/valid outcomes; this
changes no production range check. All malformed fixture state is restored.

Normal focused safety/descriptor/guard tests pass in26.473s; stats+register checker
passes in28.852s. A mechanical first build failure (castFailure argument arity)
is retained; only that signature call error was corrected. The successful leaf
instruction sequence is retained; only trap destinations and cold restore tails
change. No root, frame, compact reference or native ABI layout changes.

On the older PR1006 branch, a module without allocation used boundary collection
before the corrupted handle reached native code; the first public test panicked
in that older collector path. The final fixture has an unused but observable
allocator and asserts Required&&Exact, preserving enabled native collection.
Both public normal21.675s and checked24.954s pass. The identical final fixture
against parent production still fails on dirty globals in24.228s, so the root
requirement did not hide the regression. All failures remain in the data.

GPT-6 Sol with explicit daybreak_blue independently reviewed the source and
fixture follow-up and found no defects. Remaining gaps are pinned locals,
i31/foreign inputs, subtype-interval failures and other physical corruptions.
Production changes only cold branch routing/restores; no extra hot instructions,
checks removed, root/frame changes or performance claim.

PR1006 was already merged into PR948. This is a separate ready follow-up above
PR948 head b33520617; the only source conflict retained the checked indexed
space selection and routed its existing condA guard through the restore tail.
On that current parent, normal23.983s and checked24.528s pass, including the
installed guard regression suite. Parent/decode behavior is preserved.
