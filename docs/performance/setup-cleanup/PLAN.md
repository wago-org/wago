# Setup and cleanup measurement plan

Baseline source: 95be283fa511db7b01d86ef72b6c58fbe1ab607a, matching the input report. Initial local changes are recorded in environment.txt. They include an unrelated .gitignore edit and untracked user files. No tracked production source was changed at baseline.

The old report identifies Go 1.27.1, Linux amd64, Ryzen 7 8845HS, 16 benchmark CPUs, WAGO_BOUNDS=signals, wago_guardpage, count=1, benchtime=1s, and all corpus workloads. Its GC settings, affinity, governor, and background load were not saved. It cannot supply a statistically controlled regression comparison. Dependency pins at its source revision match the current go.mod, bench/go.mod, and go.work. The new focused baseline is separate.

New runs: Go 1.27.1, GOAMD64=v1, CGO_ENABLED=1, wago_guardpage, CPU affinity 0-15, GOMAXPROCS=16, GOGC=100, GOMEMLIMIT=off, GODEBUG empty, WAGO_BOUNDS=signals. No single-CPU restriction on worker tests. See environment.txt for governor, kernel, and module sums. No dependency changes are planned.

Sample count fixed before measurements: ten per selected case. Diagnostic command phases use 1000 iterations per sample to bound untimed setup costs. Snapshot diagnostics and final full command/control comparisons use 200ms per sample. Baseline and candidate binaries have identical diagnostic source. Final order alternates AB, BA, for ten paired rounds. Profiling runs are separate and not included in benchstat. Do not add samples based on the observed result.

Priority: measure short commands and identify a concrete allocation site before any production edit; implement and validate the smallest confirmed fix before any independent optimization. Diagnose memory reuse and worker behavior without assuming those paths need changes. Keep the worker default at one.

## Existing timer boundaries

CommandExec: excludes compilation, input-file read, and an oracle run. Includes fresh raw WASI import construction (including config copies and state), snapshot/validation, instantiation and Wasm start function, export lookup, guest command execution, and instance Close. Wago does not construct a Runtime here. Compiled Close is absent in this existing benchmark; preserve it unchanged.

WazeroCommandExec: excludes Runtime creation, WASI host registration, compilation, input read, and oracle run. Includes fresh module configuration, guest instantiation, Wasm start function, export lookup, command call, and guest Close. WithStartFunctions disables conventional export start calls; it does not remove the Wasm start section. Deferred compiled/runtime Close occurs after the timed loop while the timer is still enabled, and is amortized into the result. The reusable WASI definitions use guest state from each fresh module.

Instantiate: excludes compilation and hostStubs construction. Includes public import snapshot, cached metadata preparation, instance allocation, memory and tables/globals/data initialization, Wasm start if present, and Close. Compiler output already has frozen metadata before publication (`publishCompilerCompiled`). Its first timed iteration can include executable mapping/activation; later iterations reuse that code state and the bounded memory caches. Hand-built Compiled values have a different first-use metadata lifecycle and are not these fixtures.

WazeroInstantiate: excludes Runtime creation, host registration, compilation, and a probe instantiation/Close. Includes fresh config, InstantiateModule, and Close. Deferred compiled/runtime Close is amortized inside the timer.

Diagnostic command Imports measures fresh raw WASI bundle construction only. Instantiate measures snapshot/binding plus instance creation with a fresh bundle prepared outside the timer. LookupExecute measures public Invoke (lookup plus call) on a fresh instance. Close measures cleanup after execution. Lifecycle calls the unchanged helper. Compilation, oracle checks, and final compiled Close are outside these child timers. No mutable command state is reused.

ImportSnapshot uses 1 or 46 generic HostCall declarations with WASI-length names. It measures snapshot validation directly; construction is outside the timer. It is a synthetic diagnostic, not a substitute for the end-to-end command measurement.

Process-wide CPU and allocation profiles include untimed setup. Attribute samples by call path, not benchmark phase name. Prefer the unchanged CommandExec loop or the complete Imports loop for profiles.
