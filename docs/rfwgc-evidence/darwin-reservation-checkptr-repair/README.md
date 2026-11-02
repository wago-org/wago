# Darwin reservation inspection repair

Native audit `e3222e019` identified a checkptr abort at a test-only uintptr-to-pointer conversion. The analogous large-array conversion had the same risk. Both are replaced with reflection reads of the exact collector-owned uint32 Count fields while the instance is idle and live. There is no reconstructed pointer, checkptr suppression, production API, or production code change. The 47 live-struct calls, 2,000 array calls, every return-value assertion, zero-unused-reservation assertions, and exact allocation totals are unchanged.

An immutable source08f export plus an exact hashed single-file overlay passed both tests in ordinary, wagodebug and wago_regalloccheck under ARM64 QEMU with `-gcflags=all=-d=checkptr=2`: six build/execution commands exit0, six test passes, zero skips. The existing narrowly reviewed signal map was retained. This is strict-checkptr execution, not race or native Darwin qualification. The original native failures remain preserved. Native Darwin all-three-mode race follow-through and independent review remain required.

`run.py`, `results.json`, `provenance.json` and raw logs retain exact commands, source overlay hash, environment and outcomes. The sibling read-only audit is retained at `../native-run-41466555967/sibling-audit.json`; no equivalent bridge/raw-state test was found. This repair does not address the separately unresolved TestTwoIntegerResultRegisterCall native stack overflow.

Only test inspection and evidence changed; shipped runtime performance cannot change. No benchmarks, lease, dispatch, push or PR. All owned commands completed.
