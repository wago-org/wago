# Scalar argument writer without captured closure

Experimental source-only checkpoint based on mapped branch 4c28014c. The zero-to-four argument writer now calls ordinary explicit-argument helpers instead of a captured local function. Stores retain descending 3-to-0 order, 32-bit truncation for non-wide slots and little-endian encoding. Invocation admission, native entry, traps, lease cleanup, general fallback and the greater-than-four argument path are unchanged.

Motivation: the previously measured same-policy TinyGo ELF has three unconditional allocation calls at invokeScalarAdmitted entry, requesting 8/24/8 bytes and boxing fn/args/in before any argument-count check. This source change aims to remove capture boxes. Neither removal in generated ELF nor runtime benefit has been established for this candidate; yyjson's actual selected path and early spike cause are still unverified.

Pending qualification after CPU grant:
- gofmt and focused TestPreparedScalarArgumentsSmall tests: all zero-through-four counts and 16 width masks, untouched trailing bytes, nil zero-arg case, descending bounds-panic behavior.
- Standard Go existing prepared-function/scalar/trap/cleanup/ownership regressions, then broader public tests where feasible.
- Actual TinyGo scalar tests with production sources intact; retain exact public-package compilation blockers if any, not a false full-suite pass.
- Build exact official default-stack mapped adapter with matching prior configuration. Independently disassemble invokeScalarAdmitted and any emitted helper/inlined sites; verify the three entry runtime.alloc calls disappear and are not merely moved to a helper. Record source/binary hashes and compare native guest images/oracles.
- Only then compare unchanged exact yyjson three-way early trajectories and full execution gate. No extra warmup, forced GC, different resets or cleanup suppression. Keep observer diagnostics separate from acceptance.

This worktree is sparse (src and tests/support plus root files) to fit temporary disk. Missing full-corpus test fixtures must be restored selectively or reported, never treated as successful skipped coverage. No publication or build at this checkpoint.
