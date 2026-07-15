//go:build (linux || darwin || windows) && (amd64 || arm64)

package runtime

import (
	"encoding/binary"
	"fmt"
	"strings"
)

// TrapCode mirrors vb::TrapCode (src/core/common/TrapCode.hpp).
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
	TrapUnsupportedTailCall  TrapCode = 15
	TrapNullReference        TrapCode = 16
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
	TrapUnsupportedTailCall:  "tail call target requires an unsupported context switch",
	TrapNullReference:        "null reference",
}

func (c TrapCode) String() string {
	if m, ok := trapMessages[c]; ok {
		return m
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
