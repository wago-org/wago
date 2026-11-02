Pushed platform-test repairs at `af48e57b41cf4251459178ac0c1c2055edad8a72` (five test files only; no production changes).

- Windows GC publication test now proves the deliberate GC admission rejection, then checks authenticated-wrapper public global/table rejection and unchanged storage. GC-capable targets keep guest execution; bulk atomicity coverage is unchanged.
- Moved only the executable cold-decoder regression to an `_amd64_test.go` file. Cross-host AST/routing contracts remain untagged. The moved function is byte-identical; the old ARM64 build reproduces the failure.
- ARM64 import tests now reflect the explicit Linux/Darwin unwind-map policy and Windows no-plan specialization. All invocation outputs, callback counts, native admission checks, repeated division traps, and code-image oracles remain covered.

Final immutable pin: 27/27 local commands PASS across ordinary, wagodebug and wago_regalloccheck, plus 12 source-selection checks. Includes Linux ARM64 QEMU public and backend execution and backend/Wago cross-builds for Windows AMD64/ARM64 and Darwin ARM64, plus Linux ARM64/RV64 backend cross-builds. Native Windows execution remains pending normal CI; cross-builds do not close that gate. Fallback-adapter checks use standard Go with wago_target_tinygo, not native TinyGo.

Original failure logs and exact receipts are retained only on `campaign/rfwgc-port-record` under `docs/rfwgc-evidence/platform-test-repairs-af48/`. PR remains 267 files. The old c32 measurement pin is superseded; no samples were acquired. Historical PDFium timeout and final performance proof remain separate open items.
