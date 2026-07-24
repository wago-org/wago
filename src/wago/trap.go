package wago

import wruntime "github.com/wago-org/wago/src/core/runtime"

// TrapError is returned by Invoke when wasm execution traps. Recover it with
// errors.As and inspect Code:
//
//	var te *wago.TrapError
//	if errors.As(err, &te) && te.Code == wago.TrapLinMemOutOfBounds { ... }
type TrapError = wruntime.TrapError

// TrapCode identifies why wasm execution trapped. Its String method gives a
// human-readable reason.
type TrapCode = wruntime.TrapCode

// Trap codes carried by TrapError.Code.
const (
	TrapNone                 = wruntime.TrapNone
	TrapUnreachable          = wruntime.TrapUnreachable
	TrapBuiltin              = wruntime.TrapBuiltin
	TrapLinMemOutOfBounds    = wruntime.TrapLinMemOutOfBounds
	TrapLinMemCouldNotExtend = wruntime.TrapLinMemCouldNotExtend
	TrapIndirectOutOfBounds  = wruntime.TrapIndirectOutOfBounds
	TrapIndirectWrongSig     = wruntime.TrapIndirectWrongSig
	TrapLinkedMemNotLinked   = wruntime.TrapLinkedMemNotLinked
	TrapLinkedMemOutOfBounds = wruntime.TrapLinkedMemOutOfBounds
	TrapDivZero              = wruntime.TrapDivZero
	TrapDivOverflow          = wruntime.TrapDivOverflow
	TrapTruncOverflow        = wruntime.TrapTruncOverflow
	TrapInterrupted          = wruntime.TrapInterrupted
	TrapStackFenceBreached   = wruntime.TrapStackFenceBreached
	TrapCalledFnNotLinked    = wruntime.TrapCalledFnNotLinked
	TrapTableOutOfBounds     = wruntime.TrapTableOutOfBounds
)
