# Fuse a final type test with its true-arm first field read

Astra found that `local.get L; ref.test (ref T); if; local.get L; ref.cast T; struct.get T F` resolves and validates the same object twice on the successful path. Own only this exact prefix. One native leaf returns the Boolean and checked object address. Its field-extent checks run only after the type test succeeds. False tests retain their original behavior. Recheck final canonical identity after a successful subtype interval lookup, preserving the following final cast even with malformed interval data.

Only final structs, same local/type, non-null test, primitive/no-parameter block signatures and GP scalar/ref32 fields are admitted. Reject intervening operations, nullable tests, nonfinal types, different locals, exact markers, FP/SIMD fields, inline/local-base remaps, EH/custom/interval/vector regions. Bound each lookahead to64 bytes and each function to16 sites. The 17th test uses ordinary lowering.

The leaf returns EAX Boolean/RDX address. Pre-leaf flush materializes older operands. Detached-object materialization can conservatively clear canonicalSlots, so verify actual canonical slot layout before restoring the bit. Pin RDX only through ordinary if-entry setup and the immediate first field load. Release it before normal compilation resumes; it never spans a call, allocation or safepoint. General address-certificate register policy is unchanged. Preserve ref32 root flags and all original source-call masks. Cold traps restore saved pins. The public branch composes this with PR1011 unknown-PC tails.

## Measurements

All full runs use the fixed compiler/request, fresh native/result caches, CPU12,14/GOMAXPROCS2 and diagnostic-free hosts. The control is installed stack8e4a3d7d4; candidate differs only in these four AMD64 production files. Both include the preceding defined-leaf cold-pin fix. Later PR1011 safety follow-ups are qualified jointly before installation. No V8 compilation was timed: the known target remains4s.

The first preserved-register candidate admitted929 sites in257 functions. Its full medians37.3136→37.0700s overlap; hold this limited variant. The final RDX version admits5196 sites in1443 functions, with16-site caps. Native bytes24177611→24501563 (+323952,1.34%). Exact native root admission is unchanged:25794 safepoints,56923 source callsites,max77 roots,1983045 metadata bytes. Diagnostic admission JSON is gzip-compressed without timestamps; decode with `python3 -m gzip -d` after copying the chosen file.

Final six-run balanced full comparison:

| | Three times(s) | Median(s) |
|---|---|---|
| Control |38.0470,36.6536,36.3057|36.6536|
| Candidate |36.1912,35.8262,36.2259|36.1912|

Reduction1.2615%; all candidate samples fall below all controls, with a narrow minimum separation of0.080s. All outputs have the required hashes and main()=42; all artifacts retain exact native roots. Median peak RSS differs by about1.2MiB, which does not establish a memory improvement. The candidate is still about9.05 times the known4s V8 result.

Final fixture/micro comparison uses three balanced six-run rounds on CPU15/GOMAXPROCS1. For8192 reads, successful test/field loop median55025.5→40140.5ns (-27.05%). Intervening-nop fallback45899.5→45770ns (-0.28%, noise). Wrong-type loop42486.5→43291.5ns (+1.89%): do not claim every path improves. The earlier micro series used the earlier fixture without added dirty-global/root/cap cases; retain it separately, do not mix samples. Full results determine the small end-user gain.

## Validation and limits

Initial parent tests show two resolver calls instead of one and no fusion counter. Early red fixture revisions also exposed harness errors: absent root plans for noncollecting leaves and an incorrect seven-pin expectation. Both corrected cases and all raw failures remain visible. Final checked tests admit the pressure case with four pinned locals and one resolver, reject the intended negative shapes and enforce16/17-site behavior. Fresh and codec runs cover null/i31/wrong types, packed signs, conditional short header/handle extents, bad spaces and forged subtype intervals. Six dirty globals prove actual R9-R11 saves and their values after a field-extent trap. Reference roots survive forced collections both in local storage and below the test on the operand stack; normal, tiny and forced nursery configurations pass.

Checked focused tests pass26.954s, normal25.764s; ordinary predicate fallback with WAGO_AMD64_LONE_PREDICATE=0 also passes. Profiling/checked tests pass23.210s, including code-byte neutrality, source and unwind map validation, the fused first-field source location and a retained root under forced collection. The initial profile harness observed before deferred GC code mapping and saw no image; instantiating both artifacts before observing corrects the harness. Its raw failure remains.

The regular-Go lean production source build passes2.378s. Its unit lane fails in unchanged compile_test.go zero-length diagnostic arrays at111/125/130 on BOTH parent and candidate; this is not a claimed real TinyGo runtime qualification. The corrupt high32 global is rejected by the existing public invocation boundary before native entry; its test checks that diagnostic and does not claim native-leaf coverage. No physical check is removed.

Astra supplied semantic/register proof. GPT-6 Sol with explicit Daybreak Blue reviewed the preserved and RDX source variants (88.146s/108.222s) with no code findings. Requests, runtime model/mode records, test failures, raw samples, source hashes, admission rows, build/measurement scripts and control variants are retained under data/gc-test-field-2026-10-21/. Complete public/combined qualification and composed review follow before publication; their results will be appended.

## Final branch composition and qualification

The older public PR948 base still interprets RSI as an exact-cast flag in its generic defined leaf. Auto-merging the performance delta onto that base made the nonzero required extent select exact matching in field mode. The fresh/codec forged-interval regression caught the error: false was returned where the following final cast must trap. The initial composition review missed it. Retain that failed public test, do not treat review as a substitute for tests.

For the older public leaf, omit the exact-flag branch when field mode is enabled; ordinary interval matching then reaches the canonical/extent checks as intended. Generic old modes remain unchanged. The measured combined stack already has a newer canonical-identity fastpath with exact handling restricted to cast mode; it needs no code change for this correction. Both production forms receive explicit second review, including a corrective Daybreak pass with no further findings. The final public normal25.671s, checked26.490s and profile25.978s runs pass.

Complete combined qualification uses a4126816db82e6b1d7cd257517d7214ab3875131: installed a0be7510f plus the performance delta, retaining its existing canonical fastpath and all PR1011 safety fixes. Full root normal154.837s and checked141.925s pass; benchmark module checked27.480s passes; checked+stats focused24.679s passes. Pinned spec corpus setup32.693s is retained. These are Go suite/setup elapsed times, not Dewdrop compile latency. The user ARM64 edit is outside this clean AMD64 qualification worktree and remains untouched.

Upstream subsequently rewrote commit metadata on32 watched branches. Astra found identical HEAD trees, ordered parent trees and binary patches across all36 processed entries. No code patch needs another pick. The new public performance branch is rebased onto the identical current safety HEAD542e0b529 so its PR shows only this improvement. The qualification's AMD64 source remains the reference for resolving the known older-base exact-flag hunk when picking into the combined branch.
