//go:build linux && riscv64 && wago_rv64_experimental && !tinygo && !wago_target_tinygo && !wago_precompiled

package wago

import (
	"fmt"

	railshot "github.com/wago-org/wago/src/core/compiler/backend/railshot/riscv64"
	"github.com/wago-org/wago/src/core/compiler/wasm"
	encoder "github.com/wago-org/wago/src/core/encoder/riscv64"
)

type railshotImportBinding = railshot.ImportBinding
type railshotCompileOptions = railshot.CompileOptions
type railshotCompiledModule = encoder.CompiledModule
type railshotKnobInfo = railshot.KnobInfo
type railshotOptimizationSnapshot = railshot.OptimizationSnapshot
type railshotModuleStats = railshot.ModuleStats

func railshotOptKnobs() []railshotKnobInfo { return railshot.OptKnobs() }
func railshotOptKnobSnapshot() ([]railshotKnobInfo, railshotOptimizationSnapshot) {
	return railshot.OptKnobSnapshot()
}
func railshotCurrentOptKnobSnapshot() railshotOptimizationSnapshot {
	return railshot.CurrentOptKnobSnapshot()
}
func railshotSetOptKnob(name string, on bool) bool { return railshot.SetOptKnob(name, on) }

func railshotCompileModuleWith(m *wasm.Module, opts railshotCompileOptions) (*railshotCompiledModule, error) {
	if len(opts.ImportBindings) != 0 {
		return nil, fmt.Errorf("%w: public function-import admission is pending", railshot.ErrUnsupported)
	}
	// Callable public artifacts use the cooperative ABI unconditionally. The
	// new RV64 architecture identity denotes this framed, polling RV64G ABI.
	opts.Interruptible = true
	return railshot.CompileModuleWith(m, opts)
}

func railshotGCNativeCodeTelemetry(*railshotModuleStats) GCNativeCodeTelemetry {
	return GCNativeCodeTelemetry{}
}

// Unsupported import metadata is rejected before instantiation can request
// these thunks. An empty blob also cannot be published as executable code.
func railshotHostIndirectThunk(uint32) []byte                    { return nil }
func railshotHostIndirectSyncThunk(uint32, int, int) []byte      { return nil }
func railshotHostIndirectOwnedSyncThunk(uint32, int, int) []byte { return nil }

func railshotAdmitSource(data []byte) error { return railshot.AdmitProductionSource(data) }
