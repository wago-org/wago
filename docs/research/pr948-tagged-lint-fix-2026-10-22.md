# PR 948: repair tagged stats fixture calls after main merge

The [lint job](https://github.com/wago-org/wago/actions/runs/39732958160/job/119672128373)
fails at head `72e684df38cef599be936dbed3229a5a5b85999e` because four fixture
calls use the old `newGCFrameRootPlan` signature. Main separated exact-root
admission from generic-GC execution. Ordinary/register-check builds exclude
these files; the diagnostics lint profile includes them.

Update two calls in `gc_numeric_leaf_inline_stats_amd64_test.go` and two in
`gc_trapping_guard_stats_amd64_test.go` to request exact roots and generic GC
execution explicitly. Keep their counter, hidden-root, callsite, trap, import,
source-mask, and optimization selection assertions. This is a tagged fixture
API update. It changes no runtime lowering or lint baseline.

The focused combined stats/register-check run reproduces all four compile
errors before the edit, then passes after the edit (25.450 s including build,
peak RSS 3,664,760 KiB; package tests 0.009 s). The failing build is retained.
Staticcheck 2026.2.1 is installed and its Go 1.27.1/bootstrap/binary provenance
is verified with the repository bootstrap script. The complete `just lint`
recipe passes: generated-file sync, both vet profiles, all four Staticcheck
profiles, and all four script test commands. The gate takes 172.806 s
and peaks at 3,649,928 KiB RSS. Existing reviewed findings stay visible;
there are zero new findings. This long validation activity exceeds the
30-second target and remains measured. No compiler speed claim is made.

The original full ordinary/register-check and checked benchmark gates for the
main merge remain recorded in `pr948-main-sync-2026-10-22.md`. This follow-up
checks the newly exposed tagged fixture path. No test is removed, timeout
raised, or baseline extended to hide the compile errors. Native CI for the
pushed follow-up is still required.
