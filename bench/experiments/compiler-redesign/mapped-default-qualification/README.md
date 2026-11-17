# Default-stack mapped qualification

Actual TinyGo tasks/conservative/opt2 with no -stack-size; mapped opt-in tag,
WAGO_MAPPED_OPERAND_ARENA unset/enabled. Production unchanged from4dad64e3.
Built source2c8c00ad2519aaaaae225b3791687b4307e5905e.

Full AMD64 test package compiled; all named TestMappedOperandArena tests passed,
including forced-GC roots, heap overflow and panic cleanup.
Normal full public src/wago test-package compilation was killed exit137 before
any test output. No cgroup memory.events was available: OOM is NOT confirmed.
An isolated package retained byte-identical production files and the complete
original tinygo_lifecycle_test.go plus repaired tinygo_compiled_finalizer_test.go.
Only benchAddOneModule fixture was extracted unchanged. Source hashes and exact
excluded test-file list are included. This is not a full public suite pass.

All9 selected existing lifecycle tests passed, including forced collection,
queued close ownership,1000 compile-close cycles, instantiate and overlapping
runtime shutdown. Real production finalizer passed3modes×4rounds:
compile-only initially unmapped; explicit mapping unsealed4096bytes; activated
sealed4096bytes. Each reached closed=true/mem0/refs0 without explicitCompiled.Close
or replacing the finalizer.

Original fixture failures retained: compile-only wrongly assumed eager mapping;
then noinline returned helper retained stale owner pointers in dead stack frames.
TinyGo conservatively scans whole live task stacks. Matched256KiB control failed
the latter identically. Correct fixture creates/drops owner in a terminating task,
passes ONLY cache sidecar/error, and observes finalization from another task.
No stack scrubbing or production collector/finalizer changes.

Recorded cold build succeeded with default stack, binary SHA256
6c39f4d3eee81adcaa361e2bb3fe37bf4f167d375915aab38fa9c7e541484249.
Original prepared receipt retained; updated build receipt records exact source.
Smoke5 passed10/10 and full145 passed290/290 oracle/protocol records using the
same binary in both controller slots. Each performs first compile after prepare.
These are correctness gates, NOT acceptance timing/RSS or execution comparisons.
