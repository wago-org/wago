> Historical source006 checkpoint. The subsequent retained near-parity result is documented in [native-shared-parity.md](native-shared-parity.md).

# Post-main integration qualification

Source `0666c2e3eaa7536b3743ef6f36cf047e89828133` is retained in PR #963 at merge `20457298`. The merged compiler/runtime source tree matches that measured revision exactly; capture identity remains `0666c2e3`. Main `069a26ea` is integrated. The earlier `6eb3a1df` mixed-memory cache fix also passed a targeted native AMD64 regression; externally interrupted Remote validation is not a full AMD64 pass.

The retained fixes restore shifted and paired dot/hash vectorization under optional literal-register pressure, preserve validated native helper pins across fixed-shared import alternatives, prefer encodable immediate adds, and prove complete constant-copy ranges against immutable memory minima. Tests cover source/artifact execution, overlap, zero length, independent memory minima, failed growth, traps, native/Go fallback and policy rollback. Broad ordinary/checker backend/runtime/public checks and focused profiling checks pass. Full qualification uses Go 1.27.1, GOMAXPROCS=1 and the Apple M4 Max device lock.

## Matched SDK comparison with published source9ed

All 266 workload contracts per source complete: 532 captures, 15 timing samples per successful phase, no outcome changes or collection/resource failures. Every ratio below compares **0666c2e3 / 9ed26450**, not the main branch. Numeric pairs exclude unsupported outcomes.

| Cohort | Steady time | Compile time | Compile peak RSS |
| --- | --- | --- | --- |
| All, 255 numeric pairs | -6.48% | +7.31% | +2.76% |
| Features, 107 numeric pairs | -0.73% | +12.88% | +3.19% |
| Application/core, 148 numeric pairs | -10.43% | +3.45% | +2.44% |

All-cohort instantiation time increases 8.09%; first-call time decreases 6.68%. The generated-image audit verifies all 255 persisted SHA256 hashes and byte counts against actual SDK captures; 160 images change, including integrated main improvements. The prior unchanged-image result belongs to source9ed, not this checkpoint.

## Reversed outlier checks and remaining costs

The selected 127 workload contracts receive 508 additional captures: two sources, both orders, nine independent launches and 45 samples per successful phase/order. Two steady regressions over 5% repeat:

| Workload | Forward | Reverse |
| --- | --- | --- |
| yyjson parse/edit/write | +6.95% | +17.82% |
| Point-to-segment distance | +10.28% | +6.53% |

Seventy compile, 48 instantiation and three first-call cases exceed 5% in both orders among this selected set. Complete rows, including improvements, are retained in the evidence. This is not a regression-free result, and these selected counts are not a whole-corpus failure rate.

Ordinary shared host loops improve 7.30%/24.54%, opaque host loops 6.44%/20.29%, and private-secondary copy 11.82%/10.70%, versus source9ed. The varying host-loop gain magnitude remains visible. Separate source0666/pinned-w2c2-Clang exact-contract nine-launch comparisons put ordinary helpers at 1.513x/1.502x, opaque helpers at 1.501x/1.492x, and compact imports at 1.028x/1.028x peer cost. Parity remains incomplete. The complete source0666/peer samples are in `post-main-peer-controls.json`. These use owned typed callbacks and WasmFunc.Invoke; they do not establish legacy Instance.Invoke cost.

## Fresh pinned-main compile control

Five independent fresh-process launches per artifact/source/order cover all 145 core application artifacts against main `069a26ea`. Source0666 compile time increases **7.82%/8.30%**, allocations **0.48%/0.48%**, and process peak RSS **2.93%/3.06%**. Input/config creation is outside the Compile timer; process RSS includes startup/input/config/compile/close. This standalone control supplements SDK resource captures and is not an execution comparison against main.

## Compatibility and delivery

All 373 canonical ARM64 compatibility contracts complete: **346 pass, 27 unsupported, zero fail**, unchanged from source9ed. Existing w2c2 Clang -O0 compatibility is separate from optimized peer performance; its failed/unsupported feature markers remain correctly gated on complete compatibility. No production deployment is authorized.

The exact-source local refresh has 639 captures/1,437 observations. The receipt, preserved-history, database-health and browser audit is recorded in `local-publication-post-main.json`: all 613,809 prior observations are preserved; SQLite quick_check passes and foreign-key violations are zero. Browser verification at the requested mean/performance URL sees source0666, 22 numeric Wago feature rows, Compact Import Section ~343 µs and Threads ~266 µs, with no browser errors. The compatibility page also sees the new source without errors. Capture timestamps/revisions remain unchanged. See `local-post-main-features.png`.

Native target caching is held for insufficient gains. Native argument staging `1549bac6` is held because independent SDK runs regress in both orders despite favorable paired diagnostics. Adjacent plain shared-memory forwarding is a separate private experiment, documented in `shared-memory-forwarding-semantics.md`, and is not part of measured source0666.

Readable aggregates and every repeated outlier are in `post-main-qualification.json`. `post-main-qualification-evidence.json.gz` preserves 1,413 exact captures with SHA256 hashes, the fresh-process main controls and image audit. The prior publication and controls remain historical evidence.
