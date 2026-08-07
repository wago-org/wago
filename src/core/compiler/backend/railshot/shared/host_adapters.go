package shared

import (
	"fmt"

	"github.com/wago-org/wago/src/core/compiler/wasm"
)

// HostAdapterSet returns the local functions that can be entered through the
// wrapper ABI. Valid WebAssembly requires every ref.func target to be declared
// by an export or initializer, so exports, start, element payloads, global
// initializers, and table initializers are the complete externally addressable
// set. Direct same-module calls use InternalEntry and do not need wrappers.
func HostAdapterSet(m *wasm.Module) ([]bool, error) {
	set := make([]bool, len(m.Code))
	imports := m.ImportedFuncCount()
	mark := func(index uint32) {
		local := int(index) - imports
		if local >= 0 && local < len(set) {
			set[local] = true
		}
	}
	for _, ex := range m.Exports {
		if ex.Index.Kind == wasm.ExternFunc {
			mark(ex.Index.Index)
		}
	}
	if m.Start != nil {
		mark(uint32(*m.Start))
	}
	for i := range m.Tables {
		if m.Tables[i].Init != nil {
			if err := walkConstRefFuncs(*m.Tables[i].Init, mark); err != nil {
				return nil, fmt.Errorf("table %d initializer: %w", i, err)
			}
		}
	}
	for i := range m.Globals {
		if err := walkConstRefFuncs(m.Globals[i].Init, mark); err != nil {
			return nil, fmt.Errorf("global %d initializer: %w", i, err)
		}
	}
	for i := range m.Elements {
		elem := &m.Elements[i]
		if elem.Kind.Kind == wasm.ElemFuncs {
			for _, index := range elem.Kind.Funcs {
				mark(uint32(index))
			}
			continue
		}
		for j := range elem.Kind.Exprs {
			if err := walkConstRefFuncs(elem.Kind.Exprs[j], mark); err != nil {
				return nil, fmt.Errorf("element %d expression %d: %w", i, j, err)
			}
		}
	}
	// The low-level backend historically exposes function zero as the invocation
	// seam for synthetic modules with no Wasm-visible entry at all. Preserve that
	// single test/tool entry while still eliding every direct-only callee.
	if len(set) != 0 {
		any := false
		for _, adapter := range set {
			any = any || adapter
		}
		if !any {
			set[0] = true
		}
	}
	return set, nil
}

func walkConstRefFuncs(expr wasm.Expr, mark func(uint32)) error {
	if len(expr.BodyBytes) == 0 {
		for i := range expr.Instrs {
			if expr.Instrs[i].Kind == wasm.InstrRefFunc {
				mark(expr.Instrs[i].Index)
			}
		}
		return nil
	}
	r := wasm.NewReader(expr.BodyBytes)
	for r.BytesLeft() != 0 {
		op, err := r.Byte()
		if err != nil {
			return err
		}
		if op == 0x0b {
			if r.BytesLeft() != 0 {
				return fmt.Errorf("trailing bytes after end")
			}
			return nil
		}
		imm, err := wasm.ClassifyInstructionImmediate(r, op)
		if err != nil {
			return err
		}
		if imm.Kind == wasm.InstrRefFunc {
			mark(imm.Index)
		}
	}
	return fmt.Errorf("missing end")
}
