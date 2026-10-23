// Counter slots are outside outgoing Go arguments and parked native metadata.
// Each owner initializes its own counter; nested owners cannot reset it.
#define INLINE_HOST_YIELD_INIT(counter) \
 MOVD $const_inlineHostYieldInterval, R9; \
 MOVD R9, counter(RSP)

// Results are already published. A Go call may move the stack and clobber all
// argument registers. Reload the control pointer, and honor cancellation before
// restoring native state. The common path adds no call or allocation.
#define INLINE_HOST_YIELD(counter, done, abort, trapOffset, ctrlOffset) \
 MOVD counter(RSP), R9; \
 SUB $1, R9, R9; \
 MOVD R9, counter(RSP); \
 CBNZ R9, done; \
 MOVD $const_inlineHostYieldInterval, R9; \
 MOVD R9, counter(RSP); \
 BL ·inlineHostYield(SB); \
 MOVD trap+trapOffset(FP), R9; \
 MOVWU 0(R9), R11; \
 CBNZ R11, abort; \
 MOVD ctrl+ctrlOffset(FP), R10; \
done:
