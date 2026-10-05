//go:build ((linux || darwin || windows) && (amd64 || arm64)) || (linux && riscv64 && !tinygo && !wago_target_tinygo)

package runtime

import (
	"encoding/binary"
	"fmt"
	"strings"

	"github.com/wago-org/wago/src/core/runtime/abi"
)

// TrapCode mirrors vb::TrapCode (src/core/common/TrapCode.hpp).
type TrapCode uint32

const (
	TrapNone                 TrapCode = abi.TrapNone
	TrapUnreachable          TrapCode = abi.TrapUnreachable
	TrapBuiltin              TrapCode = abi.TrapBuiltin
	TrapLinMemOutOfBounds    TrapCode = abi.TrapLinMemOutOfBounds
	TrapLinMemCouldNotExtend TrapCode = abi.TrapLinMemCouldNotExtend
	TrapIndirectOutOfBounds  TrapCode = abi.TrapIndirectOutOfBounds
	TrapIndirectWrongSig     TrapCode = abi.TrapIndirectWrongSig
	TrapLinkedMemNotLinked   TrapCode = abi.TrapLinkedMemNotLinked
	TrapLinkedMemOutOfBounds TrapCode = abi.TrapLinkedMemOutOfBounds
	TrapDivZero              TrapCode = abi.TrapDivZero
	TrapDivOverflow          TrapCode = abi.TrapDivOverflow
	TrapTruncOverflow        TrapCode = abi.TrapTruncOverflow
	TrapInterrupted          TrapCode = abi.TrapInterrupted
	TrapStackFenceBreached   TrapCode = abi.TrapStackFenceBreached
	TrapCalledFnNotLinked    TrapCode = abi.TrapCalledFnNotLinked
	TrapUnsupportedTailCall  TrapCode = abi.TrapUnsupportedTailCall
	TrapNullReference        TrapCode = abi.TrapNullReference
	TrapUnhandledException   TrapCode = abi.TrapUnhandledException
	TrapCastFailure          TrapCode = abi.TrapCastFailure
	TrapTableOutOfBounds     TrapCode = abi.TrapTableOutOfBounds
	TrapAtomicUnaligned      TrapCode = abi.TrapAtomicUnaligned
	TrapExpectedSharedMemory TrapCode = abi.TrapExpectedSharedMemory
	TrapHostEventOverflow    TrapCode = abi.TrapHostEventOverflow
)

var trapMessages = [...]string{
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
	TrapUnsupportedTailCall:  "tail call target requires an unsupported context switch",
	TrapNullReference:        "null reference",
	TrapUnhandledException:   "unhandled WebAssembly exception",
	TrapCastFailure:          "cast failure",
	TrapTableOutOfBounds:     "table access out of bounds",
	TrapAtomicUnaligned:      "unaligned atomic memory access",
	TrapExpectedSharedMemory: "expected shared memory",
	TrapHostEventOverflow:    "deferred host event log capacity exceeded",
}

func (c TrapCode) String() string {
	if uint32(c) < uint32(len(trapMessages)) && trapMessages[c] != "" {
		return trapMessages[c]
	}
	return fmt.Sprintf("trap(%d)", uint32(c))
}

// TrapFrame identifies a logical Wasm function and bytecode offset. Name is
// filled by the public runtime after it resolves the module's name metadata.
type TrapFrame struct {
	FunctionIndex     uint32 `json:"functionIndex"`
	FunctionName      string `json:"functionName,omitempty"`
	ProgramCounter    uint32 `json:"programCounter,omitempty"`
	HasProgramCounter bool   `json:"hasProgramCounter"`
}

func (frame TrapFrame) String() string {
	name := frame.FunctionName
	if name == "" {
		name = fmt.Sprintf("func[%d]", frame.FunctionIndex)
	}
	if frame.HasProgramCounter {
		return fmt.Sprintf("%s (func[%d], wasm pc 0x%x)", name, frame.FunctionIndex, frame.ProgramCounter)
	}
	return fmt.Sprintf("%s (func[%d])", name, frame.FunctionIndex)
}

// TrapError is returned by Engine.Call when native code set a non-zero trap.
// Frames is ordered from the trap site outward. The current handler-jump ABI
// records the precise innermost frame without adding work to successful calls.
type TrapError struct {
	Code   TrapCode    `json:"code"`
	Frames []TrapFrame `json:"frames,omitempty"`
}

func (e *TrapError) Error() string {
	if e == nil {
		return "wasm trap"
	}
	var message strings.Builder
	message.WriteString("wasm trap: ")
	message.WriteString(e.Code.String())
	for _, frame := range e.Frames {
		message.WriteString("\n    at ")
		message.WriteString(frame.String())
	}
	return message.String()
}

func trapErrorFromBuffer(code TrapCode, trap []byte) *TrapError {
	err := &TrapError{Code: code}
	if len(trap) < TrapBufferBytes {
		return err
	}
	location := binary.LittleEndian.Uint64(trap[16:24])
	encodedFunction := uint32(location)
	if encodedFunction == 0 {
		return err
	}
	pc := uint32(location >> 32)
	err.Frames = []TrapFrame{{
		FunctionIndex:     encodedFunction - 1,
		ProgramCounter:    pc,
		HasProgramCounter: pc != ^uint32(0),
	}}
	return err
}
