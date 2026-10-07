//go:build linux && !tinygo

package riscv64

import (
	"github.com/wago-org/wago/internal/jitprofile"
	railcore "github.com/wago-org/wago/src/core/compiler/backend/railshot"
	"github.com/wago-org/wago/src/core/compiler/backend/railshot/shared"
	"github.com/wago-org/wago/src/core/compiler/codegen"
	"github.com/wago-org/wago/src/core/compiler/optimization"
)

type ImportBinding = shared.ImportBinding
type KnobInfo = optimization.Info
type OptimizationSnapshot = optimization.Snapshot

// CompileOptions preserves the common compiler contract. Unsupported semantic
// options fail before staging an image; conservative optimization hints may use
// the fully checked serial path. ForceFallback remains a genuine RV64 diagnostic.
type CompileOptions struct {
	SourceMaps, UnwindMaps, Profile bool
	BitCountFeatures                uint8
	Optimizations                   map[string]bool
	OptimizationSnapshot            OptimizationSnapshot
	OptimizationDeltas              map[string]bool
	CompactNative                   bool
	Workers                         int
	DeferCodeMapping                bool
	ElideBoundsChecks               bool
	NoBoundsFacts                   bool
	ImportBindings                  []ImportBinding
	SyncHostCalls                   bool
	SyncHostSlots                   int
	Interruptible                   bool
	// ForeignTableCalls requires a runtime that authenticates and retains all
	// canonical table targets through entry, host resume and cold decoration.
	// Entries use a synchronous root control frame and full signature transport.
	ForeignTableCalls               bool
	MemoryPressureAt                int
	MemoryPressure                  func()
	GCTypeSubtypingRefTest          bool
	GCStructHelpers, GCArrayHelpers bool
	GCFrameRoots                    *shared.GCModuleFrameRootPlan
	Codegen                         codegen.Options
	// Stats collects CompileModuleWith's finalized local-function sizes with
	// wago_codegenstats or wago_profile. Success replaces the sink; failure
	// leaves it unchanged. The isolated private pilot does not collect stats.
	Stats               *ModuleStats
	CollectInlineReport bool
	CustomInstructions  map[uint32]railcore.CustomInstruction
	ForceFallback       bool
	production          bool
	publicStarts        bool
	hostImports         bool
}

// CodegenStats reports finalized local-function CodeBytes and FrameBytes.
// FuncIdx is the local definition index, excluding imported functions. CodeBytes
// includes the framed wrapper and cold traps, but not inter-function alignment
// or separately instantiated import adapters. GCCodeBytes.Total is the same
// measured size. All other fields are currently unavailable and remain zero/nil;
// in particular, zero BoundsChecks/Spills/Reloads is not a measured count.
// Profiling, source maps and unwind attribution remain unsupported.
type CodegenStats struct {
	FuncIdx, CodeBytes, FrameBytes int
	Name                           string
	Spills, Reloads, BoundsChecks  int
	Calls, Peephole                map[string]int
	FinalizerFallback              string
	GCCodeBytes                    shared.GCNativeCodeBytes
}

// ModuleStats owns a bounded snapshot of this compile's finalized functions.
// Optional profiling fields remain empty; reusing a sink drops old attribution.
type ModuleStats struct {
	CodeSites      []shared.NativeCodeSite
	UnwindRanges   []jitprofile.UnwindRange
	SourceRanges   []shared.NativeSourceRange
	SourceFrames   []shared.NativeInlineFrame
	ProfileRegions []jitprofile.Region
	Funcs          []*CodegenStats
}

var optimizationBindings = optimization.NewBindings("riscv64")

func OptKnobs() []KnobInfo { return optimizationBindings.Infos() }
func OptKnobSnapshot() ([]KnobInfo, OptimizationSnapshot) {
	return optimizationBindings.Snapshot()
}
func CurrentOptKnobSnapshot() OptimizationSnapshot { return optimizationBindings.CurrentSnapshot() }
func SetOptKnob(name string, on bool) bool         { return optimizationBindings.Set(name, on) }
