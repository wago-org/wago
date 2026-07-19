package arm32

import (
	"fmt"

	"github.com/wago-org/wago/src/core/compiler/backend/railshot/shared"
	"github.com/wago-org/wago/src/core/compiler/wasm"
	a32 "github.com/wago-org/wago/src/core/encoder/arm32"
	"github.com/wago-org/wago/src/core/runtime/embedded32"
)

// ModuleCompileOptions controls deterministic embedded code-arena preflight.
type ModuleCompileOptions = shared.EmbeddedModuleOptions

// CompiledModule is one module-wide Thumb-2 image with local-function metadata.
type CompiledModule = shared.EmbeddedModule

func CompileModule(m *wasm.Module) (*CompiledModule, error) {
	return CompileModuleWith(m, ModuleCompileOptions{})
}

// CompileModuleWith compiles the strict currently admitted i32/control subset
// into one 16-byte-aligned Thumb-2 image. Unsupported module state and target-
// incompatible signatures are rejected before any image is returned.
func CompileModuleWith(m *wasm.Module, opts ModuleCompileOptions) (*CompiledModule, error) {
	if m == nil {
		return nil, fmt.Errorf("arm32: nil module")
	}
	relocs := make([][]callReloc, len(m.Code))
	cm, err := shared.CompileEmbeddedModule(m, opts, "arm32", 32, []byte{0x00, 0xbf}, func(funcIdx int, ft *wasm.CompType, locals []wasm.LocalRun, body []byte) ([]byte, error) {
		if homogeneousFunction(ft, locals, wasm.I32, true) {
			code, r, err := compileModuleBeachhead(m, funcIdx, len(ft.Params), body)
			relocs[funcIdx] = r
			return code, err
		}
		code, r, err := compileModuleFunction(m, ft, locals, body)
		relocs[funcIdx] = r
		return code, err
	})
	if err != nil {
		return nil, err
	}
	a := a32.Asm{B: cm.Code}
	for i := range relocs {
		for _, reloc := range relocs[i] {
			if reloc.target < 0 || reloc.target >= len(cm.Entry) || !a.PatchCall(cm.Entry[i]+reloc.at, cm.Entry[reloc.target]) {
				return nil, fmt.Errorf("arm32: call relocation from function %d to %d out of range", i, reloc.target)
			}
		}
	}
	exported := make([]bool, len(cm.Functions))
	for i := range m.Exports {
		export := &m.Exports[i]
		if export.Index.Kind == wasm.ExternFunc && export.Index.Index >= cm.ImportedFunctions && uint64(export.Index.Index-cm.ImportedFunctions) < uint64(len(exported)) {
			exported[export.Index.Index-cm.ImportedFunctions] = true
		}
	}
	for i, needed := range exported {
		if !needed {
			continue
		}
		for len(a.B)%16 != 0 {
			a.Nop()
		}
		meta := &cm.Functions[i]
		overflowSlots := uint32(0)
		if meta.ParamSlots > 4 {
			overflowSlots = uint32(meta.ParamSlots - 4)
		}
		if meta.ResultSlots > 4 && uint32(meta.ResultSlots-4) > overflowSlots {
			overflowSlots = uint32(meta.ResultSlots - 4)
		}
		stateBase := overflowSlots * 4
		frame := (stateBase + 12 + 15) &^ 15
		if frame > 4096 {
			return nil, fmt.Errorf("arm32: exported function %d entry frame exceeds displacement", i)
		}
		meta.CallOffset, meta.HasCallEntry = uint32(a.Len()), true
		a.MovImm32(a32.R12, frame)
		a.Sub(a32.SP, a32.SP, a32.R12)
		a.Str(a32.R11, a32.SP, uint16(stateBase+4))
		a.Str(a32.LR, a32.SP, uint16(stateBase+8))
		a.MovReg(a32.R12, a32.R0)
		a.Ldr(a32.R11, a32.R12, embedded32.CallABIContextOffset)
		a.Ldr(a32.R1, a32.R12, embedded32.CallABIResultsOffset)
		a.Str(a32.R1, a32.SP, uint16(stateBase))
		a.Ldr(a32.R12, a32.R12, embedded32.CallABIParametersOffset)
		for slot := uint16(0); slot < meta.ParamSlots; slot++ {
			if slot < 4 {
				a.Ldr(a32.R0+a32.Reg(slot), a32.R12, slot*4)
			} else {
				a.Ldr(a32.LR, a32.R12, slot*4)
				a.Str(a32.LR, a32.SP, (slot-4)*4)
			}
		}
		call := a.Call()
		a.Ldr(a32.R12, a32.R11, embedded32.ContextTrapCellOffset)
		a.Ldr(a32.R12, a32.R12, 0)
		a.MovImm32(a32.LR, 0)
		a.Cmp(a32.R12, a32.LR)
		success := a.FarBcond(a32.CondEQ)
		a.MovReg(a32.R0, a32.R12)
		done := a.Branch()
		successTarget := a.Len()
		a.Ldr(a32.R12, a32.SP, uint16(stateBase))
		for slot := uint16(0); slot < meta.ResultSlots; slot++ {
			if slot < 4 {
				a.Str(a32.R0+a32.Reg(slot), a32.R12, slot*4)
			} else {
				a.Ldr(a32.R1, a32.SP, (slot-4)*4)
				a.Str(a32.R1, a32.R12, slot*4)
			}
		}
		a.MovImm32(a32.R0, 0)
		doneTarget := a.Len()
		a.Ldr(a32.R11, a32.SP, uint16(stateBase+4))
		a.Ldr(a32.LR, a32.SP, uint16(stateBase+8))
		a.MovImm32(a32.R12, frame)
		a.Add(a32.SP, a32.SP, a32.R12)
		a.Ret()
		a.Align4()
		if !a.PatchCall(call, cm.Entry[i]) || !a.PatchFarBranch(success, successTarget) || !a.PatchBranch(done, doneTarget) {
			return nil, fmt.Errorf("arm32: exported function %d entry relocation out of range", i)
		}
	}
	if cm.Start != nil {
		for len(a.B)%16 != 0 {
			a.Nop()
		}
		startEntry := a.Len()
		a.MovImm32(a32.R12, 8)
		a.Sub(a32.SP, a32.SP, a32.R12)
		a.Str(a32.R11, a32.SP, 0)
		a.Str(a32.LR, a32.SP, 4)
		a.MovReg(a32.R11, a32.R0)
		call := a.Call()
		a.Ldr(a32.R1, a32.R11, embedded32.ContextTrapCellOffset)
		a.Ldr(a32.R0, a32.R1, 0)
		a.Ldr(a32.R11, a32.SP, 0)
		a.Ldr(a32.LR, a32.SP, 4)
		a.MovImm32(a32.R12, 8)
		a.Add(a32.SP, a32.SP, a32.R12)
		a.Ret()
		a.Align4()
		startLocal := *cm.Start - cm.ImportedFunctions
		if int(startLocal) >= len(cm.Entry) || !a.PatchCall(call, cm.Entry[startLocal]) {
			return nil, fmt.Errorf("arm32: start relocation out of range")
		}
		cm.StartEntry = &startEntry
	}
	cm.Code = a.B
	if (opts.EnforceCapacity || opts.CodeCapacity != 0) && uint64(len(cm.Code)) > uint64(opts.CodeCapacity) {
		return nil, fmt.Errorf("arm32: code arena capacity %d is below emitted image %d", opts.CodeCapacity, len(cm.Code))
	}
	if uint32(len(cm.Code)) > cm.RequiredCodeBytes {
		cm.RequiredCodeBytes = uint32(len(cm.Code))
	}
	return cm, nil
}

func compileModuleFunction(m *wasm.Module, ft *wasm.CompType, locals []wasm.LocalRun, body []byte) ([]byte, []callReloc, error) {
	if homogeneousFunction(ft, locals, wasm.I32, true) {
		return nil, nil, fmt.Errorf("internal i32 module compiler dispatch")
	}
	return compileMixedModuleFunction(m, ft, locals, body)
}

func usesMixedModuleCompiler(ft *wasm.CompType, locals []wasm.LocalRun) bool {
	return !homogeneousFunction(ft, locals, wasm.I32, true)
}

func homogeneousFunction(ft *wasm.CompType, locals []wasm.LocalRun, typ wasm.ValType, allowVoid bool) bool {
	for _, p := range ft.Params {
		if p != typ {
			return false
		}
	}
	if len(ft.Results) == 0 {
		if !allowVoid {
			return false
		}
	} else if len(ft.Results) != 1 || ft.Results[0] != typ {
		return false
	}
	for _, run := range locals {
		if run.Type != typ {
			return false
		}
	}
	return true
}

// CompileModuleToArena preflights against the remaining arena capacity, then
// compiles and publishes the complete image transactionally.
func CompileModuleToArena(m *wasm.Module, arena *embedded32.CodeArena, publish embedded32.CodePublisher) (*shared.PublishedEmbeddedModule, error) {
	if arena == nil {
		return nil, embedded32.ErrInvalidArena
	}
	cm, err := CompileModuleWith(m, ModuleCompileOptions{CodeCapacity: arena.Capacity() - arena.Used(), EnforceCapacity: true})
	if err != nil {
		return nil, err
	}
	return shared.PublishEmbeddedModule(arena, cm, publish)
}
