package wago

import (
	"fmt"

	corecompiler "github.com/wago-org/wago/src/core/compiler"
	"github.com/wago-org/wago/src/core/compiler/backend/dragline"
	compilerprofile "github.com/wago-org/wago/src/core/compiler/profile"
)

// CompilerEngine identifies one complete compiler pipeline.
type CompilerEngine = corecompiler.Engine

// CompilerProfile is the backend-neutral original-Wasm profile format.
type CompilerProfile = compilerprofile.Module
type CompilerProfileSite = compilerprofile.Site
type CompilerProfileEdgeCount = compilerprofile.EdgeCount
type CompilerProfileTargetCount = compilerprofile.TargetCount
type CompilerProfileTargetHistogram = compilerprofile.TargetHistogram
type CompilerProfilePhase = compilerprofile.Phase
type CompilerTierPolicy = compilerprofile.TierPolicy
type CompilerTierPlan = compilerprofile.TierPlan
type CompilerNativeSelectionPolicy = compilerprofile.SelectionPolicy
type CompilerNativeFunctionOpportunity = compilerprofile.FunctionOpportunity

const (
	CompilerProfileStartup = compilerprofile.PhaseStartup
	CompilerProfileSteady  = compilerprofile.PhaseSteady
	CompilerProfileRare    = compilerprofile.PhaseRare
)

// HostImport identifies an imported host function by its exact Wasm module and
// field names.
type HostImport = corecompiler.HostImport
type HostHeapMask = corecompiler.HostHeapMask
type HostEffectFlags = corecompiler.HostEffectFlags
type HostEffectContract = corecompiler.HostEffectContract

const (
	HostHeapLinearMemory = corecompiler.HostHeapLinearMemory
	HostHeapTable        = corecompiler.HostHeapTable
	HostHeapGlobal       = corecompiler.HostHeapGlobal
	HostHeapGCHeader     = corecompiler.HostHeapGCHeader
	HostHeapGCStruct     = corecompiler.HostHeapGCStruct
	HostHeapGCArray      = corecompiler.HostHeapGCArray
	HostHeapImportState  = corecompiler.HostHeapImportState
	HostHeapRuntimeState = corecompiler.HostHeapRuntimeState
	HostHeapUnknown      = corecompiler.HostHeapUnknown

	HostEffectMayGrow     = corecompiler.HostEffectMayGrow
	HostEffectMayAllocate = corecompiler.HostEffectMayAllocate
	HostEffectMayCollect  = corecompiler.HostEffectMayCollect
	HostEffectMayReenter  = corecompiler.HostEffectMayReenter
	HostEffectMayThrow    = corecompiler.HostEffectMayThrow
)

type FunctionArtifactCache = corecompiler.FunctionArtifactCache
type FunctionCacheStats = corecompiler.FunctionCacheStats

// NewFunctionArtifactCache creates a bounded process-shared Dragline cache.
func NewFunctionArtifactCache(maxBytes uint64) *FunctionArtifactCache {
	return corecompiler.NewFunctionArtifactCache(maxBytes)
}

// PlanCompilerTier deterministically selects bounded original-Wasm hot
// functions and direct-call clusters from an immutable profile.
func PlanCompilerTier(profile CompilerProfile, importedFunctions uint32, directCalls [][]uint32, policy CompilerTierPolicy) (CompilerTierPlan, error) {
	return compilerprofile.PlanTier(profile, importedFunctions, directCalls, policy)
}

// PlanCompilerNativeClones selects hot native-opportunity roots and their
// complete bounded local direct-call closures for CompileNativeClone.
func PlanCompilerNativeClones(profile CompilerProfile, importedFunctions uint32, directCalls [][]uint32, opportunities []CompilerNativeFunctionOpportunity, policy CompilerNativeSelectionPolicy) (CompilerTierPlan, error) {
	return compilerprofile.PlanNativeClones(profile, importedFunctions, directCalls, opportunities, policy)
}

// OptimizationObjective selects the primary compiler quality tradeoff.
type OptimizationObjective = corecompiler.OptimizationObjective

const (
	OptimizeSpeed    = corecompiler.ObjectiveSpeed
	OptimizeBalanced = corecompiler.ObjectiveBalanced
	OptimizeSize     = corecompiler.ObjectiveSize
)

const (
	CompilerRailshot = corecompiler.EngineRailshot
	CompilerDragline = corecompiler.EngineDragline
)

// CompilerTargetMode controls how target-specific generated code may be.
type CompilerTargetMode = corecompiler.TargetMode

const (
	TargetCompatibility = corecompiler.TargetCompatibility
	TargetNative        = corecompiler.TargetNative
)

// CompilerFallback controls explicit whole-module fallback after Dragline
// reports an unsupported module. Per-function fallback is not supported.
type CompilerFallback uint8

const (
	CompilerFallbackNone CompilerFallback = iota
	CompilerFallbackRailshot
)

func (f CompilerFallback) String() string {
	switch f {
	case CompilerFallbackNone:
		return "none"
	case CompilerFallbackRailshot:
		return "railshot"
	default:
		return fmt.Sprintf("CompilerFallback(%d)", uint8(f))
	}
}

// DraglineUnsupportedError reports a valid module outside Dragline's current
// compilation subset. Dragline never silently delegates it to Railshot.
type DraglineUnsupportedError = dragline.UnsupportedError

// DraglineResourceLimitError reports valid Wasm that exceeds a bounded
// Dragline compiler structure. Explicit whole-module fallback may recover it.
type DraglineResourceLimitError = dragline.ResourceLimitError
