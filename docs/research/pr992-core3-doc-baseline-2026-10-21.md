# PR #992 Core 3 baseline documentation check

The ARM64 job for head `3f6212934d1f3d6c4ee2527b00df3e5bfdbf3d34` passes
`cli/runtime/commands/run` (including the repaired signal-log test) and the
Wago runtime tests. It fails only `TestRelease3DocumentationMatchesCommittedBaseline`.
Job: https://github.com/wago-org/wago/actions/runs/39660906917/job/119446788561

CI tests merge `0d028a53aa35e31cfc8a317e90abc825226d9f26`, which combines
that head with main `f91caadd3ccdc3bd0ab431412088d34d1f9e90a2`. Main's feature
page rewrite removed the Core 3 baseline statement. The PR head alone still
had it, which explains why its local check passed.

Keep the new phase-based feature page and restore the existing baseline claim:
258 files, 2,226 passed modules, and 58,038 passed assertions, with zero failures,
skips, or gaps. Link the committed baseline report for the exact revision and
tools, and keep `SupportedFeatures()` as the authority for the active build.
No runtime code, test expectation, or baseline result changes.

The existing check fails on the exact CI merge before the repair (0.183 s).
After the repair the full spectest package passes on that merge (0.172 s) and
the PR head (0.215 s). The documentation check validates 85 Markdown files on
the merge (0.135 s). These times include Go command startup/build work. Raw
failure/validation results and the original job log are stored in
`docs/research/data/pr992-core3-doc-baseline-2026-10-21/`.

The PR-head documentation check also passes (84 files, 0.146 s). The Linux
ARM64 test binary builds (0.220 s), and both baseline documentation tests pass
under QEMU (0.089 s) on the repaired CI merge. This is emulated ARM64 evidence;
the next hosted ARM64 job must still confirm the result.

Daybreak Blue reviewed the six-line change and saved evidence. It found no
issue. The new phase layout, test expectations, and baseline stay intact.
