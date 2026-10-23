# Preserve ambiguous deferred resolver trap locations

PR1011 review found that a collapsed resolver failure tail creates one trap record after runBody. Its current Wasm PC belongs to the final source instruction, not to the failed resolver. Singleton trap groups then report a false precise source location.

Add trapAlwaysUnknownPC. It uses the normal trapSite hook and function attribution, then records the existing uint32 all-ones unknown-PC sentinel. Both normal and shared trap emitters already support it. Apply it only to deferred defined test/cast, final struct-field and cast/array-get cold tails. Ordinary inline trap checks retain exact source PCs.

The fresh and codec generic resolver regressions now require an ambiguous PC. Parent production fails at false PC0x41/0x44 in22.967s; the fix passes in25.425s, together with exact direct/inlined unreachable and ambiguous shared-site frame tests. Raw commands and diagnostics: data/deferred-resolver-trap-pc-2026-10-21/. No guest hot instructions, GC checks, roots or ABI change. Daybreak review is required before this commit is pushed to the ready PR; it will also review the adjacent saved-pin fixes.

The subsequent leaf-trap note retains the completed current-mode Daybreak review and fixture review; both report no concrete findings.
