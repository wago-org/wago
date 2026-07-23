#include "textflag.h"

TEXT ·hostSupportsAVX512(SB), NOSPLIT, $0-1
	MOVL	$1, AX
	CPUID
	// OSXSAVE and AVX are prerequisites for querying/restoring vector state.
	TESTL	$(1<<27), CX
	JZ	no
	TESTL	$(1<<28), CX
	JZ	no

	XORL	CX, CX
	XGETBV
	// XMM, YMM, opmask, ZMM_Hi256, and Hi16_ZMM must all be OS-enabled.
	ANDL	$0xe6, AX
	CMPL	AX, $0xe6
	JNE	no

	MOVL	$7, AX
	XORL	CX, CX
	CPUID
	// F supplies ZMM and dword/qword operations; BW supplies byte/word ops.
	TESTL	$(1<<16), BX
	JZ	no
	TESTL	$(1<<17), BX
	JZ	no
	TESTL	$(1<<30), BX
	JZ	no
	MOVB	$1, ret+0(FP)
	RET
no:
	MOVB	$0, ret+0(FP)
	RET

TEXT ·hostPrefersFullWidthAVX512(SB), NOSPLIT, $0-1
	XORL	AX, AX
	CPUID
	// Non-AMD implementations use native 512-bit vector execution.
	CMPL	BX, $0x68747541	// "Auth"
	JNE	full
	CMPL	DX, $0x69746e65	// "enti"
	JNE	full
	CMPL	CX, $0x444d4163	// "cAMD"
	JNE	full

	MOVL	$1, AX
	CPUID
	MOVL	AX, CX
	SHRL	$8, CX
	ANDL	$0xf, CX
	CMPL	CX, $0xf
	JNE	notfull
	MOVL	AX, DX
	SHRL	$20, DX
	ANDL	$0xff, DX
	ADDL	DX, CX
	// Zen 5 (family 1ah) widened the datapaths; Zen 4 (19h) cracks ZMM.
	CMPL	CX, $0x1a
	JAE	full
notfull:
	MOVB	$0, ret+0(FP)
	RET
full:
	MOVB	$1, ret+0(FP)
	RET
