# Preserve context for verified native scalar imports

Initial candidate `d4dea937` against `2cd322f0` in two reversed wasm.fyi runs on Apple M4
Max, using the pinned SIMD w2c2-clang `f303ed27` peer. Exact artifacts, contracts,
bindings and all four phases were checked; each successful phase has 15 samples.
All 24 captures / 96 phase rows completed without collector or resource failures.

| Steady workload | Change, first / reverse | Current / peer, first / reverse |
| --- | --- | --- |
| Ordinary compact-import baseline | -35.96% / -36.16% | 2.309x / 2.317x |
| Owned identity thread callback | -45.03% / -45.30% | 1.723x / 1.713x |
| Owned opaque thread callback | +2.87% / +2.59% | 28.923x / 28.418x |

Compact imports now take about 679 us, and identity thread callbacks about
333 us. They remain slower than the peer. The opaque callback has a small
persistent regression versus the immediately preceding scheduler optimization,
which is explicitly retained as a tradeoff for the two larger gains. Its
latency remains roughly half the pre-scheduler-change measurement; the complete
snapshot separately measures both changes against the original retained source.
Dispatch control measurements changed -50% then 0%, despite unchanged code;
these tiny timer-scale observations are not claimed as an improvement.

The existing native-leaf verifier already proves straight-line register-only
arithmetic, excluding calls, memory, closure/state access and traps. Its native
wrapper writes only caller-clobbered registers and the result buffer. The new
instance-owned dispatch tag records that contract. ARM64 may then omit the
cross-instance context saves, publication, restoration and memory-bound refresh.
Arguments, operands and locals still use the existing wrapper ABI and homing.
Go callbacks and Wasm providers retain their complete ordinary paths. Tail calls
retain the wrapper ABI and mask both dispatch tag bits before using context
pointers. Serialized modules continue dropping native-leaf admission.

A new admission test failed before adding the contract. Afterward native leaf,
owned reference, shared-memory, cross-instance, Core 3 execution and official
atomic diagnostics passed. New coverage checks live locals and mutable globals
across repeated imports, shared memory, register ABI enabled/disabled and shared
base caching enabled/disabled. Native AMD64 host/thread/cross-instance checks
passed on Remote. The first Remote broad run lacked two checked-in dynamic
funcref fixtures; copying those into the owned test cache resolved that setup
failure. A trial alternate working directory also lacked global test fixtures;
the final run used the established package directory.

Three alternating compiler-cost pairs (100 passes, 218 Core 3 / 27 thread
modules) show Core 3 allocation and code effectively unchanged, RSS +0.23%;
thread allocation +0.24%, code +0.41%, RSS +0.37%. Elapsed differences were
-1.33% / -8.11%; these short compiler timings are noisy, not claimed wins.
The selected compact module grows from 1,184 to 1,248 code bytes and the thread
modules from 832 to 864 bytes.

The JSON retains complete paired captures and compiler/validation evidence.
A complete 266-case source snapshot is collecting privately. Local publication
is deferred until complete and the requested two-hour interval has elapsed;
no production deployment or push occurred.

Follow-up complete-snapshot audit found a regression in the minimal dependent
callback loop. Fresh reversed captures confirm 2.72–2.76 ns before versus
3.81–3.84 ns at d4dea937, a 39–40% regression. This revision was superseded
before any local publication. The replacement `dde95c0e` narrows context shortcut emission
to modules with resources or multiple imports, preserving the lean existing
single-import resource-free route. It requires fresh paired and full validation.
