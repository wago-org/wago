package riscv32

import (
	"fmt"

	"github.com/wago-org/wago/src/core/compiler/backend/railshot/shared"
	"github.com/wago-org/wago/src/core/compiler/wasm"
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
	return shared.CompileEmbeddedModule(m, opts, "riscv32", 40, []byte{0x13, 0x00, 0x00, 0x00}, compileModuleFunction)
}

func compileModuleFunction(ft *wasm.CompType, locals []wasm.LocalRun, body []byte) ([]byte, error) {
	if homogeneousFunction(ft, locals, wasm.I32, true) {
		return compileModuleBeachhead(len(ft.Params), body)
	}
	if homogeneousFunction(ft, locals, wasm.I64, false) {
		return CompileI64Function(len(ft.Params), body)
	}
	if homogeneousFunction(ft, locals, wasm.F64, false) {
		return CompileF64BitFunction(len(ft.Params), body)
	}
	if homogeneousFunction(ft, locals, wasm.V128, false) {
		return CompileV128Function(len(ft.Params), body)
	}
	return nil, fmt.Errorf("mixed-width function signature or locals are not yet supported")
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
