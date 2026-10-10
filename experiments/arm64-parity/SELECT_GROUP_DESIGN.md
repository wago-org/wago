# Proposed general select-group guard

Status: experimental i32 prototype, disabled by default. Preliminary and independent measurements are recorded in NOTES.md; retention gates remain pending.

The rejected per-select lazy branch saved work for sparse conditions but slowed
independent high-entropy input by 52%. A different cover can retain branchless
inner selects while guarding the entire group with one predictable zero test.

Recognize a bounded sequence of pure integer expressions whose selects preserve
the same accumulator on the false edge and update that accumulator on the true
edge. All conditions test constant masks of one unchanged integer local. The
union of those masks is sufficient: if it is zero, every update preserves the
accumulator and all pure arithmetic in the group can be skipped.

No artifact identifiers, specific masks, hash constants or algorithms qualify a
group. Admission derives from instruction cost, number of updates, union-mask
bit count and state reconciliation requirements. Prefer enough distinct tested
bits to keep the group guard predictable on independent random masks. Individual
selects remain eager inside the nonzero path.

## Compiler invariants

- Parse a bounded source window once, using a private reader copy and a fixed-size
  abstract operand stack. Never scan the remainder of the function repeatedly.
- Start at a realized local.tee accumulator. The group consumes that initial
  operand and finishes with local.set of the same accumulator.
- Only accumulator assignments after select are admitted. Reject calls, loads,
  division, references, stores, control flow and any writes to the mask local.
- Both false operands and select-local destinations identify the same accumulator.
- Initially require a pinned integer accumulator and mask, no dynamic regional
  leases, and no call-making function. This supplies explicit homes at both paths.
- Flush the surviving operand prefix before emitting the guard. Both paths must
  agree on stack height, canonical homes and borrowed register lifetimes.
- Keep the incoming accumulator register on the skip path. Clear assignment-version
  facts at the join; preserve only width invariants that hold on both paths.
- Never claim canonical upper bits for a skipped dirty i32 parameter. Declared
  i32 locals already have a canonical representation invariant.
- Use the normal bytecode lowering loop for the nonzero path. Close the native
  guard exactly at the recorded source end, including select/local-set peepholes
  that consume the final instruction ahead of the loop.
- Keep the prototype disabled until correctness, source-lifetime pressure and
  high-entropy regression tests pass.

## Independent checks before retention

Use separately generated groups with different arithmetic, masks, widths,
accumulator aliases and surviving operand prefixes. Mathematical oracles must
cover zero masks, individual bits, overlapping masks, all bits, high bits,
overflow and changing input values. Rejection tests include trapping loads,
division, modified masks and different false-edge locals.

Benchmark sparse, all-zero, all-nonzero, independent random and alternating masks.
Compare execution, compilation and native size against the current eager selector.
A gain on one sparse application is insufficient. Mitigate admission or guard
cost if high-entropy workloads regress before discarding the proposal.

Run each batch through ~/benchmark-lock.py --wait and release at case boundaries.
