# Distinct i32 native host leaves

Owner: t3-thread://v1/mcp:8faf6900-ac97-419e-b761-141f062b89c2

Base: `90f2b57dafa7f8c3cc7e3aca38b91148f15e1d1e`.
Branch: `candidate/wago-hostcall-direct-distinct-i32`.
Authority: campaign census/admission in `/home/dev/Projects/wago-hostcall-record/docs/hostcall/`; Slipstream `bd640ddf0:docs/host-call-boundary-redesign-2026-11-01.md`, safety contract v2.

Status: qualification in progress. No performance measurements, benchmarks, CI dispatches, PRs, children, or excluded GC campaign work.

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

`qualified-gates.json` is the final package/mode gate manifest, with exact commands, execution head, exit codes and explicit environment. Matching `qualified-*.log` files retain output. `cross.json` records compile-only targets and exact commands; its `executed` fields are false. `baseline.json` records exact-base probes. `gates.json` and `harness-gates.json` preserve earlier attempts with incomplete host tooling; they are not passing qualification claims.

Go build temporaries are candidate-owned and removed after each command. Cross-compiled binaries are removed after each build. Shared Go caches and other checkouts/artifacts are untouched. Conformance submodules in this assigned worktree were checked out at the exact base's pins: spec-v1 `a8bcbafe6d2fb191ce0188de0e18fdc107fa2598`, spec-v2 `05ca4182176763112561ae20153975c12bd689e4`, spec-v3 `9d36019973201a19f9c9ebb0f10828b2fe2374aa`.

## Host tooling and baseline probes

The unmodified exact base was exported with `git archive` into temporary `.candidate-baseline` inside this assigned checkout, then removed. Its pinned spec-v3 was symlinked from this worktree for read-only fixture probes. No parent/current checkout or excluded campaign was used.

1. Host PowerShell 7.6.8 aborts without ICU. Both root PowerShell bootstrap tests reproduce the failure at the exact base. `DOTNET_SYSTEM_GLOBALIZATION_INVARIANT=1` passes those same tests; bootstrap source is unchanged.
2. Host WABT 1.0.42 fails the repository's explicit 1.0.41 requirement, also reproduced at the exact base. Candidate-owned WABT 1.0.41 matches the repository bootstrap archive checksum (recorded in `tooling.json`).
3. WABT 1.0.41 cannot parse all pinned GC text. The exact base reproduces missing `WAGO_SPEC_INTERPRETER`. Building the exact pinned Release 3 interpreter and setting its path/revision makes the same baseline GC array bulk accounting test pass. The OCaml/Dune/Menhir packages and hashes, source revision and interpreter hash are in `tooling.json`; source/build/download artifacts were removed immediately after the build.
4. Initial temporary-tree probes hit mise trust and inherited GOROOT mismatches before tests ran. Canonical baseline probes explicitly select Go 1.27.1 and matching GOROOT; no shared mise configuration was changed.

Only Linux amd64 / Go 1.27.1 execution is claimed. Other supported native cells are cross-compiled, not executed. No arm64/RV64 emulator or foreign host execution is claimed. TinyGo execution is unavailable and unqualified. The new RV64 class remains generic.
