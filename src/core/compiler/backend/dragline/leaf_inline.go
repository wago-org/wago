package dragline

import (
	"github.com/wago-org/wago/src/core/compiler/backend/dragline/railssa"
	"github.com/wago-org/wago/src/core/compiler/wasm"
)

const (
	leafInlineMaxBody   = 384
	leafInlineMaxGrowth = 4096
	leafInlineMaxSites  = 32
	leafInlineMaxLocals = 256
)

type leafInlineOffset struct {
	rewritten uint32
	original  uint32
}

type leafInlineCandidate struct {
	stack *railssa.StackFunc
	body  []byte
	base  uint32
	bound bool
}

// boundedLeafInlineModule expands small direct leaf calls before CFG/SSA
// construction. Scalar memory effects and traps remain in their original order;
// allocating/runtime helpers, reference values, and further calls are excluded.
// Each caller has explicit byte, site, and local growth limits. Bindings can be
// reused across sites because an eligible callee cannot call or reenter, but its
// declared locals must be reset on EVERY invocation, including loop iterations.
func boundedLeafInlineModule(m *wasm.Module, localIndex int, stack *railssa.StackFunc) (*wasm.Module, []leafInlineOffset) {
	if m == nil || stack == nil || localIndex < 0 || localIndex >= len(m.Code) {
		return nil, nil
	}
	var candidates map[uint32]*leafInlineCandidate
	for _, in := range stack.Instrs {
		if in.Kind != wasm.InstrCall || in.U32() < stack.ImportedFuncs {
			continue
		}
		if candidates == nil {
			candidates = make(map[uint32]*leafInlineCandidate)
		}
		if _, seen := candidates[in.U32()]; seen {
			continue
		}
		candidates[in.U32()] = leafInlineCallee(m, int(in.U32()-stack.ImportedFuncs))
	}
	if len(candidates) == 0 {
		return nil, nil
	}
	body := m.Code[localIndex].BodyBytes
	r := wasm.NewReader(body)
	classifier := wasm.NewModuleInstructionClassifier(m, true)
	var immediate wasm.InstructionImmediate
	out := make([]byte, 0, len(body))
	offsets := make([]leafInlineOffset, 0, len(stack.Instrs))
	locals := make([]wasm.ValType, 0)
	sites := 0
	for r.HasNext() {
		start := r.Offset()
		opcode, err := r.Byte()
		if err != nil || classifier.ClassifyInto(r, opcode, &immediate) != nil {
			return nil, nil
		}
		offsets = append(offsets, leafInlineOffset{uint32(len(out)), uint32(start)})
		candidate := candidates[immediate.Index]
		if immediate.Kind == wasm.InstrCall && candidate != nil && sites < leafInlineMaxSites {
			base := candidate.base
			if !candidate.bound {
				base = uint32(len(stack.Locals) + len(locals))
			}
			// Build separately so a rejected expansion leaves no partial binding.
			expansion := appendLeafInlineCall(nil, candidate.stack, base)
			expansion, err = appendRecursiveInlineBody(expansion, m, candidate.body, base)
			addedLocals := 0
			if !candidate.bound {
				addedLocals = len(candidate.stack.Locals)
			}
			if err == nil && len(locals)+addedLocals <= leafInlineMaxLocals &&
				len(out)+len(expansion)-r.Offset() <= leafInlineMaxGrowth {
				out = append(out, expansion...)
				sites++
				if !candidate.bound {
					locals = append(locals, candidate.stack.Locals...)
					candidate.base, candidate.bound = base, true
				}
				continue
			}
		}
		out = append(out, body[start:r.Offset()]...)
	}
	if sites == 0 {
		return nil, nil
	}
	clone := *m
	clone.Code = append([]wasm.Func(nil), m.Code...)
	fn := clone.Code[localIndex]
	fn.BodyBytes, fn.Body, fn.LocalDeclBytes = out, wasm.Expr{}, 0
	fn.Locals.Runs = append([]wasm.LocalRun(nil), fn.Locals.Runs...)
	for _, typ := range locals {
		n := len(fn.Locals.Runs)
		if n != 0 && fn.Locals.Runs[n-1].Type == typ {
			fn.Locals.Runs[n-1].Count++
		} else {
			fn.Locals.Runs = append(fn.Locals.Runs, wasm.LocalRun{Count: 1, Type: typ})
		}
	}
	clone.Code[localIndex] = fn
	clone.BranchHints = nil
	return &clone, offsets
}

func leafInlineCallee(m *wasm.Module, localIndex int) *leafInlineCandidate {
	if localIndex < 0 || localIndex >= len(m.Code) {
		return nil
	}
	body := m.Code[localIndex].BodyBytes
	if len(body) == 0 || len(body) > leafInlineMaxBody {
		return nil
	}
	stack, err := railssa.BuildStackFunc(m, localIndex)
	if err != nil || len(stack.Params) > 8 || len(stack.Results) > 1 || len(stack.Locals) > 40 {
		return nil
	}
	for _, typ := range stack.Locals {
		if !leafInlineScalar(typ) {
			return nil
		}
	}
	for _, typ := range stack.Results {
		if !leafInlineScalar(typ) {
			return nil
		}
	}
	r := wasm.NewReader(body)
	classifier := wasm.NewModuleInstructionClassifier(m, true)
	var immediate wasm.InstructionImmediate
	for r.HasNext() {
		opcode, err := r.Byte()
		if err != nil || classifier.ClassifyInto(r, opcode, &immediate) != nil {
			return nil
		}
		kind := immediate.Kind
		if opcode == 0x05 || opcode == 0x0b || railssa.ContextFreeTrapFreeKind(kind) {
			continue
		}
		if kind == wasm.InstrGlobalGet || kind == wasm.InstrGlobalSet {
			if int(immediate.Index) >= len(stack.Globals) || !leafInlineScalar(stack.Globals[immediate.Index]) {
				return nil
			}
			continue
		}
		if kind >= wasm.InstrI32Load && kind <= wasm.InstrMemorySize ||
			kind >= wasm.InstrI32DivS && kind <= wasm.InstrI32RemU ||
			kind >= wasm.InstrI64DivS && kind <= wasm.InstrI64RemU ||
			kind >= wasm.InstrI32TruncF32S && kind <= wasm.InstrI32TruncF64U ||
			kind >= wasm.InstrI64TruncF32S && kind <= wasm.InstrI64TruncF64U ||
			kind == wasm.InstrUnreachable {
			continue
		}
		return nil
	}
	return &leafInlineCandidate{stack: stack, body: body}
}

func leafInlineScalar(typ wasm.ValType) bool {
	return typ == wasm.I32 || typ == wasm.I64 || typ == wasm.F32 || typ == wasm.F64
}

func appendLeafInlineCall(out []byte, stack *railssa.StackFunc, base uint32) []byte {
	for param := len(stack.Params) - 1; param >= 0; param-- {
		out = append(out, 0x21)
		out = appendRecursiveInlineU32(out, base+uint32(param))
	}
	for local := len(stack.Params); local < len(stack.Locals); local++ {
		switch stack.Locals[local] {
		case wasm.I32:
			out = append(out, 0x41, 0)
		case wasm.I64:
			out = append(out, 0x42, 0)
		case wasm.F32:
			out = append(out, 0x43, 0, 0, 0, 0)
		case wasm.F64:
			out = append(out, 0x44, 0, 0, 0, 0, 0, 0, 0, 0)
		}
		out = append(out, 0x21)
		out = appendRecursiveInlineU32(out, base+uint32(local))
	}
	blockType := byte(0x40)
	if len(stack.Results) != 0 {
		switch stack.Results[0] {
		case wasm.I32:
			blockType = 0x7f
		case wasm.I64:
			blockType = 0x7e
		case wasm.F32:
			blockType = 0x7d
		case wasm.F64:
			blockType = 0x7c
		}
	}
	return append(out, 0x02, blockType)
}

// Copied instructions are attributed to the original caller's call site.
// The span table also preserves offsets of unexpanded caller instructions,
// even when StackFunc construction combines adjacent bytecode instructions.
func restoreLeafInlineOffsets(stack *railssa.StackFunc, offsets []leafInlineOffset) {
	span := 0
	for i := range stack.Instrs {
		for span+1 < len(offsets) && offsets[span+1].rewritten <= stack.Instrs[i].Offset {
			span++
		}
		stack.Instrs[i].Offset = offsets[span].original
	}
}
