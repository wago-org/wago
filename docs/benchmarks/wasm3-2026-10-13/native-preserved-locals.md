# Native leaf caller local pins

Retain d9d12e8a provisionally: runtime-verified native i32 leaves clobber only X0–X17 and no FP registers. Their caller branch now keeps higher GP and FP local pins in registers. Ordinary Go and cross-instance branches still spill and restore the same locals, and the outer direct-Go branch joins with the same tracked local state. The wrapper ABI, lifetime, native scheduling, traps and ownership rules are unchanged.

Fresh adapter captures and reversed source order improve ordinary compact imports by 11.00% and 11.16%, to roughly 590 µs. It remains 2.02–2.04 times slower than SIMD w2c2 Clang. Code grows from 1248 to 1280 bytes. Three alternating public prepared Invoke pairs independently improve compact imports about 10–11%, with zero per-call allocations.

Native thread identity is 11.95% slower in the first order and 1.55% faster in reverse; no repeatable thread improvement is claimed. The opaque owned Go callback is 0.39% and 2.17% slower, still about 22–23 times the peer. The minimal dependent callback loop is minus 0.56% / plus 0.58%, with unchanged code size. A complete corpus audit must still confirm this tradeoff.

Focused native scalar, aliases, FP-local, shared-memory, ordinary-Go and cross-instance tests pass. The new FP test keeps four computed f64 locals live across native and effectful Go callbacks, both before and after artifact round-trip. The real official Core3 TestSpecSuiteExec and pinned threads atomic suite pass without skips. An earlier selector omitted TestSpecSuiteExec; it is not used as evidence of Core3 coverage.

Three compiler pairs over 218 Core3 and 27 thread modules retain allocations and code size. Default-bounds latency is +1.9% / +6.2%; explicit-bounds latency is -0.3% / -3.9%; these short differences do not establish a compile win or persistent regression. Peak RSS stays within about one percent. Complete source-specific adapter captures include their own compilation phases.

All paired capture source identities, artifact/contract hashes, four phases and fifteen samples per successful phase passed the strict audit. No production deployment; complete local source refresh follows the existing two-hour window.
