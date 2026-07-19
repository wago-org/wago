package riscv32

import (
	"fmt"

	"github.com/wago-org/wago/src/core/compiler/backend/railshot/shared"
	"github.com/wago-org/wago/src/core/compiler/wasm"
	rv "github.com/wago-org/wago/src/core/encoder/riscv32"
	"github.com/wago-org/wago/src/core/runtime/embedded32"
)

// ModuleCompileOptions controls deterministic embedded code-arena preflight.
type ModuleCompileOptions = shared.EmbeddedModuleOptions

// CompiledModule is one module-wide RV32IM image with local-function metadata.
type CompiledModule = shared.EmbeddedModule

func CompileModule(m *wasm.Module) (*CompiledModule, error) {
	return CompileModuleWith(m, ModuleCompileOptions{})
}

// CompileModuleWith compiles the strict currently admitted i32/control subset
// into one 16-byte-aligned RV32IM image. Unsupported module state and target-
// incompatible signatures are rejected before any image is returned.
func CompileModuleWith(m *wasm.Module, opts ModuleCompileOptions) (*CompiledModule, error) {
	if m == nil {
		return nil, fmt.Errorf("riscv32: nil module")
	}
	relocs := make([][]callReloc, len(m.Code))
	cm, err := shared.CompileEmbeddedModule(m, opts, "riscv32", 40, []byte{0x13, 0x00, 0x00, 0x00}, func(funcIdx int, ft *wasm.CompType, locals []wasm.LocalRun, body []byte) ([]byte, error) {
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
	a := rv.Asm{B: cm.Code}
	for i := range relocs {
		for _, reloc := range relocs[i] {
			if reloc.target < 0 || reloc.target >= len(cm.Entry) || !a.PatchFarJump(cm.Entry[i]+reloc.at, cm.Entry[reloc.target]) {
				return nil, fmt.Errorf("riscv32: call relocation from function %d to %d out of range", i, reloc.target)
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
		overflowSlots := int32(0)
		if meta.ParamSlots > 8 {
			overflowSlots = int32(meta.ParamSlots - 8)
		}
		if meta.ResultSlots > 8 && int32(meta.ResultSlots-8) > overflowSlots {
			overflowSlots = int32(meta.ResultSlots - 8)
		}
		stateBase := overflowSlots * 4
		frame := (stateBase + 12 + 15) &^ 15
		if frame > 2048 {
			return nil, fmt.Errorf("riscv32: exported function %d entry frame exceeds displacement", i)
		}
		meta.CallOffset, meta.HasCallEntry = uint32(a.Len()), true
		a.Addi(rv.SP, rv.SP, -frame)
		a.Sw(rv.X23, rv.SP, stateBase+4)
		a.Sw(rv.RA, rv.SP, stateBase+8)
		a.MovReg(rv.T0, rv.A0)
		a.Lw(rv.X23, rv.T0, embedded32.CallABIContextOffset)
		a.Lw(rv.T1, rv.T0, embedded32.CallABIResultsOffset)
		a.Sw(rv.T1, rv.SP, stateBase)
		a.Lw(rv.T0, rv.T0, embedded32.CallABIParametersOffset)
		for slot := uint16(0); slot < meta.ParamSlots; slot++ {
			if slot < 8 {
				a.Lw(rv.A0+rv.Reg(slot), rv.T0, int32(slot)*4)
			} else {
				a.Lw(rv.T1, rv.T0, int32(slot)*4)
				a.Sw(rv.T1, rv.SP, int32(slot-8)*4)
			}
		}
		call := a.FarCall(branchScratch)
		a.Lw(rv.T0, rv.X23, embedded32.ContextTrapCellOffset)
		a.Lw(rv.T1, rv.T0, 0)
		success := a.FarBcond(rv.T1, rv.Zero, rv.CondEQ, branchScratch)
		a.MovReg(rv.A0, rv.T1)
		done := a.FarJump(rv.Zero, branchScratch)
		successTarget := a.Len()
		a.Lw(rv.T0, rv.SP, stateBase)
		for slot := uint16(0); slot < meta.ResultSlots; slot++ {
			if slot < 8 {
				a.Sw(rv.A0+rv.Reg(slot), rv.T0, int32(slot)*4)
			} else {
				a.Lw(rv.T1, rv.SP, int32(slot-8)*4)
				a.Sw(rv.T1, rv.T0, int32(slot)*4)
			}
		}
		a.MovImm32(rv.A0, 0)
		doneTarget := a.Len()
		a.Lw(rv.X23, rv.SP, stateBase+4)
		a.Lw(rv.RA, rv.SP, stateBase+8)
		a.Addi(rv.SP, rv.SP, frame)
		a.Ret()
		if !a.PatchFarJump(call, cm.Entry[i]) || !a.PatchFarBranch(success, successTarget) || !a.PatchFarJump(done, doneTarget) {
			return nil, fmt.Errorf("riscv32: exported function %d entry relocation out of range", i)
		}
	}
	if cm.Start != nil {
		for len(a.B)%16 != 0 {
			a.Nop()
		}
		startEntry := a.Len()
		a.Addi(rv.SP, rv.SP, -16)
		a.Sw(rv.X23, rv.SP, 0)
		a.Sw(rv.RA, rv.SP, 4)
		a.MovReg(rv.X23, rv.A0)
		call := a.FarCall(branchScratch)
		a.Lw(rv.T0, rv.X23, embedded32.ContextTrapCellOffset)
		a.Lw(rv.A0, rv.T0, 0)
		a.Lw(rv.X23, rv.SP, 0)
		a.Lw(rv.RA, rv.SP, 4)
		a.Addi(rv.SP, rv.SP, 16)
		a.Ret()
		startLocal := *cm.Start - cm.ImportedFunctions
		if int(startLocal) >= len(cm.Entry) || !a.PatchFarJump(call, cm.Entry[startLocal]) {
			return nil, fmt.Errorf("riscv32: start relocation out of range")
		}
		cm.StartEntry = &startEntry
	}
	cm.Code = a.B
	if (opts.EnforceCapacity || opts.CodeCapacity != 0) && uint64(len(cm.Code)) > uint64(opts.CodeCapacity) {
		return nil, fmt.Errorf("riscv32: code arena capacity %d is below emitted image %d", opts.CodeCapacity, len(cm.Code))
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
