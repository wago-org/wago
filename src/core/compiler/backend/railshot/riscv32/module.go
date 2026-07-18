package riscv32

import (
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
	return shared.CompileEmbeddedI32Module(m, opts, "riscv32", 8, 40, []byte{0x13, 0x00, 0x00, 0x00}, compileModuleBeachhead)
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
