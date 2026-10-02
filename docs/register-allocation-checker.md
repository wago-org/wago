# Debug register-allocation transfer checker

Build with `-tags=wago_regalloccheck` to check Railshot allocation transfers while
compiling Wasm. There is no runtime setting, environment switch, new production
IR, or external checker/solver dependency. A checker failure follows the normal
compiler error path and begins with `regalloccheck:`.

`just test unit` runs the ordinary root/CLI suites and the checked suites.
The other `just test` correctness recipes (including corpus, spec, guard-page,
fuzz and TinyGo), regression stress, verification and test/coverage cards enable
the checker. Build/release and performance-benchmark recipes stay ordinary.
Direct commands do **not** acquire a tag automatically:

```sh
go test ./...                         # ordinary build contract
go test -tags=wago_regalloccheck ./... # checked compiler
go test -tags=wago_regalloccheck,wago_guardpage ./src/wago
```

## What this first version checks

- Canonical operand-stack flushes, including staged flushes, pressure spills,
  reloads and both halves of v128 spills
- The slot copies used to place control-edge values, including overlapping
  source/destination ranges
- Resolved ABI/entry parallel-register shuffles, including swaps, cycles and
  ARM64 swap-chain rewrites
- Immutable integer, scalar-float and vector cache admissions versus later
  physical calls, including helper calls

The machine-state model names physical register bytes and frame bytes. GP and
FP banks are separate. Each input/definition receives an opaque symbolic value;
loads, stores and moves copy that value, and clobbers destroy it. Expectations
are retained independently of `regUser`, `fregUser` and rewritten `storage`.
A reload never introduces a fresh identity to match its claimed owner. Partial
writes cannot preserve an entire vector by accident. Scalar FP memory loads
clear the old upper-lane identities on both targets; ARM64 scalar register moves
and GP-to-FP moves do too. AMD64 legacy scalar register moves retain their
architecturally preserved upper lanes. Unknown bytes do not prove
ownership; unspecified high carrier bytes of i32/f32 are ignored only when
checking a raw eight-byte slot copy.

The encoder reports actual transfer operands, including the frame displacement,
inside each checked canonicalization window. Semantic arithmetic definitions are
trusted after checking the concrete input uses. Target-hint reloads validate
their actual destination before arithmetic; folded AMD64 ALU/multiply/compare
reads validate the actual encoder frame address. A missing folded read cannot
be hidden by assigning the result's identity. An ABI shuffle is checked against
its original parallel assignment and
its final encoder-reported transfers, including GP/FP bank and scratch-frame
addresses, rather than its mutable resolution graph or requested callbacks.
Immutable cache identities start at the actual preload and are never reseeded at
a physical call. They have no spill/reload protocol, so a call that destroys one
is an error even if a call-presence hint says otherwise.

## Explicit boundaries and gaps

This is a transfer/preservation checker, not a whole-function correctness proof.
A canonicalization window assumes its input locations contain the input values.
It observes every covered transfer until the complete canonical image is checked.
A partial flush also checks its still-live condition/argument suffix before the
window closes; a staged slot copy uses the **same** state. Standalone control-edge copies and
ABI shuffles have their own input contracts. They do not establish correctness of
arbitrary instructions between windows or prove that different incoming CFG
paths compute the same value. The shared state model supplies intersection for
join facts, but the backend currently verifies edge placement, not a whole-CFG
fixed point.

Arithmetic/ISA semantics, arbitrary non-transfer scratch clobbers, deferred
memory/trap ordering, full pinned-local lifetime analysis and unmodeled lowering
outside these windows remain covered by existing execution/regression tests.
Encoder byte selection is trusted. Incorrect incoming metadata may therefore be
outside this first checker's coverage. Expanding coverage requires an explicit
semantic input/use/definition contract; do not seed a new symbol after a failed
reload, infer correctness from matching allocator bookkeeping, or silently claim
an unsupported instruction was checked.

## Ordinary-build contract and qualification

The tag selects the implementation and state. Ordinary compiler and encoder
structs contain only leading zero-sized compatibility placeholders. Every hook
has a compile-time false guard. Standard-Go tests compare actual sizes,
alignments and every
non-checker field offset against layouts with the placeholders removed. Those
reflection-based tests are excluded under TinyGo, which does not implement
reflect.StructOf; the ordinary production placeholders remain zero-sized.
`scripts/check-diagnostic-dce.sh` also rejects retained checker implementation
symbols in ordinary manager, runtime, minimal-runtime and embedding binaries.

Before qualifying a change, run both builds, compare generated guest-code
fingerprints, inspect ordinary hot-path disassembly and compare matched compile
benchmarks against the same unmodified base, including allocation counts. Tagged
compiler overhead is expected and is not a production performance result.

Negative tests corrupt physical effects while leaving allocator ownership
plausible: reload the wrong slot, overwrite a later operand's spill during
canonicalization, partially overwrite a vector, clobber a preloaded constant with
a call, copy overlapping slots in the wrong direction, consume corrupted deferred
inputs, emit no-op/wrong-bank ABI shuffles, and reload the wrong FP swap slot.
These must fail
without relying on guest execution. Keep execution regressions as an independent
oracle.

## Methodology

The [regalloc2 checker](https://docs.rs/regalloc2/latest/regalloc2/checker/index.html)
tracks symbolic values through allocation edits and checks uses against them;
its full analysis intersects facts at CFG joins. Wago applies that independence
to smaller existing transfer seams rather than adding an allocation IR or
claiming regalloc2's whole-function coverage.

[Arrival](https://cfallin.org/pubs/oopsla2025_arrival.pdf), section 4, shows why
verification must include state effects, not just output arithmetic. That
motivates explicit frame writes and call clobbers here. Arrival's authoritative
ISA/SMT instruction-selection proofs are separate research; this checker neither
implements nor depends on them.
