# Independent fixture repair review

Reviewed commit `4913c91e1ad5636543d0ea7eed740b16de1b9bf2` against runtime source pin `7e41fafbbcd737ed1edce11b94b43b4eda134dcf`, the failed native audit in `native-tinygo-run-41453552853`, and the canonical harness at the reviewed commit. Scope was test correctness, negative discrimination, cleanup lifetime, census requirements, adapter identity, API/catalog provenance, and the available final cross-build receipts. Production and workflow files were not changed.

## Findings

No review finding was identified in the bounded scope.

The canonical resume test retains the direct `nativeResumeStackTop() == preparedInt.stackTop` check. It then saves `e.stackTop`, assigns `preparedInt.stackTop ^ uintptr(16)`, repeats the physical-top check, and restores the saved entry value before the deferred `Close`. XOR with nonzero 16 is guaranteed to differ for `uintptr`, so an implementation that incorrectly returns `e.stackTop` fails the second check without assuming any relationship between independently allocated mappings. The canonical file and the harness candidate have SHA-256 `583b4d60df164b0849d7663469bfae38e5edaf7948fbe9688836c3d539ee3ac5`.

The cancellation fixture has required `cancel` and `close` subtests in every mode's suite plan. Both establish the import-entry handshake and perform eight GCs before interruption. Cancel checks `errors.Is(err, context.Canceled)`; close retains `requireInterruptedTrap`. Both retain post-unwind GC, checked normal `Close`, and final GC. Deferred `cancel` and `Close` cover assertion, timeout-panic, and early-failure cleanup. The handshake proves import entry only, matching the repaired evidence's bounded claim.

At source pin `7e41`, `InvokeContext` documents that cancellation returns `ctx.Err()` and implements that conversion. The same pin's optimization catalog registers `dead-gc-new` with `amd64(...)`, not ARM64. The harness asserts exactly two occurrences before removing only those two option calls from its ARM64 stage8 copy; the remaining source body and its relocation, generation, result-42, trap, and configuration controls are not weakened. The source AMD64 fixture is not modified.

The harness includes both required cancellation children in ordinary, `wagodebug`, and `wago_regalloccheck`; `census-controls.json` records missing-child rejection for all six mode/child combinations. Workflow bounds, including 333 job-minutes, are unchanged by the reviewed commit.

## Linux TinyGo execution boundary

On Linux x86-64, TinyGo 0.43.0 (Go 1.27.1, LLVM 22.1.4), the targeted canonical test was attempted in ordinary, `wagodebug`, and `wago_regalloccheck` modes:

`tinygo test [-tags=<mode>] -run '^TestTinyGoNativeResumeUsesPhysicalStackTop$' -count=1 ./src/core/runtime`

All three attempts failed at link, before test execution, because the installed unpatched compiler could not resolve `runtime.wagoRegisterForeignStack` and `runtime.wagoUnregisterForeignStack`. The retained patched tool under the Darwin supplement is Mach-O, not an executable Linux compiler. Consequently the canonical test and an immutable-overlay wrong-field negative control were **NOT RUN**. This is an exact Linux toolchain limitation and supplies no Darwin evidence.

## Cross-build and verdict

The parent-owned nine-way Darwin Mach-O cross-build driver was still running when inspected. Its owned `validation.log` was empty and therefore no final cross-build result is credited or modified here.

Activation verdict: **NO**. The fixture repair is statically acceptable and has no bounded review finding, but activation still requires (1) the parent driver's final nine-way Darwin Mach-O cross-build PASS and (2) a patched native Darwin TinyGo all-three-mode execution proving the canonical resume positive and wrong-field negative discrimination, required cancel/close children and cleanup, and stage8 ARM64 guest/control execution. This Linux review is not Darwin proof.
