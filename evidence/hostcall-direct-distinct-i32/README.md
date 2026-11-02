# Distinct i32 native host leaves

Owner: t3-thread://v1/mcp:8faf6900-ac97-419e-b761-141f062b89c2

Base: `90f2b57dafa7f8c3cc7e3aca38b91148f15e1d1e`.
Branch: `candidate/wago-hostcall-direct-distinct-i32`.
Authority: campaign census/admission in `/home/dev/Projects/wago-hostcall-record/docs/hostcall/`; Slipstream `bd640ddf0:docs/host-call-boundary-redesign-2026-11-01.md`, safety contract v2.

Status: **READY TO MEASURE — Linux amd64 native candidate, with an explicit baseline qualification exception. The unmodified ordinary unit gate remains red; this is not an all-green unit-recipe claim.** No performance measurements, benchmarks, CI dispatches, PRs, children, or excluded GC campaign work.

## Implementation

The amd64/arm64 registry owns an immutable collection of individually acquired `(PC, exact Go type, parameter count)` cache references for distinct imports, separately from its optional legacy trampoline leaf. Existing machine-code decoders and generated wrappers are unchanged. Every distinct import needs a usable direct entry. Failure rejects/releases the complete provisional collection. Equal code identities can share mappings, including aliases, without merging import/owner identity.

Wago checks every resolved binding's index, exact i32 unary/binary signature, gate and instance GC domain; validates self-owned dispatch cells; snapshots dispatch/control/leaf flag; binds indexed entries; then registers the native host owner. Any publication/owner failure restores targets/control/flags before code release. Distinct registrations retain the generic frame trampoline and checked shared/reference/table/reexport entries. Native flags disable bounded Go host segments. Single-import isolated admission is unchanged. Existing single/same-PC registration retains its legacy trampoline, including arm64's staged-only possibility.

No public host-function API or accepted callback shape changes. New core installation functions require the existing internal runtime bridge capability. No decoder or compiler expansion. arm64 register entry remains direct wrapper +192. RV64's separate registry embeds the control address and is unchanged; new distinct registration/binding stubs deliberately decline. RV64's existing single/same-PC path remains available.

## Regression coverage

- Native-enabled versus `native-leaf-host=false`, checking each instance's leaf flag and every dispatch cell tag, distinct unary/binary signatures, two distinct binary bodies, aliases, import permutations and wraparound arithmetic.
- Raw poisoned u64 argument/result homes, adjacent sentinels and argument/result overlap, entering the acquired native mapping directly with the wrapper ABI.
- Local/global/context preservation; direct execution after table/reference calls and traps; table, call_ref, tail, reexport and foreign instance paths; result high-word normalization.
- Captured and effectful callbacks, callback trap/recovery/effect counts, plugin gate/revocation, local GC and imported/dynamic/store collector flags, profiling fallback.
- One-P scheduling, concurrent Go collection and cancellation through the existing native scheduling harness.
- Indexed cache identity, per-binding alias reference counts, cross-instance sharing, partial/final close, ordinary concurrent cache registrations, provisional rejection/error/missing-direct at every acquisition, every cell publication, and native owner publication failure followed by retry.

## Evidence format

`qualified-gates.json` is the complete package/mode gate manifest, with exact commands, execution head, exit codes and explicit environment. Matching `qualified-*.log` files retain output. `cross.json` records compile-only targets and exact commands; its `executed` fields are false. `baseline.json` records exact-base probes. `gates.json` and `harness-gates.json` preserve earlier attempts with incomplete host tooling; they are not passing qualification claims.

Go build temporaries are candidate-owned and removed after each command. Cross-compiled binaries are removed after each build. Shared Go caches and other checkouts/artifacts are untouched. Conformance submodules in this assigned worktree were checked out at the exact base's pins: spec-v1 `a8bcbafe6d2fb191ce0188de0e18fdc107fa2598`, spec-v2 `05ca4182176763112561ae20153975c12bd689e4`, spec-v3 `9d36019973201a19f9c9ebb0f10828b2fe2374aa`.

## Host tooling and baseline probes

The unmodified exact base was exported with `git archive` into temporary `.candidate-baseline` inside this assigned checkout, then removed. Its pinned spec-v3 was symlinked from this worktree for read-only fixture probes. No parent/current checkout or excluded campaign was used.

1. Host PowerShell 7.6.8 aborts without ICU. Both root PowerShell bootstrap tests reproduce the failure at the exact base. `DOTNET_SYSTEM_GLOBALIZATION_INVARIANT=1` passes those same tests; bootstrap source is unchanged.
2. Host WABT 1.0.42 fails the repository's explicit 1.0.41 requirement, also reproduced at the exact base. Candidate-owned WABT 1.0.41 matches the repository bootstrap archive checksum (recorded in `tooling.json`).
3. WABT 1.0.41 cannot parse all pinned GC text. The exact base reproduces missing `WAGO_SPEC_INTERPRETER`. Building the exact pinned Release 3 interpreter and setting its path/revision makes the same baseline GC array bulk accounting test pass. The OCaml/Dune/Menhir packages and hashes, source revision and interpreter hash are in `tooling.json`; source/build/download artifacts were removed immediately after the build.
4. Initial temporary-tree probes hit mise trust and inherited GOROOT mismatches before tests ran. Canonical baseline probes explicitly select Go 1.27.1 and matching GOROOT; no shared mise configuration was changed.

Only Linux amd64 / Go 1.27.1 execution is claimed. Other supported native cells are cross-compiled, not executed. No arm64/RV64 emulator or foreign host execution is claimed. TinyGo 0.43.0 standalone execution was exercised by the package recipe; the distinct class remains disabled there. Its existing cancellation abort is reproduced at the base, as recorded below. The new RV64 class remains generic.

5. `TestPluginRuntimeBinaryResolvesGlobalBuild` intermittently fails with `text file busy` in the unmodified exact-base tree, reproduced with `go test -tags=wago_regalloccheck,wago_runtime -count=10 -v -run '^TestPluginRuntimeBinaryResolvesGlobalBuild$' ./cli/manager/internal/plugin`. See `baseline-plugin-install.log` and `baseline.json`. No plugin installer source was changed.
6. One candidate ordinary-root attempt aborted in `TestTinyGoSupportsCooperativeInterruption`. Seven recorded isolated exact-base test invocations passed, so the unchanged baseline test's generated executable was copied through its own child `/proc/.../exe` while executing, then rerun as a correctness-only cancellation probe. Attempt 619 reproduced the same SIGABRT/nil-pointer panic. `baseline-tinygo-stress.json` pins the exact source revision, binary SHA256 and attempt count; `baseline-tinygo-stress.log` preserves the failure. `reproduce-tinygo.py` reproduces without modifying baseline sources. No timings or benchmark results were collected. The executable, downloaded OCaml packages, interpreter build tree and exact-base exports were removed. This no-import standalone fixture never admits native host leaves.

## Gate matrix and head attribution

Runtime source head: `0ab4994f229b995bdce5cc89e557cb8c40c0cea4` (source tree `bdb7f54858ea6a2b29ced4134410a9dcab8f27d3`). Later commits contain evidence only. The implementation is based directly on the required base; no campaign/parent changes were imported.

| Gate | Result | Evidence |
| --- | --- | --- |
| Focused native leaves, forced-generic oracle and actual direct staging-bypass assertions | PASS, ordinary/debug/checked/race; profiling correctly generic | `route-gates.json`, `route-*.log` |
| `go build ./...`; `go build -tags wago_runtime ./cli/...` | PASS | `qualified-gates.json` |
| Ordinary full root, unmodified command | RED: confirmed exact-base TinyGo cancellation abort only; no native candidate failure | `qualified-ordinary.log`, `final-root-retry.log`, baseline stress evidence |
| Full checked root (`wago_regalloccheck`) | PASS | `qualified-checked.log` |
| Both ordinary and checked `wago_runtime` CLI recipe commands | PASS | `qualified-runtime*.log` |
| Checked installer module and checked bench module with exact unit-recipe exclusions | PASS | `qualified-installer-checked.log`, `qualified-bench-module-checked.log` |
| Full changed packages: ordinary, `wagodebug`, `wago_regalloccheck` | PASS on final source head | `final-gates.json` |
| Exact three-package race gate | PASS twice with complete fixtures | `final-race.log`, `qualified-race.log` |
| New distinct tests under `wago_profile` | PASS; intentional profile-only native assertions skipped | `route-profile.log`, `qualified-profile.log` |
| Runtime/Wago test compilation, six foreign OS/architecture cells | PASS (12 builds); execution NOT claimed | `cross.json` |

The standard ordinary root result remains red and is not described as an all-green unit recipe. Its confirmed baseline failure is kept separate from the admitted native class. The ordinary root command excluding only the confirmed baseline cancellation test is recorded separately in `ordinary-baseline-exception.json`; it does not replace or erase the unmodified-command result. `log-summary.json` enumerates explicit failures/skips and package counts; standard package logs do not enumerate individual internal skips.

The ordinary root command excluding only `TestTinyGoSupportsCooperativeInterruption` **passed**. The full checked root, both CLI runtime modes, checked installer/bench modules, full ordinary/debug/checked native packages, profile fallback, focused tests in all modes, both exact race runs, and all 12 cross-compilation builds passed. `qualification.json` is the machine-readable disposition. No measurements were run; no full-unit all-green status is claimed.

Temporary tooling paths in manifests are historical execution paths, now removed. Re-provision the recorded URL/checksum and pinned interpreter/package versions before replaying gate scripts. Only this candidate's temporary files were removed.

## Orchestrator safety review

An independent sol review found no implementation correctness issue, and confirmed the staging-marker differential assertion proves actual direct execution. It found that both replay scripts recorded failed command statuses but returned zero themselves. The orchestrator corrected both aggregate exit statuses. `test-gate-runners.py` executes the real runners with controlled subprocess outcomes: all-success and every command position failing normally or by signal (50 scenarios). Each failure remains in the complete manifest and makes the runner return nonzero. The contract check passed; `gate-runner-contract.log` retains its output. These evidence-tool changes do not change the qualified runtime source tree.

## Orchestrator complete ordinary-root run

The complete ordinary root suite, **without test exclusions**, passed with `go test -p=1 -count=1 ./...` and the same verified WABT/interpreter/environment as the successful pinned-baseline run. `orchestrator-full-ordinary.json` records the exact environment and `orchestrator-full-ordinary.log` retains output. Package serialization resolves the observed concurrent-package test instability without removing tests. The earlier concurrent-command TinyGo abort and its exact-base reproduction remain recorded; this does not rewrite those red results or assert an all-green unmodified unit recipe. Runtime source is unchanged from the qualified source head.
