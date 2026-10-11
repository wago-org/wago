# Rejected memory-consumer borrowed-source shifts

Rejected on Apple M4 Max arm64, 2026-10-11, isolated baseline 37174169. No production source, hook or catalog option was imported.

The previous generic borrowed-source shift experiment was already rejected for dilation regressions. This candidate narrowed admission to a deferred i32 left shift by a constant used immediately as a memory32 address, reading a permanent GP-pinned local. Regional leases, globals, memory64, numerical shifts, right shifts, rotates and variable counts retain existing lowering. It emits a distinct destination W-register LSL and owns its scratch result. Wasm count masking, 32-bit wrapping, older pending traps, bounds checks, memarg displacements and source local lifetimes remain unchanged. There is no corpus or manifest gating and no new scan or sidecar.

Native fixtures passed with codegen statistics, register-allocation checks and guard-page builds: source aliases/overwrites beneath sixteen older local reads, counts -1/0/1/2/31/32/63/64, unsigned-high/wrapping addresses, and explicit plus genuine signal bounds. The fixture uses guarded JobMemory and CallGuarded for signal lowering; ordinary runArm64WrapperWithOptions does not support invalid signal-memory addresses. Five application input contracts also passed under paired execution. All148 correctness was deliberately not run after the failed performance screen.

The native register-allocation kernel shrank from 1952 to 1920 bytes and removed exactly eight MOV X12,local copies before LSL W12,#2. Components shrank 1556 to 1520 bytes; dilation, AES-128 and DCT native images were byte-identical. Screen measurements were six same-thread alternating pairs at 150ms per state; confirmation used ten execution pairs at 300ms and eight compilation pairs at 200ms. All ran through the shared FIFO benchmark lock as counted-next.

The confirmation found register-allocation execution -0.14% median paired (IQR -0.37% to -0.00%) and components +1.34% (IQR +0.80% to +2.24%). Compilation remained near noise: register allocation +0.39%, components -0.17%. The tiny target gain does not justify the repeat components regression. No padding mitigation or broader qualification was attempted.

Raw observations, complete native images, focused assembly diffs, source patch and disabled source/tests are retained here. Negative timing deltas mean faster. Physical-core affinity is not guaranteed by thread locking or user-interactive QoS.
