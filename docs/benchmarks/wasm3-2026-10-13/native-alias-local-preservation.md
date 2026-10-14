# Native alias local preservation

Retain e6888634. Restrict native leaf caller local preservation to modules with multiple imports. This keeps the compact-import gain while restoring the single-import native thread body byte for byte to bf51902c. It supersedes the broader d9d12e8a experiment, whose full snapshot regressed native thread identity and was withheld from publication.

Fresh and reversed wasm.fyi adapter captures improve ordinary compact imports by 10.59% and 10.17%, to about 600 microseconds, still about twice SIMD w2c2 Clang. Native thread identity changes by -0.62% and -0.15%. The opaque callback changes by +1.36% and -2.09%; no consistent opaque improvement is claimed. The minimal dependent callback loop changes by -1.18% and -0.76%.

All paired source identities, artifact and contract hashes, four phases and fifteen samples per successful phase passed the audit. Focused native alias, FP local, shared-memory, cross-instance and runtime tests passed. The FP local test now includes two aliases so it exercises the restricted optimization.

The complete snapshot audited 266 current captures and 798 current/baseline/peer manifests. Functional outcomes are unchanged from bf51902c. Main and feature steady geometric means versus that baseline are 0.995 and 1.006. Native thread identity is 335 microseconds, ordinary compact imports 611 microseconds, and the opaque callback 4.390 milliseconds. These are 1.72, 2.05 and 22.69 times their pinned peer timings; per-feature parity remains incomplete. Peer full-suite timings were captured earlier, so these ratios do not isolate this optimization. Compile phases include different JIT versus translation/Clang work and are not equivalent compiler tasks.

The complete source is queued for the existing two-hour local refresh window. No production deployment or push.
