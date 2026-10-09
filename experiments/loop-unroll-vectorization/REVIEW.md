# Independent review record

Two independent GPT reviewers inspected the implementation and evidence. The
correctness reviewer checked memory effects, trap positions, address and counter
wrap, live locals, floating point rules, and code ownership. The measurement
reviewer checked inputs, path controls, timing selection, sample order, and result
checks. Earlier read-only audits checked the existing recognizers and loop rules.

The reviews found these issues. Each actionable issue was corrected.

- Duplicated source operations lost precise trap positions. The lowering now
  retains an original-position tape. A pointer-load regression failed before the
  repair and passed after it. A second regression checks an unchanged SIMD suffix.
- Writing tests lacked full memory32 wrap cases and live exit values. Tests now
  check full 4 GiB memory, final-boundary accesses, writes before traps, partial
  overlap, and exit locals.
- The floating point oracle initially required one exact NaN encoding. It now
  applies Wasm's permitted canonical and arithmetic NaN results. Non-NaN results
  and unaffected memory remain exact.
- Source-byte limits did not bound native bytes. Physical function spans now
  have an 8 KiB limit; the complete native image has a 256 KiB limit. Total bytes
  conservatively bound added bytes. Oversized attempts restore source compilation.
- Parallel compilation can return a nil code-image owner. Fallback cleanup now
  checks ownership. Module fallback tests use one and two workers.
- Omitted function entries can alias an internal entry. Function limits now use
  the emitter's physical span before module layout, instead of entry intervals.
- The first oversized-function test used load/drop instructions which the
  compiler removed. The replacement uses visible host calls after the loop.
  It checks that recognition succeeds and that native fallback restores the
  original code. These calls are outside the body that the plan replays.
- Raw `f32` argument values 3 and 7 represented subnormal floats. The timing
  inputs now use the bit encodings of normal values 3.0 and 7.0.
- Short timing cases and inputs larger than L3 were missing. Both were added.
- A matched factor-one grouped emitter was needed to separate emitter setup
  from grouping. Variant H supplies this control.
- Hierarchical benchmark filters initially omitted either execution or compile
  timings. Separate filters now select both. Smoke outputs verify their selection.
- Pilot and final samples could mix. Final runs require a fresh directory.
- Inherited compiler environment settings could alter defaults. The scripts
  clear inherited `WAGO_*` controls before each comparison set.
- Conservative CPU features made the integer-map negative result incomplete.
  Matched modern-feature scalar/vector timings now cover both maps. They retain
  128-bit width. Full focused correctness checks also pass with modern features.
- Large-counter tests now exercise widened range overflow with boundary traps
  after at most two iterations. Unaligned sum tests compare all exit locals.
- Final evidence review found isolated small dependent-loop gains and occasional
  streaming allocated-byte samples. The report retains them and qualifies the
  zero-allocation and sustained-performance statements.
- The final modern comparison passed independent numeric, break-even, and
  assembly checks. The reviewer found nonzero 262144-element f32 allocated-byte
  medians; the report limits its zero-byte statement to count 8192.

The final compiler repair received no remaining correctness finding. Focused
checked tests, physical-size boundary tests, and source-fallback tests pass.
The final benchmark design received no further finding after environment cleanup.
Native ARM64 execution remains unavailable.

An earlier Claude Code attempt failed with `Not logged in`. Its output is saved
in `results/AB-claude-review.txt`. **No Claude review occurred.**
