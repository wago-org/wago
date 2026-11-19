# Future flush-plan harness correction (unexecuted)

The original run remains exit 1. This separate source-only package preserves the reviewed variable-shadowing fix as an exact diff and receipt. The Python copy parameterizes the original workspace root as `WAGO_PROOF_ROOT`; this is a packaging-only change. The receipt hashes the original reviewed correction, not this parameterized copy. No harness execution or result replacement occurred.

For a separately authorized future run, copy the script to a new attempt directory as `run.py` and supply the trusted fixture checkout, toolchain, dependency graph, and flags under the configured proof root. The package does not contain those external prerequisites or any compiled outputs.
