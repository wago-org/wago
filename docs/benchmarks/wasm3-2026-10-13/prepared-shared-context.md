# Rooted prepared shared callback dispatch

Retain bf51902c: prepared fixed shared callbacks pass their rooted activation directly to the existing context bridge. This removes the method-value adapter without detaching resources or weakening admission, lifetime, trap, FP, parking, or reentry behavior. Ordinary prepared Invoke and sessions use the same guarded route; unsupported bridges retain the existing fallback.

Two source-pinned benchmark orders, with 15 samples per phase, improve the opaque shared callback workload by 1.78% and 2.21% versus e57e1232. It remains 22.34–22.83 times slower than SIMD w2c2 Clang. Native thread identity, compact imports, and the minimal dependent callback loop are controls; their small changes are not attributed to this optimization. Generated guest code is unchanged in all four cases.

Three alternating public API pairs improve one-iteration prepared calls by 4.72% and 4096-iteration calls by 1.86%, with zero allocations per call. Three alternating compiler pairs over 218 Core3 and 27 thread modules show unchanged code size and allocations. Core3 compile time is 1.2% higher; thread compile time is 9.4% lower; neither is presented as a compiler optimization.

ARM64 diagnostic, official Core3/atomic, prepared-admission and FP preservation tests pass. The FP test forces Go stack growth and GC while poisoning V8–V15. Disabled bridge fallback tests and native AMD64 runtime tests pass. A complete 266-workload frozen-source snapshot is collecting before local publication; no production deployment.

The prior complete e57e1232 snapshot is visible on the local Features page. A frontend rebuild replaced its assets while the server retained the previous HTML shell; restarting the local API restored all 22 numeric Wago feature rows without changing the frontend source or historical observations.
