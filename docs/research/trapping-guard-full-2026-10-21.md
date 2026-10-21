# Bounded trapping-guard certificate: full compiler qualification

This candidate is not published or installed. Prototype 46a9e4e99 adds a
bounded complete-prefix plan: full checked final cast/field read, an actually
selected two-i32 comparison inline, trapping true arm, same-local/same-final-type
second read inside the already-validated extent. No native call, allocation,
safepoint or write occurs while the one-use raw address is pinned. Existing
free RBP/R12/R13/R14 is required. Frame size, saved roots and call masks do not
grow. Unsupported control/EH/custom/vector/interval/pressure patterns fall back.

Common dependencies c6db3230a contain existing PR1006 and PR1007 changes; these
are already in the installed control. Combined qualification is
59f785143d7ed810f305bce435d1bef6768adf25 above a13816b55. Production patch SHA256
9750d2e14358734c2a34cdf7d5cd485b8f21dbd374eb24ca81bbc9c6ee51dac0.
It applies without conflicts to the complete installed stack. All four
production paths and six fixture/test paths are the prototype source bytes.
Normal focused tests pass in 27.460s and checked+stats in 28.513s; host builds
in 8.162s (SHA256 3a12c39b2d865064e5086ff6db18a91c0f08cffa9d53793b6cd79885e3c05807).

All sixty micro samples pass. The positive 8192-call loop falls
83186 to 65747 ns (-20.96%), same-field single entry -2.85%, two negative cases
-0.20/-0.23%, seven-actual-pin fallback +0.91% with overlapping samples.
All report zero allocations; some loop samples report 1–2 B/op, retained as-is.

Daybreak Blue independently reviewed the bounded proof, real inline selection,
stack/control/trap paths, exact root preservation, extent and pin lifetime and
found no defects before proposed publication. Test gaps include every comparison
opcode, i64 second read, every cache register, failed-lookahead budget boundary,
direct exclusion admission tests and synthetic second-load source profiling.

Ordinary public Compile under telemetry admits six guard reuses in six
functions, including three hot array_get_unchecked specializations. One is
918B versus the installed fusion's 942B, with two native resolvers and one
numeric inline, 56B frame/two spills/two actual local pins. Compile is 0.464s.
Native code is 24,170,715B. Required exact roots remain 25,794 safepoints,
56,923 source calls, maximum 77 roots and 1,983,045 metadata bytes. The overlay
captures diagnostics only and is never part of the timed or installed binary.

Six Wago-only full-job samples pass. Control is41.780014,39.043444,39.137522s
(median39.137522); guard is37.121889,37.454365,38.212315s(median37.454365).
The median reduction is4.301%, and all candidate samples are below all control
samples. Output Wasm/cache hashes, validation/main42 and exact-root admission
all pass. No V8 timings are repeated. Full-suite/public checks remain pending.
Static evidence is in data/wago-trapping-guard-static-2026-10-21/.

A second independent review used the supported current-model route, GPT-6 Sol
with explicit daybreak_blue access through Codex CLI. It found no concrete bugs.
Its initial claim that empty/NOP-only true arms were admitted was withdrawn on
recheck: line143 requires unreachable before the tail loop. The outstanding
coverage gap is successful reuse with each of R12–R14. Both the original review
and the correction are retained; no production change was needed.

## Final joint qualification

Both public feature suites pass: normal26.388s and checked+stats27.944s.
The complete combined root suite passes normally154.411s and with the register
checker143.381s; the nested benchmark module passes checked28.225s. Exact three
pinned spec corpora setup took44.692s. Full commands/output/failures are retained
in data/gc-read-followups-qualification-2026-10-21/. No production changes followed
these checks. Both isolated full-job matrices passed and their effects are
reported separately. A final combined full-job pair is recorded separately.

The final combined serial integration pair is42.173588s control and39.366880s
candidate (6.655% lower). This is one directional pair, not a three-pair estimate.
Both outputs match the frozen Wasm/cache hashes, validate/main42 and have required
exact roots. No build or test overlaps these two timings. Raw commands/identities
are in data/wago-gc-read-combined-2026-10-21/. Relative improvements from the two
independent matrices are not added, and absolute times across matrices change
with host conditions. No V8 timing is repeated.
