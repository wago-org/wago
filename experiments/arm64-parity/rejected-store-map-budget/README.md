# Store-map compiler-budget revisit (rejected)

Apple M4 Max ARM64, isolated c74d21c1 source, default-off experiment. No workload identifiers or manifest arguments enter code recognition.

The archived prototype was restored only in an isolated worktree. Its exact native tests pass. Seven-case reversed, same-thread/QoS compile/execute comparisons reproduce strong execution gains (resize -33.1%, max pooling -31.2%, components -17.5%) but weak gains (~1%) in autocorrelation, bootstrap and sort-merge join. Six paired rounds, 75 ms per state; raw observations retained.

Concrete analysis-cost reductions replace 32-byte symbolic nodes with 8-byte nodes; write scanner/proof/vector/guard plans into caller-owned storage; use bounded uint16 reference counts and uint32 branch positions; avoid redundant read-only reduction scans after proving a store loop. Native test suites continue to pass. All fourteen captured off/on native images are byte-identical to the original restored prototype.

Compact-plan six-pair 100 ms comparison reduces compile overhead from +11.35% to +7.87% autocorrelation, +11.49% to +7.32% sort-merge, +20.74% to +15.60% inference, +13.35% to +7.66% max pooling and +13.76% to +8.35% components. Further guard-output pointers and mutually exclusive scanner dispatch do not materially improve the remaining cost. Weak execution gains still do not justify +7–16% compilation, so production restoration is rejected. This is focused evidence, not full corpus qualification or parity completion.

A separate policy-query copy mitigation removes actual Go instructions but eight reversed cross-binary compilation pairs across twelve cases are broadly flat; it is not recommended for retention on that evidence. Public value methods were preserved in the later trial by adding a pointer query used only by private fn.opt. These files record unretained experiments; source archive and integration patch should not be applied wholesale.
