//go:build (linux && (amd64 || arm64)) || (darwin && arm64)

package runtime

import "fmt"

// TrapCode is Wago's stable native-to-Go trap ABI. Codes 0 through 14 mirror
// vb::TrapCode (src/core/common/TrapCode.hpp); Wago-specific codes are appended.
type TrapCode uint32

const (
	TrapNone                 TrapCode = 0
	TrapUnreachable          TrapCode = 1
	TrapBuiltin              TrapCode = 2
	TrapLinMemOutOfBounds    TrapCode = 3
	TrapLinMemCouldNotExtend TrapCode = 4
	TrapIndirectOutOfBounds  TrapCode = 5
	TrapIndirectWrongSig     TrapCode = 6
	TrapLinkedMemNotLinked   TrapCode = 7
	TrapLinkedMemOutOfBounds TrapCode = 8
	TrapDivZero              TrapCode = 9
	TrapDivOverflow          TrapCode = 10
	TrapTruncOverflow        TrapCode = 11
	TrapInterrupted          TrapCode = 12
	TrapStackFenceBreached   TrapCode = 13
	TrapCalledFnNotLinked    TrapCode = 14
	TrapTableOutOfBounds     TrapCode = 15
)

var trapMessages = map[TrapCode]string{
	TrapNone:                 "no trap",
	TrapUnreachable:          "unreachable instruction executed",
	TrapBuiltin:              "builtin.trap executed",
	TrapLinMemOutOfBounds:    "linear memory access out of bounds",
	TrapLinMemCouldNotExtend: "could not extend linear memory",
	TrapIndirectOutOfBounds:  "indirect call out of bounds (table)",
	TrapIndirectWrongSig:     "indirect call with wrong signature",
	TrapLinkedMemNotLinked:   "linked memory not linked",
	TrapLinkedMemOutOfBounds: "linked memory access out of bounds",
	TrapDivZero:              "integer division by zero",
	TrapDivOverflow:          "integer division overflow",
	TrapTruncOverflow:        "float-to-int conversion overflow",
	TrapInterrupted:          "runtime interrupt requested",
	TrapStackFenceBreached:   "stack fence breached",
	TrapCalledFnNotLinked:    "called function not linked",
	TrapTableOutOfBounds:     "table access out of bounds",
}

func (c TrapCode) String() string {
	if m, ok := trapMessages[c]; ok {
		return m
	}
	return fmt.Sprintf("trap(%d)", uint32(c))
}

// TrapError is returned by Engine.Call when native code set a non-zero trap.
type TrapError struct{ Code TrapCode }

func (e *TrapError) Error() string { return "wasm trap: " + e.Code.String() }
