# Rejected select producer-fact experiment

Apple M4 Max arm64, 2026-10-11, isolated baseline 37174169. No production source, hook or catalog option was imported.

The profiler showed CSEL W6 followed by redundant MOV W6,W6 at native offset 0x3f0. The current eager and flags select paths publish a result node without canonical i32 provenance. This default-off experiment records factUpper32Zero for W-form integer selects and select-to-local tee nodes; 64-bit selects remain unchanged. It changes only node provenance, adding no function scan or sidecar. The existing self-load-defined local rule still retains deliberate address renames. No corpus gating exists.

Strict native fixtures passed with codegen statistics, register-allocation checks and guard-page builds. They cover noncanonical serialized i32 carriers, ordinary/flags/tee select paths, both memory bounds modes and invalid addresses, source preservation, unchanged i64 code and unchanged intentional load-rename code. Signal cases use InstallGuardTrapHandler, NewJobMemoryGuarded and CallGuarded. The five focused application contracts passed too. No all148 run followed the flat timing result.

Eight alternating same-thread execution pairs at 200ms found register-allocation median paired -0.23% (IQR -0.33% to +0.17%); pooled off/on medians 16.7445/16.7446 microseconds are flat. Six compilation pairs at 150ms give -0.85%. Only one instruction is eliminated from an infrequent spill-selection path and padding later keeps the total kernel at 1952 bytes. AES-128, components, DCT and dilation images are byte-identical, so their timing shifts cannot be attributed to this candidate; in particular dilation -2.39% is drift.

Root rejected a flat producer-facts import. Sources/tests are retained disabled beside the patch, raw data, native images and assembly diff. Negative timing deltas mean faster; thread locking/QoS do not guarantee physical-core affinity.
