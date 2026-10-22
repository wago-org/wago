package abi

// Trap values shared by native emitters and the production runtime.
const (
	TrapNone = iota
	TrapUnreachable
	TrapBuiltin
	TrapLinMemOutOfBounds
	TrapLinMemCouldNotExtend
	TrapIndirectOutOfBounds
	TrapIndirectWrongSig
	TrapLinkedMemNotLinked
	TrapLinkedMemOutOfBounds
	TrapDivZero
	TrapDivOverflow
	TrapTruncOverflow
	TrapInterrupted
	TrapStackFenceBreached
	TrapCalledFnNotLinked
	TrapUnsupportedTailCall
	TrapNullReference
	TrapUnhandledException
	TrapCastFailure
	TrapTableOutOfBounds
	TrapAtomicUnaligned
	TrapExpectedSharedMemory
	TrapHostEventOverflow
)
