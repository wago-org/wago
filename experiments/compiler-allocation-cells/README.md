# Transient allocation cells

Based on selective-layout checkpoint 26026e06. Exact candidate sampled allocator
callers motivate three bounded source changes, not a speedup claim:

* scanLoopCallFreeDetails: inline the captured finish closure at the single
  terminal path; preserve all budget mutation, candidate ordering/ties and
  floatSafe behavior. The candidate array is still bounded to 16 entries.
* minimumMemoryAccess: recursion threads the remaining budget by value, left
  child before right, preserving consumption even on unknown expressions.
* Asm.LoadIdx: keep scalar opcode bytes rather than dynamically selected slice
  literals; preserve exact REX/opcode/SIB/displacement order.

Old implementations survive only in test files as differential oracles.
Targeted tests compare malformed/scalar/control/float inputs, candidate saturation,
step cutoff, budget edge cases, expression widths/depth and remaining budget.
589,824 LoadIdx combinations compare exact old/new bytes (registers, displacement
boundaries, widths and signedness). No production function tests were weakened.

Ordinary targeted Go packages passed; diagnostic-tag targeted packages passed,
including existing minimum-bounds selection tests. Logs retained beside this
note. No full CI or TinyGo adapter build was run in this step, per the requested
fast batched workflow. A TinyGo ELF check must prove that the allocation calls
are actually eliminated; source intent and Go tests alone do not prove it.
Then compare representative exact native images/oracles and matched cold timing.
No buffer ownership, shared cache, collector or runtime settings changed.
