# Cooperative exact linear sum (source-only experiment)

Base fc11f7a7. Opt in with wago_experimental_cooperative_linear_sum. Existing
vector code is untouched; no YMM changes or general interruptibility removal.

Exact existing inspector only: i64 load/add, i32 address +=8, countdown -=1,
branch; nonshared memory32, no effects/calls/EH/custom/GC/inlined source. Original
loop header controlSite stays at its poll. Cold merge metadata records a separate
post-poll marker and original Wasm PC for exact header recognition.

Unlike ordinary noninterruptible mode, candidate recording does NOT prove memory
bounds. hoistedLoopBoundsCover explicitly refuses cooperative candidates. First
scalar load and runtime-guard fallback retain original checks. After that first
scalar iteration, a widened remaining-range check admits only in-bounds,
nonwrapping accesses; failure branches to original decrement/header/poll, not an
early memory trap. No speculative grouped access precedes this check. Existing
4-GiB wrap dispatch remains; failed full-range proof keeps original scalar path.

Polls: original entry/header unchanged; first completed scalar iteration before
terminal decision; each completed group of four including final group; every
scalar tail iteration including terminal. Maximum completed iterations between
polls is four, not five. Zero-count invocation still visits the original header
poll. One dedicated 8-byte spill preserves a live accumulator around the check;
TEST/CMP restores required flags. Successful exit combines modulo-2^64 sums;
interruption uses existing cold unwind and leaves memory unchanged. Runtime
fallback never branches directly to body past the header poll.

Tests prepared for counts 0/1/2/3/4/5/7/8/9/31/511/8192 plus OOB and wrap-sized
counts; enabled vs disabled result/trap parity; emitted selection and first/group/
tail poll sites; deterministic delayed interruption at group/final/tail and
failed-range fallback visits. Delayed tests additionally use the existing
wago_experimental_bounded_vector_polls tag solely to reuse its qualified test
trampoline. Physical full-4GiB-memory execution remains a separate resource-heavy
gate; existing wrap-emission tests must also pass. No tests/builds or speed
measurements have run at this checkpoint. Ordinary default-code/ELF and full
correctness gates still required. Cold merge storage grows by 16 bytes only with the experiment tag; default
builds embed an empty state and retain the existing 104-byte merge layout.

## Sparse physical 4 GiB qualification

`TestCooperativeLinearSumSparse4GiBQualification` uses the existing runtime's
MAP_NORESERVE-backed NewJobMemory(1<<32), touches exactly 64 bytes at each end,
and directly closes the reservation. It compares enabled/disabled code against
an independent uint32-wrapping address and modulo-uint64 sum oracle, including
0xfffffff0/count5, counts 0/1/2/3/4/5/7/8/9, and unaligned boundary OOB cases.
No whole-memory clear, copy, or scan is used. Allocation failure is a failing
UNQUALIFIED gate, never a silent skip. It logs /proc/self/status VmHWM before,
with the touched mapping, and after cleanup; run separately with -v for useful
peak attribution. MAP_NORESERVE does not promise an absolute RSS ceiling.
This corrects the earlier assumption that executing this gate requires 4 GiB
physical memory. Test source is present; execution remains pending CPU grant.

## Sparse group polling (unqualified source checkpoint)

Additional opt-in `wago_experimental_linear_sum_poll64` keeps the original
cooperative tag's four-load policy unchanged when absent. It changes only the
proven-range grouped fast path: after each completed four-load group, poll when
`remaining & 60 == 0`. Low two scalar-tail bits are deliberately ignored. The
last full group therefore always polls, and the first partial period is at most
16 groups; subsequent periods are exactly 16 groups (64 guest loads). Original
entry/header, first scalar, every scalar tail, and failed-range fallback polls
are unchanged. No resident counter or stack slot is added. This is a bounded
amount of guest work, not a wall-clock cancellation guarantee.

Focused tests now derive delayed/final group counts from policy, retain scalar
fallback interruption, cover every tail residue, and count actual group poll
visits with non-activating injection around 64/128 boundaries. Values compare
against the original scalar path. Source formatted and diff-checked only at
this checkpoint; awaiting CPU grant for targeted tests and one adapter build.
