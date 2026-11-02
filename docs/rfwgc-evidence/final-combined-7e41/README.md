# Immutable 7e41 combined qualification

Source: `7e41fafbbcd737ed1edce11b94b43b4eda134dcf`. Work is **INCOMPLETE**; current gate counts and exact pending names are in `final-summary.json` and `remaining-gates.json`. Final port approval belongs to the parent.

Scope is the original 217 combined gates plus three actual TinyGo unsupported-global follow-up gates, one per mode (220 total). `gate-plan.json` records finite executable batches. Only gate names/drivers were inherited. No d246, 9ad, RR implementation, native ARM64 or Darwin passes are imported.

The fresh source/cache is `/home/dev/.cache/wago-final-7e41-82jf5iko`. Archive SHA-256 is `9ca226384ac27874ae0ffd36030931f805b232e963dcb1475f5c87b84660a3f5`. All 4,018 tracked src blobs match the pin. Execution driver SHA-256 is `4054801551b214c363b706045ac039f2a435fc7cb1f04f7fab762c51d45f5070`. Original exports, exact gitlinks, WABT/spec interpreter, explicit Go GOROOT, Node, patched TinyGo and drivers have retained identity manifests. PATH contains explicit installations and system binaries, with no mise shims; GOENV is disabled.

Runtime repair census includes original RR3 and retirement families. An initial harness census mistakenly required ARM64-only TestSharedEntryGuardCancellationAndGC on AMD64. The command itself passed; that assertion failure and original driver remain preserved. `repairs-v3.py` fixes only the AMD64 census, verifies the saved ordinary receipt before reuse, and retains the guard in ARM64 requirements. No original test or source expectation changed.

Actual TinyGo follow-ups run unchanged TestUnsupportedConversionPublicGlobalRejectsOpaqueBeforeDirectory and TestUnsupportedConversionGlobalValidationOrdering with scheduler=threads and gc=conservative. Each mode requires both top-level passes and all eight ordering subcases, with no skip or helper early-return credit. No wago_target_tinygo tag adapter substitutes for actual TinyGo execution.

Original RR3 ARM64 QEMU probes and production signal selection remain unchanged. Known signal-64 timer failures remain failures, not skips or native passes. Native ARM64 5847 focused/race/timer proof and its separate backend diagnostic rerun are parent-owned; they do not qualify this source or erase QEMU outcomes. Native Darwin is separate as well.

The parent may hand off an installer checksum prerequisite. Current 7e41 already has a pinned cli/wago-installer/go.sum. On handoff, compare the exact non-doc diff and source identities; retain this immutable export. Validate affected installer/root/CLI/build/CI gates in a separate exact-pin delta export. Do not relabel existing runtime receipts or silently change the candidate pin.

Reporting only: run report.py, ledger.py, then refresh-remaining.py. Before reuse/final handoff, run integrity.py and process-drain.py (the latter requires no active owned tests). No performance timing, benchmarks, lease, production edits, push, PR, subagents or new review. Evidence-only changes cannot affect shipped performance. Shared dirt, index and submodules remain outside this worker's edits.
