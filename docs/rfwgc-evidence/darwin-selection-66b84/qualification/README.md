# Darwin unsupported-contract selection repair

Source: 66b84a47705a82a09617c4f96fdcdbeb0b983d27; parent af48e57b41cf4251459178ac0c1c2055edad8a72.
Only two test build constraints changed. Supported Darwin ARM64 receipt adapters must not execute unsupported-only rejection contracts. Positive make42/initializer55 and global publication test files remain selected. Windows rejection files and RV64 token controls remain selected.

Results in results.json are cross-compilation and standard-Go fallback execution, not native Darwin/Windows execution. Each mode checks Darwin ARM64, Windows AMD64/ARM64, Linux RV64 source selection and test-package compilation, plus fallback metadata controls. The driver was paused after the ordinary fallback command for an owner-directed diagnosis quiet window; that command finished naturally. It resumed only on explicit release, without repeating completed gates.

Separate Linux race failure at af48: package alarm fired after 600.038 seconds, WAST parent age 10 seconds and div-rem child age under one second. No data-race report or child-deadline failure is present. The nonverbose log cannot attribute preceding suite cost. Workflow command/default timeout are unchanged from main90f2. No timeout change or runtime conclusion is made by this repair.

All campaign evidence stays off the PR branch. Native CI and package-duration diagnosis remain open.
