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
	cm.Code = a.B
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
