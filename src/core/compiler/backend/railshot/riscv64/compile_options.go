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
	MemoryPressureAt                int
	MemoryPressure                  func()
	GCTypeSubtypingRefTest          bool
	GCStructHelpers, GCArrayHelpers bool
	GCFrameRoots                    *shared.GCModuleFrameRootPlan
	Codegen                         codegen.Options
	Stats                           *ModuleStats
	CollectInlineReport             bool
	CustomInstructions              map[uint32]railcore.CustomInstruction
	ForceFallback                   bool
	production                      bool
}

// The initial public envelope rejects profiling until finalized source/unwind
// attribution exists. These shared-shaped metadata types keep that contract
// type-correct without fabricating an empty successful profile.
type CodegenStats struct {
	FuncIdx, CodeBytes, FrameBytes int
	Name                           string
	Spills, Reloads, BoundsChecks  int
	Calls, Peephole                map[string]int
	FinalizerFallback              string
	GCCodeBytes                    shared.GCNativeCodeBytes
}

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
