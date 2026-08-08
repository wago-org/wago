//go:build arm64

package wago

import (
	railshot "github.com/wago-org/wago/src/core/compiler/backend/railshot/arm64"
	"github.com/wago-org/wago/src/core/compiler/wasm"
	encoderarm64 "github.com/wago-org/wago/src/core/encoder/arm64"
)

type railshotImportBinding = railshot.ImportBinding
type railshotCompileOptions = railshot.CompileOptions
type railshotCompiledModule = encoderarm64.CompiledModule
type railshotKnobInfo = railshot.KnobInfo
type railshotModuleStats = railshot.ModuleStats

func railshotOptKnobs() []railshotKnobInfo         { return railshot.OptKnobs() }
func railshotSetOptKnob(name string, on bool) bool { return railshot.SetOptKnob(name, on) }

func railshotCompileModuleWith(m *wasm.Module, opts railshotCompileOptions) (*railshotCompiledModule, error) {
	return railshot.CompileModuleWith(m, opts)
}

func railshotGCNativeCodeTelemetry(stats *railshotModuleStats) GCNativeCodeTelemetry {
	var out GCNativeCodeTelemetry
	if stats == nil {
		return out
	}
	for _, function := range stats.Funcs {
		if function == nil {
			continue
		}
		b := function.GCCodeBytes
		out.Add(GCNativeCodeTelemetry{
			TotalBytes:            uint64(b.Total),
			AllocationBytes:       uint64(b.Allocation),
			HandleResolutionBytes: uint64(b.HandleResolution),
			TypeCastBytes:         uint64(b.TypeCast),
			NullCheckBytes:        uint64(b.NullCheck),
			BoundsCheckBytes:      uint64(b.BoundsCheck),
			BarrierBytes:          uint64(b.Barrier),
			SpillReloadBytes:      uint64(b.SpillReload),
			HelperCallBytes:       uint64(b.HelperCall),
			SharedStubBytes:       uint64(b.SharedStub),
			TrapStubBytes:         uint64(b.TrapStub),
			RootMapBytes:          uint64(b.RootMap),
		})
	}
	return out
}

func railshotHostIndirectThunk(importIdx uint32) []byte {
	return railshot.HostIndirectThunk(importIdx)
}

func railshotHostIndirectSyncThunk(importIdx uint32, paramSlots, resultSlots int) []byte {
	return railshot.HostIndirectSyncThunk(importIdx, paramSlots, resultSlots)
}

func railshotHostIndirectOwnedSyncThunk(importIdx uint32, paramSlots, resultSlots int) []byte {
	return railshot.HostIndirectOwnedSyncThunk(importIdx, paramSlots, resultSlots)
}
