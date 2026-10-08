//go:build linux && !tinygo

package riscv64

import (
	"github.com/wago-org/wago/internal/jitprofile"
	railcore "github.com/wago-org/wago/src/core/compiler/backend/railshot"
	"github.com/wago-org/wago/src/core/compiler/backend/railshot/shared"
	"github.com/wago-org/wago/src/core/compiler/codegen"
	"github.com/wago-org/wago/src/core/compiler/optimization"
	"github.com/wago-org/wago/src/core/compiler/wasm"
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
	// InvocationContext requires the RV64 public 136-byte context and Engine/
	// activation-owned views. Legacy private contexts must leave it false.
	// NativeTailCalls requires the invocation-bound proper-tail factory runtime.
	// It is an internal qualification contract until every target family is ready.
	NativeTailCalls bool
	// NativeReferenceCalls permits ordinary call_ref and reference control under
	// retained canonical owners. Call transport and control witnesses are distinct.
	NativeReferenceCalls            bool
	InvocationContext               bool
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
	// Public TailCall remains gated until the whole instruction/target family.
	tailModule     *wasm.Module
	localTailCalls bool
	// Source production and private decoded adapters collect symbolic import sites.
	importTailCalls  bool
	dynamicTailCalls bool // declared-type indirect/reference source qualification
	// A source typed-tail requirement must have a qualified emitted plan, even
	// when all typed-tail instructions occur in unreachable code.
	referenceTailInstructions    bool
	referenceCallInstructions    bool
	referenceControlInstructions bool
	tailPlans                    *[]tailImportPlan
	tailSiteBase                 uint32
}

// CodegenStats reports finalized local-function CodeBytes and FrameBytes.
// FuncIdx is the local definition index, excluding imported functions. CodeBytes
// includes the framed wrapper and cold traps, but not inter-function alignment
// or separately instantiated import adapters. GCCodeBytes.Total is the same
// measured size. All other fields are currently unavailable and remain zero/nil;
// in particular, zero BoundsChecks/Spills/Reloads is not a measured count.
// Profile/SourceMaps add sparse finalized metadata in wago_profile builds;
// operation counters, CodeSites, inline reports and unwind remain unavailable.
type CodegenStats struct {
	FuncIdx, CodeBytes, FrameBytes int
	Name                           string
	Spills, Reloads, BoundsChecks  int
	Calls, Peephole                map[string]int
	FinalizerFallback              string
	GCCodeBytes                    shared.GCNativeCodeBytes
}

// ModuleStats owns a bounded snapshot of this compile's finalized functions.
// Profile/SourceMaps are opt-in; reusing a sink replaces all old attribution.
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
