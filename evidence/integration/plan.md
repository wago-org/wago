Continuation baseline: verified current main 05af869ebcf25feb723249ac64c7a1de87d28fd0,
merged privately as 8dfea5ee. Existing #815 hypothesis/negative criteria still apply.
No production compiler/runtime edits, publication or shared checkout mutations.

Additional acceptance: practical just capture/compare/test commands work; existing
Fibonacci's mapped scalar region is supported, without qualifying its unmapped
wrapper/calls; independent operand facts and conditional-branch conditions survive
matching-target normalization; malformed metadata/source ranges and unknown effects
never qualify. False complete results mean revise/reject. Native ARM64 unavailable.

Before measurement: matched trimpath/buildvcs=false baseline/candidate runtime and
AMD64 benchmark builds at current main; compare binary hashes. Five alternating
short A/B samples pinned to one CPU, GOMAXPROCS=1, 20ms/compile workload, plus three
A/A small-scalar controls and fixed execution iterations. Finish builds before
timing. Report medians/ranges, code bytes, allocations and noise honestly. Tool
costs measured separately; no RSS/retained heap claim without measurement. Interpret
the prior >60% tiny compile signal against binary identity and A/A noise.
