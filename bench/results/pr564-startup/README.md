# Optimized TinyGo startup check

Optimized TinyGo 0.41.1 CLI binaries intermittently failed during regexp setup,
including on `--version`, before Wasm execution. Failures also occurred in old
main and prior PR binaries. This was not established as a new PR564 regression.
Debug-only CI smoke runs had not exposed the release-settings failure.

The fix removes runtime-initialized regexes for canonical plugin paths, manifest
slugs, platform names, and GitHub usernames. Shared ASCII scanners preserve the
exact prior grammar. Callers retain their existing length bounds and errors;
inputs are never repaired or normalized. Standard-Go tests compare the scanners
with the old regexes on boundary cases, every byte inserted in selected inputs,
and 20,000 deterministic generated strings per grammar. Separate tests check
known cases under TinyGo and zero allocations under standard Go.

Before integrating main `731e95ff2`, the patched Minimal/Tiny and Standard/Tiny
binaries each passed 80 fresh-process checks: 20 each for `--version`, fib(20),
fib(30), and floating-point hypot(3,4). Both use Go 1.22.12 and pinned TinyGo
0.41.1 with `-scheduler=tasks -no-debug -opt=z -gc=conservative`. Binary hashes and
all output are in the smoke JSON files. These runs demonstrate that the observed
startup failure did not recur; they do not diagnose a general TinyGo compiler/GC
bug or certify every command. Final integrated CI is a separate gate.

CI now repeats startup and a real-module call with these optimized settings on
each supported native TinyGo CLI target. The existing debug tests remain.

`namecheck-bench.txt` records zero B/op and zero allocs/op. Its elapsed timings are
not a comparison claim: other compilation work ran concurrently. The full project
tests, targeted runtime grammar tests, differential tests, and TinyGo known-case
test passed. `tiny-smoke.json` and the debug transcript retain earlier failures;
the first debug transcript used an incorrectly relative WAGO_HOME, so it is only
diagnostic. The repeated old release failures use the recorded absolute paths.
