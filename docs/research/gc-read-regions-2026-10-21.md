# Bounded GC read regions

## Change

Inline small read-only GC helpers through the normal inline parameter homes and control-frame machinery. Admit at most two integer/collector-reference parameters, one such result, no declared source locals, 192 source bytes, two nested `if` frames and eight physically planned GC read groups. Expand only fully matched binary numeric leaves. Follow only proved complete forwarding chains with matching signatures.

Admission uses operations and types. Function names, indexes, standard-module paths and declaration order have no role. Integer-only inlining and forwarding are kept in the same review because this pass extends their shared call path. The public base is PR #1011 above #948; forwarding PR #1008 is merged into the branch for PR #1007. Reference register calls (#1009) and type-test/first-field fusion (#1012) remain separate.

Each removed outer call consumes exactly one original source root mask. Expanded numeric callees consume none. Approved native read leaves cannot enter Go, collect, allocate or write. An unexpected helper, host/internal call, source mask or safepoint produces a visible compiler error. Cursor/safepoint counts are checked at region exit.

A checked raw object address may be reused only for the same unchanged parameter, exact final type and an already covered extent. An earlier check is never enlarged to cover a later field. Keep one certificate in a free preserved register, or in one private ordinary inline local if those registers are occupied. The private home is unpinned, explicitly non-root and separate from source locals and operand spills. Its certificate starts invalid at each splice; store the checked address before making the proof valid. No initialization store is needed. Never replace a valid home for a different object or a larger-extent read while a saved entry proof could be restored. Branch-created proofs do not escape a join.

Reserve a private home only for a later covered read under a dominating lexical control path. Single-read helpers do not emit a cache store or reserve a home. Keep the caller's original `nLocals`; include compiler locals in frame size and keep the stack fence. Limit emitted read regions and distinct reserved read targets to 16 per caller, so added read-region storage is bounded by 48 eight-byte slots. An admitted inline target grows from 56 to 64 compiler-only bytes for its immutable plan pointer; the dense four-byte lookup per local function remains unchanged. The explicit structure-size guard is updated to cover that intended cost.

Native staging first canonicalizes the older operand prefix through the checked partial-flush path, with all consumed operands still linked. Protect their complete slot extent until materialization finishes. Operands bypass redundant canonical stores. Preserve an index that owns RAX before moving the object to RAX. Existing physical type, tag, null, handle, extent and array-bound checks remain. The register checker is retained.

## Defects caught before publication

Daybreak found that an integer control join can overwrite RBP despite an allocator reservation. The red allocation-policy test reproduced the choice of RBP. Region emission now saves/disables register merging for its synthetic and nested control frames and restores the caller's policy on all exits. Caller frames retain their own merge flags. The cache allocator also excludes the merge register when merging remains active.

An all-trapping inline body can make later caller calls unreachable to emission even though the original root plan still counts their masks. A public regression produced `Required:true, Exact:false`. The planner now tracks fallthrough through its bounded control structure and rejects all-trapping bodies before modifying caller state. The fixture keeps that ordinary call, followed by an allocating call; exact roots and the unreachable trap both pass.

All red results are retained. An early report write failed because the disk was full; 16 GiB of Go build cache older than one day was removed and the check rerun. One micro harness initially used the wrong working directory. An initial counter callback after CLI `Main` did not run because that path exits the process; it was moved into the run command's finish callback. These failed attempts are not timing evidence.

## Dynamic work

Separate counter builds run the identical pinned compiler/request, with fresh module/result caches, correct output hashes, output `main` 42 and exact roots. Counters preserve registers, flags and RSP. Their absolute addresses name process-lifetime diagnostic storage, so these artifacts must not be installed or reused in a different process. No diagnostic elapsed time is used as a speed result.

Both read-region prototypes execute 1,108,528,643 region entries, 425,448,721 expanded numeric calls and 576,727,202 array reads. The first prototype has 1,192,520,982 checked struct reads and 190,171,619 reused reads. The final pass has 957,251,977 checked struct reads and 425,440,624 reused reads. Its private homes save **235,269,005 repeated resolutions relative to the register-only prototype**.

The leading accessor executes 344,003,732 times. The register-only prototype resolves its wrapper 566,582,763 times; the final pass resolves it 344,003,732 times and reuses that proof for every backing-field read. Of these reuses, 222,579,031 use the private stack home. The baseline standalone trapping guard already reused that wrapper address; the private home preserves that useful work when the helper moves into a caller with no free preserved register. Do not present these prototype-relative counts as new checks removed from the installed baseline.

## Timings

Known V8 reference: 4 seconds. V8 was not rerun. Node is used only to validate the output module's `main` result.

Three serial balanced pairs use CPU affinity `12,14`, `GOMAXPROCS=2`, diagnostic-free hosts, fresh native/result caches, the same 512 MiB heap/64 MiB nursery and the same installed GC stack at `073b2f61880e965d3c33ba83e32f72e9391a845d`. Run order is control/candidate, candidate/control, control/candidate. These are measurements on the combined local stack, not on the narrower public PR base.

| Version | Full-job elapsed seconds | Median |
| --- | --- | --- |
| Control | 35.805910, 35.140665, 35.439423 | 35.439423 |
| Read regions, narrow staging, register/private-home proof | 34.751379, 34.406983, 34.424798 | 34.424798 |

The median falls **2.863%**. Every candidate sample is below every control sample. The result is still **8.61 times the known four-second reference**. The earlier register-only prototype was flat: 35.172388 to 35.079722 seconds, 0.26% within the observed spread. That prototype also predates the control-register fix and was not qualified or installed.

Median user CPU falls from 35.579689 to 34.605678 seconds. RSS medians are 1,097,732 and 1,141,976 KiB; ranges overlap. No memory gain is claimed. Native code grows from 24,516,539 to 25,622,571 bytes (4.51%). Exact-root metadata falls from 1,983,045 to 1,803,076 bytes. Safepoints stay at 25,794 and maximum roots at 77. Static source callsites fall from 56,923 to 50,438: 6,485 regions in 1,554 callers.

The final conservative all-trap filter and reservation cap were added after those timings. An ordinary public-API compilation proves that they emit **byte-identical native code for the pinned compiler**: SHA-256 `5f3a7b1c1c2be27dee30b225df83a7934f8fdc5b8eb8dfb7046f2eedab566c33`, compared directly with the timed saved artifact. Final native compilation takes 0.477 seconds in that diagnostic. This is source/code identity evidence, not another latency sample.

The focused benchmark performs 8,192 reads, with correct sums. ABBA samples run on CPU 15 with `GOMAXPROCS=1`, 200 ms per case. The final fixture includes both ordinary and preserved-register-pressure loops. The final ordinary loop falls from 69,774 to 62,162.5 ns per batch (10.91%); the pressure loop falls from 77,723.5 to 68,563.5 ns (11.79%). Both report zero allocations/op. Some ordinary-loop samples report one or two amortized bytes/op from first-entry setup, so this is not a zero-byte claim. Exact results and every sample are in `micro-final/summary.json` and raw output. An earlier ordinary-loop fixture measured 70,086 to 61,458 ns per batch (12.31%); that is kept separately.

Compiler SHA-256: `c64fcf044b72cb7356ad598a35670045931337cfa7a6bb9d5916cd304b23fba6`.
Request SHA-256: `077b0ff0271706cc3034f3e06c7a228f227c49aeec7786d21373b2072af89c72`.
Output Wasm SHA-256: `89af055df4b2a4e44be109f10f50d98b5e29deb4810194aca234c9e019912fbb`.
Output cache SHA-256: `c3a9142e2f4a1186fb5d47f78dfea5adcbf1892e52ff0a803a7c3332e7236240`.
Managed host patch SHA-256: `3f016f6442cc891cf982b5edf6f4de9952e7cb03b634c5c40f5c4e7f8842ec87`.

## Validation and reviews

Focused normal, register-checked and source-profile tests cover compiled/saved-code reload; forced minor/major and Tiny collection with verification; null, wrong type, negative/high indexes; missing/short backing storage and trap order; conditional 20-byte versus 24-byte header/handle extents; live reference operands and returned references across collection; different objects at consecutive splices; distinct cached targets; branches/nested branches and sibling object reads; no-free-register private homes; the 16-site ordinary-call fallback; and rejected allocation/store/loop/host/unsupported-type/large/all-trap bodies. Unexpected helper/mask/safepoint paths have visible compiler diagnostics. A separate bound test limits caller reservations.

A fresh Astra agent supplied the whole-GC audit and the bounded implementation/proof constraints. Claude supplied an independent constructor-check research lead; that lead is not implemented here. Actual GPT-6 Sol Daybreak Blue CLI sessions performed the code reviews. Root verifies `model=gpt-6-sol` and `cyber_access_program=daybreak_blue` in their session turn contexts. Source review alone is not an execution or speed claim. The first review found the control-register defect; subsequent private/public reviews found no further actionable issue.

The full public normal run took 144.131 seconds and passed every package except the compiler-only target-size guard (56 versus the intended new 64 bytes). After updating that guard, the complete AMD64 package passed in 9.628 seconds. The full checked public suite passed in 133.727 seconds, the full checked benchmark suite in 29.801 seconds, and the final focused stats/checker/profile suite in 26.003 seconds. All initial failures and full-package rerun logs remain visible. Combined-stack checks and installation are recorded separately after the pushed commits are picked. Wago's pre-existing native ABI value remains 9. No ABI or codec version changes.

## Evidence

[Raw reports, failures, build identities, counter requests and review mode evidence](data/wago-read-regions-2026-10-21). Native profile files and timing hosts remain in the paths named by the manifests. No native timing hosts or test executables are embedded in this evidence directory; small input Wasm fixtures are retained for reproduction.
