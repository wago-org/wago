//go:build linux && riscv64 && wago_rv64_experimental && !tinygo && !wago_target_tinygo && !wago_precompiled

package wago

import (
	"fmt"

	railshot "github.com/wago-org/wago/src/core/compiler/backend/railshot/riscv64"
	"github.com/wago-org/wago/src/core/compiler/wasm"
	encoder "github.com/wago-org/wago/src/core/encoder/riscv64"
	coreruntime "github.com/wago-org/wago/src/core/runtime"
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
	if m == nil {
		return railshot.CompileModuleWith(m, opts)
	}
	maxImportArity := coreruntime.MaxHostArity
	vectorProfile, err := railshot.UsesSWAR(m)
	if err != nil {
		return nil, err
	}
	if vectorProfile {
		maxImportArity = 128
	}
	for i, im := range m.Imports {
		if im.Type.Kind == wasm.ExternFunc {
			ft, ok := m.ImportFuncType(i)
			if !ok || len(ft.Params) > maxImportArity || len(ft.Results) > maxImportArity {
				return nil, fmt.Errorf("%w: public function import signature arity", railshot.ErrUnsupported)
			}
		}
	}
	// Callable public artifacts use the cooperative ABI unconditionally. The
	// new RV64 architecture identity denotes this framed, polling RV64G ABI.
	opts.Interruptible = true
	opts.ForeignTableCalls = true
	opts.InvocationContext = true
	return railshot.CompileModuleWith(m, opts)
}

func railshotGCNativeCodeTelemetry(stats *railshotModuleStats) GCNativeCodeTelemetry {
	var out GCNativeCodeTelemetry
	if stats != nil {
		for _, function := range stats.Funcs {
			if function != nil {
				out.TotalBytes += uint64(function.GCCodeBytes.Total)
			}
		}
	}
	return out
}

// Deferred events retain pre-publication gates.
func railshotHostIndirectThunk(uint32) []byte { return nil }
func railshotHostIndirectSyncThunk(index uint32, params, results int) []byte {
	return railshot.HostIndirectSyncThunk(index, params, results)
}
func railshotHostIndirectOwnedSyncThunk(index uint32, params, results int) []byte {
	return railshot.HostIndirectOwnedSyncThunk(index, params, results)
}

func railshotAdmitSource(data []byte, m *wasm.Module) error {
	if err := railshot.AdmitProductionSource(data); err != nil {
		return err
	}
	return railshot.AdmitPublicInitializers(m)
}

func railshotI31CoreSource(data []byte) bool { return railshot.AdmitI31CoreSource(data) }

func railshotGCRefEqSource(data []byte) bool { return railshot.AdmitGCRefEqSource(data) }

func railshotGCStructGetSource(data []byte) bool { return railshot.AdmitGCStructGetSource(data) }

func railshotGCArrayLocalSource(data []byte) bool { return railshot.AdmitGCArrayLocalSource(data) }

func railshotGCArrayUniformSource(data []byte) bool { return railshot.AdmitGCArrayUniformSource(data) }

func railshotGCArrayFixedLocalSource(data []byte) bool {
	return railshot.AdmitGCArrayFixedLocalSource(data)
}

func railshotGCArrayFixedOfficialSource(data []byte) bool {
	return railshot.AdmitGCArrayFixedOfficialSource(data)
}

func railshotGCArrayDefaultOfficialSource(data []byte) bool {
	return railshot.AdmitGCArrayDefaultOfficialSource(data)
}

func railshotGCArrayPackedDataSource(data []byte) bool {
	return railshot.AdmitGCArrayPackedDataSource(data)
}

func railshotGCArrayReferenceSource(data []byte) bool {
	return railshot.AdmitGCArrayReferenceSource(data)
}

func railshotGCArrayInitElemSource(data []byte) bool {
	return railshot.AdmitGCArrayInitElemSource(data)
}

func railshotConfigureI31Core(exactSource bool, opts *railshotCompileOptions) {
	opts.I31CoreSource = exactSource
}

func railshotConfigureGCRefEq(exactSource bool, opts *railshotCompileOptions) {
	opts.GCRefEqSource = exactSource
}

func railshotConfigureGCStructGet(exactSource bool, opts *railshotCompileOptions) {
	opts.GCStructGetSource = exactSource
}

func railshotConfigureGCArrayLocal(exactSource bool, opts *railshotCompileOptions) {
	opts.GCArrayLocalSource = exactSource
}

func railshotConfigureGCArrayUniform(exactSource bool, opts *railshotCompileOptions) {
	opts.GCArrayUniformSource = exactSource
}

func railshotConfigureGCArrayFixedLocal(exactSource bool, opts *railshotCompileOptions) {
	opts.GCArrayFixedLocalSource = exactSource
}

func railshotConfigureGCArrayFixedOfficial(exactSource bool, opts *railshotCompileOptions) {
	opts.GCArrayFixedOfficialSource = exactSource
}

func railshotConfigureGCArrayDefaultOfficial(exactSource bool, opts *railshotCompileOptions) {
	opts.GCArrayDefaultOfficialSource = exactSource
}

func railshotConfigureGCArrayPackedData(exactSource bool, opts *railshotCompileOptions) {
	opts.GCArrayPackedDataSource = exactSource
}

func railshotConfigureGCArrayReference(exactSource bool, opts *railshotCompileOptions) {
	opts.GCArrayReferenceSource = exactSource
}

func railshotConfigureGCArrayInitElem(exactSource bool, opts *railshotCompileOptions) {
	opts.GCArrayInitElemSource = exactSource
}
