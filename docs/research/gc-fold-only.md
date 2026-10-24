# Branchless selected-space descriptor experiment

Baseline: `97f07070f` (fixed Old preload plus folded non-Old selection).
This separate branch removes the Old preloads, compare, and selection branches
from the same five resolver bodies. Every space instead uses the same checked
space byte, `SHL 4`, and two indexed loads. The selected descriptor is exact;
Large does not alias Old. All physical and type guards remain. No metadata,
register, frame, root-map, allocation, or trap-tail changes are made.

The experiment isolates cheaper descriptor address arithmetic from the Old-only
load-dependency shortcut. Uniform-space rings cannot measure branch prediction
cost for mixed generations. New alternating and fixed xorshift32 random
Old/Nursery rings use the same 4096 objects and dependent field-read loop. All
objects start in Nursery; the chosen subset is promoted after allocation. Every
actual space byte is then asserted. The timed loop allocates nothing. Both
patterns must finish a full ring and multiple ring traversals with exact output.

Keep the same mixed fixtures in all compared binaries. Compare the original
indexed resolver, Old-specialized folded resolver, and branchless folded resolver
on the four uniform spaces plus the two mixed patterns. Do not infer a full
compiler gain from these microbenchmarks. Validation: a Go overlay of `97f07070f` fails the new structural checks as
expected (4.64 s). Focused normal and checked (`wago_regalloccheck,
wago_codegenstats`) suites pass (16.03 s each). They include exact selected
descriptor faults, invalid handles, collector-space/type checks, collection/codec
roots, and both mixed fixtures. Matching original (`2c53a7838`), refined Old
(`97f07070f`), and fold-only benchmark binaries build in 13.66/13.64/0.75 s.
All three also pass the same descriptor and ring fixtures. No full compiler result exists for fold-only.


## Matched three-way microbenchmark

Three ABCCBA blocks used A=original `2c53a7838`, B=refined Old `97f07070f`,
C=fold-only `f7ebbe973`. All used exactly the same final fixture file, with only
`gc_direct.go` changed by Go overlay. CPU 15, GOMAXPROCS=1, 200 ms per case;
108 samples total, six per variant/case. Root confirmed no competing Go or
compiler jobs. All samples, including early original random 9.109 ns and Old
Tiny 4.972 ns outliers, are retained in `gc-fold-only-results`.

| Pattern | Original ns/op | Refined Old ns/op | Fold-only ns/op | Fold vs original |
| --- | ---: | ---: | ---: | ---: |
| Old | 4.8220 | 3.5285 | 4.4225 | -8.28% |
| Nursery | 4.8170 | 4.4470 | 4.4225 | -8.19% |
| Large | 4.8115 | 4.4220 | 4.4250 | -8.03% |
| Tiny | 4.8215 | 4.4290 | 4.4240 | -8.24% |
| Alternating | 4.8395 | 4.4650 | 4.4395 | -8.27% |
| Random | 4.8275 | 3.9920 | 4.4325 | -8.18% |

The branchless arithmetic change gives a consistent roughly 8% gain in this
physical dependent-chain reducer. The refined Old branch remains faster for
uniform Old and the fixed random mix. This experiment therefore does **not**
prove a real-workload misprediction penalty from the Old branch. The repeating
4096-node pattern may be learnable by the predictor; hardware branch counters
were not collected. The alternating case is also highly predictable. These are
controlled mixed-generation coverage, not a general branch-entropy proof.

The next comparison is the real compiler, on the current fusion baseline with
only this folded indexing change. Do not infer its gain from the reducer or
claim it is faster than the Old experiment in general. All physical/type guards
and selected-descriptor corruption regressions remain intact.
