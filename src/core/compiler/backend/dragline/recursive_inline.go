package dragline

import (
	"fmt"

	"github.com/wago-org/wago/src/core/compiler/backend/dragline/railssa"
	"github.com/wago-org/wago/src/core/compiler/wasm"
)

// boundedRecursiveInlineModule expands one level of a small self-recursive
// function before SSA construction. The remaining calls in the copied body stay
// recursive, so native growth is bounded by the original number of call sites.
//
// This deliberately follows the conservative Railshot policy: a single local
// integer function, no declared locals, loops, memory, globals, indirect calls,
// or trapping operations. Those constraints make the synthetic local binding
// and function-label block exact without importing a general Wasm inliner into
// the backend.
func boundedRecursiveInlineModule(m *wasm.Module, localIndex int, stack *railssa.StackFunc) *wasm.Module {
	if m == nil || stack == nil || localIndex != 0 || len(m.Code) != 1 || m.ImportedFuncCount() != 0 ||
		len(m.Code[0].BodyBytes) == 0 || len(m.Code[0].BodyBytes) > 64 || len(m.Code[0].Locals.Runs) != 0 ||
		len(stack.Params) > 4 || len(stack.Results) > 1 || len(stack.Locals) != len(stack.Params) ||
		!railssa.ContextFreeTrapFree(stack, true) {
		return nil
	}
	for _, typ := range stack.Params {
		if typ != wasm.I32 && typ != wasm.I64 {
			return nil
		}
	}
	for _, typ := range stack.Results {
		if typ != wasm.I32 && typ != wasm.I64 {
			return nil
		}
	}
	calls := 0
	for _, instruction := range stack.Instrs {
		switch instruction.Kind {
		case wasm.InstrLoop:
			return nil
		case wasm.InstrCall:
			if instruction.U32() != stack.FunctionIndex {
				return nil
			}
			calls++
		case wasm.InstrCallIndirect, wasm.InstrReturnCall, wasm.InstrReturnCallIndirect, wasm.InstrCallRef, wasm.InstrReturnCallRef:
			return nil
		}
	}
	if calls == 0 || calls > 2 {
		return nil
	}

	body, err := expandSelfCalls(m, m.Code[0].BodyBytes, uint32(len(stack.Params)), stack.Results)
	if err != nil {
		return nil
	}
	clone := *m
	clone.Code = append([]wasm.Func(nil), m.Code...)
	fn := clone.Code[0]
	fn.BodyBytes = body
	fn.Body = wasm.Expr{}
	fn.LocalDeclBytes = 0
	fn.Locals.Runs = append([]wasm.LocalRun(nil), fn.Locals.Runs...)
	for _, typ := range stack.Params {
		n := len(fn.Locals.Runs)
		if n != 0 && fn.Locals.Runs[n-1].Type == typ {
			fn.Locals.Runs[n-1].Count++
		} else {
			fn.Locals.Runs = append(fn.Locals.Runs, wasm.LocalRun{Count: 1, Type: typ})
		}
	}
	clone.Code[0] = fn
	// Source byte offsets no longer match after expansion. Branch hints are only
	// advisory, so dropping them is safer than attaching one to the wrong branch.
	clone.BranchHints = nil
	return &clone
}

func expandSelfCalls(m *wasm.Module, body []byte, localBase uint32, results []wasm.ValType) ([]byte, error) {
	classifier := wasm.NewModuleInstructionClassifier(m, true)
	r := wasm.NewReader(body)
	out := make([]byte, 0, len(body)*3)
	var immediate wasm.InstructionImmediate
	for r.HasNext() {
		start := r.Offset()
		opcode, err := r.Byte()
		if err != nil || classifier.ClassifyInto(r, opcode, &immediate) != nil {
			return nil, fmt.Errorf("decode recursive inline source at %d", start)
		}
		if immediate.Kind != wasm.InstrCall || immediate.Index != 0 {
			out = append(out, body[start:r.Offset()]...)
			continue
		}
		for param := int(localBase) - 1; param >= 0; param-- {
			out = append(out, 0x21)
			out = appendRecursiveInlineU32(out, localBase+uint32(param))
		}
		out = append(out, 0x02, recursiveInlineBlockType(results))
		out, err = appendRecursiveInlineBody(out, m, body, localBase)
		if err != nil {
			return nil, err
		}
	}
	return out, nil
}

func appendRecursiveInlineBody(out []byte, m *wasm.Module, body []byte, localBase uint32) ([]byte, error) {
	classifier := wasm.NewModuleInstructionClassifier(m, true)
	r := wasm.NewReader(body)
	depth := uint32(0)
	var immediate wasm.InstructionImmediate
	for r.HasNext() {
		start := r.Offset()
		opcode, err := r.Byte()
		if err != nil || classifier.ClassifyInto(r, opcode, &immediate) != nil {
			return nil, fmt.Errorf("decode recursive inline body at %d", start)
		}
		switch immediate.Kind {
		case wasm.InstrLocalGet, wasm.InstrLocalSet, wasm.InstrLocalTee:
			out = append(out, opcode)
			out = appendRecursiveInlineU32(out, localBase+immediate.Index)
		case wasm.InstrReturn:
			out = append(out, 0x0c)
			out = appendRecursiveInlineU32(out, depth)
		default:
			if opcode == 0x0b {
				if depth == 0 {
					out = append(out, opcode)
					return out, nil
				}
				depth--
			}
			out = append(out, body[start:r.Offset()]...)
			if immediate.Kind == wasm.InstrBlock || immediate.Kind == wasm.InstrLoop || immediate.Kind == wasm.InstrIf {
				depth++
			}
		}
	}
	return nil, fmt.Errorf("recursive inline body has no function end")
}

func recursiveInlineBlockType(results []wasm.ValType) byte {
	if len(results) == 0 {
		return 0x40
	}
	if results[0] == wasm.I32 {
		return 0x7f
	}
	return 0x7e
}

func appendRecursiveInlineU32(dst []byte, value uint32) []byte {
	for value >= 0x80 {
		dst = append(dst, byte(value)|0x80)
		value >>= 7
	}
	return append(dst, byte(value))
}
