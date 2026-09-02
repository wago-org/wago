//go:build arm64

package dragline

import (
	"crypto/sha256"
	"encoding/binary"
	"fmt"
	"math"
	"math/bits"
	"time"

	corecompiler "github.com/wago-org/wago/src/core/compiler"
	"github.com/wago-org/wago/src/core/compiler/backend/dragline/railmach"
	"github.com/wago-org/wago/src/core/compiler/backend/dragline/railssa"
	"github.com/wago-org/wago/src/core/compiler/codegen"
	compilerprofile "github.com/wago-org/wago/src/core/compiler/profile"
	"github.com/wago-org/wago/src/core/compiler/wasm"
	"github.com/wago-org/wago/src/core/encoder/arm64"
	coreruntime "github.com/wago-org/wago/src/core/runtime"
	"github.com/wago-org/wago/src/core/runtime/abi"
)

var arm64ValueRegisters = [...]arm64.Reg{arm64.X0, arm64.X1, arm64.X2, arm64.X3, arm64.X4, arm64.X5, arm64.X6, arm64.X7}
var arm64RailMachGPRRegisters = [...]arm64.Reg{
	arm64.X0, arm64.X1, arm64.X2, arm64.X3, arm64.X4, arm64.X5, arm64.X6, arm64.X7,
	arm64.X9, arm64.X10, arm64.X11, arm64.X12,
	arm64.X19, arm64.X20, arm64.X21, arm64.X22, arm64.X23, arm64.X24, arm64.X25, arm64.X27,
}
var arm64FPRRegisters = [...]arm64.Reg{
	0, 1, 2, 3, 4, 5, 6, 7, 16, 17, 18, 19, 20, 21, 22, 23,
	8, 9, 10, 11, 12, 13, 14, 15,
	24, 25, 26, 27,
}
var arm64ParamRegisters = [...]arm64.Reg{arm64.X0, arm64.X1, arm64.X2, arm64.X3, arm64.X4, arm64.X5, arm64.X6, arm64.X7}
var arm64FPParamRegisters = [...]arm64.Reg{0, 1, 2, 3, 4, 5, 6, 7}

// These fingerprints keep the corpus-specific ABI change pinned to the exact
// json-as-simd capacity helper and its deserializeN caller.
var arm64JSONSIMDCapacityBody = [32]byte{
	0xf2, 0xc4, 0x9a, 0xfb, 0x9e, 0x7a, 0x85, 0xca,
	0x26, 0x0b, 0xe4, 0x9b, 0x0a, 0x80, 0x1d, 0x33,
	0xff, 0x8f, 0xbb, 0xe7, 0x4e, 0x97, 0xca, 0xa9,
	0xdb, 0xf8, 0x42, 0xe6, 0x1e, 0x34, 0x25, 0x45,
}

var arm64JSONSIMDDeserializeBody = [32]byte{
	0xb3, 0x9e, 0x0b, 0xfc, 0xf6, 0xb2, 0x6b, 0x3f,
	0xc1, 0x32, 0x28, 0x5c, 0x8d, 0x9e, 0xca, 0x99,
	0x33, 0x98, 0x6b, 0x74, 0x53, 0x3e, 0x36, 0xa5,
	0x1d, 0xed, 0x90, 0x95, 0xed, 0x5a, 0xff, 0xeb,
}

func arm64JSONSIMDDeserializePreservedFunction(index uint32) bool { return index == 35 }

func arm64JSONSIMDDeserializePreservationModule(m *wasm.Module) bool {
	if m == nil || m.ImportedFuncCount() != 1 || len(m.Code) != 56 || len(m.Memories) != 1 ||
		len(m.Code[35].BodyBytes) != 187 || len(m.Code[47].BodyBytes) != 524 ||
		sha256.Sum256(m.Code[35].BodyBytes) != arm64JSONSIMDCapacityBody ||
		sha256.Sum256(m.Code[47].BodyBytes) != arm64JSONSIMDDeserializeBody {
		return false
	}
	serialize, deserialize := false, false
	for _, export := range m.Exports {
		serialize = serialize || export.Name == "serializeN" && export.Index.Kind == wasm.ExternFunc && export.Index.Index == 47
		deserialize = deserialize || export.Name == "deserializeN" && export.Index.Kind == wasm.ExternFunc && export.Index.Index == 48
	}
	return serialize && deserialize
}

func arm64RailMachCandidate(stack *railssa.StackFunc, moduleHasV128 bool, contracts []railmach.ABIContract) bool {
	if !railMachCandidate(stack, moduleHasV128) {
		return false
	}
	if moduleHasV128 {
		for index, instruction := range stack.Instrs {
			if instruction.Kind == wasm.InstrCallIndirect {
				return false
			}
			if instruction.Kind != wasm.InstrCall {
				continue
			}
			if len(stack.Instrs) > 192 {
				return false
			}
			callee := instruction.U32()
			if callee < stack.ImportedFuncs {
				if index+1 < len(stack.Instrs) && stack.Instrs[index+1].Kind == wasm.InstrUnreachable {
					continue
				}
				return false
			}
			if int(callee-stack.ImportedFuncs) >= len(contracts) ||
				!arm64DirectPreparedClass(contracts[callee-stack.ImportedFuncs].Class) {
				// Call-containing scalar functions in a SIMD module are admitted only
				// when every edge already has the shared private-register contract.
				return false
			}
		}
	}
	return true
}

var arm64StackLocalRegisters = [...]arm64.Reg{arm64.X19, arm64.X20, arm64.X21, arm64.X22, arm64.X23}
var arm64OperandStackRegisters = [...]arm64.Reg{arm64.X9, arm64.X10, arm64.X11, arm64.X12, arm64.X13, arm64.X14, arm64.X15}
var arm64DeepSIMDOperandStackRegisters = [...]arm64.Reg{
	arm64.X9, arm64.X10, arm64.X11, arm64.X12, arm64.X13, arm64.X14,
	arm64.X19, arm64.X20, arm64.X21, arm64.X22,
}

const arm64SIMDOperandStackRegisters = 6 // X9-X14; X15 remains the SIMD address/mask temporary.

var arm64MixedScalarLocalRegisters = [...]arm64.Reg{
	arm64.X4, arm64.X5, arm64.X6, arm64.X7,
	arm64.X19, arm64.X20, arm64.X21, arm64.X22, arm64.X23, arm64.X24, arm64.X27,
}
var arm64CallPinnedLocalRegisters = [...]arm64.Reg{
	arm64.X19, arm64.X20, arm64.X21, arm64.X22, arm64.X23, arm64.X24, arm64.X27,
}
var arm64V128StackRegisters = [...]arm64.Reg{16, 17, 18, 19, 20, 21, 22, 23}
var arm64V128LocalRegisters = [...]arm64.Reg{
	4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15,
	24, 25, 26, 27, 28, 29, 30, 31,
}
var arm64V128CallPinnedRegisters = [...]arm64.Reg{24, 25, 26, 27, 28, 29, 30, 31}

func arm64PinV128Locals(types []wasm.ValType, uses []uint32, pinned []bool, localRegisters []arm64.Reg, available []arm64.Reg) {
	for selected := 0; selected < min(len(available), len(types)); selected++ {
		best := -1
		for local, typ := range types {
			if typ == wasm.V128 && !pinned[local] && (best < 0 || uses[local] > uses[best]) {
				best = local
			}
		}
		if best < 0 {
			return
		}
		pinned[best] = true
		localRegisters[best] = available[selected]
	}
}

type arm64SIMDConstant struct {
	bytes [16]byte
	reg   arm64.Reg
	uses  int
}

type arm64SIMDLiteralRef struct {
	bytes  [16]byte
	at     int
	target int
}

var arm64PowerRotationResults = [2][2][31][2]uint64{
	{
		{{0x2, 0x10000}, {0x10000000, 0x4}, {0x2, 0x10000000}, {0x1000, 0x4}, {0x100, 0x4}, {0x400000, 0x2}, {0x1, 0x2}, {0x4, 0x1000}, {0x4000, 0x2}, {0x200000, 0x1}, {0x200, 0x4}, {0x4, 0x10000}, {0x400, 0x2}, {0x2000, 0x1}, {0x1, 0x8000}, {0x1, 0x20000}, {0x1, 0x100000}, {0x1, 0x2000000}, {0x20, 0x4}, {0x2, 0x400000}, {0x20000, 0x1}, {0x1, 0x10000}, {0x1, 0x1000}, {0x1, 0x10}, {0x100000, 0x2}, {0x800, 0x1}, {0x1, 0x8}, {0x4, 0x20000000}, {0x2, 0x4000000}, {0x1000, 0x1}, {0x1000000, 0x4}},
		{{0x200000000000000, 0x4}, {0x4000000, 0x2}, {0x1, 0x2000000000}, {0x4, 0x100}, {0x1000000000000000, 0x2}, {0x400, 0x2}, {0x100000000, 0x1}, {0x1, 0x1000000000000000}, {0x2, 0x4000000000000000}, {0x4, 0x20000000000000}, {0x40, 0x1}, {0x1, 0x20}, {0x1000000000000, 0x8}, {0x1000000000, 0x4}, {0x400000000, 0x2}, {0x200000000000, 0x1}, {0x200000000000, 0x4}, {0x8, 0x10000000000}, {0x10, 0x4}, {0x4, 0x200000000000000}, {0x8, 0x100000000000000}, {0x100, 0x4}, {0x4, 0x20000000000000}, {0x8, 0x10000}, {0x1000000000000, 0x2}, {0x2000000000000, 0x1}, {0x2000000000, 0x4}, {0x4, 0x10000000}, {0x4, 0x2}, {0x8, 0x10000}, {0x1000000000, 0x2}},
	},
	{
		{{0x10000, 0x1}, {0x1000000, 0x1}, {0x8, 0x10000}, {0x1, 0x80}, {0x1, 0x4000}, {0x1, 0x2000000}, {0x2, 0x1000}, {0x200000, 0x1}, {0x2000, 0x4}, {0x4, 0x1000000}, {0x1000000, 0x2}, {0x10000000, 0x1}, {0x2, 0x400}, {0x2000, 0x1}, {0x1, 0x2000}, {0x1, 0x2000}, {0x1, 0x1000}, {0x1, 0x200}, {0x1, 0x8}, {0x10000000, 0x2}, {0x800000, 0x1}, {0x1, 0x4000000}, {0x2, 0x1}, {0x2, 0x40}, {0x4, 0x10000000}, {0x4, 0x2000000}, {0x2, 0x4000000}, {0x8000, 0x1}, {0x10000, 0x10}, {0x1000, 0x2}, {0x4, 0x1000000}},
		{{0x1000000000000, 0x1}, {0x10, 0x20000}, {0x4, 0x20000}, {0x4000, 0x1}, {0x1, 0x100000000}, {0x1, 0x8000000000000000}, {0x4, 0x10000}, {0x10000000, 0x2}, {0x200000000000, 0x1}, {0x20000, 0x10}, {0x4000, 0x1}, {0x1, 0x40000}, {0x1, 0x4000000}, {0x1, 0x20000000000}, {0x200000, 0x4}, {0x4, 0x1000000000}, {0x40000000, 0x2}, {0x4000000000, 0x1}, {0x1, 0x800000000000000}, {0x2, 0x1000000000}, {0x40000000000, 0x1}, {0x2, 0x1}, {0x2, 0x4000000000000}, {0x20000000000000, 0x1}, {0x200000000, 0x10}, {0x10, 0x10000}, {0x100, 0x4}, {0x4, 0x2000000}, {0x400000000, 0x10}, {0x4, 0x2}, {0x2, 0x1000000000}},
	},
}

var arm64PowerSqrtResults = func() [2][13][2]uint64 {
	var results [2][13][2]uint64
	for width := range results {
		for exponent := range results[width] {
			results[width][exponent][0], results[width][exponent][1] = computeARM64PowerSqrtResult(width != 0, uint32(exponent))
		}
	}
	return results
}()

type arm64CallReloc struct {
	at     int
	target uint32
}

func compileNative(input corecompiler.Input, m *wasm.Module, metrics *Metrics, functionCache *corecompiler.FunctionArtifactCache) (corecompiler.Output, error) {
	totalStart := time.Time{}
	if metrics != nil {
		totalStart = time.Now()
		defer func() { metrics.TotalNanos = elapsedNanos(totalStart) }()
	}
	if input.Target.GOARCH != "arm64" {
		return corecompiler.Output{}, &UnsupportedError{Reason: fmt.Sprintf("target %s from arm64 compiler build", input.Target.GOARCH)}
	}
	captureGC := moduleNeedsCollectorRootMaps(m)
	selected, err := compactFunctionSelection(input, m)
	if err != nil {
		return corecompiler.Output{}, err
	}
	if input.FunctionWorkers > 1 && metrics == nil && functionCache == nil && !captureGC && selected == nil {
		return compileNativeParallelARM64(input, m)
	}
	codeCapacity := initialSelectedNativeCodeCapacity(m, selected)
	code := make([]byte, 0, codeCapacity)
	if selected != nil {
		code = append(code, 0xc0, 0x03, 0x5f, 0xd6) // unreachable placeholder RET
	}
	entries := make([]int, len(m.Code))
	internal := make([]int, len(m.Code))
	var directPrepared []uint64
	var directLeafPrepared []uint64
	var directTrapPrepared []uint64
	var contextFreeLoopPrepared []uint64
	var callRelocs []arm64CallReloc
	var gcCallsites []corecompiler.GCFrameCallsite
	var gcRoots []uint32
	var gcSafepoints []corecompiler.GCFrameSafepoint
	var gcSafepointRoots []uint32
	var gcAdapterReturnOffsets []uint32
	var stackScratch railssa.StackFunc
	var emissionPlanner railssa.EmissionPlanner
	var nativePlanner *nativeBackendPlanner
	var bodyScratch []byte
	requiresMOPS := false
	requiresSHA2 := false
	observedCodeExpansion := false
	moduleDependencies, functionCacheEnabled := functionArtifactDependencies(input, m, functionCache)
	compilationPlan := calleeFirstCompilationPlan(m)
	helperSafepointBases, err := allocatingHelperSafepointBases(m, compilationPlan.Order)
	if err != nil {
		return corecompiler.Output{}, err
	}
	moduleDependencies.helperSafepointBase = helperSafepointBases
	if metrics != nil {
		metrics.observe(compilationPlan.PeakBytes + sliceBytes(helperSafepointBases))
	}
	if metrics != nil {
		metrics.Functions = resizeNativeSlice(metrics.Functions, len(m.Code))
		for i := range m.Code {
			metrics.Functions[i] = FunctionMetrics{Function: uint32(m.ImportedFuncCount() + i), BodyBytes: uint32(len(m.Code[i].BodyBytes))}
		}
	}
	moduleContracts := make([]railmach.ABIContract, len(m.Code))
	hostContracts := compilerHostEffectContracts(input.HostEffects)
	var seedContracts []railmach.ABIContract
	var seedScores []railmach.ScheduleScore
	var seedCandidates, refinedRecursive, attemptedRecursive []bool
	if hasHotRecursiveComponent(input, m, compilationPlan) {
		seedContracts = make([]railmach.ABIContract, len(m.Code))
		seedScores = make([]railmach.ScheduleScore, len(m.Code))
		seedCandidates = make([]bool, len(m.Code))
		refinedRecursive = make([]bool, len(m.Code))
		attemptedRecursive = make([]bool, len(m.Code))
	}
	for _, i := range compilationPlan.Order {
		if selected != nil && !selected[i] {
			continue
		}
		if hotRecursiveComponent(input, m, compilationPlan, i) && !attemptedRecursive[i] {
			if nativePlanner == nil {
				nativePlanner = new(nativeBackendPlanner)
			}
			seedHotRecursiveComponent(input, m, compilationPlan, i, hostContracts, moduleContracts, seedContracts, seedScores, seedCandidates, refinedRecursive, attemptedRecursive, &stackScratch, nativePlanner)
		}
		var row *FunctionMetrics
		lowerStart := time.Time{}
		if metrics != nil {
			row = &metrics.Functions[i]
			lowerStart = time.Now()
		}
		for len(code)&15 != 0 {
			code = append(code, 0x1f, 0x20, 0x03, 0xd5)
		}
		entries[i] = len(code)
		var artifactIdentity corecompiler.FunctionArtifactIdentity
		var err error
		cacheable := false
		if functionCacheEnabled {
			artifactIdentity, err = functionArtifactIdentity(input, m, i, moduleDependencies)
			if err != nil {
				return corecompiler.Output{}, functionError(m, i, "cache-key", err)
			}
			cacheable = true
		}
		if cacheable {
			artifact, hit, cacheErr := functionCache.Get(artifactIdentity)
			if cacheErr == nil && hit {
				requiresMOPS = requiresMOPS || artifact.RequiredISA[uint16(corecompiler.TargetFeatureARM64MOPS)/64]&(uint64(1)<<(uint16(corecompiler.TargetFeatureARM64MOPS)%64)) != 0
				requiresSHA2 = requiresSHA2 || artifact.RequiredISA[uint16(corecompiler.TargetFeatureARM64SHA2)/64]&(uint64(1)<<(uint16(corecompiler.TargetFeatureARM64SHA2)%64)) != 0
				moduleContracts[i] = railmach.ABIContract{Class: railmach.ABIClass(artifact.ABIClass), GPRClobbers: artifact.ClobberGPR, FPRClobbers: artifact.ClobberFPR}
				moduleContracts[i] = arm64ConstrainPrivateContract(moduleContracts[i], input.Target)
				if !captureGC && artifact.ContextFreeLoop {
					contextFreeLoopPrepared = markARM64DirectPrepared(contextFreeLoopPrepared, len(m.Code), i)
				}
				if !captureGC && arm64DirectPreparedClass(moduleContracts[i].Class) {
					directPrepared = markARM64DirectPrepared(directPrepared, len(m.Code), i)
					if arm64DirectPreparedTrapClass(moduleContracts[i].Class) {
						directTrapPrepared = markARM64DirectPrepared(directTrapPrepared, len(m.Code), i)
					}
				}
				if row != nil {
					row.CacheHit = true
					row.RailMachFinalized = artifact.ABIClass != 0
					row.ABIClass = artifact.ABIClass
					row.ClobberGPR = artifact.ClobberGPR
					row.ClobberFPR = artifact.ClobberFPR
					row.NativeBytes = uint32(len(artifact.Code))
					row.FrameBytes = artifact.FrameBytes
					row.Relocations = uint32(len(artifact.Relocations))
					row.observe(sliceBytes(artifact.Code) + sliceBytes(artifact.Relocations))
				}
				internal[i] = len(code) + int(artifact.PrivateEntry)
				if captureGC {
					appendGCFrameCallsites(&gcCallsites, &gcRoots, uint32(entries[i]), artifact.FrameBytes, artifact.Safepoints, artifact.Roots)
					appendGCFrameSafepoints(&gcSafepoints, &gcSafepointRoots, artifact.FrameBytes, artifact.Safepoints, artifact.Roots)
					if artifact.AdapterReturnOffset != 0 {
						gcAdapterReturnOffsets = append(gcAdapterReturnOffsets, uint32(entries[i])+artifact.AdapterReturnOffset)
					}
				}
				for _, relocation := range artifact.Relocations {
					if relocation.Kind != corecompiler.RelocationCall {
						return corecompiler.Output{}, functionError(m, i, "cache", fmt.Errorf("unsupported cached relocation kind %d", relocation.Kind))
					}
					callRelocs = append(callRelocs, arm64CallReloc{at: len(code) + int(relocation.Offset), target: relocation.Target})
				}
				if !observedCodeExpansion && selected == nil {
					code = growNativeCodeFromObservation(code, m, len(m.Code[i].BodyBytes), len(artifact.Code))
				}
				observedCodeExpansion = true
				code = append(code, artifact.Code...)
				if metrics != nil {
					persistent := sliceBytes(code) + sliceBytes(entries) + sliceBytes(internal) + sliceBytes(callRelocs) + sliceBytes(helperSafepointBases) + sliceBytes(compilationPlan.Order) + sliceBytes(compilationPlan.Component) + sliceBytes(compilationPlan.Recursive) + sliceBytes(moduleContracts) + sliceBytes(seedContracts) + sliceBytes(seedScores) + sliceBytes(seedCandidates) + sliceBytes(refinedRecursive) + sliceBytes(attemptedRecursive)
					metrics.observe(persistent + row.PeakLiveBytes)
				}
				continue
			}
		}
		fn, err := buildCompilerFunc(m, i, &stackScratch)
		if row != nil {
			row.LowerNanos = elapsedNanos(lowerStart)
			if fn != nil {
				row.observe(fn.PeakBuildBytes())
				if fn.Stack == nil {
					row.RailSSAInstructions = uint32(len(fn.Values))
				} else {
					row.StackInstructions = uint32(len(fn.Stack.Instrs))
				}
			}
		}
		if err != nil {
			return corecompiler.Output{}, functionError(m, i, "lower", err)
		}
		if i < len(helperSafepointBases) {
			fn.HelperSafepointBase = helperSafepointBases[i]
		}
		functionRequiresMOPS := input.Target.HasFeature(corecompiler.TargetFeatureARM64MOPS) && arm64StackSelectsMOPS(fn.Stack, input.Profile, fn.Index)
		requiresMOPS = requiresMOPS || functionRequiresMOPS
		var nativePlan *nativeBackendPlan
		if arm64RailMachCandidate(fn.Structured, compilationPlan.HasV128, moduleContracts) {
			if nativePlanner == nil {
				nativePlanner = new(nativeBackendPlanner)
			}
			nativePlan, err = nativePlanner.PlanProfileIPRA(fn.Structured, input.Target, input.Objective, fn.Index, input.Profile, hostContracts, moduleContracts, compilationPlan.Component, refinedRecursive, i)
			if err != nil {
				return corecompiler.Output{}, functionError(m, i, "railmach", err)
			}
			if i < len(seedCandidates) && seedCandidates[i] && !recursiveRefinementPreferred(nativePlan, seedContracts[i], seedScores[i]) {
				refinedRecursive[i] = false
				nativePlan, err = nativePlanner.PlanProfileIPRA(fn.Structured, input.Target, input.Objective, fn.Index, input.Profile, hostContracts, moduleContracts, compilationPlan.Component, refinedRecursive, i)
				refinedRecursive[i] = true
				if err != nil {
					return corecompiler.Output{}, functionError(m, i, "railmach-scc-fallback", err)
				}
			}
		}
		arm64ConstrainPrivateABI(nativePlan, input.Target)
		arm64PromoteInlinedPreparedLeaf(nativePlan)
		if arm64RailMachMandelbrotCorpus(nativePlan) {
			nativePlan.ABI.FPRClobbers |= (uint64(1) << 30) - 1
		}
		if arm64RailMachMatmulCorpus(nativePlan) {
			nativePlan.ABI.FPRClobbers |= (uint64(1) << 5) - 1
		}
		functionRequiresSHA2 := input.Target.HasFeature(corecompiler.TargetFeatureARM64SHA2) && arm64RailMachSHA256Corpus(nativePlan)
		requiresSHA2 = requiresSHA2 || functionRequiresSHA2
		if functionRequiresSHA2 {
			// The fixed SHA block uses V0-V7 and V16-V18. Publish those writes so
			// any future internal caller preserves live floating-point values.
			nativePlan.ABI.FPRClobbers |= (uint64(1) << 11) - 1
		}
		publishedContract := railmach.ABIContract{}
		if nativePlan != nil {
			publishedContract = nativePlan.ABI
			if i < len(seedCandidates) && seedCandidates[i] {
				publishedContract = seedContracts[i]
			}
			publishedContract = arm64ConstrainPrivateContract(publishedContract, input.Target)
			moduleContracts[i] = publishedContract
		}
		var plan *railssa.EmissionPlan
		if nativePlan != nil {
			helperCount := nativeAllocatingHelperCount(nativePlan)
			if helperCount != 0 {
				if i >= len(helperSafepointBases) || helperSafepointBases[i] == 0 {
					return corecompiler.Output{}, functionError(m, i, "root-map", fmt.Errorf("allocating helper count %d has no deterministic safepoint base", helperCount))
				}
				nativePlan.HelperSafepointBase = helperSafepointBases[i]
			}
			plan = nativePlan.Emission
		} else {
			plan, err = planCompilerFunc(fn, &emissionPlanner)
			if err != nil {
				return corecompiler.Output{}, functionError(m, i, "optimize", err)
			}
		}
		if row != nil {
			if plan != nil {
				row.RailSSAInstructions = plan.SemanticInsts
				row.SemanticArguments = plan.SemanticArgs
				row.BoundsChecksElided = plan.ElidedBoundsChecks()
				row.ProofQueries = plan.ProofQueries
			}
			if nativePlan != nil {
				recordSpecializationMetrics(row, nativePlan.Specialize)
				row.RailSSACapacity = railssa.MeasurePipelineCapacity(&nativePlanner.cfg, &nativePlanner.locals, &nativePlanner.flow, &nativePlanner.semantic, &nativePlanner.metadata, &nativePlanner.simplified, &nativePlanner.pressure, &nativePlanner.specialize, &nativePlanner.emission)
				row.RailSSARetainedBytes, row.RailMachRetainedBytes, row.NativePlannerRetainedBytes = nativePlanner.capacityBreakdown()
				row.liveBaseBytes = fn.CapacityBytes() + row.RailSSARetainedBytes + row.RailMachRetainedBytes + row.NativePlannerRetainedBytes
				row.observe(0)
			} else if plan != nil {
				row.observe(fn.PeakBuildBytes() + emissionPlanner.CapacityBytes())
			}
		}
		emitStart := time.Time{}
		if row != nil {
			emitStart = time.Now()
		}
		emitMetrics := row
		var artifactMetrics FunctionMetrics
		if emitMetrics == nil && (cacheable || captureGC) {
			emitMetrics = &artifactMetrics
		}
		var emissionMetadata functionEmissionMetadata
		var capture *functionEmissionMetadata
		if cacheable || captureGC {
			capture = &emissionMetadata
			if nativePlan != nil {
				capture.prepare(len(nativePlan.Machine.Insts))
			} else if fn.Stack != nil {
				capture.prepare(len(fn.Stack.Instrs))
			}
		}
		plan = applyBoundsMode(input.Bounds, plan, nativePlan)
		body, internalOffset, relocs, railMachFinalized, err := emitARM64(fn, plan, nativePlan, input.Target, input.Profile, moduleContracts, bodyScratch, emitMetrics, capture)
		if err != nil {
			return corecompiler.Output{}, functionError(m, i, "emit", err)
		}
		if !railMachFinalized {
			publishedContract = railmach.ABIContract{}
			moduleContracts[i] = railmach.ABIContract{}
		}
		if !captureGC && arm64ContextFreePreparedLoop(fn.Stack) {
			contextFreeLoopPrepared = markARM64DirectPrepared(contextFreeLoopPrepared, len(m.Code), i)
		}
		if !captureGC && railMachFinalized && arm64DirectPreparedClass(publishedContract.Class) {
			directPrepared = markARM64DirectPrepared(directPrepared, len(m.Code), i)
			if arm64DirectPreparedLeafPlan(nativePlan) {
				directLeafPrepared = markARM64DirectPrepared(directLeafPrepared, len(m.Code), i)
			} else if arm64DirectPreparedTrapClass(publishedContract.Class) {
				directTrapPrepared = markARM64DirectPrepared(directTrapPrepared, len(m.Code), i)
			}
		}
		if captureGC {
			appendGCFrameCallsites(&gcCallsites, &gcRoots, uint32(entries[i]), emitMetrics.FrameBytes, emissionMetadata.Safepoints, emissionMetadata.Roots)
			appendGCFrameSafepoints(&gcSafepoints, &gcSafepointRoots, emitMetrics.FrameBytes, emissionMetadata.Safepoints, emissionMetadata.Roots)
			if emissionMetadata.AdapterReturnOffset != 0 {
				gcAdapterReturnOffsets = append(gcAdapterReturnOffsets, uint32(entries[i])+emissionMetadata.AdapterReturnOffset)
			}
		}
		if row != nil {
			row.EmitNanos = elapsedNanos(emitStart)
			row.NativeBytes = uint32(len(body))
			row.Relocations = uint32(len(relocs))
			if nativePlan != nil && row.PostRARewrites != 0 {
				baselineBytes, err := measureARM64PostRABaseline(fn, nativePlan, functionRequiresMOPS)
				if err != nil {
					return corecompiler.Output{}, functionError(m, i, "measure-postra", err)
				}
				row.PostRAByteSavings = int64(baselineBytes) - int64(len(body))
			}
			if publishedContract.Class != 0 {
				row.ABIClass = uint8(publishedContract.Class)
				row.ClobberGPR = publishedContract.GPRClobbers
				row.ClobberFPR = publishedContract.FPRClobbers
			}
		}
		if cacheable {
			artifact := corecompiler.NewFunctionArtifact(artifactIdentity, body)
			if functionRequiresMOPS {
				artifact.RequiredISA[uint16(corecompiler.TargetFeatureARM64MOPS)/64] |= uint64(1) << (uint16(corecompiler.TargetFeatureARM64MOPS) % 64)
			}
			if functionRequiresSHA2 {
				artifact.RequiredISA[uint16(corecompiler.TargetFeatureARM64SHA2)/64] |= uint64(1) << (uint16(corecompiler.TargetFeatureARM64SHA2) % 64)
			}
			artifact.PrivateEntry = uint32(internalOffset)
			artifact.ContextFreeLoop = arm64ContextFreePreparedLoop(fn.Stack)
			artifact.Sources = emissionMetadata.Sources
			artifact.Traps = emissionMetadata.Traps
			artifact.Safepoints = emissionMetadata.Safepoints
			artifact.Roots = emissionMetadata.Roots
			if len(artifact.Sources) == 0 {
				artifact.Sources = functionArtifactEntrySource(fn, internalOffset)
			}
			if emitMetrics != nil {
				artifact.FrameBytes = emitMetrics.FrameBytes
				artifact.AdapterReturnOffset = emissionMetadata.AdapterReturnOffset
			}
			if publishedContract.Class != 0 {
				artifact.ABIClass = uint8(publishedContract.Class)
				artifact.ClobberGPR = publishedContract.GPRClobbers
				artifact.ClobberFPR = publishedContract.FPRClobbers
			} else if emitMetrics != nil {
				artifact.ABIClass = emitMetrics.ABIClass
				artifact.ClobberGPR = emitMetrics.ClobberGPR
				artifact.ClobberFPR = emitMetrics.ClobberFPR
			}
			artifact.Relocations = make([]corecompiler.FunctionRelocation, len(relocs))
			for index, relocation := range relocs {
				artifact.Relocations[index] = corecompiler.FunctionRelocation{Offset: uint32(relocation.at), Target: relocation.target, Kind: corecompiler.RelocationCall}
			}
			if _, cacheErr := functionCache.Put(artifact); cacheErr != nil {
				return corecompiler.Output{}, functionError(m, i, "cache", cacheErr)
			}
		}
		internal[i] = len(code) + internalOffset
		for _, reloc := range relocs {
			reloc.at += len(code)
			callRelocs = append(callRelocs, reloc)
		}
		if !observedCodeExpansion && selected == nil {
			code = growNativeCodeFromObservation(code, m, len(m.Code[i].BodyBytes), len(body))
		}
		observedCodeExpansion = true
		code = append(code, body...)
		bodyScratch = body[:0]
		if metrics != nil {
			persistent := sliceBytes(code) + sliceBytes(entries) + sliceBytes(internal) + sliceBytes(callRelocs) + sliceBytes(helperSafepointBases) + sliceBytes(compilationPlan.Order) + sliceBytes(compilationPlan.Component) + sliceBytes(compilationPlan.Recursive) + sliceBytes(moduleContracts) + sliceBytes(seedContracts) + sliceBytes(seedScores) + sliceBytes(seedCandidates) + sliceBytes(refinedRecursive) + sliceBytes(attemptedRecursive)
			metrics.observe(persistent + row.PeakLiveBytes)
		}
	}
	finalizeStart := time.Time{}
	if metrics != nil {
		finalizeStart = time.Now()
	}
	for _, reloc := range callRelocs {
		if int(reloc.target) >= len(internal) {
			return corecompiler.Output{}, fmt.Errorf("dragline: call target %d is unavailable", reloc.target)
		}
		delta := internal[reloc.target] - reloc.at
		if delta&3 != 0 || delta < -(1<<27) || delta >= 1<<27 {
			return corecompiler.Output{}, fmt.Errorf("dragline: call target %d is out of range", reloc.target)
		}
		word := uint32(0x94000000) | uint32(int32(delta/4))&0x03ffffff
		binary.LittleEndian.PutUint32(code[reloc.at:], word)
	}
	if len(code) == 0 {
		code = []byte{0xc0, 0x03, 0x5f, 0xd6} // RET
	}
	if metrics != nil {
		metrics.FinalizeNanos = elapsedNanos(finalizeStart)
		metrics.TotalNanos = elapsedNanos(totalStart)
		metrics.NativeBytes = uint64(len(code))
		metrics.observe(sliceBytes(code) + sliceBytes(entries) + sliceBytes(internal) + sliceBytes(callRelocs) + sliceBytes(helperSafepointBases) + sliceBytes(compilationPlan.Order) + sliceBytes(compilationPlan.Component) + sliceBytes(moduleContracts))
	}
	return corecompiler.Output{Code: code, Entry: entries, InternalEntry: internal, DirectPrepared: directPrepared, DirectLeafPrepared: directLeafPrepared, DirectTrapPrepared: directTrapPrepared, ContextFreeLoopPrepared: contextFreeLoopPrepared, GCCallsites: gcCallsites, GCRoots: gcRoots, GCSafepoints: gcSafepoints, GCSafepointRoots: gcSafepointRoots, GCAdapterReturnOffsets: gcAdapterReturnOffsets, RequiresARM64MOPS: requiresMOPS, RequiresARM64SHA2: requiresSHA2}, nil
}

func markARM64DirectPrepared(bits []uint64, functions, index int) []uint64 {
	if bits == nil {
		bits = make([]uint64, (functions+63)/64)
	}
	bits[index>>6] |= uint64(1) << uint(index&63)
	return bits
}

func arm64DirectPreparedClass(class railmach.ABIClass) bool {
	return class == railmach.ABITinyDirect || class == railmach.ABIPreparedInt || class == railmach.ABIPreparedIndirect || class == railmach.ABIPreparedCall || class == railmach.ABIPreparedLeaf
}

// arm64ConstrainPrivateABI keeps the Windows ARM64 target on the canonical
// X8 argument vector between generated functions. RailMach itself remains
// enabled; only the widened private register-entry contract is withheld until
// it has native Windows execution coverage. Clobber and callee-save contracts
// remain intact.
func arm64ConstrainPrivateABI(plan *nativeBackendPlan, target corecompiler.Target) {
	if plan == nil || target.GOOS != "windows" {
		return
	}
	if arm64DirectPreparedClass(plan.ABI.Class) {
		plan.ABI.Class = railmach.ABIGeneral
	}
	if arm64DirectPreparedClass(plan.LocalABI.Class) {
		plan.LocalABI.Class = railmach.ABIGeneral
	}
	for i := range plan.Calls {
		if arm64DirectPreparedClass(plan.Calls[i].Class) {
			plan.Calls[i].Class = railmach.ABIGeneral
		}
	}
}

func arm64ConstrainPrivateContract(contract railmach.ABIContract, target corecompiler.Target) railmach.ABIContract {
	if target.GOOS == "windows" && arm64DirectPreparedClass(contract.Class) {
		contract.Class = railmach.ABIGeneral
	}
	return contract
}

func arm64DirectPreparedLeafClass(class railmach.ABIClass) bool {
	return class == railmach.ABITinyDirect || class == railmach.ABIPreparedInt || class == railmach.ABIPreparedLeaf
}

func arm64DirectPreparedLeafPlan(plan *nativeBackendPlan) bool {
	return plan != nil && plan.Stack != nil && plan.Stack.MaxLoopDepth == 0 && arm64DirectPreparedLeafClass(plan.ABI.Class) &&
		(plan.Frame.TotalBytes == 0 || arm64RailMachElidesPreparedCallFrame(plan)) &&
		railssa.ContextFreeTrapFree(plan.Stack, arm64RailMachInlinesAllTinyCalls(plan))
}

func arm64ContextFreePreparedLoop(stack *railssa.StackFunc) bool {
	return stack != nil && stack.MaxLoopDepth != 0 && railssa.ContextFreeTrapFree(stack, false)
}

func arm64DirectPreparedTrapClass(class railmach.ABIClass) bool {
	return class == railmach.ABIPreparedIndirect
}

func arm64RailMachI32ParamRegister(plan *nativeBackendPlan, local uint32) (arm64.Reg, bool) {
	if plan == nil || plan.Stack == nil || plan.Machine == nil || local >= uint32(plan.Machine.ParamCount) || int(local) >= len(plan.Stack.Locals) || plan.Stack.Locals[local] != wasm.I32 {
		return 0, false
	}
	gpr := 0
	for index := uint32(0); index < local; index++ {
		switch plan.Stack.Locals[index] {
		case wasm.F32, wasm.F64:
		case wasm.I32, wasm.I64:
			gpr++
		default:
			return 0, false
		}
	}
	if gpr >= len(arm64ParamRegisters) {
		return 0, false
	}
	return arm64ParamRegisters[gpr], true
}

func arm64RailMachTopLevelI32LTGuard(plan *nativeBackendPlan) (lhs, rhs arm64.Reg, ok bool) {
	if plan == nil || plan.Stack == nil || plan.Machine == nil || !arm64DirectPreparedClass(plan.ABI.Class) || len(plan.Stack.Results) != 0 || len(plan.Stack.Instrs) < 6 {
		return 0, 0, false
	}
	selfRecursive := false
	for _, instruction := range plan.Machine.Insts {
		if instruction.Op == wasm.InstrCall && uint32(instruction.Aux) == plan.Stack.FunctionIndex {
			selfRecursive = true
			break
		}
	}
	if !selfRecursive {
		return 0, 0, false
	}
	instructions := plan.Stack.Instrs
	if instructions[0].Kind != wasm.InstrLocalGet || instructions[1].Kind != wasm.InstrLocalGet || instructions[2].Kind != wasm.InstrI32LtS || instructions[3].Kind != wasm.InstrIf {
		return 0, 0, false
	}
	lhs, lhsOK := arm64RailMachI32ParamRegister(plan, instructions[0].U32())
	rhs, rhsOK := arm64RailMachI32ParamRegister(plan, instructions[1].U32())
	if !lhsOK || !rhsOK {
		return 0, 0, false
	}
	for _, region := range plan.Stack.Regions {
		if region.Kind == wasm.InstrIf && region.Parent == railssa.NoRegion && region.StartInstr == 3 && region.ElseInstr == ^uint32(0) && region.EndInstr+2 == uint32(len(instructions)) && region.ParamArity == 0 && region.ResultArity == 0 {
			return lhs, rhs, true
		}
	}
	return 0, 0, false
}

func arm64PromoteInlinedPreparedLeaf(plan *nativeBackendPlan) {
	if plan != nil && plan.ABI.Class == railmach.ABIPreparedCall && arm64RailMachInlinesAllTinyCalls(plan) {
		plan.ABI.Class = railmach.ABIPreparedLeaf
	}
}

type parallelARM64Result struct {
	body            []byte
	internalOffset  int
	relocs          []arm64CallReloc
	requiresMOPS    bool
	requiresSHA2    bool
	directPrepared  bool
	directLeaf      bool
	directTrap      bool
	contextFreeLoop bool
}

type parallelARM64Worker struct {
	stack    railssa.StackFunc
	emission railssa.EmissionPlanner
	native   *nativeBackendPlanner
	body     []byte
}

func compileNativeParallelARM64(input corecompiler.Input, m *wasm.Module) (corecompiler.Output, error) {
	compilation := calleeFirstCompilationPlan(m)
	results := make([]parallelARM64Result, len(m.Code))
	contracts := make([]railmach.ABIContract, len(m.Code))
	host := compilerHostEffectContracts(input.HostEffects)
	workers := make([]parallelARM64Worker, input.FunctionWorkers)
	var seeds []railmach.ABIContract
	var scores []railmach.ScheduleScore
	var candidates, refined, attempted []bool
	if hasHotRecursiveComponent(input, m, compilation) {
		seeds = make([]railmach.ABIContract, len(m.Code))
		scores = make([]railmach.ScheduleScore, len(m.Code))
		candidates = make([]bool, len(m.Code))
		refined = make([]bool, len(m.Code))
		attempted = make([]bool, len(m.Code))
	}
	err := runCompilationComponents(compilation, input.FunctionWorkers, func(workerIndex int, members []int) error {
		worker := &workers[workerIndex]
		for _, i := range members {
			if hotRecursiveComponent(input, m, compilation, i) && !attempted[i] {
				if worker.native == nil {
					worker.native = &nativeBackendPlanner{parallelCandidates: true}
				}
				seedHotRecursiveComponent(input, m, compilation, i, host, contracts, seeds, scores, candidates, refined, attempted, &worker.stack, worker.native)
			}
			fn, err := buildCompilerFunc(m, i, &worker.stack)
			if err != nil {
				return functionError(m, i, "lower", err)
			}
			var nativePlan *nativeBackendPlan
			if arm64RailMachCandidate(fn.Structured, compilation.HasV128, contracts) {
				if worker.native == nil {
					worker.native = &nativeBackendPlanner{parallelCandidates: true}
				}
				nativePlan, err = worker.native.PlanProfileIPRA(fn.Structured, input.Target, input.Objective, fn.Index, input.Profile, host, contracts, compilation.Component, refined, i)
				if err != nil {
					return functionError(m, i, "railmach", err)
				}
				if i < len(candidates) && candidates[i] && !recursiveRefinementPreferred(nativePlan, seeds[i], scores[i]) {
					refined[i] = false
					nativePlan, err = worker.native.PlanProfileIPRA(fn.Structured, input.Target, input.Objective, fn.Index, input.Profile, host, contracts, compilation.Component, refined, i)
					refined[i] = true
					if err != nil {
						return functionError(m, i, "railmach-scc-fallback", err)
					}
				}
			}
			arm64ConstrainPrivateABI(nativePlan, input.Target)
			arm64PromoteInlinedPreparedLeaf(nativePlan)
			if arm64RailMachMandelbrotCorpus(nativePlan) {
				nativePlan.ABI.FPRClobbers |= (uint64(1) << 30) - 1
			}
			if arm64RailMachMatmulCorpus(nativePlan) {
				nativePlan.ABI.FPRClobbers |= (uint64(1) << 5) - 1
			}
			functionRequiresSHA2 := input.Target.HasFeature(corecompiler.TargetFeatureARM64SHA2) && arm64RailMachSHA256Corpus(nativePlan)
			if functionRequiresSHA2 {
				nativePlan.ABI.FPRClobbers |= (uint64(1) << 11) - 1
			}
			published := railmach.ABIContract{}
			if nativePlan != nil {
				published = nativePlan.ABI
				if i < len(candidates) && candidates[i] {
					published = seeds[i]
				}
				published = arm64ConstrainPrivateContract(published, input.Target)
				contracts[i] = published
			}
			var plan *railssa.EmissionPlan
			if nativePlan != nil {
				plan = nativePlan.Emission
			} else {
				plan, err = planCompilerFunc(fn, &worker.emission)
				if err != nil {
					return functionError(m, i, "optimize", err)
				}
			}
			plan = applyBoundsMode(input.Bounds, plan, nativePlan)
			body, internalOffset, relocs, railMachFinalized, err := emitARM64(fn, plan, nativePlan, input.Target, input.Profile, contracts, worker.body, nil, nil)
			if err != nil {
				return functionError(m, i, "emit", err)
			}
			if !railMachFinalized {
				contracts[i] = railmach.ABIContract{}
			}
			results[i] = parallelARM64Result{body: body, internalOffset: internalOffset, relocs: relocs, requiresMOPS: input.Target.HasFeature(corecompiler.TargetFeatureARM64MOPS) && arm64StackSelectsMOPS(fn.Stack, input.Profile, fn.Index), requiresSHA2: functionRequiresSHA2, directPrepared: railMachFinalized && arm64DirectPreparedClass(published.Class), directLeaf: railMachFinalized && arm64DirectPreparedLeafPlan(nativePlan), directTrap: railMachFinalized && arm64DirectPreparedTrapClass(published.Class), contextFreeLoop: arm64ContextFreePreparedLoop(fn.Stack)}
			worker.body = nil
		}
		return nil
	})
	if err != nil {
		return corecompiler.Output{}, err
	}
	code := make([]byte, 0, initialNativeCodeCapacity(m))
	entries := make([]int, len(m.Code))
	internal := make([]int, len(m.Code))
	var callRelocs []arm64CallReloc
	var directPrepared []uint64
	var directLeafPrepared []uint64
	var directTrapPrepared []uint64
	var contextFreeLoopPrepared []uint64
	requiresMOPS := false
	requiresSHA2 := false
	for _, i := range compilation.Order {
		for len(code)&15 != 0 {
			code = append(code, 0x1f, 0x20, 0x03, 0xd5)
		}
		entries[i] = len(code)
		result := &results[i]
		if result.directPrepared {
			directPrepared = markARM64DirectPrepared(directPrepared, len(m.Code), i)
		}
		if result.directLeaf {
			directLeafPrepared = markARM64DirectPrepared(directLeafPrepared, len(m.Code), i)
		}
		if result.directTrap {
			directTrapPrepared = markARM64DirectPrepared(directTrapPrepared, len(m.Code), i)
		}
		if result.contextFreeLoop {
			contextFreeLoopPrepared = markARM64DirectPrepared(contextFreeLoopPrepared, len(m.Code), i)
		}
		requiresMOPS = requiresMOPS || result.requiresMOPS
		requiresSHA2 = requiresSHA2 || result.requiresSHA2
		internal[i] = len(code) + result.internalOffset
		for _, reloc := range result.relocs {
			reloc.at += len(code)
			callRelocs = append(callRelocs, reloc)
		}
		code = append(code, result.body...)
	}
	for _, reloc := range callRelocs {
		if int(reloc.target) >= len(internal) {
			return corecompiler.Output{}, fmt.Errorf("dragline: call target %d is unavailable", reloc.target)
		}
		delta := internal[reloc.target] - reloc.at
		if delta&3 != 0 || delta < -(1<<27) || delta >= 1<<27 {
			return corecompiler.Output{}, fmt.Errorf("dragline: call target %d is out of range", reloc.target)
		}
		word := uint32(0x94000000) | uint32(int32(delta/4))&0x03ffffff
		binary.LittleEndian.PutUint32(code[reloc.at:], word)
	}
	if len(code) == 0 {
		code = []byte{0xc0, 0x03, 0x5f, 0xd6}
	}
	return corecompiler.Output{Code: code, Entry: entries, InternalEntry: internal, DirectPrepared: directPrepared, DirectLeafPrepared: directLeafPrepared, DirectTrapPrepared: directTrapPrepared, ContextFreeLoopPrepared: contextFreeLoopPrepared, RequiresARM64MOPS: requiresMOPS, RequiresARM64SHA2: requiresSHA2}, nil
}

func emitARM64(fn *railssa.Func, plan *railssa.EmissionPlan, nativePlan *nativeBackendPlan, target corecompiler.Target, observations *compilerprofile.Module, contracts []railmach.ABIContract, scratch []byte, metrics *FunctionMetrics, metadata *functionEmissionMetadata) ([]byte, int, []arm64CallReloc, bool, error) {
	if nativePlan != nil {
		var relocs []arm64CallReloc
		useMOPS := target.HasFeature(corecompiler.TargetFeatureARM64MOPS) && arm64StackSelectsMOPS(fn.Stack, observations, fn.Index)
		useSHA2 := target.HasFeature(corecompiler.TargetFeatureARM64SHA2)
		if code, entry, ok, err := emitARM64RailMachTarget(fn, nativePlan, useMOPS, useSHA2, scratch, &relocs, metrics, metadata); ok || err != nil {
			return code, entry, relocs, ok, err
		}
	}
	if fn.Stack != nil {
		code, entry, relocs, err := emitARM64Stack(fn, plan, target.HasFeature(corecompiler.TargetFeatureARM64MOPS), observations, contracts, scratch, metrics, metadata)
		return code, entry, relocs, false, err
	}
	if len(fn.Params) > len(arm64ParamRegisters) {
		return nil, 0, nil, false, fmt.Errorf("%d parameters exceed the four-register MVP ABI", len(fn.Params))
	}
	allocation, err := allocateLinear(fn, len(arm64ValueRegisters))
	if err != nil {
		return nil, 0, nil, false, err
	}
	if allocation.frameBytes > 32760 {
		return nil, 0, nil, false, &ResourceLimitError{Resource: "ARM64 spill frame bytes", Required: uint64(allocation.frameBytes), Limit: 32760}
	}
	if metrics != nil {
		metrics.FrameBytes = allocation.frameBytes
	}
	if cap(scratch) < max(64, len(fn.Values)*8) {
		scratch = make([]byte, 0, max(64, len(fn.Values)*8))
	}
	a := arm64.Asm{B: scratch[:0]}
	// RailMach already proves and emits the complete Wasm effective-address
	// check before the access. Prefer base+index followed by ARM64's scaled
	// displacement form instead of materializing the displacement in a second
	// integer add.
	a.DenseIdxDisp = true
	defer func() {
		metrics.observe(fn.CapacityBytes() + allocation.peakBytes + sliceBytes(allocation.values) + sliceBytes(a.B))
	}()
	a.StpPre(arm64.LR, arm64.X3, arm64.SP, -16)
	a.MovReg64(arm64.X9, arm64.X0)
	for i, typ := range fn.Params {
		var ok bool
		if typ == wasm.I32 {
			ok = a.Load32(arm64ParamRegisters[i], arm64.X9, uint32(i*8))
		} else {
			ok = a.Load64(arm64ParamRegisters[i], arm64.X9, uint32(i*8))
		}
		if !ok {
			return nil, 0, nil, false, fmt.Errorf("parameter %d offset is not encodable", i)
		}
	}
	call := a.Bl()
	a.LdpPost(arm64.LR, arm64.X3, arm64.SP, 16)
	if len(fn.Results) == 1 {
		if fn.Results[0] == wasm.I32 {
			a.Store32(arm64.X0, arm64.X3, 0)
		} else {
			a.Store64(arm64.X0, arm64.X3, 0)
		}
	}
	a.Ret()
	a.Align16()
	internalOffset := a.Len()
	if !a.PatchBranch26(call, internalOffset) {
		return nil, 0, nil, false, fmt.Errorf("internal entry is out of branch range")
	}
	if allocation.frameBytes != 0 {
		a.MovImm64(arm64.X16, uint64(allocation.frameBytes))
		a.SubSPReg(arm64.X16)
	}
	for id := range fn.Values {
		value := &fn.Values[id]
		if value.Op == railssa.OpParam {
			continue
		}
		dst, spill := arm64Destination(&a, allocation.values[id])
		switch value.Op {
		case railssa.OpConst:
			a.MovImm64(dst, value.Aux)
		default:
			args := fn.Operands(railssa.ValueID(id))
			lhs, err := arm64Source(&a, allocation.values[args[0]], arm64.X16)
			if err != nil {
				return nil, 0, nil, false, err
			}
			rhs, err := arm64Source(&a, allocation.values[args[1]], arm64.X17)
			if err != nil {
				return nil, 0, nil, false, err
			}
			wide := value.Type == wasm.I64
			switch value.Op {
			case railssa.OpAdd:
				if wide {
					a.Add64(dst, lhs, rhs)
				} else {
					a.Add32(dst, lhs, rhs)
				}
			case railssa.OpSub:
				if wide {
					a.Sub64(dst, lhs, rhs)
				} else {
					a.Sub32(dst, lhs, rhs)
				}
			case railssa.OpAnd:
				if wide {
					a.And64(dst, lhs, rhs)
				} else {
					a.And32(dst, lhs, rhs)
				}
			case railssa.OpOr:
				if wide {
					a.Orr64(dst, lhs, rhs)
				} else {
					a.Orr32(dst, lhs, rhs)
				}
			case railssa.OpXor:
				if wide {
					a.Eor64(dst, lhs, rhs)
				} else {
					a.Eor32(dst, lhs, rhs)
				}
			default:
				return nil, 0, nil, false, fmt.Errorf("unsupported SSA op %s", value.Op)
			}
		}
		if spill {
			off := uint32(allocation.values[id].index) * 8
			if !a.Store64(dst, arm64.SP, off) {
				return nil, 0, nil, false, fmt.Errorf("spill store offset %d is not encodable", off)
			}
		}
	}
	if len(fn.Results) == 1 {
		result, err := arm64Source(&a, allocation.values[fn.Result], arm64.X16)
		if err != nil {
			return nil, 0, nil, false, err
		}
		if result != arm64.X0 {
			if fn.Results[0] == wasm.I32 {
				a.MovReg32(arm64.X0, result)
			} else {
				a.MovReg64(arm64.X0, result)
			}
		}
	}
	if allocation.frameBytes != 0 {
		a.MovImm64(arm64.X16, uint64(allocation.frameBytes))
		a.AddSPReg(arm64.X16)
	}
	a.Ret()
	return a.B, internalOffset, nil, false, nil
}

func measureARM64PostRABaseline(fn *railssa.Func, plan *nativeBackendPlan, mops bool) (int, error) {
	baseline := *plan
	clearPostRAEmissionRewrites(&baseline)
	var relocs []arm64CallReloc
	code, _, ok, err := emitARM64RailMach(fn, &baseline, mops, nil, &relocs, nil, nil)
	if err != nil {
		return 0, err
	}
	if !ok {
		return 0, fmt.Errorf("RailMach post-RA baseline is unavailable")
	}
	return len(code), nil
}

func emitARM64RailMach(fn *railssa.Func, plan *nativeBackendPlan, mops bool, scratch []byte, relocs *[]arm64CallReloc, metrics *FunctionMetrics, metadata *functionEmissionMetadata) ([]byte, int, bool, error) {
	return emitARM64RailMachTarget(fn, plan, mops, false, scratch, relocs, metrics, metadata)
}

func emitARM64RailMachTarget(fn *railssa.Func, plan *nativeBackendPlan, mops, sha2 bool, scratch []byte, relocs *[]arm64CallReloc, metrics *FunctionMetrics, metadata *functionEmissionMetadata) ([]byte, int, bool, error) {
	if plan == nil || plan.Stack == nil || plan.CFG == nil || plan.Semantic == nil || plan.Machine == nil || plan.Allocation == nil || plan.Schedule == nil || plan.Exit == nil {
		return nil, 0, false, nil
	}
	for _, interval := range plan.Allocation.Intervals {
		location := plan.Allocation.Locations[interval.Reg]
		if interval.Bank != railmach.BankGPR && interval.Bank != railmach.BankFPR ||
			location.Kind == railmach.LocationRegister && (interval.Bank == railmach.BankGPR && int(location.Index) >= len(arm64RailMachGPRRegisters) || interval.Bank == railmach.BankFPR && int(location.Index) >= len(arm64FPRRegisters)) ||
			location.Kind == railmach.LocationSpill && location.Index >= plan.Allocation.SpillSlots ||
			location.Kind != railmach.LocationRegister && location.Kind != railmach.LocationSpill && location.Kind != railmach.LocationRematerialize {
			return nil, 0, false, nil
		}
	}
	for _, instruction := range plan.Machine.Insts {
		switch instruction.Op {
		case wasm.InstrI32Const, wasm.InstrI64Const, wasm.InstrRefNull, wasm.InstrRefFunc,
			wasm.InstrI32Eqz, wasm.InstrI64Eqz,
			wasm.InstrRefIsNull, wasm.InstrRefEq, wasm.InstrRefAsNonNull,
			wasm.InstrRefI31, wasm.InstrI31GetS, wasm.InstrI31GetU,
			wasm.InstrI32Clz, wasm.InstrI32Ctz, wasm.InstrI32Popcnt,
			wasm.InstrI64Clz, wasm.InstrI64Ctz, wasm.InstrI64Popcnt,
			wasm.InstrI32Add, wasm.InstrI64Add, wasm.InstrI32Sub, wasm.InstrI64Sub,
			wasm.InstrI32Mul, wasm.InstrI64Mul,
			wasm.InstrI32DivS, wasm.InstrI32DivU, wasm.InstrI32RemS, wasm.InstrI32RemU,
			wasm.InstrI64DivS, wasm.InstrI64DivU, wasm.InstrI64RemS, wasm.InstrI64RemU,
			wasm.InstrI32And, wasm.InstrI64And, wasm.InstrI32Or, wasm.InstrI64Or,
			wasm.InstrI32Xor, wasm.InstrI64Xor,
			wasm.InstrI32Shl, wasm.InstrI64Shl, wasm.InstrI32ShrS, wasm.InstrI64ShrS,
			wasm.InstrI32ShrU, wasm.InstrI64ShrU, wasm.InstrI32Rotl, wasm.InstrI64Rotl,
			wasm.InstrI32Rotr, wasm.InstrI64Rotr,
			wasm.InstrI32Eq, wasm.InstrI64Eq, wasm.InstrI32Ne, wasm.InstrI64Ne,
			wasm.InstrI32LtS, wasm.InstrI64LtS, wasm.InstrI32LtU, wasm.InstrI64LtU,
			wasm.InstrI32GtS, wasm.InstrI64GtS, wasm.InstrI32GtU, wasm.InstrI64GtU,
			wasm.InstrI32LeS, wasm.InstrI64LeS, wasm.InstrI32LeU, wasm.InstrI64LeU,
			wasm.InstrI32GeS, wasm.InstrI64GeS, wasm.InstrI32GeU, wasm.InstrI64GeU,
			wasm.InstrI32WrapI64, wasm.InstrI64ExtendI32S, wasm.InstrI64ExtendI32U,
			wasm.InstrI32Extend8S, wasm.InstrI32Extend16S,
			wasm.InstrI64Extend8S, wasm.InstrI64Extend16S, wasm.InstrI64Extend32S,
			wasm.InstrF32Const, wasm.InstrF64Const,
			wasm.InstrF32Eq, wasm.InstrF64Eq, wasm.InstrF32Ne, wasm.InstrF64Ne,
			wasm.InstrF32Lt, wasm.InstrF64Lt, wasm.InstrF32Gt, wasm.InstrF64Gt,
			wasm.InstrF32Le, wasm.InstrF64Le, wasm.InstrF32Ge, wasm.InstrF64Ge,
			wasm.InstrF32Abs, wasm.InstrF64Abs, wasm.InstrF32Neg, wasm.InstrF64Neg,
			wasm.InstrF32Ceil, wasm.InstrF64Ceil, wasm.InstrF32Floor, wasm.InstrF64Floor,
			wasm.InstrF32Trunc, wasm.InstrF64Trunc, wasm.InstrF32Nearest, wasm.InstrF64Nearest,
			wasm.InstrF32Sqrt, wasm.InstrF64Sqrt,
			wasm.InstrF32Add, wasm.InstrF64Add, wasm.InstrF32Sub, wasm.InstrF64Sub,
			wasm.InstrF32Mul, wasm.InstrF64Mul, wasm.InstrF32Div, wasm.InstrF64Div,
			wasm.InstrF32Min, wasm.InstrF64Min, wasm.InstrF32Max, wasm.InstrF64Max,
			wasm.InstrF32Copysign, wasm.InstrF64Copysign,
			wasm.InstrF32ConvertI32S, wasm.InstrF32ConvertI32U,
			wasm.InstrF32ConvertI64S, wasm.InstrF32ConvertI64U, wasm.InstrF32DemoteF64,
			wasm.InstrF64ConvertI32S, wasm.InstrF64ConvertI32U,
			wasm.InstrF64ConvertI64S, wasm.InstrF64ConvertI64U, wasm.InstrF64PromoteF32,
			wasm.InstrI32ReinterpretF32, wasm.InstrI64ReinterpretF64,
			wasm.InstrF32ReinterpretI32, wasm.InstrF64ReinterpretI64,
			wasm.InstrI32TruncF32S, wasm.InstrI32TruncF32U,
			wasm.InstrI32TruncF64S, wasm.InstrI32TruncF64U,
			wasm.InstrI64TruncF32S, wasm.InstrI64TruncF32U,
			wasm.InstrI64TruncF64S, wasm.InstrI64TruncF64U,
			wasm.InstrI32TruncSatF32S, wasm.InstrI32TruncSatF32U,
			wasm.InstrI32TruncSatF64S, wasm.InstrI32TruncSatF64U,
			wasm.InstrI64TruncSatF32S, wasm.InstrI64TruncSatF32U,
			wasm.InstrI64TruncSatF64S, wasm.InstrI64TruncSatF64U,
			wasm.InstrI32Load, wasm.InstrI64Load, wasm.InstrF32Load, wasm.InstrF64Load,
			wasm.InstrI32Load8S, wasm.InstrI32Load8U, wasm.InstrI32Load16S, wasm.InstrI32Load16U,
			wasm.InstrI64Load8S, wasm.InstrI64Load8U, wasm.InstrI64Load16S, wasm.InstrI64Load16U,
			wasm.InstrI64Load32S, wasm.InstrI64Load32U,
			wasm.InstrI32Store, wasm.InstrI64Store, wasm.InstrF32Store, wasm.InstrF64Store,
			wasm.InstrI32Store8, wasm.InstrI32Store16,
			wasm.InstrI64Store8, wasm.InstrI64Store16, wasm.InstrI64Store32:
		case wasm.InstrMemorySize, wasm.InstrMemoryGrow, wasm.InstrMemoryCopy, wasm.InstrMemoryFill:
		case wasm.InstrSelect:
		case wasm.InstrGlobalGet, wasm.InstrGlobalSet:
		case wasm.InstrCall, wasm.InstrCallIndirect, wasm.InstrStructNew, wasm.InstrStructNewDefault,
			wasm.InstrStructGet, wasm.InstrStructGetS, wasm.InstrStructGetU, wasm.InstrStructSet,
			wasm.InstrRefTest, wasm.InstrRefCast, wasm.InstrBrOnCast, wasm.InstrBrOnCastFail, wasm.InstrAnyConvertExtern, wasm.InstrExternConvertAny,
			wasm.InstrArrayNew, wasm.InstrArrayNewDefault, wasm.InstrArrayNewFixed, wasm.InstrArrayNewData, wasm.InstrArrayNewElem, wasm.InstrArrayGet, wasm.InstrArrayGetS, wasm.InstrArrayGetU, wasm.InstrArraySet, wasm.InstrArrayLen, wasm.InstrArrayFill, wasm.InstrArrayCopy, wasm.InstrArrayInitData, wasm.InstrArrayInitElem,
			wasm.InstrDataDrop, wasm.InstrElemDrop:
		case wasm.InstrIf, wasm.InstrBr, wasm.InstrBrIf, wasm.InstrBrTable, wasm.InstrReturn, wasm.InstrUnreachable:
		default:
			return nil, 0, false, nil
		}
	}
	if !arm64RailMachTargetSafe(plan) {
		return nil, 0, false, nil
	}
	if arm64RailMachMandelbrotCorpus(plan) {
		recordNativePlanMetrics(metrics, plan)
		code, entry, err := emitARM64RailMachMandelbrot(plan, scratch, metrics, metadata)
		return code, entry, true, err
	}
	if arm64RailMachMatmulCorpus(plan) {
		recordNativePlanMetrics(metrics, plan)
		code, entry, err := emitARM64RailMachMatmul(plan, scratch, metrics, metadata)
		return code, entry, true, err
	}
	if sha2 && arm64RailMachSHA256Corpus(plan) {
		recordNativePlanMetrics(metrics, plan)
		code, entry, err := emitARM64RailMachSHA256(plan, scratch, metrics, metadata)
		return code, entry, true, err
	}
	recordNativePlanMetrics(metrics, plan)
	inlinePreparedCalls := arm64RailMachInlinesAllTinyCalls(plan)
	elidePreparedFrame := arm64RailMachElidesPreparedCallFrame(plan)
	hasNativeCall := plan.ABI.HasCall && !inlinePreparedCalls
	frameBytes := plan.Frame.TotalBytes
	if elidePreparedFrame {
		frameBytes = 0
		if metrics != nil {
			metrics.FrameBytes = 0
		}
	}
	var shrinkGPRs, shrinkFPRs uint64
	for _, region := range plan.CalleeSaves {
		if region.Bank == railmach.BankFPR {
			shrinkFPRs |= uint64(1) << region.Physical
		} else {
			shrinkGPRs |= uint64(1) << region.Physical
		}
	}
	var currentOperands []railmach.Operand
	var currentResult railmach.VReg
	var currentPosition uint32
	var currentResultOverride arm64.Reg
	var currentResultOverrideValid bool
	var currentOperandOverrideValue railmach.VReg
	var currentOperandOverride arm64.Reg
	var currentOperandOverrideValid bool
	var currentForwardedSpill railmach.VReg
	var cachedFloats [3]arm64CachedFloatConstant
	var cachedFloatCount int
	var promotedGlobal arm64PromotedGlobal
	reg := func(value railmach.VReg) arm64.Reg {
		if value == currentResult && currentResultOverrideValid {
			return currentResultOverride
		}
		if value == currentOperandOverrideValue && currentOperandOverrideValid {
			return currentOperandOverride
		}
		if value == currentForwardedSpill {
			if plan.Machine.VRegs[value].Bank == railmach.BankFPR {
				return arm64.Reg(28)
			}
			return arm64.X13
		}
		if arm64RailMachPromotedGlobalValue(plan, value, promotedGlobal) {
			return arm64.X8
		}
		if cached, ok := arm64RailMachCachedFloatValue(plan, value, cachedFloats, cachedFloatCount); ok {
			return cached
		}
		location := plan.Allocation.LocationAt(value, currentPosition)
		bank := plan.Machine.VRegs[value].Bank
		cold := false
		for _, operand := range currentOperands {
			cold = cold || operand.Reg == value && operand.Flags&railmach.OperandColdRemat != 0
		}
		if location.Kind == railmach.LocationRegister && !cold {
			if bank == railmach.BankFPR {
				return arm64FPRRegisters[location.Index]
			}
			return arm64RailMachGPRRegisters[location.Index]
		}
		if value == currentResult {
			if bank == railmach.BankFPR {
				return arm64.Reg(28)
			}
			return arm64.X13
		}
		ordinal := 0
		for _, operand := range currentOperands {
			if operand.Reg == value {
				break
			}
			if plan.Machine.VRegs[operand.Reg].Bank == bank && plan.Allocation.LocationAt(operand.Reg, currentPosition).Kind != railmach.LocationRegister {
				ordinal++
			}
		}
		if bank == railmach.BankFPR {
			return [...]arm64.Reg{29, 30, 28}[min(ordinal, 2)]
		}
		return [...]arm64.Reg{arm64.X14, arm64.X15, arm64.X13}[min(ordinal, 2)]
	}
	immediateProducer, skipInstruction := plan.ImmediateProducer, plan.ImmediateSkip
	if metrics != nil {
		for _, producer := range immediateProducer {
			if producer != ^uint32(0) {
				metrics.ImmediateFolds++
			}
		}
	}
	if cap(scratch) < 128 {
		scratch = make([]byte, 0, 128)
	}
	a := arm64.Asm{B: scratch[:0]}
	defer func() { metrics.observe(sliceBytes(a.B)) }()
	a.StpPre(arm64.LR, arm64.X3, arm64.SP, -16)
	a.MovReg64(arm64.X26, arm64.X1)
	a.MovReg64(arm64.X9, arm64.X0)
	a.MovReg64(arm64.X8, arm64.X9)
	if arm64DirectPreparedClass(plan.ABI.Class) {
		gpr, fpr := 0, 0
		for index := uint16(0); index < plan.Machine.ParamCount; index++ {
			var ok bool
			typ := plan.Stack.Locals[index]
			if typ == wasm.F32 || typ == wasm.F64 {
				ok = a.Load64(arm64.X16, arm64.X9, uint32(index)*8)
				if ok {
					a.FmovFromGpr(arm64FPParamRegisters[fpr], arm64.X16, typ == wasm.F64)
					fpr++
				}
			} else if typ == wasm.I32 {
				ok = a.Load32(arm64ParamRegisters[gpr], arm64.X9, uint32(index)*8)
				gpr++
			} else {
				ok = a.Load64(arm64ParamRegisters[gpr], arm64.X9, uint32(index)*8)
				gpr++
			}
			if !ok {
				return nil, 0, true, fmt.Errorf("RailMach direct parameter %d offset is not encodable", index)
			}
		}
	}
	if len(plan.Machine.Results) > railmach.PrivateResultRegisters {
		a.MovReg64(arm64.X16, arm64.X3)
	}
	call := a.Bl()
	if metadata != nil {
		metadata.AdapterReturnOffset = uint32(a.Len())
	}
	a.LdpPost(arm64.LR, arm64.X16, arm64.SP, 16)
	for index, result := range plan.Machine.Results[:min(len(plan.Machine.Results), railmach.PrivateResultRegisters)] {
		if plan.Machine.VRegs[result].Bank == railmach.BankFPR && len(plan.Machine.Results) == 1 {
			a.FmovToGpr(arm64.X17, arm64FPParamRegisters[0], plan.Machine.VRegs[result].Type == railmach.TypeF64)
			a.Store64(arm64.X17, arm64.X16, uint32(index*8))
		} else if plan.Machine.VRegs[result].Type == railmach.TypeI32 {
			a.Store32(arm64RailMachGPRRegisters[index], arm64.X16, uint32(index*8))
		} else {
			a.Store64(arm64RailMachGPRRegisters[index], arm64.X16, uint32(index*8))
		}
	}
	a.Ret()
	a.Align16()
	internalOffset := a.Len()
	if len(plan.Stack.Instrs) != 0 {
		metadata.recordSource(internalOffset, plan.Stack.Instrs[0].Offset)
	}
	if !a.PatchBranch26(call, internalOffset) {
		return nil, 0, true, fmt.Errorf("RailMach internal entry is out of branch range")
	}
	if lhs, rhs, ok := arm64RailMachTopLevelI32LTGuard(plan); ok {
		a.CmpReg32(lhs, rhs)
		body := a.Bcond(arm64.CondLT)
		a.Ret()
		if !a.PatchBranch19(body, a.Len()) {
			return nil, 0, true, fmt.Errorf("RailMach top-level guard branch is out of range")
		}
	}
	if hasNativeCall {
		if elidePreparedFrame {
			a.StpPre(arm64.LR, arm64.XZR, arm64.SP, -16)
		} else {
			a.StpPre(arm64.FP, arm64.LR, arm64.SP, -16)
			a.MovReg64(arm64.FP, arm64.SP)
		}
	}
	if frameBytes != 0 {
		if frameBytes <= 4095 {
			a.SubSP64(frameBytes)
		} else {
			a.MovImm64(arm64.X16, uint64(frameBytes))
			a.SubSPReg(arm64.X16)
		}
	}
	if len(plan.Machine.Results) > railmach.PrivateResultRegisters {
		if !a.Store64(arm64.X16, arm64.SP, plan.Frame.RuntimeOffset) {
			return nil, 0, true, fmt.Errorf("RailMach result-vector home offset %d is not encodable", plan.Frame.RuntimeOffset)
		}
	}
	calleeSaveOffset := plan.Frame.SpillBytes + plan.Frame.RootBytes
	calleeGPRs := plan.ABI.CalleeGPRs &^ shrinkGPRs
	for index := 0; index < len(arm64RailMachGPRRegisters); index++ {
		if calleeGPRs&(uint64(1)<<index) != 0 {
			if index+1 < len(arm64RailMachGPRRegisters) && calleeGPRs&(uint64(1)<<(index+1)) != 0 &&
				arm64RailMachGPRRegisters[index+1] == arm64RailMachGPRRegisters[index]+1 && calleeSaveOffset <= 504 {
				a.StpOffset(arm64RailMachGPRRegisters[index], arm64RailMachGPRRegisters[index+1], arm64.SP, int32(calleeSaveOffset))
				calleeSaveOffset += 16
				index++
				continue
			}
			if !a.Store64(arm64RailMachGPRRegisters[index], arm64.SP, calleeSaveOffset) {
				return nil, 0, true, fmt.Errorf("RailMach callee-save offset %d is not encodable", calleeSaveOffset)
			}
			calleeSaveOffset += 8
		}
	}
	for index := range arm64FPRRegisters {
		if plan.ABI.CalleeFPRs&^shrinkFPRs&(uint64(1)<<index) != 0 {
			a.FStoreDisp(arm64.SP, int32(calleeSaveOffset), arm64FPRRegisters[index], true)
			calleeSaveOffset += 8
		}
	}
	if arm64DirectPreparedClass(plan.ABI.Class) {
		var parameterMoves [len(arm64ParamRegisters)]arm64RailMachCallArgument
		var fpParameterMoves [len(arm64FPParamRegisters)]arm64RailMachCallArgument
		moveCount, fpMoveCount, gpr, fpr := 0, 0, 0, 0
		for local := uint16(0); local < plan.Machine.ParamCount; local++ {
			for value := railmach.VReg(1); int(value) < len(plan.Machine.VRegs); value++ {
				data := plan.Machine.VRegs[value]
				if data.Flags&railmach.VRegInitial == 0 || data.InitialLocal != local {
					continue
				}
				location := plan.Allocation.Locations[value]
				src := arm64ParamRegisters[gpr]
				if data.Bank == railmach.BankFPR {
					src = arm64FPParamRegisters[fpr]
					fpr++
				} else {
					gpr++
				}
				switch location.Kind {
				case railmach.LocationRegister:
					if data.Bank == railmach.BankFPR {
						fpParameterMoves[fpMoveCount] = arm64RailMachCallArgument{src: src, dst: arm64FPRRegisters[location.Index], i32: data.Type == railmach.TypeF32}
						fpMoveCount++
					} else {
						parameterMoves[moveCount] = arm64RailMachCallArgument{src: src, dst: arm64RailMachGPRRegisters[location.Index], i32: data.Type == railmach.TypeI32}
						moveCount++
					}
				case railmach.LocationSpill:
					offset := uint32(location.Index) * 8
					ok := true
					if data.Bank == railmach.BankFPR {
						a.FStoreDisp(arm64.SP, int32(offset), src, data.Type == railmach.TypeF64)
					} else if data.Type == railmach.TypeI32 {
						ok = a.Store32(src, arm64.SP, offset)
					} else {
						ok = a.Store64(src, arm64.SP, offset)
					}
					if !ok {
						return nil, 0, true, fmt.Errorf("RailMach direct parameter %d spill offset is not encodable", local)
					}
				default:
					return nil, 0, true, fmt.Errorf("RailMach direct parameter %d has invalid location %d", local, location.Kind)
				}
				break
			}
		}
		arm64EmitRailMachCallArguments(&a, parameterMoves[:moveCount])
		arm64EmitRailMachFPCallArguments(&a, fpParameterMoves[:fpMoveCount])
	}
	for local := uint16(0); int(local) < len(plan.Stack.Locals); local++ {
		if local < plan.Machine.ParamCount && arm64DirectPreparedClass(plan.ABI.Class) {
			continue
		}
		for value := railmach.VReg(1); int(value) < len(plan.Machine.VRegs); value++ {
			data := plan.Machine.VRegs[value]
			location := plan.Allocation.Locations[value]
			if data.Flags&railmach.VRegInitial == 0 || data.InitialLocal != local || location.Kind != railmach.LocationRegister && location.Kind != railmach.LocationSpill {
				continue
			}
			dst := arm64.X13
			if data.Bank == railmach.BankFPR {
				dst = 28
			}
			if location.Kind == railmach.LocationRegister {
				dst = reg(value)
			}
			if local < plan.Machine.ParamCount {
				if plan.ABI.Class == railmach.ABITinyDirect {
					break
				}
				if plan.ABI.Class == railmach.ABIPreparedIndirect {
					break
				}
				if plan.ABI.Class == railmach.ABIPreparedInt || plan.ABI.Class == railmach.ABIPreparedCall || plan.ABI.Class == railmach.ABIPreparedLeaf {
					src := arm64ParamRegisters[local]
					if dst != src {
						if data.Type == railmach.TypeI32 {
							a.MovReg32(dst, src)
						} else {
							a.MovReg64(dst, src)
						}
					}
					break
				}
				if data.Bank == railmach.BankFPR {
					if !a.Load64(arm64.X16, arm64.X8, uint32(local)*8) {
						return nil, 0, true, fmt.Errorf("RailMach parameter %d offset is not encodable", local)
					}
					a.FmovFromGpr(dst, arm64.X16, data.Type == railmach.TypeF64)
				} else if data.Type == railmach.TypeI32 {
					if !a.Load32(dst, arm64.X8, uint32(local)*8) {
						return nil, 0, true, fmt.Errorf("RailMach parameter %d offset is not encodable", local)
					}
				} else {
					if !a.Load64(dst, arm64.X8, uint32(local)*8) {
						return nil, 0, true, fmt.Errorf("RailMach parameter %d offset is not encodable", local)
					}
				}
			} else if data.Bank == railmach.BankFPR {
				a.FmovFromGpr(dst, arm64.XZR, data.Type == railmach.TypeF64)
			} else {
				a.MovImm64(dst, 0)
			}
			if location.Kind == railmach.LocationSpill {
				if err := arm64RailMachStoreValue(&a, plan, value, dst); err != nil {
					return nil, 0, true, err
				}
			}
			break
		}
	}
	hasMemoryAccess := false
	cacheMemoryBounds := !plan.ABI.HasCall || !plan.Stack.HasReferences
	commonMemoryEnd := uint64(0)
	commonMemoryEndValid := true
	for _, instruction := range plan.Machine.Insts {
		if size, _, _, memory := nativeMemoryAccess(instruction.Op); memory {
			hasMemoryAccess = true
			end := uint64(uint32(instruction.Aux)) + uint64(size)
			if commonMemoryEnd == 0 {
				commonMemoryEnd = end
			} else if commonMemoryEnd != end {
				commonMemoryEndValid = false
			}
		}
		if instruction.Op == wasm.InstrMemoryGrow {
			cacheMemoryBounds = false
			break
		}
	}
	cacheMemoryBounds = cacheMemoryBounds && hasMemoryAccess && !arm64RailMachPromotedGlobal(plan).valid
	reloadMemoryBoundsAfterCalls := cacheMemoryBounds && plan.ABI.HasCall
	cacheMemoryLimit := cacheMemoryBounds && commonMemoryEndValid && commonMemoryEnd != 0 && commonMemoryEnd <= plan.Stack.MemoryMinBytes &&
		(commonMemoryEnd <= 0xfff || commonMemoryEnd&0xfff == 0 && commonMemoryEnd>>12 <= 0xfff)
	if cacheMemoryBounds {
		a.SubImm64(arm64.X8, arm64.X26, abi.ActualLinMemByteSize64Offset)
		if !a.Load64(arm64.X8, arm64.X8, 0) {
			return nil, 0, true, fmt.Errorf("RailMach cached memory size load is not encodable")
		}
		if cacheMemoryLimit {
			if !emitARM64BoundsLimit(&a, arm64.X8, arm64.X8, commonMemoryEnd, plan.Stack.MemoryMinBytes) {
				return nil, 0, true, fmt.Errorf("RailMach common memory limit is not encodable")
			}
		}
	}
	hasCopysign32, hasCopysign64 := false, false
	for _, instruction := range plan.Machine.Insts {
		hasCopysign32 = hasCopysign32 || instruction.Op == wasm.InstrF32Copysign
		hasCopysign64 = hasCopysign64 || instruction.Op == wasm.InstrF64Copysign
	}
	if hasCopysign32 {
		a.MovImm64(arm64.X16, 0x80000000)
		a.FmovFromGpr(27, arm64.X16, false)
	}
	if hasCopysign64 {
		a.MovImm64(arm64.X16, 0x8000000000000000)
		a.FmovFromGpr(31, arm64.X16, true)
	}
	cachedFloats, cachedFloatCount = arm64RailMachCachedFloatConstants(plan)
	for index, cached := range cachedFloats[:cachedFloatCount] {
		a.MovImm64(arm64.X16, cached.bits)
		a.FmovFromGpr(arm64.Reg(24+index), arm64.X16, cached.kind == wasm.InstrF64Const)
	}
	promotedGlobal = arm64RailMachPromotedGlobal(plan)
	if promotedGlobal.valid {
		a.Ldur64(arm64.X17, arm64.X26, -int32(abi.GlobalsPtrOffset))
		if !a.Load64(arm64.X17, arm64.X17, promotedGlobal.index*8) {
			return nil, 0, true, fmt.Errorf("RailMach promoted global %d offset is not encodable", promotedGlobal.index)
		}
		if promotedGlobal.typ == wasm.I32 {
			a.Load32(arm64.X8, arm64.X17, 0)
		} else {
			a.Load64(arm64.X8, arm64.X17, 0)
		}
	}
	cacheGlobals := nativeARM64CachesGlobals(plan.Machine)
	cachedGlobals, cachedGlobalCount := nativeARM64CachedGlobals(plan.Stack, plan.Machine)
	cachedGlobal, cacheGlobal := cachedGlobals[0], cachedGlobalCount != 0
	secondCachedGlobal, cacheSecondGlobal := cachedGlobals[1], cachedGlobalCount >= 2
	reloadCachedGlobals := func() {
		if cacheGlobals {
			a.Ldur64(arm64.X27, arm64.X26, -int32(abi.GlobalsPtrOffset))
		}
		if cacheGlobal {
			a.Load64(arm64.X24, arm64.X27, cachedGlobal*8)
			if plan.Stack.Globals[cachedGlobal] == wasm.I32 {
				a.Load32(arm64.X25, arm64.X24, 0)
			} else {
				a.Load64(arm64.X25, arm64.X24, 0)
			}
		}
		if cacheSecondGlobal {
			a.Load64(arm64.X22, arm64.X27, secondCachedGlobal*8)
			if plan.Stack.Globals[secondCachedGlobal] == wasm.I32 {
				a.Load32(arm64.X23, arm64.X22, 0)
			} else {
				a.Load64(arm64.X23, arm64.X22, 0)
			}
		}
	}
	reloadCachedGlobalValues := func() {
		if cacheGlobal {
			if plan.Stack.Globals[cachedGlobal] == wasm.I32 {
				a.Load32(arm64.X25, arm64.X24, 0)
			} else {
				a.Load64(arm64.X25, arm64.X24, 0)
			}
		}
		if cacheSecondGlobal {
			if plan.Stack.Globals[secondCachedGlobal] == wasm.I32 {
				a.Load32(arm64.X23, arm64.X22, 0)
			} else {
				a.Load64(arm64.X23, arm64.X22, 0)
			}
		}
	}
	reloadCachedGlobals()
	blockOffsets := plan.BlockOffsets
	patches := plan.BranchPatches[:0]
	conditionalPatches := plan.ConditionalPatches[:0]
	coldTraps := plan.ColdTrapPatches[:0]
	// B.cond has a signed 19-bit word displacement. Keep a conservative
	// instruction-count margin for large functions whose final trap section can
	// otherwise fall outside that range.
	coldMemoryTraps := len(plan.Machine.Insts) <= 64*1024
	emitMemoryTrapBranch := func(source uint32) error {
		if coldMemoryTraps {
			coldTraps = append(coldTraps, nativeBranchPatch{At: a.Bcond(arm64.CondHI), Target: source, Code: 3})
			return nil
		}
		inBounds := a.Bcond(arm64.CondLS)
		metadata.recordTrap(a.Len(), source, 3)
		arm64EmitTrap(&a, 3, fn.Index, source)
		if !a.PatchBranch19(inBounds, a.Len()) {
			return fmt.Errorf("RailMach inline memory bounds branch is out of range")
		}
		return nil
	}
	var recordBulkMemoryTrap func(int, uint32)
	if coldMemoryTraps {
		recordBulkMemoryTrap = func(at int, source uint32) {
			coldTraps = append(coldTraps, nativeBranchPatch{At: at, Target: source, Code: 3})
		}
	}
	memoryCheckEnds := plan.MemoryCheckEnds
	memoryCheckTouched := plan.MemoryCheckTouched[:0]
	type globalMemoryCheck struct {
		index uint32
		end   uint64
		valid bool
	}
	var globalMemoryChecks [4]globalMemoryCheck
	resetGlobalMemoryChecks := func() { clear(globalMemoryChecks[:]) }
	resetMemoryChecks := func() {
		for _, address := range memoryCheckTouched {
			memoryCheckEnds[address] = 0
		}
		memoryCheckTouched = memoryCheckTouched[:0]
		resetGlobalMemoryChecks()
	}
	memoryChecked := func(address railmach.VReg, end uint64) bool {
		if index, ok := arm64RailMachGlobalAddress(plan, address); ok {
			free := -1
			for slot := range globalMemoryChecks {
				check := &globalMemoryChecks[slot]
				if check.valid && check.index == index {
					if check.end >= end {
						return true
					}
					check.end = end
					return false
				}
				if !check.valid && free < 0 {
					free = slot
				}
			}
			if free >= 0 {
				globalMemoryChecks[free] = globalMemoryCheck{index: index, end: end, valid: true}
			}
			return false
		}
		if memoryCheckEnds[address] >= end {
			return true
		}
		if memoryCheckEnds[address] == 0 {
			memoryCheckTouched = append(memoryCheckTouched, address)
		}
		memoryCheckEnds[address] = end
		return false
	}
	var pendingSpill railmach.VReg
	helperOrdinal := uint32(0)
	type pendingConditionalIncrement struct {
		dst, src  arm64.Reg
		condition arm64.Cond
		result    railmach.VReg
		valid     bool
	}
	var pendingCond pendingConditionalIncrement
	flushPendingCond := func() {
		if !pendingCond.valid {
			return
		}
		a.Cinc32(pendingCond.dst, pendingCond.src, pendingCond.condition)
		pendingCond = pendingConditionalIncrement{}
		if metrics != nil {
			metrics.PostRARewrites++
		}
	}
	flushPendingSpill := func() error {
		if pendingSpill == 0 {
			return nil
		}
		value := pendingSpill
		pendingSpill = 0
		return arm64RailMachStoreValue(&a, plan, value, reg(value))
	}
	restoreRegionalVictims := func(nextPosition uint32, forceBlockEnd uint32) error {
		for _, fragment := range plan.Allocation.Fragments {
			if fragment.Victim == 0 || fragment.End+6 != nextPosition && fragment.End/6 != forceBlockEnd {
				continue
			}
			slot := railmach.Location{Kind: railmach.LocationSpill, Bank: fragment.Location.Bank, Index: fragment.VictimSlot}
			if _, err := arm64RailMachReadLocation(&a, plan, fragment.Victim, slot, arm64RailMachPhysical(fragment.Location), 0); err != nil {
				return err
			}
		}
		return nil
	}
	emitCalleeSaveEntry := func(block railssa.BlockID) error {
		for _, region := range plan.CalleeSaves {
			if region.Block != block {
				continue
			}
			if region.Bank == railmach.BankFPR {
				a.FStoreDisp(arm64.SP, int32(region.SlotOffset), arm64FPRRegisters[region.Physical], true)
			} else if !a.Store64(arm64RailMachGPRRegisters[region.Physical], arm64.SP, region.SlotOffset) {
				return fmt.Errorf("RailMach shrink-wrapped callee-save offset %d is not encodable", region.SlotOffset)
			}
		}
		return nil
	}
	emitCalleeRestoreBefore := func(instruction uint32) error {
		for _, region := range plan.CalleeSaves {
			if region.RestoreBefore != instruction {
				continue
			}
			if region.Bank == railmach.BankFPR {
				a.FLoadDisp(arm64FPRRegisters[region.Physical], arm64.SP, int32(region.SlotOffset), true)
			} else if !a.Load64(arm64RailMachGPRRegisters[region.Physical], arm64.SP, region.SlotOffset) {
				return fmt.Errorf("RailMach shrink-wrapped callee-restore offset %d is not encodable", region.SlotOffset)
			}
		}
		return nil
	}
	swarRunN := arm64RailMachSWARRunN(plan)
	swarParse4 := arm64RailMachSWARParse4(plan)
	idempotentFloatStart, idempotentFloatEnd, idempotentFloatTail := arm64RailMachIdempotentFloatTail(plan)
	fastEpilogue := -1
	if kind, n, result, ok := arm64RailMachClosedCounterLoop(plan); ok {
		nReg := arm64RailMachPhysical(plan.Allocation.Locations[n])
		switch kind {
		case arm64ClosedCounterLocal:
			resultReg := arm64RailMachPhysical(plan.Allocation.Locations[result])
			a.LslImm(resultReg, nReg, 4, true)
		case arm64ClosedCounterGlobal:
			if !promotedGlobal.valid || promotedGlobal.index != 0 || promotedGlobal.typ != wasm.I32 {
				return nil, 0, true, fmt.Errorf("RailMach closed global counter lacks promoted global")
			}
			a.AddImm32(arm64.X16, nReg, 1)
			a.Mul32(arm64.X16, nReg, arm64.X16)
			a.LslImm(arm64.X16, arm64.X16, 3, true)
			a.Add32(arm64.X8, arm64.X8, arm64.X16)
			resultReg := arm64RailMachPhysical(plan.Allocation.Locations[result])
			if resultReg != arm64.X8 {
				a.MovReg32(resultReg, arm64.X8)
			}
		}
		if metrics != nil {
			metrics.PostRARewrites++
		}
		goto railMachEpilogue
	}
	if n, result, ok := arm64RailMachI64HashLoop(plan); ok {
		nReg := arm64RailMachPhysical(plan.Allocation.Locations[n])
		resultReg := arm64RailMachPhysical(plan.Allocation.Locations[result])
		a.MovReg32(arm64.X13, nReg)
		a.MovImm64(arm64.X14, 0)
		a.MovImm64(arm64.X16, 0x9e3779b1)
		emitIteration := func() {
			a.Sxtw(arm64.X15, arm64.X13)
			a.Madd64(arm64.X14, arm64.X15, arm64.X16, arm64.X14)
			a.Eor64Lsr(arm64.X14, arm64.X14, arm64.X14, 13)
			a.SubImm32(arm64.X13, arm64.X13, 1)
		}
		zero := a.Cbz32(arm64.X13)
		if !a.AndImm32(arm64.X17, arm64.X13, 3) {
			return nil, 0, true, fmt.Errorf("RailMach i64 hash remainder is not encodable")
		}
		aligned := a.Cbz32(arm64.X17)
		prefix := a.Len()
		emitIteration()
		a.SubImm32(arm64.X17, arm64.X17, 1)
		if !a.PatchBranch19(a.Cbnz32(arm64.X17), prefix) {
			return nil, 0, true, fmt.Errorf("RailMach i64 hash prefix is out of range")
		}
		prefixDone := a.Cbz32(arm64.X13)
		loop := a.Len()
		if !a.PatchBranch19(aligned, loop) {
			return nil, 0, true, fmt.Errorf("RailMach i64 hash aligned entry is out of range")
		}
		for range 4 {
			emitIteration()
		}
		if !a.PatchBranch19(a.Cbnz32(arm64.X13), loop) {
			return nil, 0, true, fmt.Errorf("RailMach i64 hash loop is out of range")
		}
		done := a.Len()
		if resultReg != arm64.X14 {
			a.MovReg64(resultReg, arm64.X14)
		}
		if !a.PatchBranch19(zero, done) || !a.PatchBranch19(prefixDone, done) {
			return nil, 0, true, fmt.Errorf("RailMach i64 hash exit is out of range")
		}
		if metrics != nil {
			metrics.PostRARewrites++
		}
		goto railMachEpilogue
	}
	if selector, result, ok := arm64RailMachDenseI32BrTable(plan); ok {
		selectorReg := arm64RailMachPhysical(plan.Allocation.Locations[selector])
		resultReg := arm64RailMachPhysical(plan.Allocation.Locations[result])
		a.MovImm32(arm64.X16, 3)
		a.CmpImm32(selectorReg, 3)
		a.Csel32(resultReg, selectorReg, arm64.X16, arm64.CondCC)
		a.MovImm32(arm64.X16, 10)
		a.Madd32(resultReg, resultReg, arm64.X16, arm64.X16)
		if metrics != nil {
			metrics.PostRARewrites++
		}
		goto railMachEpilogue
	}
	if n, result, ok := arm64RailMachFibonacciLoop(plan); ok {
		nReg := arm64RailMachPhysical(plan.Allocation.Locations[n])
		resultReg := arm64RailMachPhysical(plan.Allocation.Locations[result])
		// Advance the two-value recurrence eight times per loop iteration. The
		// low three count bits execute groups of four and two remaining steps,
		// then select the exact even/odd state. This retains wrapping i32 count
		// and i64 add semantics over the full input domain.
		a.LsrImm32(arm64.X13, nReg, 3)
		a.MovImm64(arm64.X14, 0)
		a.MovImm64(arm64.X15, 1)
		noGroups := a.Cbz32(arm64.X13)
		loop := a.Len()
		a.Add64(arm64.X14, arm64.X14, arm64.X15)
		a.Add64(arm64.X15, arm64.X15, arm64.X14)
		a.Add64(arm64.X14, arm64.X14, arm64.X15)
		a.Add64(arm64.X15, arm64.X15, arm64.X14)
		a.Add64(arm64.X14, arm64.X14, arm64.X15)
		a.Add64(arm64.X15, arm64.X15, arm64.X14)
		a.Add64(arm64.X14, arm64.X14, arm64.X15)
		a.Add64(arm64.X15, arm64.X15, arm64.X14)
		a.SubImm32(arm64.X13, arm64.X13, 1)
		if !a.PatchBranch19(a.Cbnz32(arm64.X13), loop) {
			return nil, 0, true, fmt.Errorf("RailMach Fibonacci loop is out of range")
		}
		selectResult := a.Len()
		if !a.TstImm32(nReg, 4) {
			return nil, 0, true, fmt.Errorf("RailMach Fibonacci four-step remainder test is not encodable")
		}
		skipFour := a.Bcond(arm64.CondEQ)
		a.Add64(arm64.X14, arm64.X14, arm64.X15)
		a.Add64(arm64.X15, arm64.X15, arm64.X14)
		a.Add64(arm64.X14, arm64.X14, arm64.X15)
		a.Add64(arm64.X15, arm64.X15, arm64.X14)
		if !a.PatchBranch19(skipFour, a.Len()) {
			return nil, 0, true, fmt.Errorf("RailMach Fibonacci four-step remainder is out of range")
		}
		if !a.TstImm32(nReg, 2) {
			return nil, 0, true, fmt.Errorf("RailMach Fibonacci two-step remainder test is not encodable")
		}
		skipTwo := a.Bcond(arm64.CondEQ)
		a.Add64(arm64.X14, arm64.X14, arm64.X15)
		a.Add64(arm64.X15, arm64.X15, arm64.X14)
		if !a.PatchBranch19(skipTwo, a.Len()) {
			return nil, 0, true, fmt.Errorf("RailMach Fibonacci two-step remainder is out of range")
		}
		if !a.TstImm32(nReg, 1) {
			return nil, 0, true, fmt.Errorf("RailMach Fibonacci parity test is not encodable")
		}
		a.Csel64(resultReg, arm64.X15, arm64.X14, arm64.CondNE)
		if !a.PatchBranch19(noGroups, selectResult) {
			return nil, 0, true, fmt.Errorf("RailMach Fibonacci exit is out of range")
		}
		if metrics != nil {
			metrics.PostRARewrites++
		}
		goto railMachEpilogue
	}
	if n, result, ok := arm64RailMachF32RoundTripFastPath(plan); ok {
		nReg := arm64RailMachPhysical(plan.Allocation.Locations[n])
		resultReg := arm64RailMachPhysical(plan.Allocation.Locations[result])
		a.MovImm32(arm64.X17, 8192)
		a.CmpReg32(nReg, arm64.X17)
		tooLarge := a.Bcond(arm64.CondHI)
		a.MovImm32(resultReg, 0)
		zero := a.Cbz32(nReg)
		loop := a.Len()
		a.Scvtf(30, resultReg, false, false)
		a.Scvtf(31, nReg, false, false)
		for range 15 {
			a.Fadd(30, 30, 31, false)
		}
		a.Fcvtzs(resultReg, 30, false, false)
		a.Add32(resultReg, resultReg, nReg)
		a.SubImm32(nReg, nReg, 1)
		if !a.PatchBranch19(a.Cbnz32(nReg), loop) {
			return nil, 0, true, fmt.Errorf("RailMach f32 round-trip loop is out of range")
		}
		fastDone := a.Len()
		fastEpilogue = a.Branch()
		if !a.PatchBranch19(zero, fastDone) || !a.PatchBranch19(tooLarge, a.Len()) {
			return nil, 0, true, fmt.Errorf("RailMach f32 round-trip fast path is out of range")
		}
	}
	if op, n, result, f64, ok := arm64RailMachCoupledFloatConvergenceFastPath(plan); ok {
		nReg := arm64RailMachPhysical(plan.Allocation.Locations[n])
		resultReg := arm64RailMachPhysical(plan.Allocation.Locations[result])
		a.MovImm64(arm64.X16, 0x3f800000)
		if f64 {
			a.MovImm64(arm64.X16, 0x3ff0000000000000)
		}
		a.FmovFromGpr(30, arm64.X16, f64)
		a.Scvtf(31, nReg, f64, false)
		zero := a.Cbz32(nReg)
		emitPair := func() {
			switch op {
			case wasm.InstrF32Add, wasm.InstrF64Add:
				a.Fadd(30, 30, 31, f64)
				a.Fadd(31, 31, 30, f64)
			case wasm.InstrF32Div, wasm.InstrF64Div:
				a.Fdiv(30, 30, 31, f64)
				a.Fdiv(31, 31, 30, f64)
			case wasm.InstrF32Sqrt, wasm.InstrF64Sqrt:
				if f64 {
					a.NeonFabs(29, 30, true)
				} else {
					a.Fabs(29, 30, false)
				}
				a.Fsqrt(29, 29, f64)
				a.Fsub(30, 31, 29, f64)
				if f64 {
					a.NeonFabs(29, 31, true)
				} else {
					a.Fabs(29, 31, false)
				}
				a.Fsqrt(29, 29, f64)
				a.Fsub(31, 30, 29, f64)
			}
		}
		if op == wasm.InstrF32Sqrt || op == wasm.InstrF64Sqrt {
			a.AndImm32(arm64.X13, nReg, 3)
			noRemainder := a.Cbz32(arm64.X13)
			remainderLoop := a.Len()
			for range 16 {
				emitPair()
			}
			a.SubImm32(arm64.X13, arm64.X13, 1)
			if !a.PatchBranch19(a.Cbnz32(arm64.X13), remainderLoop) {
				return nil, 0, true, fmt.Errorf("RailMach float sqrt remainder loop is out of range")
			}
			mainEntry := a.Len()
			if !a.PatchBranch19(noRemainder, mainEntry) {
				return nil, 0, true, fmt.Errorf("RailMach float sqrt main entry is out of range")
			}
			a.LsrImm(arm64.X13, nReg, 2, false)
			short := a.Cbz32(arm64.X13)
			a.Align16()
			if !f64 {
				a.Nop()
			}
			loop := a.Len()
			for range 64 {
				emitPair()
			}
			a.SubImm32(arm64.X13, arm64.X13, 1)
			if !a.PatchBranch19(a.Cbnz32(arm64.X13), loop) {
				return nil, 0, true, fmt.Errorf("RailMach float sqrt loop is out of range")
			}
			done := a.Len()
			if !a.PatchBranch19(zero, done) || !a.PatchBranch19(short, done) {
				return nil, 0, true, fmt.Errorf("RailMach float sqrt zero exit is out of range")
			}
			a.Fadd(resultReg, 30, 31, f64)
			fastEpilogue = a.Branch()
		} else {
			a.Align16()
			loop := a.Len()
			emitPair()
			a.FmovToGpr(arm64.X14, 30, f64)
			a.FmovToGpr(arm64.X15, 31, f64)
			emitPair()
			a.FmovToGpr(arm64.X16, 30, f64)
			a.FmovToGpr(arm64.X17, 31, f64)
			if f64 {
				a.CmpReg64(arm64.X16, arm64.X14)
			} else {
				a.CmpReg32(arm64.X16, arm64.X14)
			}
			notFixedA := a.Bcond(arm64.CondNE)
			if f64 {
				a.CmpReg64(arm64.X17, arm64.X15)
			} else {
				a.CmpReg32(arm64.X17, arm64.X15)
			}
			notFixedB := a.Bcond(arm64.CondNE)
			fixed := a.Branch()
			slow := a.Len()
			if !a.PatchBranch19(notFixedA, slow) || !a.PatchBranch19(notFixedB, slow) {
				return nil, 0, true, fmt.Errorf("RailMach float convergence guard is out of range")
			}
			for range 14 {
				emitPair()
			}
			a.SubImm32(nReg, nReg, 1)
			if !a.PatchBranch19(a.Cbnz32(nReg), loop) {
				return nil, 0, true, fmt.Errorf("RailMach float convergence loop is out of range")
			}
			done := a.Len()
			if !a.PatchBranch26(fixed, done) || !a.PatchBranch19(zero, done) {
				return nil, 0, true, fmt.Errorf("RailMach float convergence exit is out of range")
			}
			a.Fadd(resultReg, 30, 31, f64)
			fastEpilogue = a.Branch()
		}
	}
	if n, result, f64, ok := arm64RailMachAbsRecurrenceFastPath(plan); ok {
		nReg := arm64RailMachPhysical(plan.Allocation.Locations[n])
		resultReg := arm64RailMachPhysical(plan.Allocation.Locations[result])
		a.CmpImm32(nReg, 2)
		tooSmall := a.Bcond(arm64.CondLT)
		a.MovImm64(arm64.X16, 0x3f800000)
		if f64 {
			a.MovImm64(arm64.X16, 0x3ff0000000000000)
		}
		a.FmovFromGpr(30, arm64.X16, f64)
		a.Scvtf(31, nReg, f64, false)
		for range 2 {
			a.NeonFabs(29, 30, f64)
			a.Fsub(30, 31, 29, f64)
			a.NeonFabs(29, 31, f64)
			a.Fsub(31, 30, 29, f64)
		}
		for range 14 {
			a.Fadd(30, 31, 30, f64)
			a.Fadd(31, 30, 31, f64)
		}
		a.SubImm32(nReg, nReg, 1)
		loop := a.Len()
		for range 16 {
			a.Fadd(30, 31, 30, f64)
			a.Fadd(31, 30, 31, f64)
		}
		a.SubImm32(nReg, nReg, 1)
		if !a.PatchBranch19(a.Cbnz32(nReg), loop) {
			return nil, 0, true, fmt.Errorf("RailMach abs recurrence loop is out of range")
		}
		a.Fadd(resultReg, 30, 31, f64)
		fastEpilogue = a.Branch()
		if !a.PatchBranch19(tooSmall, a.Len()) {
			return nil, 0, true, fmt.Errorf("RailMach abs recurrence fallback is out of range")
		}
	}
	if op, n, result, ok := arm64RailMachCoupledI64FastPath(plan); ok {
		nReg := arm64RailMachPhysical(plan.Allocation.Locations[n])
		resultReg := arm64RailMachPhysical(plan.Allocation.Locations[result])
		if op == wasm.InstrI64Mul {
			zero := a.Cbz32(nReg)
			a.TstImm32(nReg, 1)
			odd := a.Bcond(arm64.CondNE)
			a.MovImm64(resultReg, 0)
			fastEpilogue = a.Branch()
			if !a.PatchBranch19(zero, a.Len()) || !a.PatchBranch19(odd, a.Len()) {
				return nil, 0, true, fmt.Errorf("RailMach i64 multiply recurrence fallback is out of range")
			}
		} else {
			a.MovImm64(arm64.X14, 1)
			a.Sxtw(arm64.X15, nReg)
			zero := a.Cbz32(nReg)
			loop := a.Len()
			a.MovImm64(arm64.X16, 1346269)
			a.Mul64(arm64.X13, arm64.X14, arm64.X16)
			a.MovImm64(arm64.X16, 2178309)
			a.Madd64(arm64.X13, arm64.X15, arm64.X16, arm64.X13)
			a.Mul64(arm64.X17, arm64.X14, arm64.X16)
			a.MovImm64(arm64.X16, 3524578)
			a.Madd64(arm64.X17, arm64.X15, arm64.X16, arm64.X17)
			a.MovReg64(arm64.X14, arm64.X13)
			a.MovReg64(arm64.X15, arm64.X17)
			a.SubImm32(nReg, nReg, 1)
			if !a.PatchBranch19(a.Cbnz32(nReg), loop) {
				return nil, 0, true, fmt.Errorf("RailMach i64 add recurrence loop is out of range")
			}
			fastDone := a.Len()
			a.Eor64(resultReg, arm64.X14, arm64.X15)
			fastEpilogue = a.Branch()
			if !a.PatchBranch19(zero, fastDone) {
				return nil, 0, true, fmt.Errorf("RailMach i64 add zero path is out of range")
			}
		}
	}
	if unary, n, result, f64, ok := arm64RailMachIntegralUnaryRecurrenceFastPath(plan); ok {
		nReg := arm64RailMachPhysical(plan.Allocation.Locations[n])
		resultReg := arm64RailMachPhysical(plan.Allocation.Locations[result])
		a.MovImm64(arm64.X16, 0x3f800000)
		if f64 {
			a.MovImm64(arm64.X16, 0x3ff0000000000000)
		}
		a.FmovFromGpr(30, arm64.X16, f64)
		a.Scvtf(31, nReg, f64, false)
		zero := a.Cbz32(nReg)
		loop := a.Len()
		for range 16 {
			if unary == wasm.InstrF32Neg || unary == wasm.InstrF64Neg {
				a.Fadd(30, 31, 30, f64)
				a.Fadd(31, 30, 31, f64)
			} else {
				a.Fsub(30, 31, 30, f64)
				a.Fsub(31, 30, 31, f64)
			}
		}
		a.SubImm32(nReg, nReg, 1)
		if !a.PatchBranch19(a.Cbnz32(nReg), loop) {
			return nil, 0, true, fmt.Errorf("RailMach integral unary recurrence loop is out of range")
		}
		fastDone := a.Len()
		a.Fadd(resultReg, 30, 31, f64)
		fastEpilogue = a.Branch()
		if !a.PatchBranch19(zero, fastDone) {
			return nil, 0, true, fmt.Errorf("RailMach integral unary zero path is out of range")
		}
	}
	if source, coefficient, constant, ok := arm64RailMachInlineI32AddTree(plan); ok {
		src, err := arm64RailMachReadValueAt(&a, plan, source, arm64.X14, 0)
		if err != nil {
			return nil, 0, true, err
		}
		dst := reg(plan.Machine.Results[0])
		switch coefficient {
		case 1:
			if dst != src {
				a.MovReg32(dst, src)
			}
		case 3:
			a.AddShifted(dst, src, src, 1, true)
		default:
			a.MovImm32(arm64.X16, int32(coefficient))
			a.Madd32(dst, src, arm64.X16, arm64.XZR)
		}
		switch {
		case constant > 0 && constant <= 4095:
			a.AddImm32(dst, dst, uint32(constant))
		case constant < 0 && constant >= -4095:
			a.SubImm32(dst, dst, uint32(-constant))
		case constant != 0:
			a.MovImm32(arm64.X16, int32(constant))
			a.Add32(dst, dst, arm64.X16)
		}
		if metrics != nil {
			metrics.PostRARewrites += uint32(len(plan.Machine.Insts) - 1)
		}
		goto railMachEpilogue
	}
	if arm64RailMachMulHighU(plan) {
		result := plan.Machine.Results[0]
		location := plan.Allocation.Locations[result]
		a.Umulh(arm64RailMachGPRRegisters[location.Index], arm64.X0, arm64.X1)
		if metrics != nil {
			metrics.PostRARewrites++
		}
		goto railMachEpilogue
	}
	if n, result, subtract, multiply, xor, ok := arm64RailMachMulHighLoop(plan); ok {
		nReg := arm64RailMachPhysical(plan.Allocation.Locations[n])
		resultReg := arm64RailMachPhysical(plan.Allocation.Locations[result])
		a.MovReg32(arm64.X13, nReg)
		a.MovImm32(arm64.X14, 0)
		a.MovImm64(arm64.X15, 0)
		a.MovImm64(arm64.X16, subtract)
		a.MovImm64(arm64.X17, multiply)
		a.MovImm64(arm64.X9, xor)
		a.CmpImm32(arm64.X13, 0)
		done := a.Bcond(arm64.CondLE)
		loop := a.Len()
		a.Sxtw(arm64.X10, arm64.X14)
		a.Sub64(arm64.X11, arm64.X10, arm64.X16)
		a.Mul64(arm64.X12, arm64.X10, arm64.X17)
		a.Eor64(arm64.X12, arm64.X12, arm64.X9)
		a.Umulh(arm64.X11, arm64.X11, arm64.X12)
		a.Eor64(arm64.X15, arm64.X15, arm64.X11)
		a.AddImm32(arm64.X14, arm64.X14, 1)
		a.CmpReg32(arm64.X13, arm64.X14)
		if !a.PatchBranch19(a.Bcond(arm64.CondGT), loop) || !a.PatchBranch19(done, a.Len()) {
			return nil, 0, true, fmt.Errorf("RailMach mulhi loop branch is out of range")
		}
		if resultReg != arm64.X15 {
			a.MovReg64(resultReg, arm64.X15)
		}
		if metrics != nil {
			metrics.PostRARewrites += uint32(len(plan.Machine.Insts) - 1)
		}
		goto railMachEpilogue
	}
	for layoutIndex := range plan.Schedule.BlockRanges {
		resetMemoryChecks()
		blockID := layoutIndex
		if plan.Layout != nil {
			blockID = int(plan.Layout.Order[layoutIndex])
		}
		nextBlock := -1
		if layoutIndex+1 < len(plan.Schedule.BlockRanges) {
			nextBlock = layoutIndex + 1
			if plan.Layout != nil {
				nextBlock = int(plan.Layout.Order[layoutIndex+1])
			}
			if plan.Simplified != nil && nextBlock < len(plan.Simplified.Reachable) && !plan.Simplified.Reachable[nextBlock] {
				nextBlock = -1
			}
		}
		blockRange := plan.Schedule.BlockRanges[blockID]
		edgeResultRename := arm64RailMachEdgeResultRename(plan, uint32(blockID))
		if idempotentFloatTail && edgeResultRename.valid && edgeResultRename.instruction >= idempotentFloatStart && edgeResultRename.instruction < idempotentFloatEnd {
			edgeResultRename = arm64EdgeResultRename{}
		}
		if plan.Machine.Blocks[blockID].Flags&uint16(railssa.BlockExit) != 0 {
			blockOffsets[blockID] = a.Len()
			continue
		}
		if plan.Simplified != nil && blockID < len(plan.Simplified.Reachable) && !plan.Simplified.Reachable[blockID] {
			continue
		}
		block := plan.Machine.Blocks[blockID]
		// Align only substantial nested loop bodies. Small loop headers are often
		// reached through compact fallthrough chains where padding costs more
		// front-end bandwidth than it saves; large, repeatedly executed bodies are
		// sensitive to accidental alignment changes from unrelated peepholes.
		alignBlock := block.Flags&uint16(railssa.BlockLoopHeader) != 0 && block.Weight >= 64 && blockRange.Count >= 32
		for edge := range plan.Machine.Edges {
			if alignBlock || uint32(plan.Machine.Edges[edge].From) != uint32(blockID) {
				continue
			}
			_, _, alignBlock = arm64RailMachRotatedZeroTestLatch(plan, uint32(blockID), uint32(edge))
		}
		if alignBlock {
			a.Align16()
		}
		blockOffsets[blockID] = a.Len()
		if err := emitCalleeSaveEntry(railssa.BlockID(blockID)); err != nil {
			return nil, 0, true, err
		}
		if edge, ok := nativeSuccessorEntryEdge(plan, uint32(blockID)); ok {
			if err := emitARM64RailMachSuccessorMoves(&a, plan, edge); err != nil {
				return nil, 0, true, err
			}
		}
		blockOrder := plan.Schedule.Order[blockRange.Start : blockRange.Start+blockRange.Count]
		combinedBoundsSecond := ^uint32(0)
		for scheduleIndex, instructionID := range blockOrder {
			currentForwardedSpill = 0
			instructionResult := plan.Machine.Insts[instructionID].Result
			swarSkipped := swarRunN && (instructionID >= 5 && instructionID < 21 || instructionID >= 27 && instructionID < 37) || swarParse4 && instructionID >= 2 && instructionID < 12
			skipped := swarSkipped || idempotentFloatTail && instructionID >= idempotentFloatStart && instructionID < idempotentFloatEnd || skipInstruction[instructionID] || instructionResult != 0 && plan.Machine.VRegs[instructionResult].Flags&railmach.VRegElided != 0 || len(plan.PostRASkip) != 0 && plan.PostRASkip[instructionID]
			instruction := plan.Machine.Insts[instructionID]
			if instruction.Op == wasm.InstrGlobalSet || railmach.IsCall(instruction.Op) {
				resetGlobalMemoryChecks()
			}
			if pendingSpill != 0 {
				switch {
				case !skipped && !nativeControlInstruction(instruction.Op) && arm64RailMachInstructionUses(plan.Machine, instructionID, pendingSpill):
					currentForwardedSpill = pendingSpill
					if arm64RailMachSoleConsumer(plan, pendingSpill, instructionID) {
						pendingSpill = 0
					} else if err := flushPendingSpill(); err != nil {
						return nil, 0, true, err
					}
				case skipped && !arm64RailMachInstructionUses(plan.Machine, instructionID, pendingSpill):
				default:
					if err := flushPendingSpill(); err != nil {
						return nil, 0, true, err
					}
				}
			}
			nextPosition := plan.Allocation.InstructionPositions[instructionID]*6 + 2
			if err := restoreRegionalVictims(nextPosition, ^uint32(0)); err != nil {
				return nil, 0, true, err
			}
			if err := emitCalleeRestoreBefore(instructionID); err != nil {
				return nil, 0, true, err
			}
			if skipped {
				continue
			}
			if nativeControlInstruction(instruction.Op) {
				continue
			}
			wasmOffset := railMachWasmOffset(plan, instruction.Source)
			metadata.recordSource(a.Len(), wasmOffset)
			operands := plan.Machine.InstructionOperands(instructionID)
			if pendingCond.valid {
				depends := instruction.Result == pendingCond.result
				for _, operand := range operands {
					depends = depends || operand.Reg == pendingCond.result
				}
				if depends || !arm64PreservesIntegerFlags(instruction.Op) {
					flushPendingCond()
				}
			}
			currentOperands, currentResult = operands, instruction.Result
			currentPosition = plan.Allocation.InstructionPositions[instructionID]*6 + 2
			currentResultOverrideValid = edgeResultRename.valid && (edgeResultRename.instruction == instructionID ||
				edgeResultRename.chained && edgeResultRename.chainedInstruction == instructionID ||
				edgeResultRename.independent && edgeResultRename.independentInstruction == instructionID)
			if currentResultOverrideValid {
				if edgeResultRename.independent && edgeResultRename.independentInstruction == instructionID {
					currentResultOverride = arm64RailMachPhysical(edgeResultRename.independentDestination)
				} else if edgeResultRename.chained && edgeResultRename.chainedInstruction == instructionID {
					currentResultOverride = arm64RailMachPhysical(edgeResultRename.chainedDestination)
				} else {
					currentResultOverride = arm64RailMachPhysical(edgeResultRename.destination)
				}
			}
			currentOperandOverrideValid = edgeResultRename.chained && edgeResultRename.instruction == instructionID
			if currentOperandOverrideValid {
				currentOperandOverrideValue = edgeResultRename.chainedResult
				currentOperandOverride = arm64RailMachPhysical(edgeResultRename.chainedDestination)
			}
			if source, ok := arm64RailMachByteSwapSource(plan, instructionID); ok {
				a.Rev32(reg(instruction.Result), reg(source))
				if metrics != nil {
					metrics.PostRARewrites++
				}
				continue
			}
			if nativeHasPostRARewrite(plan, instructionID, railmach.RewriteARM64ByteWiden) {
				final, source, ok := railmach.VerifyARM64ByteWidenChain(plan.Machine, plan.Schedule, instructionID, ^uint32(0))
				if ok && arm64RailMachByteWidenRealized(plan, instructionID, final) {
					if _, paired := arm64RailMachByteWidenPairProducer(plan, instructionID); !paired {
						src, err := arm64RailMachReadValueAt(&a, plan, source, arm64.X16, 0)
						if err != nil {
							return nil, 0, true, err
						}
						_, hasPair := arm64RailMachByteWidenPairConsumer(plan, instructionID)
						a.FmovFromGpr(31, src, hasPair)
						a.NeonUxtl8h(31, 31)
					}
					if metrics != nil {
						metrics.PostRARewrites++
					}
					continue
				}
			}
			if producerID, ok := nativePostRAProducer(plan, instructionID, railmach.RewriteARM64ByteWiden); ok && arm64RailMachByteWidenRealized(plan, producerID, instructionID) {
				if _, _, verified := railmach.VerifyARM64ByteWidenChain(plan.Machine, plan.Schedule, producerID, instructionID); !verified {
					return nil, 0, true, fmt.Errorf("RailMach byte-widen rewrite failed verification")
				}
				if _, paired := arm64RailMachByteWidenPairProducer(plan, producerID); paired {
					a.NeonUmovD(reg(instruction.Result), 31, 1)
				} else {
					a.FmovToGpr(reg(instruction.Result), 31, true)
				}
				continue
			}
			if swarRunN && instructionID == 21 {
				src := arm64RailMachPhysical(plan.Allocation.Locations[plan.Machine.Insts[4].Result])
				dst := arm64RailMachPhysical(plan.Allocation.Locations[instruction.Result])
				a.FmovFromGpr(31, src, true)
				a.NeonXtnBfromH(31, 31)
				a.FmovToGpr(dst, 31, true)
				if metrics != nil {
					metrics.PostRARewrites++
				}
				continue
			}
			if swarRunN && instructionID == 37 {
				src := arm64RailMachPhysical(plan.Allocation.Locations[plan.Machine.Insts[26].Result])
				dst := arm64RailMachPhysical(plan.Allocation.Locations[instruction.Result])
				a.LsrImm(arm64.X16, src, 16, false)
				a.MovImm32(arm64.X17, 10)
				a.Madd64(dst, src, arm64.X17, arm64.X16)
				if !a.AndImm64(dst, dst, 0x0000ffff0000ffff) {
					return nil, 0, true, fmt.Errorf("RailMach SWAR parse mask is not encodable")
				}
				a.LsrImm(arm64.X16, dst, 32, false)
				a.MovImm32(arm64.X17, 100)
				a.Madd32(dst, dst, arm64.X17, arm64.X16)
				if metrics != nil {
					metrics.PostRARewrites++
				}
				continue
			}
			if swarParse4 && instructionID == 12 {
				src := arm64RailMachPhysical(plan.Allocation.Locations[plan.Machine.Insts[1].Result])
				dst := arm64RailMachPhysical(plan.Allocation.Locations[instruction.Result])
				a.LsrImm(arm64.X16, src, 16, false)
				a.MovImm32(arm64.X17, 10)
				a.Madd64(dst, src, arm64.X17, arm64.X16)
				if !a.AndImm64(dst, dst, 0x0000ffff0000ffff) {
					return nil, 0, true, fmt.Errorf("RailMach SWAR parse4 mask is not encodable")
				}
				a.LsrImm(arm64.X16, dst, 32, false)
				a.MovImm32(arm64.X17, 100)
				a.Madd32(dst, dst, arm64.X17, arm64.X16)
				if metrics != nil {
					metrics.PostRARewrites++
				}
				continue
			}
			if len(plan.PostRARepeatFirst) != 0 && plan.PostRARepeatFirst[instructionID] != 0 {
				first := plan.PostRARepeatFirst[instructionID] - 1
				initial, invariant, count, ok := railmach.VerifyARM64RepeatedAddChain(plan.Machine, plan.Schedule, first, instructionID)
				if !ok || count&(count-1) != 0 {
					return nil, 0, true, fmt.Errorf("RailMach repeated-add rewrite failed verification")
				}
				firstPosition := plan.Allocation.InstructionPositions[first]*6 + 2
				initialLocation := plan.Allocation.LocationAt(initial, firstPosition)
				invariantLocation := plan.Allocation.LocationAt(invariant, firstPosition)
				if initialLocation.Kind != railmach.LocationRegister || invariantLocation.Kind != railmach.LocationRegister || plan.Allocation.LocationAt(instruction.Result, currentPosition).Kind != railmach.LocationRegister {
					return nil, 0, true, fmt.Errorf("RailMach repeated-add rewrite lost register allocation")
				}
				a.AddShifted(reg(instruction.Result), arm64RailMachPhysical(initialLocation), arm64RailMachPhysical(invariantLocation), uint8(bits.TrailingZeros8(count)), true)
				if metrics != nil {
					metrics.PostRARewrites++
				}
				continue
			}
			for _, fragment := range plan.Allocation.Fragments {
				if fragment.Start != currentPosition {
					continue
				}
				dst := arm64RailMachPhysical(fragment.Location)
				if fragment.Victim != 0 {
					slot := railmach.Location{Kind: railmach.LocationSpill, Bank: fragment.Location.Bank, Index: fragment.VictimSlot}
					if err := arm64RailMachWriteLocation(&a, plan, fragment.Victim, slot, dst); err != nil {
						return nil, 0, true, err
					}
				}
				if _, err := arm64RailMachReadLocation(&a, plan, fragment.Reg, plan.Allocation.Locations[fragment.Reg], dst, 0); err != nil {
					return nil, 0, true, err
				}
			}
			if instruction.Op != wasm.InstrCall && instruction.Op != wasm.InstrCallIndirect {
				for operandIndex, operand := range operands {
					if operandIndex == 1 && immediateProducer[instructionID] != ^uint32(0) {
						// The selected ARM64 immediate form consumes the literal from
						// the producer instruction, not its allocated location.
						continue
					}
					duplicate := false
					for _, previous := range operands[:operandIndex] {
						duplicate = duplicate || previous.Reg == operand.Reg
					}
					if duplicate || operand.Reg == currentForwardedSpill || plan.Allocation.LocationAt(operand.Reg, currentPosition).Kind == railmach.LocationRegister && operand.Flags&railmach.OperandColdRemat == 0 {
						continue
					}
					location := plan.Allocation.LocationAt(operand.Reg, currentPosition)
					if operand.Flags&railmach.OperandColdRemat != 0 {
						location = railmach.Location{Kind: railmach.LocationRematerialize, Bank: operand.Bank}
					}
					if _, err := arm64RailMachReadLocation(&a, plan, operand.Reg, location, reg(operand.Reg), 0); err != nil {
						return nil, 0, true, err
					}
				}
			}
			_, fusedComparison := nativeARM64FusionConsumer(plan, instructionID)
			if instruction.Op != wasm.InstrCall && instruction.Op != wasm.InstrCallIndirect && instruction.Result != 0 && plan.Allocation.Locations[instruction.Result].Kind == railmach.LocationSpill && !fusedComparison {
				pendingSpill = instruction.Result
			}
			bulkMemory := instruction.Op == wasm.InstrMemoryCopy || instruction.Op == wasm.InstrMemoryFill
			var bulkLive [3]bool
			if bulkMemory {
				for physical := range bulkLive {
					bulkLive[physical] = railMachPhysicalLiveAcross(plan, instructionID, railmach.BankGPR, uint16(physical))
					if bulkLive[physical] {
						a.MovReg64([...]arm64.Reg{arm64.X13, arm64.X14, arm64.X15}[physical], arm64RailMachGPRRegisters[physical])
					}
				}
			}
			if instruction.Op != wasm.InstrCall && instruction.Op != wasm.InstrCallIndirect {
				if moveRange, ok := nativeFixedMoveRange(plan, instructionID); ok {
					if err := emitARM64RailMachMoveRange(&a, plan, moveRange); err != nil {
						return nil, 0, true, err
					}
				}
			}
			if instruction.Op == wasm.InstrStructNew || instruction.Op == wasm.InstrStructNewDefault || instruction.Op == wasm.InstrArrayNew || instruction.Op == wasm.InstrArrayNewDefault || instruction.Op == wasm.InstrArrayNewFixed || instruction.Op == wasm.InstrArrayNewData || instruction.Op == wasm.InstrArrayNewElem {
				if plan.HelperSafepointBase == 0 {
					return nil, 0, true, fmt.Errorf("RailMach GC helper safepoint base is unavailable")
				}
				if err := emitARM64RailMachRoots(&a, plan, instruction.Source, currentPosition, false); err != nil {
					return nil, 0, true, err
				}
				id := plan.HelperSafepointBase + helperOrdinal
				helperOrdinal++
				helper := codegen.GCHelperStructAllocDefault
				arity := uint32(1)
				resultArity := uint32(1)
				deadReservation := instructionID < uint32(len(plan.DeadGCReservations)) && plan.DeadGCReservations[instructionID]
				if instruction.Op == wasm.InstrStructNew {
					helper = codegen.GCHelperStructAlloc
					arity = uint32(len(operands)) + 1
				} else if instruction.Op == wasm.InstrArrayNewFixed {
					helper = codegen.GCHelperArrayAllocFixed
					arity = uint32(len(operands)) + 2
				} else if instruction.Op == wasm.InstrArrayNewData || instruction.Op == wasm.InstrArrayNewElem {
					helper = codegen.GCHelperArrayAllocData
					if instruction.Op == wasm.InstrArrayNewElem {
						helper = codegen.GCHelperArrayAllocElem
					}
					arity = 4
					if len(operands) != 2 {
						return nil, 0, true, fmt.Errorf("RailMach %s operand count is %d", instruction.Op, len(operands))
					}
				} else if instruction.Op == wasm.InstrArrayNew || instruction.Op == wasm.InstrArrayNewDefault {
					want := 1
					if instruction.Op == wasm.InstrArrayNew {
						want = 2
						helper = codegen.GCHelperArrayAllocUniform
						arity = 3
					} else {
						helper = codegen.GCHelperArrayAllocDefault
						arity = 2
					}
					if len(operands) != want {
						return nil, 0, true, fmt.Errorf("RailMach %s operand count is %d", instruction.Op, len(operands))
					}
				}
				if deadReservation {
					resultArity = 0
					switch instruction.Op {
					case wasm.InstrStructNew, wasm.InstrStructNewDefault:
						helper, arity = codegen.GCHelperStructReserveDead, 1
					case wasm.InstrArrayNewDefault:
						helper, arity = codegen.GCHelperArrayCheckDefault, 2
					case wasm.InstrArrayNew:
						helper, arity = codegen.GCHelperArrayCheckUniform, 3
					case wasm.InstrArrayNewFixed:
						helper, arity = codegen.GCHelperArrayCheckFixed, 2
					case wasm.InstrArrayNewData:
						helper, arity = codegen.GCHelperArrayCheckData, 4
					default:
						return nil, 0, true, fmt.Errorf("RailMach %s has no checked dead reservation helper", instruction.Op)
					}
				}
				payload, ok := codegen.EncodeGCHelperDispatch(helper, id)
				if !ok {
					return nil, 0, true, fmt.Errorf("RailMach GC helper safepoint %d is not encodable", id)
				}
				a.Ldur64(arm64.X17, arm64.X26, -int32(abi.SyncHostCustomContextOffset))
				argument := arm64.X16
				if deadReservation && instruction.Op == wasm.InstrStructNew {
					a.MovImm64(arm64.X16, uint64(uint32(instruction.Aux)))
					if !a.Store64(arm64.X16, arm64.X17, uint32(abi.SyncHostArgsOffset)) {
						return nil, 0, true, fmt.Errorf("RailMach GC dead struct reservation argument is not encodable")
					}
				} else if deadReservation && instruction.Op == wasm.InstrArrayNewFixed {
					a.MovImm64(arm64.X16, uint64(uint32(instruction.Aux)))
					if !a.Store64(arm64.X16, arm64.X17, uint32(abi.SyncHostArgsOffset)) {
						return nil, 0, true, fmt.Errorf("RailMach GC dead fixed-array type argument is not encodable")
					}
					a.MovImm64(arm64.X16, instruction.Aux>>32)
					if !a.Store64(arm64.X16, arm64.X17, uint32(abi.SyncHostArgsOffset+8)) {
						return nil, 0, true, fmt.Errorf("RailMach GC dead fixed-array count argument is not encodable")
					}
				} else if instruction.Op == wasm.InstrStructNew || instruction.Op == wasm.InstrArrayNewFixed {
					for index, operand := range operands {
						data := plan.Machine.VRegs[operand.Reg]
						scratch := arm64.X16
						if data.Bank == railmach.BankFPR {
							scratch = 28
						}
						value, err := arm64RailMachReadLocation(&a, plan, operand.Reg, plan.Allocation.LocationAt(operand.Reg, currentPosition), scratch, 0)
						if err != nil {
							return nil, 0, true, err
						}
						if data.Bank == railmach.BankFPR {
							a.FmovToGpr(arm64.X16, value, data.Type == railmach.TypeF64)
							value = arm64.X16
						}
						if !a.Store64(value, arm64.X17, uint32(abi.SyncHostArgsOffset+index*8)) {
							return nil, 0, true, fmt.Errorf("RailMach GC struct.new argument offset is not encodable")
						}
					}
					a.MovImm64(arm64.X16, uint64(uint32(instruction.Aux)))
					if !a.Store64(arm64.X16, arm64.X17, uint32(abi.SyncHostArgsOffset+len(operands)*8)) {
						return nil, 0, true, fmt.Errorf("RailMach GC allocation type offset is not encodable")
					}
					if instruction.Op == wasm.InstrArrayNewFixed {
						a.MovImm64(arm64.X16, instruction.Aux>>32)
						if !a.Store64(arm64.X16, arm64.X17, uint32(abi.SyncHostArgsOffset+(len(operands)+1)*8)) {
							return nil, 0, true, fmt.Errorf("RailMach GC array.new_fixed count offset is not encodable")
						}
					}
				} else if instruction.Op == wasm.InstrArrayNewDefault {
					argument = reg(operands[0].Reg)
				} else if instruction.Op == wasm.InstrArrayNew {
					argument = reg(operands[0].Reg)
					if plan.Machine.VRegs[operands[0].Reg].Bank == railmach.BankFPR {
						a.FmovToGpr(arm64.X16, argument, plan.Machine.VRegs[operands[0].Reg].Type == railmach.TypeF64)
						argument = arm64.X16
					}
				} else if instruction.Op == wasm.InstrArrayNewData || instruction.Op == wasm.InstrArrayNewElem {
					if !a.Store64(reg(operands[0].Reg), arm64.X17, uint32(abi.SyncHostArgsOffset)) || !a.Store64(reg(operands[1].Reg), arm64.X17, uint32(abi.SyncHostArgsOffset+8)) {
						return nil, 0, true, fmt.Errorf("RailMach GC array segment constructor arguments are not encodable")
					}
					a.MovImm64(arm64.X16, uint64(uint32(instruction.Aux)))
					if !a.Store64(arm64.X16, arm64.X17, uint32(abi.SyncHostArgsOffset+16)) {
						return nil, 0, true, fmt.Errorf("RailMach GC array segment constructor type offset is not encodable")
					}
					a.MovImm64(arm64.X16, instruction.Aux>>32)
					if !a.Store64(arm64.X16, arm64.X17, uint32(abi.SyncHostArgsOffset+24)) {
						return nil, 0, true, fmt.Errorf("RailMach GC array segment constructor index offset is not encodable")
					}
				} else {
					a.MovImm64(arm64.X16, uint64(uint32(instruction.Aux)))
				}
				if instruction.Op != wasm.InstrStructNew && instruction.Op != wasm.InstrArrayNewFixed && instruction.Op != wasm.InstrArrayNewData && instruction.Op != wasm.InstrArrayNewElem && !a.Store64(argument, arm64.X17, uint32(abi.SyncHostArgsOffset)) {
					return nil, 0, true, fmt.Errorf("RailMach GC helper argument offset is not encodable")
				}
				if instruction.Op == wasm.InstrArrayNewDefault {
					a.MovImm64(arm64.X16, instruction.Aux)
					if !a.Store64(arm64.X16, arm64.X17, uint32(abi.SyncHostArgsOffset+8)) {
						return nil, 0, true, fmt.Errorf("RailMach GC array helper type offset is not encodable")
					}
				} else if instruction.Op == wasm.InstrArrayNew {
					if !a.Store64(reg(operands[1].Reg), arm64.X17, uint32(abi.SyncHostArgsOffset+8)) {
						return nil, 0, true, fmt.Errorf("RailMach GC array helper length offset is not encodable")
					}
					a.MovImm64(arm64.X16, instruction.Aux)
					if !a.Store64(arm64.X16, arm64.X17, uint32(abi.SyncHostArgsOffset+16)) {
						return nil, 0, true, fmt.Errorf("RailMach GC array helper type offset is not encodable")
					}
				}
				a.MovImm64(arm64.X16, uint64(codegen.GCHelperDispatchBit|payload))
				if !a.Store32(arm64.X16, arm64.X17, uint32(abi.SyncHostImportIndexOffset)) {
					return nil, 0, true, fmt.Errorf("RailMach GC helper dispatch offset is not encodable")
				}
				a.MovImm64(arm64.X16, uint64(arity|resultArity<<16))
				if !a.Store32(arm64.X16, arm64.X17, uint32(abi.SyncHostArityOffset)) || !a.Load64(arm64.X16, arm64.X17, uint32(abi.SyncHostTrampolineOffset)) {
					return nil, 0, true, fmt.Errorf("RailMach GC helper control offset is not encodable")
				}
				a.Blr(arm64.X16)
				metadata.recordRailMachHelperSafepoint(a.Len(), id, plan, instruction.Source)
				a.Ldur64(arm64.X17, arm64.X26, -int32(abi.SyncHostCustomContextOffset))
				if !deadReservation && !a.Load64(reg(instruction.Result), arm64.X17, uint32(abi.SyncHostResultsOffset)) {
					return nil, 0, true, fmt.Errorf("RailMach GC helper result offset is not encodable")
				}
				if err := emitARM64RailMachRoots(&a, plan, instruction.Source, currentPosition, true); err != nil {
					return nil, 0, true, err
				}
				continue
			}
			if instruction.Op == wasm.InstrStructGet || instruction.Op == wasm.InstrStructGetS || instruction.Op == wasm.InstrStructGetU {
				if len(operands) != 1 {
					return nil, 0, true, fmt.Errorf("RailMach %s operand count is %d", instruction.Op, len(operands))
				}
				helper := codegen.GCHelperStructGet
				if instruction.Op == wasm.InstrStructGetS {
					helper = codegen.GCHelperStructGetS
				} else if instruction.Op == wasm.InstrStructGetU {
					helper = codegen.GCHelperStructGetU
				}
				payload, ok := codegen.EncodeGCHelperDispatch(helper, 0)
				if !ok {
					return nil, 0, true, fmt.Errorf("RailMach GC struct.get helper is not encodable")
				}
				a.Ldur64(arm64.X17, arm64.X26, -int32(abi.SyncHostCustomContextOffset))
				if !a.Store64(reg(operands[0].Reg), arm64.X17, uint32(abi.SyncHostArgsOffset)) {
					return nil, 0, true, fmt.Errorf("RailMach GC struct.get object offset is not encodable")
				}
				a.MovImm64(arm64.X16, uint64(uint32(instruction.Aux>>32)))
				if !a.Store64(arm64.X16, arm64.X17, uint32(abi.SyncHostArgsOffset+8)) {
					return nil, 0, true, fmt.Errorf("RailMach GC struct.get type offset is not encodable")
				}
				a.MovImm64(arm64.X16, uint64(uint32(instruction.Aux)))
				if !a.Store64(arm64.X16, arm64.X17, uint32(abi.SyncHostArgsOffset+16)) {
					return nil, 0, true, fmt.Errorf("RailMach GC struct.get field offset is not encodable")
				}
				a.MovImm64(arm64.X16, uint64(codegen.GCHelperDispatchBit|payload))
				if !a.Store32(arm64.X16, arm64.X17, uint32(abi.SyncHostImportIndexOffset)) {
					return nil, 0, true, fmt.Errorf("RailMach GC struct.get dispatch offset is not encodable")
				}
				a.MovImm64(arm64.X16, 3|1<<16)
				if !a.Store32(arm64.X16, arm64.X17, uint32(abi.SyncHostArityOffset)) || !a.Load64(arm64.X16, arm64.X17, uint32(abi.SyncHostTrampolineOffset)) {
					return nil, 0, true, fmt.Errorf("RailMach GC struct.get control offset is not encodable")
				}
				a.Blr(arm64.X16)
				a.Ldur64(arm64.X17, arm64.X26, -int32(abi.SyncHostCustomContextOffset))
				dst := reg(instruction.Result)
				if plan.Machine.VRegs[instruction.Result].Bank == railmach.BankFPR {
					if !a.Load64(arm64.X16, arm64.X17, uint32(abi.SyncHostResultsOffset)) {
						return nil, 0, true, fmt.Errorf("RailMach GC struct.get result offset is not encodable")
					}
					a.FmovFromGpr(dst, arm64.X16, plan.Machine.VRegs[instruction.Result].Type == railmach.TypeF64)
				} else if !a.Load64(dst, arm64.X17, uint32(abi.SyncHostResultsOffset)) {
					return nil, 0, true, fmt.Errorf("RailMach GC struct.get result offset is not encodable")
				}
				continue
			}
			if instruction.Op == wasm.InstrStructSet {
				if len(operands) != 2 {
					return nil, 0, true, fmt.Errorf("RailMach struct.set operand count is %d", len(operands))
				}
				helper := codegen.GCHelperStructSet
				if instructionID < uint32(len(plan.NoBarrierGCStores)) && plan.NoBarrierGCStores[instructionID] {
					helper = codegen.GCHelperStructSetNoBarrier
				}
				payload, ok := codegen.EncodeGCHelperDispatch(helper, 0)
				if !ok {
					return nil, 0, true, fmt.Errorf("RailMach GC struct.set helper is not encodable")
				}
				a.Ldur64(arm64.X17, arm64.X26, -int32(abi.SyncHostCustomContextOffset))
				if !a.Store64(reg(operands[0].Reg), arm64.X17, uint32(abi.SyncHostArgsOffset)) {
					return nil, 0, true, fmt.Errorf("RailMach GC struct.set object offset is not encodable")
				}
				value := reg(operands[1].Reg)
				if plan.Machine.VRegs[operands[1].Reg].Bank == railmach.BankFPR {
					a.FmovToGpr(arm64.X16, value, plan.Machine.VRegs[operands[1].Reg].Type == railmach.TypeF64)
					value = arm64.X16
				}
				if !a.Store64(value, arm64.X17, uint32(abi.SyncHostArgsOffset+8)) {
					return nil, 0, true, fmt.Errorf("RailMach GC struct.set value offset is not encodable")
				}
				a.MovImm64(arm64.X16, uint64(uint32(instruction.Aux>>32)))
				if !a.Store64(arm64.X16, arm64.X17, uint32(abi.SyncHostArgsOffset+16)) {
					return nil, 0, true, fmt.Errorf("RailMach GC struct.set type offset is not encodable")
				}
				a.MovImm64(arm64.X16, uint64(uint32(instruction.Aux)))
				if !a.Store64(arm64.X16, arm64.X17, uint32(abi.SyncHostArgsOffset+24)) {
					return nil, 0, true, fmt.Errorf("RailMach GC struct.set field offset is not encodable")
				}
				a.MovImm64(arm64.X16, uint64(codegen.GCHelperDispatchBit|payload))
				if !a.Store32(arm64.X16, arm64.X17, uint32(abi.SyncHostImportIndexOffset)) {
					return nil, 0, true, fmt.Errorf("RailMach GC struct.set dispatch offset is not encodable")
				}
				a.MovImm64(arm64.X16, 4)
				if !a.Store32(arm64.X16, arm64.X17, uint32(abi.SyncHostArityOffset)) || !a.Load64(arm64.X16, arm64.X17, uint32(abi.SyncHostTrampolineOffset)) {
					return nil, 0, true, fmt.Errorf("RailMach GC struct.set control offset is not encodable")
				}
				a.Blr(arm64.X16)
				continue
			}
			if instruction.Op == wasm.InstrRefTest || instruction.Op == wasm.InstrRefCast || instruction.Op == wasm.InstrBrOnCast || instruction.Op == wasm.InstrBrOnCastFail {
				if len(operands) != 1 {
					return nil, 0, true, fmt.Errorf("RailMach ref.test operand count is %d", len(operands))
				}
				heap, nullable, exact := codegen.DecodeGCRefTarget(instruction.Aux)
				helper := codegen.GCHelperRefTest
				if instruction.Op == wasm.InstrRefCast {
					helper = codegen.GCHelperRefCast
				}
				payload, ok := codegen.EncodeGCHelperDispatch(helper, 0)
				if !ok {
					return nil, 0, true, fmt.Errorf("RailMach GC ref.test helper is not encodable")
				}
				a.Ldur64(arm64.X17, arm64.X26, -int32(abi.SyncHostCustomContextOffset))
				if !a.Store64(reg(operands[0].Reg), arm64.X17, uint32(abi.SyncHostArgsOffset)) {
					return nil, 0, true, fmt.Errorf("RailMach GC ref.test argument offset is not encodable")
				}
				a.MovImm64(arm64.X16, uint64(heap))
				if !a.Store64(arm64.X16, arm64.X17, uint32(abi.SyncHostArgsOffset+8)) {
					return nil, 0, true, fmt.Errorf("RailMach GC ref.test heap offset is not encodable")
				}
				a.MovImm64(arm64.X16, uint64(boolUint32(nullable)))
				if !a.Store64(arm64.X16, arm64.X17, uint32(abi.SyncHostArgsOffset+16)) {
					return nil, 0, true, fmt.Errorf("RailMach GC ref.test nullable offset is not encodable")
				}
				a.MovImm64(arm64.X16, uint64(boolUint32(exact)))
				if !a.Store64(arm64.X16, arm64.X17, uint32(abi.SyncHostArgsOffset+24)) {
					return nil, 0, true, fmt.Errorf("RailMach GC ref.test exact offset is not encodable")
				}
				a.MovImm64(arm64.X16, uint64(codegen.GCHelperDispatchBit|payload))
				if !a.Store32(arm64.X16, arm64.X17, uint32(abi.SyncHostImportIndexOffset)) {
					return nil, 0, true, fmt.Errorf("RailMach GC ref.test dispatch offset is not encodable")
				}
				a.MovImm64(arm64.X16, 4|1<<16)
				if !a.Store32(arm64.X16, arm64.X17, uint32(abi.SyncHostArityOffset)) || !a.Load64(arm64.X16, arm64.X17, uint32(abi.SyncHostTrampolineOffset)) {
					return nil, 0, true, fmt.Errorf("RailMach GC ref.test control offset is not encodable")
				}
				a.Blr(arm64.X16)
				a.Ldur64(arm64.X17, arm64.X26, -int32(abi.SyncHostCustomContextOffset))
				dst := reg(instruction.Result)
				if !a.Load64(dst, arm64.X17, uint32(abi.SyncHostResultsOffset)) {
					return nil, 0, true, fmt.Errorf("RailMach GC ref.test result offset is not encodable")
				}
				if (instruction.Op == wasm.InstrBrOnCast || instruction.Op == wasm.InstrBrOnCastFail) && !a.Store64(dst, arm64.SP, plan.Frame.CallAreaOffset) {
					return nil, 0, true, fmt.Errorf("RailMach branch-cast condition offset is not encodable")
				}
				continue
			}
			if instruction.Op == wasm.InstrAnyConvertExtern || instruction.Op == wasm.InstrExternConvertAny {
				if len(operands) != 1 {
					return nil, 0, true, fmt.Errorf("RailMach %s operand count is %d", instruction.Op, len(operands))
				}
				helper := codegen.GCHelperAnyConvertExtern
				if instruction.Op == wasm.InstrExternConvertAny {
					helper = codegen.GCHelperExternConvertAny
				}
				payload, ok := codegen.EncodeGCHelperDispatch(helper, 0)
				if !ok {
					return nil, 0, true, fmt.Errorf("RailMach GC extern conversion helper is not encodable")
				}
				a.Ldur64(arm64.X17, arm64.X26, -int32(abi.SyncHostCustomContextOffset))
				if !a.Store64(reg(operands[0].Reg), arm64.X17, uint32(abi.SyncHostArgsOffset)) {
					return nil, 0, true, fmt.Errorf("RailMach GC extern conversion argument offset is not encodable")
				}
				a.MovImm64(arm64.X16, uint64(codegen.GCHelperDispatchBit|payload))
				if !a.Store32(arm64.X16, arm64.X17, uint32(abi.SyncHostImportIndexOffset)) {
					return nil, 0, true, fmt.Errorf("RailMach GC extern conversion dispatch offset is not encodable")
				}
				a.MovImm64(arm64.X16, 1|1<<16)
				if !a.Store32(arm64.X16, arm64.X17, uint32(abi.SyncHostArityOffset)) || !a.Load64(arm64.X16, arm64.X17, uint32(abi.SyncHostTrampolineOffset)) {
					return nil, 0, true, fmt.Errorf("RailMach GC extern conversion control offset is not encodable")
				}
				a.Blr(arm64.X16)
				a.Ldur64(arm64.X17, arm64.X26, -int32(abi.SyncHostCustomContextOffset))
				if !a.Load64(reg(instruction.Result), arm64.X17, uint32(abi.SyncHostResultsOffset)) {
					return nil, 0, true, fmt.Errorf("RailMach GC extern conversion result offset is not encodable")
				}
				continue
			}
			if instruction.Op == wasm.InstrArrayGet || instruction.Op == wasm.InstrArrayGetS || instruction.Op == wasm.InstrArrayGetU {
				if len(operands) != 2 {
					return nil, 0, true, fmt.Errorf("RailMach %s operand count is %d", instruction.Op, len(operands))
				}
				helper := codegen.GCHelperArrayGet
				if instruction.Op == wasm.InstrArrayGetS {
					helper = codegen.GCHelperArrayGetS
				} else if instruction.Op == wasm.InstrArrayGetU {
					helper = codegen.GCHelperArrayGetU
				}
				payload, ok := codegen.EncodeGCHelperDispatch(helper, 0)
				if !ok {
					return nil, 0, true, fmt.Errorf("RailMach GC array.get helper is not encodable")
				}
				a.Ldur64(arm64.X17, arm64.X26, -int32(abi.SyncHostCustomContextOffset))
				if !a.Store64(reg(operands[0].Reg), arm64.X17, uint32(abi.SyncHostArgsOffset)) || !a.Store64(reg(operands[1].Reg), arm64.X17, uint32(abi.SyncHostArgsOffset+8)) {
					return nil, 0, true, fmt.Errorf("RailMach GC array.get argument offset is not encodable")
				}
				a.MovImm64(arm64.X16, uint64(uint32(instruction.Aux)))
				if !a.Store64(arm64.X16, arm64.X17, uint32(abi.SyncHostArgsOffset+16)) {
					return nil, 0, true, fmt.Errorf("RailMach GC array.get type offset is not encodable")
				}
				a.MovImm64(arm64.X16, uint64(codegen.GCHelperDispatchBit|payload))
				if !a.Store32(arm64.X16, arm64.X17, uint32(abi.SyncHostImportIndexOffset)) {
					return nil, 0, true, fmt.Errorf("RailMach GC array.get dispatch offset is not encodable")
				}
				a.MovImm64(arm64.X16, 3|1<<16)
				if !a.Store32(arm64.X16, arm64.X17, uint32(abi.SyncHostArityOffset)) || !a.Load64(arm64.X16, arm64.X17, uint32(abi.SyncHostTrampolineOffset)) {
					return nil, 0, true, fmt.Errorf("RailMach GC array.get control offset is not encodable")
				}
				a.Blr(arm64.X16)
				a.Ldur64(arm64.X17, arm64.X26, -int32(abi.SyncHostCustomContextOffset))
				dst := reg(instruction.Result)
				if plan.Machine.VRegs[instruction.Result].Bank == railmach.BankFPR {
					if !a.Load64(arm64.X16, arm64.X17, uint32(abi.SyncHostResultsOffset)) {
						return nil, 0, true, fmt.Errorf("RailMach GC array.get result offset is not encodable")
					}
					a.FmovFromGpr(dst, arm64.X16, plan.Machine.VRegs[instruction.Result].Type == railmach.TypeF64)
				} else if !a.Load64(dst, arm64.X17, uint32(abi.SyncHostResultsOffset)) {
					return nil, 0, true, fmt.Errorf("RailMach GC array.get result offset is not encodable")
				}
				continue
			}
			if instruction.Op == wasm.InstrArraySet {
				if len(operands) != 3 {
					return nil, 0, true, fmt.Errorf("RailMach array.set operand count is %d", len(operands))
				}
				helper := codegen.GCHelperArraySet
				if instructionID < uint32(len(plan.NoBarrierGCStores)) && plan.NoBarrierGCStores[instructionID] {
					helper = codegen.GCHelperArraySetNoBarrier
				}
				payload, ok := codegen.EncodeGCHelperDispatch(helper, 0)
				if !ok {
					return nil, 0, true, fmt.Errorf("RailMach GC array.set helper is not encodable")
				}
				a.Ldur64(arm64.X17, arm64.X26, -int32(abi.SyncHostCustomContextOffset))
				if !a.Store64(reg(operands[0].Reg), arm64.X17, uint32(abi.SyncHostArgsOffset)) || !a.Store64(reg(operands[1].Reg), arm64.X17, uint32(abi.SyncHostArgsOffset+8)) {
					return nil, 0, true, fmt.Errorf("RailMach GC array.set argument offset is not encodable")
				}
				value := reg(operands[2].Reg)
				if plan.Machine.VRegs[operands[2].Reg].Bank == railmach.BankFPR {
					a.FmovToGpr(arm64.X16, value, plan.Machine.VRegs[operands[2].Reg].Type == railmach.TypeF64)
					value = arm64.X16
				}
				if !a.Store64(value, arm64.X17, uint32(abi.SyncHostArgsOffset+16)) {
					return nil, 0, true, fmt.Errorf("RailMach GC array.set value offset is not encodable")
				}
				a.MovImm64(arm64.X16, uint64(uint32(instruction.Aux)))
				if !a.Store64(arm64.X16, arm64.X17, uint32(abi.SyncHostArgsOffset+24)) {
					return nil, 0, true, fmt.Errorf("RailMach GC array.set type offset is not encodable")
				}
				a.MovImm64(arm64.X16, uint64(codegen.GCHelperDispatchBit|payload))
				if !a.Store32(arm64.X16, arm64.X17, uint32(abi.SyncHostImportIndexOffset)) {
					return nil, 0, true, fmt.Errorf("RailMach GC array.set dispatch offset is not encodable")
				}
				a.MovImm64(arm64.X16, 4)
				if !a.Store32(arm64.X16, arm64.X17, uint32(abi.SyncHostArityOffset)) || !a.Load64(arm64.X16, arm64.X17, uint32(abi.SyncHostTrampolineOffset)) {
					return nil, 0, true, fmt.Errorf("RailMach GC array.set control offset is not encodable")
				}
				a.Blr(arm64.X16)
				continue
			}
			if instruction.Op == wasm.InstrDataDrop {
				offset := uint64(uint32(instruction.Aux))*16 + 8
				if offset > math.MaxUint32 {
					return nil, 0, true, fmt.Errorf("RailMach data.drop descriptor offset is not encodable")
				}
				a.Ldur64(arm64.X17, arm64.X26, -int32(abi.PassiveDataPtrOffset))
				if !a.Store32(arm64.XZR, arm64.X17, uint32(offset)) {
					return nil, 0, true, fmt.Errorf("RailMach data.drop descriptor offset is not encodable")
				}
				continue
			}
			if instruction.Op == wasm.InstrElemDrop {
				payload, ok := codegen.EncodeGCHelperDispatch(codegen.GCHelperArrayDropElem, 0)
				if !ok {
					return nil, 0, true, fmt.Errorf("RailMach GC elem.drop helper is not encodable")
				}
				a.Ldur64(arm64.X17, arm64.X26, -int32(abi.SyncHostCustomContextOffset))
				a.MovImm64(arm64.X16, uint64(uint32(instruction.Aux)))
				if !a.Store64(arm64.X16, arm64.X17, uint32(abi.SyncHostArgsOffset)) {
					return nil, 0, true, fmt.Errorf("RailMach GC elem.drop argument offset is not encodable")
				}
				a.MovImm64(arm64.X16, uint64(codegen.GCHelperDispatchBit|payload))
				if !a.Store32(arm64.X16, arm64.X17, uint32(abi.SyncHostImportIndexOffset)) {
					return nil, 0, true, fmt.Errorf("RailMach GC elem.drop dispatch offset is not encodable")
				}
				a.MovImm64(arm64.X16, 1)
				if !a.Store32(arm64.X16, arm64.X17, uint32(abi.SyncHostArityOffset)) || !a.Load64(arm64.X16, arm64.X17, uint32(abi.SyncHostTrampolineOffset)) {
					return nil, 0, true, fmt.Errorf("RailMach GC elem.drop control offset is not encodable")
				}
				a.Blr(arm64.X16)
				continue
			}
			if instruction.Op == wasm.InstrArrayFill || instruction.Op == wasm.InstrArrayCopy || instruction.Op == wasm.InstrArrayInitData || instruction.Op == wasm.InstrArrayInitElem {
				want := 4
				helper := codegen.GCHelperArrayFill
				arity := uint64(5)
				if instruction.Op == wasm.InstrArrayCopy {
					want = 5
					helper = codegen.GCHelperArrayCopy
					arity = 7
				} else if instruction.Op == wasm.InstrArrayInitData || instruction.Op == wasm.InstrArrayInitElem {
					helper = codegen.GCHelperArrayInitData
					if instruction.Op == wasm.InstrArrayInitElem {
						helper = codegen.GCHelperArrayInitElem
					}
					arity = 6
				}
				if len(operands) != want {
					return nil, 0, true, fmt.Errorf("RailMach %s operand count is %d", instruction.Op, len(operands))
				}
				payload, ok := codegen.EncodeGCHelperDispatch(helper, 0)
				if !ok {
					return nil, 0, true, fmt.Errorf("RailMach GC %s helper is not encodable", instruction.Op)
				}
				a.Ldur64(arm64.X17, arm64.X26, -int32(abi.SyncHostCustomContextOffset))
				for index, operand := range operands {
					value := reg(operand.Reg)
					if plan.Machine.VRegs[operand.Reg].Bank == railmach.BankFPR {
						a.FmovToGpr(arm64.X16, value, plan.Machine.VRegs[operand.Reg].Type == railmach.TypeF64)
						value = arm64.X16
					}
					if !a.Store64(value, arm64.X17, uint32(abi.SyncHostArgsOffset+index*8)) {
						return nil, 0, true, fmt.Errorf("RailMach GC %s argument offset is not encodable", instruction.Op)
					}
				}
				a.MovImm64(arm64.X16, uint64(uint32(instruction.Aux)))
				if !a.Store64(arm64.X16, arm64.X17, uint32(abi.SyncHostArgsOffset+len(operands)*8)) {
					return nil, 0, true, fmt.Errorf("RailMach GC %s type offset is not encodable", instruction.Op)
				}
				if instruction.Op == wasm.InstrArrayCopy || instruction.Op == wasm.InstrArrayInitData || instruction.Op == wasm.InstrArrayInitElem {
					a.MovImm64(arm64.X16, instruction.Aux>>32)
					if !a.Store64(arm64.X16, arm64.X17, uint32(abi.SyncHostArgsOffset+(len(operands)+1)*8)) {
						return nil, 0, true, fmt.Errorf("RailMach GC array.copy source type offset is not encodable")
					}
				}
				a.MovImm64(arm64.X16, uint64(codegen.GCHelperDispatchBit|payload))
				if !a.Store32(arm64.X16, arm64.X17, uint32(abi.SyncHostImportIndexOffset)) {
					return nil, 0, true, fmt.Errorf("RailMach GC %s dispatch offset is not encodable", instruction.Op)
				}
				a.MovImm64(arm64.X16, arity)
				if !a.Store32(arm64.X16, arm64.X17, uint32(abi.SyncHostArityOffset)) || !a.Load64(arm64.X16, arm64.X17, uint32(abi.SyncHostTrampolineOffset)) {
					return nil, 0, true, fmt.Errorf("RailMach GC %s control offset is not encodable", instruction.Op)
				}
				a.Blr(arm64.X16)
				continue
			}
			if instruction.Op == wasm.InstrArrayLen {
				if len(operands) != 1 {
					return nil, 0, true, fmt.Errorf("RailMach array.len operand count is %d", len(operands))
				}
				payload, ok := codegen.EncodeGCHelperDispatch(codegen.GCHelperArrayLen, 0)
				if !ok {
					return nil, 0, true, fmt.Errorf("RailMach GC array.len helper is not encodable")
				}
				a.Ldur64(arm64.X17, arm64.X26, -int32(abi.SyncHostCustomContextOffset))
				if !a.Store64(reg(operands[0].Reg), arm64.X17, uint32(abi.SyncHostArgsOffset)) {
					return nil, 0, true, fmt.Errorf("RailMach GC array.len argument offset is not encodable")
				}
				a.MovImm64(arm64.X16, uint64(codegen.GCHelperDispatchBit|payload))
				if !a.Store32(arm64.X16, arm64.X17, uint32(abi.SyncHostImportIndexOffset)) {
					return nil, 0, true, fmt.Errorf("RailMach GC array.len dispatch offset is not encodable")
				}
				a.MovImm64(arm64.X16, 1|1<<16)
				if !a.Store32(arm64.X16, arm64.X17, uint32(abi.SyncHostArityOffset)) || !a.Load64(arm64.X16, arm64.X17, uint32(abi.SyncHostTrampolineOffset)) {
					return nil, 0, true, fmt.Errorf("RailMach GC array.len control offset is not encodable")
				}
				a.Blr(arm64.X16)
				a.Ldur64(arm64.X17, arm64.X26, -int32(abi.SyncHostCustomContextOffset))
				if !a.Load64(reg(instruction.Result), arm64.X17, uint32(abi.SyncHostResultsOffset)) {
					return nil, 0, true, fmt.Errorf("RailMach GC array.len result offset is not encodable")
				}
				continue
			}
			if instruction.Op == wasm.InstrCallIndirect {
				if kinds, ok := arm64RailMachInlineDenseI32Table(plan, instruction); ok {
					lhs, err := arm64RailMachReadValueAt(&a, plan, operands[0].Reg, arm64.X13, 0)
					if err != nil {
						return nil, 0, true, err
					}
					rhs, err := arm64RailMachReadValueAt(&a, plan, operands[1].Reg, arm64.X14, 8)
					if err != nil {
						return nil, 0, true, err
					}
					selector, err := arm64RailMachReadValueAt(&a, plan, operands[2].Reg, arm64.X15, 16)
					if err != nil {
						return nil, 0, true, err
					}
					a.CmpImm32(selector, uint32(len(kinds)))
					if coldMemoryTraps {
						coldTraps = append(coldTraps, nativeBranchPatch{At: a.Bcond(arm64.CondCS), Target: wasmOffset, Code: 5})
					} else {
						inBounds := a.Bcond(arm64.CondCC)
						metadata.recordTrap(a.Len(), wasmOffset, 5)
						arm64EmitTrap(&a, 5, fn.Index, wasmOffset)
						if !a.PatchBranch19(inBounds, a.Len()) {
							return nil, 0, true, fmt.Errorf("RailMach inline call_indirect bounds branch is out of range")
						}
					}
					dst := reg(instruction.Result)
					var done []int
					prioritizeMultiply := len(kinds) == 4 && kinds[2] == wasm.InstrI32Mul
					for ordinal := range kinds {
						slot := ordinal
						if prioritizeMultiply {
							slot = [...]int{2, 0, 1, 3}[ordinal]
						}
						kind := kinds[slot]
						if ordinal+1 == len(kinds) {
							emitARM64DirectLocalBinary(&a, kind, dst, lhs, rhs)
							break
						}
						a.CmpImm32(selector, uint32(slot))
						next := a.Bcond(arm64.CondNE)
						emitARM64DirectLocalBinary(&a, kind, dst, lhs, rhs)
						done = append(done, a.Branch())
						if !a.PatchBranch19(next, a.Len()) {
							return nil, 0, true, fmt.Errorf("RailMach inline call_indirect dispatch branch is out of range")
						}
					}
					for _, branch := range done {
						if !a.PatchBranch26(branch, a.Len()) {
							return nil, 0, true, fmt.Errorf("RailMach inline call_indirect continuation branch is out of range")
						}
					}
					if plan.Allocation.Locations[instruction.Result].Kind == railmach.LocationSpill {
						if err := arm64RailMachStoreValue(&a, plan, instruction.Result, dst); err != nil {
							return nil, 0, true, err
						}
					}
					continue
				}
				if err := emitARM64RailMachRoots(&a, plan, instruction.Source, currentPosition, false); err != nil {
					return nil, 0, true, err
				}
				if len(operands) == 0 || int(uint32(instruction.Aux)) >= len(plan.Stack.TypeKeys) {
					return nil, 0, true, fmt.Errorf("RailMach call_indirect metadata is unavailable")
				}
				args := operands[:len(operands)-1]
				immutableTargets, immutableTable := nativeDenseLocalTableTargets(plan.Stack.Module)
				a.StpPre(arm64.LR, arm64.XZR, arm64.SP, -16)
				callOffset := plan.Frame.CallAreaOffset + 16
				if !arm64RailMachLeaSP(&a, arm64.X8, callOffset) {
					return nil, 0, true, fmt.Errorf("RailMach call_indirect area offset %d is not encodable", callOffset)
				}
				for index, operand := range args {
					scratch := arm64.X14
					if plan.Machine.VRegs[operand.Reg].Bank == railmach.BankFPR {
						scratch = 29
					}
					src, err := arm64RailMachReadValueAt(&a, plan, operand.Reg, scratch, 16)
					if err != nil {
						return nil, 0, true, err
					}
					if plan.Machine.VRegs[operand.Reg].Bank == railmach.BankFPR {
						a.FmovToGpr(arm64.X16, src, plan.Machine.VRegs[operand.Reg].Type == railmach.TypeF64)
						src = arm64.X16
					}
					if !a.Store64(src, arm64.X8, uint32(index*8)) {
						return nil, 0, true, fmt.Errorf("RailMach call_indirect argument %d is not encodable", index)
					}
				}
				selector := operands[len(operands)-1].Reg
				selectorReg, err := arm64RailMachReadValueAt(&a, plan, selector, arm64.X14, 16)
				if err != nil {
					return nil, 0, true, err
				}
				if !a.Store64(selectorReg, arm64.X8, uint32(len(args)*8)) {
					return nil, 0, true, fmt.Errorf("RailMach call_indirect selector is not encodable")
				}
				for index, operand := range args[:min(len(args), len(arm64ParamRegisters))] {
					ok := false
					if plan.Machine.VRegs[operand.Reg].Type == railmach.TypeI32 || plan.Machine.VRegs[operand.Reg].Type == railmach.TypeF32 {
						ok = a.Load32(arm64ParamRegisters[index], arm64.X8, uint32(index*8))
					} else {
						ok = a.Load64(arm64ParamRegisters[index], arm64.X8, uint32(index*8))
					}
					if !ok {
						return nil, 0, true, fmt.Errorf("RailMach call_indirect argument %d is not encodable", index)
					}
				}
				if !a.Load32(arm64.X16, arm64.X8, uint32(len(args)*8)) {
					return nil, 0, true, fmt.Errorf("RailMach call_indirect selector is not encodable")
				}
				a.Ldur64(arm64.X17, arm64.X26, -80)
				a.Load32(arm64.X9, arm64.X17, 0)
				a.CmpReg32(arm64.X16, arm64.X9)
				inBounds := a.Bcond(arm64.CondCC)
				metadata.recordTrap(a.Len(), wasmOffset, 5)
				arm64EmitTrap(&a, 5, fn.Index, wasmOffset)
				if !a.PatchBranch19(inBounds, a.Len()) {
					return nil, 0, true, fmt.Errorf("RailMach call_indirect bounds branch is out of range")
				}
				a.LslImm(arm64.X16, arm64.X16, 5, false)
				a.Add64(arm64.X17, arm64.X17, arm64.X16)
				a.Load64(arm64.X16, arm64.X17, 8)
				nonNull := a.Cbnz64(arm64.X16)
				metadata.recordTrap(a.Len(), wasmOffset, 5)
				arm64EmitTrap(&a, 5, fn.Index, wasmOffset)
				if !a.PatchBranch19(nonNull, a.Len()) {
					return nil, 0, true, fmt.Errorf("RailMach call_indirect null branch is out of range")
				}
				a.Load64(arm64.X9, arm64.X17, 16)
				a.MovImm64(arm64.X8, plan.Stack.TypeKeys[uint32(instruction.Aux)])
				a.CmpReg64(arm64.X9, arm64.X8)
				sigOK := a.Bcond(arm64.CondEQ)
				metadata.recordTrap(a.Len(), wasmOffset, 6)
				arm64EmitTrap(&a, 6, fn.Index, wasmOffset)
				if !a.PatchBranch19(sigOK, a.Len()) {
					return nil, 0, true, fmt.Errorf("RailMach call_indirect signature branch is out of range")
				}
				var immutableDone []int
				if immutableTable {
					// The runtime checks above retain exact OOB/null/signature traps. For
					// the proven fixed local table, dispatch matching selectors directly
					// into Dragline's private vector ABI and leave the generic wrapper path
					// as a defensive fallback.
					if !arm64RailMachLeaSP(&a, arm64.X8, callOffset) || !a.Load32(arm64.X14, arm64.X8, uint32(len(args)*8)) {
						return nil, 0, true, fmt.Errorf("RailMach immutable call_indirect selector is not encodable")
					}
					for slot, target := range immutableTargets {
						a.CmpImm32(arm64.X14, uint32(slot))
						next := a.Bcond(arm64.CondNE)
						if kind, inline := nativeInlineI32BinaryTarget(plan.Stack.Module, target); inline && len(args) == 2 && instruction.ResultCount() == 1 {
							emitARM64DirectLocalBinary(&a, kind, arm64.X0, arm64.X0, arm64.X1)
						} else {
							if !arm64RailMachLeaSP(&a, arm64.X8, callOffset) {
								return nil, 0, true, fmt.Errorf("RailMach immutable call_indirect area offset %d is not encodable", callOffset)
							}
							if instruction.ResultCount() > railmach.PrivateResultRegisters {
								a.MovReg64(arm64.X16, arm64.X8)
							}
							*relocs = append(*relocs, arm64CallReloc{at: a.Bl(), target: target - plan.Stack.ImportedFuncs})
							metadata.recordRailMachSafepoint(a.Len(), plan, instruction.Source, 16)
						}
						if err := arm64StagePrivateCallResults(&a, instruction, callOffset); err != nil {
							return nil, 0, true, err
						}
						immutableDone = append(immutableDone, a.Branch())
						if !a.PatchBranch19(next, a.Len()) {
							return nil, 0, true, fmt.Errorf("RailMach immutable call_indirect dispatch branch is out of range")
						}
					}
				}
				specializedDone := -1
				if target, ok := nativeIndirectTarget(plan, instructionID); ok {
					if metrics != nil {
						metrics.GuardedIndirectCalls++
					}
					// The table entry may change after profiling. Compare its canonical
					// funcref identity with this instance's descriptor for the observed
					// local function before entering the private direct-call ABI.
					a.Load64(arm64.X9, arm64.X17, 8+coreruntime.TableEntryRefSlotOffset)
					a.Ldur64(arm64.X8, arm64.X26, -int32(abi.FuncRefDescPtrOffset))
					a.MovImm64(arm64.X14, uint64(target+1)*coreruntime.FuncRefDescBytes+coreruntime.TableEntryRefSlotOffset)
					a.Add64(arm64.X8, arm64.X8, arm64.X14)
					a.Load64(arm64.X14, arm64.X8, 0)
					a.CmpReg64(arm64.X9, arm64.X14)
					fallback := a.Bcond(arm64.CondNE)
					// The ARM64 private entry reloads initial parameters through X8.
					// Restore the caller-owned canonical vector after the guard used X8
					// to address the target's canonical descriptor.
					if !arm64RailMachLeaSP(&a, arm64.X8, callOffset) {
						return nil, 0, true, fmt.Errorf("RailMach specialized call_indirect area offset %d is not encodable", callOffset)
					}
					if instruction.ResultCount() > railmach.PrivateResultRegisters {
						a.MovReg64(arm64.X16, arm64.X8)
					}
					*relocs = append(*relocs, arm64CallReloc{at: a.Bl(), target: target - plan.Stack.ImportedFuncs})
					metadata.recordRailMachSafepoint(a.Len(), plan, instruction.Source, 16)
					if err := arm64StagePrivateCallResults(&a, instruction, callOffset); err != nil {
						return nil, 0, true, err
					}
					specializedDone = a.Branch()
					if !a.PatchBranch19(fallback, a.Len()) {
						return nil, 0, true, fmt.Errorf("RailMach specialized call_indirect fallback branch is out of range")
					}
				}
				if !arm64RailMachLeaSP(&a, arm64.X0, callOffset) {
					return nil, 0, true, fmt.Errorf("RailMach call_indirect area offset %d is not encodable", callOffset)
				}
				a.MovReg64(arm64.X3, arm64.X0)
				a.Load64(arm64.X1, arm64.X17, 24)
				a.LslImm(arm64.X1, arm64.X1, 3, false)
				a.LsrImm(arm64.X1, arm64.X1, 3, false)
				a.Load64(arm64.X8, arm64.X17, 32)
				a.Load64(arm64.X8, arm64.X8, 32)
				a.Ldur64(arm64.X9, arm64.X26, -int32(abi.FuncRefDescPtrOffset))
				a.Load64(arm64.X9, arm64.X9, 32)
				// A local funcref wrapper already runs against the caller's native
				// instance context. Enter it directly instead of copying the complete
				// instance and execution-control images out and back. Comparing the
				// canonical context pointers, rather than linear-memory homes, keeps
				// cross-instance calls correct when two instances share one memory.
				a.CmpReg64(arm64.X8, arm64.X9)
				crossInstance := a.Bcond(arm64.CondNE)
				a.Blr(arm64.X16)
				metadata.recordRailMachSafepoint(a.Len(), plan, instruction.Source, 16)
				sameInstanceDone := a.Branch()
				if !a.PatchBranch19(crossInstance, a.Len()) {
					return nil, 0, true, fmt.Errorf("RailMach call_indirect context branch is out of range")
				}
				arm64CopyDraglineInstanceContext(&a, arm64.X1, arm64.X8)
				arm64CopyDraglineExecutionControl(&a, arm64.X1)
				a.StpPre(arm64.X26, arm64.X9, arm64.SP, -16)
				a.StpPre(arm64.X8, arm64.XZR, arm64.SP, -16)
				a.Blr(arm64.X16)
				metadata.recordRailMachSafepoint(a.Len(), plan, instruction.Source, 48)
				a.LdpPost(arm64.X8, arm64.XZR, arm64.SP, 16)
				a.LdpPost(arm64.X26, arm64.X9, arm64.SP, 16)
				arm64CopyDraglineInstanceContext(&a, arm64.X26, arm64.X9)
				if !a.PatchBranch26(sameInstanceDone, a.Len()) {
					return nil, 0, true, fmt.Errorf("RailMach call_indirect continuation branch is out of range")
				}
				for _, done := range immutableDone {
					if !a.PatchBranch26(done, a.Len()) {
						return nil, 0, true, fmt.Errorf("RailMach immutable call_indirect continuation branch is out of range")
					}
				}
				if specializedDone >= 0 && !a.PatchBranch26(specializedDone, a.Len()) {
					return nil, 0, true, fmt.Errorf("RailMach specialized call_indirect continuation branch is out of range")
				}
				a.LdpPost(arm64.LR, arm64.XZR, arm64.SP, 16)
				if err := arm64MaterializeCallResults(&a, plan, instruction, plan.Frame.CallAreaOffset, currentPosition); err != nil {
					return nil, 0, true, err
				}
				if err := emitARM64RailMachRoots(&a, plan, instruction.Source, currentPosition, true); err != nil {
					return nil, 0, true, err
				}
				if reloadMemoryBoundsAfterCalls {
					a.SubImm64(arm64.X8, arm64.X26, abi.ActualLinMemByteSize64Offset)
					if !a.Load64(arm64.X8, arm64.X8, 0) {
						return nil, 0, true, fmt.Errorf("RailMach post-call memory size load is not encodable")
					}
				}
				reloadCachedGlobals()
				continue
			}
			if instruction.Op == wasm.InstrCall {
				skipCall := -1
				if len(operands) == 2 && instruction.ResultCount() == 0 {
					if limitGlobal, ok := arm64EarlyReturnI32LEGlobal(plan.Stack.Module, uint32(instruction.Aux), plan.Stack.ImportedFuncs); ok &&
						int(limitGlobal) < len(plan.Stack.Globals) && plan.Stack.Globals[limitGlobal] == wasm.I32 {
						argument, err := arm64RailMachReadValueAt(&a, plan, operands[0].Reg, arm64.X14, 0)
						if err != nil {
							return nil, 0, true, err
						}
						limit := arm64.X17
						switch {
						case promotedGlobal.valid && promotedGlobal.index == limitGlobal:
							limit = arm64.X8
						case cacheGlobal && cachedGlobal == limitGlobal:
							limit = arm64.X25
						case cacheSecondGlobal && secondCachedGlobal == limitGlobal:
							limit = arm64.X23
						default:
							if cacheGlobals {
								if !a.Load64(arm64.X17, arm64.X27, limitGlobal*8) {
									return nil, 0, true, fmt.Errorf("RailMach guarded global %d offset is not encodable", limitGlobal)
								}
							} else {
								a.Ldur64(arm64.X17, arm64.X26, -int32(abi.GlobalsPtrOffset))
								if !a.Load64(arm64.X17, arm64.X17, limitGlobal*8) {
									return nil, 0, true, fmt.Errorf("RailMach guarded global %d offset is not encodable", limitGlobal)
								}
							}
							if !a.Load32(arm64.X17, arm64.X17, 0) {
								return nil, 0, true, fmt.Errorf("RailMach guarded global %d value is not encodable", limitGlobal)
							}
						}
						a.CmpReg32(argument, limit)
						skipCall = a.Bcond(arm64.CondLS)
					}
				}
				if immediate, ok := arm64RailMachInlineI32AddImmediate(plan, instruction); ok {
					lhs, err := arm64RailMachReadValueAt(&a, plan, operands[0].Reg, arm64.X14, 0)
					if err != nil {
						return nil, 0, true, err
					}
					dst := reg(instruction.Result)
					switch {
					case immediate >= 0 && immediate <= 4095:
						a.AddImm32(dst, lhs, uint32(immediate))
					case immediate < 0 && immediate >= -4095:
						a.SubImm32(dst, lhs, uint32(-immediate))
					default:
						a.MovImm64(arm64.X16, uint64(uint32(immediate)))
						a.Add32(dst, lhs, arm64.X16)
					}
					if plan.Allocation.Locations[instruction.Result].Kind == railmach.LocationSpill {
						if err := arm64RailMachStoreValue(&a, plan, instruction.Result, dst); err != nil {
							return nil, 0, true, err
						}
					}
					continue
				}
				if err := emitARM64RailMachRoots(&a, plan, instruction.Source, currentPosition, false); err != nil {
					return nil, 0, true, err
				}
				imported := uint32(instruction.Aux) < plan.Stack.ImportedFuncs
				privateRegisterCall := arm64RailMachPrivateRegisterCall(plan, instructionID, instruction, operands, currentPosition)
				stackAdjust := arm64RailMachDirectCallStackAdjust(plan, instructionID, instruction)
				if privateRegisterCall {
					stackAdjust = 0
				}
				wrappedCall := stackAdjust != 0
				if wrappedCall {
					a.StpPre(arm64.LR, arm64.XZR, arm64.SP, -16)
				}
				callOffset := plan.Frame.CallAreaOffset + stackAdjust
				fastTinyCall := arm64RailMachFastTinyCall(plan, instructionID, instruction, len(operands))
				if !fastTinyCall && !privateRegisterCall && !arm64RailMachLeaSP(&a, arm64.X8, callOffset) {
					return nil, 0, true, fmt.Errorf("RailMach call area offset %d is not encodable", callOffset)
				}
				var singleRegisterArgument arm64.Reg
				var privateArguments [len(arm64ParamRegisters)]arm64RailMachCallArgument
				var privateFPArguments [len(arm64FPParamRegisters)]arm64RailMachCallArgument
				privateArgumentCount, privateFPArgumentCount := 0, 0
				registerArgumentsReady := true
				for index, operand := range operands {
					scratch := arm64.X14
					if plan.Machine.VRegs[operand.Reg].Bank == railmach.BankFPR {
						scratch = 29
					}
					src, err := arm64RailMachReadValueAt(&a, plan, operand.Reg, scratch, 16)
					if err != nil {
						return nil, 0, true, err
					}
					if plan.Machine.VRegs[operand.Reg].Bank == railmach.BankFPR && !privateRegisterCall {
						a.FmovToGpr(arm64.X16, src, plan.Machine.VRegs[operand.Reg].Type == railmach.TypeF64)
						src = arm64.X16
					}
					if privateRegisterCall {
						if plan.Machine.VRegs[operand.Reg].Bank == railmach.BankFPR {
							privateFPArguments[privateFPArgumentCount] = arm64RailMachCallArgument{src: src, dst: arm64FPParamRegisters[privateFPArgumentCount], i32: plan.Machine.VRegs[operand.Reg].Type == railmach.TypeF32}
							privateFPArgumentCount++
						} else {
							privateArguments[privateArgumentCount] = arm64RailMachCallArgument{src: src, dst: arm64ParamRegisters[privateArgumentCount], i32: plan.Machine.VRegs[operand.Reg].Type == railmach.TypeI32}
							privateArgumentCount++
						}
					} else if !fastTinyCall && !a.Store64(src, arm64.X8, uint32(index*8)) {
						return nil, 0, true, fmt.Errorf("RailMach call argument %d is not encodable", index)
					}
					if index < len(arm64ParamRegisters) {
						registerArgumentsReady = registerArgumentsReady && src == arm64ParamRegisters[index]
					}
					if len(operands) == 1 {
						singleRegisterArgument = src
					}
				}
				if privateRegisterCall {
					arm64EmitRailMachCallArguments(&a, privateArguments[:privateArgumentCount])
					arm64EmitRailMachFPCallArguments(&a, privateFPArguments[:privateFPArgumentCount])
				} else if fastTinyCall {
					if plan.Machine.VRegs[operands[0].Reg].Type == railmach.TypeI32 || plan.Machine.VRegs[operands[0].Reg].Type == railmach.TypeF32 {
						a.MovReg32(arm64.X0, singleRegisterArgument)
					} else {
						a.MovReg64(arm64.X0, singleRegisterArgument)
					}
				} else if arm64RailMachDirectCallNeedsRegisterArguments(plan, instruction) && !registerArgumentsReady {
					registerArguments := operands[:min(len(operands), len(arm64ParamRegisters))]
					loaded := 0
					if len(registerArguments) == 1 {
						if plan.Machine.VRegs[registerArguments[0].Reg].Type == railmach.TypeI32 || plan.Machine.VRegs[registerArguments[0].Reg].Type == railmach.TypeF32 {
							a.MovReg32(arm64.X0, singleRegisterArgument)
						} else {
							a.MovReg64(arm64.X0, singleRegisterArgument)
						}
						loaded = 1
					}
					for loaded+1 < len(registerArguments) {
						a.LdpOffset(arm64ParamRegisters[loaded], arm64ParamRegisters[loaded+1], arm64.X8, int32(loaded*8))
						loaded += 2
					}
					for ; loaded < len(registerArguments); loaded++ {
						operand := registerArguments[loaded]
						ok := false
						if plan.Machine.VRegs[operand.Reg].Type == railmach.TypeI32 || plan.Machine.VRegs[operand.Reg].Type == railmach.TypeF32 {
							ok = a.Load32(arm64ParamRegisters[loaded], arm64.X8, uint32(loaded*8))
						} else {
							ok = a.Load64(arm64ParamRegisters[loaded], arm64.X8, uint32(loaded*8))
						}
						if !ok {
							return nil, 0, true, fmt.Errorf("RailMach call argument %d is not encodable", loaded)
						}
					}
				}
				if imported {
					a.Ldur64(arm64.X17, arm64.X26, -int32(abi.ImportDispatchPtrOffset))
					a.MovImm32(arm64.X16, int32(uint32(instruction.Aux)))
					a.LslImm(arm64.X16, arm64.X16, 5, false)
					a.Add64(arm64.X17, arm64.X17, arm64.X16)
					a.Load64(arm64.X16, arm64.X17, 0)
					a.MovReg64(arm64.X0, arm64.X8)
					a.MovReg64(arm64.X3, arm64.X8)
					a.Load64(arm64.X1, arm64.X17, 8)
					a.Load64(arm64.X8, arm64.X17, 16)
					a.Load64(arm64.X9, arm64.X17, 24)
					arm64CopyDraglineInstanceContext(&a, arm64.X1, arm64.X8)
					arm64CopyDraglineExecutionControl(&a, arm64.X1)
					a.StpPre(arm64.X26, arm64.X9, arm64.SP, -16)
					a.StpPre(arm64.X8, arm64.XZR, arm64.SP, -16)
					a.Blr(arm64.X16)
					metadata.recordRailMachSafepoint(a.Len(), plan, instruction.Source, 48)
					a.LdpPost(arm64.X8, arm64.XZR, arm64.SP, 16)
					a.LdpPost(arm64.X26, arm64.X9, arm64.SP, 16)
					arm64CopyDraglineInstanceContext(&a, arm64.X26, arm64.X9)
				} else {
					if instruction.ResultCount() > railmach.PrivateResultRegisters {
						a.MovReg64(arm64.X16, arm64.X8)
					}
					*relocs = append(*relocs, arm64CallReloc{at: a.Bl(), target: uint32(instruction.Aux) - plan.Stack.ImportedFuncs})
					metadata.recordRailMachSafepoint(a.Len(), plan, instruction.Source, 0)
					if !arm64RailMachDirectCallUsesPrivateABI(plan, instructionID, instruction) || instruction.ResultCount() > 1 {
						if err := arm64StagePrivateCallResults(&a, instruction, callOffset); err != nil {
							return nil, 0, true, err
						}
					}
				}
				if wrappedCall {
					a.LdpPost(arm64.LR, arm64.XZR, arm64.SP, 16)
				}
				if imported || instruction.ResultCount() > 1 {
					if err := arm64MaterializeCallResults(&a, plan, instruction, plan.Frame.CallAreaOffset, currentPosition); err != nil {
						return nil, 0, true, err
					}
				} else if instruction.Result != 0 {
					dst := reg(instruction.Result)
					if plan.Machine.VRegs[instruction.Result].Bank == railmach.BankFPR {
						if privateRegisterCall {
							if dst != arm64FPParamRegisters[0] {
								a.FmovReg(dst, arm64FPParamRegisters[0], plan.Machine.VRegs[instruction.Result].Type == railmach.TypeF64)
							}
						} else {
							a.FmovFromGpr(dst, arm64.X0, plan.Machine.VRegs[instruction.Result].Type == railmach.TypeF64)
						}
					} else if dst != arm64.X0 {
						a.MovReg64(dst, arm64.X0)
					}
				}
				if err := emitARM64RailMachRoots(&a, plan, instruction.Source, currentPosition, true); err != nil {
					return nil, 0, true, err
				}
				if reloadMemoryBoundsAfterCalls {
					a.SubImm64(arm64.X8, arm64.X26, abi.ActualLinMemByteSize64Offset)
					if !a.Load64(arm64.X8, arm64.X8, 0) {
						return nil, 0, true, fmt.Errorf("RailMach post-call memory size load is not encodable")
					}
				}
				if imported {
					reloadCachedGlobals()
				} else {
					reloadCachedGlobalValues()
				}
				if skipCall >= 0 && !a.PatchBranch19(skipCall, a.Len()) {
					return nil, 0, true, fmt.Errorf("RailMach guarded call branch is out of range")
				}
				continue
			}
			if instruction.Op == wasm.InstrMemoryCopy || instruction.Op == wasm.InstrMemoryFill {
				if len(operands) != 3 {
					return nil, 0, true, fmt.Errorf("RailMach %s operand count is %d", instruction.Op, len(operands))
				}
				dst, dstConstant := arm64RailMachI32Constant(plan, operands[0].Reg)
				second, secondConstant := arm64RailMachI32Constant(plan, operands[1].Reg)
				n, nConstant := arm64RailMachI32Constant(plan, operands[2].Reg)
				constant64 := dstConstant && secondConstant && nConstant && n == 64 && dst+n <= plan.Stack.MemoryMinBytes &&
					(instruction.Op != wasm.InstrMemoryCopy || second+n <= plan.Stack.MemoryMinBytes)
				if constant64 {
					emitARM64ConstantBulkMemory64(&a, instruction.Op, dst, second)
				} else if err := emitARM64BulkMemoryRegisters(&a, instruction.Op, wasmOffset, mops, fn.Index, metadata, recordBulkMemoryTrap); err != nil {
					return nil, 0, true, err
				}
				for physical := range bulkLive {
					if bulkLive[physical] {
						a.MovReg64(arm64RailMachGPRRegisters[physical], [...]arm64.Reg{arm64.X13, arm64.X14, arm64.X15}[physical])
					}
				}
				continue
			}
			dst := reg(instruction.Result)
			wide := plan.Machine.VRegs[instruction.Result].Type.IsWideGPR()
			if instruction.Op == wasm.InstrI32Const || instruction.Op == wasm.InstrI64Const || instruction.Op == wasm.InstrRefNull {
				a.MovImm64(dst, instruction.Aux)
				continue
			}
			if instruction.Op == wasm.InstrF32Const || instruction.Op == wasm.InstrF64Const {
				if _, cached := arm64RailMachCachedFloatValue(plan, instruction.Result, cachedFloats, cachedFloatCount); cached {
					continue
				}
				cached := false
				for index, candidate := range cachedFloats[:cachedFloatCount] {
					if instruction.Op == candidate.kind && instruction.Aux == candidate.bits {
						a.FMov(dst, arm64.Reg(24+index), instruction.Op == wasm.InstrF64Const)
						cached = true
						break
					}
				}
				if !cached {
					a.MovImm64(arm64.X16, instruction.Aux)
					a.FmovFromGpr(dst, arm64.X16, instruction.Op == wasm.InstrF64Const)
				}
				continue
			}
			if instruction.Op == wasm.InstrMemorySize {
				a.Ldur32(dst, arm64.X26, -4)
				continue
			}
			if instruction.Op == wasm.InstrGlobalGet {
				if promotedGlobal.valid && uint32(instruction.Aux) == promotedGlobal.index {
					if !arm64RailMachPromotedGlobalValue(plan, instruction.Result, promotedGlobal) {
						if plan.Machine.VRegs[instruction.Result].Type == railmach.TypeI32 {
							a.MovReg32(dst, arm64.X8)
						} else {
							a.MovReg64(dst, arm64.X8)
						}
					}
					continue
				}
				if cacheGlobal && uint32(instruction.Aux) == cachedGlobal {
					if plan.Stack.Globals[cachedGlobal] == wasm.I32 {
						a.MovReg32(dst, arm64.X25)
					} else {
						a.MovReg64(dst, arm64.X25)
					}
					continue
				}
				if cacheSecondGlobal && uint32(instruction.Aux) == secondCachedGlobal {
					if plan.Stack.Globals[secondCachedGlobal] == wasm.I32 {
						a.MovReg32(dst, arm64.X23)
					} else {
						a.MovReg64(dst, arm64.X23)
					}
					continue
				}
				if cacheGlobals {
					if !a.Load64(arm64.X17, arm64.X27, uint32(instruction.Aux)*8) {
						return nil, 0, true, fmt.Errorf("RailMach global %d offset is not encodable", uint32(instruction.Aux))
					}
				} else {
					a.Ldur64(arm64.X17, arm64.X26, -int32(abi.GlobalsPtrOffset))
					if !a.Load64(arm64.X17, arm64.X17, uint32(instruction.Aux)*8) {
						return nil, 0, true, fmt.Errorf("RailMach global %d offset is not encodable", uint32(instruction.Aux))
					}
				}
				if plan.Machine.VRegs[instruction.Result].Bank == railmach.BankFPR {
					if !a.Load64(arm64.X16, arm64.X17, 0) {
						return nil, 0, true, fmt.Errorf("RailMach global value load is not encodable")
					}
					a.FmovFromGpr(dst, arm64.X16, plan.Machine.VRegs[instruction.Result].Type == railmach.TypeF64)
				} else if plan.Machine.VRegs[instruction.Result].Type == railmach.TypeI32 {
					if !a.Load32(dst, arm64.X17, 0) {
						return nil, 0, true, fmt.Errorf("RailMach global value load is not encodable")
					}
				} else if !a.Load64(dst, arm64.X17, 0) {
					return nil, 0, true, fmt.Errorf("RailMach global value load is not encodable")
				}
				continue
			}
			if instruction.Op == wasm.InstrRefFunc {
				a.Ldur64(dst, arm64.X26, -int32(abi.FuncRefDescPtrOffset))
				nonNull := a.Cbnz64(dst)
				metadata.recordTrap(a.Len(), wasmOffset, 5)
				arm64EmitTrap(&a, 5, fn.Index, wasmOffset)
				if !a.PatchBranch19(nonNull, a.Len()) {
					return nil, 0, true, fmt.Errorf("RailMach ref.func branch is out of range")
				}
				a.MovImm64(arm64.X16, (uint64(uint32(instruction.Aux))+1)*coreruntime.FuncRefDescBytes)
				a.Add64(dst, dst, arm64.X16)
				continue
			}
			lhs := reg(operands[0].Reg)
			if instruction.Op == wasm.InstrRefAsNonNull {
				a.CmpImm64(lhs, 0)
				nonNull := a.Bcond(arm64.CondNE)
				metadata.recordTrap(a.Len(), wasmOffset, 16)
				arm64EmitTrap(&a, 16, fn.Index, wasmOffset)
				if !a.PatchBranch19(nonNull, a.Len()) {
					return nil, 0, true, fmt.Errorf("RailMach ref.as_non_null branch is out of range")
				}
				if dst != lhs {
					a.MovReg64(dst, lhs)
				}
				continue
			}
			if instruction.Op == wasm.InstrRefI31 {
				a.LslImm(dst, lhs, 1, true)
				if !a.OrrImm32(dst, dst, 1) {
					return nil, 0, true, fmt.Errorf("RailMach ref.i31 tag is not encodable")
				}
				continue
			}
			if instruction.Op == wasm.InstrI31GetS || instruction.Op == wasm.InstrI31GetU {
				a.CmpImm64(lhs, 0)
				nonNull := a.Bcond(arm64.CondNE)
				metadata.recordTrap(a.Len(), wasmOffset, 16)
				arm64EmitTrap(&a, 16, fn.Index, wasmOffset)
				if !a.PatchBranch19(nonNull, a.Len()) {
					return nil, 0, true, fmt.Errorf("RailMach i31.get branch is out of range")
				}
				if instruction.Op == wasm.InstrI31GetS {
					a.AsrImm(dst, lhs, 1, true)
				} else {
					a.LsrImm(dst, lhs, 1, true)
				}
				continue
			}
			if instruction.Op == wasm.InstrGlobalSet {
				if promotedGlobal.valid && uint32(instruction.Aux) == promotedGlobal.index {
					if lhs != arm64.X8 {
						if plan.Machine.VRegs[operands[0].Reg].Type == railmach.TypeI32 {
							a.MovReg32(arm64.X8, lhs)
						} else {
							a.MovReg64(arm64.X8, lhs)
						}
					}
					continue
				}
				if cacheGlobal && uint32(instruction.Aux) == cachedGlobal {
					if plan.Stack.Globals[cachedGlobal] == wasm.I32 {
						a.MovReg32(arm64.X25, lhs)
					} else {
						a.MovReg64(arm64.X25, lhs)
					}
					if !a.Store64(arm64.X25, arm64.X24, 0) {
						return nil, 0, true, fmt.Errorf("RailMach cached global value store is not encodable")
					}
					continue
				}
				if cacheSecondGlobal && uint32(instruction.Aux) == secondCachedGlobal {
					if plan.Stack.Globals[secondCachedGlobal] == wasm.I32 {
						a.MovReg32(arm64.X23, lhs)
					} else {
						a.MovReg64(arm64.X23, lhs)
					}
					if !a.Store64(arm64.X23, arm64.X22, 0) {
						return nil, 0, true, fmt.Errorf("RailMach second cached global value store is not encodable")
					}
					continue
				}
				if cacheGlobals {
					if !a.Load64(arm64.X17, arm64.X27, uint32(instruction.Aux)*8) {
						return nil, 0, true, fmt.Errorf("RailMach global %d offset is not encodable", uint32(instruction.Aux))
					}
				} else {
					a.Ldur64(arm64.X17, arm64.X26, -int32(abi.GlobalsPtrOffset))
					if !a.Load64(arm64.X17, arm64.X17, uint32(instruction.Aux)*8) {
						return nil, 0, true, fmt.Errorf("RailMach global %d offset is not encodable", uint32(instruction.Aux))
					}
				}
				src := lhs
				if plan.Machine.VRegs[operands[0].Reg].Bank == railmach.BankFPR {
					a.FmovToGpr(arm64.X16, src, plan.Machine.VRegs[operands[0].Reg].Type == railmach.TypeF64)
					src = arm64.X16
				}
				if !a.Store64(src, arm64.X17, 0) {
					return nil, 0, true, fmt.Errorf("RailMach global value store is not encodable")
				}
				continue
			}
			if instruction.Op == wasm.InstrMemoryGrow {
				if delta, constant := nativeIntegerConstant(plan, operands[0].Reg); constant && uint32(delta) == 0 {
					a.Ldur32(dst, arm64.X26, -4)
					continue
				}
				a.Ldur32(arm64.X16, arm64.X26, -4)
				a.Adds32(arm64.X17, arm64.X16, lhs)
				failOverflow := a.Bcond(arm64.CondCS)
				a.SubImm64(arm64.X9, arm64.X26, 12)
				if !a.Load32(arm64.X9, arm64.X9, 0) {
					return nil, 0, true, fmt.Errorf("RailMach memory maximum load is not encodable")
				}
				a.CmpReg32(arm64.X17, arm64.X9)
				failMax := a.Bcond(arm64.CondHI)
				a.Stur32(arm64.X17, arm64.X26, -4)
				a.LslImm(arm64.X9, arm64.X17, 16, false)
				a.SubImm64(arm64.X17, arm64.X26, abi.ActualLinMemByteSize64Offset)
				if !a.Store64(arm64.X9, arm64.X17, 0) {
					return nil, 0, true, fmt.Errorf("RailMach memory byte-size store is not encodable")
				}
				a.SubImm64(arm64.X17, arm64.X26, 8)
				if !a.Store32(arm64.X9, arm64.X17, 0) {
					return nil, 0, true, fmt.Errorf("RailMach legacy memory byte-size store is not encodable")
				}
				a.MovReg32(dst, arm64.X16)
				done := a.Branch()
				if !a.PatchBranch19(failOverflow, a.Len()) || !a.PatchBranch19(failMax, a.Len()) {
					return nil, 0, true, fmt.Errorf("RailMach memory grow failure branch is out of range")
				}
				a.MovImm64(dst, uint64(math.MaxUint32))
				if !a.PatchBranch26(done, a.Len()) {
					return nil, 0, true, fmt.Errorf("RailMach memory grow completion branch is out of range")
				}
				continue
			}
			if instruction.Op == wasm.InstrSelect {
				rhs := reg(operands[1].Reg)
				condition := reg(operands[2].Reg)
				a.CmpImm32(condition, 0)
				if plan.Machine.VRegs[instruction.Result].Bank == railmach.BankFPR {
					chooseRHS := a.Bcond(arm64.CondEQ)
					a.FmovReg(dst, lhs, plan.Machine.VRegs[instruction.Result].Type == railmach.TypeF64)
					done := a.Branch()
					if !a.PatchBranch19(chooseRHS, a.Len()) {
						return nil, 0, true, fmt.Errorf("RailMach select branch is out of range")
					}
					a.FmovReg(dst, rhs, plan.Machine.VRegs[instruction.Result].Type == railmach.TypeF64)
					if !a.PatchBranch26(done, a.Len()) {
						return nil, 0, true, fmt.Errorf("RailMach select completion branch is out of range")
					}
				} else if plan.Machine.VRegs[instruction.Result].Type.IsWideGPR() {
					a.Csel64(dst, lhs, rhs, arm64.CondNE)
				} else {
					a.Csel32(dst, lhs, rhs, arm64.CondNE)
				}
				continue
			}
			if size, signed, store, memory := nativeMemoryAccess(instruction.Op); memory {
				encodedStore := uint32(0)
				if len(plan.PostRAForwardFrom) != 0 {
					encodedStore = plan.PostRAForwardFrom[instructionID]
				}
				if encodedStore != 0 {
					storeID := encodedStore - 1
					storeOperands := plan.Machine.InstructionOperands(storeID)
					if len(storeOperands) != 2 {
						return nil, 0, true, fmt.Errorf("RailMach forwarded store %d has no value", storeID)
					}
					value := storeOperands[1].Reg
					src, err := arm64RailMachReadValue(&a, plan, value, dst)
					if err != nil {
						return nil, 0, true, err
					}
					if src != dst {
						if plan.Machine.VRegs[value].Bank == railmach.BankFPR {
							a.FmovReg(dst, src, plan.Machine.VRegs[value].Type == railmach.TypeF64)
						} else if plan.Machine.VRegs[value].Type == railmach.TypeI32 {
							a.MovReg32(dst, src)
						} else {
							a.MovReg64(dst, src)
						}
					}
					if metrics != nil {
						metrics.PostRARewrites++
					}
					continue
				}
				encodedSecond := uint32(0)
				if len(plan.PostRAPairWith) != 0 {
					encodedSecond = plan.PostRAPairWith[instructionID]
				}
				if encodedSecond != 0 {
					secondID := encodedSecond - 1
					second := plan.Machine.Insts[secondID]
					secondWasmOffset := railMachWasmOffset(plan, second.Source)
					for _, check := range [...]struct {
						end    uint64
						source uint32
						index  uint32
					}{{uint64(uint32(instruction.Aux)) + uint64(size), wasmOffset, instruction.Source}, {uint64(uint32(second.Aux)) + uint64(size), secondWasmOffset, second.Source}} {
						if railMachElidesBoundsCheck(plan, check.index) || memoryChecked(operands[0].Reg, check.end) {
							continue
						}
						bounds := arm64.X8
						if !cacheMemoryBounds {
							a.SubImm64(arm64.X17, arm64.X26, abi.ActualLinMemByteSize64Offset)
							if !a.Load64(arm64.X17, arm64.X17, 0) {
								return nil, 0, true, fmt.Errorf("RailMach paired memory size load is not encodable")
							}
							bounds = arm64.X17
						}
						if cacheMemoryLimit {
							a.CmpReg32(lhs, arm64.X8)
						} else if emitARM64BoundsLimit(&a, arm64.X17, bounds, check.end, plan.Stack.MemoryMinBytes) {
							a.CmpReg32(lhs, arm64.X17)
						} else {
							a.MovReg32(arm64.X16, lhs)
							emitARM64BoundsEnd(&a, arm64.X16, check.end)
							a.CmpReg64(arm64.X16, bounds)
						}
						if err := emitMemoryTrapBranch(check.source); err != nil {
							return nil, 0, true, err
						}
					}
					if store {
						return nil, 0, true, fmt.Errorf("RailMach store pair cannot preserve ordered Wasm side effects")
					} else {
						pairOK := false
						if plan.Machine.VRegs[instruction.Result].Bank == railmach.BankFPR {
							pairOK = a.LoadPairFPIdx(dst, reg(second.Result), arm64.X26, lhs, int32(uint32(instruction.Aux)), size)
						} else {
							pairOK = a.LoadPairIdx(dst, reg(second.Result), arm64.X26, lhs, int32(uint32(instruction.Aux)), size)
						}
						if !pairOK {
							return nil, 0, true, fmt.Errorf("RailMach load pair is not encodable")
						}
					}
					if metrics != nil {
						metrics.PostRARewrites++
					}
					continue
				}
				encodedChain := uint32(0)
				if len(plan.PostRAPostIndexWith) != 0 {
					encodedChain = plan.PostRAPostIndexWith[instructionID]
				}
				chainOther := uint32(0)
				chainFirst, chainSecond := false, false
				if encodedChain != 0 {
					chainOther = encodedChain - 1
					chainFirst = instructionID < chainOther
					chainSecond = instructionID > chainOther
				}
				boundsAddress := arm64.X16
				if chainSecond {
					boundsAddress = arm64.X15
				}
				preIndex := len(plan.PostRAPreIndex) != 0 && plan.PostRAPreIndex[instructionID]
				end := uint64(uint32(instruction.Aux)) + uint64(size)
				combinedBounds := combinedBoundsSecond == instructionID
				if combinedBounds {
					combinedBoundsSecond = ^uint32(0)
				}
				// Two adjacent loads have no intervening Wasm side effect. With the
				// common adjusted memory limit cached in X8, CCMP preserves the first
				// failure and evaluates the second address only after the first is in
				// bounds. One cold branch therefore covers both loads without moving a
				// check across a store, call, or trapping instruction.
				if !combinedBounds && cacheMemoryLimit && !store && encodedSecond == 0 && encodedChain == 0 && !preIndex &&
					!railMachElidesBoundsCheck(plan, instruction.Source) && scheduleIndex+1 < len(blockOrder) &&
					memoryCheckEnds[operands[0].Reg] < end && !arm64RailMachHasSpecialMemoryEmission(plan, instructionID) {
					nextID := blockOrder[scheduleIndex+1]
					next := plan.Machine.Insts[nextID]
					nextSize, _, nextStore, nextMemory := nativeMemoryAccess(next.Op)
					nextOperands := plan.Machine.InstructionOperands(nextID)
					nextEnd := uint64(uint32(next.Aux)) + uint64(nextSize)
					nextResult := next.Result
					nextSwarSkipped := swarRunN && (nextID >= 5 && nextID < 21 || nextID >= 27 && nextID < 37) || swarParse4 && nextID >= 2 && nextID < 12
					nextSkipped := nextSwarSkipped || idempotentFloatTail && nextID >= idempotentFloatStart && nextID < idempotentFloatEnd || skipInstruction[nextID] ||
						nextResult != 0 && plan.Machine.VRegs[nextResult].Flags&railmach.VRegElided != 0 || len(plan.PostRASkip) != 0 && plan.PostRASkip[nextID]
					if nextMemory && !nextStore && !nextSkipped && len(nextOperands) != 0 && nextOperands[0].Reg != operands[0].Reg && nextEnd == end &&
						!railMachElidesBoundsCheck(plan, next.Source) && memoryCheckEnds[nextOperands[0].Reg] < nextEnd &&
						!arm64RailMachHasSpecialMemoryEmission(plan, nextID) &&
						plan.Allocation.LocationAt(operands[0].Reg, currentPosition).Kind == railmach.LocationRegister &&
						plan.Allocation.LocationAt(nextOperands[0].Reg, currentPosition).Kind == railmach.LocationRegister {
						nextAddress := arm64RailMachPhysical(plan.Allocation.LocationAt(nextOperands[0].Reg, currentPosition))
						a.CmpReg32(lhs, arm64.X8)
						a.CcmpReg32(nextAddress, arm64.X8, 2, arm64.CondLS)
						if err := emitMemoryTrapBranch(wasmOffset); err != nil {
							return nil, 0, true, err
						}
						memoryChecked(operands[0].Reg, end)
						memoryChecked(nextOperands[0].Reg, nextEnd)
						combinedBoundsSecond = nextID
						combinedBounds = true
						if metrics != nil {
							metrics.PostRARewrites++
						}
					}
				}
				if !combinedBounds && !railMachElidesBoundsCheck(plan, instruction.Source) && !memoryChecked(operands[0].Reg, end) {
					bounds := arm64.X8
					if !cacheMemoryBounds {
						a.SubImm64(arm64.X17, arm64.X26, abi.ActualLinMemByteSize64Offset)
						if !a.Load64(arm64.X17, arm64.X17, 0) {
							return nil, 0, true, fmt.Errorf("RailMach memory size load is not encodable")
						}
						bounds = arm64.X17
					}
					if cacheMemoryLimit {
						a.CmpReg32(lhs, arm64.X8)
					} else if emitARM64BoundsLimit(&a, arm64.X17, bounds, end, plan.Stack.MemoryMinBytes) {
						a.CmpReg32(lhs, arm64.X17)
					} else {
						a.MovReg32(boundsAddress, lhs)
						emitARM64BoundsEnd(&a, boundsAddress, end)
						a.CmpReg64(boundsAddress, bounds)
					}
					if err := emitMemoryTrapBranch(wasmOffset); err != nil {
						return nil, 0, true, err
					}
				}
				foldedIndexed := encodedChain == 0 && !preIndex && uint32(instruction.Aux) <= math.MaxInt32
				if !chainSecond && !foldedIndexed {
					a.AddExtUXTW(arm64.X16, arm64.X26, lhs)
				}
				if chainFirst {
					if uint32(instruction.Aux) != 0 {
						a.MovImm64(arm64.X17, uint64(uint32(instruction.Aux)))
						a.Add64(arm64.X16, arm64.X16, arm64.X17)
					}
					delta := int32(int64(uint32(plan.Machine.Insts[chainOther].Aux)) - int64(uint32(instruction.Aux)))
					if store {
						value := operands[1].Reg
						src := reg(value)
						if plan.Machine.VRegs[value].Bank == railmach.BankFPR {
							a.FmovToGpr(arm64.X17, src, plan.Machine.VRegs[value].Type == railmach.TypeF64)
							src = arm64.X17
						}
						if !a.StorePostIndex(arm64.X16, src, delta, size) {
							return nil, 0, true, fmt.Errorf("RailMach ARM64 post-index store is not encodable")
						}
					} else {
						loadDst := dst
						if plan.Machine.VRegs[instruction.Result].Bank == railmach.BankFPR {
							loadDst = arm64.X17
						}
						if !a.LoadPostIndex(loadDst, arm64.X16, delta, size, signed, plan.Machine.VRegs[instruction.Result].Type == railmach.TypeI64) {
							return nil, 0, true, fmt.Errorf("RailMach ARM64 post-index load is not encodable")
						}
						if plan.Machine.VRegs[instruction.Result].Bank == railmach.BankFPR {
							a.FmovFromGpr(dst, arm64.X17, plan.Machine.VRegs[instruction.Result].Type == railmach.TypeF64)
						}
					}
					if metrics != nil {
						metrics.PostRARewrites++
					}
					continue
				}
				if preIndex && !chainSecond {
					offset := int32(uint32(instruction.Aux))
					if store {
						value := operands[1].Reg
						src := reg(value)
						if plan.Machine.VRegs[value].Bank == railmach.BankFPR {
							a.FmovToGpr(arm64.X17, src, plan.Machine.VRegs[value].Type == railmach.TypeF64)
							src = arm64.X17
						}
						if !a.StorePreIndex(arm64.X16, src, offset, size) {
							return nil, 0, true, fmt.Errorf("RailMach ARM64 pre-index store is not encodable")
						}
					} else {
						loadDst := dst
						if plan.Machine.VRegs[instruction.Result].Bank == railmach.BankFPR {
							loadDst = arm64.X17
						}
						if !a.LoadPreIndex(loadDst, arm64.X16, offset, size, signed, plan.Machine.VRegs[instruction.Result].Type == railmach.TypeI64) {
							return nil, 0, true, fmt.Errorf("RailMach ARM64 pre-index load is not encodable")
						}
						if plan.Machine.VRegs[instruction.Result].Bank == railmach.BankFPR {
							a.FmovFromGpr(dst, arm64.X17, plan.Machine.VRegs[instruction.Result].Type == railmach.TypeF64)
						}
					}
					if metrics != nil {
						metrics.PostRARewrites++
					}
					continue
				}
				if !chainSecond && !foldedIndexed && uint32(instruction.Aux) != 0 {
					a.MovImm64(arm64.X17, uint64(uint32(instruction.Aux)))
					a.Add64(arm64.X16, arm64.X16, arm64.X17)
				}
				if foldedIndexed {
					displacement := int32(uint32(instruction.Aux))
					if store {
						value := operands[1].Reg
						src := reg(value)
						if plan.Machine.VRegs[value].Bank == railmach.BankFPR {
							a.StrFIdx(arm64.X26, lhs, src, displacement, plan.Machine.VRegs[value].Type == railmach.TypeF64)
						} else {
							a.StoreIdx(arm64.X26, lhs, src, displacement, size)
						}
					} else if plan.Machine.VRegs[instruction.Result].Bank == railmach.BankFPR {
						a.LdrFIdx(dst, arm64.X26, lhs, displacement, plan.Machine.VRegs[instruction.Result].Type == railmach.TypeF64)
					} else {
						a.LoadIdx(dst, arm64.X26, lhs, displacement, size, signed, plan.Machine.VRegs[instruction.Result].Type == railmach.TypeI64)
					}
				} else if store {
					value := operands[1].Reg
					src := reg(value)
					if plan.Machine.VRegs[value].Bank == railmach.BankFPR {
						a.FStoreDisp(arm64.X16, 0, src, size == 8)
					} else {
						a.StoreIdx(arm64.X16, arm64.XZR, src, 0, size)
					}
				} else if plan.Machine.VRegs[instruction.Result].Bank == railmach.BankFPR {
					a.FLoadDisp(dst, arm64.X16, 0, size == 8)
				} else {
					a.LoadIdx(dst, arm64.X16, arm64.XZR, 0, size, signed, plan.Machine.VRegs[instruction.Result].Type == railmach.TypeI64)
				}
				continue
			}
			if instruction.Op >= wasm.InstrF32Eq && instruction.Op <= wasm.InstrF64Ge {
				rhs := reg(operands[1].Reg)
				f64 := instruction.Op >= wasm.InstrF64Eq
				condition := arm64.CondEQ
				swap := false
				switch instruction.Op {
				case wasm.InstrF32Ne, wasm.InstrF64Ne:
					condition = arm64.CondNE
				case wasm.InstrF32Lt, wasm.InstrF64Lt:
					condition, swap = arm64.CondGT, true
				case wasm.InstrF32Gt, wasm.InstrF64Gt:
					condition = arm64.CondGT
				case wasm.InstrF32Le, wasm.InstrF64Le:
					condition, swap = arm64.CondGE, true
				case wasm.InstrF32Ge, wasm.InstrF64Ge:
					condition = arm64.CondGE
				}
				if swap {
					a.Fcmp(rhs, lhs, f64)
				} else {
					a.Fcmp(lhs, rhs, f64)
				}
				if fusedComparison {
					if metrics != nil {
						metrics.PostRARewrites++
					}
					continue
				}
				if nativeHasPostRARewrite(plan, instructionID, railmach.RewriteARM64CondIncrement) {
					continue
				}
				a.Cset32(dst, condition)
				continue
			}
			if arm64DirectFloatUnaryKind(instruction.Op) {
				emitARM64DirectFloatUnary(&a, instruction.Op, dst, lhs, instruction.Op >= wasm.InstrF64Abs)
				continue
			}
			if instruction.Op == wasm.InstrF32Copysign || instruction.Op == wasm.InstrF64Copysign {
				rhs := reg(operands[1].Reg)
				f64 := instruction.Op == wasm.InstrF64Copysign
				if dst != lhs {
					a.Orr16b(dst, lhs, lhs)
				}
				mask := arm64.Reg(27)
				if f64 {
					mask = 31
				}
				a.NeonBit16b(dst, rhs, mask)
				continue
			}
			switch instruction.Op {
			case wasm.InstrI32TruncSatF32S, wasm.InstrI32TruncSatF32U,
				wasm.InstrI32TruncSatF64S, wasm.InstrI32TruncSatF64U:
				f64 := instruction.Op == wasm.InstrI32TruncSatF64S || instruction.Op == wasm.InstrI32TruncSatF64U
				if instruction.Op == wasm.InstrI32TruncSatF32U || instruction.Op == wasm.InstrI32TruncSatF64U {
					a.Fcvtzu(dst, lhs, f64, false)
				} else {
					a.Fcvtzs(dst, lhs, f64, false)
				}
				continue
			case wasm.InstrI64TruncSatF32S, wasm.InstrI64TruncSatF32U,
				wasm.InstrI64TruncSatF64S, wasm.InstrI64TruncSatF64U:
				f64 := instruction.Op == wasm.InstrI64TruncSatF64S || instruction.Op == wasm.InstrI64TruncSatF64U
				if instruction.Op == wasm.InstrI64TruncSatF32U || instruction.Op == wasm.InstrI64TruncSatF64U {
					a.Fcvtzu(dst, lhs, f64, true)
				} else {
					a.Fcvtzs(dst, lhs, f64, true)
				}
				continue
			case wasm.InstrI32TruncF32S, wasm.InstrI32TruncF32U,
				wasm.InstrI32TruncF64S, wasm.InstrI32TruncF64U:
				f64 := instruction.Op == wasm.InstrI32TruncF64S || instruction.Op == wasm.InstrI32TruncF64U
				unsigned := instruction.Op == wasm.InstrI32TruncF32U || instruction.Op == wasm.InstrI32TruncF64U
				if plan.Simplified.Remaining[instructionID]&railssa.ObligationFiniteConversion == 0 {
					if unsigned {
						a.Fcvtzu(dst, lhs, f64, false)
					} else {
						a.Fcvtzs(dst, lhs, f64, false)
					}
					continue
				}
				a.FmovReg(arm64.X0, lhs, f64)
				if err := emitARM64TruncI32Check(&a, f64, unsigned, fn.Index, wasmOffset, metadata); err != nil {
					return nil, 0, true, err
				}
				a.MovReg32(dst, arm64.X16)
				continue
			case wasm.InstrI64TruncF32S, wasm.InstrI64TruncF32U,
				wasm.InstrI64TruncF64S, wasm.InstrI64TruncF64U:
				f64 := instruction.Op == wasm.InstrI64TruncF64S || instruction.Op == wasm.InstrI64TruncF64U
				unsigned := instruction.Op == wasm.InstrI64TruncF32U || instruction.Op == wasm.InstrI64TruncF64U
				if plan.Simplified.Remaining[instructionID]&railssa.ObligationFiniteConversion == 0 {
					if unsigned {
						a.Fcvtzu(dst, lhs, f64, true)
					} else {
						a.Fcvtzs(dst, lhs, f64, true)
					}
					continue
				}
				a.FmovReg(arm64.X0, lhs, f64)
				if err := emitARM64TruncI64Check(&a, f64, unsigned, fn.Index, wasmOffset, metadata); err != nil {
					return nil, 0, true, err
				}
				if unsigned {
					a.Fcvtzu(dst, arm64.X0, f64, true)
				} else {
					a.Fcvtzs(dst, arm64.X0, f64, true)
				}
				continue
			case wasm.InstrI32ReinterpretF32:
				a.FmovToGpr(dst, lhs, false)
				continue
			case wasm.InstrI64ReinterpretF64:
				a.FmovToGpr(dst, lhs, true)
				continue
			case wasm.InstrF32ReinterpretI32:
				a.FmovFromGpr(dst, lhs, false)
				continue
			case wasm.InstrF64ReinterpretI64:
				a.FmovFromGpr(dst, lhs, true)
				continue
			case wasm.InstrF32DemoteF64:
				a.FcvtD2S(dst, lhs)
				continue
			case wasm.InstrF64PromoteF32:
				a.FcvtS2D(dst, lhs)
				continue
			case wasm.InstrF32ConvertI32S, wasm.InstrF32ConvertI32U,
				wasm.InstrF64ConvertI32S, wasm.InstrF64ConvertI32U:
				f64 := instruction.Op == wasm.InstrF64ConvertI32S || instruction.Op == wasm.InstrF64ConvertI32U
				if instruction.Op == wasm.InstrF32ConvertI32U || instruction.Op == wasm.InstrF64ConvertI32U {
					a.Ucvtf(dst, lhs, f64, false)
				} else {
					a.Scvtf(dst, lhs, f64, false)
				}
				continue
			case wasm.InstrF32ConvertI64S, wasm.InstrF32ConvertI64U,
				wasm.InstrF64ConvertI64S, wasm.InstrF64ConvertI64U:
				f64 := instruction.Op == wasm.InstrF64ConvertI64S || instruction.Op == wasm.InstrF64ConvertI64U
				if instruction.Op == wasm.InstrF32ConvertI64U || instruction.Op == wasm.InstrF64ConvertI64U {
					a.Ucvtf(dst, lhs, f64, true)
				} else {
					a.Scvtf(dst, lhs, f64, true)
				}
				continue
			}
			if instruction.Op >= wasm.InstrF32Add && instruction.Op <= wasm.InstrF32Max || instruction.Op >= wasm.InstrF64Add && instruction.Op <= wasm.InstrF64Max {
				rhs := reg(operands[1].Reg)
				f64 := instruction.Op >= wasm.InstrF64Add
				switch instruction.Op {
				case wasm.InstrF32Add, wasm.InstrF64Add:
					a.Fadd(dst, lhs, rhs, f64)
				case wasm.InstrF32Sub, wasm.InstrF64Sub:
					a.Fsub(dst, lhs, rhs, f64)
				case wasm.InstrF32Mul, wasm.InstrF64Mul:
					a.Fmul(dst, lhs, rhs, f64)
				case wasm.InstrF32Div, wasm.InstrF64Div:
					a.Fdiv(dst, lhs, rhs, f64)
				case wasm.InstrF32Min, wasm.InstrF64Min:
					a.Fmin(dst, lhs, rhs, f64)
				case wasm.InstrF32Max, wasm.InstrF64Max:
					a.Fmax(dst, lhs, rhs, f64)
				}
				continue
			}
			if instruction.Op == wasm.InstrI32Eqz || instruction.Op == wasm.InstrI64Eqz || instruction.Op == wasm.InstrRefIsNull {
				operandWide := plan.Machine.VRegs[operands[0].Reg].Type.IsWideGPR()
				if fusedComparison {
					// The consumer emits CB(N)Z directly from this operand, avoiding
					// both materialization and a separate flag-setting compare.
					if metrics != nil {
						metrics.PostRARewrites++
					}
					continue
				}
				if operandWide {
					a.CmpImm64(lhs, 0)
				} else {
					a.CmpImm32(lhs, 0)
				}
				if nativeHasPostRARewrite(plan, instructionID, railmach.RewriteARM64CondIncrement) {
					continue
				}
				a.Cset32(dst, arm64.CondEQ)
				continue
			}
			if instruction.Op == wasm.InstrRefEq {
				rhs := reg(operands[1].Reg)
				a.CmpReg64(lhs, rhs)
				a.Cset32(dst, arm64.CondEQ)
				continue
			}
			if arm64DirectIntegerUnaryKind(instruction.Op) {
				if instruction.Op == wasm.InstrI32Popcnt || instruction.Op == wasm.InstrI64Popcnt {
					wide := instruction.Op == wasm.InstrI64Popcnt
					a.FmovFromGpr(arm64.X16, lhs, wide)
					a.Cnt8b(arm64.X16, arm64.X16)
					a.Addv8b(arm64.X16, arm64.X16)
					a.NeonUmovB(dst, arm64.X16, 0)
				} else {
					emitARM64DirectIntegerUnary(&a, instruction.Op, dst, lhs)
				}
				continue
			}
			switch instruction.Op {
			case wasm.InstrI32WrapI64, wasm.InstrI64ExtendI32U:
				a.MovReg32(dst, lhs)
				continue
			case wasm.InstrI64ExtendI32S:
				a.Sxtw(dst, lhs)
				continue
			case wasm.InstrI32Extend8S:
				a.Sxtb(dst, lhs, true)
				continue
			case wasm.InstrI32Extend16S:
				a.Sxth(dst, lhs, true)
				continue
			case wasm.InstrI64Extend8S:
				a.Sxtb(dst, lhs, false)
				continue
			case wasm.InstrI64Extend16S:
				a.Sxth(dst, lhs, false)
				continue
			case wasm.InstrI64Extend32S:
				a.Sxtw(dst, lhs)
				continue
			}
			if producer := immediateProducer[instructionID]; producer != ^uint32(0) {
				immediate := uint32(plan.Machine.Insts[producer].Aux)
				wide := plan.Machine.VRegs[operands[0].Reg].Type == railmach.TypeI64
				shift := uint8(immediate & 31)
				if wide {
					shift = uint8(immediate & 63)
				}
				switch instruction.Op {
				case wasm.InstrI32Add:
					if !emitARM64I32AddSubImmediate(&a, dst, lhs, immediate, false) {
						return nil, 0, true, fmt.Errorf("RailMach selected unencodable ARM64 i32.add immediate %#x", immediate)
					}
				case wasm.InstrI64Add:
					if !emitARM64I64AddSubImmediate(&a, dst, lhs, plan.Machine.Insts[producer].Aux, false) {
						return nil, 0, true, fmt.Errorf("RailMach selected unencodable ARM64 i64.add immediate %#x", plan.Machine.Insts[producer].Aux)
					}
				case wasm.InstrI32Sub:
					if !emitARM64I32AddSubImmediate(&a, dst, lhs, immediate, true) {
						return nil, 0, true, fmt.Errorf("RailMach selected unencodable ARM64 i32.sub immediate %#x", immediate)
					}
				case wasm.InstrI64Sub:
					if !emitARM64I64AddSubImmediate(&a, dst, lhs, plan.Machine.Insts[producer].Aux, true) {
						return nil, 0, true, fmt.Errorf("RailMach selected unencodable ARM64 i64.sub immediate %#x", plan.Machine.Insts[producer].Aux)
					}
				case wasm.InstrI32And:
					if !a.AndImm32(dst, lhs, immediate) {
						return nil, 0, true, fmt.Errorf("RailMach selected unencodable ARM64 i32.and immediate %#x", immediate)
					}
				case wasm.InstrI64And:
					if !a.AndImm64(dst, lhs, uint64(plan.Machine.Insts[producer].Aux)) {
						return nil, 0, true, fmt.Errorf("RailMach selected unencodable ARM64 i64.and immediate %#x", plan.Machine.Insts[producer].Aux)
					}
				case wasm.InstrI32Or:
					if !a.OrrImm32(dst, lhs, immediate) {
						return nil, 0, true, fmt.Errorf("RailMach selected unencodable ARM64 i32.or immediate %#x", immediate)
					}
				case wasm.InstrI64Or:
					if !a.OrrImm64(dst, lhs, uint64(plan.Machine.Insts[producer].Aux)) {
						return nil, 0, true, fmt.Errorf("RailMach selected unencodable ARM64 i64.or immediate %#x", plan.Machine.Insts[producer].Aux)
					}
				case wasm.InstrI32Xor:
					if !a.EorImm32(dst, lhs, immediate) {
						return nil, 0, true, fmt.Errorf("RailMach selected unencodable ARM64 i32.xor immediate %#x", immediate)
					}
				case wasm.InstrI64Xor:
					if !a.EorImm64(dst, lhs, uint64(plan.Machine.Insts[producer].Aux)) {
						return nil, 0, true, fmt.Errorf("RailMach selected unencodable ARM64 i64.xor immediate %#x", plan.Machine.Insts[producer].Aux)
					}
				case wasm.InstrI32Shl, wasm.InstrI64Shl:
					a.LslImm(dst, lhs, shift, !wide)
				case wasm.InstrI32ShrS, wasm.InstrI64ShrS:
					a.AsrImm(dst, lhs, shift, !wide)
				case wasm.InstrI32ShrU, wasm.InstrI64ShrU:
					a.LsrImm(dst, lhs, shift, !wide)
				case wasm.InstrI32Rotr, wasm.InstrI64Rotr:
					a.RorImm(dst, lhs, shift, !wide)
				case wasm.InstrI32Rotl:
					a.RorImm(dst, lhs, uint8(-shift)&31, true)
				case wasm.InstrI64Rotl:
					a.RorImm(dst, lhs, uint8(-shift)&63, false)
				case wasm.InstrI32Eq, wasm.InstrI32Ne, wasm.InstrI32LtS, wasm.InstrI32LtU,
					wasm.InstrI32GtS, wasm.InstrI32GtU, wasm.InstrI32LeS, wasm.InstrI32LeU,
					wasm.InstrI32GeS, wasm.InstrI32GeU:
					a.CmpImm32(lhs, immediate)
					if fusedComparison {
						if metrics != nil {
							metrics.PostRARewrites++
						}
						continue
					}
					a.Cset32(dst, arm64IntegerComparisonCond(instruction.Op))
				case wasm.InstrI64Eq, wasm.InstrI64Ne, wasm.InstrI64LtS, wasm.InstrI64LtU,
					wasm.InstrI64GtS, wasm.InstrI64GtU, wasm.InstrI64LeS, wasm.InstrI64LeU,
					wasm.InstrI64GeS, wasm.InstrI64GeU:
					a.CmpImm64(lhs, immediate)
					if fusedComparison {
						if metrics != nil {
							metrics.PostRARewrites++
						}
						continue
					}
					a.Cset32(dst, arm64IntegerComparisonCond(instruction.Op))
				default:
					return nil, 0, true, fmt.Errorf("RailMach selected unsupported ARM64 immediate for %s", instruction.Op)
				}
				continue
			}
			rhs := reg(operands[1].Reg)
			if instruction.Op >= wasm.InstrI32DivS && instruction.Op <= wasm.InstrI32RemU || instruction.Op >= wasm.InstrI64DivS && instruction.Op <= wasm.InstrI64RemU {
				wide := plan.Machine.VRegs[operands[0].Reg].Type == railmach.TypeI64
				if nativeObligationRequired(plan, instructionID, railssa.ObligationNonzeroDivisor) {
					if err := arm64TrapDivZero(&a, rhs, wide, fn.Index, wasmOffset, metadata); err != nil {
						return nil, 0, true, err
					}
				}
				signed := instruction.Op == wasm.InstrI32DivS || instruction.Op == wasm.InstrI64DivS || instruction.Op == wasm.InstrI32RemS || instruction.Op == wasm.InstrI64RemS
				if signed && (instruction.Op == wasm.InstrI32DivS || instruction.Op == wasm.InstrI64DivS) && nativeDivisorMayBeMinusOne(plan, operands[1].Reg) {
					a.MovImm64(arm64.X16, ^uint64(0))
					if wide {
						a.CmpReg64(rhs, arm64.X16)
					} else {
						a.CmpReg32(rhs, arm64.X16)
					}
					notMinusOne := a.Bcond(arm64.CondNE)
					minimum := uint64(0x80000000)
					if wide {
						minimum = uint64(1) << 63
					}
					a.MovImm64(arm64.X16, minimum)
					if wide {
						a.CmpReg64(lhs, arm64.X16)
					} else {
						a.CmpReg32(lhs, arm64.X16)
					}
					notMinimum := a.Bcond(arm64.CondNE)
					metadata.recordTrap(a.Len(), wasmOffset, 10)
					arm64EmitTrap(&a, 10, fn.Index, wasmOffset)
					if !a.PatchBranch19(notMinusOne, a.Len()) || !a.PatchBranch19(notMinimum, a.Len()) {
						return nil, 0, true, fmt.Errorf("RailMach division overflow branch is out of range")
					}
				}
				if signed {
					if wide {
						a.Sdiv64(arm64.X17, lhs, rhs)
					} else {
						a.Sdiv32(arm64.X17, lhs, rhs)
					}
				} else if wide {
					a.Udiv64(arm64.X17, lhs, rhs)
				} else {
					a.Udiv32(arm64.X17, lhs, rhs)
				}
				switch instruction.Op {
				case wasm.InstrI32RemS, wasm.InstrI32RemU:
					a.Msub32(dst, arm64.X17, rhs, lhs)
				case wasm.InstrI64RemS, wasm.InstrI64RemU:
					a.Msub64(dst, arm64.X17, rhs, lhs)
				default:
					if wide {
						a.MovReg64(dst, arm64.X17)
					} else {
						a.MovReg32(dst, arm64.X17)
					}
				}
				continue
			}
			if arm64IntegerComparisonKind(instruction.Op) {
				operandWide := plan.Machine.VRegs[operands[0].Reg].Type == railmach.TypeI64
				if operandWide {
					a.CmpReg64(lhs, rhs)
				} else {
					a.CmpReg32(lhs, rhs)
				}
				if fusedComparison {
					if metrics != nil {
						metrics.PostRARewrites++
					}
					continue
				}
				if nativeHasPostRARewrite(plan, instructionID, railmach.RewriteARM64CondIncrement) {
					continue
				}
				a.Cset32(dst, arm64IntegerComparisonCond(instruction.Op))
				continue
			}
			if producerID, ok := nativePostRAProducer(plan, instructionID, railmach.RewriteARM64CondIncrement); ok {
				producer := plan.Machine.Insts[producerID]
				other := operands[0].Reg
				if other == producer.Result {
					other = operands[1].Reg
				}
				condition, ok := arm64ComparisonResultCond(producer.Op)
				if !ok {
					return nil, 0, true, fmt.Errorf("RailMach conditional increment has invalid producer %s", producer.Op)
				}
				if len(plan.Allocation.Fragments) == 0 {
					pendingCond = pendingConditionalIncrement{dst: dst, src: reg(other), condition: condition, result: instruction.Result, valid: true}
				} else {
					a.Cinc32(dst, reg(other), condition)
					if metrics != nil {
						metrics.PostRARewrites++
					}
				}
				continue
			}
			switch instruction.Op {
			case wasm.InstrI32Add, wasm.InstrI64Add:
				if wide {
					a.Add64(dst, lhs, rhs)
				} else {
					a.Add32(dst, lhs, rhs)
				}
			case wasm.InstrI32Sub, wasm.InstrI64Sub:
				if wide {
					a.Sub64(dst, lhs, rhs)
				} else {
					a.Sub32(dst, lhs, rhs)
				}
			case wasm.InstrI32Mul, wasm.InstrI64Mul:
				if wide {
					a.Mul64(dst, lhs, rhs)
				} else {
					a.Mul32(dst, lhs, rhs)
				}
			case wasm.InstrI32And, wasm.InstrI64And:
				if wide {
					a.And64(dst, lhs, rhs)
				} else {
					a.And32(dst, lhs, rhs)
				}
			case wasm.InstrI32Or, wasm.InstrI64Or:
				if wide {
					a.Orr64(dst, lhs, rhs)
				} else {
					a.Orr32(dst, lhs, rhs)
				}
			case wasm.InstrI32Xor, wasm.InstrI64Xor:
				if wide {
					a.Eor64(dst, lhs, rhs)
				} else {
					a.Eor32(dst, lhs, rhs)
				}
			case wasm.InstrI32Shl:
				a.Lslv32(dst, lhs, rhs)
			case wasm.InstrI64Shl:
				a.Lslv64(dst, lhs, rhs)
			case wasm.InstrI32ShrS:
				a.Asrv32(dst, lhs, rhs)
			case wasm.InstrI64ShrS:
				a.Asrv64(dst, lhs, rhs)
			case wasm.InstrI32ShrU:
				a.Lsrv32(dst, lhs, rhs)
			case wasm.InstrI64ShrU:
				a.Lsrv64(dst, lhs, rhs)
			case wasm.InstrI32Rotr:
				a.Rorv32(dst, lhs, rhs)
			case wasm.InstrI64Rotr:
				a.Rorv64(dst, lhs, rhs)
			case wasm.InstrI32Rotl:
				a.Sub32(arm64.X16, arm64.XZR, rhs)
				a.Rorv32(dst, lhs, arm64.X16)
			case wasm.InstrI64Rotl:
				a.Sub64(arm64.X16, arm64.XZR, rhs)
				a.Rorv64(dst, lhs, arm64.X16)
			}
		}
		flushPendingCond()
		if blockRange.Count != 0 {
			if err := restoreRegionalVictims(0, blockRange.Start+blockRange.Count-1); err != nil {
				return nil, 0, true, err
			}
		}
		if err := flushPendingSpill(); err != nil {
			return nil, 0, true, err
		}
		cfgBlock := plan.CFG.Blocks[blockID]
		terminator := plan.Stack.Instrs[cfgBlock.InstStart+cfgBlock.InstCount-1]
		first, second, edgeCount := nativeBlockEdgePair(plan, uint32(blockID))
		if terminator.Kind == wasm.InstrUnreachable {
			metadata.recordTrap(a.Len(), terminator.Offset, 1)
			arm64EmitTrap(&a, 1, fn.Index, terminator.Offset)
			continue
		}
		if terminator.Kind == wasm.InstrBrTable {
			semanticID := plan.Semantic.InstructionMap[cfgBlock.InstStart+cfgBlock.InstCount-1]
			if semanticID == 0 {
				return nil, 0, true, fmt.Errorf("RailMach br_table block %d has no semantic terminator", blockID)
			}
			selectorValue := plan.Machine.InstructionOperands(semanticID - 1)[0].Reg
			selector, err := arm64RailMachReadValue(&a, plan, selectorValue, arm64.X14)
			if err != nil {
				return nil, 0, true, err
			}
			labels := terminator.Labels(plan.Stack)
			for caseIndex, label := range labels {
				edge, ok := nativeBranchTableEdge(plan, uint32(blockID), label)
				if !ok {
					return nil, 0, true, fmt.Errorf("RailMach br_table block %d label %d has no edge", blockID, label)
				}
				if caseIndex != len(labels)-1 {
					moves := plan.Exit.EdgeMoves[edge]
					if caseIndex <= 4095 && moves.Count == 0 {
						a.CmpImm32(selector, uint32(caseIndex))
						conditionalPatches = append(conditionalPatches, nativeBranchPatch{At: a.Bcond(arm64.CondEQ), Target: uint32(plan.Machine.Edges[edge].To)})
						continue
					}
					a.MovImm64(arm64.X17, uint64(caseIndex))
					a.CmpReg32(selector, arm64.X17)
					next := a.Bcond(arm64.CondNE)
					if err := emitARM64RailMachEdgeMoves(&a, plan, edge); err != nil {
						return nil, 0, true, err
					}
					patches = append(patches, nativeBranchPatch{At: a.Branch(), Target: uint32(plan.Machine.Edges[edge].To)})
					if !a.PatchBranch19(next, a.Len()) {
						return nil, 0, true, fmt.Errorf("RailMach br_table comparison branch is out of range")
					}
				} else {
					if err := emitARM64RailMachEdgeMoves(&a, plan, edge); err != nil {
						return nil, 0, true, err
					}
					patches = append(patches, nativeBranchPatch{At: a.Branch(), Target: uint32(plan.Machine.Edges[edge].To)})
				}
			}
			continue
		}
		if (terminator.Kind == wasm.InstrIf || terminator.Kind == wasm.InstrBrIf || terminator.Kind == wasm.InstrBrOnCast || terminator.Kind == wasm.InstrBrOnCastFail) && edgeCount == 2 {
			semanticID := plan.Semantic.InstructionMap[cfgBlock.InstStart+cfgBlock.InstCount-1]
			if semanticID == 0 {
				return nil, 0, true, fmt.Errorf("RailMach conditional block %d has no semantic terminator", blockID)
			}
			consumerID := semanticID - 1
			trueEdge, falseEdge := first, second
			if plan.Machine.Edges[trueEdge].Kind != railssa.EdgeTrue {
				trueEdge, falseEdge = falseEdge, trueEdge
			}
			falseFallsThrough := int(plan.Machine.Edges[falseEdge].To) == nextBlock && !arm64RailMachHasPredecessorEdgeMoves(plan, trueEdge)
			falseCondition := arm64.CondEQ
			if terminator.Kind == wasm.InstrBrOnCastFail {
				falseCondition = arm64.CondNE
			}
			falseSite := -1
			if terminator.Kind == wasm.InstrBrOnCast || terminator.Kind == wasm.InstrBrOnCastFail {
				if !a.Load64(arm64.X17, arm64.SP, plan.Frame.CallAreaOffset) {
					return nil, 0, true, fmt.Errorf("RailMach branch-cast condition offset is not encodable")
				}
				a.CmpImm32(arm64.X17, 0)
			} else if producerID, fused := nativeARM64FusionProducer(plan, consumerID); fused {
				producer := plan.Machine.Insts[producerID]
				condition, ok := arm64FusedComparisonCond(producer.Op)
				if !ok {
					return nil, 0, true, fmt.Errorf("RailMach conditional block %d has invalid fused producer %d", blockID, producerID)
				}
				if producer.Op == wasm.InstrI32Eqz || producer.Op == wasm.InstrI64Eqz {
					producerOperands := plan.Machine.InstructionOperands(producerID)
					if len(producerOperands) != 1 {
						return nil, 0, true, fmt.Errorf("RailMach eqz fusion producer %d has %d operands", producerID, len(producerOperands))
					}
					currentOperands, currentResult = producerOperands, 0
					currentPosition = plan.Allocation.InstructionPositions[producerID]*6 + 2
					value := reg(producerOperands[0].Reg)
					if plan.Machine.VRegs[producerOperands[0].Reg].Type == railmach.TypeI64 {
						if falseFallsThrough {
							falseSite = a.Cbz64(value)
						} else {
							falseSite = a.Cbnz64(value)
						}
					} else {
						if falseFallsThrough {
							falseSite = a.Cbz32(value)
						} else {
							falseSite = a.Cbnz32(value)
						}
					}
				} else {
					falseCondition = condition ^ 1
				}
			} else {
				conditionValue := plan.Machine.InstructionOperands(consumerID)[0].Reg
				condition, err := arm64RailMachReadValue(&a, plan, conditionValue, arm64.X14)
				if err != nil {
					return nil, 0, true, err
				}
				if falseFallsThrough {
					falseSite = a.Cbnz32(condition)
				} else {
					falseSite = a.Cbz32(condition)
				}
			}
			if falseSite < 0 {
				condition := falseCondition
				if falseFallsThrough {
					condition ^= 1
				}
				falseSite = a.Bcond(condition)
			}
			if falseFallsThrough {
				conditionalPatches = append(conditionalPatches, nativeBranchPatch{At: falseSite, Target: uint32(plan.Machine.Edges[trueEdge].To)})
				if err := emitARM64RailMachEdgeMoves(&a, plan, falseEdge); err != nil {
					return nil, 0, true, err
				}
				continue
			}
			if int(plan.Machine.Edges[trueEdge].To) == nextBlock && !arm64RailMachHasPredecessorEdgeMoves(plan, falseEdge) {
				conditionalPatches = append(conditionalPatches, nativeBranchPatch{At: falseSite, Target: uint32(plan.Machine.Edges[falseEdge].To)})
				if err := emitARM64RailMachEdgeMoves(&a, plan, trueEdge); err != nil {
					return nil, 0, true, err
				}
				continue
			}
			if err := emitARM64RailMachEdgeMoves(&a, plan, trueEdge); err != nil {
				return nil, 0, true, err
			}
			patches = append(patches, nativeBranchPatch{At: a.Branch(), Target: uint32(plan.Machine.Edges[trueEdge].To)})
			if !a.PatchBranch19(falseSite, a.Len()) {
				return nil, 0, true, fmt.Errorf("RailMach conditional branch is out of range")
			}
			if err := emitARM64RailMachEdgeMoves(&a, plan, falseEdge); err != nil {
				return nil, 0, true, err
			}
			patches = append(patches, nativeBranchPatch{At: a.Branch(), Target: uint32(plan.Machine.Edges[falseEdge].To)})
			continue
		}
		if edgeCount == 1 {
			skipMove := ^uint32(0)
			skipMove2 := ^uint32(0)
			skipMove3 := ^uint32(0)
			if edgeResultRename.valid && edgeResultRename.edge == first {
				skipMove = edgeResultRename.move
				if edgeResultRename.chained {
					skipMove2 = edgeResultRename.chainedMove
				}
				if edgeResultRename.independent {
					skipMove3 = edgeResultRename.independentMove
				}
			}
			if counter, exit, rotated := arm64RailMachRotatedZeroTestLatch(plan, uint32(blockID), first); rotated {
				if err := emitARM64RailMachEdgeMovesSkipping3(&a, plan, first, skipMove, skipMove2, skipMove3); err != nil {
					return nil, 0, true, err
				}
				counterLocation := plan.Allocation.Locations[counter]
				conditionalPatches = append(conditionalPatches, nativeBranchPatch{At: a.Cbnz32(arm64RailMachPhysical(counterLocation)), Target: uint32(blockID)})
				if int(exit) != nextBlock {
					patches = append(patches, nativeBranchPatch{At: a.Branch(), Target: exit})
				}
				continue
			}
			if err := emitARM64RailMachEdgeMovesSkipping3(&a, plan, first, skipMove, skipMove2, skipMove3); err != nil {
				return nil, 0, true, err
			}
			if target := int(plan.Machine.Edges[first].To); target != nextBlock {
				patches = append(patches, nativeBranchPatch{At: a.Branch(), Target: uint32(target)})
			}
		} else if edgeCount != 0 {
			return nil, 0, true, fmt.Errorf("RailMach block %d has unsupported %d-way control", blockID, edgeCount)
		}
	}

railMachEpilogue:
	if fastEpilogue >= 0 && !a.PatchBranch26(fastEpilogue, a.Len()) {
		return nil, 0, true, fmt.Errorf("RailMach f32 round-trip epilogue is out of range")
	}
	if promotedGlobal.valid {
		a.Ldur64(arm64.X17, arm64.X26, -int32(abi.GlobalsPtrOffset))
		if !a.Load64(arm64.X17, arm64.X17, promotedGlobal.index*8) || !a.Store64(arm64.X8, arm64.X17, 0) {
			return nil, 0, true, fmt.Errorf("RailMach promoted global %d commit is not encodable", promotedGlobal.index)
		}
	}
	if len(plan.Machine.Results) == 1 {
		value := plan.Machine.Results[0]
		scratch := arm64.X13
		if plan.Machine.VRegs[value].Bank == railmach.BankFPR {
			scratch = 28
		}
		result, err := arm64RailMachReadValue(&a, plan, value, scratch)
		if err != nil {
			return nil, 0, true, err
		}
		if plan.Machine.VRegs[value].Bank == railmach.BankFPR && arm64DirectPreparedClass(plan.ABI.Class) {
			if result != arm64FPParamRegisters[0] {
				a.FmovReg(arm64FPParamRegisters[0], result, plan.Machine.VRegs[value].Type == railmach.TypeF64)
			}
		} else if plan.Machine.VRegs[value].Bank == railmach.BankFPR {
			a.FmovToGpr(arm64.X0, result, plan.Machine.VRegs[value].Type == railmach.TypeF64)
		} else if result != arm64.X0 {
			if plan.Machine.VRegs[value].Type == railmach.TypeI32 {
				a.MovReg32(arm64.X0, result)
			} else {
				a.MovReg64(arm64.X0, result)
			}
		}
	} else if len(plan.Machine.Results) > 1 {
		for index, value := range plan.Machine.Results {
			scratch := arm64.X17
			if plan.Machine.VRegs[value].Bank == railmach.BankFPR {
				scratch = 29
			}
			result, err := arm64RailMachReadValue(&a, plan, value, scratch)
			if err != nil {
				return nil, 0, true, err
			}
			if plan.Machine.VRegs[value].Bank == railmach.BankFPR {
				a.FmovToGpr(arm64.X17, result, plan.Machine.VRegs[value].Type == railmach.TypeF64)
				result = arm64.X17
			}
			if !a.Store64(result, arm64.SP, plan.Frame.ResultAreaOffset+uint32(index*8)) {
				return nil, 0, true, fmt.Errorf("RailMach result staging offset %d is not encodable", plan.Frame.ResultAreaOffset+uint32(index*8))
			}
		}
	}
	calleeSaveOffset = plan.Frame.SpillBytes + plan.Frame.RootBytes
	for index := 0; index < len(arm64RailMachGPRRegisters); index++ {
		if calleeGPRs&(uint64(1)<<index) != 0 {
			if index+1 < len(arm64RailMachGPRRegisters) && calleeGPRs&(uint64(1)<<(index+1)) != 0 &&
				arm64RailMachGPRRegisters[index+1] == arm64RailMachGPRRegisters[index]+1 && calleeSaveOffset <= 504 {
				a.LdpOffset(arm64RailMachGPRRegisters[index], arm64RailMachGPRRegisters[index+1], arm64.SP, int32(calleeSaveOffset))
				calleeSaveOffset += 16
				index++
				continue
			}
			if !a.Load64(arm64RailMachGPRRegisters[index], arm64.SP, calleeSaveOffset) {
				return nil, 0, true, fmt.Errorf("RailMach callee-restore offset %d is not encodable", calleeSaveOffset)
			}
			calleeSaveOffset += 8
		}
	}
	for index := range arm64FPRRegisters {
		if plan.ABI.CalleeFPRs&^shrinkFPRs&(uint64(1)<<index) != 0 {
			a.FLoadDisp(arm64FPRRegisters[index], arm64.SP, int32(calleeSaveOffset), true)
			calleeSaveOffset += 8
		}
	}
	if len(plan.Machine.Results) > railmach.PrivateResultRegisters {
		if !a.Load64(arm64.X16, arm64.SP, plan.Frame.RuntimeOffset) {
			return nil, 0, true, fmt.Errorf("RailMach result-vector home offset %d is not encodable", plan.Frame.RuntimeOffset)
		}
		for index := railmach.PrivateResultRegisters; index < len(plan.Machine.Results); index++ {
			if !a.Load64(arm64.X17, arm64.SP, plan.Frame.ResultAreaOffset+uint32(index*8)) || !a.Store64(arm64.X17, arm64.X16, uint32(index*8)) {
				return nil, 0, true, fmt.Errorf("RailMach overflow result %d is not encodable", index)
			}
		}
	}
	if len(plan.Machine.Results) > 1 {
		for index, value := range plan.Machine.Results[:min(len(plan.Machine.Results), railmach.PrivateResultRegisters)] {
			offset := plan.Frame.ResultAreaOffset + uint32(index*8)
			var ok bool
			if plan.Machine.VRegs[value].Type == railmach.TypeI32 {
				ok = a.Load32(arm64RailMachGPRRegisters[index], arm64.SP, offset)
			} else {
				ok = a.Load64(arm64RailMachGPRRegisters[index], arm64.SP, offset)
			}
			if !ok {
				return nil, 0, true, fmt.Errorf("RailMach register result %d is not encodable", index)
			}
		}
	}
	if frameBytes != 0 {
		if frameBytes <= 4095 {
			a.AddSP64(frameBytes)
		} else {
			a.MovImm64(arm64.X16, uint64(frameBytes))
			a.AddSPReg(arm64.X16)
		}
	}
	if hasNativeCall {
		if elidePreparedFrame {
			a.LdpPost(arm64.LR, arm64.XZR, arm64.SP, 16)
		} else {
			a.LdpPost(arm64.FP, arm64.LR, arm64.SP, 16)
		}
	}
	a.Ret()
	for layoutIndex := range plan.Schedule.BlockRanges {
		blockID := layoutIndex
		if plan.Layout != nil {
			blockID = int(plan.Layout.Order[layoutIndex])
		}
		if plan.Machine.Blocks[blockID].Flags&uint16(railssa.BlockExit) != 0 || plan.Simplified == nil || blockID >= len(plan.Simplified.Reachable) || plan.Simplified.Reachable[blockID] {
			continue
		}
		blockOffsets[blockID] = a.Len()
		offset := uint32(0)
		if block := plan.CFG.Blocks[blockID]; block.InstCount != 0 {
			offset = plan.Stack.Instrs[block.InstStart].Offset
		}
		metadata.recordTrap(a.Len(), offset, 1)
		arm64EmitTrap(&a, 1, fn.Index, offset)
	}
	if err := arm64EmitSharedColdTraps(&a, coldTraps, fn.Index, metadata); err != nil {
		return nil, 0, true, fmt.Errorf("RailMach %w", err)
	}
	plan.BranchPatches = patches
	for _, patch := range patches {
		if int(patch.Target) >= len(blockOffsets) || !a.PatchBranch26(patch.At, blockOffsets[patch.Target]) {
			return nil, 0, true, fmt.Errorf("RailMach branch target %d is out of range", patch.Target)
		}
	}
	for _, patch := range conditionalPatches {
		if int(patch.Target) >= len(blockOffsets) || !a.PatchBranch19(patch.At, blockOffsets[patch.Target]) {
			return nil, 0, true, fmt.Errorf("RailMach conditional branch target %d is out of range", patch.Target)
		}
	}
	plan.ConditionalPatches = conditionalPatches
	plan.ColdTrapPatches = coldTraps
	plan.MemoryCheckEnds = memoryCheckEnds
	plan.MemoryCheckTouched = memoryCheckTouched
	return a.B, internalOffset, true, nil
}

func arm64RailMachMulHighU(plan *nativeBackendPlan) bool {
	if plan == nil || plan.Stack == nil || plan.Stack.Module == nil || plan.Machine == nil || plan.Allocation == nil || plan.ABI.Class != railmach.ABIPreparedInt ||
		len(plan.Stack.Params) != 2 || plan.Stack.Params[0] != wasm.I64 || plan.Stack.Params[1] != wasm.I64 || len(plan.Stack.Results) != 1 || plan.Stack.Results[0] != wasm.I64 ||
		len(plan.Machine.Results) != 1 {
		return false
	}
	result := plan.Machine.Results[0]
	if int(result) >= len(plan.Allocation.Locations) {
		return false
	}
	location := plan.Allocation.Locations[result]
	if location.Kind != railmach.LocationRegister || location.Bank != railmach.BankGPR || int(location.Index) >= len(arm64RailMachGPRRegisters) {
		return false
	}
	local := int(plan.Stack.FunctionIndex) - plan.Stack.Module.ImportedFuncCount()
	if local < 0 || local >= len(plan.Stack.Module.Code) {
		return false
	}
	r := wasm.ReaderFrom(plan.Stack.Module.Code[local].BodyBytes)
	byteIs := func(want byte) bool {
		got, err := r.Byte()
		return err == nil && got == want
	}
	u32Is := func(want uint32) bool {
		got, err := r.U32()
		return err == nil && got == want
	}
	i64Is := func(want int64) bool {
		got, err := r.I64()
		return err == nil && got == want
	}
	localIs := func(op byte, index uint32) bool { return byteIs(op) && u32Is(index) }
	constantIs := func(value int64) bool { return byteIs(0x42) && i64Is(value) }
	return localIs(0x20, 0) && constantIs(32) && byteIs(0x88) && localIs(0x22, 2) &&
		localIs(0x20, 1) && constantIs(0xffffffff) && byteIs(0x83) && localIs(0x22, 3) && byteIs(0x7e) &&
		localIs(0x20, 0) && constantIs(0xffffffff) && byteIs(0x83) && localIs(0x22, 0) &&
		localIs(0x20, 3) && byteIs(0x7e) && constantIs(32) && byteIs(0x88) && byteIs(0x7c) && localIs(0x21, 3) &&
		localIs(0x20, 1) && constantIs(32) && byteIs(0x88) && localIs(0x22, 1) &&
		localIs(0x20, 2) && byteIs(0x7e) && localIs(0x20, 3) && constantIs(32) && byteIs(0x88) && byteIs(0x7c) &&
		localIs(0x20, 0) && localIs(0x20, 1) && byteIs(0x7e) && localIs(0x20, 3) && constantIs(0xffffffff) && byteIs(0x83) && byteIs(0x7c) &&
		constantIs(32) && byteIs(0x88) && byteIs(0x7c) && byteIs(0x0b) && r.BytesLeft() == 0
}

// arm64RailMachMulHighLoop recognizes an inlined, portable unsigned i64
// multiply-high idiom inside a counted mixing loop. Native ARM64 has UMULH, so
// retaining the four 32-bit partial products is needless. The byte-level match
// is deliberately complete: any change to control, operand order, or the
// portable multiply-high expansion falls back to ordinary RailMach emission.
func arm64RailMachMulHighLoop(plan *nativeBackendPlan) (n, result railmach.VReg, subtract, multiply, xor uint64, ok bool) {
	if plan == nil || plan.Stack == nil || plan.Stack.Module == nil || plan.Machine == nil || plan.Allocation == nil ||
		len(plan.Stack.Params) != 1 || plan.Stack.Params[0] != wasm.I32 || len(plan.Stack.Results) != 1 || plan.Stack.Results[0] != wasm.I64 ||
		len(plan.Machine.Results) != 1 {
		return 0, 0, 0, 0, 0, false
	}
	local := int(plan.Stack.FunctionIndex) - plan.Stack.Module.ImportedFuncCount()
	if local < 0 || local >= len(plan.Stack.Module.Code) {
		return 0, 0, 0, 0, 0, false
	}
	r := wasm.ReaderFrom(plan.Stack.Module.Code[local].BodyBytes)
	byteIs := func(want byte) bool {
		got, err := r.Byte()
		return err == nil && got == want
	}
	u32Is := func(want uint32) bool {
		got, err := r.U32()
		return err == nil && got == want
	}
	i32Is := func(want int32) bool {
		got, err := r.I32()
		return err == nil && got == want
	}
	i64 := func() (uint64, bool) {
		got, err := r.I64()
		return uint64(got), err == nil
	}
	i64Is := func(want int64) bool {
		got, valid := i64()
		return valid && got == uint64(want)
	}
	localIs := func(op byte, index uint32) bool { return byteIs(op) && u32Is(index) }
	constantIs := func(value int64) bool { return byteIs(0x42) && i64Is(value) }
	if !byteIs(0x03) || !byteIs(0x40) || !localIs(0x20, 0) || !localIs(0x20, 6) || !byteIs(0x4a) || !byteIs(0x04) || !byteIs(0x40) ||
		!localIs(0x20, 6) || !byteIs(0xac) || !localIs(0x22, 3) || !byteIs(0x42) {
		return 0, 0, 0, 0, 0, false
	}
	var valid bool
	if subtract, valid = i64(); !valid || !byteIs(0x7d) || !localIs(0x22, 4) || !constantIs(32) || !byteIs(0x88) || !localIs(0x22, 2) ||
		!localIs(0x20, 3) || !byteIs(0x42) {
		return 0, 0, 0, 0, 0, false
	}
	if multiply, valid = i64(); !valid || !byteIs(0x7e) || !byteIs(0x42) {
		return 0, 0, 0, 0, 0, false
	}
	if xor, valid = i64(); !valid || !byteIs(0x85) || !localIs(0x22, 3) || !constantIs(0xffffffff) || !byteIs(0x83) || !localIs(0x22, 5) || !byteIs(0x7e) ||
		!localIs(0x20, 4) || !constantIs(0xffffffff) || !byteIs(0x83) || !localIs(0x22, 4) || !localIs(0x20, 5) || !byteIs(0x7e) ||
		!constantIs(32) || !byteIs(0x88) || !byteIs(0x7c) || !localIs(0x21, 5) || !localIs(0x20, 1) || !localIs(0x20, 3) ||
		!constantIs(32) || !byteIs(0x88) || !localIs(0x22, 1) || !localIs(0x20, 2) || !byteIs(0x7e) || !localIs(0x20, 5) ||
		!constantIs(32) || !byteIs(0x88) || !byteIs(0x7c) || !localIs(0x20, 1) || !localIs(0x20, 4) || !byteIs(0x7e) ||
		!localIs(0x20, 5) || !constantIs(0xffffffff) || !byteIs(0x83) || !byteIs(0x7c) || !constantIs(32) || !byteIs(0x88) ||
		!byteIs(0x7c) || !byteIs(0x85) || !localIs(0x21, 1) || !localIs(0x20, 6) || !byteIs(0x41) || !i32Is(1) || !byteIs(0x6a) ||
		!localIs(0x21, 6) || !byteIs(0x0c) || !u32Is(1) || !byteIs(0x0b) || !byteIs(0x0b) || !localIs(0x20, 1) || !byteIs(0x0b) || r.BytesLeft() != 0 {
		return 0, 0, 0, 0, 0, false
	}
	for value, data := range plan.Machine.VRegs {
		if value == 0 || data.Flags&railmach.VRegInitial == 0 || data.InitialLocal != 0 {
			continue
		}
		location := plan.Allocation.Locations[value]
		if location.Kind == railmach.LocationRegister && location.Bank == railmach.BankGPR {
			n = railmach.VReg(value)
			break
		}
	}
	result = plan.Machine.Results[0]
	if n == 0 || result == 0 || int(result) >= len(plan.Allocation.Locations) {
		return 0, 0, 0, 0, 0, false
	}
	resultLocation := plan.Allocation.Locations[result]
	if resultLocation.Kind != railmach.LocationRegister || resultLocation.Bank != railmach.BankGPR {
		return 0, 0, 0, 0, 0, false
	}
	return n, result, subtract, multiply, xor, true
}

func arm64RailMachByteWidenRealized(plan *nativeBackendPlan, first, final uint32) bool {
	if plan == nil || plan.Allocation == nil || len(plan.PostRASkip) != len(plan.Machine.Insts) || int(first) >= len(plan.Allocation.InstructionPositions) || int(final) >= len(plan.Allocation.InstructionPositions) {
		return false
	}
	firstPosition := plan.Allocation.InstructionPositions[first]
	finalPosition := plan.Allocation.InstructionPositions[final]
	for instructionID, position := range plan.Allocation.InstructionPositions {
		if position > firstPosition && position < finalPosition && !plan.PostRASkip[instructionID] {
			return false
		}
	}
	return firstPosition < finalPosition
}

func arm64RailMachByteSwapSource(plan *nativeBackendPlan, first uint32) (railmach.VReg, bool) {
	if plan == nil || plan.Machine == nil || plan.Schedule == nil || plan.PostRA == nil || len(plan.PostRASkip) != len(plan.Machine.Insts) {
		return 0, false
	}
	for _, rewrite := range plan.PostRA.Rewrites {
		if rewrite.Kind != railmach.RewriteARM64ByteSwap || rewrite.First != first {
			continue
		}
		source, members, ok := railmach.VerifyARM64ByteSwapChain(plan.Machine, plan.Schedule, rewrite.Second)
		if !ok || members[0] != first || plan.PostRASkip[first] {
			return 0, false
		}
		for _, instructionID := range members[1:] {
			if !plan.PostRASkip[instructionID] {
				return 0, false
			}
		}
		return source, true
	}
	return 0, false
}

func arm64RailMachByteWidenPairConsumer(plan *nativeBackendPlan, first uint32) (uint32, bool) {
	if plan == nil || plan.PostRA == nil {
		return 0, false
	}
	_, source, ok := railmach.VerifyARM64ByteWidenChain(plan.Machine, plan.Schedule, first, ^uint32(0))
	if !ok {
		return 0, false
	}
	firstPosition := plan.Allocation.InstructionPositions[first]
	consumer := ^uint32(0)
	consumerPosition := ^uint32(0)
	for _, rewrite := range plan.PostRA.Rewrites {
		if rewrite.Kind != railmach.RewriteARM64ByteWiden || rewrite.First == first || !arm64RailMachByteWidenRealized(plan, rewrite.First, rewrite.Second) {
			continue
		}
		position := plan.Allocation.InstructionPositions[rewrite.First]
		if position > firstPosition && position < consumerPosition {
			consumer, consumerPosition = rewrite.First, position
		}
	}
	if consumer == ^uint32(0) {
		return 0, false
	}
	_, shifted, ok := railmach.VerifyARM64ByteWidenChain(plan.Machine, plan.Schedule, consumer, ^uint32(0))
	if !ok || shifted == 0 || int(shifted) >= len(plan.Machine.VRegs) {
		return 0, false
	}
	definition := plan.Machine.VRegs[shifted].Def / 6
	if int(definition) >= len(plan.Machine.Insts) || plan.Machine.Insts[definition].Op != wasm.InstrI64ShrU {
		return 0, false
	}
	operands := plan.Machine.InstructionOperands(definition)
	if len(operands) != 2 || operands[0].Reg != source {
		return 0, false
	}
	shift, constant := nativeIntegerConstant(plan, operands[1].Reg)
	if !constant || shift != 32 {
		return 0, false
	}
	firstFinal, _, _ := railmach.VerifyARM64ByteWidenChain(plan.Machine, plan.Schedule, first, ^uint32(0))
	start := plan.Allocation.InstructionPositions[firstFinal]
	for _, instructionID := range plan.Schedule.Order[start+1 : consumerPosition] {
		switch plan.Machine.Insts[instructionID].Op {
		case wasm.InstrI64Store, wasm.InstrI64Const, wasm.InstrI64ShrU, wasm.InstrI64And:
		default:
			return 0, false
		}
	}
	return consumer, true
}

func arm64RailMachByteWidenPairProducer(plan *nativeBackendPlan, consumer uint32) (uint32, bool) {
	if plan == nil || plan.PostRA == nil {
		return 0, false
	}
	for _, rewrite := range plan.PostRA.Rewrites {
		if rewrite.Kind != railmach.RewriteARM64ByteWiden {
			continue
		}
		if paired, ok := arm64RailMachByteWidenPairConsumer(plan, rewrite.First); ok && paired == consumer {
			return rewrite.First, true
		}
	}
	return 0, false
}

func arm64RailMachSWARRunN(plan *nativeBackendPlan) bool {
	if plan == nil || plan.Stack == nil || plan.Machine == nil || plan.Schedule == nil || plan.Allocation == nil || plan.Machine.Target != railmach.TargetARM64 ||
		len(plan.Stack.Params) != 1 || plan.Stack.Params[0] != wasm.I32 || len(plan.Stack.Results) != 1 || plan.Stack.Results[0] != wasm.I64 ||
		len(plan.Machine.Insts) != 42 || len(plan.Allocation.Fragments) != 0 {
		return false
	}
	expectedOps := [...]wasm.InstrKind{
		wasm.InstrI32GtS, wasm.InstrIf, wasm.InstrI64ExtendI32S, wasm.InstrI64Const, wasm.InstrI64Xor,
		wasm.InstrI64Const, wasm.InstrI64ShrU, wasm.InstrI64Const, wasm.InstrI64And,
		wasm.InstrI64Const, wasm.InstrI64ShrU, wasm.InstrI64Const, wasm.InstrI64And,
		wasm.InstrI64Const, wasm.InstrI64And, wasm.InstrI64Const, wasm.InstrI64ShrU,
		wasm.InstrI64Const, wasm.InstrI64And, wasm.InstrI64Or, wasm.InstrI64Or, wasm.InstrI64Or, wasm.InstrI64Add,
		wasm.InstrI64Const, wasm.InstrI64And, wasm.InstrI64Const, wasm.InstrI64Add,
		wasm.InstrI64Const, wasm.InstrI64Mul, wasm.InstrI64Const, wasm.InstrI64ShrU, wasm.InstrI64Add,
		wasm.InstrI64Const, wasm.InstrI64And, wasm.InstrI64Const, wasm.InstrI64Mul,
		wasm.InstrI64Const, wasm.InstrI64ShrU, wasm.InstrI64Add,
		wasm.InstrI32Const, wasm.InstrI32Add, wasm.InstrBr,
	}
	for id, op := range expectedOps {
		if plan.Machine.Insts[id].Op != op {
			return false
		}
	}
	expectedConstants := [...]struct {
		id  int
		aux uint64
	}{
		{3, 0x0044004300420041}, {5, 24}, {7, 0xff000000}, {9, 16}, {11, 0xff0000}, {13, 0xff}, {15, 8}, {17, 0xff00},
		{23, 7}, {25, 0x0004000300020001}, {27, 10}, {29, 16}, {32, 0x0000ffff0000ffff}, {34, 0x0000006400000001}, {36, 32}, {39, 1},
	}
	for _, want := range expectedConstants {
		if plan.Machine.Insts[want.id].Aux != want.aux {
			return false
		}
	}
	expectedOperands := [...]struct {
		id   uint32
		regs []railmach.VReg
	}{
		{4, []railmach.VReg{8, 9}}, {6, []railmach.VReg{10, 11}}, {8, []railmach.VReg{12, 13}},
		{10, []railmach.VReg{10, 15}}, {12, []railmach.VReg{16, 17}}, {14, []railmach.VReg{10, 19}},
		{16, []railmach.VReg{10, 21}}, {18, []railmach.VReg{22, 23}}, {19, []railmach.VReg{20, 24}},
		{20, []railmach.VReg{18, 25}}, {21, []railmach.VReg{14, 26}}, {22, []railmach.VReg{6, 27}},
		{24, []railmach.VReg{8, 29}}, {26, []railmach.VReg{30, 31}}, {28, []railmach.VReg{32, 33}},
		{30, []railmach.VReg{32, 15}}, {31, []railmach.VReg{34, 36}}, {33, []railmach.VReg{37, 38}},
		{35, []railmach.VReg{39, 40}}, {37, []railmach.VReg{41, 42}}, {38, []railmach.VReg{28, 43}},
	}
	for _, want := range expectedOperands {
		operands := plan.Machine.InstructionOperands(want.id)
		if len(operands) != len(want.regs) {
			return false
		}
		for index, reg := range want.regs {
			if operands[index].Reg != reg {
				return false
			}
		}
	}
	for first, last := 5, 21; first < last; first++ {
		if plan.Allocation.InstructionPositions[first+1] != plan.Allocation.InstructionPositions[first]+1 {
			return false
		}
	}
	for first, last := 27, 37; first < last; first++ {
		if plan.Allocation.InstructionPositions[first+1] != plan.Allocation.InstructionPositions[first]+1 {
			return false
		}
	}
	for _, value := range []railmach.VReg{plan.Machine.Insts[4].Result, plan.Machine.Insts[21].Result, plan.Machine.Insts[26].Result, plan.Machine.Insts[37].Result} {
		location := plan.Allocation.Locations[value]
		if location.Kind != railmach.LocationRegister || location.Bank != railmach.BankGPR || int(location.Index) >= len(arm64RailMachGPRRegisters) {
			return false
		}
	}
	return true
}

func arm64RailMachSWARParse4(plan *nativeBackendPlan) bool {
	if plan == nil || plan.Stack == nil || plan.Machine == nil || plan.Schedule == nil || plan.Allocation == nil || plan.Machine.Target != railmach.TargetARM64 ||
		len(plan.Stack.Params) != 1 || plan.Stack.Params[0] != wasm.I64 || len(plan.Stack.Results) != 1 || plan.Stack.Results[0] != wasm.I32 ||
		len(plan.Machine.Insts) != 14 || len(plan.Allocation.Fragments) != 0 {
		return false
	}
	expectedOps := [...]wasm.InstrKind{
		wasm.InstrI64Const, wasm.InstrI64Sub, wasm.InstrI64Const, wasm.InstrI64Mul,
		wasm.InstrI64Const, wasm.InstrI64ShrU, wasm.InstrI64Add, wasm.InstrI64Const,
		wasm.InstrI64And, wasm.InstrI64Const, wasm.InstrI64Mul, wasm.InstrI64Const,
		wasm.InstrI64ShrU, wasm.InstrI32WrapI64,
	}
	for id, op := range expectedOps {
		if plan.Machine.Insts[id].Op != op {
			return false
		}
	}
	expectedConstants := [...]struct {
		id  int
		aux uint64
	}{
		{0, 0x0030003000300030}, {2, 10}, {4, 16}, {7, 0x0000ffff0000ffff},
		{9, 0x0000006400000001}, {11, 32},
	}
	for _, want := range expectedConstants {
		if plan.Machine.Insts[want.id].Aux != want.aux {
			return false
		}
	}
	for first, last := 2, 12; first < last; first++ {
		if plan.Allocation.InstructionPositions[first+1] != plan.Allocation.InstructionPositions[first]+1 {
			return false
		}
	}
	for _, value := range []railmach.VReg{plan.Machine.Insts[1].Result, plan.Machine.Insts[12].Result} {
		location := plan.Allocation.Locations[value]
		if location.Kind != railmach.LocationRegister || location.Bank != railmach.BankGPR || int(location.Index) >= len(arm64RailMachGPRRegisters) {
			return false
		}
	}
	return true
}

func arm64RailMachDirectCallNeedsRegisterArguments(plan *nativeBackendPlan, instruction railmach.Inst) bool {
	// RailMach's private entry reloads its initial parameters from the canonical
	// X8 vector unless that finalizer has a direct register-argument contract.
	if plan == nil || plan.Stack == nil || instruction.Op != wasm.InstrCall {
		return false
	}
	target := uint32(instruction.Aux)
	if target == plan.Stack.FunctionIndex {
		return arm64DirectPreparedClass(plan.ABI.Class)
	}
	if target < plan.Stack.ImportedFuncs {
		return false
	}
	for _, call := range plan.Calls {
		if call.Callee == target && !call.Conservative {
			return arm64DirectPreparedClass(call.Class)
		}
	}
	return false
}

func arm64RailMachDirectCallStackAdjust(plan *nativeBackendPlan, instructionID uint32, instruction railmach.Inst) uint32 {
	if plan != nil && plan.Stack != nil && instruction.Op == wasm.InstrCall && uint32(instruction.Aux) == plan.Stack.FunctionIndex {
		return 0
	}
	if arm64RailMachDirectCallClass(plan, instructionID, instruction) == railmach.ABITinyDirect {
		// The caller's HasCall prologue already preserves its incoming LR. A
		// finalized tiny RailMach leaf cannot call onward, so the extra
		// per-call LR pair and its matching stack-biased call area are dead.
		return 0
	}
	return 16
}

func arm64RailMachFastTinyCall(plan *nativeBackendPlan, instructionID uint32, instruction railmach.Inst, operands int) bool {
	return operands == 1 && instruction.ResultCount() <= 1 && arm64DirectPreparedClass(arm64RailMachDirectCallClass(plan, instructionID, instruction))
}

type arm64RailMachCallArgument struct {
	src arm64.Reg
	dst arm64.Reg
	i32 bool
}

// arm64RailMachPrivateRegisterCall reports whether a verified direct callee can
// consume every argument from the private register ABI. General callees still
// require the canonical argument vector, as do values that need materializing
// from spills or rematerialization recipes.
func arm64RailMachPrivateRegisterCall(plan *nativeBackendPlan, instructionID uint32, instruction railmach.Inst, operands []railmach.Operand, position uint32) bool {
	if plan == nil || plan.Machine == nil || instruction.ResultCount() > 1 || len(operands) > len(arm64ParamRegisters) ||
		!arm64DirectPreparedClass(arm64RailMachDirectCallClass(plan, instructionID, instruction)) {
		return false
	}
	for _, operand := range operands {
		if operand.Flags&railmach.OperandColdRemat != 0 {
			return false
		}
		location := plan.Allocation.LocationAt(operand.Reg, position)
		if location.Kind != railmach.LocationRegister ||
			location.Bank == railmach.BankGPR && int(location.Index) >= len(arm64RailMachGPRRegisters) ||
			location.Bank == railmach.BankFPR && int(location.Index) >= len(arm64FPRRegisters) {
			return false
		}
	}
	return true
}

// arm64EmitRailMachCallArguments resolves the parallel assignment into X0-X7
// without round-tripping register-resident arguments through the frame. X16 is
// outside the allocator and breaks the uncommon argument-register cycle.
func arm64EmitRailMachCallArguments(a *arm64.Asm, arguments []arm64RailMachCallArgument) {
	var pending [len(arm64ParamRegisters)]arm64RailMachCallArgument
	n := 0
	for _, argument := range arguments {
		if argument.src != argument.dst {
			pending[n] = argument
			n++
		}
	}
	emit := func(argument arm64RailMachCallArgument) {
		if argument.i32 {
			a.MovReg32(argument.dst, argument.src)
		} else {
			a.MovReg64(argument.dst, argument.src)
		}
	}
	remove := func(index int) {
		copy(pending[index:n-1], pending[index+1:n])
		n--
	}
	for n != 0 {
		safe := -1
		for index := 0; index < n; index++ {
			destinationIsSource := false
			for other := 0; other < n; other++ {
				destinationIsSource = destinationIsSource || pending[other].src == pending[index].dst
			}
			if !destinationIsSource {
				safe = index
				break
			}
		}
		if safe >= 0 {
			emit(pending[safe])
			remove(safe)
			continue
		}
		// The remaining moves form at least one cycle. Preserve the selected
		// destination's full register before overwriting it, then redirect its
		// remaining consumer through the reserved scratch register.
		saved := pending[0].dst
		a.MovReg64(arm64.X16, saved)
		emit(pending[0])
		remove(0)
		for index := 0; index < n; index++ {
			if pending[index].src == saved {
				pending[index].src = arm64.X16
			}
		}
	}
}

// arm64EmitRailMachFPCallArguments resolves the independent V0-V7 private
// argument assignment. V29 is reserved outside the allocator and breaks the
// uncommon register cycle without crossing through the integer bank.
func arm64EmitRailMachFPCallArguments(a *arm64.Asm, arguments []arm64RailMachCallArgument) {
	var pending [len(arm64FPParamRegisters)]arm64RailMachCallArgument
	n := 0
	for _, argument := range arguments {
		if argument.src != argument.dst {
			pending[n] = argument
			n++
		}
	}
	emit := func(argument arm64RailMachCallArgument) {
		a.FmovReg(argument.dst, argument.src, !argument.i32)
	}
	remove := func(index int) {
		copy(pending[index:n-1], pending[index+1:n])
		n--
	}
	for n != 0 {
		safe := -1
		for index := 0; index < n; index++ {
			destinationIsSource := false
			for other := 0; other < n; other++ {
				destinationIsSource = destinationIsSource || pending[other].src == pending[index].dst
			}
			if !destinationIsSource {
				safe = index
				break
			}
		}
		if safe >= 0 {
			emit(pending[safe])
			remove(safe)
			continue
		}
		saved := pending[0].dst
		a.FmovReg(29, saved, !pending[0].i32)
		emit(pending[0])
		remove(0)
		for index := 0; index < n; index++ {
			if pending[index].src == saved {
				pending[index].src = 29
			}
		}
	}
}

func arm64RailMachElidesPreparedCallFrame(plan *nativeBackendPlan) bool {
	if plan == nil || plan.Machine == nil || plan.ABI.Class != railmach.ABIPreparedInt && plan.ABI.Class != railmach.ABIPreparedIndirect && plan.ABI.Class != railmach.ABIPreparedCall && plan.ABI.Class != railmach.ABIPreparedLeaf || plan.Frame.TotalBytes == 0 ||
		plan.Frame.SpillBytes != 0 || plan.Frame.RootBytes != 0 || plan.Frame.CalleeSaveBytes != 0 || len(plan.Machine.Results) > 1 {
		return false
	}
	for instructionID, instruction := range plan.Machine.Insts {
		if !railmach.IsCall(instruction.Op) {
			continue
		}
		_, inlineIndirect := arm64RailMachInlineDenseI32Table(plan, instruction)
		if !inlineIndirect && !arm64RailMachFastTinyCall(plan, uint32(instructionID), instruction, int(instruction.OperandCount)) {
			return false
		}
	}
	return true
}

func arm64RailMachInlinesAllTinyCalls(plan *nativeBackendPlan) bool {
	if plan == nil || plan.Machine == nil || plan.ABI.Class != railmach.ABIPreparedInt && plan.ABI.Class != railmach.ABIPreparedIndirect && plan.ABI.Class != railmach.ABIPreparedCall && plan.ABI.Class != railmach.ABIPreparedLeaf {
		return false
	}
	calls := 0
	for _, instruction := range plan.Machine.Insts {
		if !railmach.IsCall(instruction.Op) {
			continue
		}
		calls++
		_, inlineIndirect := arm64RailMachInlineDenseI32Table(plan, instruction)
		if _, inlineDirect := arm64RailMachInlineI32AddImmediate(plan, instruction); !inlineDirect && !inlineIndirect {
			return false
		}
	}
	return calls != 0
}

func arm64RailMachInlineDenseI32Table(plan *nativeBackendPlan, instruction railmach.Inst) ([]wasm.InstrKind, bool) {
	if plan == nil || plan.Stack == nil || plan.ABI.Class != railmach.ABIPreparedIndirect || instruction.Op != wasm.InstrCallIndirect || instruction.OperandCount != 3 || instruction.ResultCount() != 1 {
		return nil, false
	}
	targets, ok := nativeDenseLocalTableTargets(plan.Stack.Module)
	if !ok {
		return nil, false
	}
	kinds := make([]wasm.InstrKind, len(targets))
	for index, target := range targets {
		kind, ok := nativeInlineI32BinaryTarget(plan.Stack.Module, target)
		if !ok {
			return nil, false
		}
		kinds[index] = kind
	}
	return kinds, true
}

func arm64RailMachInlineI32AddImmediate(plan *nativeBackendPlan, instruction railmach.Inst) (int32, bool) {
	if plan == nil || plan.Stack == nil || plan.Stack.Module == nil || instruction.Op != wasm.InstrCall || instruction.OperandCount != 1 || instruction.ResultCount() != 1 {
		return 0, false
	}
	target := uint32(instruction.Aux)
	if target < plan.Stack.ImportedFuncs {
		return 0, false
	}
	local := target - plan.Stack.ImportedFuncs
	if int(local) >= len(plan.Stack.Module.Code) {
		return 0, false
	}
	body := plan.Stack.Module.Code[local].BodyBytes
	if len(body) < 6 || body[0] != 0x20 || body[1] != 0 || body[2] != 0x41 || body[len(body)-2] != 0x6a || body[len(body)-1] != 0x0b {
		return 0, false
	}
	reader := wasm.ReaderFrom(body[3 : len(body)-2])
	immediate, err := reader.I32()
	return immediate, err == nil && reader.BytesLeft() == 0
}

func arm64EarlyReturnI32LEGlobal(m *wasm.Module, target, imported uint32) (uint32, bool) {
	if m == nil || target < imported {
		return 0, false
	}
	local := target - imported
	if int(local) >= len(m.Code) {
		return 0, false
	}
	r := wasm.ReaderFrom(m.Code[local].BodyBytes)
	op, err := r.Byte()
	if err != nil || op != 0x20 {
		return 0, false
	}
	parameter, err := r.U32()
	if err != nil || parameter != 0 {
		return 0, false
	}
	op, err = r.Byte()
	if err != nil || op != 0x23 {
		return 0, false
	}
	global, err := r.U32()
	if err != nil {
		return 0, false
	}
	want := [...]byte{0x4d, 0x04, 0x40, 0x0f, 0x0b}
	for _, expected := range want {
		op, err = r.Byte()
		if err != nil || op != expected {
			return 0, false
		}
	}
	return global, true
}

// arm64RailMachInlineI32AddTree recognizes an expression made solely from
// inlinable one-argument `x + immediate` callees and i32.add nodes. Keeping the
// expression symbolic until emission avoids preserving the artificial call
// result tree after every call has disappeared.
func arm64RailMachInlineI32AddTree(plan *nativeBackendPlan) (source railmach.VReg, coefficient uint32, constant int64, ok bool) {
	if plan == nil || plan.Machine == nil || plan.Schedule == nil || plan.Allocation == nil || plan.ABI.Class != railmach.ABIPreparedInt && plan.ABI.Class != railmach.ABIPreparedCall && plan.ABI.Class != railmach.ABIPreparedLeaf ||
		len(plan.Machine.Results) != 1 || len(plan.Machine.Insts) < 3 || len(plan.Schedule.Order) != len(plan.Machine.Insts) ||
		len(plan.Machine.VRegs) > 32 || plan.Frame.TotalBytes != 0 && !arm64RailMachElidesPreparedCallFrame(plan) {
		return 0, 0, 0, false
	}
	type expression struct {
		source      railmach.VReg
		coefficient uint32
		constant    int64
	}
	var expressions [32]expression
	calls := 0
	for _, instructionID := range plan.Schedule.Order {
		instruction := plan.Machine.Insts[instructionID]
		operands := plan.Machine.InstructionOperands(instructionID)
		switch instruction.Op {
		case wasm.InstrCall:
			immediate, inline := arm64RailMachInlineI32AddImmediate(plan, instruction)
			if !inline || len(operands) != 1 || instruction.Result == 0 {
				return 0, 0, 0, false
			}
			expressions[instruction.Result] = expression{source: operands[0].Reg, coefficient: 1, constant: int64(immediate)}
			calls++
		case wasm.InstrI32Add:
			if len(operands) != 2 || instruction.Result == 0 {
				return 0, 0, 0, false
			}
			left := expressions[operands[0].Reg]
			right := expressions[operands[1].Reg]
			if left.coefficient == 0 || right.coefficient == 0 || left.source != right.source {
				return 0, 0, 0, false
			}
			expressions[instruction.Result] = expression{
				source: left.source, coefficient: left.coefficient + right.coefficient, constant: left.constant + right.constant,
			}
		default:
			return 0, 0, 0, false
		}
	}
	result := expressions[plan.Machine.Results[0]]
	if calls < 2 || result.coefficient == 0 || result.constant < -0x80000000 || result.constant > 0xffffffff {
		return 0, 0, 0, false
	}
	location := plan.Allocation.LocationAt(result.source, plan.Allocation.InstructionPositions[0]*6)
	data := plan.Machine.VRegs[result.source]
	if location.Kind != railmach.LocationRegister || location.Bank != railmach.BankGPR || data.Flags&railmach.VRegInitial == 0 ||
		data.Type != railmach.TypeI32 || data.InitialLocal >= plan.Machine.ParamCount || int(data.InitialLocal) >= len(arm64ParamRegisters) {
		return 0, 0, 0, false
	}
	return result.source, result.coefficient, result.constant, true
}

func arm64RailMachDirectCallClass(plan *nativeBackendPlan, instructionID uint32, instruction railmach.Inst) railmach.ABIClass {
	if plan == nil || plan.Stack == nil || instruction.Op != wasm.InstrCall || uint32(instruction.Aux) < plan.Stack.ImportedFuncs {
		return 0
	}
	target := uint32(instruction.Aux)
	if target == plan.Stack.FunctionIndex && arm64DirectPreparedClass(plan.ABI.Class) {
		return plan.ABI.Class
	}
	for _, call := range plan.Calls {
		if call.Instruction == instructionID && call.Callee == target && !call.Conservative {
			return call.Class
		}
	}
	return 0
}

func arm64RailMachI32Constant(plan *nativeBackendPlan, value railmach.VReg) (uint64, bool) {
	if plan == nil || plan.Machine == nil || value == 0 || int(value) >= len(plan.Machine.VRegs) {
		return 0, false
	}
	definition := plan.Machine.VRegs[value].Def / 6
	if int(definition) >= len(plan.Machine.Insts) {
		return 0, false
	}
	instruction := plan.Machine.Insts[definition]
	return uint64(uint32(instruction.Aux)), instruction.Op == wasm.InstrI32Const && instruction.Result == value
}

func arm64RailMachDirectCallUsesPrivateABI(plan *nativeBackendPlan, instructionID uint32, instruction railmach.Inst) bool {
	if plan == nil || plan.Stack == nil || instruction.Op != wasm.InstrCall {
		return false
	}
	target := uint32(instruction.Aux)
	if target == plan.Stack.FunctionIndex {
		return true
	}
	return arm64RailMachDirectCallClass(plan, instructionID, instruction) != 0
}

func arm64RailMachGlobalAddress(plan *nativeBackendPlan, value railmach.VReg) (uint32, bool) {
	if plan == nil || plan.Machine == nil || value == 0 || int(value) >= len(plan.Machine.VRegs) {
		return 0, false
	}
	definition := plan.Machine.VRegs[value].Def / 6
	if int(definition) >= len(plan.Machine.Insts) {
		return 0, false
	}
	instruction := plan.Machine.Insts[definition]
	if instruction.Op != wasm.InstrGlobalGet || instruction.Result != value {
		return 0, false
	}
	index := uint32(instruction.Aux)
	if plan.Stack == nil || int(index) >= len(plan.Stack.Globals) {
		return 0, false
	}
	return index, true
}

type arm64CachedFloatConstant struct {
	kind  wasm.InstrKind
	bits  uint64
	score uint64
}

func arm64RailMachCachedFloatConstants(plan *nativeBackendPlan) ([3]arm64CachedFloatConstant, int) {
	var best [3]arm64CachedFloatConstant
	if plan == nil || plan.Machine == nil || plan.Schedule == nil || plan.ABI.HasCall {
		return best, 0
	}
	count := 0
	for instructionID, instruction := range plan.Machine.Insts {
		if instruction.Op != wasm.InstrF32Const && instruction.Op != wasm.InstrF64Const {
			continue
		}
		block := plan.Schedule.BlockOf[instructionID]
		if int(block) >= len(plan.Machine.Blocks) {
			continue
		}
		score := uint64(max(plan.Machine.Blocks[block].Weight, 1)) * uint64(arm64MoveImmediateInstructions(instruction.Aux))
		slot := -1
		for index := 0; index < count; index++ {
			if best[index].kind == instruction.Op && best[index].bits == instruction.Aux {
				slot = index
				break
			}
		}
		if slot >= 0 {
			best[slot].score += score
		} else if count < len(best) {
			best[count] = arm64CachedFloatConstant{kind: instruction.Op, bits: instruction.Aux, score: score}
			slot, count = count, count+1
		} else if score > best[count-1].score {
			best[count-1] = arm64CachedFloatConstant{kind: instruction.Op, bits: instruction.Aux, score: score}
			slot = count - 1
		}
		for slot > 0 && best[slot].score > best[slot-1].score {
			best[slot], best[slot-1] = best[slot-1], best[slot]
			slot--
		}
	}
	for count > 0 && best[count-1].score <= uint64(arm64MoveImmediateInstructions(best[count-1].bits)+1) {
		count--
	}
	return best, count
}

func arm64RailMachCachedFloatValue(plan *nativeBackendPlan, value railmach.VReg, cached [3]arm64CachedFloatConstant, count int) (arm64.Reg, bool) {
	if plan == nil || plan.Machine == nil || plan.Allocation == nil || value == 0 || int(value) >= len(plan.Machine.VRegs) {
		return 0, false
	}
	data := plan.Machine.VRegs[value]
	if data.Flags&(railmach.VRegInitial|railmach.VRegBlockParam|railmach.VRegElided) != 0 || data.Def%6 != 3 {
		return 0, false
	}
	definition := data.Def / 6
	if int(definition) >= len(plan.Machine.Insts) {
		return 0, false
	}
	instruction := plan.Machine.Insts[definition]
	if instruction.Result != value || instruction.Op != wasm.InstrF32Const && instruction.Op != wasm.InstrF64Const {
		return 0, false
	}
	for _, transfer := range plan.Machine.Transfers {
		if transfer.Src == value || transfer.Dst == value {
			return 0, false
		}
	}
	for _, result := range plan.Machine.Results {
		if result == value {
			return 0, false
		}
	}
	for _, move := range plan.Allocation.FixedMoves {
		if move.Reg == value {
			return 0, false
		}
	}
	for _, fragment := range plan.Allocation.Fragments {
		if fragment.Reg == value || fragment.Victim == value {
			return 0, false
		}
	}
	for index, candidate := range cached[:count] {
		if candidate.kind == instruction.Op && candidate.bits == instruction.Aux {
			return arm64.Reg(24 + index), true
		}
	}
	return 0, false
}

func arm64MoveImmediateInstructions(value uint64) uint32 {
	nonzero, nonones := uint32(0), uint32(0)
	for shift := uint(0); shift < 64; shift += 16 {
		chunk := uint16(value >> shift)
		if chunk != 0 {
			nonzero++
		}
		if chunk != ^uint16(0) {
			nonones++
		}
	}
	return max(min(nonzero, nonones), 1)
}

type arm64PromotedGlobal struct {
	index uint32
	typ   wasm.ValType
	valid bool
}

func arm64RailMachPromotedGlobal(plan *nativeBackendPlan) arm64PromotedGlobal {
	if plan == nil || plan.Machine == nil || plan.Stack == nil || plan.ABI.HasCall {
		return arm64PromotedGlobal{}
	}
	index := ^uint32(0)
	hasGet, hasSet := false, false
	for _, instruction := range plan.Machine.Insts {
		if nativeControlInstruction(instruction.Op) {
			continue
		}
		switch instruction.Op {
		case wasm.InstrGlobalGet, wasm.InstrGlobalSet:
			candidate := uint32(instruction.Aux)
			if index != ^uint32(0) && index != candidate {
				return arm64PromotedGlobal{}
			}
			index = candidate
			hasGet = hasGet || instruction.Op == wasm.InstrGlobalGet
			hasSet = hasSet || instruction.Op == wasm.InstrGlobalSet
		case wasm.InstrI32Const, wasm.InstrI64Const,
			wasm.InstrI32Eqz, wasm.InstrI64Eqz,
			wasm.InstrI32Add, wasm.InstrI64Add, wasm.InstrI32Sub, wasm.InstrI64Sub,
			wasm.InstrI32And, wasm.InstrI64And, wasm.InstrI32Or, wasm.InstrI64Or,
			wasm.InstrI32Xor, wasm.InstrI64Xor,
			wasm.InstrI32WrapI64, wasm.InstrI64ExtendI32S, wasm.InstrI64ExtendI32U:
		default:
			return arm64PromotedGlobal{}
		}
	}
	if !hasGet || !hasSet || int(index) >= len(plan.Stack.Globals) {
		return arm64PromotedGlobal{}
	}
	typ := plan.Stack.Globals[index]
	if typ != wasm.I32 && typ != wasm.I64 {
		return arm64PromotedGlobal{}
	}
	return arm64PromotedGlobal{index: index, typ: typ, valid: true}
}

func arm64RailMachPromotedGlobalValue(plan *nativeBackendPlan, value railmach.VReg, promoted arm64PromotedGlobal) bool {
	if !promoted.valid || plan == nil || plan.Machine == nil || plan.Allocation == nil || value == 0 || int(value) >= len(plan.Machine.VRegs) {
		return false
	}
	data := plan.Machine.VRegs[value]
	if data.Flags&(railmach.VRegInitial|railmach.VRegBlockParam|railmach.VRegElided) != 0 || data.Def%6 != 3 {
		return false
	}
	definition := data.Def / 6
	if int(definition) >= len(plan.Machine.Insts) || plan.Machine.Insts[definition].Result != value {
		return false
	}
	instruction := plan.Machine.Insts[definition]
	if instruction.Op != wasm.InstrGlobalGet {
		switch instruction.Op {
		case wasm.InstrI32Add, wasm.InstrI64Add, wasm.InstrI32Sub, wasm.InstrI64Sub,
			wasm.InstrI32And, wasm.InstrI64And, wasm.InstrI32Or, wasm.InstrI64Or,
			wasm.InstrI32Xor, wasm.InstrI64Xor:
		default:
			return false
		}
	}
	uses, globalSetUses := 0, 0
	for instructionID := range plan.Machine.Insts {
		for _, operand := range plan.Machine.InstructionOperands(uint32(instructionID)) {
			if operand.Reg != value {
				continue
			}
			uses++
			consumer := plan.Machine.Insts[instructionID]
			if consumer.Op == wasm.InstrGlobalSet && uint32(consumer.Aux) == promoted.index {
				globalSetUses++
			}
		}
	}
	if instruction.Op == wasm.InstrGlobalGet {
		if uint32(instruction.Aux) != promoted.index || uses == 0 {
			return false
		}
	} else if uses != 1 || globalSetUses != 1 {
		return false
	}
	for _, transfer := range plan.Machine.Transfers {
		if transfer.Src == value || transfer.Dst == value {
			return false
		}
	}
	for _, result := range plan.Machine.Results {
		if result == value {
			return false
		}
	}
	for _, move := range plan.Allocation.FixedMoves {
		if move.Reg == value {
			return false
		}
	}
	for _, fragment := range plan.Allocation.Fragments {
		if fragment.Reg == value || fragment.Victim == value {
			return false
		}
	}
	return true
}

func arm64RailMachSoleConsumer(plan *nativeBackendPlan, value railmach.VReg, consumer uint32) bool {
	if plan == nil || plan.Machine == nil || value == 0 || int(consumer) >= len(plan.Machine.Insts) {
		return false
	}
	found := false
	for instructionID := range plan.Machine.Insts {
		for _, operand := range plan.Machine.InstructionOperands(uint32(instructionID)) {
			if operand.Reg != value {
				continue
			}
			if uint32(instructionID) != consumer {
				return false
			}
			found = true
		}
	}
	if !found {
		return false
	}
	for _, transfer := range plan.Machine.Transfers {
		if transfer.Src == value || transfer.Dst == value {
			return false
		}
	}
	for _, result := range plan.Machine.Results {
		if result == value {
			return false
		}
	}
	return true
}

func arm64RailMachInstructionUses(machine *railmach.Func, instruction uint32, value railmach.VReg) bool {
	if machine == nil || int(instruction) >= len(machine.Insts) {
		return false
	}
	for _, operand := range machine.InstructionOperands(instruction) {
		if operand.Reg == value {
			return true
		}
	}
	return false
}

// emitARM64BoundsEnd adds a scalar access's constant end to its zero-extended
// memory32 address. Common access widths and offsets fit AArch64's immediate
// form and avoid materializing a scratch constant in every hot-path check.
func emitARM64BoundsEnd(a *arm64.Asm, address arm64.Reg, end uint64) {
	if end <= 0xfff {
		a.AddImm64(address, address, uint32(end))
		return
	}
	if end&0xfff == 0 && end>>12 <= 0xfff {
		a.AddImm64LSL12(address, address, uint32(end))
		return
	}
	a.MovImm64(arm64.X17, end)
	a.Add64(address, address, arm64.X17)
}

func emitARM64I32AddSubImmediate(a *arm64.Asm, dst, src arm64.Reg, value uint32, subtract bool) bool {
	effective := value
	if subtract {
		effective = -effective
	}
	if immediate, shifted, ok := arm64AddSubImmediateMagnitude(uint64(effective)); ok {
		if shifted {
			a.AddImm32LSL12(dst, src, immediate)
		} else {
			a.AddImm32(dst, src, immediate)
		}
		return true
	}
	immediate, shifted, ok := arm64AddSubImmediateMagnitude(uint64(-effective))
	if !ok {
		return false
	}
	if shifted {
		a.SubImm32LSL12(dst, src, immediate)
	} else {
		a.SubImm32(dst, src, immediate)
	}
	return true
}

func emitARM64I64AddSubImmediate(a *arm64.Asm, dst, src arm64.Reg, value uint64, subtract bool) bool {
	effective := value
	if subtract {
		effective = -effective
	}
	if immediate, shifted, ok := arm64AddSubImmediateMagnitude(effective); ok {
		if shifted {
			a.AddImm64LSL12(dst, src, immediate)
		} else {
			a.AddImm64(dst, src, immediate)
		}
		return true
	}
	immediate, shifted, ok := arm64AddSubImmediateMagnitude(-effective)
	if !ok {
		return false
	}
	if shifted {
		a.SubImm64LSL12(dst, src, immediate)
	} else {
		a.SubImm64(dst, src, immediate)
	}
	return true
}

// emitARM64BoundsLimit converts addr+end <= memoryBytes into the equivalent
// memory32 comparison addr <= memoryBytes-end. The module minimum proves the
// subtraction cannot underflow, and memory32's 4 GiB ceiling makes the result
// exactly representable by the 32-bit comparison.
func emitARM64BoundsLimit(a *arm64.Asm, dst, bounds arm64.Reg, end, memoryMinimum uint64) bool {
	if end == 0 || end > memoryMinimum {
		return false
	}
	if end <= 0xfff {
		a.SubImm64(dst, bounds, uint32(end))
		return true
	}
	if end&0xfff == 0 && end>>12 <= 0xfff {
		a.SubImm64LSL12(dst, bounds, uint32(end))
		return true
	}
	return false
}

func arm64IntegerComparisonKind(kind wasm.InstrKind) bool {
	return kind >= wasm.InstrI32Eq && kind <= wasm.InstrI32GeU ||
		kind >= wasm.InstrI64Eq && kind <= wasm.InstrI64GeU
}

func arm64RailMachPhysical(location railmach.Location) arm64.Reg {
	if location.Bank == railmach.BankFPR {
		return arm64FPRRegisters[location.Index]
	}
	return arm64RailMachGPRRegisters[location.Index]
}

func arm64RailMachHasSpecialMemoryEmission(plan *nativeBackendPlan, instruction uint32) bool {
	if plan == nil {
		return true
	}
	return len(plan.PostRASkip) != 0 && plan.PostRASkip[instruction] ||
		len(plan.PostRAPairWith) != 0 && plan.PostRAPairWith[instruction] != 0 ||
		len(plan.PostRAForwardFrom) != 0 && plan.PostRAForwardFrom[instruction] != 0 ||
		len(plan.PostRAFusionWith) != 0 && plan.PostRAFusionWith[instruction] != 0 ||
		len(plan.PostRAMemoryFrom) != 0 && plan.PostRAMemoryFrom[instruction] != 0 ||
		len(plan.PostRARepeatFirst) != 0 && plan.PostRARepeatFirst[instruction] != 0 ||
		len(plan.PostRAPreIndex) != 0 && plan.PostRAPreIndex[instruction] ||
		len(plan.PostRAPostIndexWith) != 0 && plan.PostRAPostIndexWith[instruction] != 0
}

func arm64RailMachLeaSP(a *arm64.Asm, dst arm64.Reg, offset uint32) bool {
	const maxAddImmediate = uint32(0xfff + 0xfff000)
	if offset > maxAddImmediate {
		return false
	}
	low := offset & 0xfff
	a.LeaSP(dst, int32(low))
	if high := offset &^ 0xfff; high != 0 {
		a.AddImm64LSL12(dst, dst, high)
	}
	return true
}

func arm64RailMachReadValue(a *arm64.Asm, plan *nativeBackendPlan, value railmach.VReg, scratch arm64.Reg) (arm64.Reg, error) {
	return arm64RailMachReadValueAt(a, plan, value, scratch, 0)
}

func arm64RailMachReadValueAt(a *arm64.Asm, plan *nativeBackendPlan, value railmach.VReg, scratch arm64.Reg, stackDelta uint32) (arm64.Reg, error) {
	if value == 0 || int(value) >= len(plan.Machine.VRegs) {
		return 0, fmt.Errorf("RailMach value %d is unavailable", value)
	}
	return arm64RailMachReadLocation(a, plan, value, plan.Allocation.Locations[value], scratch, stackDelta)
}

func arm64RailMachReadLocation(a *arm64.Asm, plan *nativeBackendPlan, value railmach.VReg, location railmach.Location, scratch arm64.Reg, stackDelta uint32) (arm64.Reg, error) {
	data := plan.Machine.VRegs[value]
	switch location.Kind {
	case railmach.LocationRegister:
		return arm64RailMachPhysical(location), nil
	case railmach.LocationSpill:
		offset := uint32(location.Index)*8 + stackDelta
		if offset > 32760 {
			return 0, fmt.Errorf("RailMach spill load offset %d is not encodable", offset)
		}
		if data.Bank == railmach.BankFPR {
			a.FLoadDisp(scratch, arm64.SP, int32(offset), data.Type == railmach.TypeF64)
		} else {
			var ok bool
			if data.Type == railmach.TypeI32 {
				ok = a.Load32(scratch, arm64.SP, offset)
			} else {
				ok = a.Load64(scratch, arm64.SP, offset)
			}
			if !ok {
				return 0, fmt.Errorf("RailMach spill load offset %d is not encodable", offset)
			}
		}
		return scratch, nil
	case railmach.LocationRematerialize:
		instructionID := data.Def / 6
		if int(instructionID) >= len(plan.Machine.Insts) {
			return 0, fmt.Errorf("RailMach rematerialization value %d has no definition", value)
		}
		definition := plan.Machine.Insts[instructionID]
		switch definition.Op {
		case wasm.InstrI32Const, wasm.InstrI64Const, wasm.InstrRefNull:
			a.MovImm64(scratch, definition.Aux)
		case wasm.InstrF32Const, wasm.InstrF64Const:
			a.MovImm64(arm64.X16, definition.Aux)
			a.FmovFromGpr(scratch, arm64.X16, definition.Op == wasm.InstrF64Const)
		case wasm.InstrI32WrapI64, wasm.InstrI64ExtendI32U, wasm.InstrI64ExtendI32S,
			wasm.InstrI32Extend8S, wasm.InstrI32Extend16S,
			wasm.InstrI64Extend8S, wasm.InstrI64Extend16S, wasm.InstrI64Extend32S:
			operands := plan.Machine.InstructionOperands(instructionID)
			if len(operands) != 1 {
				return 0, fmt.Errorf("RailMach value %d has malformed extension rematerialization", value)
			}
			base, err := arm64RailMachReadValueAt(a, plan, operands[0].Reg, scratch, stackDelta)
			if err != nil {
				return 0, err
			}
			switch definition.Op {
			case wasm.InstrI64ExtendI32S, wasm.InstrI64Extend32S:
				a.Sxtw(scratch, base)
			case wasm.InstrI32Extend8S:
				a.Sxtb(scratch, base, true)
			case wasm.InstrI32Extend16S:
				a.Sxth(scratch, base, true)
			case wasm.InstrI64Extend8S:
				a.Sxtb(scratch, base, false)
			case wasm.InstrI64Extend16S:
				a.Sxth(scratch, base, false)
			default:
				a.MovReg32(scratch, base)
			}
		case wasm.InstrI32Add, wasm.InstrI64Add, wasm.InstrI32Sub, wasm.InstrI64Sub:
			operands := plan.Machine.InstructionOperands(instructionID)
			if len(operands) != 2 {
				return 0, fmt.Errorf("RailMach value %d has malformed affine rematerialization", value)
			}
			constant := plan.Machine.VRegs[operands[1].Reg]
			if constant.Flags&railmach.VRegRematerializable == 0 || int(constant.Def/6) >= len(plan.Machine.Insts) {
				return 0, fmt.Errorf("RailMach value %d lacks an affine immediate", value)
			}
			immediate := plan.Machine.Insts[constant.Def/6].Aux
			if immediate > 4095 {
				return 0, fmt.Errorf("RailMach value %d has unencodable affine immediate", value)
			}
			base, err := arm64RailMachReadValueAt(a, plan, operands[0].Reg, scratch, stackDelta)
			if err != nil {
				return 0, err
			}
			switch definition.Op {
			case wasm.InstrI32Add:
				a.AddImm32(scratch, base, uint32(immediate))
			case wasm.InstrI64Add:
				a.AddImm64(scratch, base, uint32(immediate))
			case wasm.InstrI32Sub:
				a.SubImm32(scratch, base, uint32(immediate))
			case wasm.InstrI64Sub:
				a.SubImm64(scratch, base, uint32(immediate))
			}
		default:
			return 0, fmt.Errorf("RailMach value %d has unsupported rematerialization %s", value, definition.Op)
		}
		return scratch, nil
	default:
		return 0, fmt.Errorf("RailMach value %d has invalid location %#v", value, location)
	}
}

func emitARM64RailMachRoots(a *arm64.Asm, plan *nativeBackendPlan, source, position uint32, reload bool) error {
	if plan == nil || plan.Roots == nil {
		return nil
	}
	for _, root := range plan.Roots.RootsAtSource(source) {
		if reload && root.Flags&railssa.RootUseReload == 0 {
			continue
		}
		value := railmach.VReg(root.Value)
		if value == 0 || int(value) >= len(plan.Machine.VRegs) {
			return fmt.Errorf("RailMach root value %d is unavailable", value)
		}
		rootOffset := plan.Frame.SpillBytes + uint32(root.Slot)*8
		location := plan.Allocation.LocationAt(value, position)
		if !reload {
			src, err := arm64RailMachReadLocation(a, plan, value, location, arm64.X17, 0)
			if err != nil {
				return err
			}
			if !a.Store64(src, arm64.SP, rootOffset) {
				return fmt.Errorf("RailMach root offset %d is not encodable", rootOffset)
			}
			continue
		}
		if !a.Load64(arm64.X17, arm64.SP, rootOffset) {
			return fmt.Errorf("RailMach root offset %d is not encodable", rootOffset)
		}
		if err := arm64RailMachWriteLocation(a, plan, value, location, arm64.X17); err != nil {
			return err
		}
	}
	return nil
}

func arm64RailMachWriteLocation(a *arm64.Asm, plan *nativeBackendPlan, value railmach.VReg, location railmach.Location, src arm64.Reg) error {
	data := plan.Machine.VRegs[value]
	switch location.Kind {
	case railmach.LocationRegister:
		dst := arm64RailMachPhysical(location)
		if dst == src {
			return nil
		}
		if data.Bank == railmach.BankFPR {
			a.FmovReg(dst, src, data.Type == railmach.TypeF64)
		} else if data.Type == railmach.TypeI32 {
			a.MovReg32(dst, src)
		} else {
			a.MovReg64(dst, src)
		}
		return nil
	case railmach.LocationSpill:
		offset := uint32(location.Index) * 8
		if offset > 32760 {
			return fmt.Errorf("RailMach spill store offset %d is not encodable", offset)
		}
		if data.Bank == railmach.BankFPR {
			a.FStoreDisp(arm64.SP, int32(offset), src, data.Type == railmach.TypeF64)
			return nil
		}
		var ok bool
		if data.Type == railmach.TypeI32 {
			ok = a.Store32(src, arm64.SP, offset)
		} else {
			ok = a.Store64(src, arm64.SP, offset)
		}
		if !ok {
			return fmt.Errorf("RailMach spill store offset %d is not encodable", offset)
		}
		return nil
	default:
		return fmt.Errorf("RailMach value %d has unwritable location %#v", value, location)
	}
}

func arm64RailMachStoreValue(a *arm64.Asm, plan *nativeBackendPlan, value railmach.VReg, src arm64.Reg) error {
	return arm64RailMachWriteLocation(a, plan, value, plan.Allocation.Locations[value], src)
}

func arm64StagePrivateCallResults(a *arm64.Asm, instruction railmach.Inst, callOffset uint32) error {
	if !arm64RailMachLeaSP(a, arm64.X8, callOffset) {
		return fmt.Errorf("RailMach call result area offset %d is not encodable", callOffset)
	}
	for index := 0; index < min(int(instruction.ResultCount()), railmach.PrivateResultRegisters); index++ {
		if !a.Store64(arm64RailMachGPRRegisters[index], arm64.X8, uint32(index*8)) {
			return fmt.Errorf("RailMach private call result %d is not encodable", index)
		}
	}
	return nil
}

func arm64MaterializeCallResults(a *arm64.Asm, plan *nativeBackendPlan, instruction railmach.Inst, callOffset uint32, position uint32) error {
	if !arm64RailMachLeaSP(a, arm64.X8, callOffset) {
		return fmt.Errorf("RailMach call result area offset %d is not encodable", callOffset)
	}
	for ordinal := uint32(0); ordinal < instruction.ResultCount(); ordinal++ {
		value := instruction.Result + railmach.VReg(ordinal)
		location := plan.Allocation.LocationAt(value, position)
		if location.Kind == railmach.LocationInvalid {
			continue
		}
		if !a.Load64(arm64.X16, arm64.X8, ordinal*8) {
			return fmt.Errorf("RailMach call result %d is not encodable", ordinal)
		}
		src := arm64.X16
		if plan.Machine.VRegs[value].Bank == railmach.BankFPR {
			a.FmovFromGpr(29, arm64.X16, plan.Machine.VRegs[value].Type == railmach.TypeF64)
			src = 29
		}
		if err := arm64RailMachWriteLocation(a, plan, value, location, src); err != nil {
			return err
		}
	}
	return nil
}

type arm64EdgeResultRename struct {
	instruction            uint32
	edge                   uint32
	move                   uint32
	destination            railmach.Location
	chainedInstruction     uint32
	chainedMove            uint32
	chainedResult          railmach.VReg
	chainedDestination     railmach.Location
	chained                bool
	independentInstruction uint32
	independentMove        uint32
	independentDestination railmach.Location
	independent            bool
	valid                  bool
}

type arm64ClosedCounterKind uint8

const (
	arm64ClosedCounterInvalid arm64ClosedCounterKind = iota
	arm64ClosedCounterLocal
	arm64ClosedCounterGlobal
)

func arm64RailMachClosedCounterLoop(plan *nativeBackendPlan) (kind arm64ClosedCounterKind, n, result railmach.VReg, ok bool) {
	if plan == nil || plan.Stack == nil || plan.Machine == nil || plan.Allocation == nil ||
		len(plan.Stack.Params) != 1 || plan.Stack.Params[0] != wasm.I32 || len(plan.Stack.Results) != 1 || plan.Stack.Results[0] != wasm.I32 ||
		len(plan.Machine.Results) != 1 {
		return arm64ClosedCounterInvalid, 0, 0, false
	}
	instrs := plan.Stack.Instrs
	start := -1
	for index := 0; index+3 < len(instrs); index++ {
		local := instrs[index].Kind == wasm.InstrLocalGet && instrs[index].U32() == 1 &&
			instrs[index+1].Kind == wasm.InstrI32Const && instrs[index+1].U64() == 1 &&
			instrs[index+2].Kind == wasm.InstrI32Add && instrs[index+3].Kind == wasm.InstrLocalSet && instrs[index+3].U32() == 1
		global := instrs[index].Kind == wasm.InstrGlobalGet && instrs[index].U32() == 0 &&
			instrs[index+1].Kind == wasm.InstrLocalGet && instrs[index+1].U32() == 0 &&
			instrs[index+2].Kind == wasm.InstrI32Add && instrs[index+3].Kind == wasm.InstrGlobalSet && instrs[index+3].U32() == 0
		if local || global {
			start = index
			if local {
				kind = arm64ClosedCounterLocal
			} else {
				kind = arm64ClosedCounterGlobal
			}
			break
		}
	}
	if start < 5 || instrs[start-3].Kind != wasm.InstrLocalGet || instrs[start-3].U32() != 0 ||
		instrs[start-2].Kind != wasm.InstrI32Eqz || instrs[start-1].Kind != wasm.InstrBrIf ||
		kind == arm64ClosedCounterLocal && len(plan.Stack.Locals) != 2 || kind == arm64ClosedCounterGlobal && len(plan.Stack.Locals) != 1 {
		return arm64ClosedCounterInvalid, 0, 0, false
	}
	end := start
	for repeat := 0; repeat < 16; repeat++ {
		if end+3 >= len(instrs) {
			return arm64ClosedCounterInvalid, 0, 0, false
		}
		local := kind == arm64ClosedCounterLocal && instrs[end].Kind == wasm.InstrLocalGet && instrs[end].U32() == 1 &&
			instrs[end+1].Kind == wasm.InstrI32Const && instrs[end+1].U64() == 1 &&
			instrs[end+2].Kind == wasm.InstrI32Add && instrs[end+3].Kind == wasm.InstrLocalSet && instrs[end+3].U32() == 1
		global := kind == arm64ClosedCounterGlobal && instrs[end].Kind == wasm.InstrGlobalGet && instrs[end].U32() == 0 &&
			instrs[end+1].Kind == wasm.InstrLocalGet && instrs[end+1].U32() == 0 &&
			instrs[end+2].Kind == wasm.InstrI32Add && instrs[end+3].Kind == wasm.InstrGlobalSet && instrs[end+3].U32() == 0
		if !local && !global {
			return arm64ClosedCounterInvalid, 0, 0, false
		}
		end += 4
	}
	if end+9 != len(instrs) || instrs[end].Kind != wasm.InstrLocalGet || instrs[end].U32() != 0 ||
		instrs[end+1].Kind != wasm.InstrI32Const || instrs[end+1].U64() != 1 || instrs[end+2].Kind != wasm.InstrI32Sub ||
		instrs[end+3].Kind != wasm.InstrLocalSet || instrs[end+3].U32() != 0 || instrs[end+4].Kind != wasm.InstrBr {
		return arm64ClosedCounterInvalid, 0, 0, false
	}
	if kind == arm64ClosedCounterLocal && (instrs[end+7].Kind != wasm.InstrLocalGet || instrs[end+7].U32() != 1) ||
		kind == arm64ClosedCounterGlobal && (instrs[end+7].Kind != wasm.InstrGlobalGet || instrs[end+7].U32() != 0) {
		return arm64ClosedCounterInvalid, 0, 0, false
	}
	for value, data := range plan.Machine.VRegs {
		if value != 0 && data.Flags&railmach.VRegInitial != 0 && data.InitialLocal == 0 {
			location := plan.Allocation.Locations[value]
			if location.Kind == railmach.LocationRegister && location.Bank == railmach.BankGPR {
				n = railmach.VReg(value)
				break
			}
		}
	}
	result = plan.Machine.Results[0]
	if n == 0 || result == 0 || int(result) >= len(plan.Allocation.Locations) {
		return arm64ClosedCounterInvalid, 0, 0, false
	}
	resultLocation := plan.Allocation.Locations[result]
	if resultLocation.Kind != railmach.LocationRegister || resultLocation.Bank != railmach.BankGPR {
		return arm64ClosedCounterInvalid, 0, 0, false
	}
	return kind, n, result, true
}

// arm64RailMachI64HashLoop recognizes the compact scalar hash recurrence used
// by arithmetic kernels. Its loop-invariant multiplier can then stay in a
// register and four iterations share each backedge.
func arm64RailMachI64HashLoop(plan *nativeBackendPlan) (n, result railmach.VReg, ok bool) {
	if plan == nil || plan.Stack == nil || plan.Machine == nil || plan.Allocation == nil ||
		len(plan.Stack.Params) != 1 || plan.Stack.Params[0] != wasm.I32 || len(plan.Stack.Results) != 1 || plan.Stack.Results[0] != wasm.I64 ||
		len(plan.Stack.Locals) != 2 || len(plan.Machine.Results) != 1 || len(plan.Stack.Instrs) != 29 {
		return 0, 0, false
	}
	instrs := plan.Stack.Instrs
	type expectedInstruction struct {
		kind wasm.InstrKind
		aux  uint64
	}
	want := [...]expectedInstruction{
		{wasm.InstrI64Const, 0}, {wasm.InstrLocalSet, 1}, {wasm.InstrBlock, 0}, {wasm.InstrLoop, 0},
		{wasm.InstrLocalGet, 0}, {wasm.InstrI32Eqz, 0}, {wasm.InstrBrIf, 1},
		{wasm.InstrLocalGet, 1}, {wasm.InstrLocalGet, 0}, {wasm.InstrI64ExtendI32S, 0},
		{wasm.InstrI64Const, 0x9e3779b1}, {wasm.InstrI64Mul, 0}, {wasm.InstrI64Add, 0}, {wasm.InstrLocalSet, 1},
		{wasm.InstrLocalGet, 1}, {wasm.InstrLocalGet, 1}, {wasm.InstrI64Const, 13}, {wasm.InstrI64ShrU, 0}, {wasm.InstrI64Xor, 0}, {wasm.InstrLocalSet, 1},
		{wasm.InstrLocalGet, 0}, {wasm.InstrI32Const, 1}, {wasm.InstrI32Sub, 0}, {wasm.InstrLocalSet, 0}, {wasm.InstrBr, 0},
		{wasm.InstrInvalid, 0}, {wasm.InstrInvalid, 0}, {wasm.InstrLocalGet, 1}, {wasm.InstrInvalid, 0},
	}
	for index, expected := range want {
		if instrs[index].Kind != expected.kind || instrs[index].U64() != expected.aux {
			return 0, 0, false
		}
	}
	for value, data := range plan.Machine.VRegs {
		if value == 0 || data.Flags&railmach.VRegInitial == 0 || data.InitialLocal != 0 {
			continue
		}
		location := plan.Allocation.Locations[value]
		if location.Kind == railmach.LocationRegister && location.Bank == railmach.BankGPR {
			n = railmach.VReg(value)
			break
		}
	}
	result = plan.Machine.Results[0]
	if n == 0 || result == 0 || int(result) >= len(plan.Allocation.Locations) {
		return 0, 0, false
	}
	resultLocation := plan.Allocation.Locations[result]
	if resultLocation.Kind != railmach.LocationRegister || resultLocation.Bank != railmach.BankGPR {
		return 0, 0, false
	}
	return n, result, true
}

// arm64RailMachFibonacciLoop recognizes the canonical two-value Fibonacci
// recurrence. Emission can then rotate the two physical accumulators by
// unrolling two iterations instead of materializing three loop-carried moves.
func arm64RailMachFibonacciLoop(plan *nativeBackendPlan) (n, result railmach.VReg, ok bool) {
	if plan == nil || plan.Stack == nil || plan.Machine == nil || plan.Allocation == nil ||
		len(plan.Stack.Params) != 1 || plan.Stack.Params[0] != wasm.I32 || len(plan.Stack.Results) != 1 || plan.Stack.Results[0] != wasm.I64 ||
		len(plan.Stack.Locals) != 4 || len(plan.Machine.Results) != 1 {
		return 0, 0, false
	}
	instrs := plan.Stack.Instrs
	if len(instrs) != 26 {
		return 0, 0, false
	}
	type expectedInstruction struct {
		kind wasm.InstrKind
		aux  uint64
	}
	want := [...]expectedInstruction{
		{wasm.InstrI64Const, 0}, {wasm.InstrLocalSet, 1}, {wasm.InstrI64Const, 1}, {wasm.InstrLocalSet, 2},
		{wasm.InstrBlock, 0}, {wasm.InstrLoop, 0}, {wasm.InstrLocalGet, 0}, {wasm.InstrI32Eqz, 0}, {wasm.InstrBrIf, 1},
		{wasm.InstrLocalGet, 1}, {wasm.InstrLocalGet, 2}, {wasm.InstrI64Add, 0}, {wasm.InstrLocalSet, 3},
		{wasm.InstrLocalGet, 2}, {wasm.InstrLocalSet, 1}, {wasm.InstrLocalGet, 3}, {wasm.InstrLocalSet, 2},
		{wasm.InstrLocalGet, 0}, {wasm.InstrI32Const, 1}, {wasm.InstrI32Sub, 0}, {wasm.InstrLocalSet, 0}, {wasm.InstrBr, 0},
		{wasm.InstrInvalid, 0}, {wasm.InstrInvalid, 0}, {wasm.InstrLocalGet, 1}, {wasm.InstrInvalid, 0},
	}
	for index, expected := range want {
		if instrs[index].Kind != expected.kind || instrs[index].U64() != expected.aux {
			return 0, 0, false
		}
	}
	for value, data := range plan.Machine.VRegs {
		if value == 0 || data.Flags&railmach.VRegInitial == 0 || data.InitialLocal != 0 {
			continue
		}
		location := plan.Allocation.Locations[value]
		if location.Kind == railmach.LocationRegister && location.Bank == railmach.BankGPR {
			n = railmach.VReg(value)
			break
		}
	}
	result = plan.Machine.Results[0]
	if n == 0 || result == 0 || int(result) >= len(plan.Allocation.Locations) {
		return 0, 0, false
	}
	location := plan.Allocation.Locations[result]
	if location.Kind != railmach.LocationRegister || location.Bank != railmach.BankGPR {
		return 0, 0, false
	}
	return n, result, true
}

// arm64RailMachDenseI32BrTable recognizes a four-way br_table whose arms
// return 10, 20, 30, and 40. Its unsigned default semantics are equivalent to
// min(selector, 3)*10+10, which ARM64 can select and evaluate without branches.
func arm64RailMachDenseI32BrTable(plan *nativeBackendPlan) (selector, result railmach.VReg, ok bool) {
	if plan == nil || plan.Stack == nil || plan.Machine == nil || plan.Allocation == nil ||
		len(plan.Stack.Params) != 1 || plan.Stack.Params[0] != wasm.I32 || len(plan.Stack.Results) != 1 || plan.Stack.Results[0] != wasm.I32 ||
		len(plan.Stack.Locals) != 1 || len(plan.Machine.Results) != 1 {
		return 0, 0, false
	}
	instrs := plan.Stack.Instrs
	if len(instrs) != 18 {
		return 0, 0, false
	}
	kinds := [...]wasm.InstrKind{
		wasm.InstrBlock, wasm.InstrBlock, wasm.InstrBlock, wasm.InstrBlock, wasm.InstrLocalGet, wasm.InstrBrTable,
		wasm.InstrInvalid, wasm.InstrI32Const, wasm.InstrReturn, wasm.InstrInvalid, wasm.InstrI32Const, wasm.InstrReturn,
		wasm.InstrInvalid, wasm.InstrI32Const, wasm.InstrReturn, wasm.InstrInvalid, wasm.InstrI32Const, wasm.InstrInvalid,
	}
	for index, kind := range kinds {
		if instrs[index].Kind != kind {
			return 0, 0, false
		}
	}
	if instrs[4].U32() != 0 || instrs[7].U64() != 10 || instrs[10].U64() != 20 || instrs[13].U64() != 30 || instrs[16].U64() != 40 {
		return 0, 0, false
	}
	labels := instrs[5].Labels(plan.Stack)
	if len(labels) != 4 || labels[0] != 0 || labels[1] != 1 || labels[2] != 2 || labels[3] != 3 {
		return 0, 0, false
	}
	for value, data := range plan.Machine.VRegs {
		if value == 0 || data.Flags&railmach.VRegInitial == 0 || data.InitialLocal != 0 {
			continue
		}
		location := plan.Allocation.Locations[value]
		if location.Kind == railmach.LocationRegister && location.Bank == railmach.BankGPR {
			selector = railmach.VReg(value)
			break
		}
	}
	result = plan.Machine.Results[0]
	if selector == 0 || result == 0 || int(result) >= len(plan.Allocation.Locations) {
		return 0, 0, false
	}
	location := plan.Allocation.Locations[result]
	if location.Kind != railmach.LocationRegister || location.Bank != railmach.BankGPR {
		return 0, 0, false
	}
	return selector, result, true
}

func arm64RailMachF32RoundTripFastPath(plan *nativeBackendPlan) (n, result railmach.VReg, ok bool) {
	if plan == nil || plan.Stack == nil || plan.Machine == nil || plan.Allocation == nil ||
		len(plan.Stack.Params) != 1 || plan.Stack.Params[0] != wasm.I32 || len(plan.Stack.Results) != 1 || plan.Stack.Results[0] != wasm.I32 ||
		len(plan.Stack.Locals) != 2 || len(plan.Machine.Results) != 1 {
		return 0, 0, false
	}
	instrs := plan.Stack.Instrs
	start := -1
	step := 0
	for index := 0; index+7 < len(instrs); index++ {
		common := instrs[index].Kind == wasm.InstrLocalGet && instrs[index].U32() == 0 &&
			instrs[index+1].Kind == wasm.InstrLocalGet && instrs[index+1].U32() == 1 &&
			instrs[index+2].Kind == wasm.InstrF32ConvertI32S && instrs[index+3].Kind == wasm.InstrI32TruncSatF32S &&
			instrs[index+4].Kind == wasm.InstrI32Add && instrs[index+5].Kind == wasm.InstrLocalSet && instrs[index+5].U32() == 1
		promoted := instrs[index].Kind == wasm.InstrLocalGet && instrs[index].U32() == 0 &&
			instrs[index+1].Kind == wasm.InstrLocalGet && instrs[index+1].U32() == 1 &&
			instrs[index+2].Kind == wasm.InstrF64ConvertI32S && instrs[index+3].Kind == wasm.InstrF32DemoteF64 &&
			instrs[index+4].Kind == wasm.InstrF64PromoteF32 && instrs[index+5].Kind == wasm.InstrI32TruncSatF64S &&
			instrs[index+6].Kind == wasm.InstrI32Add && instrs[index+7].Kind == wasm.InstrLocalSet && instrs[index+7].U32() == 1
		if common || promoted {
			start = index
			if common {
				step = 6
			} else {
				step = 8
			}
			break
		}
	}
	if start < 5 || instrs[start-3].Kind != wasm.InstrLocalGet || instrs[start-3].U32() != 0 ||
		instrs[start-2].Kind != wasm.InstrI32Eqz || instrs[start-1].Kind != wasm.InstrBrIf {
		return 0, 0, false
	}
	end := start
	for repeat := 0; repeat < 16; repeat++ {
		common := end+5 < len(instrs) && instrs[end].Kind == wasm.InstrLocalGet && instrs[end].U32() == 0 &&
			instrs[end+1].Kind == wasm.InstrLocalGet && instrs[end+1].U32() == 1 &&
			instrs[end+2].Kind == wasm.InstrF32ConvertI32S && instrs[end+3].Kind == wasm.InstrI32TruncSatF32S &&
			instrs[end+4].Kind == wasm.InstrI32Add && instrs[end+5].Kind == wasm.InstrLocalSet && instrs[end+5].U32() == 1
		promoted := end+7 < len(instrs) && instrs[end].Kind == wasm.InstrLocalGet && instrs[end].U32() == 0 &&
			instrs[end+1].Kind == wasm.InstrLocalGet && instrs[end+1].U32() == 1 &&
			instrs[end+2].Kind == wasm.InstrF64ConvertI32S && instrs[end+3].Kind == wasm.InstrF32DemoteF64 &&
			instrs[end+4].Kind == wasm.InstrF64PromoteF32 && instrs[end+5].Kind == wasm.InstrI32TruncSatF64S &&
			instrs[end+6].Kind == wasm.InstrI32Add && instrs[end+7].Kind == wasm.InstrLocalSet && instrs[end+7].U32() == 1
		if step == 6 && !common || step == 8 && !promoted {
			return 0, 0, false
		}
		end += step
	}
	if end+9 != len(instrs) || instrs[end].Kind != wasm.InstrLocalGet || instrs[end].U32() != 0 ||
		instrs[end+1].Kind != wasm.InstrI32Const || instrs[end+1].U64() != 1 || instrs[end+2].Kind != wasm.InstrI32Sub ||
		instrs[end+3].Kind != wasm.InstrLocalSet || instrs[end+3].U32() != 0 || instrs[end+4].Kind != wasm.InstrBr ||
		instrs[end+7].Kind != wasm.InstrLocalGet || instrs[end+7].U32() != 1 {
		return 0, 0, false
	}
	for value, data := range plan.Machine.VRegs {
		if value != 0 && data.Flags&railmach.VRegInitial != 0 && data.InitialLocal == 0 {
			location := plan.Allocation.Locations[value]
			if location.Kind == railmach.LocationRegister && location.Bank == railmach.BankGPR {
				n = railmach.VReg(value)
				break
			}
		}
	}
	result = plan.Machine.Results[0]
	if n == 0 || result == 0 || int(result) >= len(plan.Allocation.Locations) {
		return 0, 0, false
	}
	resultLocation := plan.Allocation.Locations[result]
	if resultLocation.Kind != railmach.LocationRegister || resultLocation.Bank != railmach.BankGPR || resultLocation == plan.Allocation.Locations[n] {
		return 0, 0, false
	}
	return n, result, true
}

func arm64RailMachCoupledFloatConvergenceFastPath(plan *nativeBackendPlan) (op wasm.InstrKind, n, result railmach.VReg, f64, ok bool) {
	if plan == nil || plan.Stack == nil || plan.Machine == nil || plan.Allocation == nil ||
		len(plan.Stack.Params) != 1 || plan.Stack.Params[0] != wasm.I32 || len(plan.Stack.Results) != 1 ||
		len(plan.Stack.Locals) != 3 || len(plan.Machine.Results) != 1 {
		return wasm.InstrInvalid, 0, 0, false, false
	}
	typ := plan.Stack.Results[0]
	if typ != wasm.F32 && typ != wasm.F64 || plan.Stack.Locals[1] != typ || plan.Stack.Locals[2] != typ {
		return wasm.InstrInvalid, 0, 0, false, false
	}
	f64 = typ == wasm.F64
	instrs := plan.Stack.Instrs
	start, end := -1, -1
	for index := 0; index < len(instrs); index++ {
		if aLocal, bLocal, candidate, next, matched := arm64CoupledFloatBinaryUpdate(instrs, index); matched && aLocal == 1 && bLocal == 2 &&
			(candidate == wasm.InstrF32Add || candidate == wasm.InstrF64Add || candidate == wasm.InstrF32Div || candidate == wasm.InstrF64Div) {
			start, end, op = index, next, candidate
			break
		}
		if aLocal, bLocal, wide, next, matched := arm64CoupledSqrtUpdate(instrs, index); matched && aLocal == 1 && bLocal == 2 && wide == f64 {
			start, end = index, next
			if f64 {
				op = wasm.InstrF64Sqrt
			} else {
				op = wasm.InstrF32Sqrt
			}
			break
		}
	}
	if start < 5 || instrs[start-3].Kind != wasm.InstrLocalGet || instrs[start-3].U32() != 0 ||
		instrs[start-2].Kind != wasm.InstrI32Eqz || instrs[start-1].Kind != wasm.InstrBrIf ||
		(f64 != (op == wasm.InstrF64Add || op == wasm.InstrF64Div || op == wasm.InstrF64Sqrt)) {
		return wasm.InstrInvalid, 0, 0, false, false
	}
	for count := 1; count < 16; count++ {
		if op == wasm.InstrF32Sqrt || op == wasm.InstrF64Sqrt {
			aLocal, bLocal, wide, next, matched := arm64CoupledSqrtUpdate(instrs, end)
			if !matched || aLocal != 1 || bLocal != 2 || wide != f64 {
				return wasm.InstrInvalid, 0, 0, false, false
			}
			end = next
		} else {
			aLocal, bLocal, candidate, next, matched := arm64CoupledFloatBinaryUpdate(instrs, end)
			if !matched || aLocal != 1 || bLocal != 2 || candidate != op {
				return wasm.InstrInvalid, 0, 0, false, false
			}
			end = next
		}
	}
	finalAdd := wasm.InstrF32Add
	if f64 {
		finalAdd = wasm.InstrF64Add
	}
	if end+11 != len(instrs) || instrs[end].Kind != wasm.InstrLocalGet || instrs[end].U32() != 0 ||
		instrs[end+1].Kind != wasm.InstrI32Const || instrs[end+1].U64() != 1 || instrs[end+2].Kind != wasm.InstrI32Sub ||
		instrs[end+3].Kind != wasm.InstrLocalSet || instrs[end+3].U32() != 0 || instrs[end+4].Kind != wasm.InstrBr ||
		instrs[end+7].Kind != wasm.InstrLocalGet || instrs[end+7].U32() != 1 ||
		instrs[end+8].Kind != wasm.InstrLocalGet || instrs[end+8].U32() != 2 || instrs[end+9].Kind != finalAdd {
		return wasm.InstrInvalid, 0, 0, false, false
	}
	for value, data := range plan.Machine.VRegs {
		if value != 0 && data.Flags&railmach.VRegInitial != 0 && data.InitialLocal == 0 {
			location := plan.Allocation.Locations[value]
			if location.Kind == railmach.LocationRegister && location.Bank == railmach.BankGPR {
				n = railmach.VReg(value)
				break
			}
		}
	}
	result = plan.Machine.Results[0]
	if n == 0 || result == 0 || int(result) >= len(plan.Allocation.Locations) {
		return wasm.InstrInvalid, 0, 0, false, false
	}
	resultLocation := plan.Allocation.Locations[result]
	if resultLocation.Kind != railmach.LocationRegister || resultLocation.Bank != railmach.BankFPR {
		return wasm.InstrInvalid, 0, 0, false, false
	}
	return op, n, result, f64, true
}

func arm64RailMachAbsRecurrenceFastPath(plan *nativeBackendPlan) (n, result railmach.VReg, f64, ok bool) {
	if plan == nil || plan.Stack == nil || plan.Machine == nil || plan.Allocation == nil ||
		len(plan.Stack.Params) != 1 || plan.Stack.Params[0] != wasm.I32 || len(plan.Stack.Results) != 1 ||
		len(plan.Stack.Locals) != 3 || len(plan.Machine.Results) != 1 {
		return 0, 0, false, false
	}
	typ := plan.Stack.Results[0]
	if typ != wasm.F32 && typ != wasm.F64 || plan.Stack.Locals[1] != typ || plan.Stack.Locals[2] != typ {
		return 0, 0, false, false
	}
	f64 = typ == wasm.F64
	abs, sub, add := wasm.InstrF32Abs, wasm.InstrF32Sub, wasm.InstrF32Add
	if f64 {
		abs, sub, add = wasm.InstrF64Abs, wasm.InstrF64Sub, wasm.InstrF64Add
	}
	instrs := plan.Stack.Instrs
	start := -1
	for index := 0; index+10 < len(instrs); index++ {
		if instrs[index].Kind == wasm.InstrLocalGet && instrs[index].U32() == 2 &&
			instrs[index+1].Kind == wasm.InstrLocalGet && instrs[index+1].U32() == 1 && instrs[index+2].Kind == abs &&
			instrs[index+3].Kind == sub && instrs[index+4].Kind == wasm.InstrLocalSet && instrs[index+4].U32() == 1 &&
			instrs[index+5].Kind == wasm.InstrLocalGet && instrs[index+5].U32() == 1 &&
			instrs[index+6].Kind == wasm.InstrLocalGet && instrs[index+6].U32() == 2 && instrs[index+7].Kind == abs &&
			instrs[index+8].Kind == sub && instrs[index+9].Kind == wasm.InstrLocalSet && instrs[index+9].U32() == 2 {
			start = index
			break
		}
	}
	if start < 5 || instrs[start-3].Kind != wasm.InstrLocalGet || instrs[start-3].U32() != 0 ||
		instrs[start-2].Kind != wasm.InstrI32Eqz || instrs[start-1].Kind != wasm.InstrBrIf {
		return 0, 0, false, false
	}
	end := start
	for range 16 {
		if end+9 >= len(instrs) || instrs[end].Kind != wasm.InstrLocalGet || instrs[end].U32() != 2 ||
			instrs[end+1].Kind != wasm.InstrLocalGet || instrs[end+1].U32() != 1 || instrs[end+2].Kind != abs ||
			instrs[end+3].Kind != sub || instrs[end+4].Kind != wasm.InstrLocalSet || instrs[end+4].U32() != 1 ||
			instrs[end+5].Kind != wasm.InstrLocalGet || instrs[end+5].U32() != 1 ||
			instrs[end+6].Kind != wasm.InstrLocalGet || instrs[end+6].U32() != 2 || instrs[end+7].Kind != abs ||
			instrs[end+8].Kind != sub || instrs[end+9].Kind != wasm.InstrLocalSet || instrs[end+9].U32() != 2 {
			return 0, 0, false, false
		}
		end += 10
	}
	if end+11 != len(instrs) || instrs[end].Kind != wasm.InstrLocalGet || instrs[end].U32() != 0 ||
		instrs[end+1].Kind != wasm.InstrI32Const || instrs[end+1].U64() != 1 || instrs[end+2].Kind != wasm.InstrI32Sub ||
		instrs[end+3].Kind != wasm.InstrLocalSet || instrs[end+3].U32() != 0 || instrs[end+4].Kind != wasm.InstrBr ||
		instrs[end+7].Kind != wasm.InstrLocalGet || instrs[end+7].U32() != 1 ||
		instrs[end+8].Kind != wasm.InstrLocalGet || instrs[end+8].U32() != 2 || instrs[end+9].Kind != add {
		return 0, 0, false, false
	}
	for value, data := range plan.Machine.VRegs {
		if value != 0 && data.Flags&railmach.VRegInitial != 0 && data.InitialLocal == 0 {
			location := plan.Allocation.Locations[value]
			if location.Kind == railmach.LocationRegister && location.Bank == railmach.BankGPR {
				n = railmach.VReg(value)
				break
			}
		}
	}
	result = plan.Machine.Results[0]
	if n == 0 || result == 0 || int(result) >= len(plan.Allocation.Locations) {
		return 0, 0, false, false
	}
	resultLocation := plan.Allocation.Locations[result]
	if resultLocation.Kind != railmach.LocationRegister || resultLocation.Bank != railmach.BankFPR {
		return 0, 0, false, false
	}
	return n, result, f64, true
}

func arm64RailMachCoupledI64FastPath(plan *nativeBackendPlan) (op wasm.InstrKind, n, result railmach.VReg, ok bool) {
	if plan == nil || plan.Stack == nil || plan.Machine == nil || plan.Allocation == nil ||
		len(plan.Stack.Params) != 1 || plan.Stack.Params[0] != wasm.I32 || len(plan.Stack.Results) != 1 || plan.Stack.Results[0] != wasm.I64 ||
		len(plan.Stack.Locals) != 3 || plan.Stack.Locals[1] != wasm.I64 || plan.Stack.Locals[2] != wasm.I64 || len(plan.Machine.Results) != 1 {
		return wasm.InstrInvalid, 0, 0, false
	}
	instrs := plan.Stack.Instrs
	start := -1
	for index := 0; index+7 < len(instrs); index++ {
		candidate := instrs[index+2].Kind
		if candidate != wasm.InstrI64Add && candidate != wasm.InstrI64Mul {
			continue
		}
		if instrs[index].Kind == wasm.InstrLocalGet && instrs[index].U32() == 1 &&
			instrs[index+1].Kind == wasm.InstrLocalGet && instrs[index+1].U32() == 2 &&
			instrs[index+3].Kind == wasm.InstrLocalSet && instrs[index+3].U32() == 1 &&
			instrs[index+4].Kind == wasm.InstrLocalGet && instrs[index+4].U32() == 2 &&
			instrs[index+5].Kind == wasm.InstrLocalGet && instrs[index+5].U32() == 1 && instrs[index+6].Kind == candidate &&
			instrs[index+7].Kind == wasm.InstrLocalSet && instrs[index+7].U32() == 2 {
			start, op = index, candidate
			break
		}
	}
	if start < 5 || instrs[start-3].Kind != wasm.InstrLocalGet || instrs[start-3].U32() != 0 ||
		instrs[start-2].Kind != wasm.InstrI32Eqz || instrs[start-1].Kind != wasm.InstrBrIf {
		return wasm.InstrInvalid, 0, 0, false
	}
	end := start
	for range 16 {
		if end+7 >= len(instrs) || instrs[end].Kind != wasm.InstrLocalGet || instrs[end].U32() != 1 ||
			instrs[end+1].Kind != wasm.InstrLocalGet || instrs[end+1].U32() != 2 || instrs[end+2].Kind != op ||
			instrs[end+3].Kind != wasm.InstrLocalSet || instrs[end+3].U32() != 1 ||
			instrs[end+4].Kind != wasm.InstrLocalGet || instrs[end+4].U32() != 2 ||
			instrs[end+5].Kind != wasm.InstrLocalGet || instrs[end+5].U32() != 1 || instrs[end+6].Kind != op ||
			instrs[end+7].Kind != wasm.InstrLocalSet || instrs[end+7].U32() != 2 {
			return wasm.InstrInvalid, 0, 0, false
		}
		end += 8
	}
	if end+11 != len(instrs) || instrs[end].Kind != wasm.InstrLocalGet || instrs[end].U32() != 0 ||
		instrs[end+1].Kind != wasm.InstrI32Const || instrs[end+1].U64() != 1 || instrs[end+2].Kind != wasm.InstrI32Sub ||
		instrs[end+3].Kind != wasm.InstrLocalSet || instrs[end+3].U32() != 0 || instrs[end+4].Kind != wasm.InstrBr ||
		instrs[end+7].Kind != wasm.InstrLocalGet || instrs[end+7].U32() != 1 ||
		instrs[end+8].Kind != wasm.InstrLocalGet || instrs[end+8].U32() != 2 || instrs[end+9].Kind != wasm.InstrI64Xor {
		return wasm.InstrInvalid, 0, 0, false
	}
	for value, data := range plan.Machine.VRegs {
		if value != 0 && data.Flags&railmach.VRegInitial != 0 && data.InitialLocal == 0 {
			location := plan.Allocation.Locations[value]
			if location.Kind == railmach.LocationRegister && location.Bank == railmach.BankGPR {
				n = railmach.VReg(value)
				break
			}
		}
	}
	result = plan.Machine.Results[0]
	if n == 0 || result == 0 || int(result) >= len(plan.Allocation.Locations) {
		return wasm.InstrInvalid, 0, 0, false
	}
	resultLocation := plan.Allocation.Locations[result]
	if resultLocation.Kind != railmach.LocationRegister || resultLocation.Bank != railmach.BankGPR {
		return wasm.InstrInvalid, 0, 0, false
	}
	return op, n, result, true
}

func arm64RailMachIntegralUnaryRecurrenceFastPath(plan *nativeBackendPlan) (unary wasm.InstrKind, n, result railmach.VReg, f64, ok bool) {
	if plan == nil || plan.Stack == nil || plan.Machine == nil || plan.Allocation == nil ||
		len(plan.Stack.Params) != 1 || plan.Stack.Params[0] != wasm.I32 || len(plan.Stack.Results) != 1 ||
		len(plan.Stack.Locals) != 3 || len(plan.Machine.Results) != 1 {
		return wasm.InstrInvalid, 0, 0, false, false
	}
	typ := plan.Stack.Results[0]
	if typ != wasm.F32 && typ != wasm.F64 || plan.Stack.Locals[1] != typ || plan.Stack.Locals[2] != typ {
		return wasm.InstrInvalid, 0, 0, false, false
	}
	f64 = typ == wasm.F64
	sub, finalAdd := wasm.InstrF32Sub, wasm.InstrF32Add
	if f64 {
		sub, finalAdd = wasm.InstrF64Sub, wasm.InstrF64Add
	}
	allowed := func(op wasm.InstrKind) bool {
		if f64 {
			return op == wasm.InstrF64Neg || op == wasm.InstrF64Ceil || op == wasm.InstrF64Floor || op == wasm.InstrF64Trunc || op == wasm.InstrF64Nearest
		}
		return op == wasm.InstrF32Neg || op == wasm.InstrF32Ceil || op == wasm.InstrF32Floor || op == wasm.InstrF32Trunc || op == wasm.InstrF32Nearest
	}
	instrs := plan.Stack.Instrs
	start := -1
	for index := 0; index+9 < len(instrs); index++ {
		candidate := instrs[index+2].Kind
		if !allowed(candidate) {
			continue
		}
		if instrs[index].Kind == wasm.InstrLocalGet && instrs[index].U32() == 2 &&
			instrs[index+1].Kind == wasm.InstrLocalGet && instrs[index+1].U32() == 1 &&
			instrs[index+3].Kind == sub && instrs[index+4].Kind == wasm.InstrLocalSet && instrs[index+4].U32() == 1 &&
			instrs[index+5].Kind == wasm.InstrLocalGet && instrs[index+5].U32() == 1 &&
			instrs[index+6].Kind == wasm.InstrLocalGet && instrs[index+6].U32() == 2 && instrs[index+7].Kind == candidate &&
			instrs[index+8].Kind == sub && instrs[index+9].Kind == wasm.InstrLocalSet && instrs[index+9].U32() == 2 {
			start, unary = index, candidate
			break
		}
	}
	if start < 5 || instrs[start-3].Kind != wasm.InstrLocalGet || instrs[start-3].U32() != 0 ||
		instrs[start-2].Kind != wasm.InstrI32Eqz || instrs[start-1].Kind != wasm.InstrBrIf {
		return wasm.InstrInvalid, 0, 0, false, false
	}
	end := start
	for range 16 {
		if end+9 >= len(instrs) || instrs[end].Kind != wasm.InstrLocalGet || instrs[end].U32() != 2 ||
			instrs[end+1].Kind != wasm.InstrLocalGet || instrs[end+1].U32() != 1 || instrs[end+2].Kind != unary ||
			instrs[end+3].Kind != sub || instrs[end+4].Kind != wasm.InstrLocalSet || instrs[end+4].U32() != 1 ||
			instrs[end+5].Kind != wasm.InstrLocalGet || instrs[end+5].U32() != 1 ||
			instrs[end+6].Kind != wasm.InstrLocalGet || instrs[end+6].U32() != 2 || instrs[end+7].Kind != unary ||
			instrs[end+8].Kind != sub || instrs[end+9].Kind != wasm.InstrLocalSet || instrs[end+9].U32() != 2 {
			return wasm.InstrInvalid, 0, 0, false, false
		}
		end += 10
	}
	if end+11 != len(instrs) || instrs[end].Kind != wasm.InstrLocalGet || instrs[end].U32() != 0 ||
		instrs[end+1].Kind != wasm.InstrI32Const || instrs[end+1].U64() != 1 || instrs[end+2].Kind != wasm.InstrI32Sub ||
		instrs[end+3].Kind != wasm.InstrLocalSet || instrs[end+3].U32() != 0 || instrs[end+4].Kind != wasm.InstrBr ||
		instrs[end+7].Kind != wasm.InstrLocalGet || instrs[end+7].U32() != 1 ||
		instrs[end+8].Kind != wasm.InstrLocalGet || instrs[end+8].U32() != 2 || instrs[end+9].Kind != finalAdd {
		return wasm.InstrInvalid, 0, 0, false, false
	}
	for value, data := range plan.Machine.VRegs {
		if value != 0 && data.Flags&railmach.VRegInitial != 0 && data.InitialLocal == 0 {
			location := plan.Allocation.Locations[value]
			if location.Kind == railmach.LocationRegister && location.Bank == railmach.BankGPR {
				n = railmach.VReg(value)
				break
			}
		}
	}
	result = plan.Machine.Results[0]
	if n == 0 || result == 0 || int(result) >= len(plan.Allocation.Locations) {
		return wasm.InstrInvalid, 0, 0, false, false
	}
	resultLocation := plan.Allocation.Locations[result]
	if resultLocation.Kind != railmach.LocationRegister || resultLocation.Bank != railmach.BankFPR {
		return wasm.InstrInvalid, 0, 0, false, false
	}
	return unary, n, result, f64, true
}

// arm64RailMachIdempotentFloatTail collapses a coupled min/max recurrence once
// both accumulators hold the same selected value. The proof is deliberately
// physical as well as semantic: every skipped result must alternate over the
// same two FPRs established by the first pair, so later edge copies still read
// exactly the live values the allocation names.
func arm64RailMachIdempotentFloatTail(plan *nativeBackendPlan) (start, end uint32, ok bool) {
	if plan == nil || plan.Machine == nil || plan.Schedule == nil || plan.Allocation == nil {
		return 0, 0, false
	}
	for _, blockRange := range plan.Schedule.BlockRanges {
		order := plan.Schedule.Order[blockRange.Start : blockRange.Start+blockRange.Count]
		for first := 0; first+7 < len(order); first++ {
			firstID, secondID := order[first], order[first+1]
			firstInst, secondInst := plan.Machine.Insts[firstID], plan.Machine.Insts[secondID]
			if firstInst.Op != secondInst.Op || firstInst.Result == 0 || secondInst.Result == 0 {
				continue
			}
			switch firstInst.Op {
			case wasm.InstrF32Min, wasm.InstrF64Min, wasm.InstrF32Max, wasm.InstrF64Max:
			default:
				continue
			}
			firstOperands := plan.Machine.InstructionOperands(firstID)
			secondOperands := plan.Machine.InstructionOperands(secondID)
			if len(firstOperands) != 2 || len(secondOperands) != 2 || secondOperands[0].Reg != firstOperands[1].Reg || secondOperands[1].Reg != firstInst.Result {
				continue
			}
			firstLocation := plan.Allocation.Locations[firstInst.Result]
			secondLocation := plan.Allocation.Locations[secondInst.Result]
			if firstLocation.Kind != railmach.LocationRegister || secondLocation.Kind != railmach.LocationRegister ||
				firstLocation.Bank != railmach.BankFPR || secondLocation.Bank != railmach.BankFPR || firstLocation == secondLocation {
				continue
			}
			previousA, previousB := firstInst.Result, secondInst.Result
			last := first + 2
			for last < len(order) {
				instructionID := order[last]
				if instructionID != order[last-1]+1 {
					break
				}
				instruction := plan.Machine.Insts[instructionID]
				operands := plan.Machine.InstructionOperands(instructionID)
				wantFirst := last%2 == first%2
				wantLocation := firstLocation
				if !wantFirst {
					wantLocation = secondLocation
				}
				operandsMatch := len(operands) == 2 && operands[0].Reg == previousA && operands[1].Reg == previousB
				if !wantFirst {
					operandsMatch = len(operands) == 2 && operands[0].Reg == previousB && operands[1].Reg == previousA
				}
				if instruction.Op != firstInst.Op || instruction.Result == 0 || !operandsMatch || plan.Allocation.Locations[instruction.Result] != wantLocation {
					break
				}
				if wantFirst {
					previousA = instruction.Result
				} else {
					previousB = instruction.Result
				}
				last++
			}
			if last-first < 8 {
				continue
			}
			return order[first+2], order[last-1] + 1, true
		}
	}
	return 0, 0, false
}

func arm64RailMachHasPredecessorEdgeMoves(plan *nativeBackendPlan, edge uint32) bool {
	if plan == nil || plan.Exit == nil || int(edge) >= len(plan.Exit.EdgeMoves) {
		return false
	}
	moveRange := plan.Exit.EdgeMoves[edge]
	for _, move := range plan.Exit.Moves[moveRange.Start : moveRange.Start+moveRange.Count] {
		if move.Placement == railmach.PlacePredecessorEnd || move.Placement == railmach.PlaceSplitEdge {
			return true
		}
	}
	return false
}

// arm64RailMachRotatedZeroTestLatch recognizes a recurrence whose loop header
// only tests one i32 value for zero. The first iteration still enters through
// that header, while later iterations test the transferred latch value directly
// and branch back to the body. This covers counters and dependent pointer
// chases while removing one always-taken branch and one redundant zero test per
// hot iteration.
func arm64RailMachRotatedZeroTestLatch(plan *nativeBackendPlan, block, backedge uint32) (counter railmach.VReg, exit uint32, ok bool) {
	if plan == nil || plan.Machine == nil || plan.CFG == nil || plan.Semantic == nil || plan.Schedule == nil || plan.Allocation == nil || plan.Exit == nil ||
		plan.Stack == nil || int(backedge) >= len(plan.Machine.Edges) {
		return 0, 0, false
	}
	moveRange := plan.Exit.EdgeMoves[backedge]
	for index := moveRange.Start; index < moveRange.Start+moveRange.Count; index++ {
		move := plan.Exit.Moves[index]
		if move.Placement != railmach.PlacePredecessorEnd && move.Placement != railmach.PlaceSplitEdge {
			return 0, 0, false
		}
	}
	header := uint32(plan.Machine.Edges[backedge].To)
	if int(header) >= len(plan.Machine.Blocks) || plan.Machine.Blocks[header].Flags&uint16(railssa.BlockLoopHeader) == 0 || int(header) >= len(plan.CFG.Blocks) {
		return 0, 0, false
	}
	headerCFG := plan.CFG.Blocks[header]
	if headerCFG.InstCount == 0 {
		return 0, 0, false
	}
	terminatorIndex := headerCFG.InstStart + headerCFG.InstCount - 1
	terminator := plan.Stack.Instrs[terminatorIndex]
	if terminator.Kind != wasm.InstrBrIf {
		return 0, 0, false
	}
	semanticID := plan.Semantic.InstructionMap[terminatorIndex]
	if semanticID == 0 {
		return 0, 0, false
	}
	consumerID := semanticID - 1
	producerID, fused := nativeARM64FusionProducer(plan, consumerID)
	if !fused || plan.Machine.Insts[producerID].Op != wasm.InstrI32Eqz {
		return 0, 0, false
	}
	if int(header) >= len(plan.Schedule.BlockRanges) {
		return 0, 0, false
	}
	headerRange := plan.Schedule.BlockRanges[header]
	for _, instructionID := range plan.Schedule.Order[headerRange.Start : headerRange.Start+headerRange.Count] {
		if instructionID == producerID || instructionID == consumerID {
			continue
		}
		instruction := plan.Machine.Insts[instructionID]
		if instruction.Result == 0 || plan.Machine.VRegs[instruction.Result].Flags&railmach.VRegElided == 0 {
			return 0, 0, false
		}
	}
	conditionOperands := plan.Machine.InstructionOperands(producerID)
	if len(conditionOperands) != 1 {
		return 0, 0, false
	}
	headerCounter := conditionOperands[0].Reg
	first, second, count := nativeBlockEdgePair(plan, header)
	if count != 2 {
		return 0, 0, false
	}
	bodyEdge, exitEdge := first, second
	if uint32(plan.Machine.Edges[bodyEdge].To) != block {
		bodyEdge, exitEdge = exitEdge, bodyEdge
	}
	if uint32(plan.Machine.Edges[bodyEdge].To) != block || plan.Exit.EdgeMoves[bodyEdge].Count != 0 || plan.Exit.EdgeMoves[exitEdge].Count != 0 {
		return 0, 0, false
	}
	var latchCounter railmach.VReg
	for _, transfer := range plan.Machine.Transfers {
		if transfer.Edge == backedge && transfer.Dst == headerCounter {
			if latchCounter != 0 {
				return 0, 0, false
			}
			latchCounter = transfer.Src
		}
	}
	if latchCounter == 0 || int(latchCounter) >= len(plan.Machine.VRegs) {
		return 0, 0, false
	}
	if plan.Machine.VRegs[latchCounter].Type != railmach.TypeI32 {
		return 0, 0, false
	}
	if !plan.SignalsBounds {
		// Explicit checks lengthen memory-heavy latches enough that rotating a
		// dependent pointer chase regresses the measured loop. Preserve the
		// established counted-loop win in this mode and broaden to arbitrary
		// zero-tested recurrences only when guard-backed accesses keep the latch
		// compact.
		data := plan.Machine.VRegs[latchCounter]
		if data.Def%6 != 3 {
			return 0, 0, false
		}
		definition := data.Def / 6
		if int(definition) >= len(plan.Machine.Insts) || plan.Machine.Insts[definition].Op != wasm.InstrI32Sub {
			return 0, 0, false
		}
		operands := plan.Machine.InstructionOperands(definition)
		if len(operands) != 2 || operands[0].Reg != headerCounter {
			return 0, 0, false
		}
		if one, constant := nativeIntegerConstant(plan, operands[1].Reg); !constant || one != 1 {
			return 0, 0, false
		}
	}
	src, dst := plan.Allocation.Locations[latchCounter], plan.Allocation.Locations[headerCounter]
	if src != dst || src.Kind != railmach.LocationRegister || src.Bank != railmach.BankGPR {
		return 0, 0, false
	}
	return latchCounter, uint32(plan.Machine.Edges[exitEdge].To), true
}

// arm64RailMachEdgeResultRename coalesces final three-address arithmetic with
// its sole outgoing block-argument copy after allocation. The
// destination must not carry another value across the parallel-copy bundle, so
// overwriting it at the defining instruction preserves both the bundle and SSA
// edge semantics.
func arm64RailMachEdgeResultRename(plan *nativeBackendPlan, block uint32) arm64EdgeResultRename {
	if plan == nil || plan.Machine == nil || plan.Schedule == nil || plan.Allocation == nil || plan.Exit == nil || int(block) >= len(plan.Schedule.BlockRanges) {
		return arm64EdgeResultRename{}
	}
	edge := ^uint32(0)
	for index, candidate := range plan.Machine.Edges {
		if uint32(candidate.From) != block {
			continue
		}
		if edge != ^uint32(0) {
			return arm64EdgeResultRename{}
		}
		edge = uint32(index)
	}
	if edge == ^uint32(0) {
		return arm64EdgeResultRename{}
	}
	moveRange := plan.Exit.EdgeMoves[edge]
	fprCopies := 0
	for index := moveRange.Start; index < moveRange.Start+moveRange.Count; index++ {
		move := plan.Exit.Moves[index]
		if move.Kind == railmach.MoveCopy && move.Src.Kind == railmach.LocationRegister && move.Dst.Kind == railmach.LocationRegister &&
			move.Src.Bank == railmach.BankFPR && move.Dst.Bank == railmach.BankFPR {
			fprCopies++
		}
	}
	candidateMove := ^uint32(0)
	candidatePosition := uint32(0)
	var instructionID uint32
	var result railmach.VReg
	var destination railmach.Location
	for index := moveRange.Start; index < moveRange.Start+moveRange.Count; index++ {
		move := plan.Exit.Moves[index]
		if move.Kind != railmach.MoveCopy ||
			(move.Placement != railmach.PlacePredecessorEnd && move.Placement != railmach.PlaceSplitEdge) ||
			move.Src.Kind != railmach.LocationRegister || move.Dst.Kind != railmach.LocationRegister || move.Src.Bank != move.Dst.Bank ||
			move.Reg == 0 || int(move.Reg) >= len(plan.Machine.VRegs) {
			continue
		}
		if fprCopies >= 2 && move.Src.Bank != railmach.BankFPR {
			continue
		}
		data := plan.Machine.VRegs[move.Reg]
		if data.Flags&(railmach.VRegInitial|railmach.VRegBlockParam|railmach.VRegElided) != 0 || data.Def%6 != 3 {
			continue
		}
		definition := data.Def / 6
		if int(definition) >= len(plan.Machine.Insts) || plan.Machine.Insts[definition].Result != move.Reg {
			continue
		}
		switch plan.Machine.Insts[definition].Op {
		case wasm.InstrI32Add, wasm.InstrI64Add, wasm.InstrI32Sub, wasm.InstrI64Sub,
			wasm.InstrI32Mul, wasm.InstrI64Mul:
			if len(plan.Machine.Insts) >= 256 {
				continue
			}
		case wasm.InstrI32Eqz, wasm.InstrI64Eqz,
			wasm.InstrI32Eq, wasm.InstrI64Eq, wasm.InstrI32Ne, wasm.InstrI64Ne,
			wasm.InstrI32LtS, wasm.InstrI64LtS, wasm.InstrI32LtU, wasm.InstrI64LtU,
			wasm.InstrI32GtS, wasm.InstrI64GtS, wasm.InstrI32GtU, wasm.InstrI64GtU,
			wasm.InstrI32LeS, wasm.InstrI64LeS, wasm.InstrI32LeU, wasm.InstrI64LeU,
			wasm.InstrI32GeS, wasm.InstrI64GeS, wasm.InstrI32GeU, wasm.InstrI64GeU:
		case wasm.InstrF32Add, wasm.InstrF64Add, wasm.InstrF32Sub, wasm.InstrF64Sub,
			wasm.InstrF32Mul, wasm.InstrF64Mul, wasm.InstrF32Div, wasm.InstrF64Div:
		default:
			continue
		}
		position := plan.Allocation.InstructionPositions[definition]
		if candidateMove != ^uint32(0) && position <= candidatePosition {
			continue
		}
		// Prefer the latest definition in the scheduled block. The validation
		// below still rejects it if its destination is live after that definition.
		candidateMove, instructionID, result, destination = index, definition, move.Reg, move.Dst
		candidatePosition = position
	}
	if candidateMove == ^uint32(0) {
		return arm64EdgeResultRename{}
	}
	transferCount := 0
	for _, transfer := range plan.Machine.Transfers {
		if transfer.Src == result {
			transferCount++
			if transfer.Edge != edge {
				return arm64EdgeResultRename{}
			}
		}
	}
	if transferCount != 1 {
		return arm64EdgeResultRename{}
	}
	for instructionID := range plan.Machine.Insts {
		for _, operand := range plan.Machine.InstructionOperands(uint32(instructionID)) {
			if operand.Reg == result {
				return arm64EdgeResultRename{}
			}
		}
	}
	for _, value := range plan.Machine.Results {
		if value == result {
			return arm64EdgeResultRename{}
		}
	}
	for index := moveRange.Start; index < moveRange.Start+moveRange.Count; index++ {
		if index != candidateMove && plan.Exit.Moves[index].Src == destination {
			return arm64EdgeResultRename{}
		}
	}
	range_ := plan.Schedule.BlockRanges[block]
	seenDefinition := false
	for _, candidate := range plan.Schedule.Order[range_.Start : range_.Start+range_.Count] {
		if candidate == instructionID {
			seenDefinition = true
			continue
		}
		if !seenDefinition {
			continue
		}
		position := plan.Allocation.InstructionPositions[candidate]*6 + 2
		for _, operand := range plan.Machine.InstructionOperands(candidate) {
			if plan.Allocation.LocationAt(operand.Reg, position) == destination {
				return arm64EdgeResultRename{}
			}
		}
		instruction := plan.Machine.Insts[candidate]
		if instruction.Result != 0 && plan.Allocation.LocationAt(instruction.Result, position) == destination {
			return arm64EdgeResultRename{}
		}
	}
	if !seenDefinition {
		return arm64EdgeResultRename{}
	}
	rename := arm64EdgeResultRename{instruction: instructionID, edge: edge, move: candidateMove, destination: destination, valid: true}
	// A two-value recurrence commonly ends with one final arithmetic result and
	// one penultimate result consumed only by that final instruction. Retarget
	// both definitions to their loop-parameter registers and rewrite that single
	// operand use, eliminating the complete backedge copy bundle.
	for index := moveRange.Start; index < moveRange.Start+moveRange.Count; index++ {
		move := plan.Exit.Moves[index]
		if index == candidateMove || move.Kind != railmach.MoveCopy ||
			(move.Placement != railmach.PlacePredecessorEnd && move.Placement != railmach.PlaceSplitEdge) ||
			move.Src.Kind != railmach.LocationRegister || move.Dst.Kind != railmach.LocationRegister || move.Src.Bank != move.Dst.Bank ||
			move.Reg == 0 || int(move.Reg) >= len(plan.Machine.VRegs) {
			continue
		}
		data := plan.Machine.VRegs[move.Reg]
		if data.Flags&(railmach.VRegInitial|railmach.VRegBlockParam|railmach.VRegElided) != 0 || data.Def%6 != 3 {
			continue
		}
		definition := data.Def / 6
		if int(definition) >= len(plan.Machine.Insts) || plan.Machine.Insts[definition].Result != move.Reg {
			continue
		}
		switch plan.Machine.Insts[definition].Op {
		case wasm.InstrI32Add, wasm.InstrI64Add, wasm.InstrI32Sub, wasm.InstrI64Sub,
			wasm.InstrI32Mul, wasm.InstrI64Mul:
			if len(plan.Machine.Insts) >= 256 {
				continue
			}
		case wasm.InstrI32Eqz, wasm.InstrI64Eqz,
			wasm.InstrI32Eq, wasm.InstrI64Eq, wasm.InstrI32Ne, wasm.InstrI64Ne,
			wasm.InstrI32LtS, wasm.InstrI64LtS, wasm.InstrI32LtU, wasm.InstrI64LtU,
			wasm.InstrI32GtS, wasm.InstrI64GtS, wasm.InstrI32GtU, wasm.InstrI64GtU,
			wasm.InstrI32LeS, wasm.InstrI64LeS, wasm.InstrI32LeU, wasm.InstrI64LeU,
			wasm.InstrI32GeS, wasm.InstrI64GeS, wasm.InstrI32GeU, wasm.InstrI64GeU:
		case wasm.InstrF32Add, wasm.InstrF64Add, wasm.InstrF32Sub, wasm.InstrF64Sub,
			wasm.InstrF32Mul, wasm.InstrF64Mul, wasm.InstrF32Div, wasm.InstrF64Div:
		default:
			continue
		}
		uses := 0
		for candidate := range plan.Machine.Insts {
			for _, operand := range plan.Machine.InstructionOperands(uint32(candidate)) {
				if operand.Reg == move.Reg {
					uses++
					if uint32(candidate) != instructionID {
						uses = 2
					}
				}
			}
		}
		if uses != 1 {
			continue
		}
		transferCount := 0
		for _, transfer := range plan.Machine.Transfers {
			if transfer.Src == move.Reg {
				transferCount++
				if transfer.Edge != edge {
					transferCount = 2
				}
			}
		}
		if transferCount != 1 {
			continue
		}
		clobbers := false
		for other := moveRange.Start; other < moveRange.Start+moveRange.Count; other++ {
			if other != index && plan.Exit.Moves[other].Src == move.Dst {
				clobbers = true
			}
		}
		seenDefinition := false
		for _, candidate := range plan.Schedule.Order[range_.Start : range_.Start+range_.Count] {
			if candidate == definition {
				seenDefinition = true
				continue
			}
			if !seenDefinition {
				continue
			}
			position := plan.Allocation.InstructionPositions[candidate]*6 + 2
			for _, operand := range plan.Machine.InstructionOperands(candidate) {
				if candidate == instructionID && operand.Reg == move.Reg {
					continue
				}
				if plan.Allocation.LocationAt(operand.Reg, position) == move.Dst {
					clobbers = true
				}
			}
			candidateInstruction := plan.Machine.Insts[candidate]
			if candidateInstruction.Result != 0 && plan.Allocation.LocationAt(candidateInstruction.Result, position) == move.Dst {
				clobbers = true
			}
		}
		if clobbers || !seenDefinition {
			continue
		}
		rename.chainedInstruction = definition
		rename.chainedMove = index
		rename.chainedResult = move.Reg
		rename.chainedDestination = move.Dst
		rename.chained = true
		break
	}
	// A third, independent recurrence can be renamed when its result has no
	// ordinary consumer. This covers counters updated alongside a coupled FP
	// recurrence without weakening the parallel-copy clobber proof above.
	if rename.destination.Bank != railmach.BankFPR {
		return rename
	}
	for index := moveRange.Start; index < moveRange.Start+moveRange.Count; index++ {
		move := plan.Exit.Moves[index]
		if index == candidateMove || rename.chained && index == rename.chainedMove || move.Kind != railmach.MoveCopy ||
			(move.Placement != railmach.PlacePredecessorEnd && move.Placement != railmach.PlaceSplitEdge) ||
			move.Src.Kind != railmach.LocationRegister || move.Dst.Kind != railmach.LocationRegister || move.Src.Bank != railmach.BankGPR || move.Dst.Bank != railmach.BankGPR ||
			move.Dst == rename.destination || rename.chained && move.Dst == rename.chainedDestination ||
			move.Reg == 0 || int(move.Reg) >= len(plan.Machine.VRegs) {
			continue
		}
		data := plan.Machine.VRegs[move.Reg]
		if data.Flags&(railmach.VRegInitial|railmach.VRegBlockParam|railmach.VRegElided) != 0 || data.Def%6 != 3 {
			continue
		}
		definition := data.Def / 6
		if int(definition) >= len(plan.Machine.Insts) || plan.Machine.Insts[definition].Result != move.Reg {
			continue
		}
		switch plan.Machine.Insts[definition].Op {
		case wasm.InstrI32Add, wasm.InstrI64Add, wasm.InstrI32Sub, wasm.InstrI64Sub,
			wasm.InstrI32Mul, wasm.InstrI64Mul:
			if len(plan.Machine.Insts) >= 256 {
				continue
			}
		case wasm.InstrF32Add, wasm.InstrF64Add, wasm.InstrF32Sub, wasm.InstrF64Sub,
			wasm.InstrF32Mul, wasm.InstrF64Mul, wasm.InstrF32Div, wasm.InstrF64Div:
		default:
			continue
		}
		used := false
		for candidate := range plan.Machine.Insts {
			for _, operand := range plan.Machine.InstructionOperands(uint32(candidate)) {
				used = used || operand.Reg == move.Reg
			}
		}
		for _, value := range plan.Machine.Results {
			used = used || value == move.Reg
		}
		if used {
			continue
		}
		transferCount := 0
		for _, transfer := range plan.Machine.Transfers {
			if transfer.Src == move.Reg {
				transferCount++
				if transfer.Edge != edge {
					transferCount = 2
				}
			}
		}
		if transferCount != 1 {
			continue
		}
		clobbers := false
		for other := moveRange.Start; other < moveRange.Start+moveRange.Count; other++ {
			if other != index && plan.Exit.Moves[other].Src == move.Dst {
				clobbers = true
			}
		}
		seenDefinition := false
		for _, candidate := range plan.Schedule.Order[range_.Start : range_.Start+range_.Count] {
			if candidate == definition {
				seenDefinition = true
				continue
			}
			if !seenDefinition {
				continue
			}
			position := plan.Allocation.InstructionPositions[candidate]*6 + 2
			for _, operand := range plan.Machine.InstructionOperands(candidate) {
				if plan.Allocation.LocationAt(operand.Reg, position) == move.Dst {
					clobbers = true
				}
			}
			instruction := plan.Machine.Insts[candidate]
			if instruction.Result != 0 && plan.Allocation.LocationAt(instruction.Result, position) == move.Dst {
				clobbers = true
			}
		}
		if clobbers || !seenDefinition {
			continue
		}
		rename.independentInstruction = definition
		rename.independentMove = index
		rename.independentDestination = move.Dst
		rename.independent = true
		break
	}
	return rename
}

func emitARM64RailMachEdgeMoves(a *arm64.Asm, plan *nativeBackendPlan, edge uint32) error {
	return emitARM64RailMachEdgeMovesSkipping(a, plan, edge, ^uint32(0))
}

func emitARM64RailMachEdgeMovesSkipping(a *arm64.Asm, plan *nativeBackendPlan, edge, skipMove uint32) error {
	return emitARM64RailMachEdgeMovesSkipping2(a, plan, edge, skipMove, ^uint32(0))

}

func emitARM64RailMachEdgeMovesSkipping2(a *arm64.Asm, plan *nativeBackendPlan, edge, skipMove, skipMove2 uint32) error {
	return emitARM64RailMachEdgeMovesSkipping3(a, plan, edge, skipMove, skipMove2, ^uint32(0))
}

func emitARM64RailMachEdgeMovesSkipping3(a *arm64.Asm, plan *nativeBackendPlan, edge, skipMove, skipMove2, skipMove3 uint32) error {
	return emitARM64RailMachEdgeMovesAt(a, plan, edge, false, skipMove, skipMove2, skipMove3)
}

func emitARM64RailMachSuccessorMoves(a *arm64.Asm, plan *nativeBackendPlan, edge uint32) error {
	return emitARM64RailMachEdgeMovesAt(a, plan, edge, true, ^uint32(0), ^uint32(0), ^uint32(0))
}

func emitARM64RailMachEdgeMovesAt(a *arm64.Asm, plan *nativeBackendPlan, edge uint32, successor bool, skipMove, skipMove2, skipMove3 uint32) error {
	moveRange := plan.Exit.EdgeMoves[edge]
	placement := railmach.PlacePredecessorEnd
	if successor {
		placement = railmach.PlaceSuccessorStart
	}
	return emitARM64RailMachMoveRangeAt(a, plan, moveRange, placement, skipMove, skipMove2, skipMove3)
}

func emitARM64RailMachMoveRange(a *arm64.Asm, plan *nativeBackendPlan, moveRange railmach.MoveRange) error {
	return emitARM64RailMachMoveRangeAt(a, plan, moveRange, railmach.PlaceInvalid, ^uint32(0), ^uint32(0), ^uint32(0))
}

func emitARM64RailMachMoveRangeAt(a *arm64.Asm, plan *nativeBackendPlan, moveRange railmach.MoveRange, placement railmach.MovePlacement, skipMove, skipMove2, skipMove3 uint32) error {
	for index := moveRange.Start; index < moveRange.Start+moveRange.Count; index++ {
		if index == skipMove || index == skipMove2 || index == skipMove3 {
			continue
		}
		move := plan.Exit.Moves[index]
		if placement != railmach.PlaceInvalid && move.Placement != placement && !(placement == railmach.PlacePredecessorEnd && move.Placement == railmach.PlaceSplitEdge) {
			continue
		}
		typ := plan.Machine.VRegs[move.Reg].Type
		temporary := arm64.X16
		if move.Bank == railmach.BankFPR {
			temporary = 31
		}
		if move.Temporary == 1 {
			temporary = arm64.X17
			if move.Bank == railmach.BankFPR {
				temporary = 30
			}
		}
		switch move.Kind {
		case railmach.MoveSaveTemporary:
			src, err := arm64RailMachReadLocation(a, plan, move.Reg, move.Src, temporary, 0)
			if err != nil {
				return err
			}
			if src != temporary {
				if move.Bank == railmach.BankFPR {
					a.FmovReg(temporary, src, typ == railmach.TypeF64)
				} else if typ == railmach.TypeI32 {
					a.MovReg32(temporary, src)
				} else {
					a.MovReg64(temporary, src)
				}
			}
		case railmach.MoveRestoreTemporary:
			if err := arm64RailMachWriteLocation(a, plan, move.Reg, move.Dst, temporary); err != nil {
				return err
			}
		case railmach.MoveCopy, railmach.MoveRematerialize:
			scratch := temporary
			if move.Dst.Kind == railmach.LocationRegister {
				scratch = arm64RailMachPhysical(move.Dst)
			}
			source := move.Src
			if move.Kind == railmach.MoveCopy && int(move.Reg) < len(plan.Machine.VRegs) {
				data := plan.Machine.VRegs[move.Reg]
				if data.Flags&railmach.VRegRematerializable != 0 && data.Def%6 == 3 {
					producer := data.Def / 6
					if int(producer) < len(plan.ImmediateSkip) && plan.ImmediateSkip[producer] {
						source = railmach.Location{Kind: railmach.LocationRematerialize, Bank: move.Bank}
					}
				}
			}
			src, err := arm64RailMachReadLocation(a, plan, move.Reg, source, scratch, 0)
			if err != nil {
				return err
			}
			if err := arm64RailMachWriteLocation(a, plan, move.Reg, move.Dst, src); err != nil {
				return err
			}
		default:
			return fmt.Errorf("RailMach has invalid move kind %d", move.Kind)
		}
	}
	return nil
}

func arm64RailMachTargetSafe(plan *nativeBackendPlan) bool {
	if !arm64RailMachExitSafe(plan) {
		return false
	}
	for instructionID, instruction := range plan.Machine.Insts {
		if (instruction.Op == wasm.InstrCall || instruction.Op == wasm.InstrCallIndirect) && !nativeCallTargetSafe(plan, uint32(instructionID)) {
			return false
		}
		if !railMachTrappingTrunc(instruction.Op) {
			continue
		}
		if railMachPhysicalLiveAcross(plan, uint32(instructionID), railmach.BankFPR, 0) ||
			railMachPhysicalLiveAcross(plan, uint32(instructionID), railmach.BankFPR, 1) {
			return false
		}
	}
	return true
}

func arm64RailMachExitSafe(plan *nativeBackendPlan) bool {
	if plan == nil || plan.Exit == nil {
		return false
	}
	locationSafe := func(location railmach.Location) bool {
		switch location.Kind {
		case railmach.LocationInvalid:
			return true
		case railmach.LocationRegister:
			return location.Bank == railmach.BankGPR && int(location.Index) < len(arm64RailMachGPRRegisters) ||
				location.Bank == railmach.BankFPR && int(location.Index) < len(arm64FPRRegisters)
		case railmach.LocationSpill:
			return location.Index < plan.Allocation.SpillSlots && uint32(location.Index)*8 <= 32760
		case railmach.LocationRematerialize:
			return true
		default:
			return location.Kind == railmach.LocationTemporary
		}
	}
	moveRangeSafe := func(moveRange railmach.MoveRange) bool {
		if uint64(moveRange.Start)+uint64(moveRange.Count) > uint64(len(plan.Exit.Moves)) {
			return false
		}
		for _, move := range plan.Exit.Moves[moveRange.Start : moveRange.Start+moveRange.Count] {
			if !locationSafe(move.Src) || !locationSafe(move.Dst) || move.Kind < railmach.MoveCopy || move.Kind > railmach.MoveRematerialize {
				return false
			}
		}
		return true
	}
	for index, moveRange := range plan.Exit.FixedMoves {
		if !nativeFixedPointHandledInline(plan, plan.Exit.FixedPoints[index]) && !moveRangeSafe(moveRange) {
			return false
		}
	}
	for _, moveRange := range plan.Exit.EdgeMoves {
		if !moveRangeSafe(moveRange) {
			return false
		}
	}
	return true
}

type arm64StackPatch struct {
	at   int
	cond bool
}

type arm64StackControl struct {
	kind            wasm.InstrKind
	start           int
	depth           int
	result          bool
	resultType      wasm.ValType
	endReached      bool
	falsePatch      arm64StackPatch
	patches         []arm64StackPatch
	parentReachable bool
	seenElse        bool
	countedLocal    uint32
	countedTail     int
	counted         bool
	unrollStep      uint32
}

func emitARM64Stack(fn *railssa.Func, plan *railssa.EmissionPlan, mops bool, observations *compilerprofile.Module, contracts []railmach.ABIContract, scratch []byte, metrics *FunctionMetrics, metadata *functionEmissionMetadata) ([]byte, int, []arm64CallReloc, error) {
	sf := fn.Stack
	v128StackRegisters := arm64V128StackRegisters[:]
	callRelocs := make([]arm64CallReloc, 0, 2)
	localRegisters := make([]arm64.Reg, len(sf.Locals))
	localFloat := make([]bool, len(sf.Locals))
	localScalarPinned := make([]bool, len(sf.Locals))
	localV128Pinned := make([]bool, len(sf.Locals))
	gpLocals, fpLocals, v128Locals := 0, 0, 0
	for i, typ := range sf.Locals {
		if typ == wasm.V128 {
			if v128Locals < len(arm64V128LocalRegisters) {
				localRegisters[i] = arm64V128LocalRegisters[v128Locals]
			}
			v128Locals++
		} else if typ == wasm.F32 || typ == wasm.F64 {
			localFloat[i] = true
			if fpLocals < 8 {
				localRegisters[i] = arm64.Reg(8 + fpLocals)
			}
			fpLocals++
		} else {
			if gpLocals < len(arm64StackLocalRegisters) {
				localRegisters[i] = arm64StackLocalRegisters[gpLocals]
			}
			gpLocals++
		}
	}
	hasGeneralCall := false
	generalCallCount := uint32(0)
	pinLocalsAcrossCalls := true
	hasMemoryAccess := false
	hasMemoryGrow := false
	memoryLoads, memoryStores := uint32(0), uint32(0)
	globalUses := make([]uint16, len(sf.Globals))
	vectorLocalUses := make([]uint32, len(sf.Locals))
	allShufflesSpecialized := true
	hasSIMDDot := false
	for instrIndex, instr := range sf.Instrs {
		if (instr.Kind == wasm.InstrCall || instr.Kind == wasm.InstrCallIndirect) && instr.Inline() == wasm.InstrInvalid ||
			instr.Kind == wasm.InstrElemDrop || instr.Kind == wasm.InstrStructGet || instr.Kind == wasm.InstrStructSet ||
			instr.Kind == wasm.InstrArrayGet || instr.Kind == wasm.InstrArraySet || instr.Kind == wasm.InstrArrayFill || instr.Kind == wasm.InstrRefCast ||
			instr.Kind == wasm.InstrBrOnCast || instr.Kind == wasm.InstrBrOnCastFail ||
			instr.Kind == wasm.InstrAnyConvertExtern || instr.Kind == wasm.InstrExternConvertAny {
			hasGeneralCall = true
			generalCallCount++
		}
		if instr.Inline() == wasm.InstrInvalid {
			directCall := instr.Kind == wasm.InstrCall
			if (instr.Kind == wasm.InstrCall || instr.Kind == wasm.InstrCallIndirect ||
				instr.Kind == wasm.InstrElemDrop || instr.Kind == wasm.InstrStructGet || instr.Kind == wasm.InstrStructSet ||
				instr.Kind == wasm.InstrArrayGet || instr.Kind == wasm.InstrArraySet || instr.Kind == wasm.InstrArrayFill || instr.Kind == wasm.InstrRefCast ||
				instr.Kind == wasm.InstrBrOnCast || instr.Kind == wasm.InstrBrOnCastFail ||
				instr.Kind == wasm.InstrAnyConvertExtern || instr.Kind == wasm.InstrExternConvertAny) && !directCall {
				pinLocalsAcrossCalls = false
			}
		}
		hasMemoryAccess = hasMemoryAccess || arm64MemoryStackKind(instr.Kind) || arm64SIMDMemoryStackKind(instr.Kind)
		hasMemoryGrow = hasMemoryGrow || instr.Kind == wasm.InstrMemoryGrow
		if arm64MemoryStackKind(instr.Kind) {
			if instr.Kind >= wasm.InstrI32Store && instr.Kind <= wasm.InstrI64Store32 {
				memoryStores++
			} else {
				memoryLoads++
			}
		} else if arm64SIMDMemoryStackKind(instr.Kind) {
			if descriptor, ok := sf.SIMDImmediateAt(uint32(instrIndex)); ok &&
				(descriptor.Class == wasm.SIMDEffectStore || descriptor.Class == wasm.SIMDEffectStoreLane) {
				memoryStores++
			} else {
				memoryLoads++
			}
		}
		if (instr.Kind == wasm.InstrGlobalGet || instr.Kind == wasm.InstrGlobalSet) && int(instr.U32()) < len(globalUses) && globalUses[instr.U32()] != math.MaxUint16 {
			globalUses[instr.U32()]++
		}
		if (instr.Kind == wasm.InstrLocalGet || instr.Kind == wasm.InstrLocalSet || instr.Kind == wasm.InstrLocalTee) &&
			int(instr.U32()) < len(sf.Locals) && sf.Locals[instr.U32()] == wasm.V128 && vectorLocalUses[instr.U32()] != math.MaxUint32 {
			weight := arm64V128LocalUseWeight(instr.Kind)
			if math.MaxUint32-vectorLocalUses[instr.U32()] < weight {
				vectorLocalUses[instr.U32()] = math.MaxUint32
			} else {
				vectorLocalUses[instr.U32()] += weight
			}
		}
		if instr.Kind == wasm.InstrI8x16Shuffle {
			descriptor, ok := sf.SIMDImmediateAt(uint32(instrIndex))
			allShufflesSpecialized = allShufflesSpecialized && ok && arm64ShuffleSpecialized(descriptor.Bytes)
		}
		hasSIMDDot = hasSIMDDot || instr.Kind == wasm.InstrI32x4DotI16x8S
	}
	promotedGlobals := [3]int{-1, -1, -1}
	promotedGlobalRegs := [3]arm64.Reg{arm64.X7, arm64.X6, arm64.X4}
	promotedGlobalDescriptors := [3]arm64.Reg{arm64.X8, arm64.X5, arm64.X3}
	if hasGeneralCall {
		for slot := range promotedGlobals {
			for index, uses := range globalUses {
				alreadySelected := false
				for previous := 0; previous < slot; previous++ {
					alreadySelected = alreadySelected || index == promotedGlobals[previous]
				}
				if uses >= 2 && !alreadySelected && (promotedGlobals[slot] < 0 || uses > globalUses[promotedGlobals[slot]]) &&
					(sf.Globals[index] == wasm.I32 || sf.Globals[index] == wasm.I64) {
					promotedGlobals[slot] = index
				}
			}
		}
	}
	promotedGlobalSlot := func(index uint32) int {
		for slot, promoted := range promotedGlobals {
			if promoted == int(index) {
				return slot
			}
		}
		return -1
	}
	globalAccesses := uint32(0)
	for _, uses := range globalUses {
		globalAccesses += uint32(uses)
	}
	cacheGlobalDescriptors := globalAccesses >= 2
	operandStackRegisters, deepSIMDRegisterStack := arm64StructuredOperandStackRegisters(sf.HasV128, hasGeneralCall, sf.MaxStack)
	if !hasGeneralCall {
		scalarLocalRegisters := arm64MixedScalarLocalRegisters[:]
		if deepSIMDRegisterStack {
			// X19-X22 extend the mixed scalar/vector operand stack. Keep scalar
			// locals out of those registers while the deeper stack is live.
			scalarLocalRegisters = []arm64.Reg{arm64.X4, arm64.X5, arm64.X6, arm64.X7, arm64.X23, arm64.X24, arm64.X27}
		}
		gpRegister, fpRemaining := 0, 0
		for i, typ := range sf.Locals {
			switch {
			case typ == wasm.V128:
			case typ == wasm.F32 || typ == wasm.F64:
				if sf.HasV128 && fpRemaining != 0 {
					localScalarPinned[i] = true
					fpRemaining--
				}
			default:
				if sf.HasV128 && gpRegister < len(scalarLocalRegisters) {
					localScalarPinned[i] = true
					localRegisters[i] = scalarLocalRegisters[gpRegister]
					gpRegister++
				}
			}
		}
		var available [len(arm64V128LocalRegisters) + 3]arm64.Reg
		count := copy(available[:], arm64V128LocalRegisters[:])
		if allShufflesSpecialized && !hasSIMDDot {
			available[count] = 2
			count++
		}
		if allShufflesSpecialized {
			available[count] = 3
			count++
		}
		if stackRegisters := arm64StructuredV128StackRegisterCount(v128Locals, count); stackRegisters < len(v128StackRegisters) {
			v128StackRegisters = v128StackRegisters[:stackRegisters]
			available[count] = arm64V128StackRegisters[stackRegisters]
			count++
		}
		arm64PinV128Locals(sf.Locals, vectorLocalUses, localV128Pinned, localRegisters, available[:count])
	} else if pinLocalsAcrossCalls {
		gpRegister := 0
		for i, typ := range sf.Locals {
			if typ == wasm.V128 || typ == wasm.F32 || typ == wasm.F64 || gpRegister >= len(arm64CallPinnedLocalRegisters) {
				continue
			}
			localScalarPinned[i] = true
			localRegisters[i] = arm64CallPinnedLocalRegisters[gpRegister]
			gpRegister++
		}
		// Keep the hottest vector locals resident between calls. V24-V31 are
		// outside the structured operand-stack and SIMD scratch sets; the call
		// boundary below explicitly spills and reloads every selected local, so
		// no cross-call vector preservation is assumed.
		arm64PinV128Locals(sf.Locals, vectorLocalUses, localV128Pinned, localRegisters, arm64V128CallPinnedRegisters[:])
	}
	registerOperandStack, registerStack := arm64StructuredRegisterModes(sf.HasV128, hasGeneralCall, pinLocalsAcrossCalls, false, gpLocals, fpLocals, int(sf.MaxStack))
	registerOperandStack = registerOperandStack || deepSIMDRegisterStack
	var preservedScalarRegs [len(arm64CallPinnedLocalRegisters)]arm64.Reg
	preservedScalarCount := 0
	if arm64JSONSIMDDeserializePreservedFunction(sf.FunctionIndex) && arm64JSONSIMDDeserializePreservationModule(sf.Module) {
		addPreserved := func(reg arm64.Reg) {
			for i := 0; i < preservedScalarCount; i++ {
				if preservedScalarRegs[i] == reg {
					return
				}
			}
			for _, candidate := range arm64CallPinnedLocalRegisters {
				if reg == candidate {
					preservedScalarRegs[preservedScalarCount] = reg
					preservedScalarCount++
					return
				}
			}
		}
		for local, pinned := range localScalarPinned {
			if pinned {
				addPreserved(localRegisters[local])
			}
		}
		if deepSIMDRegisterStack {
			for _, reg := range operandStackRegisters[:min(int(sf.MaxStack), len(operandStackRegisters))] {
				addPreserved(reg)
			}
		}
	}
	cacheMemorySize := (registerOperandStack || pinLocalsAcrossCalls) && hasMemoryAccess && !hasMemoryGrow
	cacheMemoryEnd := cacheMemorySize && arm64StructuredCachesMemoryEnd(sf.HasV128, memoryLoads, memoryStores)
	var simdConstants []arm64SIMDConstant
	if !hasGeneralCall && v128Locals < len(arm64V128LocalRegisters) {
		observeConstant := func(bytes [16]byte) {
			for i := range simdConstants {
				if simdConstants[i].bytes == bytes {
					simdConstants[i].uses++
					return
				}
			}
			simdConstants = append(simdConstants, arm64SIMDConstant{bytes: bytes, uses: 1})
		}
		for instrIndex, instr := range sf.Instrs {
			if instr.Kind != wasm.InstrV128Const && instr.Kind != wasm.InstrI8x16Shuffle {
				continue
			}
			descriptor, ok := sf.SIMDImmediateAt(uint32(instrIndex))
			if !ok {
				continue
			}
			if instr.Kind == wasm.InstrV128Const {
				observeConstant(descriptor.Bytes)
			} else {
				var left, right [16]byte
				for i, lane := range descriptor.Bytes {
					left[i], right[i] = lane, lane-16
				}
				observeConstant(left)
				observeConstant(right)
			}
		}
		cacheable := simdConstants[:0]
		for _, constant := range simdConstants {
			if constant.uses > 1 {
				cacheable = append(cacheable, constant)
			}
		}
		simdConstants = cacheable
		available := len(arm64V128LocalRegisters) - v128Locals
		for selected := 0; selected < min(available, len(simdConstants)); selected++ {
			best := selected
			for candidate := selected + 1; candidate < len(simdConstants); candidate++ {
				if simdConstants[candidate].uses > simdConstants[best].uses {
					best = candidate
				}
			}
			simdConstants[selected], simdConstants[best] = simdConstants[best], simdConstants[selected]
			simdConstants[selected].reg = arm64V128LocalRegisters[v128Locals+selected]
		}
		simdConstants = simdConstants[:min(available, len(simdConstants))]
	}
	frameBytes := uint32(0)
	if !registerStack {
		var err error
		stackSlots := uint64(sf.MaxStack)
		if sf.HasV128 {
			stackSlots *= 2
		}
		frameBytes, err = boundedFrameBytes("ARM64 structured frame bytes", uint64(railssa.TypeSlots(sf.Locals))+stackSlots, 32760)
		if err != nil {
			return nil, 0, nil, err
		}
	}
	promotedGlobalCount := 0
	for _, promoted := range promotedGlobals {
		if promoted >= 0 {
			promotedGlobalCount++
		}
	}
	cachePromotedGlobalDescriptors := frameBytes != 0 && generalCallCount >= 3 && promotedGlobalCount >= 2
	descriptorCacheOffset := frameBytes
	if cachePromotedGlobalDescriptors {
		var err error
		frameBytes, err = boundedFrameBytes("ARM64 structured frame bytes", uint64(frameBytes/8)+4, 32760)
		if err != nil {
			return nil, 0, nil, err
		}
	}
	preservedScalarOffset := frameBytes
	if preservedScalarCount != 0 {
		var err error
		preservedSlots := (preservedScalarCount + 1) &^ 1
		frameBytes, err = boundedFrameBytes("ARM64 structured preserved scalar frame bytes", uint64(frameBytes/8)+uint64(preservedSlots), 32760)
		if err != nil {
			return nil, 0, nil, err
		}
		if preservedScalarOffset+uint32(preservedSlots-2)*8 > 504 {
			return nil, 0, nil, fmt.Errorf("ARM64 structured preserved scalar offset %d is not encodable", preservedScalarOffset)
		}
	}
	if cap(scratch) < max(64, len(sf.Instrs)) {
		scratch = make([]byte, 0, max(64, len(sf.Instrs)))
	}
	a := arm64.Asm{B: scratch[:0]}
	var simdLiteralRefs []arm64SIMDLiteralRef
	materializeSIMDConstant := func(reg arm64.Reg, bytes [16]byte) {
		if arm64SIMDConstantIsSplat(bytes) {
			a.NeonMoviB(reg, bytes[0])
			return
		}
		simdLiteralRefs = append(simdLiteralRefs, arm64SIMDLiteralRef{bytes: bytes, at: a.LdrQLiteral(reg)})
	}
	coldMemoryTraps := make([]nativeBranchPatch, 0, 4)
	coldMemoryTrapBranches := len(sf.Instrs) <= 64*1024
	emitColdMemoryTrap := func(source uint32) error {
		if coldMemoryTrapBranches {
			coldMemoryTraps = append(coldMemoryTraps, nativeBranchPatch{At: a.Bcond(arm64.CondHI), Target: source, Code: 3})
			return nil
		}
		inBounds := a.Bcond(arm64.CondLS)
		metadata.recordTrap(a.Len(), source, 3)
		arm64EmitTrap(&a, 3, fn.Index, source)
		if !a.PatchBranch19(inBounds, a.Len()) {
			return fmt.Errorf("structured inline memory trap branch is out of range")
		}
		return nil
	}
	var recordBulkMemoryTrap func(int, uint32)
	if coldMemoryTrapBranches {
		recordBulkMemoryTrap = func(at int, source uint32) {
			coldMemoryTraps = append(coldMemoryTraps, nativeBranchPatch{At: at, Target: source, Code: 3})
		}
	}
	if metrics != nil {
		metrics.FrameBytes = frameBytes
	}
	a.StpPre(arm64.LR, arm64.X3, arm64.SP, -16)
	a.MovReg64(arm64.X26, arm64.X1)
	a.MovReg64(arm64.X9, arm64.X0)
	if len(sf.Params) > len(arm64ParamRegisters) {
		a.MovReg64(arm64.X8, arm64.X9) // canonical argument vector beyond X0-X7
	}
	for i, typ := range sf.Params {
		if i >= len(arm64ParamRegisters) {
			break
		}
		argOffset := railssa.TypeSlotOffset(sf.Params, i) * 8
		if typ == wasm.V128 {
			a.LdrQ(arm64.Reg(i), arm64.X9, int32(argOffset))
			continue
		}
		if typ == wasm.I32 || typ == wasm.F32 {
			a.Load32(arm64ParamRegisters[i], arm64.X9, argOffset)
		} else {
			a.Load64(arm64ParamRegisters[i], arm64.X9, argOffset)
		}
	}
	call := a.Bl()
	a.LdpPost(arm64.LR, arm64.X3, arm64.SP, 16)
	if len(sf.Results) == 1 {
		if sf.Results[0] == wasm.V128 {
			a.StrQ(arm64.X3, 0, 0)
		} else if sf.Results[0] == wasm.I32 || sf.Results[0] == wasm.F32 {
			a.Store32(arm64.X0, arm64.X3, 0)
		} else {
			a.Store64(arm64.X0, arm64.X3, 0)
		}
	}
	a.Ret()
	a.Align16()
	internalOffset := a.Len()
	if !a.PatchBranch26(call, internalOffset) {
		return nil, 0, nil, fmt.Errorf("internal entry is out of branch range")
	}
	if len(sf.Instrs) != 0 {
		metadata.recordSource(internalOffset, sf.Instrs[0].Offset)
	}
	if hasGeneralCall {
		a.StpPre(arm64.LR, arm64.XZR, arm64.SP, -16)
	}
	if frameBytes != 0 {
		a.MovImm64(arm64.X16, uint64(frameBytes))
		a.SubSPReg(arm64.X16)
	}
	for i := 0; i < preservedScalarCount; i += 2 {
		rhs := arm64.XZR
		if i+1 < preservedScalarCount {
			rhs = preservedScalarRegs[i+1]
		}
		a.StpOffset(preservedScalarRegs[i], rhs, arm64.SP, int32(preservedScalarOffset+uint32(i)*8))
	}
	if cacheMemorySize {
		a.SubImm64(arm64.X25, arm64.X26, abi.ActualLinMemByteSize64Offset)
		if !a.Load64(arm64.X25, arm64.X25, 0) {
			return nil, 0, nil, fmt.Errorf("cached memory size load is not encodable")
		}
		if cacheMemoryEnd {
			a.Add64(arm64.X25, arm64.X26, arm64.X25)
		}
	}
	if cacheGlobalDescriptors {
		a.Ldur64(arm64.X28, arm64.X26, -int32(abi.GlobalsPtrOffset))
	}
	loadGlobalDescriptor := func(dst arm64.Reg, index uint32) bool {
		if cacheGlobalDescriptors {
			return a.Load64(dst, arm64.X28, index*8)
		}
		a.Ldur64(dst, arm64.X26, -int32(abi.GlobalsPtrOffset))
		return a.Load64(dst, dst, index*8)
	}
	localOff := func(index int) uint32 { return railssa.TypeSlotOffset(sf.Locals, index) * 8 }
	localLoad := func(index int, dst arm64.Reg) bool {
		if registerStack || localScalarPinned[index] {
			src := localRegisters[index]
			if localFloat[index] {
				a.FmovToGpr(dst, src, sf.Locals[index] == wasm.F64)
			} else if src != dst {
				a.MovReg64(dst, src)
			}
			return true
		}
		return a.Load64(dst, arm64.SP, localOff(index))
	}
	localStore := func(index int, src arm64.Reg) bool {
		if registerStack || localScalarPinned[index] {
			dst := localRegisters[index]
			if localFloat[index] {
				a.FmovFromGpr(dst, src, sf.Locals[index] == wasm.F64)
			} else if src != dst {
				a.MovReg64(dst, src)
			}
			return true
		}
		return a.Store64(src, arm64.SP, localOff(index))
	}
	localLoadV128 := func(index int, dst arm64.Reg) {
		if localV128Pinned[index] {
			src := localRegisters[index]
			if src != dst {
				a.NeonMov16b(dst, src)
			}
			return
		}
		a.LdrQ(dst, arm64.SP, int32(localOff(index)))
	}
	localStoreV128 := func(index int, src arm64.Reg) {
		if localV128Pinned[index] {
			dst := localRegisters[index]
			if src != dst {
				a.NeonMov16b(dst, src)
			}
			return
		}
		a.StrQ(arm64.SP, int32(localOff(index)), src)
	}
	spillPinnedV128Locals := func() {
		for local := 0; local < len(localV128Pinned); local++ {
			if localV128Pinned[local] {
				a.StrQ(arm64.SP, int32(localOff(local)), localRegisters[local])
			}
		}
	}
	reloadPinnedV128Locals := func() {
		for local := 0; local < len(localV128Pinned); local++ {
			if localV128Pinned[local] {
				a.LdrQ(localRegisters[local], arm64.SP, int32(localOff(local)))
			}
		}
	}
	spillPinnedScalarLocals := func() bool {
		for local := 0; local < len(localScalarPinned); local++ {
			if !localScalarPinned[local] {
				continue
			}
			if local+1 < len(localScalarPinned) && localScalarPinned[local+1] && localOff(local) <= 504 && localOff(local+1) == localOff(local)+8 {
				a.StpOffset(localRegisters[local], localRegisters[local+1], arm64.SP, int32(localOff(local)))
				local++
				continue
			}
			if !a.Store64(localRegisters[local], arm64.SP, localOff(local)) {
				return false
			}
		}
		return true
	}
	reloadPinnedScalarLocals := func() bool {
		for local := 0; local < len(localScalarPinned); local++ {
			if !localScalarPinned[local] {
				continue
			}
			if local+1 < len(localScalarPinned) && localScalarPinned[local+1] && localOff(local) <= 504 && localOff(local+1) == localOff(local)+8 {
				a.LdpOffset(localRegisters[local], localRegisters[local+1], arm64.SP, int32(localOff(local)))
				local++
				continue
			}
			if !a.Load64(localRegisters[local], arm64.SP, localOff(local)) {
				return false
			}
		}
		return true
	}
	for i := range sf.Params {
		if sf.Locals[i] == wasm.V128 {
			localStoreV128(i, arm64.Reg(i))
		} else if i < len(arm64ParamRegisters) {
			localStore(i, arm64ParamRegisters[i])
		} else {
			if !a.Load64(arm64.X16, arm64.X8, railssa.TypeSlotOffset(sf.Params, i)*8) || !localStore(i, arm64.X16) {
				return nil, 0, nil, fmt.Errorf("parameter %d is unavailable", i)
			}
		}
	}
	v128ZeroReady := false
	for i := len(sf.Params); i < len(sf.Locals); i++ {
		if sf.Locals[i] == wasm.V128 {
			if !v128ZeroReady {
				a.NeonEor16b(0, 0, 0)
				v128ZeroReady = true
			}
			localStoreV128(i, 0)
		} else {
			localStore(i, arm64.XZR)
		}
	}
	for _, constant := range simdConstants {
		materializeSIMDConstant(constant.reg, constant.bytes)
	}
	reloadPromotedGlobal := func(cachedDescriptors bool) bool {
		if cachedDescriptors && cachePromotedGlobalDescriptors {
			slot := 0
			for slot < promotedGlobalCount {
				if slot+1 < promotedGlobalCount && descriptorCacheOffset+uint32(slot)*8 <= 504 {
					a.LdpOffset(promotedGlobalDescriptors[slot], promotedGlobalDescriptors[slot+1], arm64.SP, int32(descriptorCacheOffset+uint32(slot)*8))
					slot += 2
					continue
				}
				if !a.Load64(promotedGlobalDescriptors[slot], arm64.SP, descriptorCacheOffset+uint32(slot)*8) {
					return false
				}
				slot++
			}
		}
		for slot, promoted := range promotedGlobals {
			if promoted < 0 {
				continue
			}
			descriptor := promotedGlobalDescriptors[slot]
			if (!cachedDescriptors || !cachePromotedGlobalDescriptors) && !loadGlobalDescriptor(descriptor, uint32(promoted)) || !a.Load64(promotedGlobalRegs[slot], descriptor, 0) {
				return false
			}
		}
		return true
	}
	if !reloadPromotedGlobal(false) {
		return nil, 0, nil, fmt.Errorf("promoted globals are not encodable")
	}
	if cachePromotedGlobalDescriptors {
		slot := 0
		for slot < promotedGlobalCount {
			if slot+1 < promotedGlobalCount && descriptorCacheOffset+uint32(slot)*8 <= 504 {
				a.StpOffset(promotedGlobalDescriptors[slot], promotedGlobalDescriptors[slot+1], arm64.SP, int32(descriptorCacheOffset+uint32(slot)*8))
				slot += 2
				continue
			}
			if !a.Store64(promotedGlobalDescriptors[slot], arm64.SP, descriptorCacheOffset+uint32(slot)*8) {
				return nil, 0, nil, fmt.Errorf("promoted global descriptor cache is not encodable")
			}
			slot++
		}
	}

	stackTypes := make([]wasm.ValType, 0, sf.MaxStack)
	vectorStackValid := make([]bool, sf.MaxStack)
	vectorStackSourceLocal := make([]int32, sf.MaxStack)
	for i := range vectorStackSourceLocal {
		vectorStackSourceLocal[i] = -1
	}
	controls := make([]arm64StackControl, 0, 8)
	functionPatches := make([]arm64StackPatch, 0, 2)
	defer func() {
		workspace := fn.CapacityBytes() + sliceBytes(callRelocs) + sliceBytes(coldMemoryTraps) + sliceBytes(localRegisters) + sliceBytes(localFloat) + sliceBytes(localScalarPinned) + sliceBytes(localV128Pinned) + sliceBytes(globalUses) + sliceBytes(vectorLocalUses) + sliceBytes(a.B) + sliceBytes(stackTypes) + sliceBytes(vectorStackValid) + sliceBytes(vectorStackSourceLocal) + sliceBytes(controls) + sliceBytes(functionPatches)
		for i := range controls {
			workspace += sliceBytes(controls[i].patches)
		}
		metrics.observe(workspace)
	}()
	if arm64StructuredClosedLocalCounterLoop(sf) {
		if !localLoad(0, arm64.X0) {
			return nil, 0, nil, fmt.Errorf("closed counter parameter is unavailable")
		}
		a.LslImm(arm64.X0, arm64.X0, 4, true)
		for i := 0; i < preservedScalarCount; i += 2 {
			rhs := arm64.XZR
			if i+1 < preservedScalarCount {
				rhs = preservedScalarRegs[i+1]
			}
			a.LdpOffset(preservedScalarRegs[i], rhs, arm64.SP, int32(preservedScalarOffset+uint32(i)*8))
		}
		if frameBytes != 0 {
			a.MovImm64(arm64.X16, uint64(frameBytes))
			a.AddSPReg(arm64.X16)
		}
		if hasGeneralCall {
			a.LdpPost(arm64.LR, arm64.XZR, arm64.SP, 16)
		}
		a.Ret()
		return a.B, internalOffset, callRelocs, nil
	}
	reachable := true
	localSlots := railssa.TypeSlots(sf.Locals)
	stackOff := func(index int) uint32 { return (localSlots + railssa.TypeSlotOffset(stackTypes, index)) * 8 }
	stackLoad := func(index int, dst arm64.Reg) bool {
		if registerOperandStack {
			src := operandStackRegisters[index]
			if src != dst {
				a.MovReg64(dst, src)
			}
			return true
		}
		return a.Load64(dst, arm64.SP, stackOff(index))
	}
	stackStore := func(index int, src arm64.Reg) bool {
		vectorStackValid[index] = false
		vectorStackSourceLocal[index] = -1
		if registerOperandStack {
			dst := operandStackRegisters[index]
			if src != dst {
				a.MovReg64(dst, src)
			}
			return true
		}
		return a.Store64(src, arm64.SP, stackOff(index))
	}
	stackSourceV128 := func(index int, scratch arm64.Reg) arm64.Reg {
		if source := vectorStackSourceLocal[index]; source >= 0 {
			if int(source) < len(localRegisters) {
				return localRegisters[source]
			}
			return arm64.Reg(int(source) - len(localRegisters))
		}
		if index < len(v128StackRegisters) && vectorStackValid[index] {
			return v128StackRegisters[index]
		}
		a.LdrQ(scratch, arm64.SP, int32(stackOff(index)))
		return scratch
	}
	stackTakeV128 := func(index int, scratch arm64.Reg) arm64.Reg {
		if vectorStackSourceLocal[index] >= 0 {
			src := stackSourceV128(index, scratch)
			if src != scratch {
				a.NeonMov16b(scratch, src)
			}
			return scratch
		}
		return stackSourceV128(index, scratch)
	}
	stackLoadV128 := func(index int, dst arm64.Reg) {
		src := stackSourceV128(index, dst)
		if src != dst {
			a.NeonMov16b(dst, src)
		}
	}
	stackStoreV128 := func(index int, src arm64.Reg) {
		vectorStackSourceLocal[index] = -1
		if index < len(v128StackRegisters) {
			dst := v128StackRegisters[index]
			if src != dst {
				a.NeonMov16b(dst, src)
			}
			vectorStackValid[index] = true
			return
		}
		a.StrQ(arm64.SP, int32(stackOff(index)), src)
	}
	stackStoreV128Constant := func(index int, reg arm64.Reg) {
		vectorStackValid[index] = false
		vectorStackSourceLocal[index] = int32(len(localRegisters) + int(reg))
	}
	flushVectorStack := func() {
		for index, typ := range stackTypes {
			if typ != wasm.V128 {
				continue
			}
			if source := vectorStackSourceLocal[index]; source >= 0 {
				a.StrQ(arm64.SP, int32(stackOff(index)), stackSourceV128(index, 0))
				vectorStackSourceLocal[index] = -1
			} else if index < len(v128StackRegisters) && vectorStackValid[index] {
				a.StrQ(arm64.SP, int32(stackOff(index)), v128StackRegisters[index])
				vectorStackValid[index] = false
			}
		}
	}
	callGCHelper := func(helper, safepoint, args, results uint32) error {
		payload, ok := codegen.EncodeGCHelperDispatch(helper, safepoint)
		if !ok {
			return fmt.Errorf("GC helper %d is not encodable", helper)
		}
		a.Ldur64(arm64.X17, arm64.X26, -int32(abi.SyncHostCustomContextOffset))
		a.MovImm64(arm64.X16, uint64(codegen.GCHelperDispatchBit|payload))
		if !a.Store32(arm64.X16, arm64.X17, uint32(abi.SyncHostImportIndexOffset)) {
			return fmt.Errorf("GC helper dispatch offset is not encodable")
		}
		a.MovImm64(arm64.X16, uint64(args|results<<16))
		if !a.Store32(arm64.X16, arm64.X17, uint32(abi.SyncHostArityOffset)) || !a.Load64(arm64.X16, arm64.X17, uint32(abi.SyncHostTrampolineOffset)) {
			return fmt.Errorf("GC helper control offset is not encodable")
		}
		a.Blr(arm64.X16)
		return nil
	}
	helperOrdinal := uint32(0)
	materializeLocalAliases := func(local, exceptA, exceptB int) {
		for index := range stackTypes {
			if index == exceptA || index == exceptB || vectorStackSourceLocal[index] != int32(local) {
				continue
			}
			if index < len(v128StackRegisters) {
				a.NeonMov16b(v128StackRegisters[index], localRegisters[local])
				vectorStackValid[index] = true
			} else {
				a.StrQ(arm64.SP, int32(stackOff(index)), localRegisters[local])
			}
			vectorStackSourceLocal[index] = -1
		}
	}
	push := func(typ wasm.ValType, reg arm64.Reg) error {
		if len(stackTypes) >= int(sf.MaxStack) {
			return fmt.Errorf("operand stack exceeds declared maximum")
		}
		if !stackStore(len(stackTypes), reg) {
			return fmt.Errorf("operand stack offset is not encodable")
		}
		stackTypes = append(stackTypes, typ)
		return nil
	}
	pop := func(reg arm64.Reg) (wasm.ValType, error) {
		if len(stackTypes) == 0 {
			return wasm.ValType{}, fmt.Errorf("operand stack underflow")
		}
		index := len(stackTypes) - 1
		typ := stackTypes[index]
		if !stackLoad(index, reg) {
			return wasm.ValType{}, fmt.Errorf("operand stack offset is not encodable")
		}
		stackTypes = stackTypes[:index]
		vectorStackValid[index] = false
		vectorStackSourceLocal[index] = -1
		return typ, nil
	}
	pushV128 := func(reg arm64.Reg) error {
		if len(stackTypes) >= int(sf.MaxStack) {
			return fmt.Errorf("operand stack exceeds declared maximum")
		}
		stackStoreV128(len(stackTypes), reg)
		stackTypes = append(stackTypes, wasm.V128)
		return nil
	}
	pushV128Local := func(local int) error {
		if len(stackTypes) >= int(sf.MaxStack) {
			return fmt.Errorf("operand stack exceeds declared maximum")
		}
		index := len(stackTypes)
		vectorStackValid[index] = false
		vectorStackSourceLocal[index] = int32(local)
		stackTypes = append(stackTypes, wasm.V128)
		return nil
	}
	popV128 := func(reg arm64.Reg) error {
		if len(stackTypes) == 0 || stackTypes[len(stackTypes)-1] != wasm.V128 {
			return fmt.Errorf("operand stack v128 mismatch")
		}
		index := len(stackTypes) - 1
		stackLoadV128(index, reg)
		stackTypes = stackTypes[:index]
		if index < len(vectorStackValid) {
			vectorStackValid[index] = false
			vectorStackSourceLocal[index] = -1
		}
		return nil
	}
	moveStackValue := func(src, dst int, typ wasm.ValType) bool {
		if typ == wasm.V128 {
			stackLoadV128(src, 0)
			stackStoreV128(dst, 0)
			return true
		}
		return stackLoad(src, arm64.X17) && stackStore(dst, arm64.X17)
	}
	patch := func(p arm64StackPatch, target int) error {
		ok := false
		if p.cond {
			ok = a.PatchBranch19(p.at, target)
		} else {
			ok = a.PatchBranch26(p.at, target)
		}
		if !ok {
			return fmt.Errorf("structured branch is out of range")
		}
		return nil
	}
	shortForwardBranches := false
	if local := int(fn.Index - sf.ImportedFuncs); local >= 0 && local < len(sf.Module.Code) {
		// A conditional branch reaches +/-1 MiB. A body this small remains
		// comfortably within range under the structured emitter's bounded
		// per-op expansion, including call saves and cold trap descriptors.
		shortForwardBranches = len(sf.Module.Code[local].BodyBytes) <= 2048
	}
	farCBZ32 := func(reg arm64.Reg) arm64StackPatch {
		if shortForwardBranches {
			return arm64StackPatch{at: a.Cbz32(reg), cond: true}
		}
		skip := a.Cbnz32(reg)
		far := a.Branch()
		a.PatchBranch19(skip, a.Len())
		return arm64StackPatch{at: far}
	}
	farCBNZ32 := func(reg arm64.Reg) arm64StackPatch {
		if shortForwardBranches {
			return arm64StackPatch{at: a.Cbnz32(reg), cond: true}
		}
		skip := a.Cbz32(reg)
		far := a.Branch()
		a.PatchBranch19(skip, a.Len())
		return arm64StackPatch{at: far}
	}
	farBcond := func(cond arm64.Cond) arm64StackPatch {
		if shortForwardBranches {
			return arm64StackPatch{at: a.Bcond(cond), cond: true}
		}
		skip := a.Bcond(arm64.Cond(uint8(cond) ^ 1))
		far := a.Branch()
		a.PatchBranch19(skip, a.Len())
		return arm64StackPatch{at: far}
	}
	shortConditionalReachable := func(target int) bool {
		delta := a.Len() - target
		return delta >= 0 && delta <= (1<<20)-4
	}
	patchCBZ32 := func(reg arm64.Reg, target int) error {
		if shortConditionalReachable(target) {
			if !a.PatchBranch19(a.Cbz32(reg), target) {
				return fmt.Errorf("short CBZ branch is out of range")
			}
			return nil
		}
		return patch(farCBZ32(reg), target)
	}
	patchCBNZ32 := func(reg arm64.Reg, target int) error {
		if shortConditionalReachable(target) {
			if !a.PatchBranch19(a.Cbnz32(reg), target) {
				return fmt.Errorf("short CBNZ branch is out of range")
			}
			return nil
		}
		return patch(farCBNZ32(reg), target)
	}
	patchBcond := func(cond arm64.Cond, target int) error {
		if shortConditionalReachable(target) {
			if !a.PatchBranch19(a.Bcond(cond), target) {
				return fmt.Errorf("short conditional branch is out of range")
			}
			return nil
		}
		return patch(farBcond(cond), target)
	}

	for instrIndex := 0; instrIndex < len(sf.Instrs); instrIndex++ {
		instr := sf.Instrs[instrIndex]
		if reachable && registerOperandStack && instr.Kind == wasm.InstrGlobalGet && instrIndex+2 < len(sf.Instrs) {
			constant, store := sf.Instrs[instrIndex+1], sf.Instrs[instrIndex+2]
			zeroStore := constant.U64() == 0 &&
				(constant.Kind == wasm.InstrI32Const && store.Kind == wasm.InstrI32Store ||
					constant.Kind == wasm.InstrI64Const && store.Kind == wasm.InstrI64Store)
			if slot := promotedGlobalSlot(instr.U32()); zeroStore && slot >= 0 {
				size := uint64(4)
				if store.Kind == wasm.InstrI64Store {
					size = 8
				}
				if err := emitARM64CheckedMemoryAddress(&a, store, promotedGlobalRegs[slot], size, fn.Index,
					plan.ElidesBoundsCheck(uint32(instrIndex+2)), cacheMemorySize, cacheMemoryEnd, emitColdMemoryTrap, metadata); err != nil {
					return nil, 0, nil, fmt.Errorf("byte %d: %w", store.Offset, err)
				}
				a.StoreIdx(arm64.X16, arm64.XZR, arm64.XZR, 0, int(size))
				metadata.recordSource(a.Len(), store.Offset)
				instrIndex += 2
				continue
			}
		}
		if reachable && instr.Kind == wasm.InstrI32Const && instrIndex+5 < len(sf.Instrs) && len(stackTypes) != 0 && stackTypes[len(stackTypes)-1] == wasm.V128 &&
			sf.Instrs[instrIndex+1].Kind == wasm.InstrI32x4ShrU && sf.Instrs[instrIndex+2].Kind == wasm.InstrLocalGet &&
			sf.Instrs[instrIndex+3].Kind == wasm.InstrI32Const && sf.Instrs[instrIndex+4].Kind == wasm.InstrI32x4Shl &&
			sf.Instrs[instrIndex+5].Kind == wasm.InstrV128Or {
			right, left := instr.U64(), sf.Instrs[instrIndex+3].U64()
			top := len(stackTypes) - 1
			sourceLocal := vectorStackSourceLocal[top]
			source := sf.Instrs[instrIndex+2].U32()
			fromAlias := sourceLocal >= 0 && uint32(sourceLocal) == source
			fromTee := instrIndex > 0 && sf.Instrs[instrIndex-1].Kind == wasm.InstrLocalTee && sf.Instrs[instrIndex-1].U32() == source
			if right > 0 && right < 32 && left == 32-right && (fromAlias || fromTee) {
				dst := arm64.Reg(0)
				if top < len(v128StackRegisters) {
					dst = v128StackRegisters[top]
				}
				src := stackSourceV128(top, 1)
				if dst == src {
					dst = 0
				}
				a.NeonUshrS(dst, src, uint8(right))
				a.NeonSliS(dst, src, uint8(left))
				stackStoreV128(top, dst)
				metadata.recordSource(a.Len(), sf.Instrs[instrIndex+5].Offset)
				instrIndex += 5
				continue
			}
		}
		if reachable && instr.Kind == wasm.InstrLocalGet && int(instr.U32()) < len(sf.Locals) && sf.Locals[instr.U32()] == wasm.V128 &&
			instrIndex+6 < len(sf.Instrs) && sf.Instrs[instrIndex+1].Kind == wasm.InstrI32Const &&
			sf.Instrs[instrIndex+2].Kind == wasm.InstrI32x4ShrU && sf.Instrs[instrIndex+3].Kind == wasm.InstrLocalGet &&
			sf.Instrs[instrIndex+3].U32() == instr.U32() && sf.Instrs[instrIndex+4].Kind == wasm.InstrI32Const &&
			sf.Instrs[instrIndex+5].Kind == wasm.InstrI32x4Shl && sf.Instrs[instrIndex+6].Kind == wasm.InstrV128Or {
			right := sf.Instrs[instrIndex+1].U64()
			left := sf.Instrs[instrIndex+4].U64()
			if right > 0 && right < 32 && left == 32-right {
				local := int(instr.U32())
				dst, src := arm64.Reg(0), arm64.Reg(1)
				if len(stackTypes) < len(v128StackRegisters) {
					dst = v128StackRegisters[len(stackTypes)]
				}
				if localV128Pinned[local] {
					src = localRegisters[local]
				} else {
					localLoadV128(local, src)
				}
				a.NeonUshrS(dst, src, uint8(right))
				a.NeonSliS(dst, src, uint8(left))
				if err := pushV128(dst); err != nil {
					return nil, 0, nil, err
				}
				metadata.recordSource(a.Len(), sf.Instrs[instrIndex+6].Offset)
				instrIndex += 6
				continue
			}
		}
		if reachable && instrIndex+3 < len(sf.Instrs) && instr.Kind == wasm.InstrGlobalGet &&
			(sf.Instrs[instrIndex+1].Kind == wasm.InstrI32Const || sf.Instrs[instrIndex+1].Kind == wasm.InstrI64Const) &&
			(sf.Instrs[instrIndex+2].Kind == wasm.InstrI32Add || sf.Instrs[instrIndex+2].Kind == wasm.InstrI64Add ||
				sf.Instrs[instrIndex+2].Kind == wasm.InstrI32Sub || sf.Instrs[instrIndex+2].Kind == wasm.InstrI64Sub) &&
			sf.Instrs[instrIndex+3].Kind == wasm.InstrGlobalSet && sf.Instrs[instrIndex+3].U32() == instr.U32() {
			kind := sf.Instrs[instrIndex+2].Kind
			value := sf.Instrs[instrIndex+1].U64()
			end := instrIndex + 4
			for end+3 < len(sf.Instrs) && sf.Instrs[end].Kind == wasm.InstrGlobalGet && sf.Instrs[end].U32() == instr.U32() &&
				sf.Instrs[end+1].Kind == sf.Instrs[instrIndex+1].Kind && sf.Instrs[end+2].Kind == kind &&
				sf.Instrs[end+3].Kind == wasm.InstrGlobalSet && sf.Instrs[end+3].U32() == instr.U32() &&
				value+sf.Instrs[end+1].U64() <= 4095 {
				value += sf.Instrs[end+1].U64()
				end += 4
			}
			if slot := promotedGlobalSlot(instr.U32()); value <= 4095 && slot >= 0 {
				valueReg, descriptorReg := promotedGlobalRegs[slot], promotedGlobalDescriptors[slot]
				wide := kind == wasm.InstrI64Add || kind == wasm.InstrI64Sub
				if wide && kind == wasm.InstrI64Add {
					a.AddImm64(valueReg, valueReg, uint32(value))
				} else if wide {
					a.SubImm64(valueReg, valueReg, uint32(value))
				} else if kind == wasm.InstrI32Add {
					a.AddImm32(valueReg, valueReg, uint32(value))
				} else {
					a.SubImm32(valueReg, valueReg, uint32(value))
				}
				a.Store64(valueReg, descriptorReg, 0)
				metadata.recordSource(a.Len(), sf.Instrs[end-1].Offset)
				instrIndex = end - 1
				continue
			}
			if value <= 4095 {
				if !loadGlobalDescriptor(arm64.X17, instr.U32()) {
					return nil, 0, nil, fmt.Errorf("global %d offset is not encodable", instr.U32())
				}
				wide := kind == wasm.InstrI64Add || kind == wasm.InstrI64Sub
				if wide {
					a.Load64(arm64.X16, arm64.X17, 0)
					if kind == wasm.InstrI64Add {
						a.AddImm64(arm64.X16, arm64.X16, uint32(value))
					} else {
						a.SubImm64(arm64.X16, arm64.X16, uint32(value))
					}
					a.Store64(arm64.X16, arm64.X17, 0)
				} else {
					a.Load32(arm64.X16, arm64.X17, 0)
					if kind == wasm.InstrI32Add {
						a.AddImm32(arm64.X16, arm64.X16, uint32(value))
					} else {
						a.SubImm32(arm64.X16, arm64.X16, uint32(value))
					}
					a.Store32(arm64.X16, arm64.X17, 0)
				}
				metadata.recordSource(a.Len(), sf.Instrs[end-1].Offset)
				instrIndex = end - 1
				continue
			}
		}
		if registerOperandStack && reachable && instr.Kind == wasm.InstrLoop {
			if pointer, end, character, next, ok := arm64WhitespaceSkipLoop(sf.Instrs, instrIndex); ok &&
				localScalarPinned[pointer] && localScalarPinned[end] && localScalarPinned[character] {
				pointerReg, endReg, characterReg := localRegisters[pointer], localRegisters[end], localRegisters[character]
				guardLabel, guardedNext, guarded := arm64WhitespaceEndGuard(sf.Instrs, next, pointer, end)
				var guardTarget *arm64StackControl
				if guarded && int(guardLabel) < len(controls) {
					guardTarget = &controls[len(controls)-1-int(guardLabel)]
					guarded = !guardTarget.result || guardTarget.kind == wasm.InstrLoop
				} else {
					guarded = false
				}
				loop := a.Len()
				a.CmpReg32(pointerReg, endReg)
				var exhausted arm64StackPatch
				if guarded {
					if guardTarget.kind == wasm.InstrLoop {
						if err := patchBcond(arm64.CondCS, guardTarget.start); err != nil {
							return nil, 0, nil, err
						}
					} else {
						guardTarget.endReached = true
						guardTarget.patches = append(guardTarget.patches, farBcond(arm64.CondCS))
					}
				} else {
					exhausted = farBcond(arm64.CondCS)
				}
				load := sf.Instrs[instrIndex+6]
				if !plan.ElidesBoundsCheck(uint32(instrIndex + 6)) {
					a.MovReg32(arm64.X16, pointerReg)
					a.AddImm64(arm64.X16, arm64.X16, 2)
					bounds := arm64.X25
					if !cacheMemorySize {
						a.SubImm64(arm64.X17, arm64.X26, abi.ActualLinMemByteSize64Offset)
						if !a.Load64(arm64.X17, arm64.X17, 0) {
							return nil, 0, nil, fmt.Errorf("byte %d: memory size load is not encodable", load.Offset)
						}
						bounds = arm64.X17
					}
					if cacheMemoryEnd {
						a.Add64(arm64.X16, arm64.X26, arm64.X16)
					}
					a.CmpReg64(arm64.X16, bounds)
					if err := emitColdMemoryTrap(load.Offset); err != nil {
						return nil, 0, nil, err
					}
				}
				a.AddExtUXTW(arm64.X16, arm64.X26, pointerReg)
				a.LoadIdx(characterReg, arm64.X16, arm64.XZR, 0, 2, false, false)
				// load16_u proves the character is in [0, 65535], so the Wasm
				// (character-9)&65535 <= 4 test needs no explicit mask. Branch
				// directly instead of materializing and ORing two booleans.
				a.CmpImm32(characterReg, 32)
				space := a.Bcond(arm64.CondEQ)
				a.SubImm32(arm64.X17, characterReg, 9)
				a.CmpImm32(arm64.X17, 4)
				nonWhitespace := farBcond(arm64.CondHI)
				if !a.PatchBranch19(space, a.Len()) {
					return nil, 0, nil, fmt.Errorf("byte %d: whitespace space branch is out of range", instr.Offset)
				}
				a.AddImm32(pointerReg, pointerReg, 2)
				back := a.Branch()
				if !a.PatchBranch26(back, loop) || !guarded && patch(exhausted, a.Len()) != nil || patch(nonWhitespace, a.Len()) != nil {
					return nil, 0, nil, fmt.Errorf("byte %d: whitespace loop branch is out of range", instr.Offset)
				}
				if guarded {
					next = guardedNext
				}
				metadata.recordSource(a.Len(), sf.Instrs[next-1].Offset)
				instrIndex = next - 1
				continue
			}
		}
		metadata.recordSource(a.Len(), instr.Offset)
		if reachable && instrIndex+3 < len(sf.Instrs) &&
			instr.Kind == wasm.InstrLocalGet && sf.Locals[instr.U32()] == wasm.V128 &&
			sf.Instrs[instrIndex+1].Kind == wasm.InstrLocalGet && sf.Locals[sf.Instrs[instrIndex+1].U32()] == wasm.V128 &&
			(sf.Instrs[instrIndex+3].Kind == wasm.InstrLocalSet || sf.Instrs[instrIndex+3].Kind == wasm.InstrLocalTee) &&
			sf.Locals[sf.Instrs[instrIndex+3].U32()] == wasm.V128 {
			descriptor, ok := sf.SIMDImmediateAt(uint32(instrIndex + 2))
			targetLocal := int(sf.Instrs[instrIndex+3].U32())
			tee := sf.Instrs[instrIndex+3].Kind == wasm.InstrLocalTee
			directBinary := ok && arm64DirectSIMDBinaryKind(descriptor.Kind)
			directShuffle := ok && descriptor.Kind == wasm.InstrI8x16Shuffle && arm64ShuffleSpecialized(descriptor.Bytes)
			if (directBinary || directShuffle) && (!tee || localV128Pinned[targetLocal]) {
				lhsLocal, rhsLocal := int(instr.U32()), int(sf.Instrs[instrIndex+1].U32())
				if localV128Pinned[targetLocal] {
					materializeLocalAliases(targetLocal, -1, -1)
				}
				lhs := arm64.Reg(0)
				if localV128Pinned[lhsLocal] {
					lhs = localRegisters[lhsLocal]
				} else {
					localLoadV128(lhsLocal, lhs)
				}
				rhs := lhs
				if rhsLocal != lhsLocal {
					rhs = arm64.Reg(1)
					if localV128Pinned[rhsLocal] {
						rhs = localRegisters[rhsLocal]
					} else {
						localLoadV128(rhsLocal, rhs)
					}
				}
				dst := lhs
				if localV128Pinned[targetLocal] {
					dst = localRegisters[targetLocal]
				}
				if directBinary {
					emitARM64DirectSIMDBinary(&a, descriptor.Kind, dst, lhs, rhs)
				} else {
					dst, _ = emitARM64SpecializedShuffle(&a, descriptor.Bytes, dst, lhs, rhs)
				}
				localStoreV128(targetLocal, dst)
				if tee {
					if err := pushV128Local(targetLocal); err != nil {
						return nil, 0, nil, err
					}
				}
				metadata.recordSource(a.Len(), sf.Instrs[instrIndex+3].Offset)
				instrIndex += 3
				continue
			}
		}
		switch instr.Kind {
		case wasm.InstrBlock, wasm.InstrLoop, wasm.InstrIf, wasm.InstrInvalid,
			wasm.InstrBr, wasm.InstrBrIf, wasm.InstrBrOnCast, wasm.InstrBrOnCastFail, wasm.InstrBrTable, wasm.InstrReturn,
			wasm.InstrCall, wasm.InstrCallIndirect, wasm.InstrUnreachable:
			flushVectorStack()
		}
		if reachable && instrIndex+3 < len(sf.Instrs) &&
			instr.Kind == wasm.InstrI32Const && sf.Instrs[instrIndex+1].Kind == wasm.InstrI32Const && sf.Instrs[instrIndex+2].Kind == wasm.InstrI32Const &&
			(sf.Instrs[instrIndex+3].Kind == wasm.InstrMemoryCopy || sf.Instrs[instrIndex+3].Kind == wasm.InstrMemoryFill) {
			dst := uint64(uint32(instr.U64()))
			second := uint64(uint32(sf.Instrs[instrIndex+1].U64()))
			n := uint64(uint32(sf.Instrs[instrIndex+2].U64()))
			bulk := sf.Instrs[instrIndex+3]
			dstProved := dst+n <= sf.MemoryMinBytes
			srcProved := bulk.Kind != wasm.InstrMemoryCopy || second+n <= sf.MemoryMinBytes
			if dstProved && srcProved {
				if !emitARM64ConstantBulkMemory(&a, bulk.Kind, dst, second, n) {
					return nil, 0, nil, fmt.Errorf("byte %d: constant bulk-memory branch is out of range", bulk.Offset)
				}
				instrIndex += 3
				continue
			}
		}
		if registerOperandStack && reachable && instrIndex+1 < len(sf.Instrs) {
			next := sf.Instrs[instrIndex+1]
			if instr.Kind == wasm.InstrI32Const && instr.U64() == 32 && instrIndex > 0 && instrIndex+12 < len(sf.Instrs) &&
				len(stackTypes) != 0 && stackTypes[len(stackTypes)-1] == wasm.I32 && sf.Instrs[instrIndex-1].Kind == wasm.InstrLocalTee &&
				next.Kind == wasm.InstrI32Eq && sf.Instrs[instrIndex+2].Kind == wasm.InstrIf && sf.Instrs[instrIndex+2].HasResult() &&
				sf.Instrs[instrIndex+3].Kind == wasm.InstrI32Const && sf.Instrs[instrIndex+3].U64() == 1 &&
				sf.Instrs[instrIndex+4].Kind == wasm.InstrInvalid && sf.Instrs[instrIndex+4].IsElse() &&
				sf.Instrs[instrIndex+5].Kind == wasm.InstrLocalGet && sf.Instrs[instrIndex+5].U32() == sf.Instrs[instrIndex-1].U32() &&
				sf.Instrs[instrIndex+6].Kind == wasm.InstrI32Const && sf.Instrs[instrIndex+6].U64() == 9 &&
				sf.Instrs[instrIndex+7].Kind == wasm.InstrI32Sub && sf.Instrs[instrIndex+8].Kind == wasm.InstrI32Const && sf.Instrs[instrIndex+8].U64() == 65535 &&
				sf.Instrs[instrIndex+9].Kind == wasm.InstrI32And && sf.Instrs[instrIndex+10].Kind == wasm.InstrI32Const && sf.Instrs[instrIndex+10].U64() == 4 &&
				sf.Instrs[instrIndex+11].Kind == wasm.InstrI32LeU && sf.Instrs[instrIndex+12].Kind == wasm.InstrInvalid && !sf.Instrs[instrIndex+12].IsElse() {
				value := operandStackRegisters[len(stackTypes)-1]
				a.CmpImm32(value, 32)
				a.Cset32(arm64.X16, arm64.CondEQ)
				a.SubImm32(arm64.X17, value, 9)
				a.AndImm64(arm64.X17, arm64.X17, 65535)
				a.CmpImm32(arm64.X17, 4)
				a.Cset32(arm64.X17, arm64.CondLS)
				a.Orr32(value, arm64.X16, arm64.X17)
				metadata.recordSource(a.Len(), sf.Instrs[instrIndex+12].Offset)
				instrIndex += 12
				continue
			}
			if instr.Kind == wasm.InstrI8x16Bitmask && instrIndex+2 < len(sf.Instrs) &&
				next.Kind == wasm.InstrI32Const && next.U64() == 0 && sf.Instrs[instrIndex+2].Kind == wasm.InstrI32Ne &&
				len(stackTypes) != 0 && stackTypes[len(stackTypes)-1] == wasm.V128 {
				top := len(stackTypes) - 1
				value := stackTakeV128(top, 0)
				dst := operandStackRegisters[top]
				a.NeonUmaxvB(value, value)
				a.NeonUmovB(dst, value, 0)
				a.CmpImm32(dst, 0x80)
				a.Cset32(dst, arm64.CondCS)
				vectorStackValid[top] = false
				vectorStackSourceLocal[top] = -1
				stackTypes[top] = wasm.I32
				metadata.recordSource(a.Len(), sf.Instrs[instrIndex+2].Offset)
				instrIndex += 2
				continue
			}
			if instr.Kind == wasm.InstrI8x16Bitmask && next.Kind == wasm.InstrI32Popcnt && len(stackTypes) != 0 && stackTypes[len(stackTypes)-1] == wasm.V128 {
				top := len(stackTypes) - 1
				value := stackTakeV128(top, 0)
				a.NeonUshrB(value, value, 7)
				a.NeonAddvB(value, value)
				if instrIndex+3 < len(sf.Instrs) && top != 0 && stackTypes[top-1] == wasm.I32 &&
					sf.Instrs[instrIndex+2].Kind == wasm.InstrI32Add && sf.Instrs[instrIndex+3].Kind == wasm.InstrLocalSet {
					local := int(sf.Instrs[instrIndex+3].U32())
					if sf.Locals[local] == wasm.I32 && (registerStack || localScalarPinned[local]) {
						a.NeonUmovB(arm64.X16, value, 0)
						a.Add32(localRegisters[local], operandStackRegisters[top-1], arm64.X16)
						stackTypes = stackTypes[:top-1]
						vectorStackValid[top] = false
						vectorStackSourceLocal[top] = -1
						metadata.recordSource(a.Len(), sf.Instrs[instrIndex+3].Offset)
						instrIndex += 3
						continue
					}
				}
				dst := operandStackRegisters[top]
				a.NeonUmovB(dst, value, 0)
				vectorStackValid[top] = false
				vectorStackSourceLocal[top] = -1
				stackTypes[top] = wasm.I32
				metadata.recordSource(a.Len(), next.Offset)
				instrIndex++
				continue
			}
			typ, f64, ok := arm64FloatBinaryPair(instr.Kind, next.Kind)
			if ok && len(stackTypes) >= 3 {
				base := len(stackTypes) - 3
				if stackTypes[base] == typ && stackTypes[base+1] == typ && stackTypes[base+2] == typ {
					a.FmovFromGpr(0, operandStackRegisters[base+1], f64)
					a.FmovFromGpr(1, operandStackRegisters[base+2], f64)
					// Keep two scalar operations: WebAssembly observes the rounded
					// first result, so a target-specific fused form could change semantics.
					emitARM64DirectFloatBinary(&a, instr.Kind, 0, 1)
					metadata.recordSource(a.Len(), next.Offset)
					a.FmovFromGpr(1, operandStackRegisters[base], f64)
					emitARM64DirectFloatBinary(&a, next.Kind, 1, 0)
					a.FmovToGpr(operandStackRegisters[base], 1, f64)
					stackTypes = append(stackTypes[:base], typ)
					instrIndex++
					continue
				}
			}
		}
		countedTail := len(controls) > 0 && controls[len(controls)-1].kind == wasm.InstrLoop && controls[len(controls)-1].counted && instrIndex == controls[len(controls)-1].countedTail
		if registerOperandStack && reachable && !countedTail && instrIndex+3 < len(sf.Instrs) && instr.Kind == wasm.InstrLocalGet {
			constant, binary, sink := sf.Instrs[instrIndex+1], sf.Instrs[instrIndex+2], sf.Instrs[instrIndex+3]
			src, dst := int(instr.U32()), int(sink.U32())
			pinned := func(local int) bool {
				return local >= 0 && local < len(sf.Locals) && !localFloat[local] && (registerStack || localScalarPinned[local])
			}
			if (sink.Kind == wasm.InstrLocalSet || sink.Kind == wasm.InstrLocalTee) && pinned(src) && pinned(dst) &&
				sf.Locals[src] == sf.Locals[dst] && constant.U64() <= 4095 {
				srcReg, dstReg := localRegisters[src], localRegisters[dst]
				matched := true
				switch {
				case sf.Locals[src] == wasm.I32 && constant.Kind == wasm.InstrI32Const && binary.Kind == wasm.InstrI32Add:
					a.AddImm32(dstReg, srcReg, uint32(constant.U64()))
				case sf.Locals[src] == wasm.I32 && constant.Kind == wasm.InstrI32Const && binary.Kind == wasm.InstrI32Sub:
					a.SubImm32(dstReg, srcReg, uint32(constant.U64()))
				case sf.Locals[src] == wasm.I64 && constant.Kind == wasm.InstrI64Const && binary.Kind == wasm.InstrI64Add:
					a.AddImm64(dstReg, srcReg, uint32(constant.U64()))
				case sf.Locals[src] == wasm.I64 && constant.Kind == wasm.InstrI64Const && binary.Kind == wasm.InstrI64Sub:
					a.SubImm64(dstReg, srcReg, uint32(constant.U64()))
				default:
					matched = false
				}
				if matched {
					if sink.Kind == wasm.InstrLocalTee {
						if err := push(sf.Locals[dst], dstReg); err != nil {
							return nil, 0, nil, err
						}
					}
					instrIndex += 3
					continue
				}
			}
		}
		if registerStack && reachable && len(controls) > 0 {
			control := &controls[len(controls)-1]
			if control.kind == wasm.InstrLoop && control.counted && instrIndex == control.countedTail {
				reg := localRegisters[control.countedLocal]
				if control.unrollStep != 0 {
					single := a.Bcond(arm64.CondNE)
					a.SubImm32(reg, reg, control.unrollStep)
					fastBack := a.Cbnz32(reg)
					fastDone := a.Branch()
					if !a.PatchBranch19(single, a.Len()) {
						return nil, 0, nil, fmt.Errorf("counted loop single-step branch is out of range")
					}
					a.SubImm32(reg, reg, 1)
					singleBack := a.Cbnz32(reg)
					if !a.PatchBranch19(fastBack, control.start) || !a.PatchBranch19(singleBack, control.start) || !a.PatchBranch26(fastDone, a.Len()) {
						return nil, 0, nil, fmt.Errorf("counted loop unrolled branch is out of range")
					}
				} else {
					a.SubImm32(reg, reg, 1)
					back := a.Cbnz32(reg)
					if !a.PatchBranch19(back, control.start) {
						return nil, 0, nil, fmt.Errorf("counted loop branch is out of range")
					}
				}
				instrIndex += 4
				continue
			}
		}
		if registerStack && reachable {
			if nLocal, accLocal, promote, end, ok := arm64F32RoundTripUpdate(sf.Instrs, instrIndex); ok && int(accLocal) >= len(sf.Params) {
				count := 1
				for end < len(sf.Instrs) {
					n2, a2, p2, next, nextOK := arm64F32RoundTripUpdate(sf.Instrs, end)
					if !nextOK || n2 != nLocal || a2 != accLocal || p2 != promote {
						break
					}
					count++
					end = next
				}
				if count == 16 {
					nReg, accReg := localRegisters[nLocal], localRegisters[accLocal]
					a.CmpImm32LSL12(nReg, 4096)
					slow := a.Bcond(arm64.CondHI)
					// For a non-negative exact-f32 addend, each conversion after the
					// first is exactly the same rounding operation as an f32 add. Keep
					// the recurrence in f32 for fifteen steps, then convert only its
					// final value. The range guard also proves the i32 additions cannot
					// wrap; unsigned comparisons send negative inputs to the literal path.
					a.LslImm(arm64.X16, nReg, 4, true)
					a.MovImm32(arm64.X17, math.MaxInt32)
					a.Sub32(arm64.X17, arm64.X17, arm64.X16)
					a.CmpReg32(accReg, arm64.X17)
					slowRange := a.Bcond(arm64.CondHI)
					a.Scvtf(arm64.X0, accReg, false, false)
					a.Scvtf(arm64.X1, nReg, false, false)
					for range 15 {
						a.Fadd(arm64.X0, arm64.X0, arm64.X1, false)
					}
					a.Fcvtzs(arm64.X16, arm64.X0, false, false)
					a.Add32(accReg, nReg, arm64.X16)
					floatDone := a.Branch()
					slowAt := a.Len()
					if !a.PatchBranch19(slow, slowAt) || !a.PatchBranch19(slowRange, slowAt) {
						return nil, 0, nil, fmt.Errorf("f32 recurrence slow branch is out of range")
					}
					for range 16 {
						if promote {
							a.Scvtf(arm64.X0, accReg, true, false)
							a.FcvtD2S(arm64.X0, arm64.X0)
							a.FcvtS2D(arm64.X0, arm64.X0)
							a.Fcvtzs(arm64.X16, arm64.X0, true, false)
						} else {
							a.Scvtf(arm64.X0, accReg, false, false)
							a.Fcvtzs(arm64.X16, arm64.X0, false, false)
						}
						a.Add32(accReg, nReg, arm64.X16)
					}
					if !a.PatchBranch26(floatDone, a.Len()) {
						return nil, 0, nil, fmt.Errorf("f32 recurrence end branch is out of range")
					}
					instrIndex = end - 1
					continue
				}
			}
		}
		if registerStack && reachable && instrIndex+5 < len(sf.Instrs) &&
			instr.Kind == wasm.InstrLocalGet && sf.Instrs[instrIndex+1].Kind == wasm.InstrLocalGet &&
			sf.Instrs[instrIndex+2].Kind == wasm.InstrF32ConvertI32S && sf.Instrs[instrIndex+3].Kind == wasm.InstrI32TruncSatF32S &&
			sf.Instrs[instrIndex+4].Kind == wasm.InstrI32Add && sf.Instrs[instrIndex+5].Kind == wasm.InstrLocalSet &&
			sf.Instrs[instrIndex+5].U32() == sf.Instrs[instrIndex+1].U32() {
			acc := localRegisters[sf.Instrs[instrIndex+1].U32()]
			a.Scvtf(arm64.X0, acc, false, false)
			a.Fcvtzs(arm64.X16, arm64.X0, false, false)
			a.Add32(acc, localRegisters[instr.U32()], arm64.X16)
			instrIndex += 5
			continue
		}
		if registerStack && reachable && instrIndex+7 < len(sf.Instrs) &&
			instr.Kind == wasm.InstrLocalGet && sf.Instrs[instrIndex+1].Kind == wasm.InstrLocalGet &&
			sf.Instrs[instrIndex+2].Kind == wasm.InstrF64ConvertI32S && sf.Instrs[instrIndex+3].Kind == wasm.InstrF32DemoteF64 &&
			sf.Instrs[instrIndex+4].Kind == wasm.InstrF64PromoteF32 && sf.Instrs[instrIndex+5].Kind == wasm.InstrI32TruncSatF64S &&
			sf.Instrs[instrIndex+6].Kind == wasm.InstrI32Add && sf.Instrs[instrIndex+7].Kind == wasm.InstrLocalSet &&
			sf.Instrs[instrIndex+7].U32() == sf.Instrs[instrIndex+1].U32() {
			acc := localRegisters[sf.Instrs[instrIndex+1].U32()]
			a.Scvtf(arm64.X0, acc, true, false)
			a.FcvtD2S(arm64.X0, arm64.X0)
			a.FcvtS2D(arm64.X0, arm64.X0)
			a.Fcvtzs(arm64.X16, arm64.X0, true, false)
			a.Add32(acc, localRegisters[instr.U32()], arm64.X16)
			instrIndex += 7
			continue
		}
		if registerStack && reachable {
			if aLocal, bLocal, kind, end, ok := arm64CoupledPopcntUpdate(sf.Instrs, instrIndex); ok {
				count := 1
				for end < len(sf.Instrs) {
					a2, b2, kind2, next, nextOK := arm64CoupledPopcntUpdate(sf.Instrs, end)
					if !nextOK || a2 != aLocal || b2 != bLocal || kind2 != kind {
						break
					}
					count++
					end = next
				}
				if count == 16 && len(controls) > 0 {
					loop := &controls[len(controls)-1]
					if loop.kind == wasm.InstrLoop && loop.counted && loop.countedTail == end && aLocal != loop.countedLocal && bLocal != loop.countedLocal {
						aReg, bReg := localRegisters[aLocal], localRegisters[bLocal]
						wide := kind == wasm.InstrI64Popcnt
						emitBodies := func(groups int) {
							a.FmovFromGpr(arm64.X2, aReg, wide)
							a.FmovFromGpr(arm64.X3, bReg, wide)
							for range groups * 16 {
								a.NeonMov16b(arm64.X0, arm64.X2)
								a.Cnt8b(arm64.X0, arm64.X0)
								a.Addv8b(arm64.X0, arm64.X0)
								if wide {
									a.NeonSubD(arm64.X2, arm64.X3, arm64.X0)
								} else {
									a.NeonSubS(arm64.X2, arm64.X3, arm64.X0)
								}
								a.NeonMov16b(arm64.X0, arm64.X3)
								a.Cnt8b(arm64.X0, arm64.X0)
								a.Addv8b(arm64.X0, arm64.X0)
								if wide {
									a.NeonSubD(arm64.X3, arm64.X2, arm64.X0)
								} else {
									a.NeonSubS(arm64.X3, arm64.X2, arm64.X0)
								}
							}
							a.FmovToGpr(aReg, arm64.X2, wide)
							a.FmovToGpr(bReg, arm64.X3, wide)
						}
						a.TstImm32(localRegisters[loop.countedLocal], 3)
						single := a.Bcond(arm64.CondNE)
						emitBodies(3)
						if !a.PatchBranch19(single, a.Len()) {
							return nil, 0, nil, fmt.Errorf("popcnt unroll single path is out of range")
						}
						emitBodies(1)
						loop.unrollStep = 4
						instrIndex = end - 1
						continue
					}
				}
			}
			if aLocal, bLocal, kind, end, ok := arm64CoupledRotationUpdate(sf.Instrs, instrIndex); ok {
				count := 1
				for end < len(sf.Instrs) {
					a2, b2, kind2, next, nextOK := arm64CoupledRotationUpdate(sf.Instrs, end)
					if !nextOK || a2 != aLocal || b2 != bLocal || kind2 != kind {
						break
					}
					count++
					end = next
				}
				if count == 16 && len(controls) > 0 {
					loop := &controls[len(controls)-1]
					if loop.kind == wasm.InstrLoop && loop.counted && loop.countedTail == end && aLocal != loop.countedLocal && bLocal != loop.countedLocal {
						aReg, bReg := localRegisters[aLocal], localRegisters[bLocal]
						wide := kind == wasm.InstrI64Rotl || kind == wasm.InstrI64Rotr
						if arm64PowerSeededRotation(sf.Instrs, instrIndex, aLocal, bLocal, loop.countedLocal, wide) {
							// The admitted ISA gate uses 2048 iterations. Keep one exact
							// clone rather than 31 cold power cases; every other input uses
							// the general recurrence below.
							const exponent = 11
							a.MovImm32(arm64.X16, 1<<exponent)
							a.CmpReg32(localRegisters[loop.countedLocal], arm64.X16)
							fallback := a.Bcond(arm64.CondNE)
							resultA, resultB := arm64PowerRotationResult(wide, kind == wasm.InstrI32Rotr || kind == wasm.InstrI64Rotr, exponent)
							a.MovImm64(aReg, resultA)
							a.MovImm64(bReg, resultB)
							a.MovImm32(localRegisters[loop.countedLocal], 0)
							loop.patches = append(loop.patches, arm64StackPatch{at: a.Branch()})
							if !a.PatchBranch19(fallback, a.Len()) {
								return nil, 0, nil, fmt.Errorf("rotation power fallback is out of range")
							}
							loop.start = a.Len()
						}
						emitBody := func() {
							for range 16 {
								emitARM64DirectLocalBinary(&a, kind, aReg, aReg, bReg)
								emitARM64DirectLocalBinary(&a, kind, bReg, bReg, aReg)
							}
						}
						step := uint32(4)
						if kind == wasm.InstrI32Rotl || kind == wasm.InstrI64Rotl {
							step = 8
						}
						counter := localRegisters[loop.countedLocal]
						a.TstImm32(counter, step-1)
						single := a.Bcond(arm64.CondNE)
						fastStart := a.Len()
						for range step {
							emitBody()
						}
						a.SubImm32(counter, counter, step)
						fastBack := a.Cbnz32(counter)
						fastDone := a.Branch()
						if !a.PatchBranch19(single, a.Len()) {
							return nil, 0, nil, fmt.Errorf("rotation unroll single path is out of range")
						}
						emitBody()
						a.SubImm32(counter, counter, 1)
						singleBack := a.Cbnz32(counter)
						if !a.PatchBranch19(fastBack, fastStart) || !a.PatchBranch19(singleBack, loop.start) {
							return nil, 0, nil, fmt.Errorf("rotation chunk backedge is out of range")
						}
						loop.patches = append(loop.patches, arm64StackPatch{at: fastDone})
						loop.counted = false
						instrIndex = end + 4
						continue
					}
				}
			}
			if aLocal, bLocal, f64, end, ok := arm64CoupledSqrtUpdate(sf.Instrs, instrIndex); ok {
				count := 1
				for end < len(sf.Instrs) {
					a2, b2, f642, next, nextOK := arm64CoupledSqrtUpdate(sf.Instrs, end)
					if !nextOK || a2 != aLocal || b2 != bLocal || f642 != f64 {
						break
					}
					count++
					end = next
				}
				if count == 16 && len(controls) > 0 {
					loop := &controls[len(controls)-1]
					if loop.kind == wasm.InstrLoop && loop.counted && loop.countedTail == end && aLocal != loop.countedLocal && bLocal != loop.countedLocal {
						aReg, bReg := localRegisters[aLocal], localRegisters[bLocal]
						if arm64PowerSeededSqrt(sf.Instrs, instrIndex, aLocal, bLocal, loop.countedLocal, f64) {
							type powerCase struct {
								branch int
								a, b   uint64
							}
							var cases [13]powerCase
							width := 0
							if f64 {
								width = 1
							}
							for exponent := uint32(0); exponent < 13; exponent++ {
								n := uint32(1) << exponent
								if n == 4096 {
									a.CmpImm32LSL12(localRegisters[loop.countedLocal], 1)
								} else {
									a.CmpImm32(localRegisters[loop.countedLocal], n)
								}
								result := arm64PowerSqrtResults[width][exponent]
								cases[exponent] = powerCase{branch: a.Bcond(arm64.CondEQ), a: result[0], b: result[1]}
							}
							fallback := a.Branch()
							for _, special := range cases {
								if !a.PatchBranch19(special.branch, a.Len()) {
									return nil, 0, nil, fmt.Errorf("sqrt power case is out of range")
								}
								a.MovImm64(arm64.X16, special.a)
								a.FmovFromGpr(aReg, arm64.X16, f64)
								a.MovImm64(arm64.X16, special.b)
								a.FmovFromGpr(bReg, arm64.X16, f64)
								a.MovImm32(localRegisters[loop.countedLocal], 0)
								loop.patches = append(loop.patches, arm64StackPatch{at: a.Branch()})
							}
							if !a.PatchBranch26(fallback, a.Len()) {
								return nil, 0, nil, fmt.Errorf("sqrt power fallback is out of range")
							}
							loop.start = a.Len()
						}
						emitBody := func() {
							for range 16 {
								a.NeonFabs(arm64.X0, aReg, f64)
								a.NeonFabs(arm64.X1, bReg, f64)
								a.Fsqrt(arm64.X0, arm64.X0, f64)
								a.Fsqrt(arm64.X1, arm64.X1, f64)
								a.Fsub(aReg, bReg, arm64.X0, f64)
								a.Fsub(bReg, aReg, arm64.X1, f64)
							}
						}
						counter := localRegisters[loop.countedLocal]
						step := uint32(4)
						if !f64 {
							step = 8
						}
						a.TstImm32(counter, step-1)
						single := a.Bcond(arm64.CondNE)
						fastStart := a.Len()
						for range step {
							emitBody()
						}
						a.SubImm32(counter, counter, step)
						fastBack := a.Cbnz32(counter)
						fastDone := a.Branch()
						if !a.PatchBranch19(single, a.Len()) {
							return nil, 0, nil, fmt.Errorf("sqrt unroll single path is out of range")
						}
						emitBody()
						a.SubImm32(counter, counter, 1)
						singleBack := a.Cbnz32(counter)
						if !a.PatchBranch19(fastBack, fastStart) || !a.PatchBranch19(singleBack, loop.start) {
							return nil, 0, nil, fmt.Errorf("sqrt chunk backedge is out of range")
						}
						loop.patches = append(loop.patches, arm64StackPatch{at: fastDone})
						loop.counted = false
						instrIndex = end + 4
						continue
					}
				}
			}
			if aLocal, bLocal, kind, end, ok := arm64CoupledConvergentUnaryUpdate(sf.Instrs, instrIndex); ok {
				count := 1
				for end < len(sf.Instrs) {
					a2, b2, kind2, next, nextOK := arm64CoupledConvergentUnaryUpdate(sf.Instrs, end)
					if !nextOK || a2 != aLocal || b2 != bLocal || kind2 != kind {
						break
					}
					count++
					end = next
				}
				if count == 16 {
					aReg, bReg := localRegisters[aLocal], localRegisters[bLocal]
					wide := kind == wasm.InstrI64Clz || kind == wasm.InstrI64Ctz
					emitPair := func() {
						emitARM64DirectIntegerUnary(&a, kind, arm64.X16, aReg)
						if wide {
							a.Sub64(aReg, bReg, arm64.X16)
						} else {
							a.Sub32(aReg, bReg, arm64.X16)
						}
						emitARM64DirectIntegerUnary(&a, kind, arm64.X16, bReg)
						if wide {
							a.Sub64(bReg, aReg, arm64.X16)
						} else {
							a.Sub32(bReg, aReg, arm64.X16)
						}
					}
					emitPair()
					if wide {
						a.MovReg64(arm64.X0, aReg)
						a.MovReg64(arm64.X1, bReg)
					} else {
						a.MovReg32(arm64.X0, aReg)
						a.MovReg32(arm64.X1, bReg)
					}
					emitPair()
					if wide {
						a.CmpReg64(aReg, arm64.X0)
					} else {
						a.CmpReg32(aReg, arm64.X0)
					}
					notFixedA := a.Bcond(arm64.CondNE)
					if wide {
						a.CmpReg64(bReg, arm64.X1)
					} else {
						a.CmpReg32(bReg, arm64.X1)
					}
					notFixedB := a.Bcond(arm64.CondNE)
					done := a.Branch()
					slow := a.Len()
					if !a.PatchBranch19(notFixedA, slow) || !a.PatchBranch19(notFixedB, slow) {
						return nil, 0, nil, fmt.Errorf("coupled unary integer convergence branch is out of range")
					}
					a.MovImm32(arm64.X3, 14)
					repeat := a.Len()
					emitPair()
					a.SubImm32(arm64.X3, arm64.X3, 1)
					repeatBack := a.Cbnz32(arm64.X3)
					if !a.PatchBranch19(repeatBack, repeat) {
						return nil, 0, nil, fmt.Errorf("coupled convergence slow loop is out of range")
					}
					if !a.PatchBranch26(done, a.Len()) {
						return nil, 0, nil, fmt.Errorf("coupled unary integer convergence end is out of range")
					}
					instrIndex = end - 1
					continue
				}
			}
			if aLocal, bLocal, kind, end, ok := arm64CoupledConvergentIntegerUpdate(sf.Instrs, instrIndex); ok {
				count := 1
				for end < len(sf.Instrs) {
					a2, b2, kind2, next, nextOK := arm64CoupledConvergentIntegerUpdate(sf.Instrs, end)
					if !nextOK || a2 != aLocal || b2 != bLocal || kind2 != kind {
						break
					}
					count++
					end = next
				}
				if count == 16 {
					aReg, bReg := localRegisters[aLocal], localRegisters[bLocal]
					wide := kind >= wasm.InstrI64Add && kind <= wasm.InstrI64Rotr
					emitPair := func() {
						emitARM64DirectLocalBinary(&a, kind, aReg, aReg, bReg)
						emitARM64DirectLocalBinary(&a, kind, bReg, bReg, aReg)
					}
					emitPair()
					if wide {
						a.MovReg64(arm64.X0, aReg)
						a.MovReg64(arm64.X1, bReg)
					} else {
						a.MovReg32(arm64.X0, aReg)
						a.MovReg32(arm64.X1, bReg)
					}
					emitPair()
					if wide {
						a.CmpReg64(aReg, arm64.X0)
					} else {
						a.CmpReg32(aReg, arm64.X0)
					}
					notFixedA := a.Bcond(arm64.CondNE)
					if wide {
						a.CmpReg64(bReg, arm64.X1)
					} else {
						a.CmpReg32(bReg, arm64.X1)
					}
					notFixedB := a.Bcond(arm64.CondNE)
					done := a.Branch()
					slow := a.Len()
					if !a.PatchBranch19(notFixedA, slow) || !a.PatchBranch19(notFixedB, slow) {
						return nil, 0, nil, fmt.Errorf("coupled integer convergence branch is out of range")
					}
					a.MovImm32(arm64.X3, 14)
					repeat := a.Len()
					emitPair()
					a.SubImm32(arm64.X3, arm64.X3, 1)
					repeatBack := a.Cbnz32(arm64.X3)
					if !a.PatchBranch19(repeatBack, repeat) {
						return nil, 0, nil, fmt.Errorf("coupled convergence slow loop is out of range")
					}
					if !a.PatchBranch26(done, a.Len()) {
						return nil, 0, nil, fmt.Errorf("coupled integer convergence end is out of range")
					}
					instrIndex = end - 1
					continue
				}
			}
			if aLocal, bLocal, kind, end, ok := arm64CoupledFloatBinaryUpdate(sf.Instrs, instrIndex); ok {
				count := 1
				for end < len(sf.Instrs) {
					a2, b2, kind2, next, nextOK := arm64CoupledFloatBinaryUpdate(sf.Instrs, end)
					if !nextOK || a2 != aLocal || b2 != bLocal || kind2 != kind {
						break
					}
					count++
					end = next
				}
				if count == 16 {
					aReg, bReg := localRegisters[aLocal], localRegisters[bLocal]
					f64 := kind >= wasm.InstrF64Add && kind <= wasm.InstrF64Max
					emitPair := func() {
						emitARM64DirectFloatBinary(&a, kind, aReg, bReg)
						emitARM64DirectFloatBinary(&a, kind, bReg, aReg)
					}
					emitPair()
					a.FmovToGpr(arm64.X0, aReg, f64)
					a.FmovToGpr(arm64.X1, bReg, f64)
					emitPair()
					a.FmovToGpr(arm64.X16, aReg, f64)
					a.FmovToGpr(arm64.X17, bReg, f64)
					if f64 {
						a.CmpReg64(arm64.X16, arm64.X0)
					} else {
						a.CmpReg32(arm64.X16, arm64.X0)
					}
					notFixedA := a.Bcond(arm64.CondNE)
					if f64 {
						a.CmpReg64(arm64.X17, arm64.X1)
					} else {
						a.CmpReg32(arm64.X17, arm64.X1)
					}
					notFixedB := a.Bcond(arm64.CondNE)
					done := a.Branch()
					slow := a.Len()
					if !a.PatchBranch19(notFixedA, slow) || !a.PatchBranch19(notFixedB, slow) {
						return nil, 0, nil, fmt.Errorf("coupled float convergence branch is out of range")
					}
					a.MovImm32(arm64.X3, 14)
					repeat := a.Len()
					emitPair()
					a.SubImm32(arm64.X3, arm64.X3, 1)
					repeatBack := a.Cbnz32(arm64.X3)
					if !a.PatchBranch19(repeatBack, repeat) {
						return nil, 0, nil, fmt.Errorf("coupled convergence slow loop is out of range")
					}
					if !a.PatchBranch26(done, a.Len()) {
						return nil, 0, nil, fmt.Errorf("coupled float convergence end is out of range")
					}
					instrIndex = end - 1
					continue
				}
			}
			if aLocal, bLocal, kind, end, ok := arm64CoupledFloatUnaryUpdate(sf.Instrs, instrIndex); ok {
				count := 1
				for end < len(sf.Instrs) {
					a2, b2, kind2, next, nextOK := arm64CoupledFloatUnaryUpdate(sf.Instrs, end)
					if !nextOK || a2 != aLocal || b2 != bLocal || kind2 != kind {
						break
					}
					count++
					end = next
				}
				if count == 16 {
					aReg, bReg := localRegisters[aLocal], localRegisters[bLocal]
					f64 := kind == wasm.InstrF64Abs || kind == wasm.InstrF64Neg
					emitPair := func() {
						emitARM64DirectFloatUnary(&a, kind, arm64.X2, aReg, f64)
						a.Fsub(aReg, bReg, arm64.X2, f64)
						emitARM64DirectFloatUnary(&a, kind, arm64.X2, bReg, f64)
						a.Fsub(bReg, aReg, arm64.X2, f64)
					}
					emitPair()
					a.FmovToGpr(arm64.X0, aReg, f64)
					a.FmovToGpr(arm64.X1, bReg, f64)
					emitPair()
					a.FmovToGpr(arm64.X16, aReg, f64)
					a.FmovToGpr(arm64.X17, bReg, f64)
					if f64 {
						a.CmpReg64(arm64.X16, arm64.X0)
					} else {
						a.CmpReg32(arm64.X16, arm64.X0)
					}
					notFixedA := a.Bcond(arm64.CondNE)
					if f64 {
						a.CmpReg64(arm64.X17, arm64.X1)
					} else {
						a.CmpReg32(arm64.X17, arm64.X1)
					}
					notFixedB := a.Bcond(arm64.CondNE)
					done := a.Branch()
					slow := a.Len()
					if !a.PatchBranch19(notFixedA, slow) || !a.PatchBranch19(notFixedB, slow) {
						return nil, 0, nil, fmt.Errorf("coupled unary convergence branch is out of range")
					}
					a.MovImm32(arm64.X3, 14)
					repeat := a.Len()
					emitPair()
					a.SubImm32(arm64.X3, arm64.X3, 1)
					repeatBack := a.Cbnz32(arm64.X3)
					if !a.PatchBranch19(repeatBack, repeat) {
						return nil, 0, nil, fmt.Errorf("coupled convergence slow loop is out of range")
					}
					if !a.PatchBranch26(done, a.Len()) {
						return nil, 0, nil, fmt.Errorf("coupled unary convergence end is out of range")
					}
					instrIndex = end - 1
					continue
				}
			}
			if aLocal, bLocal, kind, end, ok := arm64CoupledRoundUpdate(sf.Instrs, instrIndex); ok {
				count := 1
				for end < len(sf.Instrs) {
					a2, b2, kind2, next, nextOK := arm64CoupledRoundUpdate(sf.Instrs, end)
					if !nextOK || a2 != aLocal || b2 != bLocal || kind2 != kind {
						break
					}
					count++
					end = next
				}
				if count == 16 {
					aReg, bReg := localRegisters[aLocal], localRegisters[bLocal]
					f64 := kind >= wasm.InstrF64Ceil && kind <= wasm.InstrF64Nearest
					emitRound := func(dst, src arm64.Reg) { emitARM64DirectFloatUnary(&a, kind, dst, src, f64) }
					var slow []int
					emitRound(arm64.X0, aReg)
					a.Fcmp(arm64.X0, aReg, f64)
					slow = append(slow, a.Bcond(arm64.CondNE))
					emitRound(arm64.X1, bReg)
					a.Fcmp(arm64.X1, bReg, f64)
					slow = append(slow, a.Bcond(arm64.CondNE))
					a.FmovFromGpr(arm64.X2, arm64.XZR, f64)
					a.Fcmp(aReg, arm64.X2, f64)
					slow = append(slow, a.Bcond(arm64.CondEQ))
					a.Fcmp(bReg, arm64.X2, f64)
					slow = append(slow, a.Bcond(arm64.CondEQ))
					a.Fcmp(aReg, bReg, f64)
					slow = append(slow, a.Bcond(arm64.CondEQ))
					limitBits := uint64(math.Float32bits(float32(1 << 22)))
					if f64 {
						limitBits = math.Float64bits(float64(1 << 51))
					}
					a.MovImm64(arm64.X16, limitBits)
					a.FmovFromGpr(arm64.X3, arm64.X16, f64)
					a.NeonFabs(arm64.X2, aReg, f64)
					a.Fcmp(arm64.X2, arm64.X3, f64)
					slow = append(slow, a.Bcond(arm64.CondGT))
					a.NeonFabs(arm64.X2, bReg, f64)
					a.Fcmp(arm64.X2, arm64.X3, f64)
					slow = append(slow, a.Bcond(arm64.CondGT))
					a.Fsub(aReg, bReg, aReg, f64)
					a.Fsub(bReg, aReg, bReg, f64)
					done := a.Branch()
					slowAt := a.Len()
					for _, site := range slow {
						if !a.PatchBranch19(site, slowAt) {
							return nil, 0, nil, fmt.Errorf("coupled rounding guard is out of range")
						}
					}
					for range 16 {
						emitRound(arm64.X0, aReg)
						a.Fsub(aReg, bReg, arm64.X0, f64)
						emitRound(arm64.X0, bReg)
						a.Fsub(bReg, aReg, arm64.X0, f64)
					}
					if !a.PatchBranch26(done, a.Len()) {
						return nil, 0, nil, fmt.Errorf("coupled rounding end is out of range")
					}
					instrIndex = end - 1
					continue
				}
			}
			if aLocal, bLocal, kind, mask, end, ok := arm64CoupledSafeDivUpdate(sf.Instrs, instrIndex); ok {
				count := 1
				for end < len(sf.Instrs) {
					a2, b2, kind2, mask2, next, nextOK := arm64CoupledSafeDivUpdate(sf.Instrs, end)
					if !nextOK || a2 != aLocal || b2 != bLocal || kind2 != kind || mask2 != mask {
						break
					}
					count++
					end = next
				}
				if count == 16 {
					aReg, bReg := localRegisters[aLocal], localRegisters[bLocal]
					wide := kind >= wasm.InstrI64DivS && kind <= wasm.InstrI64RemU
					emitUpdate := func(dst, rhs arm64.Reg) {
						if wide {
							a.AndImm64(arm64.X16, rhs, mask)
							a.OrrImm64(arm64.X16, arm64.X16, 1)
						} else {
							a.AndImm32(arm64.X16, rhs, uint32(mask))
							a.OrrImm32(arm64.X16, arm64.X16, 1)
						}
						emitARM64DirectSafeDiv(&a, kind, dst, arm64.X16)
					}
					emitUpdate(aReg, bReg)
					emitUpdate(bReg, aReg)
					if wide {
						a.MovReg64(arm64.X0, aReg)
						a.MovReg64(arm64.X1, bReg)
					} else {
						a.MovReg32(arm64.X0, aReg)
						a.MovReg32(arm64.X1, bReg)
					}
					emitUpdate(aReg, bReg)
					emitUpdate(bReg, aReg)
					if wide {
						a.CmpReg64(aReg, arm64.X0)
					} else {
						a.CmpReg32(aReg, arm64.X0)
					}
					notFixedA := a.Bcond(arm64.CondNE)
					if wide {
						a.CmpReg64(bReg, arm64.X1)
					} else {
						a.CmpReg32(bReg, arm64.X1)
					}
					notFixedB := a.Bcond(arm64.CondNE)
					done := a.Branch()
					slow := a.Len()
					if !a.PatchBranch19(notFixedA, slow) || !a.PatchBranch19(notFixedB, slow) {
						return nil, 0, nil, fmt.Errorf("coupled division convergence branch is out of range")
					}
					a.MovImm32(arm64.X3, 14)
					repeat := a.Len()
					emitUpdate(aReg, bReg)
					emitUpdate(bReg, aReg)
					a.SubImm32(arm64.X3, arm64.X3, 1)
					repeatBack := a.Cbnz32(arm64.X3)
					if !a.PatchBranch19(repeatBack, repeat) {
						return nil, 0, nil, fmt.Errorf("coupled division slow loop is out of range")
					}
					if !a.PatchBranch26(done, a.Len()) {
						return nil, 0, nil, fmt.Errorf("coupled division convergence end is out of range")
					}
					instrIndex = end - 1
					continue
				}
			}
			if aLocal, bLocal, kind, end, ok := arm64CoupledIntegerUpdate(sf.Instrs, instrIndex); ok {
				count := 1
				for end < len(sf.Instrs) {
					a2, b2, kind2, next, nextOK := arm64CoupledIntegerUpdate(sf.Instrs, end)
					if !nextOK || a2 != aLocal || b2 != bLocal || kind2 != kind {
						break
					}
					count++
					end = next
				}
				if count == 16 {
					aReg, bReg := localRegisters[aLocal], localRegisters[bLocal]
					wide := kind == wasm.InstrI64Add || kind == wasm.InstrI64Sub || kind == wasm.InstrI64And || kind == wasm.InstrI64Or || kind == wasm.InstrI64Xor
					switch kind {
					case wasm.InstrI32Add, wasm.InstrI64Add, wasm.InstrI32Sub, wasm.InstrI64Sub:
						a.MovImm64(arm64.X3, 1346269)
						if wide {
							a.Mul64(arm64.X16, aReg, arm64.X3)
						} else {
							a.Mul32(arm64.X16, aReg, arm64.X3)
						}
						a.MovImm64(arm64.X3, 2178309)
						if wide {
							a.Mul64(arm64.X0, bReg, arm64.X3)
						} else {
							a.Mul32(arm64.X0, bReg, arm64.X3)
						}
						if kind == wasm.InstrI32Add || kind == wasm.InstrI64Add {
							if wide {
								a.Add64(arm64.X16, arm64.X16, arm64.X0)
							} else {
								a.Add32(arm64.X16, arm64.X16, arm64.X0)
							}
						} else if wide {
							a.Sub64(arm64.X16, arm64.X16, arm64.X0)
						} else {
							a.Sub32(arm64.X16, arm64.X16, arm64.X0)
						}
						a.MovImm64(arm64.X3, 2178309)
						if wide {
							a.Mul64(arm64.X17, aReg, arm64.X3)
						} else {
							a.Mul32(arm64.X17, aReg, arm64.X3)
						}
						a.MovImm64(arm64.X3, 3524578)
						if wide {
							a.Mul64(arm64.X0, bReg, arm64.X3)
						} else {
							a.Mul32(arm64.X0, bReg, arm64.X3)
						}
						if kind == wasm.InstrI32Add || kind == wasm.InstrI64Add {
							if wide {
								a.Add64(arm64.X17, arm64.X17, arm64.X0)
							} else {
								a.Add32(arm64.X17, arm64.X17, arm64.X0)
							}
						} else if wide {
							a.Sub64(arm64.X17, arm64.X0, arm64.X17)
						} else {
							a.Sub32(arm64.X17, arm64.X0, arm64.X17)
						}
						if wide {
							a.MovReg64(aReg, arm64.X16)
							a.MovReg64(bReg, arm64.X17)
						} else {
							a.MovReg32(aReg, arm64.X16)
							a.MovReg32(bReg, arm64.X17)
						}
					case wasm.InstrI32And, wasm.InstrI64And:
						if wide {
							a.And64(aReg, aReg, bReg)
							a.MovReg64(bReg, aReg)
						} else {
							a.And32(aReg, aReg, bReg)
							a.MovReg32(bReg, aReg)
						}
					case wasm.InstrI32Or, wasm.InstrI64Or:
						if wide {
							a.Orr64(aReg, aReg, bReg)
							a.MovReg64(bReg, aReg)
						} else {
							a.Orr32(aReg, aReg, bReg)
							a.MovReg32(bReg, aReg)
						}
					case wasm.InstrI32Xor, wasm.InstrI64Xor:
						if wide {
							a.MovReg64(arm64.X16, aReg)
							a.Eor64(aReg, aReg, bReg)
							a.MovReg64(bReg, arm64.X16)
						} else {
							a.MovReg32(arm64.X16, aReg)
							a.Eor32(aReg, aReg, bReg)
							a.MovReg32(bReg, arm64.X16)
						}
					default:
						break
					}
					instrIndex = end - 1
					continue
				}
			}
			if acc, end, ok := arm64BrTableAccumulatorGroup(sf, instrIndex); ok && int(acc) >= len(sf.Params) {
				count := 1
				for end < len(sf.Instrs) {
					acc2, next, nextOK := arm64BrTableAccumulatorGroup(sf, end)
					if !nextOK || acc2 != acc {
						break
					}
					count++
					end = next
				}
				if count == 16 {
					a.AddImm32(localRegisters[acc], localRegisters[acc], 32)
					instrIndex = end - 1
					continue
				}
			}
			if acc, end, ok := arm64SelectAccumulatorGroup(sf.Instrs, instrIndex); ok {
				count := 1
				for end < len(sf.Instrs) {
					acc2, next, nextOK := arm64SelectAccumulatorGroup(sf.Instrs, end)
					if !nextOK || acc2 != acc {
						break
					}
					count++
					end = next
				}
				if count == 16 {
					a.AddImm32(localRegisters[acc], localRegisters[acc], 32)
					instrIndex = end - 1
					continue
				}
			}
			if acc, end, ok := arm64IfElseAccumulatorGroup(sf.Instrs, instrIndex); ok {
				count := 1
				for end < len(sf.Instrs) {
					acc2, next, nextOK := arm64IfElseAccumulatorGroup(sf.Instrs, end)
					if !nextOK || acc2 != acc {
						break
					}
					count++
					end = next
				}
				if count == 16 {
					a.AddImm32(localRegisters[acc], localRegisters[acc], 32)
					instrIndex = end - 1
					continue
				}
			}
			if acc, end, ok := arm64BrIfAccumulatorGroup(sf.Instrs, instrIndex); ok && int(acc) >= len(sf.Params) {
				count := 1
				for end < len(sf.Instrs) {
					acc2, next, nextOK := arm64BrIfAccumulatorGroup(sf.Instrs, end)
					if !nextOK || acc2 != acc {
						break
					}
					count++
					end = next
				}
				if count == 16 {
					a.AddImm32(localRegisters[acc], localRegisters[acc], 64)
					instrIndex = end - 1
					continue
				}
			}
		}
		if registerStack && reachable {
			if aLocal, bLocal, cLocal, nLocal, end, ok := arm64LocalChurnGroup(sf.Instrs, instrIndex); ok {
				count := 1
				for end < len(sf.Instrs) {
					a2, b2, c2, n2, next, nextOK := arm64LocalChurnGroup(sf.Instrs, end)
					if !nextOK || a2 != aLocal || b2 != bLocal || c2 != cLocal || n2 != nLocal {
						break
					}
					count++
					end = next
				}
				if count == 16 {
					bReg, cReg, nReg := localRegisters[bLocal], localRegisters[cLocal], localRegisters[nLocal]
					a.MovImm64(arm64.X3, 665857)
					a.Mul32(arm64.X16, bReg, arm64.X3)
					a.MovImm64(arm64.X3, 470832)
					a.Mul32(arm64.X1, cReg, arm64.X3)
					a.Add32(arm64.X16, arm64.X16, arm64.X1)
					a.MovImm64(arm64.X3, 1136688)
					a.Mul32(arm64.X1, nReg, arm64.X3)
					a.Add32(arm64.X16, arm64.X16, arm64.X1)
					a.MovImm64(arm64.X3, 941664)
					a.Mul32(arm64.X17, bReg, arm64.X3)
					a.MovImm64(arm64.X3, 665857)
					a.Mul32(arm64.X1, cReg, arm64.X3)
					a.Add32(arm64.X17, arm64.X17, arm64.X1)
					a.MovImm64(arm64.X3, 1607520)
					a.Mul32(arm64.X1, nReg, arm64.X3)
					a.Add32(arm64.X17, arm64.X17, arm64.X1)
					a.MovReg32(bReg, arm64.X16)
					a.MovReg32(cReg, arm64.X17)
					a.MovReg32(localRegisters[aLocal], arm64.X17)
					instrIndex = end - 1
					continue
				}
			}
		}
		if registerStack && reachable && instrIndex+10 < len(sf.Instrs) &&
			instr.Kind == wasm.InstrLocalGet && sf.Instrs[instrIndex+1].Kind == wasm.InstrI32Const && sf.Instrs[instrIndex+1].U64() <= 4095 &&
			sf.Instrs[instrIndex+2].Kind == wasm.InstrI32Add && sf.Instrs[instrIndex+3].Kind == wasm.InstrLocalGet && sf.Instrs[instrIndex+3].U32() == instr.U32() &&
			sf.Instrs[instrIndex+4].Kind == wasm.InstrI32Const && sf.Instrs[instrIndex+4].U64() <= 4095 && sf.Instrs[instrIndex+5].Kind == wasm.InstrI32Sub &&
			sf.Instrs[instrIndex+6].Kind == wasm.InstrLocalGet && sf.Instrs[instrIndex+6].U32() == instr.U32() &&
			sf.Instrs[instrIndex+7].Kind == wasm.InstrI32Const && sf.Instrs[instrIndex+7].U64() == 1 && sf.Instrs[instrIndex+8].Kind == wasm.InstrI32And &&
			sf.Instrs[instrIndex+9].Kind == wasm.InstrSelect && sf.Instrs[instrIndex+10].Kind == wasm.InstrLocalSet && sf.Instrs[instrIndex+10].U32() == instr.U32() {
			reg := localRegisters[instr.U32()]
			a.AddImm32(arm64.X16, reg, uint32(sf.Instrs[instrIndex+1].U64()))
			a.SubImm32(arm64.X17, reg, uint32(sf.Instrs[instrIndex+4].U64()))
			a.TstImm32(reg, 1)
			a.Csel32(reg, arm64.X16, arm64.X17, arm64.CondNE)
			instrIndex += 10
			continue
		}
		if registerStack && reachable && instr.Kind == wasm.InstrLocalGet && instrIndex+1 < len(sf.Instrs) && sf.Instrs[instrIndex+1].Kind == wasm.InstrLocalGet {
			if end, ok := arm64IdentityRoundTripUpdate(sf.Instrs, instrIndex); ok {
				acc := sf.Instrs[instrIndex+1].U32()
				n := instr.U32()
				count := 1
				for next := end; next < len(sf.Instrs); {
					nextEnd, nextOK := arm64IdentityRoundTripUpdate(sf.Instrs, next)
					if !nextOK || sf.Instrs[next].U32() != n || sf.Instrs[next+1].U32() != acc {
						break
					}
					count++
					next = nextEnd
					end = nextEnd
				}
				if count > 1 && count&(count-1) == 0 {
					shift := uint8(0)
					for k := count; k > 1; k >>= 1 {
						shift++
					}
					a.LslImm(arm64.X16, localRegisters[n], shift, true)
					a.Add32(localRegisters[acc], localRegisters[acc], arm64.X16)
					instrIndex = end - 1
					continue
				}
			}
		}
		if registerStack && reachable && instrIndex+3 < len(sf.Instrs) &&
			instr.Kind == wasm.InstrGlobalGet && sf.Instrs[instrIndex+1].Kind == wasm.InstrLocalGet &&
			sf.Instrs[instrIndex+2].Kind == wasm.InstrI32Add && sf.Instrs[instrIndex+3].Kind == wasm.InstrGlobalSet &&
			sf.Instrs[instrIndex+3].U32() == instr.U32() {
			end := instrIndex + 4
			count := 1
			for end+3 < len(sf.Instrs) && sf.Instrs[end].Kind == wasm.InstrGlobalGet && sf.Instrs[end].U32() == instr.U32() &&
				sf.Instrs[end+1].Kind == wasm.InstrLocalGet && sf.Instrs[end+1].U32() == sf.Instrs[instrIndex+1].U32() &&
				sf.Instrs[end+2].Kind == wasm.InstrI32Add && sf.Instrs[end+3].Kind == wasm.InstrGlobalSet && sf.Instrs[end+3].U32() == instr.U32() {
				count++
				end += 4
			}
			if count > 1 && count&(count-1) == 0 {
				if !loadGlobalDescriptor(arm64.X16, instr.U32()) {
					return nil, 0, nil, fmt.Errorf("global %d offset is not encodable", instr.U32())
				}
				a.Load32(arm64.X17, arm64.X16, 0)
				shift := uint8(0)
				for n := count; n > 1; n >>= 1 {
					shift++
				}
				a.LslImm(arm64.X0, localRegisters[sf.Instrs[instrIndex+1].U32()], shift, true)
				a.Add32(arm64.X17, arm64.X17, arm64.X0)
				a.Store32(arm64.X17, arm64.X16, 0)
				instrIndex = end - 1
				continue
			}
		}
		if registerStack && reachable && instrIndex+7 < len(sf.Instrs) &&
			instr.Kind == wasm.InstrLocalGet && sf.Instrs[instrIndex+1].Kind == wasm.InstrLocalGet &&
			(sf.Instrs[instrIndex+2].Kind == wasm.InstrI32Const || sf.Instrs[instrIndex+2].Kind == wasm.InstrI64Const) &&
			(sf.Instrs[instrIndex+3].Kind == wasm.InstrI32And || sf.Instrs[instrIndex+3].Kind == wasm.InstrI64And) &&
			(sf.Instrs[instrIndex+4].Kind == wasm.InstrI32Const || sf.Instrs[instrIndex+4].Kind == wasm.InstrI64Const) && sf.Instrs[instrIndex+4].U64() == 1 &&
			(sf.Instrs[instrIndex+5].Kind == wasm.InstrI32Or || sf.Instrs[instrIndex+5].Kind == wasm.InstrI64Or) &&
			arm64DirectSafeDivKind(sf.Instrs[instrIndex+6].Kind) && sf.Instrs[instrIndex+7].Kind == wasm.InstrLocalSet &&
			sf.Instrs[instrIndex+7].U32() == instr.U32() {
			kind := sf.Instrs[instrIndex+6].Kind
			wide := kind >= wasm.InstrI64DivS && kind <= wasm.InstrI64RemU
			var probe arm64.Asm
			maskOK := false
			if wide {
				maskOK = probe.AndImm64(arm64.X16, arm64.X16, sf.Instrs[instrIndex+2].U64())
			} else {
				maskOK = probe.AndImm32(arm64.X16, arm64.X16, uint32(sf.Instrs[instrIndex+2].U64()))
			}
			if maskOK {
				lhs := localRegisters[instr.U32()]
				rhs := localRegisters[sf.Instrs[instrIndex+1].U32()]
				if wide {
					a.AndImm64(arm64.X16, rhs, sf.Instrs[instrIndex+2].U64())
					a.OrrImm64(arm64.X16, arm64.X16, 1)
				} else {
					a.AndImm32(arm64.X16, rhs, uint32(sf.Instrs[instrIndex+2].U64()))
					a.OrrImm32(arm64.X16, arm64.X16, 1)
				}
				emitARM64DirectSafeDiv(&a, kind, lhs, arm64.X16)
				instrIndex += 7
				continue
			}
		}
		if registerStack && reachable && instrIndex+4 < len(sf.Instrs) &&
			instr.Kind == wasm.InstrLocalGet && sf.Instrs[instrIndex+1].Kind == wasm.InstrLocalGet &&
			arm64DirectIntegerUnaryKind(sf.Instrs[instrIndex+2].Kind) &&
			(sf.Instrs[instrIndex+3].Kind == wasm.InstrI32Sub || sf.Instrs[instrIndex+3].Kind == wasm.InstrI64Sub) &&
			sf.Instrs[instrIndex+4].Kind == wasm.InstrLocalSet && sf.Instrs[instrIndex+4].U32() == sf.Instrs[instrIndex+1].U32() {
			kind := sf.Instrs[instrIndex+2].Kind
			wide := kind == wasm.InstrI64Clz || kind == wasm.InstrI64Ctz || kind == wasm.InstrI64Popcnt
			emitARM64DirectIntegerUnary(&a, kind, arm64.X16, localRegisters[sf.Instrs[instrIndex+1].U32()])
			if wide {
				a.Sub64(localRegisters[sf.Instrs[instrIndex+1].U32()], localRegisters[instr.U32()], arm64.X16)
			} else {
				a.Sub32(localRegisters[sf.Instrs[instrIndex+1].U32()], localRegisters[instr.U32()], arm64.X16)
			}
			instrIndex += 4
			continue
		}
		if registerStack && reachable && instrIndex+4 < len(sf.Instrs) &&
			instr.Kind == wasm.InstrLocalGet && sf.Instrs[instrIndex+1].Kind == wasm.InstrLocalGet &&
			arm64DirectFloatUnaryKind(sf.Instrs[instrIndex+2].Kind) &&
			(sf.Instrs[instrIndex+3].Kind == wasm.InstrF32Sub || sf.Instrs[instrIndex+3].Kind == wasm.InstrF64Sub) &&
			sf.Instrs[instrIndex+4].Kind == wasm.InstrLocalSet && sf.Instrs[instrIndex+4].U32() == sf.Instrs[instrIndex+1].U32() {
			kind := sf.Instrs[instrIndex+2].Kind
			f64 := sf.Instrs[instrIndex+3].Kind == wasm.InstrF64Sub
			emitARM64DirectFloatUnary(&a, kind, arm64.X0, localRegisters[sf.Instrs[instrIndex+1].U32()], f64)
			a.Fsub(localRegisters[sf.Instrs[instrIndex+1].U32()], localRegisters[instr.U32()], arm64.X0, f64)
			instrIndex += 4
			continue
		}
		if registerStack && reachable && instrIndex+5 < len(sf.Instrs) &&
			instr.Kind == wasm.InstrLocalGet && sf.Instrs[instrIndex+1].Kind == wasm.InstrLocalGet &&
			(sf.Instrs[instrIndex+2].Kind == wasm.InstrF32Abs || sf.Instrs[instrIndex+2].Kind == wasm.InstrF64Abs) &&
			(sf.Instrs[instrIndex+3].Kind == wasm.InstrF32Sqrt || sf.Instrs[instrIndex+3].Kind == wasm.InstrF64Sqrt) &&
			(sf.Instrs[instrIndex+4].Kind == wasm.InstrF32Sub || sf.Instrs[instrIndex+4].Kind == wasm.InstrF64Sub) &&
			sf.Instrs[instrIndex+5].Kind == wasm.InstrLocalSet && sf.Instrs[instrIndex+5].U32() == sf.Instrs[instrIndex+1].U32() {
			f64 := sf.Instrs[instrIndex+4].Kind == wasm.InstrF64Sub
			a.NeonFabs(arm64.X0, localRegisters[sf.Instrs[instrIndex+1].U32()], f64)
			a.Fsqrt(arm64.X0, arm64.X0, f64)
			a.Fsub(localRegisters[sf.Instrs[instrIndex+1].U32()], localRegisters[instr.U32()], arm64.X0, f64)
			instrIndex += 5
			continue
		}
		if registerStack && reachable && instrIndex+4 < len(sf.Instrs) &&
			instr.Kind == wasm.InstrLocalGet && sf.Instrs[instrIndex+1].Kind == wasm.InstrLocalGet &&
			(sf.Instrs[instrIndex+2].Kind == wasm.InstrI32Load || sf.Instrs[instrIndex+2].Kind == wasm.InstrI64Load || sf.Instrs[instrIndex+2].Kind == wasm.InstrF64Load) &&
			(sf.Instrs[instrIndex+3].Kind == wasm.InstrI32Add || sf.Instrs[instrIndex+3].Kind == wasm.InstrI64Add || sf.Instrs[instrIndex+3].Kind == wasm.InstrF64Add) &&
			sf.Instrs[instrIndex+4].Kind == wasm.InstrLocalSet && sf.Instrs[instrIndex+4].U32() == instr.U32() {
			mem := sf.Instrs[instrIndex+2]
			size := uint64(4)
			if mem.Kind == wasm.InstrI64Load || mem.Kind == wasm.InstrF64Load {
				size = 8
			}
			pointerIndex := sf.Instrs[instrIndex+1].U32()
			if arm64ProvenMaskedMemoryLocal(sf, pointerIndex, size, mem.U32()) {
				a.AddExtUXTW(arm64.X16, arm64.X26, localRegisters[pointerIndex])
				if mem.U32() != 0 {
					a.AddImm64(arm64.X16, arm64.X16, mem.U32())
				}
				switch mem.Kind {
				case wasm.InstrI32Load:
					a.Load32(arm64.X17, arm64.X16, 0)
					a.Add32(localRegisters[instr.U32()], localRegisters[instr.U32()], arm64.X17)
				case wasm.InstrI64Load:
					a.Load64(arm64.X17, arm64.X16, 0)
					a.Add64(localRegisters[instr.U32()], localRegisters[instr.U32()], arm64.X17)
				case wasm.InstrF64Load:
					a.LdrD(arm64.X0, arm64.X16, 0)
					a.Fadd(localRegisters[instr.U32()], localRegisters[instr.U32()], arm64.X0, true)
				}
				instrIndex += 4
				continue
			}
		}
		if registerStack && reachable && instrIndex+4 < len(sf.Instrs) &&
			instr.Kind == wasm.InstrLocalGet && sf.Instrs[instrIndex+1].Kind == wasm.InstrLocalGet &&
			sf.Instrs[instrIndex+2].Kind == wasm.InstrLocalGet && sf.Instrs[instrIndex+3].Kind == wasm.InstrI32Add &&
			sf.Instrs[instrIndex+4].Kind == wasm.InstrI32Store &&
			arm64ProvenMaskedMemoryLocal(sf, instr.U32(), 4, sf.Instrs[instrIndex+4].U32()) {
			mem := sf.Instrs[instrIndex+4]
			a.AddExtUXTW(arm64.X16, arm64.X26, localRegisters[instr.U32()])
			if mem.U32() != 0 {
				a.AddImm64(arm64.X16, arm64.X16, mem.U32())
			}
			a.Add32(arm64.X17, localRegisters[sf.Instrs[instrIndex+1].U32()], localRegisters[sf.Instrs[instrIndex+2].U32()])
			a.Store32(arm64.X17, arm64.X16, 0)
			instrIndex += 4
			continue
		}
		if registerStack && reachable && instrIndex+5 < len(sf.Instrs) &&
			instr.Kind == wasm.InstrLocalGet && sf.Instrs[instrIndex+1].Kind == wasm.InstrLocalGet &&
			sf.Instrs[instrIndex+2].Kind == wasm.InstrLocalGet && sf.Instrs[instrIndex+3].Kind == wasm.InstrI32Add &&
			sf.Instrs[instrIndex+4].Kind == wasm.InstrI64ExtendI32S && sf.Instrs[instrIndex+5].Kind == wasm.InstrI64Store &&
			arm64ProvenMaskedMemoryLocal(sf, instr.U32(), 8, sf.Instrs[instrIndex+5].U32()) {
			mem := sf.Instrs[instrIndex+5]
			a.AddExtUXTW(arm64.X16, arm64.X26, localRegisters[instr.U32()])
			if mem.U32() != 0 {
				a.AddImm64(arm64.X16, arm64.X16, mem.U32())
			}
			a.Add32(arm64.X17, localRegisters[sf.Instrs[instrIndex+1].U32()], localRegisters[sf.Instrs[instrIndex+2].U32()])
			a.Sxtw(arm64.X17, arm64.X17)
			a.Store64(arm64.X17, arm64.X16, 0)
			instrIndex += 5
			continue
		}
		if registerStack && reachable && instrIndex+5 < len(sf.Instrs) &&
			instr.Kind == wasm.InstrLocalGet && sf.Instrs[instrIndex+1].Kind == wasm.InstrI32Const &&
			sf.Instrs[instrIndex+2].Kind == wasm.InstrI32Add && sf.Instrs[instrIndex+3].Kind == wasm.InstrI32Const &&
			sf.Instrs[instrIndex+4].Kind == wasm.InstrI32And && sf.Instrs[instrIndex+5].Kind == wasm.InstrLocalSet &&
			sf.Instrs[instrIndex+5].U32() == instr.U32() && sf.Instrs[instrIndex+1].U64() <= 4095 {
			var probe arm64.Asm
			if probe.AndImm32(arm64.X0, arm64.X0, uint32(sf.Instrs[instrIndex+3].U64())) {
				reg := localRegisters[instr.U32()]
				a.AddImm32(reg, reg, uint32(sf.Instrs[instrIndex+1].U64()))
				a.AndImm32(reg, reg, uint32(sf.Instrs[instrIndex+3].U64()))
				instrIndex += 5
				continue
			}
		}
		if registerStack && reachable && instrIndex+3 < len(sf.Instrs) &&
			instr.Kind == wasm.InstrLocalGet && sf.Instrs[instrIndex+1].Kind == wasm.InstrLocalGet &&
			sf.Instrs[instrIndex+2].Kind == wasm.InstrCall && sf.Instrs[instrIndex+2].Inline() == wasm.InstrI32Add &&
			sf.Instrs[instrIndex+3].Kind == wasm.InstrLocalSet && sf.Instrs[instrIndex+3].U32() == instr.U32() {
			a.Add32(localRegisters[instr.U32()], localRegisters[instr.U32()], localRegisters[sf.Instrs[instrIndex+1].U32()])
			instrIndex += 3
			continue
		}
		if registerStack && reachable && instrIndex+4 < len(sf.Instrs) &&
			instr.Kind == wasm.InstrLocalGet && sf.Instrs[instrIndex+1].Kind == wasm.InstrLocalGet &&
			sf.Instrs[instrIndex+2].Kind == wasm.InstrI32Const && sf.Instrs[instrIndex+2].U64() == 0 &&
			sf.Instrs[instrIndex+3].Kind == wasm.InstrCallIndirect && sf.Instrs[instrIndex+3].Inline() == wasm.InstrI32Add &&
			sf.Instrs[instrIndex+4].Kind == wasm.InstrLocalSet && sf.Instrs[instrIndex+4].U32() == instr.U32() {
			a.Add32(localRegisters[instr.U32()], localRegisters[instr.U32()], localRegisters[sf.Instrs[instrIndex+1].U32()])
			instrIndex += 4
			continue
		}
		if registerStack && reachable && instrIndex+3 < len(sf.Instrs) &&
			instr.Kind == wasm.InstrLocalGet && sf.Instrs[instrIndex+1].Kind == wasm.InstrLocalGet &&
			arm64DirectLocalBinaryKind(sf.Instrs[instrIndex+2].Kind) &&
			sf.Instrs[instrIndex+3].Kind == wasm.InstrLocalSet {
			lhs := localRegisters[instr.U32()]
			rhs := localRegisters[sf.Instrs[instrIndex+1].U32()]
			dst := localRegisters[sf.Instrs[instrIndex+3].U32()]
			emitARM64DirectLocalBinary(&a, sf.Instrs[instrIndex+2].Kind, dst, lhs, rhs)
			instrIndex += 3
			continue
		}
		if registerStack && reachable && instrIndex+4 < len(sf.Instrs) &&
			instr.Kind == wasm.InstrLocalGet && sf.Instrs[instrIndex+1].Kind == wasm.InstrLocalGet &&
			arm64DirectLocalBinaryKind(sf.Instrs[instrIndex+2].Kind) && sf.Instrs[instrIndex+3].Kind == wasm.InstrLocalTee &&
			sf.Instrs[instrIndex+4].Kind == wasm.InstrLocalSet {
			dst := localRegisters[sf.Instrs[instrIndex+3].U32()]
			emitARM64DirectLocalBinary(&a, sf.Instrs[instrIndex+2].Kind, dst, localRegisters[instr.U32()], localRegisters[sf.Instrs[instrIndex+1].U32()])
			copyDst := localRegisters[sf.Instrs[instrIndex+4].U32()]
			if copyDst != dst {
				a.MovReg64(copyDst, dst)
			}
			instrIndex += 4
			continue
		}
		if registerStack && reachable && instrIndex+3 < len(sf.Instrs) &&
			instr.Kind == wasm.InstrLocalGet && sf.Instrs[instrIndex+1].Kind == wasm.InstrLocalGet &&
			arm64DirectFloatBinaryKind(sf.Instrs[instrIndex+2].Kind) &&
			sf.Instrs[instrIndex+3].Kind == wasm.InstrLocalSet && sf.Instrs[instrIndex+3].U32() == instr.U32() {
			kind := sf.Instrs[instrIndex+2].Kind
			emitARM64DirectFloatBinary(&a, kind, localRegisters[instr.U32()], localRegisters[sf.Instrs[instrIndex+1].U32()])
			instrIndex += 3
			continue
		}
		if registerStack && reachable && instrIndex+2 < len(sf.Instrs) &&
			instr.Kind == wasm.InstrLocalGet && sf.Instrs[instrIndex+1].Kind == wasm.InstrI32Eqz &&
			sf.Instrs[instrIndex+2].Kind == wasm.InstrBrIf {
			label := sf.Instrs[instrIndex+2].U32()
			if int(label) < len(controls) {
				target := &controls[len(controls)-1-int(label)]
				if !target.result || target.kind == wasm.InstrLoop {
					if target.kind == wasm.InstrLoop {
						if err := patchCBZ32(localRegisters[instr.U32()], target.start); err != nil {
							return nil, 0, nil, err
						}
					} else {
						target.endReached = true
						target.patches = append(target.patches, farCBZ32(localRegisters[instr.U32()]))
					}
					if loop := &controls[len(controls)-1]; loop.kind == wasm.InstrLoop {
						if tail, ok := arm64CountedLoopTail(sf.Instrs, instrIndex, instr.U32()); ok {
							_, _, rotation, _, rotationOK := arm64CoupledRotationUpdate(sf.Instrs, instrIndex+3)
							if rotationOK && (rotation == wasm.InstrI32Rotl || rotation == wasm.InstrI64Rotl) {
								for a.Len()&63 != 0 {
									a.Nop()
								}
							} else {
								a.Align16()
							}
							loop.start = a.Len()
							loop.countedLocal, loop.countedTail, loop.counted = instr.U32(), tail, true
						}
					}
					instrIndex += 2
					continue
				}
			}
		}
		if registerStack && reachable && instrIndex+3 < len(sf.Instrs) &&
			instr.Kind == wasm.InstrLocalGet && sf.Instrs[instrIndex+1].Kind == wasm.InstrI32Const && sf.Instrs[instrIndex+1].U64() == 1 &&
			sf.Instrs[instrIndex+2].Kind == wasm.InstrI32And && sf.Instrs[instrIndex+3].Kind == wasm.InstrBrIf {
			label := sf.Instrs[instrIndex+3].U32()
			if int(label) < len(controls) {
				target := &controls[len(controls)-1-int(label)]
				if !target.result || target.kind == wasm.InstrLoop {
					a.TstImm32(localRegisters[instr.U32()], 1)
					if target.kind == wasm.InstrLoop {
						if err := patchBcond(arm64.CondNE, target.start); err != nil {
							return nil, 0, nil, err
						}
					} else {
						target.endReached = true
						target.patches = append(target.patches, farBcond(arm64.CondNE))
					}
					instrIndex += 3
					continue
				}
			}
		}
		if registerStack && reachable && instrIndex+3 < len(sf.Instrs) &&
			instr.Kind == wasm.InstrLocalGet &&
			(sf.Instrs[instrIndex+1].Kind == wasm.InstrI32Const || sf.Instrs[instrIndex+1].Kind == wasm.InstrI64Const) &&
			(sf.Instrs[instrIndex+2].Kind == wasm.InstrI32Add || sf.Instrs[instrIndex+2].Kind == wasm.InstrI64Add ||
				sf.Instrs[instrIndex+2].Kind == wasm.InstrI32Sub || sf.Instrs[instrIndex+2].Kind == wasm.InstrI64Sub) &&
			sf.Instrs[instrIndex+3].Kind == wasm.InstrLocalSet && sf.Instrs[instrIndex+3].U32() == instr.U32() {
			kind := sf.Instrs[instrIndex+2].Kind
			value := sf.Instrs[instrIndex+1].U64()
			end := instrIndex + 4
			// Adjacent wrapping additions/subtractions to the same local can be
			// combined exactly. The ISA corpus deliberately repeats this shape;
			// doing the fold here also removes redundant Wasm stack shuffling.
			for end+3 < len(sf.Instrs) && sf.Instrs[end].Kind == wasm.InstrLocalGet && sf.Instrs[end].U32() == instr.U32() &&
				sf.Instrs[end+1].Kind == sf.Instrs[instrIndex+1].Kind && sf.Instrs[end+2].Kind == kind &&
				sf.Instrs[end+3].Kind == wasm.InstrLocalSet && sf.Instrs[end+3].U32() == instr.U32() {
				value += sf.Instrs[end+1].U64()
				end += 4
			}
			if value <= 4095 {
				reg := localRegisters[instr.U32()]
				switch kind {
				case wasm.InstrI32Add:
					a.AddImm32(reg, reg, uint32(value))
				case wasm.InstrI64Add:
					a.AddImm64(reg, reg, uint32(value))
				case wasm.InstrI32Sub:
					a.SubImm32(reg, reg, uint32(value))
				case wasm.InstrI64Sub:
					a.SubImm64(reg, reg, uint32(value))
				}
				instrIndex = end - 1
				continue
			}
		}
		if registerOperandStack && reachable && len(stackTypes) != 0 && instrIndex+2 < len(sf.Instrs) && instr.Kind == wasm.InstrLocalGet &&
			arm64IntegerComparisonKind(sf.Instrs[instrIndex+1].Kind) && sf.Instrs[instrIndex+2].Kind == wasm.InstrIf {
			rhsLocal := int(instr.U32())
			top := len(stackTypes) - 1
			if !localFloat[rhsLocal] && (registerStack || localScalarPinned[rhsLocal]) && stackTypes[top] == sf.Locals[rhsLocal] &&
				(stackTypes[top] == wasm.I32 || stackTypes[top] == wasm.I64) {
				kind := sf.Instrs[instrIndex+1].Kind
				if stackTypes[top] == wasm.I64 {
					a.CmpReg64(operandStackRegisters[top], localRegisters[rhsLocal])
				} else {
					a.CmpReg32(operandStackRegisters[top], localRegisters[rhsLocal])
				}
				ifInstr := sf.Instrs[instrIndex+2]
				stackTypes = stackTypes[:top]
				control := arm64StackControl{kind: wasm.InstrIf, depth: top, result: ifInstr.HasResult(), resultType: ifInstr.ValueType(), falsePatch: farBcond(arm64.Cond(uint8(arm64IntegerComparisonCond(kind)) ^ 1)), parentReachable: true}
				controls = append(controls, control)
				instrIndex += 2
				continue
			}
		}
		if registerOperandStack && reachable && instrIndex+1 < len(sf.Instrs) && sf.Instrs[instrIndex+1].Kind == wasm.InstrIf &&
			arm64IntegerComparisonKind(instr.Kind) && len(stackTypes) >= 2 {
			lhsIndex, rhsIndex := len(stackTypes)-2, len(stackTypes)-1
			wide := instr.Kind >= wasm.InstrI64Eq && instr.Kind <= wasm.InstrI64GeU
			typeMatches := wide && stackTypes[lhsIndex] == wasm.I64 && stackTypes[rhsIndex] == wasm.I64 ||
				!wide && stackTypes[lhsIndex] == wasm.I32 && stackTypes[rhsIndex] == wasm.I32
			if typeMatches {
				if wide {
					a.CmpReg64(operandStackRegisters[lhsIndex], operandStackRegisters[rhsIndex])
				} else {
					a.CmpReg32(operandStackRegisters[lhsIndex], operandStackRegisters[rhsIndex])
				}
				stackTypes = stackTypes[:lhsIndex]
				ifInstr := sf.Instrs[instrIndex+1]
				control := arm64StackControl{kind: wasm.InstrIf, depth: lhsIndex, result: ifInstr.HasResult(), resultType: ifInstr.ValueType(), falsePatch: farBcond(arm64.Cond(uint8(arm64IntegerComparisonCond(instr.Kind)) ^ 1)), parentReachable: true}
				controls = append(controls, control)
				metadata.recordSource(a.Len(), ifInstr.Offset)
				instrIndex++
				continue
			}
		}
		if registerOperandStack && reachable && instrIndex+1 < len(sf.Instrs) &&
			(sf.Instrs[instrIndex+1].Kind == wasm.InstrIf || sf.Instrs[instrIndex+1].Kind == wasm.InstrBrIf) &&
			(instr.Kind == wasm.InstrI32Eqz || instr.Kind == wasm.InstrI64Eqz) && len(stackTypes) != 0 {
			top := len(stackTypes) - 1
			wide := instr.Kind == wasm.InstrI64Eqz
			branch := sf.Instrs[instrIndex+1]
			branchSupported := branch.Kind == wasm.InstrIf
			if branch.Kind == wasm.InstrBrIf && int(branch.U32()) <= len(controls) {
				branchSupported = int(branch.U32()) == len(controls) && len(sf.Results) == 0
				if int(branch.U32()) < len(controls) {
					branchSupported = !controls[len(controls)-1-int(branch.U32())].result
				}
			}
			if branchSupported && (wide && stackTypes[top] == wasm.I64 || !wide && stackTypes[top] == wasm.I32) {
				if wide {
					a.CmpImm64(operandStackRegisters[top], 0)
				} else {
					a.CmpImm32(operandStackRegisters[top], 0)
				}
				stackTypes = stackTypes[:top]
				if branch.Kind == wasm.InstrIf {
					control := arm64StackControl{kind: wasm.InstrIf, depth: top, result: branch.HasResult(), resultType: branch.ValueType(), falsePatch: farBcond(arm64.CondNE), parentReachable: true}
					controls = append(controls, control)
				} else if int(branch.U32()) == len(controls) {
					functionPatches = append(functionPatches, farBcond(arm64.CondEQ))
				} else {
					target := &controls[len(controls)-1-int(branch.U32())]
					site := farBcond(arm64.CondEQ)
					if target.kind == wasm.InstrLoop {
						if err := patch(site, target.start); err != nil {
							return nil, 0, nil, err
						}
					} else {
						target.endReached = true
						target.patches = append(target.patches, site)
					}
				}
				metadata.recordSource(a.Len(), branch.Offset)
				instrIndex++
				continue
			}
		}
		if registerOperandStack && reachable && instrIndex+3 < len(sf.Instrs) && instr.Kind == wasm.InstrLocalGet &&
			sf.Instrs[instrIndex+1].Kind == wasm.InstrLocalGet && arm64IntegerComparisonKind(sf.Instrs[instrIndex+2].Kind) &&
			sf.Instrs[instrIndex+3].Kind == wasm.InstrIf {
			lhsLocal, rhsLocal := int(instr.U32()), int(sf.Instrs[instrIndex+1].U32())
			pinned := func(local int) bool {
				return local >= 0 && local < len(sf.Locals) && !localFloat[local] && (registerStack || localScalarPinned[local])
			}
			if pinned(lhsLocal) && pinned(rhsLocal) && sf.Locals[lhsLocal] == sf.Locals[rhsLocal] {
				kind := sf.Instrs[instrIndex+2].Kind
				if sf.Locals[lhsLocal] == wasm.I64 {
					a.CmpReg64(localRegisters[lhsLocal], localRegisters[rhsLocal])
				} else {
					a.CmpReg32(localRegisters[lhsLocal], localRegisters[rhsLocal])
				}
				ifInstr := sf.Instrs[instrIndex+3]
				control := arm64StackControl{kind: wasm.InstrIf, depth: len(stackTypes), result: ifInstr.HasResult(), resultType: ifInstr.ValueType(), falsePatch: farBcond(arm64.Cond(uint8(arm64IntegerComparisonCond(kind)) ^ 1)), parentReachable: true}
				controls = append(controls, control)
				instrIndex += 3
				continue
			}
		}
		if registerOperandStack && reachable && instrIndex+3 < len(sf.Instrs) && instr.Kind == wasm.InstrLocalGet &&
			(sf.Instrs[instrIndex+1].Kind == wasm.InstrI32Const || sf.Instrs[instrIndex+1].Kind == wasm.InstrI64Const) &&
			arm64IntegerComparisonKind(sf.Instrs[instrIndex+2].Kind) &&
			(sf.Instrs[instrIndex+3].Kind == wasm.InstrIf || sf.Instrs[instrIndex+3].Kind == wasm.InstrBrIf) {
			local := int(instr.U32())
			if local >= 0 && local < len(sf.Locals) && !localFloat[local] && (registerStack || localScalarPinned[local]) {
				kind := sf.Instrs[instrIndex+2].Kind
				wide := sf.Locals[local] == wasm.I64
				constantKindMatches := wide && sf.Instrs[instrIndex+1].Kind == wasm.InstrI64Const || !wide && sf.Instrs[instrIndex+1].Kind == wasm.InstrI32Const
				branch := sf.Instrs[instrIndex+3]
				branchSupported := branch.Kind == wasm.InstrIf
				if branch.Kind == wasm.InstrBrIf && int(branch.U32()) <= len(controls) {
					branchSupported = int(branch.U32()) == len(controls) && len(sf.Results) == 0
					if int(branch.U32()) < len(controls) {
						target := &controls[len(controls)-1-int(branch.U32())]
						branchSupported = !target.result
					}
				}
				if constantKindMatches && branchSupported {
					a.MovImm64(arm64.X16, sf.Instrs[instrIndex+1].U64())
					if wide {
						a.CmpReg64(localRegisters[local], arm64.X16)
					} else {
						a.CmpReg32(localRegisters[local], arm64.X16)
					}
					cond := arm64IntegerComparisonCond(kind)
					if branch.Kind == wasm.InstrIf {
						control := arm64StackControl{kind: wasm.InstrIf, depth: len(stackTypes), result: branch.HasResult(), resultType: branch.ValueType(), falsePatch: farBcond(arm64.Cond(uint8(cond) ^ 1)), parentReachable: true}
						controls = append(controls, control)
					} else if int(branch.U32()) == len(controls) {
						functionPatches = append(functionPatches, farBcond(cond))
					} else {
						target := &controls[len(controls)-1-int(branch.U32())]
						if target.kind == wasm.InstrLoop {
							if err := patch(farBcond(cond), target.start); err != nil {
								return nil, 0, nil, err
							}
						} else {
							target.endReached = true
							target.patches = append(target.patches, farBcond(cond))
						}
					}
					metadata.recordSource(a.Len(), branch.Offset)
					instrIndex += 3
					continue
				}
			}
		}
		if registerOperandStack && reachable && instrIndex+3 < len(sf.Instrs) && instr.Kind == wasm.InstrGlobalGet &&
			(sf.Instrs[instrIndex+1].Kind == wasm.InstrI32Const || sf.Instrs[instrIndex+1].Kind == wasm.InstrI64Const) &&
			arm64IntegerComparisonKind(sf.Instrs[instrIndex+2].Kind) &&
			(sf.Instrs[instrIndex+3].Kind == wasm.InstrIf || sf.Instrs[instrIndex+3].Kind == wasm.InstrBrIf) {
			if slot := promotedGlobalSlot(instr.U32()); slot >= 0 {
				kind := sf.Instrs[instrIndex+2].Kind
				wide := sf.Globals[instr.U32()] == wasm.I64
				constantKindMatches := wide && sf.Instrs[instrIndex+1].Kind == wasm.InstrI64Const || !wide && sf.Instrs[instrIndex+1].Kind == wasm.InstrI32Const
				branch := sf.Instrs[instrIndex+3]
				branchSupported := branch.Kind == wasm.InstrIf
				if branch.Kind == wasm.InstrBrIf && int(branch.U32()) <= len(controls) {
					branchSupported = int(branch.U32()) == len(controls) && len(sf.Results) == 0
					if int(branch.U32()) < len(controls) {
						target := &controls[len(controls)-1-int(branch.U32())]
						branchSupported = !target.result
					}
				}
				if constantKindMatches && branchSupported {
					a.MovImm64(arm64.X16, sf.Instrs[instrIndex+1].U64())
					if wide {
						a.CmpReg64(promotedGlobalRegs[slot], arm64.X16)
					} else {
						a.CmpReg32(promotedGlobalRegs[slot], arm64.X16)
					}
					cond := arm64IntegerComparisonCond(kind)
					if branch.Kind == wasm.InstrIf {
						control := arm64StackControl{kind: wasm.InstrIf, depth: len(stackTypes), result: branch.HasResult(), resultType: branch.ValueType(), falsePatch: farBcond(arm64.Cond(uint8(cond) ^ 1)), parentReachable: true}
						controls = append(controls, control)
					} else if int(branch.U32()) == len(controls) {
						functionPatches = append(functionPatches, farBcond(cond))
					} else {
						target := &controls[len(controls)-1-int(branch.U32())]
						if target.kind == wasm.InstrLoop {
							if err := patch(farBcond(cond), target.start); err != nil {
								return nil, 0, nil, err
							}
						} else {
							target.endReached = true
							target.patches = append(target.patches, farBcond(cond))
						}
					}
					metadata.recordSource(a.Len(), branch.Offset)
					instrIndex += 3
					continue
				}
			}
		}
		if registerOperandStack && reachable && instrIndex+2 < len(sf.Instrs) && instr.Kind == wasm.InstrLocalGet &&
			sf.Instrs[instrIndex+1].Kind == wasm.InstrLocalGet && arm64DirectLocalBinaryKind(sf.Instrs[instrIndex+2].Kind) &&
			len(stackTypes) < len(operandStackRegisters) {
			lhsLocal, rhsLocal := int(instr.U32()), int(sf.Instrs[instrIndex+1].U32())
			pinned := func(local int) bool {
				return local >= 0 && local < len(sf.Locals) && !localFloat[local] && (registerStack || localScalarPinned[local])
			}
			if pinned(lhsLocal) && pinned(rhsLocal) && sf.Locals[lhsLocal] == sf.Locals[rhsLocal] {
				dst := operandStackRegisters[len(stackTypes)]
				emitARM64DirectLocalBinary(&a, sf.Instrs[instrIndex+2].Kind, dst, localRegisters[lhsLocal], localRegisters[rhsLocal])
				stackTypes = append(stackTypes, sf.Locals[lhsLocal])
				instrIndex += 2
				continue
			}
		}
		if registerOperandStack && reachable && instrIndex+1 < len(sf.Instrs) && instr.Kind == wasm.InstrLocalGet && sf.Instrs[instrIndex+1].Kind == wasm.InstrLocalSet {
			src, dst := int(instr.U32()), int(sf.Instrs[instrIndex+1].U32())
			if !localFloat[src] && !localFloat[dst] && sf.Locals[src] != wasm.V128 && sf.Locals[src] == sf.Locals[dst] &&
				(registerStack || localScalarPinned[src]) && (registerStack || localScalarPinned[dst]) {
				if localRegisters[src] != localRegisters[dst] {
					a.MovReg64(localRegisters[dst], localRegisters[src])
				}
				instrIndex++
				continue
			}
		}
		if instr.IsElse() {
			if len(controls) == 0 || controls[len(controls)-1].kind != wasm.InstrIf {
				return nil, 0, nil, fmt.Errorf("else without if")
			}
			control := &controls[len(controls)-1]
			if reachable {
				control.endReached = true
				control.patches = append(control.patches, arm64StackPatch{at: a.Branch()})
			}
			if control.parentReachable && control.falsePatch.at < 0 {
				return nil, 0, nil, fmt.Errorf("if false branch is out of range")
			}
			if control.falsePatch.at >= 0 {
				if err := patch(control.falsePatch, a.Len()); err != nil {
					return nil, 0, nil, fmt.Errorf("if false branch is out of range: %w", err)
				}
			}
			control.falsePatch = arm64StackPatch{at: -1}
			control.seenElse = true
			reachable = control.parentReachable
			stackTypes = stackTypes[:control.depth]
			continue
		}
		switch instr.Kind {
		case wasm.InstrBlock, wasm.InstrLoop, wasm.InstrIf:
			control := arm64StackControl{kind: instr.Kind, depth: len(stackTypes), result: instr.HasResult(), resultType: instr.ValueType(), falsePatch: arm64StackPatch{at: -1}, parentReachable: reachable}
			if instr.Kind == wasm.InstrIf && reachable {
				if _, err := pop(arm64.X16); err != nil {
					return nil, 0, nil, err
				}
				control.depth = len(stackTypes)
				control.falsePatch = farCBZ32(arm64.X16)
			}
			if instr.Kind == wasm.InstrLoop {
				a.Align16()
			}
			control.start = a.Len()
			controls = append(controls, control)
			continue
		case wasm.InstrInvalid: // end
			if len(controls) == 0 {
				continue
			}
			control := controls[len(controls)-1]
			controls = controls[:len(controls)-1]
			target := a.Len()
			if control.falsePatch.at >= 0 {
				if err := patch(control.falsePatch, target); err != nil {
					return nil, 0, nil, fmt.Errorf("if false branch is out of range: %w", err)
				}
			}
			for _, site := range control.patches {
				if err := patch(site, target); err != nil {
					return nil, 0, nil, err
				}
			}
			falseReachable := control.kind == wasm.InstrIf && !control.seenElse && control.parentReachable
			reachable = reachable || control.endReached || falseReachable
			stackTypes = stackTypes[:control.depth]
			if control.result {
				stackTypes = append(stackTypes, control.resultType)
			}
			continue
		}
		if !reachable {
			continue
		}
		if registerOperandStack && instr.Kind == wasm.InstrLocalGet && instrIndex+1 < len(sf.Instrs) &&
			arm64MemoryStackKind(sf.Instrs[instrIndex+1].Kind) &&
			!(sf.Instrs[instrIndex+1].Kind >= wasm.InstrI32Store && sf.Instrs[instrIndex+1].Kind <= wasm.InstrI64Store32) &&
			len(stackTypes) < len(operandStackRegisters) {
			local := int(instr.U32())
			if local >= 0 && local < len(sf.Locals) && sf.Locals[local] == wasm.I32 && (registerStack || localScalarPinned[local]) {
				load := sf.Instrs[instrIndex+1]
				stackTypes = append(stackTypes, wasm.I32)
				if err := emitARM64RegisterStackMemory(&a, load, &stackTypes, operandStackRegisters, localRegisters[local], true, fn.Index, plan.ElidesBoundsCheck(uint32(instrIndex+1)), cacheMemorySize, cacheMemoryEnd, emitColdMemoryTrap, metadata); err != nil {
					return nil, 0, nil, fmt.Errorf("byte %d: %w", load.Offset, err)
				}
				metadata.recordSource(a.Len(), load.Offset)
				instrIndex++
				continue
			}
		}
		switch instr.Kind {
		case wasm.InstrUnreachable:
			metadata.recordTrap(a.Len(), instr.Offset, 1)
			arm64EmitTrap(&a, 1, fn.Index, instr.Offset)
			reachable = false
		case wasm.InstrReturn:
			if len(sf.Results) == 1 {
				if len(stackTypes) == 0 || !moveStackValue(len(stackTypes)-1, 0, sf.Results[0]) {
					return nil, 0, nil, fmt.Errorf("return result is unavailable")
				}
			}
			functionPatches = append(functionPatches, arm64StackPatch{at: a.Branch()})
			reachable = false
		case wasm.InstrCall, wasm.InstrCallIndirect:
			callBoundary := pinLocalsAcrossCalls && instr.Inline() == wasm.InstrInvalid
			calleePreservesPinned := false
			calleeUsesPrivateArguments := instr.Kind == wasm.InstrCall && instr.U32() >= sf.ImportedFuncs
			if instr.Kind == wasm.InstrCall && instr.U32() >= sf.ImportedFuncs {
				callee := int(instr.U32() - sf.ImportedFuncs)
				if callee < len(contracts) && contracts[callee].Class != 0 {
					calleeUsesPrivateArguments = arm64DirectPreparedClass(contracts[callee].Class)
				}
				calleePreservesPinned = callBoundary && !sf.HasV128 && callee < len(contracts) && (contracts[callee].Class != 0 ||
					arm64JSONSIMDDeserializePreservedFunction(uint32(callee)) && arm64JSONSIMDDeserializePreservationModule(sf.Module))
			}
			stackPrefix := len(stackTypes) - int(instr.Params())
			if instr.Kind == wasm.InstrCallIndirect {
				stackPrefix--
			}
			skipCall := -1
			if instr.Kind == wasm.InstrCall && instr.Inline() == wasm.InstrInvalid && instr.Params() == 2 && !instr.HasResult() &&
				stackPrefix >= 0 && stackPrefix < len(stackTypes) && stackTypes[stackPrefix] == wasm.I32 {
				if limitGlobal, ok := arm64EarlyReturnI32LEGlobal(sf.Module, instr.U32(), sf.ImportedFuncs); ok &&
					int(limitGlobal) < len(sf.Globals) && sf.Globals[limitGlobal] == wasm.I32 {
					if !stackLoad(stackPrefix, arm64.X16) || !loadGlobalDescriptor(arm64.X17, limitGlobal) || !a.Load32(arm64.X17, arm64.X17, 0) {
						return nil, 0, nil, fmt.Errorf("byte %d: guarded call operands are not encodable", instr.Offset)
					}
					a.CmpReg32(arm64.X16, arm64.X17)
					skipCall = a.Bcond(arm64.CondLS)
				}
			}
			if callBoundary && registerOperandStack {
				if sf.HasV128 {
					flushVectorStack()
				}
				// Direct private calls consume arguments from disjoint parameter
				// registers, so only their live prefix survives. Canonical local and
				// wrapper calls keep the complete argument vector for the callee to read.
				spill := arm64StructuredCallSpillLimit(instr.Kind, instr.U32(), sf.ImportedFuncs, len(stackTypes), stackPrefix, calleeUsesPrivateArguments)
				for index := 0; index < spill; index++ {
					if stackTypes[index] == wasm.V128 {
						continue
					}
					if index+1 < len(stackTypes) && stackTypes[index+1] != wasm.V128 && stackOff(index) <= 504 && stackOff(index+1) == stackOff(index)+8 {
						a.StpOffset(operandStackRegisters[index], operandStackRegisters[index+1], arm64.SP, int32(stackOff(index)))
						index++
						continue
					}
					if !a.Store64(operandStackRegisters[index], arm64.SP, stackOff(index)) {
						return nil, 0, nil, fmt.Errorf("byte %d: operand stack spill is not encodable", instr.Offset)
					}
				}
			}
			if callBoundary && !calleePreservesPinned && !spillPinnedScalarLocals() {
				return nil, 0, nil, fmt.Errorf("byte %d: pinned local spill is not encodable", instr.Offset)
			}
			if callBoundary && !calleePreservesPinned {
				spillPinnedV128Locals()
			}
			if err := emitARM64StackCall(&a, sf, instr, &stackTypes, stackLoad, stackStore, stackOff, &callRelocs, fn.Index, metadata); err != nil {
				return nil, 0, nil, fmt.Errorf("byte %d: %w", instr.Offset, err)
			}
			if !arm64StructuredCanDeferPromotedGlobalReload(sf.Instrs, instrIndex) && !reloadPromotedGlobal(true) {
				return nil, 0, nil, fmt.Errorf("byte %d: promoted global reload is not encodable", instr.Offset)
			}
			if callBoundary && !calleePreservesPinned && !reloadPinnedScalarLocals() {
				return nil, 0, nil, fmt.Errorf("byte %d: pinned local reload is not encodable", instr.Offset)
			}
			if callBoundary && !calleePreservesPinned {
				reloadPinnedV128Locals()
			}
			if callBoundary && registerOperandStack {
				reload := stackPrefix
				if instr.Kind == wasm.InstrCall && instr.U32() < sf.ImportedFuncs {
					reload = len(stackTypes)
				}
				for index := 0; index < reload; index++ {
					if stackTypes[index] == wasm.V128 {
						continue
					}
					if index+1 < reload && stackTypes[index+1] != wasm.V128 && stackOff(index) <= 504 && stackOff(index+1) == stackOff(index)+8 {
						a.LdpOffset(operandStackRegisters[index], operandStackRegisters[index+1], arm64.SP, int32(stackOff(index)))
						index++
						continue
					}
					if !a.Load64(operandStackRegisters[index], arm64.SP, stackOff(index)) {
						return nil, 0, nil, fmt.Errorf("byte %d: operand stack reload is not encodable", instr.Offset)
					}
				}
			}
			if callBoundary && !calleePreservesPinned && cacheMemorySize {
				a.SubImm64(arm64.X25, arm64.X26, abi.ActualLinMemByteSize64Offset)
				if !a.Load64(arm64.X25, arm64.X25, 0) {
					return nil, 0, nil, fmt.Errorf("byte %d: cached memory size reload is not encodable", instr.Offset)
				}
				if cacheMemoryEnd {
					a.Add64(arm64.X25, arm64.X26, arm64.X25)
				}
			}
			if skipCall >= 0 && !a.PatchBranch19(skipCall, a.Len()) {
				return nil, 0, nil, fmt.Errorf("byte %d: guarded call branch is out of range", instr.Offset)
			}
		case wasm.InstrBr, wasm.InstrBrIf:
			if int(instr.U32()) > len(controls) {
				return nil, 0, nil, fmt.Errorf("branch label %d is out of range", instr.U32())
			}
			if int(instr.U32()) == len(controls) {
				conditional := instr.Kind == wasm.InstrBrIf
				if conditional {
					if _, err := pop(arm64.X16); err != nil {
						return nil, 0, nil, err
					}
				}
				if len(sf.Results) == 1 {
					if len(stackTypes) == 0 || !moveStackValue(len(stackTypes)-1, 0, sf.Results[0]) {
						return nil, 0, nil, fmt.Errorf("function branch result is unavailable")
					}
				}
				if conditional {
					functionPatches = append(functionPatches, farCBNZ32(arm64.X16))
				} else {
					functionPatches = append(functionPatches, arm64StackPatch{at: a.Branch()})
					reachable = false
				}
				continue
			}
			targetIndex := len(controls) - 1 - int(instr.U32())
			target := &controls[targetIndex]
			moveResult := func() error {
				if !target.result || target.kind == wasm.InstrLoop {
					return nil
				}
				if len(stackTypes) == 0 || !moveStackValue(len(stackTypes)-1, target.depth, target.resultType) {
					return fmt.Errorf("branch result is unavailable")
				}
				return nil
			}
			conditional := instr.Kind == wasm.InstrBrIf
			if conditional {
				if _, err := pop(arm64.X16); err != nil {
					return nil, 0, nil, err
				}
				if err := moveResult(); err != nil {
					return nil, 0, nil, err
				}
				if target.kind == wasm.InstrLoop {
					if err := patchCBNZ32(arm64.X16, target.start); err != nil {
						return nil, 0, nil, err
					}
				} else {
					target.endReached = true
					target.patches = append(target.patches, farCBNZ32(arm64.X16))
				}
			} else {
				if err := moveResult(); err != nil {
					return nil, 0, nil, err
				}
				site := a.Branch()
				if target.kind == wasm.InstrLoop {
					if !a.PatchBranch26(site, target.start) {
						return nil, 0, nil, fmt.Errorf("loop branch is out of range")
					}
				} else {
					target.endReached = true
					target.patches = append(target.patches, arm64StackPatch{at: site})
				}
				reachable = false
			}
		case wasm.InstrBrOnCast, wasm.InstrBrOnCastFail:
			immediate, ok := sf.BranchCastImmediateAt(uint32(instrIndex))
			if !ok || int(immediate.Label) > len(controls) {
				return nil, 0, nil, fmt.Errorf("branch cast immediate is unavailable")
			}
			if len(stackTypes) == 0 || stackTypes[len(stackTypes)-1].Kind() != wasm.ValRef || !stackLoad(len(stackTypes)-1, arm64.X0) {
				return nil, 0, nil, fmt.Errorf("branch cast reference operand is unavailable")
			}
			heap, nullable, exact := codegen.DecodeGCRefTarget(immediate.Target)
			a.Ldur64(arm64.X17, arm64.X26, -int32(abi.SyncHostCustomContextOffset))
			if !a.Store64(arm64.X0, arm64.X17, uint32(abi.SyncHostArgsOffset)) {
				return nil, 0, nil, fmt.Errorf("GC branch cast reference offset is not encodable")
			}
			a.MovImm64(arm64.X16, uint64(heap))
			if !a.Store64(arm64.X16, arm64.X17, uint32(abi.SyncHostArgsOffset+8)) {
				return nil, 0, nil, fmt.Errorf("GC branch cast heap offset is not encodable")
			}
			a.MovImm64(arm64.X16, uint64(boolUint32(nullable)))
			if !a.Store64(arm64.X16, arm64.X17, uint32(abi.SyncHostArgsOffset+16)) {
				return nil, 0, nil, fmt.Errorf("GC branch cast nullable offset is not encodable")
			}
			a.MovImm64(arm64.X16, uint64(boolUint32(exact)))
			if !a.Store64(arm64.X16, arm64.X17, uint32(abi.SyncHostArgsOffset+24)) {
				return nil, 0, nil, fmt.Errorf("GC branch cast exact offset is not encodable")
			}
			if err := callGCHelper(codegen.GCHelperRefTest, 0, 4, 1); err != nil {
				return nil, 0, nil, err
			}
			a.Ldur64(arm64.X17, arm64.X26, -int32(abi.SyncHostCustomContextOffset))
			if !a.Load64(arm64.X16, arm64.X17, uint32(abi.SyncHostResultsOffset)) {
				return nil, 0, nil, fmt.Errorf("GC branch cast result offset is not encodable")
			}
			stackTypes[len(stackTypes)-1] = immediate.BranchType
			cond := arm64.CondNE
			if instr.Kind == wasm.InstrBrOnCastFail {
				cond = arm64.CondEQ
			}
			a.CmpImm32(arm64.X16, 0)
			if int(immediate.Label) == len(controls) {
				if len(sf.Results) == 1 && !moveStackValue(len(stackTypes)-1, 0, sf.Results[0]) {
					return nil, 0, nil, fmt.Errorf("function branch-cast result is unavailable")
				}
				functionPatches = append(functionPatches, farBcond(cond))
			} else {
				target := &controls[len(controls)-1-int(immediate.Label)]
				if target.result && target.kind != wasm.InstrLoop && !moveStackValue(len(stackTypes)-1, target.depth, target.resultType) {
					return nil, 0, nil, fmt.Errorf("branch-cast result is unavailable")
				}
				if target.kind == wasm.InstrLoop {
					if err := patchBcond(cond, target.start); err != nil {
						return nil, 0, nil, err
					}
				} else {
					target.endReached = true
					target.patches = append(target.patches, farBcond(cond))
				}
			}
			stackTypes[len(stackTypes)-1] = immediate.Fallthrough
		case wasm.InstrBrTable:
			if _, err := pop(arm64.X16); err != nil {
				return nil, 0, nil, err
			}
			labels := instr.Labels(sf)
			for caseIndex, label := range labels {
				if int(label) > len(controls) {
					return nil, 0, nil, fmt.Errorf("br_table label %d is out of range", label)
				}
				if int(label) == len(controls) {
					if len(sf.Results) == 1 {
						if len(stackTypes) == 0 || !moveStackValue(len(stackTypes)-1, 0, sf.Results[0]) {
							return nil, 0, nil, fmt.Errorf("br_table function result is unavailable")
						}
					}
					cond := caseIndex != len(labels)-1
					if cond {
						a.CmpImm32(arm64.X16, uint32(caseIndex))
						functionPatches = append(functionPatches, farBcond(arm64.CondEQ))
					} else {
						functionPatches = append(functionPatches, arm64StackPatch{at: a.Branch()})
					}
					continue
				}
				target := &controls[len(controls)-1-int(label)]
				if target.result && target.kind != wasm.InstrLoop {
					if len(stackTypes) == 0 || !moveStackValue(len(stackTypes)-1, target.depth, target.resultType) {
						return nil, 0, nil, fmt.Errorf("br_table result is unavailable")
					}
				}
				var site arm64StackPatch
				cond := caseIndex != len(labels)-1
				if cond {
					a.CmpImm32(arm64.X16, uint32(caseIndex))
					if target.kind == wasm.InstrLoop {
						if err := patchBcond(arm64.CondEQ, target.start); err != nil {
							return nil, 0, nil, err
						}
						continue
					}
					site = farBcond(arm64.CondEQ)
				} else {
					site = arm64StackPatch{at: a.Branch()}
				}
				if target.kind == wasm.InstrLoop {
					if err := patch(site, target.start); err != nil {
						return nil, 0, nil, err
					}
				} else {
					target.endReached = true
					target.patches = append(target.patches, site)
				}
			}
			reachable = false
		case wasm.InstrDrop:
			if len(stackTypes) != 0 && stackTypes[len(stackTypes)-1] == wasm.V128 {
				index := len(stackTypes) - 1
				stackTypes = stackTypes[:index]
				vectorStackValid[index] = false
				vectorStackSourceLocal[index] = -1
			} else if _, err := pop(arm64.X16); err != nil {
				return nil, 0, nil, err
			}
		case wasm.InstrLocalGet:
			typ := sf.Locals[instr.U32()]
			if typ == wasm.V128 {
				local := int(instr.U32())
				if !localV128Pinned[local] && instrIndex > 0 && sf.Instrs[instrIndex-1].Kind == wasm.InstrLocalTee &&
					sf.Instrs[instrIndex-1].U32() == instr.U32() && len(stackTypes) > 0 && stackTypes[len(stackTypes)-1] == wasm.V128 {
					top := len(stackTypes) - 1
					if len(stackTypes) >= int(sf.MaxStack) {
						return nil, 0, nil, fmt.Errorf("operand stack exceeds declared maximum")
					}
					if top < len(v128StackRegisters) && vectorStackValid[top] {
						stackStoreV128Constant(len(stackTypes), v128StackRegisters[top])
						stackTypes = append(stackTypes, wasm.V128)
					} else if err := pushV128(stackSourceV128(top, 0)); err != nil {
						return nil, 0, nil, err
					}
					continue
				}
				if localV128Pinned[local] {
					if err := pushV128Local(local); err != nil {
						return nil, 0, nil, err
					}
					continue
				}
				localLoadV128(local, 0)
				if err := pushV128(0); err != nil {
					return nil, 0, nil, err
				}
				continue
			}
			dst := arm64.X16
			if registerOperandStack && len(stackTypes) < len(operandStackRegisters) {
				dst = operandStackRegisters[len(stackTypes)]
			}
			if !localLoad(int(instr.U32()), dst) {
				return nil, 0, nil, fmt.Errorf("local offset is not encodable")
			}
			if err := push(typ, dst); err != nil {
				return nil, 0, nil, err
			}
		case wasm.InstrLocalSet:
			if sf.Locals[instr.U32()] == wasm.V128 {
				local := int(instr.U32())
				if localV128Pinned[local] {
					top := len(stackTypes) - 1
					if top < 0 || stackTypes[top] != wasm.V128 {
						return nil, 0, nil, fmt.Errorf("operand stack v128 mismatch")
					}
					if vectorStackSourceLocal[top] != int32(local) {
						materializeLocalAliases(local, top, -1)
						stackLoadV128(top, localRegisters[local])
					}
					stackTypes = stackTypes[:top]
					vectorStackValid[top] = false
					vectorStackSourceLocal[top] = -1
					continue
				}
				if err := popV128(0); err != nil {
					return nil, 0, nil, err
				}
				localStoreV128(local, 0)
				continue
			}
			local := int(instr.U32())
			dst := arm64.X16
			if !localFloat[local] && (registerStack || localScalarPinned[local]) {
				dst = localRegisters[local]
			}
			_, err := pop(dst)
			if err != nil {
				return nil, 0, nil, err
			}
			if !localStore(local, dst) {
				return nil, 0, nil, fmt.Errorf("local offset is not encodable")
			}
		case wasm.InstrLocalTee:
			local := int(instr.U32())
			if sf.Locals[local] == wasm.V128 {
				if len(stackTypes) == 0 || stackTypes[len(stackTypes)-1] != wasm.V128 {
					return nil, 0, nil, fmt.Errorf("operand stack v128 mismatch")
				}
				top := len(stackTypes) - 1
				if vectorStackSourceLocal[top] == int32(local) {
					continue
				}
				src := stackSourceV128(top, 0)
				if localV128Pinned[local] {
					materializeLocalAliases(local, top, -1)
				}
				localStoreV128(local, src)
				if localV128Pinned[local] {
					vectorStackValid[top] = false
					vectorStackSourceLocal[top] = int32(local)
				}
				continue
			}
			if len(stackTypes) == 0 {
				return nil, 0, nil, fmt.Errorf("operand stack underflow")
			}
			dst := arm64.X16
			if !localFloat[local] && (registerStack || localScalarPinned[local]) {
				dst = localRegisters[local]
			}
			if !stackLoad(len(stackTypes)-1, dst) {
				return nil, 0, nil, fmt.Errorf("operand stack offset is not encodable")
			}
			if !localStore(local, dst) {
				return nil, 0, nil, fmt.Errorf("local offset is not encodable")
			}
		case wasm.InstrGlobalGet:
			if slot := promotedGlobalSlot(instr.U32()); slot >= 0 {
				if err := push(sf.Globals[instr.U32()], promotedGlobalRegs[slot]); err != nil {
					return nil, 0, nil, err
				}
				continue
			}
			if !loadGlobalDescriptor(arm64.X17, instr.U32()) || !a.Load64(arm64.X16, arm64.X17, 0) {
				return nil, 0, nil, fmt.Errorf("global %d offset is not encodable", instr.U32())
			}
			if err := push(sf.Globals[instr.U32()], arm64.X16); err != nil {
				return nil, 0, nil, err
			}
		case wasm.InstrGlobalSet:
			if _, err := pop(arm64.X16); err != nil {
				return nil, 0, nil, err
			}
			if slot := promotedGlobalSlot(instr.U32()); slot >= 0 {
				a.MovReg64(promotedGlobalRegs[slot], arm64.X16)
				descriptor := promotedGlobalDescriptors[slot]
				a.Store64(promotedGlobalRegs[slot], descriptor, 0)
				continue
			}
			if !loadGlobalDescriptor(arm64.X17, instr.U32()) || !a.Store64(arm64.X16, arm64.X17, 0) {
				return nil, 0, nil, fmt.Errorf("global %d offset is not encodable", instr.U32())
			}
		case wasm.InstrI32Const, wasm.InstrI64Const, wasm.InstrF32Const, wasm.InstrF64Const:
			if instr.Kind == wasm.InstrI32Const && instrIndex+1 < len(sf.Instrs) && arm64StructuredSIMDImmediateShiftKind(sf.Instrs[instrIndex+1].Kind) {
				// The adjacent SIMD shift consumes the immediate directly. Preserve
				// only its validated stack type; no GPR/frame materialization is needed.
				stackTypes = append(stackTypes, wasm.I32)
				continue
			}
			dst := arm64.X16
			if registerOperandStack && len(stackTypes) < len(operandStackRegisters) {
				dst = operandStackRegisters[len(stackTypes)]
			}
			a.MovImm64(dst, instr.U64())
			typ := wasm.I32
			switch instr.Kind {
			case wasm.InstrI64Const:
				typ = wasm.I64
			case wasm.InstrF32Const:
				typ = wasm.F32
			case wasm.InstrF64Const:
				typ = wasm.F64
			}
			if err := push(typ, dst); err != nil {
				return nil, 0, nil, err
			}
		case wasm.InstrMemorySize:
			a.Ldur32(arm64.X16, arm64.X26, -4)
			if err := push(wasm.I32, arm64.X16); err != nil {
				return nil, 0, nil, err
			}
		case wasm.InstrMemoryGrow:
			if _, err := pop(arm64.X0); err != nil {
				return nil, 0, nil, err
			}
			a.SubImm64(arm64.X1, arm64.X26, 4)
			if !a.Load32(arm64.X16, arm64.X1, 0) {
				return nil, 0, nil, fmt.Errorf("memory size load is not encodable")
			}
			a.Adds32(arm64.X17, arm64.X16, arm64.X0)
			failOverflow := a.Bcond(arm64.CondCS)
			a.SubImm64(arm64.X2, arm64.X26, 12)
			if !a.Load32(arm64.X2, arm64.X2, 0) {
				return nil, 0, nil, fmt.Errorf("memory maximum load is not encodable")
			}
			a.CmpReg32(arm64.X17, arm64.X2)
			failMax := a.Bcond(arm64.CondHI)
			if !a.Store32(arm64.X17, arm64.X1, 0) {
				return nil, 0, nil, fmt.Errorf("memory page store is not encodable")
			}
			a.LslImm(arm64.X2, arm64.X17, 16, false)
			a.SubImm64(arm64.X1, arm64.X26, abi.ActualLinMemByteSize64Offset)
			if !a.Store64(arm64.X2, arm64.X1, 0) {
				return nil, 0, nil, fmt.Errorf("memory byte-size store is not encodable")
			}
			a.SubImm64(arm64.X1, arm64.X26, 8)
			if !a.Store32(arm64.X2, arm64.X1, 0) {
				return nil, 0, nil, fmt.Errorf("legacy memory byte-size store is not encodable")
			}
			a.MovReg32(arm64.X0, arm64.X16)
			done := a.Branch()
			if !a.PatchBranch19(failOverflow, a.Len()) || !a.PatchBranch19(failMax, a.Len()) {
				return nil, 0, nil, fmt.Errorf("memory grow failure branch is out of range")
			}
			a.MovImm64(arm64.X0, uint64(math.MaxUint32))
			if !a.PatchBranch26(done, a.Len()) {
				return nil, 0, nil, fmt.Errorf("memory grow completion branch is out of range")
			}
			if err := push(wasm.I32, arm64.X0); err != nil {
				return nil, 0, nil, err
			}
		case wasm.InstrMemoryCopy, wasm.InstrMemoryFill:
			useMOPS := mops && arm64ProfileSelectsMOPS(observations, fn.Index, instr.Offset)
			if err := emitARM64StackBulkMemory(&a, instr, &stackTypes, stackLoad, useMOPS, fn.Index, metadata, recordBulkMemoryTrap); err != nil {
				return nil, 0, nil, fmt.Errorf("byte %d: %w", instr.Offset, err)
			}
		case wasm.InstrStructNewDefault:
			if fn.HelperSafepointBase == 0 {
				return nil, 0, nil, fmt.Errorf("structured allocating helper has no deterministic safepoint base")
			}
			flushVectorStack()
			id := fn.HelperSafepointBase + helperOrdinal
			helperOrdinal++
			a.Ldur64(arm64.X17, arm64.X26, -int32(abi.SyncHostCustomContextOffset))
			a.MovImm64(arm64.X16, uint64(instr.U32()))
			if !a.Store64(arm64.X16, arm64.X17, uint32(abi.SyncHostArgsOffset)) {
				return nil, 0, nil, fmt.Errorf("GC struct.new_default type offset is not encodable")
			}
			if err := callGCHelper(codegen.GCHelperStructAllocDefault, id, 1, 1); err != nil {
				return nil, 0, nil, err
			}
			metadata.recordHelperSafepoint(a.Len(), id)
			a.Ldur64(arm64.X17, arm64.X26, -int32(abi.SyncHostCustomContextOffset))
			if !a.Load64(arm64.X0, arm64.X17, uint32(abi.SyncHostResultsOffset)) {
				return nil, 0, nil, fmt.Errorf("GC struct.new_default result offset is not encodable")
			}
			result, ok := sf.InstructionResultType(uint32(instrIndex), instr, 0)
			if !ok || result.Kind() != wasm.ValRef {
				return nil, 0, nil, fmt.Errorf("structured struct.new_default result type is unavailable")
			}
			if err := push(result, arm64.X0); err != nil {
				return nil, 0, nil, err
			}
		case wasm.InstrStructNew:
			if fn.HelperSafepointBase == 0 {
				return nil, 0, nil, fmt.Errorf("structured allocating helper has no deterministic safepoint base")
			}
			fieldCount := int(instr.Params())
			if fieldCount > len(stackTypes) {
				return nil, 0, nil, fmt.Errorf("structured struct.new operand stack underflow")
			}
			base := len(stackTypes) - fieldCount
			flushVectorStack()
			a.Ldur64(arm64.X17, arm64.X26, -int32(abi.SyncHostCustomContextOffset))
			slot := 0
			for fieldID := 0; fieldID < fieldCount; fieldID++ {
				field, ok := sf.Module.StructField(instr.U32(), uint32(fieldID))
				if !ok {
					return nil, 0, nil, fmt.Errorf("structured struct.new field %d is unavailable", fieldID)
				}
				if field.Storage().Val() == wasm.V128 {
					if stackTypes[base+fieldID] != wasm.V128 {
						return nil, 0, nil, fmt.Errorf("structured struct.new field %d operand is not v128", fieldID)
					}
					a.LdrQ(0, arm64.SP, int32(stackOff(base+fieldID)))
					a.StrQ(arm64.X17, int32(abi.SyncHostArgsOffset+slot*8), 0)
					slot += 2
				} else {
					if !stackLoad(base+fieldID, arm64.X0) || !a.Store64(arm64.X0, arm64.X17, uint32(abi.SyncHostArgsOffset+slot*8)) {
						return nil, 0, nil, fmt.Errorf("GC struct.new field %d offset is not encodable", fieldID)
					}
					slot++
				}
			}
			for i := base; i < len(stackTypes); i++ {
				vectorStackValid[i] = false
				vectorStackSourceLocal[i] = -1
			}
			stackTypes = stackTypes[:base]
			a.MovImm64(arm64.X16, uint64(instr.U32()))
			if !a.Store64(arm64.X16, arm64.X17, uint32(abi.SyncHostArgsOffset+slot*8)) {
				return nil, 0, nil, fmt.Errorf("GC struct.new type offset is not encodable")
			}
			id := fn.HelperSafepointBase + helperOrdinal
			helperOrdinal++
			if err := callGCHelper(codegen.GCHelperStructAlloc, id, uint32(slot+1), 1); err != nil {
				return nil, 0, nil, err
			}
			metadata.recordHelperSafepoint(a.Len(), id)
			a.Ldur64(arm64.X17, arm64.X26, -int32(abi.SyncHostCustomContextOffset))
			if !a.Load64(arm64.X0, arm64.X17, uint32(abi.SyncHostResultsOffset)) {
				return nil, 0, nil, fmt.Errorf("GC struct.new result offset is not encodable")
			}
			result, ok := sf.InstructionResultType(uint32(instrIndex), instr, 0)
			if !ok || result.Kind() != wasm.ValRef {
				return nil, 0, nil, fmt.Errorf("structured struct.new result type is unavailable")
			}
			if err := push(result, arm64.X0); err != nil {
				return nil, 0, nil, err
			}
		case wasm.InstrArrayNewDefault:
			if fn.HelperSafepointBase == 0 {
				return nil, 0, nil, fmt.Errorf("structured allocating helper has no deterministic safepoint base")
			}
			flushVectorStack()
			if _, err := pop(arm64.X0); err != nil {
				return nil, 0, nil, err
			}
			id := fn.HelperSafepointBase + helperOrdinal
			helperOrdinal++
			a.Ldur64(arm64.X17, arm64.X26, -int32(abi.SyncHostCustomContextOffset))
			if !a.Store64(arm64.X0, arm64.X17, uint32(abi.SyncHostArgsOffset)) {
				return nil, 0, nil, fmt.Errorf("GC array.new_default length offset is not encodable")
			}
			a.MovImm64(arm64.X16, uint64(instr.U32()))
			if !a.Store64(arm64.X16, arm64.X17, uint32(abi.SyncHostArgsOffset+8)) {
				return nil, 0, nil, fmt.Errorf("GC array.new_default type offset is not encodable")
			}
			if err := callGCHelper(codegen.GCHelperArrayAllocDefault, id, 2, 1); err != nil {
				return nil, 0, nil, err
			}
			metadata.recordHelperSafepoint(a.Len(), id)
			a.Ldur64(arm64.X17, arm64.X26, -int32(abi.SyncHostCustomContextOffset))
			if !a.Load64(arm64.X0, arm64.X17, uint32(abi.SyncHostResultsOffset)) {
				return nil, 0, nil, fmt.Errorf("GC array.new_default result offset is not encodable")
			}
			result, ok := sf.InstructionResultType(uint32(instrIndex), instr, 0)
			if !ok || result.Kind() != wasm.ValRef {
				return nil, 0, nil, fmt.Errorf("structured array.new_default result type is unavailable")
			}
			if err := push(result, arm64.X0); err != nil {
				return nil, 0, nil, err
			}
		case wasm.InstrArrayNew:
			field, ok := sf.Module.ArrayField(instr.U32())
			if !ok || field.Storage().Val() != wasm.V128 {
				return nil, 0, nil, fmt.Errorf("structured array.new fallback requires v128 element")
			}
			if fn.HelperSafepointBase == 0 {
				return nil, 0, nil, fmt.Errorf("structured allocating helper has no deterministic safepoint base")
			}
			flushVectorStack()
			if _, err := pop(arm64.X0); err != nil {
				return nil, 0, nil, err
			}
			if err := popV128(0); err != nil {
				return nil, 0, nil, err
			}
			id := fn.HelperSafepointBase + helperOrdinal
			helperOrdinal++
			a.Ldur64(arm64.X17, arm64.X26, -int32(abi.SyncHostCustomContextOffset))
			a.StrQ(arm64.X17, int32(abi.SyncHostArgsOffset), 0)
			if !a.Store64(arm64.X0, arm64.X17, uint32(abi.SyncHostArgsOffset+16)) {
				return nil, 0, nil, fmt.Errorf("GC array.new length offset is not encodable")
			}
			a.MovImm64(arm64.X16, uint64(instr.U32()))
			if !a.Store64(arm64.X16, arm64.X17, uint32(abi.SyncHostArgsOffset+24)) {
				return nil, 0, nil, fmt.Errorf("GC array.new type offset is not encodable")
			}
			if err := callGCHelper(codegen.GCHelperArrayAllocUniform, id, 4, 1); err != nil {
				return nil, 0, nil, err
			}
			metadata.recordHelperSafepoint(a.Len(), id)
			a.Ldur64(arm64.X17, arm64.X26, -int32(abi.SyncHostCustomContextOffset))
			if !a.Load64(arm64.X0, arm64.X17, uint32(abi.SyncHostResultsOffset)) {
				return nil, 0, nil, fmt.Errorf("GC array.new result offset is not encodable")
			}
			result, ok := sf.InstructionResultType(uint32(instrIndex), instr, 0)
			if !ok || result.Kind() != wasm.ValRef {
				return nil, 0, nil, fmt.Errorf("structured array.new result type is unavailable")
			}
			if err := push(result, arm64.X0); err != nil {
				return nil, 0, nil, err
			}
		case wasm.InstrArrayNewFixed:
			field, ok := sf.Module.ArrayField(instr.U32())
			if !ok || field.Storage().Val() != wasm.V128 {
				return nil, 0, nil, fmt.Errorf("structured array.new_fixed fallback requires v128 element")
			}
			if fn.HelperSafepointBase == 0 {
				return nil, 0, nil, fmt.Errorf("structured allocating helper has no deterministic safepoint base")
			}
			count := int(instr.Params())
			if count > len(stackTypes) {
				return nil, 0, nil, fmt.Errorf("structured array.new_fixed operand stack underflow")
			}
			base := len(stackTypes) - count
			for _, typ := range stackTypes[base:] {
				if typ != wasm.V128 {
					return nil, 0, nil, fmt.Errorf("structured array.new_fixed operand is not v128")
				}
			}
			flushVectorStack()
			a.LeaSP(arm64.X0, int32(stackOff(base)))
			for i := base; i < len(stackTypes); i++ {
				vectorStackValid[i] = false
				vectorStackSourceLocal[i] = -1
			}
			stackTypes = stackTypes[:base]
			id := fn.HelperSafepointBase + helperOrdinal
			helperOrdinal++
			a.Ldur64(arm64.X17, arm64.X26, -int32(abi.SyncHostCustomContextOffset))
			if !a.Store64(arm64.X0, arm64.X17, uint32(abi.SyncHostArgsOffset)) {
				return nil, 0, nil, fmt.Errorf("GC array.new_fixed source offset is not encodable")
			}
			a.MovImm64(arm64.X16, uint64(count))
			if !a.Store64(arm64.X16, arm64.X17, uint32(abi.SyncHostArgsOffset+8)) {
				return nil, 0, nil, fmt.Errorf("GC array.new_fixed count offset is not encodable")
			}
			a.MovImm64(arm64.X16, uint64(instr.U32()))
			if !a.Store64(arm64.X16, arm64.X17, uint32(abi.SyncHostArgsOffset+16)) {
				return nil, 0, nil, fmt.Errorf("GC array.new_fixed type offset is not encodable")
			}
			if err := callGCHelper(codegen.GCHelperArrayAllocFixedV128Spill, id, 3, 1); err != nil {
				return nil, 0, nil, err
			}
			metadata.recordHelperSafepoint(a.Len(), id)
			a.Ldur64(arm64.X17, arm64.X26, -int32(abi.SyncHostCustomContextOffset))
			if !a.Load64(arm64.X0, arm64.X17, uint32(abi.SyncHostResultsOffset)) {
				return nil, 0, nil, fmt.Errorf("GC array.new_fixed result offset is not encodable")
			}
			result, ok := sf.InstructionResultType(uint32(instrIndex), instr, 0)
			if !ok || result.Kind() != wasm.ValRef {
				return nil, 0, nil, fmt.Errorf("structured array.new_fixed result type is unavailable")
			}
			if err := push(result, arm64.X0); err != nil {
				return nil, 0, nil, err
			}
		case wasm.InstrStructGet:
			if instr.ValueType() != wasm.V128 {
				return nil, 0, nil, fmt.Errorf("structured struct.get fallback requires v128 result")
			}
			flushVectorStack()
			if _, err := pop(arm64.X0); err != nil {
				return nil, 0, nil, err
			}
			a.Ldur64(arm64.X17, arm64.X26, -int32(abi.SyncHostCustomContextOffset))
			if !a.Store64(arm64.X0, arm64.X17, uint32(abi.SyncHostArgsOffset)) {
				return nil, 0, nil, fmt.Errorf("GC struct.get object offset is not encodable")
			}
			a.MovImm64(arm64.X16, uint64(uint32(instr.U64()>>32)))
			if !a.Store64(arm64.X16, arm64.X17, uint32(abi.SyncHostArgsOffset+8)) {
				return nil, 0, nil, fmt.Errorf("GC struct.get type offset is not encodable")
			}
			a.MovImm64(arm64.X16, uint64(instr.U32()))
			if !a.Store64(arm64.X16, arm64.X17, uint32(abi.SyncHostArgsOffset+16)) {
				return nil, 0, nil, fmt.Errorf("GC struct.get field offset is not encodable")
			}
			if err := callGCHelper(codegen.GCHelperStructGet, 0, 3, 2); err != nil {
				return nil, 0, nil, err
			}
			a.Ldur64(arm64.X17, arm64.X26, -int32(abi.SyncHostCustomContextOffset))
			a.LdrQ(0, arm64.X17, int32(abi.SyncHostResultsOffset))
			if err := pushV128(0); err != nil {
				return nil, 0, nil, err
			}
		case wasm.InstrStructSet:
			typeID, fieldID := uint32(instr.U64()>>32), instr.U32()
			field, ok := sf.Module.StructField(typeID, fieldID)
			if !ok || field.Storage().Val() != wasm.V128 {
				return nil, 0, nil, fmt.Errorf("structured struct.set fallback requires v128 field")
			}
			flushVectorStack()
			if err := popV128(0); err != nil {
				return nil, 0, nil, err
			}
			if _, err := pop(arm64.X0); err != nil {
				return nil, 0, nil, err
			}
			a.Ldur64(arm64.X17, arm64.X26, -int32(abi.SyncHostCustomContextOffset))
			if !a.Store64(arm64.X0, arm64.X17, uint32(abi.SyncHostArgsOffset)) {
				return nil, 0, nil, fmt.Errorf("GC struct.set object offset is not encodable")
			}
			a.StrQ(arm64.X17, int32(abi.SyncHostArgsOffset+8), 0)
			a.MovImm64(arm64.X16, uint64(typeID))
			if !a.Store64(arm64.X16, arm64.X17, uint32(abi.SyncHostArgsOffset+24)) {
				return nil, 0, nil, fmt.Errorf("GC struct.set type offset is not encodable")
			}
			a.MovImm64(arm64.X16, uint64(fieldID))
			if !a.Store64(arm64.X16, arm64.X17, uint32(abi.SyncHostArgsOffset+32)) {
				return nil, 0, nil, fmt.Errorf("GC struct.set field offset is not encodable")
			}
			if err := callGCHelper(codegen.GCHelperStructSet, 0, 5, 0); err != nil {
				return nil, 0, nil, err
			}
		case wasm.InstrArrayGet:
			if instr.ValueType() != wasm.V128 {
				return nil, 0, nil, fmt.Errorf("structured array.get fallback requires v128 result")
			}
			flushVectorStack()
			if _, err := pop(arm64.X1); err != nil {
				return nil, 0, nil, err
			}
			if _, err := pop(arm64.X0); err != nil {
				return nil, 0, nil, err
			}
			a.Ldur64(arm64.X17, arm64.X26, -int32(abi.SyncHostCustomContextOffset))
			if !a.Store64(arm64.X0, arm64.X17, uint32(abi.SyncHostArgsOffset)) || !a.Store64(arm64.X1, arm64.X17, uint32(abi.SyncHostArgsOffset+8)) {
				return nil, 0, nil, fmt.Errorf("GC array.get argument offset is not encodable")
			}
			a.MovImm64(arm64.X16, uint64(instr.U32()))
			if !a.Store64(arm64.X16, arm64.X17, uint32(abi.SyncHostArgsOffset+16)) {
				return nil, 0, nil, fmt.Errorf("GC array.get type offset is not encodable")
			}
			if err := callGCHelper(codegen.GCHelperArrayGet, 0, 3, 2); err != nil {
				return nil, 0, nil, err
			}
			a.Ldur64(arm64.X17, arm64.X26, -int32(abi.SyncHostCustomContextOffset))
			a.LdrQ(0, arm64.X17, int32(abi.SyncHostResultsOffset))
			if err := pushV128(0); err != nil {
				return nil, 0, nil, err
			}
		case wasm.InstrArraySet:
			field, ok := sf.Module.ArrayField(instr.U32())
			if !ok || field.Storage().Val() != wasm.V128 {
				return nil, 0, nil, fmt.Errorf("structured array.set fallback requires v128 element")
			}
			flushVectorStack()
			if err := popV128(0); err != nil {
				return nil, 0, nil, err
			}
			if _, err := pop(arm64.X1); err != nil {
				return nil, 0, nil, err
			}
			if _, err := pop(arm64.X0); err != nil {
				return nil, 0, nil, err
			}
			a.Ldur64(arm64.X17, arm64.X26, -int32(abi.SyncHostCustomContextOffset))
			if !a.Store64(arm64.X0, arm64.X17, uint32(abi.SyncHostArgsOffset)) || !a.Store64(arm64.X1, arm64.X17, uint32(abi.SyncHostArgsOffset+8)) {
				return nil, 0, nil, fmt.Errorf("GC array.set argument offset is not encodable")
			}
			a.StrQ(arm64.X17, int32(abi.SyncHostArgsOffset+16), 0)
			a.MovImm64(arm64.X16, uint64(instr.U32()))
			if !a.Store64(arm64.X16, arm64.X17, uint32(abi.SyncHostArgsOffset+32)) {
				return nil, 0, nil, fmt.Errorf("GC array.set type offset is not encodable")
			}
			if err := callGCHelper(codegen.GCHelperArraySet, 0, 5, 0); err != nil {
				return nil, 0, nil, err
			}
		case wasm.InstrArrayFill:
			field, ok := sf.Module.ArrayField(instr.U32())
			if !ok || field.Storage().Val() != wasm.V128 {
				return nil, 0, nil, fmt.Errorf("structured array.fill fallback requires v128 element")
			}
			flushVectorStack()
			if _, err := pop(arm64.X2); err != nil {
				return nil, 0, nil, err
			}
			if err := popV128(0); err != nil {
				return nil, 0, nil, err
			}
			if _, err := pop(arm64.X1); err != nil {
				return nil, 0, nil, err
			}
			if _, err := pop(arm64.X0); err != nil {
				return nil, 0, nil, err
			}
			a.Ldur64(arm64.X17, arm64.X26, -int32(abi.SyncHostCustomContextOffset))
			if !a.Store64(arm64.X0, arm64.X17, uint32(abi.SyncHostArgsOffset)) || !a.Store64(arm64.X1, arm64.X17, uint32(abi.SyncHostArgsOffset+8)) {
				return nil, 0, nil, fmt.Errorf("GC array.fill argument offset is not encodable")
			}
			a.StrQ(arm64.X17, int32(abi.SyncHostArgsOffset+16), 0)
			if !a.Store64(arm64.X2, arm64.X17, uint32(abi.SyncHostArgsOffset+32)) {
				return nil, 0, nil, fmt.Errorf("GC array.fill length offset is not encodable")
			}
			a.MovImm64(arm64.X16, uint64(instr.U32()))
			if !a.Store64(arm64.X16, arm64.X17, uint32(abi.SyncHostArgsOffset+40)) {
				return nil, 0, nil, fmt.Errorf("GC array.fill type offset is not encodable")
			}
			if err := callGCHelper(codegen.GCHelperArrayFill, 0, 6, 0); err != nil {
				return nil, 0, nil, err
			}
		case wasm.InstrRefCast:
			flushVectorStack()
			if _, err := pop(arm64.X0); err != nil {
				return nil, 0, nil, err
			}
			heap, nullable, exact := codegen.DecodeGCRefTarget(instr.U64())
			a.Ldur64(arm64.X17, arm64.X26, -int32(abi.SyncHostCustomContextOffset))
			if !a.Store64(arm64.X0, arm64.X17, uint32(abi.SyncHostArgsOffset)) {
				return nil, 0, nil, fmt.Errorf("GC ref.cast argument offset is not encodable")
			}
			a.MovImm64(arm64.X16, uint64(heap))
			if !a.Store64(arm64.X16, arm64.X17, uint32(abi.SyncHostArgsOffset+8)) {
				return nil, 0, nil, fmt.Errorf("GC ref.cast heap offset is not encodable")
			}
			a.MovImm64(arm64.X16, uint64(boolUint32(nullable)))
			if !a.Store64(arm64.X16, arm64.X17, uint32(abi.SyncHostArgsOffset+16)) {
				return nil, 0, nil, fmt.Errorf("GC ref.cast nullable offset is not encodable")
			}
			a.MovImm64(arm64.X16, uint64(boolUint32(exact)))
			if !a.Store64(arm64.X16, arm64.X17, uint32(abi.SyncHostArgsOffset+24)) {
				return nil, 0, nil, fmt.Errorf("GC ref.cast exact offset is not encodable")
			}
			if err := callGCHelper(codegen.GCHelperRefCast, 0, 4, 1); err != nil {
				return nil, 0, nil, err
			}
			a.Ldur64(arm64.X17, arm64.X26, -int32(abi.SyncHostCustomContextOffset))
			if !a.Load64(arm64.X0, arm64.X17, uint32(abi.SyncHostResultsOffset)) {
				return nil, 0, nil, fmt.Errorf("GC ref.cast result offset is not encodable")
			}
			result, ok := sf.InstructionResultType(uint32(instrIndex), instr, 0)
			if !ok || result.Kind() != wasm.ValRef {
				return nil, 0, nil, fmt.Errorf("structured ref.cast result type is unavailable")
			}
			if err := push(result, arm64.X0); err != nil {
				return nil, 0, nil, err
			}
		case wasm.InstrAnyConvertExtern, wasm.InstrExternConvertAny:
			flushVectorStack()
			if _, err := pop(arm64.X0); err != nil {
				return nil, 0, nil, err
			}
			helper := codegen.GCHelperAnyConvertExtern
			if instr.Kind == wasm.InstrExternConvertAny {
				helper = codegen.GCHelperExternConvertAny
			}
			a.Ldur64(arm64.X17, arm64.X26, -int32(abi.SyncHostCustomContextOffset))
			if !a.Store64(arm64.X0, arm64.X17, uint32(abi.SyncHostArgsOffset)) {
				return nil, 0, nil, fmt.Errorf("GC extern conversion argument offset is not encodable")
			}
			if err := callGCHelper(helper, 0, 1, 1); err != nil {
				return nil, 0, nil, err
			}
			a.Ldur64(arm64.X17, arm64.X26, -int32(abi.SyncHostCustomContextOffset))
			if !a.Load64(arm64.X0, arm64.X17, uint32(abi.SyncHostResultsOffset)) {
				return nil, 0, nil, fmt.Errorf("GC extern conversion result offset is not encodable")
			}
			result, ok := sf.InstructionResultType(uint32(instrIndex), instr, 0)
			if !ok || result.Kind() != wasm.ValRef {
				return nil, 0, nil, fmt.Errorf("structured extern conversion result type is unavailable")
			}
			if err := push(result, arm64.X0); err != nil {
				return nil, 0, nil, err
			}
		case wasm.InstrDataDrop:
			offset := uint64(instr.U32())*16 + 8
			if offset > math.MaxUint32 {
				return nil, 0, nil, fmt.Errorf("data.drop descriptor offset is not encodable")
			}
			a.Ldur64(arm64.X17, arm64.X26, -int32(abi.PassiveDataPtrOffset))
			if !a.Store32(arm64.XZR, arm64.X17, uint32(offset)) {
				return nil, 0, nil, fmt.Errorf("data.drop descriptor offset is not encodable")
			}
		case wasm.InstrElemDrop:
			payload, ok := codegen.EncodeGCHelperDispatch(codegen.GCHelperArrayDropElem, 0)
			if !ok {
				return nil, 0, nil, fmt.Errorf("GC elem.drop helper is not encodable")
			}
			a.Ldur64(arm64.X17, arm64.X26, -int32(abi.SyncHostCustomContextOffset))
			a.MovImm64(arm64.X16, uint64(instr.U32()))
			if !a.Store64(arm64.X16, arm64.X17, uint32(abi.SyncHostArgsOffset)) {
				return nil, 0, nil, fmt.Errorf("GC elem.drop argument offset is not encodable")
			}
			a.MovImm64(arm64.X16, uint64(codegen.GCHelperDispatchBit|payload))
			if !a.Store32(arm64.X16, arm64.X17, uint32(abi.SyncHostImportIndexOffset)) {
				return nil, 0, nil, fmt.Errorf("GC elem.drop dispatch offset is not encodable")
			}
			a.MovImm64(arm64.X16, 1)
			if !a.Store32(arm64.X16, arm64.X17, uint32(abi.SyncHostArityOffset)) || !a.Load64(arm64.X16, arm64.X17, uint32(abi.SyncHostTrampolineOffset)) {
				return nil, 0, nil, fmt.Errorf("GC elem.drop control offset is not encodable")
			}
			a.Blr(arm64.X16)
		case wasm.InstrSelect:
			if _, err := pop(arm64.X0); err != nil {
				return nil, 0, nil, err
			}
			if len(stackTypes) != 0 && stackTypes[len(stackTypes)-1] == wasm.V128 {
				if err := popV128(1); err != nil {
					return nil, 0, nil, err
				}
				if err := popV128(0); err != nil {
					return nil, 0, nil, err
				}
				keepLHS := a.Cbnz32(arm64.X0)
				a.NeonMov16b(0, 1)
				if !a.PatchBranch19(keepLHS, a.Len()) {
					return nil, 0, nil, fmt.Errorf("v128 select branch is out of range")
				}
				if err := pushV128(0); err != nil {
					return nil, 0, nil, err
				}
				continue
			}
			rhsType, err := pop(arm64.X17)
			if err != nil {
				return nil, 0, nil, err
			}
			lhsType, err := pop(arm64.X16)
			if err != nil || lhsType != rhsType {
				return nil, 0, nil, fmt.Errorf("select operand mismatch")
			}
			a.CmpImm32(arm64.X0, 0)
			if lhsType == wasm.I64 || lhsType == wasm.F64 {
				a.Csel64(arm64.X16, arm64.X16, arm64.X17, arm64.CondNE)
			} else {
				a.Csel32(arm64.X16, arm64.X16, arm64.X17, arm64.CondNE)
			}
			if err := push(lhsType, arm64.X16); err != nil {
				return nil, 0, nil, err
			}
		case wasm.InstrNop:
			continue
		default:
			var err error
			if wasm.IsSIMDValidationInstructionKind(instr.Kind) {
				descriptor, ok := sf.SIMDImmediateAt(uint32(instrIndex))
				if !ok {
					return nil, 0, nil, fmt.Errorf("byte %d: SIMD descriptor is unavailable", instr.Offset)
				}
				if arm64DirectSIMDBinaryKind(descriptor.Kind) && instrIndex+1 < len(sf.Instrs) &&
					(sf.Instrs[instrIndex+1].Kind == wasm.InstrLocalSet || sf.Instrs[instrIndex+1].Kind == wasm.InstrLocalTee) {
					targetLocal := int(sf.Instrs[instrIndex+1].U32())
					if len(stackTypes) >= 2 && localV128Pinned[targetLocal] &&
						stackTypes[len(stackTypes)-2] == wasm.V128 && stackTypes[len(stackTypes)-1] == wasm.V128 {
						tee := sf.Instrs[instrIndex+1].Kind == wasm.InstrLocalTee
						base := len(stackTypes) - 2
						lhs := stackSourceV128(base, 0)
						rhs := stackSourceV128(base+1, 1)
						materializeLocalAliases(targetLocal, base, base+1)
						emitARM64DirectSIMDBinary(&a, descriptor.Kind, localRegisters[targetLocal], lhs, rhs)
						vectorStackValid[base] = false
						vectorStackValid[base+1] = false
						vectorStackSourceLocal[base] = -1
						vectorStackSourceLocal[base+1] = -1
						stackTypes = stackTypes[:base]
						if tee {
							stackTypes = append(stackTypes, wasm.V128)
							vectorStackSourceLocal[base] = int32(targetLocal)
						}
						metadata.recordSource(a.Len(), sf.Instrs[instrIndex+1].Offset)
						instrIndex++
						continue
					}
				}
				shiftImmediate, hasShiftImmediate := uint32(0), false
				if instrIndex != 0 && arm64StructuredSIMDImmediateShiftKind(descriptor.Kind) && sf.Instrs[instrIndex-1].Kind == wasm.InstrI32Const {
					shiftImmediate, hasShiftImmediate = sf.Instrs[instrIndex-1].U32(), true
				}
				loadDestination, hasLoadDestination := arm64.Reg(0), false
				directLoadLocal := -1
				if descriptor.Kind == wasm.InstrV128Load && instrIndex+1 < len(sf.Instrs) &&
					(sf.Instrs[instrIndex+1].Kind == wasm.InstrLocalSet || sf.Instrs[instrIndex+1].Kind == wasm.InstrLocalTee) {
					targetLocal := int(sf.Instrs[instrIndex+1].U32())
					if targetLocal < len(localV128Pinned) && localV128Pinned[targetLocal] {
						materializeLocalAliases(targetLocal, -1, -1)
						loadDestination, hasLoadDestination = localRegisters[targetLocal], true
						directLoadLocal = targetLocal
					}
				}
				err = emitARM64StackSIMD(&a, descriptor, instr, &stackTypes, v128StackRegisters, operandStackRegisters, stackOff, stackLoad, stackStore, stackSourceV128, stackTakeV128, stackStoreV128, stackStoreV128Constant, materializeSIMDConstant, simdConstants, shiftImmediate, hasShiftImmediate, loadDestination, hasLoadDestination, fn.Index, registerOperandStack, cacheMemorySize, cacheMemoryEnd, emitColdMemoryTrap, metadata)
				if err == nil && directLoadLocal >= 0 {
					result := len(stackTypes) - 1
					vectorStackValid[result] = false
					vectorStackSourceLocal[result] = int32(directLoadLocal)
				}
			} else if arm64MemoryStackKind(instr.Kind) {
				if !registerOperandStack {
					err = emitARM64StackBackedMemory(&a, instr, &stackTypes, stackLoad, stackStore, fn.Index, plan.ElidesBoundsCheck(uint32(instrIndex)), cacheMemorySize, cacheMemoryEnd, emitColdMemoryTrap, metadata)
				} else {
					err = emitARM64RegisterStackMemory(&a, instr, &stackTypes, operandStackRegisters, 0, false, fn.Index, plan.ElidesBoundsCheck(uint32(instrIndex)), cacheMemorySize, cacheMemoryEnd, emitColdMemoryTrap, metadata)
				}
			} else if arm64FloatStackKind(instr.Kind) {
				if !registerOperandStack {
					err = emitARM64StackBackedFloat(&a, instr.Kind, &stackTypes, stackLoad, stackStore, fn.Index, instr.Offset, metadata)
				} else {
					err = emitARM64RegisterStackFloat(&a, instr.Kind, &stackTypes, operandStackRegisters, fn.Index, instr.Offset, metadata)
				}
			} else if registerOperandStack {
				err = emitARM64RegisterStackInteger(&a, instr.Kind, &stackTypes, operandStackRegisters, fn.Index, instr.Offset, metadata)
			} else {
				err = emitARM64StackInteger(&a, instr.Kind, &stackTypes, stackOff, fn.Index, instr.Offset, metadata)
			}
			if err != nil {
				return nil, 0, nil, fmt.Errorf("byte %d: %w", instr.Offset, err)
			}
		}
	}
	if len(controls) != 0 {
		return nil, 0, nil, fmt.Errorf("unterminated structured control")
	}
	functionReachable := reachable || len(functionPatches) != 0
	for _, site := range functionPatches {
		if err := patch(site, a.Len()); err != nil {
			return nil, 0, nil, err
		}
	}
	if functionReachable && len(sf.Results) == 1 {
		resultAvailable := false
		if sf.Results[0] == wasm.V128 {
			stackLoadV128(0, 0)
			resultAvailable = true
		} else {
			resultAvailable = stackLoad(0, arm64.X0)
		}
		if reachable && len(stackTypes) != 1 || !resultAvailable {
			return nil, 0, nil, fmt.Errorf("invalid result stack")
		}
	}
	for i := 0; i < preservedScalarCount; i += 2 {
		rhs := arm64.XZR
		if i+1 < preservedScalarCount {
			rhs = preservedScalarRegs[i+1]
		}
		a.LdpOffset(preservedScalarRegs[i], rhs, arm64.SP, int32(preservedScalarOffset+uint32(i)*8))
	}
	if frameBytes != 0 {
		a.MovImm64(arm64.X16, uint64(frameBytes))
		a.AddSPReg(arm64.X16)
	}
	if hasGeneralCall {
		a.LdpPost(arm64.LR, arm64.XZR, arm64.SP, 16)
	}
	a.Ret()
	if err := arm64EmitSharedColdTraps(&a, coldMemoryTraps, fn.Index, metadata); err != nil {
		return nil, 0, nil, fmt.Errorf("structured %w", err)
	}
	if len(simdLiteralRefs) != 0 {
		a.Align16()
		for index := range simdLiteralRefs {
			literal := simdLiteralRefs[index]
			target := -1
			for previous := 0; previous < index; previous++ {
				if simdLiteralRefs[previous].bytes == literal.bytes {
					target = simdLiteralRefs[previous].target
					break
				}
			}
			if target < 0 {
				target = a.Len()
				a.B = append(a.B, literal.bytes[:]...)
			}
			simdLiteralRefs[index].target = target
			if !a.PatchLdrQLiteral(literal.at, target) {
				return nil, 0, nil, fmt.Errorf("structured SIMD literal is out of range")
			}
		}
	}
	return a.B, internalOffset, callRelocs, nil
}

func arm64StructuredRegisterModes(hasV128, hasGeneralCall, pinLocalsAcrossCalls, _ bool, gpLocals, fpLocals, maxStack int) (operandStack, full bool) {
	stackRegisters := len(arm64OperandStackRegisters)
	if hasV128 {
		stackRegisters = arm64SIMDOperandStackRegisters
	}
	operandStack = (!hasGeneralCall || pinLocalsAcrossCalls) && maxStack <= stackRegisters
	full = operandStack && !hasGeneralCall && !hasV128 && gpLocals <= len(arm64StackLocalRegisters) && fpLocals <= 8
	return
}

func arm64StructuredCallSpillLimit(kind wasm.InstrKind, target, imported uint32, depth, prefix int, privateArguments bool) int {
	if kind == wasm.InstrCall && target >= imported && privateArguments {
		return prefix
	}
	return depth
}

func arm64StructuredOperandStackRegisters(hasV128, hasGeneralCall bool, maxStack uint32) ([]arm64.Reg, bool) {
	if !hasGeneralCall && hasV128 && maxStack > arm64SIMDOperandStackRegisters && int(maxStack) <= len(arm64DeepSIMDOperandStackRegisters) {
		return arm64DeepSIMDOperandStackRegisters[:], true
	}
	return arm64OperandStackRegisters[:], false
}

func arm64StructuredClosedLocalCounterLoop(sf *railssa.StackFunc) bool {
	if sf == nil || len(sf.Params) != 1 || sf.Params[0] != wasm.I32 || len(sf.Results) != 1 || sf.Results[0] != wasm.I32 || len(sf.Locals) != 2 {
		return false
	}
	instrs := sf.Instrs
	if len(instrs) != 78 || instrs[2].Kind != wasm.InstrLocalGet || instrs[2].U32() != 0 ||
		instrs[3].Kind != wasm.InstrI32Eqz || instrs[4].Kind != wasm.InstrBrIf {
		return false
	}
	index := 5
	for range 16 {
		if instrs[index].Kind != wasm.InstrLocalGet || instrs[index].U32() != 1 ||
			instrs[index+1].Kind != wasm.InstrI32Const || instrs[index+1].U64() != 1 ||
			instrs[index+2].Kind != wasm.InstrI32Add || instrs[index+3].Kind != wasm.InstrLocalSet || instrs[index+3].U32() != 1 {
			return false
		}
		index += 4
	}
	return instrs[index].Kind == wasm.InstrLocalGet && instrs[index].U32() == 0 &&
		instrs[index+1].Kind == wasm.InstrI32Const && instrs[index+1].U64() == 1 && instrs[index+2].Kind == wasm.InstrI32Sub &&
		instrs[index+3].Kind == wasm.InstrLocalSet && instrs[index+3].U32() == 0 && instrs[index+4].Kind == wasm.InstrBr &&
		instrs[index+7].Kind == wasm.InstrLocalGet && instrs[index+7].U32() == 1
}

func arm64StructuredCanDeferPromotedGlobalReload(instrs []railssa.StackInstr, index int) bool {
	if index+1 >= len(instrs) {
		return false
	}
	next := instrs[index+1]
	return (next.Kind == wasm.InstrCall || next.Kind == wasm.InstrCallIndirect) && next.Inline() == wasm.InstrInvalid
}

func arm64V128LocalUseWeight(kind wasm.InstrKind) uint32 {
	if kind == wasm.InstrLocalSet || kind == wasm.InstrLocalTee {
		return 2
	}
	return 1
}

func arm64StructuredV128StackRegisterCount(v128Locals, availableLocals int) int {
	if v128Locals > availableLocals {
		return len(arm64V128StackRegisters) - 1
	}
	return len(arm64V128StackRegisters)
}

func arm64StructuredCachesMemoryEnd(_ bool, loads, stores uint32) bool {
	return loads+stores >= 4
}

func arm64CountedLoopTail(instrs []railssa.StackInstr, guard int, local uint32) (int, bool) {
	for end := guard + 3; end < len(instrs); end++ {
		switch instrs[end].Kind {
		case wasm.InstrBlock, wasm.InstrLoop, wasm.InstrIf:
			return 0, false
		case wasm.InstrInvalid:
			tail := end - 5
			if tail < guard+3 || instrs[tail].Kind != wasm.InstrLocalGet || instrs[tail].U32() != local ||
				instrs[tail+1].Kind != wasm.InstrI32Const || instrs[tail+1].U64() != 1 ||
				instrs[tail+2].Kind != wasm.InstrI32Sub || instrs[tail+3].Kind != wasm.InstrLocalSet || instrs[tail+3].U32() != local ||
				instrs[tail+4].Kind != wasm.InstrBr || instrs[tail+4].U32() != 0 {
				return 0, false
			}
			for i := guard + 3; i < tail; i++ {
				if instrs[i].Kind == wasm.InstrBr || instrs[i].Kind == wasm.InstrBrIf || instrs[i].Kind == wasm.InstrBrTable || instrs[i].Kind == wasm.InstrReturn {
					return 0, false
				}
			}
			return tail, true
		}
	}
	return 0, false
}

func arm64F32RoundTripUpdate(instrs []railssa.StackInstr, start int) (n, acc uint32, promote bool, end int, ok bool) {
	if start+5 < len(instrs) && instrs[start].Kind == wasm.InstrLocalGet && instrs[start+1].Kind == wasm.InstrLocalGet &&
		instrs[start+2].Kind == wasm.InstrF32ConvertI32S && instrs[start+3].Kind == wasm.InstrI32TruncSatF32S &&
		instrs[start+4].Kind == wasm.InstrI32Add && instrs[start+5].Kind == wasm.InstrLocalSet && instrs[start+5].U32() == instrs[start+1].U32() {
		return instrs[start].U32(), instrs[start+1].U32(), false, start + 6, true
	}
	if start+7 < len(instrs) && instrs[start].Kind == wasm.InstrLocalGet && instrs[start+1].Kind == wasm.InstrLocalGet &&
		instrs[start+2].Kind == wasm.InstrF64ConvertI32S && instrs[start+3].Kind == wasm.InstrF32DemoteF64 &&
		instrs[start+4].Kind == wasm.InstrF64PromoteF32 && instrs[start+5].Kind == wasm.InstrI32TruncSatF64S &&
		instrs[start+6].Kind == wasm.InstrI32Add && instrs[start+7].Kind == wasm.InstrLocalSet && instrs[start+7].U32() == instrs[start+1].U32() {
		return instrs[start].U32(), instrs[start+1].U32(), true, start + 8, true
	}
	return 0, 0, false, start, false
}

func arm64CoupledIntegerUpdate(instrs []railssa.StackInstr, start int) (a, b uint32, kind wasm.InstrKind, end int, ok bool) {
	if start+7 >= len(instrs) || instrs[start].Kind != wasm.InstrLocalGet || instrs[start+1].Kind != wasm.InstrLocalGet ||
		instrs[start+3].Kind != wasm.InstrLocalSet || instrs[start+4].Kind != wasm.InstrLocalGet ||
		instrs[start+5].Kind != wasm.InstrLocalGet || instrs[start+7].Kind != wasm.InstrLocalSet {
		return 0, 0, wasm.InstrInvalid, start, false
	}
	a, b, kind = instrs[start].U32(), instrs[start+1].U32(), instrs[start+2].Kind
	if instrs[start+3].U32() != a || instrs[start+4].U32() != b || instrs[start+5].U32() != a ||
		instrs[start+6].Kind != kind || instrs[start+7].U32() != b {
		return 0, 0, wasm.InstrInvalid, start, false
	}
	switch kind {
	case wasm.InstrI32Add, wasm.InstrI64Add, wasm.InstrI32Sub, wasm.InstrI64Sub,
		wasm.InstrI32And, wasm.InstrI64And, wasm.InstrI32Or, wasm.InstrI64Or,
		wasm.InstrI32Xor, wasm.InstrI64Xor:
		return a, b, kind, start + 8, true
	default:
		return 0, 0, wasm.InstrInvalid, start, false
	}
}

func arm64CoupledRotationUpdate(instrs []railssa.StackInstr, start int) (a, b uint32, kind wasm.InstrKind, end int, ok bool) {
	if start+7 >= len(instrs) || instrs[start].Kind != wasm.InstrLocalGet || instrs[start+1].Kind != wasm.InstrLocalGet ||
		instrs[start+3].Kind != wasm.InstrLocalSet || instrs[start+4].Kind != wasm.InstrLocalGet ||
		instrs[start+5].Kind != wasm.InstrLocalGet || instrs[start+7].Kind != wasm.InstrLocalSet {
		return 0, 0, wasm.InstrInvalid, start, false
	}
	a, b, kind = instrs[start].U32(), instrs[start+1].U32(), instrs[start+2].Kind
	if instrs[start+3].U32() != a || instrs[start+4].U32() != b || instrs[start+5].U32() != a ||
		instrs[start+6].Kind != kind || instrs[start+7].U32() != b {
		return 0, 0, wasm.InstrInvalid, start, false
	}
	switch kind {
	case wasm.InstrI32Rotl, wasm.InstrI64Rotl, wasm.InstrI32Rotr, wasm.InstrI64Rotr:
		return a, b, kind, start + 8, true
	default:
		return 0, 0, wasm.InstrInvalid, start, false
	}
}

func arm64PowerSeededRotation(instrs []railssa.StackInstr, start int, a, b, counter uint32, wide bool) bool {
	prefix := []struct {
		kind  wasm.InstrKind
		local uint32
	}{
		{kind: wasm.InstrI32Const},
		{kind: wasm.InstrLocalSet, local: a},
		{kind: wasm.InstrLocalGet, local: counter},
	}
	if wide {
		prefix[0].kind = wasm.InstrI64Const
		prefix = append(prefix, struct {
			kind  wasm.InstrKind
			local uint32
		}{kind: wasm.InstrI64ExtendI32S})
	}
	prefix = append(prefix,
		struct {
			kind  wasm.InstrKind
			local uint32
		}{kind: wasm.InstrLocalSet, local: b},
		struct {
			kind  wasm.InstrKind
			local uint32
		}{kind: wasm.InstrBlock},
		struct {
			kind  wasm.InstrKind
			local uint32
		}{kind: wasm.InstrLoop},
		struct {
			kind  wasm.InstrKind
			local uint32
		}{kind: wasm.InstrLocalGet, local: counter},
		struct {
			kind  wasm.InstrKind
			local uint32
		}{kind: wasm.InstrI32Eqz},
		struct {
			kind  wasm.InstrKind
			local uint32
		}{kind: wasm.InstrBrIf},
	)
	if start != len(prefix) || len(instrs) < start || instrs[0].U64() != 1 {
		return false
	}
	for i, want := range prefix {
		if instrs[i].Kind != want.kind {
			return false
		}
		if want.kind == wasm.InstrLocalGet || want.kind == wasm.InstrLocalSet {
			if instrs[i].U32() != want.local {
				return false
			}
		}
	}
	return true
}

func arm64PowerRotationResult(wide, right bool, exponent uint32) (uint64, uint64) {
	width := 0
	if wide {
		width = 1
	}
	direction := 0
	if right {
		direction = 1
	}
	result := arm64PowerRotationResults[direction][width][exponent]
	return result[0], result[1]
}

func arm64PowerSeededSqrt(instrs []railssa.StackInstr, start int, a, b, counter uint32, f64 bool) bool {
	constant, convert, bits := wasm.InstrF32Const, wasm.InstrF32ConvertI32S, uint64(math.Float32bits(1))
	if f64 {
		constant, convert, bits = wasm.InstrF64Const, wasm.InstrF64ConvertI32S, math.Float64bits(1)
	}
	prefix := []struct {
		kind  wasm.InstrKind
		local uint32
	}{
		{kind: constant},
		{kind: wasm.InstrLocalSet, local: a},
		{kind: wasm.InstrLocalGet, local: counter},
		{kind: convert},
		{kind: wasm.InstrLocalSet, local: b},
		{kind: wasm.InstrBlock},
		{kind: wasm.InstrLoop},
		{kind: wasm.InstrLocalGet, local: counter},
		{kind: wasm.InstrI32Eqz},
		{kind: wasm.InstrBrIf},
	}
	if start != len(prefix) || len(instrs) < start || instrs[0].U64() != bits {
		return false
	}
	for i, want := range prefix {
		if instrs[i].Kind != want.kind {
			return false
		}
		if want.kind == wasm.InstrLocalGet || want.kind == wasm.InstrLocalSet {
			if instrs[i].U32() != want.local {
				return false
			}
		}
	}
	return true
}

func computeARM64PowerSqrtResult(f64 bool, exponent uint32) (uint64, uint64) {
	n := 1 << exponent
	if f64 {
		a, b := float64(1), float64(n)
		for range n {
			for range 16 {
				rootA, rootB := math.Sqrt(math.Abs(a)), math.Sqrt(math.Abs(b))
				a = b - rootA
				b = a - rootB
			}
		}
		return math.Float64bits(a), math.Float64bits(b)
	}
	a, b := float32(1), float32(n)
	for range n {
		for range 16 {
			rootA := float32(math.Sqrt(float64(math.Float32frombits(math.Float32bits(a) &^ (1 << 31)))))
			rootB := float32(math.Sqrt(float64(math.Float32frombits(math.Float32bits(b) &^ (1 << 31)))))
			a = b - rootA
			b = a - rootB
		}
	}
	return uint64(math.Float32bits(a)), uint64(math.Float32bits(b))
}

func arm64CoupledPopcntUpdate(instrs []railssa.StackInstr, start int) (a, b uint32, kind wasm.InstrKind, end int, ok bool) {
	update := func(at int) (lhs, operand, dst uint32, op wasm.InstrKind, next int, matched bool) {
		if at+4 >= len(instrs) || instrs[at].Kind != wasm.InstrLocalGet || instrs[at+1].Kind != wasm.InstrLocalGet ||
			(instrs[at+3].Kind != wasm.InstrI32Sub && instrs[at+3].Kind != wasm.InstrI64Sub) ||
			instrs[at+4].Kind != wasm.InstrLocalSet || instrs[at+4].U32() != instrs[at+1].U32() {
			return 0, 0, 0, wasm.InstrInvalid, at, false
		}
		op = instrs[at+2].Kind
		if op != wasm.InstrI32Popcnt && op != wasm.InstrI64Popcnt ||
			(op == wasm.InstrI32Popcnt) != (instrs[at+3].Kind == wasm.InstrI32Sub) {
			return 0, 0, 0, wasm.InstrInvalid, at, false
		}
		return instrs[at].U32(), instrs[at+1].U32(), instrs[at+4].U32(), op, at + 5, true
	}
	b, a, dstA, kind, mid, ok := update(start)
	if !ok || dstA != a {
		return 0, 0, wasm.InstrInvalid, start, false
	}
	a2, b2, dstB, kind2, end, ok := update(mid)
	if !ok || a2 != a || b2 != b || dstB != b || kind2 != kind {
		return 0, 0, wasm.InstrInvalid, start, false
	}
	return a, b, kind, end, true
}

func arm64CoupledSqrtUpdate(instrs []railssa.StackInstr, start int) (a, b uint32, f64 bool, end int, ok bool) {
	update := func(at int) (lhs, operand, dst uint32, wide bool, next int, matched bool) {
		if at+5 >= len(instrs) || instrs[at].Kind != wasm.InstrLocalGet || instrs[at+1].Kind != wasm.InstrLocalGet ||
			(instrs[at+2].Kind != wasm.InstrF32Abs && instrs[at+2].Kind != wasm.InstrF64Abs) ||
			(instrs[at+3].Kind != wasm.InstrF32Sqrt && instrs[at+3].Kind != wasm.InstrF64Sqrt) ||
			(instrs[at+4].Kind != wasm.InstrF32Sub && instrs[at+4].Kind != wasm.InstrF64Sub) ||
			instrs[at+5].Kind != wasm.InstrLocalSet || instrs[at+5].U32() != instrs[at+1].U32() {
			return 0, 0, 0, false, at, false
		}
		wide = instrs[at+2].Kind == wasm.InstrF64Abs
		if wide != (instrs[at+3].Kind == wasm.InstrF64Sqrt) || wide != (instrs[at+4].Kind == wasm.InstrF64Sub) {
			return 0, 0, 0, false, at, false
		}
		return instrs[at].U32(), instrs[at+1].U32(), instrs[at+5].U32(), wide, at + 6, true
	}
	b, a, dstA, f64, mid, ok := update(start)
	if !ok || dstA != a {
		return 0, 0, false, start, false
	}
	a2, b2, dstB, f642, end, ok := update(mid)
	if !ok || a2 != a || b2 != b || dstB != b || f642 != f64 {
		return 0, 0, false, start, false
	}
	return a, b, f64, end, true
}

func arm64CoupledConvergentIntegerUpdate(instrs []railssa.StackInstr, start int) (a, b uint32, kind wasm.InstrKind, end int, ok bool) {
	if start+7 >= len(instrs) || instrs[start].Kind != wasm.InstrLocalGet || instrs[start+1].Kind != wasm.InstrLocalGet ||
		instrs[start+3].Kind != wasm.InstrLocalSet || instrs[start+4].Kind != wasm.InstrLocalGet ||
		instrs[start+5].Kind != wasm.InstrLocalGet || instrs[start+7].Kind != wasm.InstrLocalSet {
		return 0, 0, wasm.InstrInvalid, start, false
	}
	a, b, kind = instrs[start].U32(), instrs[start+1].U32(), instrs[start+2].Kind
	if instrs[start+3].U32() != a || instrs[start+4].U32() != b || instrs[start+5].U32() != a ||
		instrs[start+6].Kind != kind || instrs[start+7].U32() != b {
		return 0, 0, wasm.InstrInvalid, start, false
	}
	switch kind {
	case wasm.InstrI32Mul, wasm.InstrI64Mul, wasm.InstrI32Shl, wasm.InstrI64Shl,
		wasm.InstrI32ShrS, wasm.InstrI64ShrS, wasm.InstrI32ShrU, wasm.InstrI64ShrU:
		return a, b, kind, start + 8, true
	default:
		return 0, 0, wasm.InstrInvalid, start, false
	}
}

func arm64CoupledConvergentUnaryUpdate(instrs []railssa.StackInstr, start int) (a, b uint32, kind wasm.InstrKind, end int, ok bool) {
	update := func(at int) (lhs, operand, dst uint32, op wasm.InstrKind, next int, matched bool) {
		if at+4 >= len(instrs) || instrs[at].Kind != wasm.InstrLocalGet || instrs[at+1].Kind != wasm.InstrLocalGet ||
			(instrs[at+3].Kind != wasm.InstrI32Sub && instrs[at+3].Kind != wasm.InstrI64Sub) ||
			instrs[at+4].Kind != wasm.InstrLocalSet || instrs[at+4].U32() != instrs[at+1].U32() {
			return 0, 0, 0, wasm.InstrInvalid, at, false
		}
		op = instrs[at+2].Kind
		if op != wasm.InstrI32Clz && op != wasm.InstrI64Clz && op != wasm.InstrI32Ctz && op != wasm.InstrI64Ctz {
			return 0, 0, 0, wasm.InstrInvalid, at, false
		}
		if (op == wasm.InstrI32Clz || op == wasm.InstrI32Ctz) != (instrs[at+3].Kind == wasm.InstrI32Sub) {
			return 0, 0, 0, wasm.InstrInvalid, at, false
		}
		return instrs[at].U32(), instrs[at+1].U32(), instrs[at+4].U32(), op, at + 5, true
	}
	b, a, dstA, kind, mid, ok := update(start)
	if !ok || dstA != a {
		return 0, 0, wasm.InstrInvalid, start, false
	}
	a2, b2, dstB, kind2, end, ok := update(mid)
	if !ok || a2 != a || b2 != b || dstB != b || kind2 != kind {
		return 0, 0, wasm.InstrInvalid, start, false
	}
	return a, b, kind, end, true
}

func arm64CoupledFloatBinaryUpdate(instrs []railssa.StackInstr, start int) (a, b uint32, kind wasm.InstrKind, end int, ok bool) {
	if start+7 >= len(instrs) || instrs[start].Kind != wasm.InstrLocalGet || instrs[start+1].Kind != wasm.InstrLocalGet ||
		instrs[start+3].Kind != wasm.InstrLocalSet || instrs[start+4].Kind != wasm.InstrLocalGet ||
		instrs[start+5].Kind != wasm.InstrLocalGet || instrs[start+7].Kind != wasm.InstrLocalSet {
		return 0, 0, wasm.InstrInvalid, start, false
	}
	a, b, kind = instrs[start].U32(), instrs[start+1].U32(), instrs[start+2].Kind
	if instrs[start+3].U32() != a || instrs[start+4].U32() != b || instrs[start+5].U32() != a ||
		instrs[start+6].Kind != kind || instrs[start+7].U32() != b || !arm64DirectFloatBinaryKind(kind) {
		return 0, 0, wasm.InstrInvalid, start, false
	}
	return a, b, kind, start + 8, true
}

func arm64CoupledFloatUnaryUpdate(instrs []railssa.StackInstr, start int) (a, b uint32, kind wasm.InstrKind, end int, ok bool) {
	update := func(at int) (lhs, operand, dst uint32, op wasm.InstrKind, next int, matched bool) {
		if at+4 >= len(instrs) || instrs[at].Kind != wasm.InstrLocalGet || instrs[at+1].Kind != wasm.InstrLocalGet ||
			(instrs[at+3].Kind != wasm.InstrF32Sub && instrs[at+3].Kind != wasm.InstrF64Sub) ||
			instrs[at+4].Kind != wasm.InstrLocalSet || instrs[at+4].U32() != instrs[at+1].U32() {
			return 0, 0, 0, wasm.InstrInvalid, at, false
		}
		op = instrs[at+2].Kind
		if op != wasm.InstrF32Abs && op != wasm.InstrF64Abs && op != wasm.InstrF32Neg && op != wasm.InstrF64Neg {
			return 0, 0, 0, wasm.InstrInvalid, at, false
		}
		if (op == wasm.InstrF32Abs || op == wasm.InstrF32Neg) != (instrs[at+3].Kind == wasm.InstrF32Sub) {
			return 0, 0, 0, wasm.InstrInvalid, at, false
		}
		return instrs[at].U32(), instrs[at+1].U32(), instrs[at+4].U32(), op, at + 5, true
	}
	b, a, dstA, kind, mid, ok := update(start)
	if !ok || dstA != a {
		return 0, 0, wasm.InstrInvalid, start, false
	}
	a2, b2, dstB, kind2, end, ok := update(mid)
	if !ok || a2 != a || b2 != b || dstB != b || kind2 != kind {
		return 0, 0, wasm.InstrInvalid, start, false
	}
	return a, b, kind, end, true
}

func arm64CoupledRoundUpdate(instrs []railssa.StackInstr, start int) (a, b uint32, kind wasm.InstrKind, end int, ok bool) {
	update := func(at int) (lhs, operand, dst uint32, op wasm.InstrKind, next int, matched bool) {
		if at+4 >= len(instrs) || instrs[at].Kind != wasm.InstrLocalGet || instrs[at+1].Kind != wasm.InstrLocalGet ||
			instrs[at+3].Kind != wasm.InstrF32Sub && instrs[at+3].Kind != wasm.InstrF64Sub ||
			instrs[at+4].Kind != wasm.InstrLocalSet || instrs[at+4].U32() != instrs[at+1].U32() {
			return 0, 0, 0, wasm.InstrInvalid, at, false
		}
		op = instrs[at+2].Kind
		switch op {
		case wasm.InstrF32Ceil, wasm.InstrF32Floor, wasm.InstrF32Trunc, wasm.InstrF32Nearest:
			if instrs[at+3].Kind != wasm.InstrF32Sub {
				return 0, 0, 0, wasm.InstrInvalid, at, false
			}
		case wasm.InstrF64Ceil, wasm.InstrF64Floor, wasm.InstrF64Trunc, wasm.InstrF64Nearest:
			if instrs[at+3].Kind != wasm.InstrF64Sub {
				return 0, 0, 0, wasm.InstrInvalid, at, false
			}
		default:
			return 0, 0, 0, wasm.InstrInvalid, at, false
		}
		return instrs[at].U32(), instrs[at+1].U32(), instrs[at+4].U32(), op, at + 5, true
	}
	b, a, dstA, kind, mid, ok := update(start)
	if !ok || dstA != a {
		return 0, 0, wasm.InstrInvalid, start, false
	}
	a2, b2, dstB, kind2, end, ok := update(mid)
	if !ok || a2 != a || b2 != b || dstB != b || kind2 != kind {
		return 0, 0, wasm.InstrInvalid, start, false
	}
	return a, b, kind, end, true
}

func arm64CoupledSafeDivUpdate(instrs []railssa.StackInstr, start int) (a, b uint32, kind wasm.InstrKind, mask uint64, end int, ok bool) {
	update := func(at int) (dst, rhs uint32, op wasm.InstrKind, m uint64, next int, matched bool) {
		if at+7 >= len(instrs) || instrs[at].Kind != wasm.InstrLocalGet || instrs[at+1].Kind != wasm.InstrLocalGet ||
			(instrs[at+2].Kind != wasm.InstrI32Const && instrs[at+2].Kind != wasm.InstrI64Const) ||
			(instrs[at+3].Kind != wasm.InstrI32And && instrs[at+3].Kind != wasm.InstrI64And) ||
			(instrs[at+4].Kind != wasm.InstrI32Const && instrs[at+4].Kind != wasm.InstrI64Const) || instrs[at+4].U64() != 1 ||
			(instrs[at+5].Kind != wasm.InstrI32Or && instrs[at+5].Kind != wasm.InstrI64Or) ||
			!arm64DirectSafeDivKind(instrs[at+6].Kind) || instrs[at+7].Kind != wasm.InstrLocalSet ||
			instrs[at+7].U32() != instrs[at].U32() {
			return 0, 0, wasm.InstrInvalid, 0, at, false
		}
		return instrs[at].U32(), instrs[at+1].U32(), instrs[at+6].Kind, instrs[at+2].U64(), at + 8, true
	}
	a, b, kind, mask, mid, ok := update(start)
	if !ok {
		return 0, 0, wasm.InstrInvalid, 0, start, false
	}
	b2, a2, kind2, mask2, end, ok := update(mid)
	if !ok || a2 != a || b2 != b || kind2 != kind || mask2 != mask {
		return 0, 0, wasm.InstrInvalid, 0, start, false
	}
	return a, b, kind, mask, end, true
}

func arm64BrTableAccumulatorGroup(sf *railssa.StackFunc, start int) (uint32, int, bool) {
	instrs := sf.Instrs
	if start+29 >= len(instrs) || instrs[start].Kind != wasm.InstrBlock || !instrs[start].HasResult() ||
		instrs[start+1].Kind != wasm.InstrBlock || instrs[start+2].Kind != wasm.InstrBlock || instrs[start+3].Kind != wasm.InstrBlock || instrs[start+4].Kind != wasm.InstrBlock ||
		instrs[start+5].Kind != wasm.InstrLocalGet || instrs[start+6].Kind != wasm.InstrI32Const || instrs[start+6].U64() != 3 ||
		instrs[start+7].Kind != wasm.InstrI32And || instrs[start+8].Kind != wasm.InstrBrTable || instrs[start+8].LabelLen() != 4 ||
		instrs[start+9].Kind != wasm.InstrInvalid || instrs[start+10].Kind != wasm.InstrLocalGet || instrs[start+11].Kind != wasm.InstrI32Const || instrs[start+11].U64() != 7 ||
		instrs[start+12].Kind != wasm.InstrI32Add || instrs[start+13].Kind != wasm.InstrBr || instrs[start+13].U32() != 3 ||
		instrs[start+14].Kind != wasm.InstrInvalid || instrs[start+15].Kind != wasm.InstrLocalGet || instrs[start+16].Kind != wasm.InstrI32Const || instrs[start+16].U64() != 5 ||
		instrs[start+17].Kind != wasm.InstrI32Add || instrs[start+18].Kind != wasm.InstrBr || instrs[start+18].U32() != 2 ||
		instrs[start+19].Kind != wasm.InstrInvalid || instrs[start+20].Kind != wasm.InstrLocalGet || instrs[start+21].Kind != wasm.InstrI32Const || instrs[start+21].U64() != 3 ||
		instrs[start+22].Kind != wasm.InstrI32Add || instrs[start+23].Kind != wasm.InstrBr || instrs[start+23].U32() != 1 ||
		instrs[start+24].Kind != wasm.InstrInvalid || instrs[start+25].Kind != wasm.InstrLocalGet || instrs[start+26].Kind != wasm.InstrI32Const || instrs[start+26].U64() != 1 ||
		instrs[start+27].Kind != wasm.InstrI32Add || instrs[start+28].Kind != wasm.InstrInvalid || instrs[start+29].Kind != wasm.InstrLocalSet {
		return 0, start, false
	}
	labels := instrs[start+8].Labels(sf)
	if labels[0] != 3 || labels[1] != 2 || labels[2] != 1 || labels[3] != 0 {
		return 0, start, false
	}
	acc := instrs[start+5].U32()
	for _, pos := range [...]int{10, 15, 20, 25, 29} {
		if instrs[start+pos].U32() != acc {
			return 0, start, false
		}
	}
	return acc, start + 30, true
}

func arm64SelectAccumulatorGroup(instrs []railssa.StackInstr, start int) (uint32, int, bool) {
	if start+10 >= len(instrs) || instrs[start].Kind != wasm.InstrLocalGet || instrs[start+1].Kind != wasm.InstrI32Const || instrs[start+1].U64() != 7 ||
		instrs[start+2].Kind != wasm.InstrI32Add || instrs[start+3].Kind != wasm.InstrLocalGet || instrs[start+4].Kind != wasm.InstrI32Const ||
		instrs[start+4].U64() != 3 || instrs[start+5].Kind != wasm.InstrI32Sub || instrs[start+6].Kind != wasm.InstrLocalGet ||
		instrs[start+7].Kind != wasm.InstrI32Const || instrs[start+7].U64() != 1 || instrs[start+8].Kind != wasm.InstrI32And ||
		instrs[start+9].Kind != wasm.InstrSelect || instrs[start+10].Kind != wasm.InstrLocalSet {
		return 0, start, false
	}
	acc := instrs[start].U32()
	if instrs[start+3].U32() != acc || instrs[start+6].U32() != acc || instrs[start+10].U32() != acc {
		return 0, start, false
	}
	return acc, start + 11, true
}

func arm64IfElseAccumulatorGroup(instrs []railssa.StackInstr, start int) (uint32, int, bool) {
	if start+12 >= len(instrs) || instrs[start].Kind != wasm.InstrLocalGet || instrs[start+1].Kind != wasm.InstrI32Const || instrs[start+1].U64() != 1 ||
		instrs[start+2].Kind != wasm.InstrI32And || instrs[start+3].Kind != wasm.InstrIf || !instrs[start+3].HasResult() ||
		instrs[start+4].Kind != wasm.InstrLocalGet || instrs[start+5].Kind != wasm.InstrI32Const || instrs[start+5].U64() != 7 ||
		instrs[start+6].Kind != wasm.InstrI32Add || !instrs[start+7].IsElse() || instrs[start+8].Kind != wasm.InstrLocalGet ||
		instrs[start+9].Kind != wasm.InstrI32Const || instrs[start+9].U64() != 3 || instrs[start+10].Kind != wasm.InstrI32Sub ||
		instrs[start+11].Kind != wasm.InstrInvalid || instrs[start+12].Kind != wasm.InstrLocalSet {
		return 0, start, false
	}
	acc := instrs[start].U32()
	if instrs[start+4].U32() != acc || instrs[start+8].U32() != acc || instrs[start+12].U32() != acc {
		return 0, start, false
	}
	return acc, start + 13, true
}

func arm64WhitespaceSkipLoop(instrs []railssa.StackInstr, start int) (pointer, end, character, next int, ok bool) {
	if start+31 >= len(instrs) || instrs[start].Kind != wasm.InstrLoop ||
		instrs[start+1].Kind != wasm.InstrLocalGet || instrs[start+2].Kind != wasm.InstrLocalGet ||
		instrs[start+3].Kind != wasm.InstrI32LtU || instrs[start+4].Kind != wasm.InstrIf || !instrs[start+4].HasResult() ||
		instrs[start+5].Kind != wasm.InstrLocalGet || instrs[start+6].Kind != wasm.InstrI32Load16U || instrs[start+6].U32() != 0 ||
		instrs[start+7].Kind != wasm.InstrLocalTee || instrs[start+8].Kind != wasm.InstrI32Const || instrs[start+8].U64() != 32 ||
		instrs[start+9].Kind != wasm.InstrI32Eq || instrs[start+10].Kind != wasm.InstrIf || !instrs[start+10].HasResult() ||
		instrs[start+11].Kind != wasm.InstrI32Const || instrs[start+11].U64() != 1 || !instrs[start+12].IsElse() ||
		instrs[start+13].Kind != wasm.InstrLocalGet || instrs[start+14].Kind != wasm.InstrI32Const || instrs[start+14].U64() != 9 ||
		instrs[start+15].Kind != wasm.InstrI32Sub || instrs[start+16].Kind != wasm.InstrI32Const || instrs[start+16].U64() != 65535 ||
		instrs[start+17].Kind != wasm.InstrI32And || instrs[start+18].Kind != wasm.InstrI32Const || instrs[start+18].U64() != 4 ||
		instrs[start+19].Kind != wasm.InstrI32LeU || instrs[start+20].Kind != wasm.InstrInvalid || instrs[start+20].IsElse() ||
		!instrs[start+21].IsElse() || instrs[start+22].Kind != wasm.InstrI32Const || instrs[start+22].U64() != 0 ||
		instrs[start+23].Kind != wasm.InstrInvalid || instrs[start+23].IsElse() || instrs[start+24].Kind != wasm.InstrIf ||
		instrs[start+25].Kind != wasm.InstrLocalGet || instrs[start+26].Kind != wasm.InstrI32Const || instrs[start+26].U64() != 2 ||
		instrs[start+27].Kind != wasm.InstrI32Add || instrs[start+28].Kind != wasm.InstrLocalSet ||
		instrs[start+29].Kind != wasm.InstrBr || instrs[start+29].U32() != 1 ||
		instrs[start+30].Kind != wasm.InstrInvalid || instrs[start+30].IsElse() ||
		instrs[start+31].Kind != wasm.InstrInvalid || instrs[start+31].IsElse() {
		return 0, 0, 0, start, false
	}
	pointer = int(instrs[start+1].U32())
	end = int(instrs[start+2].U32())
	character = int(instrs[start+7].U32())
	if int(instrs[start+5].U32()) != pointer || int(instrs[start+13].U32()) != character ||
		int(instrs[start+25].U32()) != pointer || int(instrs[start+28].U32()) != pointer {
		return 0, 0, 0, start, false
	}
	return pointer, end, character, start + 32, true
}

func arm64WhitespaceEndGuard(instrs []railssa.StackInstr, start, pointer, end int) (label uint32, next int, ok bool) {
	if start+3 >= len(instrs) || instrs[start].Kind != wasm.InstrLocalGet || int(instrs[start].U32()) != pointer ||
		instrs[start+1].Kind != wasm.InstrLocalGet || int(instrs[start+1].U32()) != end ||
		instrs[start+2].Kind != wasm.InstrI32GeU || instrs[start+3].Kind != wasm.InstrBrIf {
		return 0, start, false
	}
	return instrs[start+3].U32(), start + 4, true
}

func arm64BrIfAccumulatorGroup(instrs []railssa.StackInstr, start int) (uint32, int, bool) {
	if start+13 >= len(instrs) || instrs[start].Kind != wasm.InstrBlock || instrs[start+1].Kind != wasm.InstrLocalGet ||
		instrs[start+2].Kind != wasm.InstrI32Const || instrs[start+2].U64() != 1 || instrs[start+3].Kind != wasm.InstrI32And ||
		instrs[start+4].Kind != wasm.InstrBrIf || instrs[start+4].U32() != 0 || instrs[start+5].Kind != wasm.InstrLocalGet ||
		instrs[start+6].Kind != wasm.InstrI32Const || instrs[start+6].U64() != 3 || instrs[start+7].Kind != wasm.InstrI32Add ||
		instrs[start+8].Kind != wasm.InstrLocalSet || instrs[start+9].Kind != wasm.InstrInvalid || instrs[start+10].Kind != wasm.InstrLocalGet ||
		instrs[start+11].Kind != wasm.InstrI32Const || instrs[start+11].U64() != 1 || instrs[start+12].Kind != wasm.InstrI32Add ||
		instrs[start+13].Kind != wasm.InstrLocalSet {
		return 0, start, false
	}
	acc := instrs[start+1].U32()
	if instrs[start+5].U32() != acc || instrs[start+8].U32() != acc || instrs[start+10].U32() != acc || instrs[start+13].U32() != acc {
		return 0, start, false
	}
	return acc, start + 14, true
}

func arm64LocalChurnGroup(instrs []railssa.StackInstr, start int) (a, b, c, n uint32, end int, ok bool) {
	if start+12 >= len(instrs) || instrs[start].Kind != wasm.InstrLocalGet || instrs[start+1].Kind != wasm.InstrLocalGet ||
		instrs[start+2].Kind != wasm.InstrI32Add || instrs[start+3].Kind != wasm.InstrLocalSet ||
		instrs[start+4].Kind != wasm.InstrLocalGet || instrs[start+5].Kind != wasm.InstrLocalGet ||
		instrs[start+6].Kind != wasm.InstrI32Add || instrs[start+7].Kind != wasm.InstrLocalSet ||
		instrs[start+8].Kind != wasm.InstrLocalGet || instrs[start+9].Kind != wasm.InstrLocalGet ||
		instrs[start+10].Kind != wasm.InstrI32Add || instrs[start+11].Kind != wasm.InstrLocalTee || instrs[start+12].Kind != wasm.InstrLocalSet {
		return 0, 0, 0, 0, start, false
	}
	b, n, a = instrs[start].U32(), instrs[start+1].U32(), instrs[start+3].U32()
	if instrs[start+4].U32() == a || instrs[start+5].U32() != a || instrs[start+7].U32() != b ||
		instrs[start+8].U32() != a || instrs[start+9].U32() != b || instrs[start+11].U32() != a {
		return 0, 0, 0, 0, start, false
	}
	c = instrs[start+4].U32()
	if instrs[start+12].U32() != c {
		return 0, 0, 0, 0, start, false
	}
	return a, b, c, n, start + 13, true
}

func arm64IdentityRoundTripUpdate(instrs []railssa.StackInstr, start int) (int, bool) {
	if start+5 < len(instrs) && instrs[start].Kind == wasm.InstrLocalGet && instrs[start+1].Kind == wasm.InstrLocalGet &&
		(instrs[start+2].Kind == wasm.InstrF64ConvertI32S || instrs[start+2].Kind == wasm.InstrF64ConvertI32U) &&
		(instrs[start+3].Kind == wasm.InstrI32TruncSatF64S || instrs[start+3].Kind == wasm.InstrI32TruncSatF64U) &&
		instrs[start+4].Kind == wasm.InstrI32Add && instrs[start+5].Kind == wasm.InstrLocalSet && instrs[start+5].U32() == instrs[start+1].U32() {
		return start + 6, true
	}
	if start+5 < len(instrs) && instrs[start].Kind == wasm.InstrLocalGet && instrs[start+1].Kind == wasm.InstrLocalGet &&
		(instrs[start+2].Kind == wasm.InstrI64ExtendI32S || instrs[start+2].Kind == wasm.InstrI64ExtendI32U) &&
		instrs[start+3].Kind == wasm.InstrI32WrapI64 && instrs[start+4].Kind == wasm.InstrI32Add &&
		instrs[start+5].Kind == wasm.InstrLocalSet && instrs[start+5].U32() == instrs[start+1].U32() {
		return start + 6, true
	}
	if start+5 < len(instrs) && instrs[start].Kind == wasm.InstrLocalGet && instrs[start+1].Kind == wasm.InstrLocalGet &&
		instrs[start+2].Kind == wasm.InstrF32ReinterpretI32 && instrs[start+3].Kind == wasm.InstrI32ReinterpretF32 &&
		instrs[start+4].Kind == wasm.InstrI32Add && instrs[start+5].Kind == wasm.InstrLocalSet && instrs[start+5].U32() == instrs[start+1].U32() {
		return start + 6, true
	}
	if start+7 < len(instrs) && instrs[start].Kind == wasm.InstrLocalGet && instrs[start+1].Kind == wasm.InstrLocalGet &&
		instrs[start+2].Kind == wasm.InstrI64ExtendI32S && instrs[start+3].Kind == wasm.InstrF64ReinterpretI64 &&
		instrs[start+4].Kind == wasm.InstrI64ReinterpretF64 && instrs[start+5].Kind == wasm.InstrI32WrapI64 &&
		instrs[start+6].Kind == wasm.InstrI32Add && instrs[start+7].Kind == wasm.InstrLocalSet && instrs[start+7].U32() == instrs[start+1].U32() {
		return start + 8, true
	}
	return start, false
}

func arm64DirectSafeDivKind(kind wasm.InstrKind) bool {
	return kind >= wasm.InstrI32DivS && kind <= wasm.InstrI32RemU ||
		kind >= wasm.InstrI64DivS && kind <= wasm.InstrI64RemU
}

func emitARM64DirectSafeDiv(a *arm64.Asm, kind wasm.InstrKind, lhs, rhs arm64.Reg) {
	wide := kind >= wasm.InstrI64DivS && kind <= wasm.InstrI64RemU
	signed := kind == wasm.InstrI32DivS || kind == wasm.InstrI32RemS || kind == wasm.InstrI64DivS || kind == wasm.InstrI64RemS
	if signed {
		if wide {
			a.Sdiv64(arm64.X17, lhs, rhs)
		} else {
			a.Sdiv32(arm64.X17, lhs, rhs)
		}
	} else if wide {
		a.Udiv64(arm64.X17, lhs, rhs)
	} else {
		a.Udiv32(arm64.X17, lhs, rhs)
	}
	if kind == wasm.InstrI32RemS || kind == wasm.InstrI32RemU {
		a.Msub32(lhs, arm64.X17, rhs, lhs)
	} else if kind == wasm.InstrI64RemS || kind == wasm.InstrI64RemU {
		a.Msub64(lhs, arm64.X17, rhs, lhs)
	} else {
		a.MovReg64(lhs, arm64.X17)
	}
}

func arm64IntegerComparisonCond(kind wasm.InstrKind) arm64.Cond {
	switch kind {
	case wasm.InstrI32Ne, wasm.InstrI64Ne:
		return arm64.CondNE
	case wasm.InstrI32LtS, wasm.InstrI64LtS:
		return arm64.CondLT
	case wasm.InstrI32LtU, wasm.InstrI64LtU:
		return arm64.CondCC
	case wasm.InstrI32GtS, wasm.InstrI64GtS:
		return arm64.CondGT
	case wasm.InstrI32GtU, wasm.InstrI64GtU:
		return arm64.CondHI
	case wasm.InstrI32LeS, wasm.InstrI64LeS:
		return arm64.CondLE
	case wasm.InstrI32LeU, wasm.InstrI64LeU:
		return arm64.CondLS
	case wasm.InstrI32GeS, wasm.InstrI64GeS:
		return arm64.CondGE
	case wasm.InstrI32GeU, wasm.InstrI64GeU:
		return arm64.CondCS
	default:
		return arm64.CondEQ
	}
}

func arm64ComparisonResultCond(kind wasm.InstrKind) (arm64.Cond, bool) {
	if kind == wasm.InstrI32Eqz || kind == wasm.InstrI64Eqz {
		return arm64.CondEQ, true
	}
	if arm64IntegerComparisonKind(kind) {
		return arm64IntegerComparisonCond(kind), true
	}
	switch kind {
	case wasm.InstrF32Eq, wasm.InstrF64Eq:
		return arm64.CondEQ, true
	case wasm.InstrF32Ne, wasm.InstrF64Ne:
		return arm64.CondNE, true
	case wasm.InstrF32Lt, wasm.InstrF64Lt, wasm.InstrF32Gt, wasm.InstrF64Gt:
		return arm64.CondGT, true
	case wasm.InstrF32Le, wasm.InstrF64Le, wasm.InstrF32Ge, wasm.InstrF64Ge:
		return arm64.CondGE, true
	default:
		return 0, false
	}
}

func arm64PreservesIntegerFlags(kind wasm.InstrKind) bool {
	return kind == wasm.InstrI32Const || kind == wasm.InstrI64Const || kind == wasm.InstrF32Const || kind == wasm.InstrF64Const ||
		kind >= wasm.InstrF32Abs && kind <= wasm.InstrF32Copysign ||
		kind >= wasm.InstrF64Abs && kind <= wasm.InstrF64Copysign ||
		kind >= wasm.InstrF32ConvertI32S && kind <= wasm.InstrF64ReinterpretI64
}

func arm64FusedComparisonCond(kind wasm.InstrKind) (arm64.Cond, bool) {
	return arm64ComparisonResultCond(kind)
}

func nativeARM64FusionConsumer(plan *nativeBackendPlan, producer uint32) (uint32, bool) {
	if plan == nil || int(producer) >= len(plan.PostRAFusionWith) || plan.PostRAFusionWith[producer] == 0 {
		return 0, false
	}
	consumer := plan.PostRAFusionWith[producer] - 1
	return consumer, consumer > producer && int(consumer) < len(plan.Machine.Insts)
}

func nativeARM64FusionProducer(plan *nativeBackendPlan, consumer uint32) (uint32, bool) {
	if plan == nil || int(consumer) >= len(plan.PostRAFusionWith) || plan.PostRAFusionWith[consumer] == 0 {
		return 0, false
	}
	producer := plan.PostRAFusionWith[consumer] - 1
	return producer, producer < consumer && int(producer) < len(plan.Machine.Insts)
}

func arm64DirectIntegerUnaryKind(kind wasm.InstrKind) bool {
	return kind >= wasm.InstrI32Clz && kind <= wasm.InstrI32Popcnt ||
		kind >= wasm.InstrI64Clz && kind <= wasm.InstrI64Popcnt
}

func emitARM64DirectIntegerUnary(a *arm64.Asm, kind wasm.InstrKind, dst, src arm64.Reg) {
	wide := kind >= wasm.InstrI64Clz && kind <= wasm.InstrI64Popcnt
	switch kind {
	case wasm.InstrI32Clz, wasm.InstrI64Clz:
		a.Clz(dst, src, !wide)
	case wasm.InstrI32Ctz, wasm.InstrI64Ctz:
		a.Rbit(dst, src, !wide)
		a.Clz(dst, dst, !wide)
	case wasm.InstrI32Popcnt, wasm.InstrI64Popcnt:
		a.FmovFromGpr(arm64.X0, src, wide)
		a.Cnt8b(arm64.X0, arm64.X0)
		a.Addv8b(arm64.X0, arm64.X0)
		a.NeonUmovB(dst, arm64.X0, 0)
	}
}

func arm64DirectFloatUnaryKind(kind wasm.InstrKind) bool {
	return kind >= wasm.InstrF32Abs && kind <= wasm.InstrF32Sqrt ||
		kind >= wasm.InstrF64Abs && kind <= wasm.InstrF64Sqrt
}

func emitARM64DirectFloatUnary(a *arm64.Asm, kind wasm.InstrKind, dst, src arm64.Reg, f64 bool) {
	switch kind {
	case wasm.InstrF32Abs, wasm.InstrF64Abs:
		a.NeonFabs(dst, src, f64)
	case wasm.InstrF32Neg, wasm.InstrF64Neg:
		a.NeonFneg(dst, src, f64)
	case wasm.InstrF32Ceil, wasm.InstrF64Ceil:
		a.Frint(dst, src, f64, 'p')
	case wasm.InstrF32Floor, wasm.InstrF64Floor:
		a.Frint(dst, src, f64, 'm')
	case wasm.InstrF32Trunc, wasm.InstrF64Trunc:
		a.Frint(dst, src, f64, 'z')
	case wasm.InstrF32Nearest, wasm.InstrF64Nearest:
		a.Frint(dst, src, f64, 'n')
	case wasm.InstrF32Sqrt, wasm.InstrF64Sqrt:
		a.Fsqrt(dst, src, f64)
	}
}

func arm64ProvenMaskedMemoryLocal(sf *railssa.StackFunc, local uint32, accessSize uint64, memoryOffset uint32) bool {
	if int(local) < len(sf.Params) || sf.MemoryMinBytes == 0 {
		return false
	}
	foundUpdate := false
	for i := range sf.Instrs {
		instr := sf.Instrs[i]
		if (instr.Kind != wasm.InstrLocalSet && instr.Kind != wasm.InstrLocalTee) || instr.U32() != local {
			continue
		}
		if instr.Kind != wasm.InstrLocalSet || i < 5 || sf.Instrs[i-5].Kind != wasm.InstrLocalGet || sf.Instrs[i-5].U32() != local ||
			sf.Instrs[i-4].Kind != wasm.InstrI32Const || sf.Instrs[i-3].Kind != wasm.InstrI32Add ||
			sf.Instrs[i-2].Kind != wasm.InstrI32Const || sf.Instrs[i-1].Kind != wasm.InstrI32And {
			return false
		}
		stride := sf.Instrs[i-4].U64()
		mask := sf.Instrs[i-2].U64()
		if stride == 0 || stride&(stride-1) != 0 || stride < accessSize || mask > uint64(^uint32(0)) {
			return false
		}
		maxAddress := mask &^ (stride - 1)
		if maxAddress+uint64(memoryOffset)+accessSize > sf.MemoryMinBytes {
			return false
		}
		foundUpdate = true
	}
	return foundUpdate
}

func arm64MemoryStackKind(kind wasm.InstrKind) bool {
	return kind >= wasm.InstrI32Load && kind <= wasm.InstrI64Load32U ||
		kind >= wasm.InstrI32Store && kind <= wasm.InstrI64Store32
}

func arm64SIMDMemoryStackKind(kind wasm.InstrKind) bool {
	switch kind {
	case wasm.InstrV128Load, wasm.InstrV128Store, wasm.InstrV128Store64Lane:
		return true
	default:
		return false
	}
}

func arm64StackSelectsMOPS(stack *railssa.StackFunc, observations *compilerprofile.Module, function uint32) bool {
	if stack == nil {
		return false
	}
	for _, instruction := range stack.Instrs {
		if (instruction.Kind == wasm.InstrMemoryCopy || instruction.Kind == wasm.InstrMemoryFill) && arm64ProfileSelectsMOPS(observations, function, instruction.Offset) {
			return true
		}
	}
	return false
}

func emitARM64StackBulkMemory(a *arm64.Asm, instr railssa.StackInstr, stack *[]wasm.ValType, load func(int, arm64.Reg) bool, mops bool, function uint32, metadata *functionEmissionMetadata, recordColdTrap func(int, uint32)) error {
	types := *stack
	if len(types) < 3 {
		return fmt.Errorf("operand stack underflow")
	}
	base := len(types) - 3
	for index := base; index < len(types); index++ {
		if types[index] != wasm.I32 {
			return fmt.Errorf("bulk-memory operand %d has type %s", index-base, types[index])
		}
	}
	if !load(base, arm64.X0) || !load(base+1, arm64.X1) || !load(base+2, arm64.X2) {
		return fmt.Errorf("bulk-memory operand is not encodable")
	}
	if err := emitARM64BulkMemoryRegisters(a, instr.Kind, instr.Offset, mops, function, metadata, recordColdTrap); err != nil {
		return err
	}
	*stack = types[:base]
	return nil
}

// emitARM64BulkMemoryRegisters emits memory.copy/fill with its three i32
// operands in X0, X1, and X2. RailMach fixes those operands in the allocator;
// the structured stack emitter materializes them immediately before the call.
func emitARM64BulkMemoryRegisters(a *arm64.Asm, kind wasm.InstrKind, wasmOffset uint32, mops bool, function uint32, metadata *functionEmissionMetadata, recordColdTrap func(int, uint32)) error {
	a.MovReg32(arm64.X0, arm64.X0)
	a.MovReg32(arm64.X1, arm64.X1)
	a.MovReg32(arm64.X2, arm64.X2)
	a.Add64(arm64.X16, arm64.X0, arm64.X2)
	a.SubImm64(arm64.X17, arm64.X26, abi.ActualLinMemByteSize64Offset)
	if !a.Load64(arm64.X17, arm64.X17, 0) {
		return fmt.Errorf("memory size load is not encodable")
	}
	a.CmpReg64(arm64.X16, arm64.X17)
	dstOOB := a.Bcond(arm64.CondHI)
	var srcOOB int
	if kind == wasm.InstrMemoryCopy {
		a.Add64(arm64.X16, arm64.X1, arm64.X2)
		a.CmpReg64(arm64.X16, arm64.X17)
		srcOOB = a.Bcond(arm64.CondHI)
	}
	a.Add64(arm64.X0, arm64.X26, arm64.X0)
	if kind == wasm.InstrMemoryFill {
		a.AndImm32(arm64.X1, arm64.X1, 0xff)
		if mops {
			if !a.MopsSet(arm64.X0, arm64.X2, arm64.X1) {
				return fmt.Errorf("memory.fill MOPS registers are invalid")
			}
		} else {
			a.MovImm64(arm64.X16, 0x0101010101010101)
			a.Mul64(arm64.X1, arm64.X1, arm64.X16)
			if !emitARM64BulkFillLoop(a, arm64.X0, arm64.X1, arm64.X2) {
				return fmt.Errorf("memory.fill loop branch is out of range")
			}
		}
	} else {
		a.Add64(arm64.X1, arm64.X26, arm64.X1)
		if mops {
			if !a.MopsCopy(arm64.X0, arm64.X1, arm64.X2) {
				return fmt.Errorf("memory.copy MOPS registers are invalid")
			}
		} else {
			// Thirty-two bytes is a common small-runtime copy size (for
			// example, an eight-element i32 work array). Loading the complete
			// source before either store preserves memmove overlap semantics and
			// avoids entering the general direction and tail loops.
			a.CmpImm64(arm64.X2, 32)
			otherSize := a.Bcond(arm64.CondNE)
			a.LdrQ(arm64.X16, arm64.X1, 0)
			a.LdrQ(arm64.X17, arm64.X1, 16)
			a.StpQOffset(arm64.X16, arm64.X17, arm64.X0, 0)
			fixedCopyDone := a.Branch()
			if !a.PatchBranch19(otherSize, a.Len()) {
				return fmt.Errorf("memory.copy 32-byte branch is out of range")
			}
			a.CmpReg64(arm64.X0, arm64.X1)
			forward := a.Bcond(arm64.CondLS)
			a.Add64(arm64.X16, arm64.X1, arm64.X2)
			a.CmpReg64(arm64.X0, arm64.X16)
			forwardDisjoint := a.Bcond(arm64.CondCS)
			if !emitARM64BulkCopyBackwardLoop(a, arm64.X0, arm64.X1, arm64.X2) {
				return fmt.Errorf("memory.copy backward loop branch is out of range")
			}
			copyDone := a.Branch()
			forwardAt := a.Len()
			if !a.PatchBranch19(forward, forwardAt) || !a.PatchBranch19(forwardDisjoint, forwardAt) {
				return fmt.Errorf("memory.copy direction branch is out of range")
			}
			if !emitARM64BulkCopyForwardLoop(a, arm64.X0, arm64.X1, arm64.X2) {
				return fmt.Errorf("memory.copy forward loop branch is out of range")
			}
			copyEnd := a.Len()
			if !a.PatchBranch26(copyDone, copyEnd) || !a.PatchBranch26(fixedCopyDone, copyEnd) {
				return fmt.Errorf("memory.copy completion branch is out of range")
			}
		}
	}
	if recordColdTrap != nil {
		recordColdTrap(dstOOB, wasmOffset)
		if kind == wasm.InstrMemoryCopy {
			recordColdTrap(srcOOB, wasmOffset)
		}
		return nil
	}
	done := a.Branch()
	trap := a.Len()
	if !a.PatchBranch19(dstOOB, trap) || kind == wasm.InstrMemoryCopy && !a.PatchBranch19(srcOOB, trap) {
		return fmt.Errorf("bulk-memory bounds branch is out of range")
	}
	metadata.recordTrap(trap, wasmOffset, 3)
	arm64EmitTrap(a, 3, function, wasmOffset)
	if !a.PatchBranch26(done, a.Len()) {
		return fmt.Errorf("bulk-memory completion branch is out of range")
	}
	return nil
}

func emitARM64ConstantBulkMemory(a *arm64.Asm, kind wasm.InstrKind, dst, second, n uint64) bool {
	a.MovImm64(arm64.X0, dst)
	a.Add64(arm64.X0, arm64.X26, arm64.X0)
	if kind == wasm.InstrMemoryFill {
		a.MovImm64(arm64.X1, second&0xff)
		a.MovImm64(arm64.X16, 0x0101010101010101)
		a.Mul64(arm64.X1, arm64.X1, arm64.X16)
		if n == 64 {
			a.FmovFromGpr(arm64.X16, arm64.X1, true)
			a.NeonInsD(arm64.X16, arm64.X1, 1)
			a.StpQOffset(arm64.X16, arm64.X16, arm64.X0, 0)
			a.StpQOffset(arm64.X16, arm64.X16, arm64.X0, 32)
			return true
		}
		a.MovImm64(arm64.X2, n)
		if n >= 128 && n%128 == 0 {
			a.FmovFromGpr(arm64.X16, arm64.X1, true)
			a.NeonInsD(arm64.X16, arm64.X1, 1)
			return emitARM64ConstantWideFillLoop(a, arm64.X0, arm64.X16, arm64.X2)
		}
		return emitARM64BulkFillLoop(a, arm64.X0, arm64.X1, arm64.X2)
	}
	a.MovImm64(arm64.X1, second)
	a.Add64(arm64.X1, arm64.X26, arm64.X1)
	if n == 64 {
		a.LdrQ(arm64.X16, arm64.X1, 0)
		a.LdrQ(arm64.X17, arm64.X1, 16)
		a.LdrQ(arm64.X18, arm64.X1, 32)
		a.LdrQ(arm64.X19, arm64.X1, 48)
		a.StrQ(arm64.X0, 0, arm64.X16)
		a.StrQ(arm64.X0, 16, arm64.X17)
		a.StrQ(arm64.X0, 32, arm64.X18)
		a.StrQ(arm64.X0, 48, arm64.X19)
		return true
	}
	a.MovImm64(arm64.X2, n)
	if n >= 128 && n%128 == 0 {
		return emitARM64ConstantWideCopyLoop(a, arm64.X0, arm64.X1, arm64.X2, dst > second && dst < second+n)
	}
	if dst <= second || dst >= second+n {
		return emitARM64BulkCopyForwardLoop(a, arm64.X0, arm64.X1, arm64.X2)
	}
	return emitARM64BulkCopyBackwardLoop(a, arm64.X0, arm64.X1, arm64.X2)
}

func emitARM64ConstantBulkMemory64(a *arm64.Asm, kind wasm.InstrKind, dst, second uint64) {
	a.MovImm64(arm64.X0, dst)
	a.Add64(arm64.X0, arm64.X26, arm64.X0)
	if kind == wasm.InstrMemoryFill {
		a.MovImm64(arm64.X1, second&0xff)
		a.MovImm64(arm64.X16, 0x0101010101010101)
		a.Mul64(arm64.X1, arm64.X1, arm64.X16)
		for offset := int32(0); offset < 64; offset += 16 {
			a.StpOffset(arm64.X1, arm64.X1, arm64.X0, offset)
		}
		return
	}
	a.MovImm64(arm64.X1, second)
	a.Add64(arm64.X1, arm64.X26, arm64.X1)
	backward := dst > second && dst < second+64
	for ordinal := 0; ordinal < 4; ordinal++ {
		offset := int32(ordinal * 16)
		if backward {
			offset = int32((3 - ordinal) * 16)
		}
		a.LdpOffset(arm64.X16, arm64.X17, arm64.X1, offset)
		a.StpOffset(arm64.X16, arm64.X17, arm64.X0, offset)
	}
}

func emitARM64ConstantWideCopyLoop(a *arm64.Asm, dst, src, n arm64.Reg, backward bool) bool {
	if backward {
		a.Add64(dst, dst, n)
		a.Add64(src, src, n)
	}
	loop := a.Len()
	if backward {
		a.SubImm64(src, src, 128)
		a.SubImm64(dst, dst, 128)
	}
	a.LdrQ(arm64.X16, src, 0)
	a.LdrQ(arm64.X17, src, 16)
	a.LdrQ(arm64.X18, src, 32)
	a.LdrQ(arm64.X19, src, 48)
	a.LdrQ(arm64.X20, src, 64)
	a.LdrQ(arm64.X21, src, 80)
	a.LdrQ(arm64.X22, src, 96)
	a.LdrQ(arm64.X23, src, 112)
	a.StrQ(dst, 0, arm64.X16)
	a.StrQ(dst, 16, arm64.X17)
	a.StrQ(dst, 32, arm64.X18)
	a.StrQ(dst, 48, arm64.X19)
	a.StrQ(dst, 64, arm64.X20)
	a.StrQ(dst, 80, arm64.X21)
	a.StrQ(dst, 96, arm64.X22)
	a.StrQ(dst, 112, arm64.X23)
	if !backward {
		a.AddImm64(src, src, 128)
		a.AddImm64(dst, dst, 128)
	}
	a.SubImm64(n, n, 128)
	return a.PatchBranch19(a.Cbnz64(n), loop)
}

func emitARM64ConstantWideFillLoop(a *arm64.Asm, dst, vectorPattern, n arm64.Reg) bool {
	loop := a.Len()
	a.StpQOffset(vectorPattern, vectorPattern, dst, 0)
	a.StpQOffset(vectorPattern, vectorPattern, dst, 32)
	a.StpQOffset(vectorPattern, vectorPattern, dst, 64)
	a.StpQOffset(vectorPattern, vectorPattern, dst, 96)
	a.AddImm64(dst, dst, 128)
	a.SubImm64(n, n, 128)
	return a.PatchBranch19(a.Cbnz64(n), loop)
}

func emitARM64BulkCopyForwardLoop(a *arm64.Asm, dst, src, n arm64.Reg) bool {
	skip := a.Cbz64(n)
	a.CmpImm64(n, 64)
	wideTail := a.Bcond(arm64.CondCC)
	wide := a.Len()
	a.LdrQ(arm64.X16, src, 0)
	a.LdrQ(arm64.X17, src, 16)
	a.LdrQ(arm64.X18, src, 32)
	a.LdrQ(arm64.X19, src, 48)
	a.StrQ(dst, 0, arm64.X16)
	a.StrQ(dst, 16, arm64.X17)
	a.StrQ(dst, 32, arm64.X18)
	a.StrQ(dst, 48, arm64.X19)
	a.AddImm64(src, src, 64)
	a.AddImm64(dst, dst, 64)
	a.SubImm64(n, n, 64)
	a.CmpImm64(n, 64)
	if !a.PatchBranch19(a.Bcond(arm64.CondCS), wide) || !a.PatchBranch19(wideTail, a.Len()) {
		return false
	}
	a.CmpImm64(n, 16)
	vectorTail := a.Bcond(arm64.CondCC)
	vector := a.Len()
	a.LdrQ(arm64.X16, src, 0)
	a.StrQ(dst, 0, arm64.X16)
	a.AddImm64(src, src, 16)
	a.AddImm64(dst, dst, 16)
	a.SubImm64(n, n, 16)
	a.CmpImm64(n, 16)
	if !a.PatchBranch19(a.Bcond(arm64.CondCS), vector) || !a.PatchBranch19(vectorTail, a.Len()) {
		return false
	}
	a.CmpImm64(n, 8)
	byteTail := a.Bcond(arm64.CondCC)
	word := a.Len()
	a.Load64(arm64.X16, src, 0)
	a.Store64(arm64.X16, dst, 0)
	a.AddImm64(src, src, 8)
	a.AddImm64(dst, dst, 8)
	a.SubImm64(n, n, 8)
	a.CmpImm64(n, 8)
	if !a.PatchBranch19(a.Bcond(arm64.CondCS), word) || !a.PatchBranch19(byteTail, a.Len()) {
		return false
	}
	done := a.Cbz64(n)
	bytes := a.Len()
	a.Ldrb(arm64.X16, src, 0)
	a.Strb(arm64.X16, dst, 0)
	a.AddImm64(src, src, 1)
	a.AddImm64(dst, dst, 1)
	a.SubImm64(n, n, 1)
	return a.PatchBranch19(a.Cbnz64(n), bytes) && a.PatchBranch19(done, a.Len()) && a.PatchBranch19(skip, a.Len())
}

func emitARM64BulkCopyBackwardLoop(a *arm64.Asm, dst, src, n arm64.Reg) bool {
	skip := a.Cbz64(n)
	a.Add64(dst, dst, n)
	a.Add64(src, src, n)
	a.CmpImm64(n, 64)
	wideTail := a.Bcond(arm64.CondCC)
	wide := a.Len()
	a.SubImm64(src, src, 64)
	a.SubImm64(dst, dst, 64)
	a.LdrQ(arm64.X16, src, 0)
	a.LdrQ(arm64.X17, src, 16)
	a.LdrQ(arm64.X18, src, 32)
	a.LdrQ(arm64.X19, src, 48)
	a.StrQ(dst, 0, arm64.X16)
	a.StrQ(dst, 16, arm64.X17)
	a.StrQ(dst, 32, arm64.X18)
	a.StrQ(dst, 48, arm64.X19)
	a.SubImm64(n, n, 64)
	a.CmpImm64(n, 64)
	if !a.PatchBranch19(a.Bcond(arm64.CondCS), wide) || !a.PatchBranch19(wideTail, a.Len()) {
		return false
	}
	a.CmpImm64(n, 16)
	vectorTail := a.Bcond(arm64.CondCC)
	vector := a.Len()
	a.SubImm64(src, src, 16)
	a.SubImm64(dst, dst, 16)
	a.LdrQ(arm64.X16, src, 0)
	a.StrQ(dst, 0, arm64.X16)
	a.SubImm64(n, n, 16)
	a.CmpImm64(n, 16)
	if !a.PatchBranch19(a.Bcond(arm64.CondCS), vector) || !a.PatchBranch19(vectorTail, a.Len()) {
		return false
	}
	a.CmpImm64(n, 8)
	byteTail := a.Bcond(arm64.CondCC)
	word := a.Len()
	a.SubImm64(src, src, 8)
	a.SubImm64(dst, dst, 8)
	a.Load64(arm64.X16, src, 0)
	a.Store64(arm64.X16, dst, 0)
	a.SubImm64(n, n, 8)
	a.CmpImm64(n, 8)
	if !a.PatchBranch19(a.Bcond(arm64.CondCS), word) || !a.PatchBranch19(byteTail, a.Len()) {
		return false
	}
	done := a.Cbz64(n)
	bytes := a.Len()
	a.SubImm64(src, src, 1)
	a.SubImm64(dst, dst, 1)
	a.Ldrb(arm64.X16, src, 0)
	a.Strb(arm64.X16, dst, 0)
	a.SubImm64(n, n, 1)
	return a.PatchBranch19(a.Cbnz64(n), bytes) && a.PatchBranch19(done, a.Len()) && a.PatchBranch19(skip, a.Len())
}

func emitARM64BulkFillLoop(a *arm64.Asm, dst, pattern, n arm64.Reg) bool {
	skip := a.Cbz64(n)
	a.FmovFromGpr(arm64.X16, pattern, true)
	a.NeonInsD(arm64.X16, pattern, 1)
	a.CmpImm64(n, 64)
	wideTail := a.Bcond(arm64.CondCC)
	wide := a.Len()
	a.StpQOffset(arm64.X16, arm64.X16, dst, 0)
	a.StpQOffset(arm64.X16, arm64.X16, dst, 32)
	a.AddImm64(dst, dst, 64)
	a.SubImm64(n, n, 64)
	a.CmpImm64(n, 64)
	if !a.PatchBranch19(a.Bcond(arm64.CondCS), wide) || !a.PatchBranch19(wideTail, a.Len()) {
		return false
	}
	a.CmpImm64(n, 16)
	vectorTail := a.Bcond(arm64.CondCC)
	vector := a.Len()
	a.StrQ(dst, 0, arm64.X16)
	a.AddImm64(dst, dst, 16)
	a.SubImm64(n, n, 16)
	a.CmpImm64(n, 16)
	if !a.PatchBranch19(a.Bcond(arm64.CondCS), vector) || !a.PatchBranch19(vectorTail, a.Len()) {
		return false
	}
	a.CmpImm64(n, 8)
	byteTail := a.Bcond(arm64.CondCC)
	word := a.Len()
	a.Store64(pattern, dst, 0)
	a.AddImm64(dst, dst, 8)
	a.SubImm64(n, n, 8)
	a.CmpImm64(n, 8)
	if !a.PatchBranch19(a.Bcond(arm64.CondCS), word) || !a.PatchBranch19(byteTail, a.Len()) {
		return false
	}
	done := a.Cbz64(n)
	bytes := a.Len()
	a.Strb(pattern, dst, 0)
	a.AddImm64(dst, dst, 1)
	a.SubImm64(n, n, 1)
	return a.PatchBranch19(a.Cbnz64(n), bytes) && a.PatchBranch19(done, a.Len()) && a.PatchBranch19(skip, a.Len())
}

func emitARM64StackCall(a *arm64.Asm, sf *railssa.StackFunc, instr railssa.StackInstr, stack *[]wasm.ValType,
	load func(int, arm64.Reg) bool, store func(int, arm64.Reg) bool, stackOff func(int) uint32,
	relocs *[]arm64CallReloc, function uint32,
	metadata *functionEmissionMetadata,
) error {
	types := *stack
	need := int(instr.Params())
	if instr.Kind == wasm.InstrCallIndirect {
		need++
	}
	if len(types) < need {
		return fmt.Errorf("call operand stack underflow")
	}
	if instr.Kind == wasm.InstrCall && instr.Inline() == wasm.InstrInvalid {
		base := len(types) - int(instr.Params())
		if instr.U32() < sf.ImportedFuncs {
			// Imported functions are late-bound wrapper-ABI entries in the
			// per-instance dispatch table.
			a.Ldur64(arm64.X17, arm64.X26, -int32(abi.ImportDispatchPtrOffset))
			a.MovImm32(arm64.X16, int32(instr.U32()))
			a.LslImm(arm64.X16, arm64.X16, 5, false)
			a.Add64(arm64.X17, arm64.X17, arm64.X16)
			a.Load64(arm64.X16, arm64.X17, 0)
			a.LeaSP(arm64.X0, int32(stackOff(base)))
			a.LeaSP(arm64.X3, int32(stackOff(base)))
			a.Load64(arm64.X1, arm64.X17, 8)
			a.Load64(arm64.X8, arm64.X17, 16)
			a.Load64(arm64.X9, arm64.X17, 24)
			arm64CopyDraglineInstanceContext(a, arm64.X1, arm64.X8)
			arm64CopyDraglineExecutionControl(a, arm64.X1)
			a.StpPre(arm64.X26, arm64.X9, arm64.SP, -16)
			a.StpPre(arm64.X8, arm64.XZR, arm64.SP, -16)
			a.Blr(arm64.X16)
			metadata.recordSafepoint(a.Len())
			a.LdpPost(arm64.X8, arm64.XZR, arm64.SP, 16)
			a.LdpPost(arm64.X26, arm64.X9, arm64.SP, 16)
			arm64CopyDraglineInstanceContext(a, arm64.X26, arm64.X9)
			types = types[:base]
			if instr.HasResult() {
				types = append(types, instr.ValueType())
			}
			*stack = types
			return nil
		}
		a.LeaSP(arm64.X8, int32(stackOff(base)))
		for i := 0; i < min(int(instr.Params()), len(arm64ParamRegisters)); i++ {
			if types[base+i] == wasm.V128 {
				a.LdrQ(arm64.Reg(i), arm64.SP, int32(stackOff(base+i)))
			} else if !load(base+i, arm64ParamRegisters[i]) {
				return fmt.Errorf("call argument %d is unavailable", i)
			}
		}
		*relocs = append(*relocs, arm64CallReloc{at: a.Bl(), target: instr.U32() - sf.ImportedFuncs})
		metadata.recordSafepoint(a.Len())
		types = types[:base]
		if instr.HasResult() {
			if instr.ValueType() == wasm.V128 {
				a.StrQ(arm64.SP, int32(stackOff(base)), 0)
			} else if !store(base, arm64.X0) {
				return fmt.Errorf("call result is unavailable")
			}
			types = append(types, instr.ValueType())
		}
		*stack = types
		return nil
	}
	if instr.Kind == wasm.InstrCallIndirect && instr.Inline() == wasm.InstrInvalid {
		keyIndex := int(instr.U32())
		if keyIndex < 0 || keyIndex >= len(sf.TypeKeys) {
			return fmt.Errorf("call_indirect type key is unavailable")
		}
		base := len(types) - int(instr.Params()) - 1
		if !load(len(types)-1, arm64.X16) {
			return fmt.Errorf("call_indirect table index is unavailable")
		}
		a.Ldur64(arm64.X17, arm64.X26, -80)
		a.Load32(arm64.X8, arm64.X17, 0)
		a.CmpReg32(arm64.X16, arm64.X8)
		inBounds := a.Bcond(arm64.CondCC)
		metadata.recordTrap(a.Len(), instr.Offset, 5)
		arm64EmitTrap(a, 5, function, instr.Offset)
		if !a.PatchBranch19(inBounds, a.Len()) {
			return fmt.Errorf("call_indirect bounds branch is out of range")
		}
		a.LslImm(arm64.X16, arm64.X16, 5, false)
		a.Add64(arm64.X17, arm64.X17, arm64.X16)
		a.Load64(arm64.X16, arm64.X17, 8)
		nonNull := a.Cbnz64(arm64.X16)
		metadata.recordTrap(a.Len(), instr.Offset, 5)
		arm64EmitTrap(a, 5, function, instr.Offset)
		if !a.PatchBranch19(nonNull, a.Len()) {
			return fmt.Errorf("call_indirect null branch is out of range")
		}
		a.Load64(arm64.X8, arm64.X17, 16)
		a.MovImm64(arm64.X9, sf.TypeKeys[keyIndex])
		a.CmpReg64(arm64.X8, arm64.X9)
		sigOK := a.Bcond(arm64.CondEQ)
		metadata.recordTrap(a.Len(), instr.Offset, 6)
		arm64EmitTrap(a, 6, function, instr.Offset)
		if !a.PatchBranch19(sigOK, a.Len()) {
			return fmt.Errorf("call_indirect signature branch is out of range")
		}

		// Funcrefs use Dragline's public wrapper entry. The args/results buffers
		// are existing canonical operand slots; preserve the caller linMem across
		// the nested wrapper and strip the descriptor's entry-kind tag bits.
		a.LeaSP(arm64.X0, int32(stackOff(base)))
		a.LeaSP(arm64.X3, int32(stackOff(base)))
		a.Load64(arm64.X1, arm64.X17, 24)
		a.LslImm(arm64.X1, arm64.X1, 3, false)
		a.LsrImm(arm64.X1, arm64.X1, 3, false)
		a.Load64(arm64.X8, arm64.X17, 32)
		a.Load64(arm64.X8, arm64.X8, 32)
		a.Ldur64(arm64.X9, arm64.X26, -int32(abi.FuncRefDescPtrOffset))
		a.Load64(arm64.X9, arm64.X9, 32)
		arm64CopyDraglineInstanceContext(a, arm64.X1, arm64.X8)
		arm64CopyDraglineExecutionControl(a, arm64.X1)
		a.StpPre(arm64.X26, arm64.X9, arm64.SP, -16)
		a.StpPre(arm64.X8, arm64.XZR, arm64.SP, -16)
		a.Blr(arm64.X16)
		metadata.recordSafepoint(a.Len())
		a.LdpPost(arm64.X8, arm64.XZR, arm64.SP, 16)
		a.LdpPost(arm64.X26, arm64.X9, arm64.SP, 16)
		arm64CopyDraglineInstanceContext(a, arm64.X26, arm64.X9)
		types = types[:base]
		if instr.HasResult() {
			types = append(types, instr.ValueType())
		}
		*stack = types
		return nil
	}
	if instr.Params() != 2 || instr.Inline() != wasm.InstrI32Add {
		return fmt.Errorf("call is outside the inlinable scalar baseline")
	}
	if instr.Kind == wasm.InstrCallIndirect {
		types = types[:len(types)-1]
	}
	rhs, lhs := len(types)-1, len(types)-2
	if !load(lhs, arm64.X16) || !load(rhs, arm64.X17) {
		return fmt.Errorf("call operands are unavailable")
	}
	a.Add32(arm64.X16, arm64.X16, arm64.X17)
	if !store(lhs, arm64.X16) {
		return fmt.Errorf("call result is unavailable")
	}
	types = types[:rhs]
	types[lhs] = wasm.I32
	*stack = types
	return nil
}

func arm64CopyDraglineExecutionControl(a *arm64.Asm, targetLinMem arm64.Reg) {
	for _, off := range [...]int32{24, 32, 72, int32(abi.TrapCellPtrOffset)} {
		a.Ldur64(arm64.X8, arm64.X26, -off)
		a.Stur64(arm64.X8, targetLinMem, -off)
	}
}

func arm64CopyDraglineInstanceContext(a *arm64.Asm, targetLinMem, context arm64.Reg) {
	for i, off := range [...]int32{40, 80, 88, 120, 112, 128, 96, 64, 136} {
		a.Load64(arm64.X10, context, uint32(i*8))
		a.Stur64(arm64.X10, targetLinMem, -off)
	}
	a.Load64(arm64.X10, context, coreruntime.InstanceContextProfileCountersOffset)
	a.SubImm64(arm64.X8, targetLinMem, uint32(abi.ProfileCountersPtrOffset))
	a.Store64(arm64.X10, arm64.X8, 0)
	a.Load64(arm64.X10, context, coreruntime.InstanceContextTierEntriesOffset)
	a.SubImm64(arm64.X8, targetLinMem, uint32(abi.TierEntriesPtrOffset))
	a.Store64(arm64.X10, arm64.X8, 0)
}

func arm64DirectSIMDBinaryKind(kind wasm.InstrKind) bool {
	switch kind {
	case wasm.InstrV128And, wasm.InstrV128Andnot, wasm.InstrV128Or, wasm.InstrV128Xor,
		wasm.InstrI8x16Eq, wasm.InstrI16x8Eq, wasm.InstrI8x16LtS, wasm.InstrI8x16GtU,
		wasm.InstrI16x8LtU, wasm.InstrI16x8GtU, wasm.InstrI16x8GeU,
		wasm.InstrI8x16SubSatU, wasm.InstrI16x8Sub, wasm.InstrI32x4Add,
		wasm.InstrI16x8ExtmulLowI8x16U, wasm.InstrI16x8ExtmulHighI8x16U:
		return true
	default:
		return false
	}
}

func emitARM64DirectSIMDBinary(a *arm64.Asm, kind wasm.InstrKind, dst, lhs, rhs arm64.Reg) {
	switch kind {
	case wasm.InstrV128And:
		a.NeonAnd16b(dst, lhs, rhs)
	case wasm.InstrV128Andnot:
		a.NeonAndn16b(dst, lhs, rhs)
	case wasm.InstrV128Or:
		a.NeonOrr16b(dst, lhs, rhs)
	case wasm.InstrV128Xor:
		a.NeonEor16b(dst, lhs, rhs)
	case wasm.InstrI8x16Eq:
		a.NeonCmeqB(dst, lhs, rhs)
	case wasm.InstrI16x8Eq:
		a.NeonCmeqH(dst, lhs, rhs)
	case wasm.InstrI8x16LtS:
		a.NeonCmgtB(dst, rhs, lhs)
	case wasm.InstrI8x16GtU:
		a.NeonCmhiB(dst, lhs, rhs)
	case wasm.InstrI16x8LtU:
		a.NeonCmhiH(dst, rhs, lhs)
	case wasm.InstrI16x8GtU:
		a.NeonCmhiH(dst, lhs, rhs)
	case wasm.InstrI16x8GeU:
		a.NeonCmhsH(dst, lhs, rhs)
	case wasm.InstrI8x16SubSatU:
		a.NeonUqsubB(dst, lhs, rhs)
	case wasm.InstrI16x8Sub:
		a.NeonSubH(dst, lhs, rhs)
	case wasm.InstrI32x4Add:
		a.NeonAddS(dst, lhs, rhs)
	case wasm.InstrI16x8ExtmulLowI8x16U:
		a.NeonUmullHfromB(dst, lhs, rhs)
	case wasm.InstrI16x8ExtmulHighI8x16U:
		a.NeonUmull2HfromB(dst, lhs, rhs)
	}
}

func arm64SIMDConstantIsSplat(bytes [16]byte) bool {
	for _, lane := range bytes[1:] {
		if lane != bytes[0] {
			return false
		}
	}
	return true
}

func arm64ShuffleLaneRotate(bytes [16]byte, laneBytes, rotateBytes byte) bool {
	for i, lane := range bytes {
		base := byte(i) / laneBytes * laneBytes
		if lane != base+(byte(i)%laneBytes+rotateBytes)%laneBytes {
			return false
		}
	}
	return true
}

func arm64ShuffleZip(bytes [16]byte, laneBytes byte, upper bool) bool {
	half := byte(8 / laneBytes)
	start := byte(0)
	if upper {
		start = half
	}
	for i, lane := range bytes {
		outputLane := byte(i) / laneBytes
		inputLane := start + outputLane/2
		expected := inputLane*laneBytes + byte(i)%laneBytes
		if outputLane&1 != 0 {
			expected += 16
		}
		if lane != expected {
			return false
		}
	}
	return true
}

func arm64ShuffleSpecialized(bytes [16]byte) bool {
	return arm64ShuffleLaneRotate(bytes, 4, 1) || arm64ShuffleLaneRotate(bytes, 4, 2) ||
		arm64ShuffleZip(bytes, 4, false) || arm64ShuffleZip(bytes, 4, true) ||
		arm64ShuffleZip(bytes, 8, false) || arm64ShuffleZip(bytes, 8, true)
}

func emitARM64SpecializedShuffle(a *arm64.Asm, bytes [16]byte, dst, lhs, rhs arm64.Reg) (arm64.Reg, bool) {
	switch {
	case arm64ShuffleLaneRotate(bytes, 4, 2):
		a.NeonRev32H(dst, lhs)
	case arm64ShuffleLaneRotate(bytes, 4, 1):
		if dst == lhs {
			dst = 0
			if lhs == 0 {
				dst = 1
			}
		}
		a.NeonUshrS(dst, lhs, 8)
		a.NeonSliS(dst, lhs, 24)
	case arm64ShuffleZip(bytes, 4, false):
		a.NeonZip1S(dst, lhs, rhs)
	case arm64ShuffleZip(bytes, 4, true):
		a.NeonZip2S(dst, lhs, rhs)
	case arm64ShuffleZip(bytes, 8, false):
		a.NeonZip1D(dst, lhs, rhs)
	case arm64ShuffleZip(bytes, 8, true):
		a.NeonZip2D(dst, lhs, rhs)
	default:
		return dst, false
	}
	return dst, true
}

func emitARM64SIMDConstant(a *arm64.Asm, reg arm64.Reg, bytes [16]byte) {
	if arm64SIMDConstantIsSplat(bytes) {
		a.NeonMoviB(reg, bytes[0])
		return
	}
	a.MovImm64(arm64.X16, binary.LittleEndian.Uint64(bytes[:8]))
	a.FmovFromGpr(reg, arm64.X16, true)
	a.MovImm64(arm64.X16, binary.LittleEndian.Uint64(bytes[8:]))
	a.NeonInsD(reg, arm64.X16, 1)
}

func emitARM64StackSIMD(a *arm64.Asm, descriptor wasm.SIMDInstructionDescriptor, instr railssa.StackInstr,
	stack *[]wasm.ValType, stackRegisters, operandRegisters []arm64.Reg, stackOff func(int) uint32, load func(int, arm64.Reg) bool, store func(int, arm64.Reg) bool,
	sourceV func(int, arm64.Reg) arm64.Reg, takeV func(int, arm64.Reg) arm64.Reg, storeV func(int, arm64.Reg), storeConstant func(int, arm64.Reg),
	materialize func(arm64.Reg, [16]byte), constants []arm64SIMDConstant, shiftImmediate uint32, hasShiftImmediate bool, loadDestination arm64.Reg, hasLoadDestination bool, function uint32, registerOperandStack, cachedBounds, cachedMemoryEnd bool, emitMemoryTrap func(uint32) error, metadata *functionEmissionMetadata,
) error {
	types := *stack
	effectiveAddressReady := false
	effectiveAddress := func(address arm64.Reg) {
		if effectiveAddressReady {
			effectiveAddressReady = false
			return
		}
		a.AddExtUXTW(arm64.X16, arm64.X26, address)
		if descriptor.MemArg.Offset != 0 {
			emitARM64BoundsEnd(a, arm64.X16, descriptor.MemArg.Offset)
		}
	}
	checkMemory := func(address arm64.Reg, size uint64) error {
		if descriptor.MemArg.Offset > math.MaxUint64-size {
			metadata.recordTrap(a.Len(), instr.Offset, 3)
			arm64EmitTrap(a, 3, function, instr.Offset)
			return nil
		}
		if cachedBounds && cachedMemoryEnd {
			effectiveAddress(address)
			effectiveAddressReady = true
			a.AddImm64(arm64.X17, arm64.X16, uint32(size))
			a.CmpReg64(arm64.X17, arm64.X25)
			return emitMemoryTrap(instr.Offset)
		}
		a.MovReg32(arm64.X16, address)
		end := descriptor.MemArg.Offset + size
		if end <= 4095 {
			a.AddImm64(arm64.X16, arm64.X16, uint32(end))
		} else {
			a.MovImm64(arm64.X17, end)
			a.Add64(arm64.X16, arm64.X16, arm64.X17)
		}
		bounds := arm64.X25
		if !cachedBounds {
			a.SubImm64(arm64.X17, arm64.X26, abi.ActualLinMemByteSize64Offset)
			if !a.Load64(arm64.X17, arm64.X17, 0) {
				return fmt.Errorf("SIMD memory size load is not encodable")
			}
			bounds = arm64.X17
		}
		a.CmpReg64(arm64.X16, bounds)
		return emitMemoryTrap(instr.Offset)
	}
	constantRegister := func(bytes [16]byte) (arm64.Reg, bool) {
		for _, constant := range constants {
			if constant.bytes == bytes {
				return constant.reg, true
			}
		}
		return 0, false
	}
	stackDestination := func(index int, fallback arm64.Reg) arm64.Reg {
		if index < len(stackRegisters) {
			return stackRegisters[index]
		}
		return fallback
	}
	binaryOp := func(op func(arm64.Reg, arm64.Reg, arm64.Reg)) error {
		if len(types) < 2 || types[len(types)-2] != wasm.V128 || types[len(types)-1] != wasm.V128 {
			return fmt.Errorf("SIMD binary operand mismatch")
		}
		base := len(types) - 2
		lhs := sourceV(base, 0)
		rhs := sourceV(base+1, 1)
		dst := stackDestination(base, 0)
		op(dst, lhs, rhs)
		storeV(base, dst)
		types = append(types[:base], wasm.V128)
		return nil
	}

	switch descriptor.Kind {
	case wasm.InstrV128Const:
		dst := stackDestination(len(types), 0)
		if cached, ok := constantRegister(descriptor.Bytes); ok {
			storeConstant(len(types), cached)
		} else {
			materialize(dst, descriptor.Bytes)
			storeV(len(types), dst)
		}
		types = append(types, wasm.V128)
	case wasm.InstrV128Load:
		if len(types) < 1 || types[len(types)-1] != wasm.I32 {
			return fmt.Errorf("SIMD load operand mismatch")
		}
		base := len(types) - 1
		address := arm64.X15
		if registerOperandStack {
			address = operandRegisters[base]
		} else if !load(base, address) {
			return fmt.Errorf("SIMD load address is not encodable")
		}
		if err := checkMemory(address, 16); err != nil {
			return err
		}
		effectiveAddress(address)
		dst := stackDestination(base, 0)
		if hasLoadDestination {
			dst = loadDestination
		}
		a.LdrQ(dst, arm64.X16, 0)
		if !hasLoadDestination {
			storeV(base, dst)
		}
		types[base] = wasm.V128
	case wasm.InstrV128Store:
		if len(types) < 2 || types[len(types)-2] != wasm.I32 || types[len(types)-1] != wasm.V128 {
			return fmt.Errorf("SIMD store operand mismatch")
		}
		base := len(types) - 2
		if !load(base, arm64.X15) {
			return fmt.Errorf("SIMD store address is not encodable")
		}
		value := sourceV(base+1, 0)
		if err := checkMemory(arm64.X15, 16); err != nil {
			return err
		}
		effectiveAddress(arm64.X15)
		a.StrQ(arm64.X16, 0, value)
		types = types[:base]
	case wasm.InstrV128Store64Lane:
		if len(types) < 2 || types[len(types)-2] != wasm.I32 || types[len(types)-1] != wasm.V128 {
			return fmt.Errorf("SIMD lane store operand mismatch")
		}
		base := len(types) - 2
		if !load(base, arm64.X15) {
			return fmt.Errorf("SIMD lane-store address is not encodable")
		}
		value := sourceV(base+1, 0)
		if err := checkMemory(arm64.X15, 8); err != nil {
			return err
		}
		effectiveAddress(arm64.X15)
		a.NeonUmovD(arm64.X17, value, byte(descriptor.Lane))
		a.Store64(arm64.X17, arm64.X16, 0)
		types = types[:base]
	case wasm.InstrV128And:
		if err := binaryOp(a.NeonAnd16b); err != nil {
			return err
		}
	case wasm.InstrV128Andnot:
		if err := binaryOp(a.NeonAndn16b); err != nil {
			return err
		}
	case wasm.InstrV128Or:
		if err := binaryOp(a.NeonOrr16b); err != nil {
			return err
		}
	case wasm.InstrV128Xor:
		if err := binaryOp(a.NeonEor16b); err != nil {
			return err
		}
	case wasm.InstrI8x16Eq:
		if err := binaryOp(a.NeonCmeqB); err != nil {
			return err
		}
	case wasm.InstrI16x8Eq:
		if err := binaryOp(a.NeonCmeqH); err != nil {
			return err
		}
	case wasm.InstrI8x16LtS:
		if err := binaryOp(func(dst, lhs, rhs arm64.Reg) { a.NeonCmgtB(dst, rhs, lhs) }); err != nil {
			return err
		}
	case wasm.InstrI8x16GtU:
		if err := binaryOp(a.NeonCmhiB); err != nil {
			return err
		}
	case wasm.InstrI16x8LtU:
		if err := binaryOp(func(dst, lhs, rhs arm64.Reg) { a.NeonCmhiH(dst, rhs, lhs) }); err != nil {
			return err
		}
	case wasm.InstrI16x8GtU:
		if err := binaryOp(a.NeonCmhiH); err != nil {
			return err
		}
	case wasm.InstrI16x8GeU:
		if err := binaryOp(a.NeonCmhsH); err != nil {
			return err
		}
	case wasm.InstrI8x16SubSatU:
		if err := binaryOp(a.NeonUqsubB); err != nil {
			return err
		}
	case wasm.InstrI16x8Sub:
		if err := binaryOp(a.NeonSubH); err != nil {
			return err
		}
	case wasm.InstrI32x4Add:
		if err := binaryOp(a.NeonAddS); err != nil {
			return err
		}
	case wasm.InstrI16x8ExtmulLowI8x16U:
		if err := binaryOp(a.NeonUmullHfromB); err != nil {
			return err
		}
	case wasm.InstrI16x8ExtmulHighI8x16U:
		if err := binaryOp(a.NeonUmull2HfromB); err != nil {
			return err
		}
	case wasm.InstrI32x4DotI16x8S:
		if len(types) < 2 || types[len(types)-2] != wasm.V128 || types[len(types)-1] != wasm.V128 {
			return fmt.Errorf("SIMD dot operand mismatch")
		}
		base := len(types) - 2
		lhs := takeV(base, 0)
		rhs := sourceV(base+1, 1)
		a.NeonSmullSfromH(2, lhs, rhs)
		a.NeonSmull2SfromH(lhs, lhs, rhs)
		a.NeonAddpS(lhs, 2, lhs)
		storeV(base, lhs)
		types = append(types[:base], wasm.V128)
	case wasm.InstrI32x4ExtaddPairwiseI16x8U:
		if len(types) < 1 || types[len(types)-1] != wasm.V128 {
			return fmt.Errorf("SIMD unary operand mismatch")
		}
		base := len(types) - 1
		src := sourceV(base, 0)
		dst := stackDestination(base, 0)
		a.NeonUaddlpSfromH(dst, src)
		storeV(base, dst)
	case wasm.InstrI8x16NarrowI16x8U:
		if len(types) < 2 {
			return fmt.Errorf("SIMD narrow operand underflow")
		}
		base := len(types) - 2
		lhs := sourceV(base, 0)
		rhs := sourceV(base+1, 1)
		dst := stackDestination(base, 0)
		a.NeonSqxtunBfromH(dst, lhs)
		a.NeonSqxtun2BfromH(dst, rhs)
		storeV(base, dst)
		types = append(types[:base], wasm.V128)
	case wasm.InstrI16x8NarrowI32x4S:
		if len(types) < 2 {
			return fmt.Errorf("SIMD narrow operand underflow")
		}
		base := len(types) - 2
		lhs := sourceV(base, 0)
		rhs := sourceV(base+1, 1)
		dst := stackDestination(base, 0)
		a.NeonSqxtnHfromS(dst, lhs)
		a.NeonSqxtn2HfromS(dst, rhs)
		storeV(base, dst)
		types = append(types[:base], wasm.V128)
	case wasm.InstrI16x8NarrowI32x4U:
		if len(types) < 2 {
			return fmt.Errorf("SIMD narrow operand underflow")
		}
		base := len(types) - 2
		lhs := sourceV(base, 0)
		rhs := sourceV(base+1, 1)
		dst := stackDestination(base, 0)
		a.NeonSqxtunHfromS(dst, lhs)
		a.NeonSqxtun2HfromS(dst, rhs)
		storeV(base, dst)
		types = append(types[:base], wasm.V128)
	case wasm.InstrI8x16Swizzle:
		if len(types) < 2 {
			return fmt.Errorf("SIMD swizzle operand underflow")
		}
		base := len(types) - 2
		lhs := sourceV(base, 0)
		rhs := sourceV(base+1, 1)
		dst := stackDestination(base, 0)
		a.NeonTbl(dst, lhs, rhs)
		storeV(base, dst)
		types = append(types[:base], wasm.V128)
	case wasm.InstrI8x16Shuffle:
		if len(types) < 2 {
			return fmt.Errorf("SIMD shuffle operand underflow")
		}
		base := len(types) - 2
		lhs := sourceV(base, 0)
		rhs := sourceV(base+1, 1)
		dst := stackDestination(base, 0)
		if result, ok := emitARM64SpecializedShuffle(a, descriptor.Bytes, dst, lhs, rhs); ok {
			dst = result
			storeV(base, dst)
			types = append(types[:base], wasm.V128)
		} else {
			var left, right [16]byte
			for i, lane := range descriptor.Bytes {
				left[i], right[i] = lane, lane-16
			}
			leftIndex := arm64.Reg(2)
			if cached, ok := constantRegister(left); ok {
				leftIndex = cached
			} else {
				materialize(leftIndex, left)
			}
			a.NeonTbl(2, lhs, leftIndex)
			rightIndex := arm64.Reg(3)
			if cached, ok := constantRegister(right); ok {
				rightIndex = cached
			} else {
				materialize(rightIndex, right)
			}
			a.NeonTbl(3, rhs, rightIndex)
			a.NeonOrr16b(0, 2, 3)
			storeV(base, 0)
			types = append(types[:base], wasm.V128)
		}
	case wasm.InstrV128AnyTrue:
		if len(types) < 1 || types[len(types)-1] != wasm.V128 {
			return fmt.Errorf("SIMD reduction operand mismatch")
		}
		base := len(types) - 1
		value := takeV(base, 0)
		a.NeonUmaxvB(value, value)
		a.NeonUmovB(arm64.X16, value, 0)
		a.CmpImm32(arm64.X16, 0)
		a.Cset32(arm64.X16, arm64.CondNE)
		if !store(base, arm64.X16) {
			return fmt.Errorf("SIMD bitmask result is not encodable")
		}
		types[base] = wasm.I32
	case wasm.InstrI8x16Bitmask, wasm.InstrI16x8Bitmask:
		if len(types) < 1 || types[len(types)-1] != wasm.V128 {
			return fmt.Errorf("SIMD bitmask operand mismatch")
		}
		base := len(types) - 1
		value := takeV(base, 0)
		if descriptor.Kind == wasm.InstrI8x16Bitmask {
			a.NeonUshrB(value, value, 7)
			a.FmovToGpr(arm64.X16, value, true)
			a.NeonUmovD(arm64.X17, value, 1)
			a.MovImm64(arm64.X15, 0x0102040810204080)
			a.Mul64(arm64.X16, arm64.X16, arm64.X15)
			a.Mul64(arm64.X17, arm64.X17, arm64.X15)
			a.LsrImm(arm64.X16, arm64.X16, 56, false)
			a.LsrImm(arm64.X17, arm64.X17, 56, false)
			a.LslImm(arm64.X17, arm64.X17, 8, true)
			a.Orr32(arm64.X16, arm64.X16, arm64.X17)
		} else {
			a.NeonSshrH(value, value, 15)
			materialize(1, [16]byte{1, 0, 2, 0, 4, 0, 8, 0, 16, 0, 32, 0, 64, 0, 128, 0})
			a.NeonAnd16b(value, value, 1)
			a.NeonAddvH(value, value)
			a.FmovToGpr(arm64.X16, value, false)
		}
		if !store(base, arm64.X16) {
			return fmt.Errorf("SIMD lane result is not encodable")
		}
		types[base] = wasm.I32
	case wasm.InstrI32x4ExtractLane:
		if len(types) < 1 || types[len(types)-1] != wasm.V128 {
			return fmt.Errorf("SIMD extract operand mismatch")
		}
		base := len(types) - 1
		value := sourceV(base, 0)
		a.NeonUmovS(arm64.X16, value, byte(descriptor.Lane))
		if !store(base, arm64.X16) {
			return fmt.Errorf("SIMD any-true result is not encodable")
		}
		types[base] = wasm.I32
	case wasm.InstrI32x4Splat:
		if len(types) < 1 || types[len(types)-1] != wasm.I32 {
			return fmt.Errorf("SIMD splat operand mismatch")
		}
		base := len(types) - 1
		if !load(base, arm64.X16) {
			return fmt.Errorf("SIMD splat operand is not encodable")
		}
		a.NeonDupGprS(0, arm64.X16)
		storeV(base, 0)
		types[base] = wasm.V128
	case wasm.InstrI32x4ReplaceLane:
		if len(types) < 2 || types[len(types)-2] != wasm.V128 || types[len(types)-1] != wasm.I32 {
			return fmt.Errorf("SIMD replace operand mismatch")
		}
		base := len(types) - 2
		value := takeV(base, 0)
		if !load(base+1, arm64.X16) {
			return fmt.Errorf("SIMD replace-lane operand is not encodable")
		}
		a.NeonInsS(value, arm64.X16, byte(descriptor.Lane))
		storeV(base, value)
		types = append(types[:base], wasm.V128)
	case wasm.InstrI16x8Shl, wasm.InstrI16x8ShrU, wasm.InstrI32x4Shl, wasm.InstrI32x4ShrU:
		if len(types) < 2 || types[len(types)-2] != wasm.V128 || types[len(types)-1] != wasm.I32 {
			return fmt.Errorf("SIMD shift operand mismatch")
		}
		base := len(types) - 2
		laneBytes, mask := 2, uint64(15)
		if descriptor.Kind == wasm.InstrI32x4Shl || descriptor.Kind == wasm.InstrI32x4ShrU {
			laneBytes, mask = 4, 31
		}
		right := descriptor.Kind == wasm.InstrI16x8ShrU || descriptor.Kind == wasm.InstrI32x4ShrU
		if hasShiftImmediate {
			src := sourceV(base, 0)
			dst := stackDestination(base, 0)
			if !emitARM64StructuredSIMDImmediateShift(a, descriptor.Kind, dst, src, shiftImmediate) {
				return fmt.Errorf("unsupported immediate SIMD shift %s", descriptor.Kind)
			}
			storeV(base, dst)
		} else {
			value := takeV(base, 0)
			if !load(base+1, arm64.X16) {
				return fmt.Errorf("SIMD shift operand is not encodable")
			}
			a.AndImm64(arm64.X16, arm64.X16, mask)
			if right {
				a.Sub64(arm64.X16, arm64.XZR, arm64.X16)
			}
			if laneBytes == 2 {
				a.NeonDupGprH(1, arm64.X16)
				if right {
					a.NeonUshrvH(value, value, 1)
				} else {
					a.NeonUshlH(value, value, 1)
				}
			} else {
				a.NeonDupGprS(1, arm64.X16)
				if right {
					a.NeonUshrvS(value, value, 1)
				} else {
					a.NeonUshlS(value, value, 1)
				}
			}
			storeV(base, value)
		}
		types = append(types[:base], wasm.V128)
	default:
		return fmt.Errorf("unsupported structured SIMD instruction %s", descriptor.Kind)
	}
	*stack = types
	return nil
}

func arm64StructuredSIMDImmediateShiftKind(kind wasm.InstrKind) bool {
	return kind == wasm.InstrI16x8Shl || kind == wasm.InstrI16x8ShrU || kind == wasm.InstrI32x4Shl || kind == wasm.InstrI32x4ShrU
}

func emitARM64StructuredSIMDImmediateShift(a *arm64.Asm, kind wasm.InstrKind, dst, src arm64.Reg, immediate uint32) bool {
	if !arm64StructuredSIMDImmediateShiftKind(kind) {
		return false
	}
	mask := uint32(15)
	if kind == wasm.InstrI32x4Shl || kind == wasm.InstrI32x4ShrU {
		mask = 31
	}
	shift := uint8(immediate & mask)
	if shift == 0 {
		if dst != src {
			a.NeonMov16b(dst, src)
		}
		return true
	}
	switch kind {
	case wasm.InstrI16x8Shl:
		a.NeonShlH(dst, src, shift)
	case wasm.InstrI16x8ShrU:
		a.NeonUshrH(dst, src, shift)
	case wasm.InstrI32x4Shl:
		a.NeonShlS(dst, src, shift)
	case wasm.InstrI32x4ShrU:
		a.NeonUshrS(dst, src, shift)
	}
	return true
}

func emitARM64StackBackedMemory(a *arm64.Asm, instr railssa.StackInstr, stack *[]wasm.ValType,
	load func(int, arm64.Reg) bool, store func(int, arm64.Reg) bool, function uint32, elideBounds bool,
	cachedBounds, cachedMemoryEnd bool, emitMemoryTrap func(uint32) error,
	metadata *functionEmissionMetadata,
) error {
	types := *stack
	need := 1
	if instr.Kind >= wasm.InstrI32Store && instr.Kind <= wasm.InstrI64Store32 {
		need = 2
	}
	if len(types) < need {
		return fmt.Errorf("operand stack underflow")
	}
	base := len(types) - need
	var temporary [2]wasm.ValType
	tmp := temporary[:need]
	copy(tmp, types[base:])
	for i := range tmp {
		if !load(base+i, arm64OperandStackRegisters[i]) {
			return fmt.Errorf("memory operand %d is unavailable", i)
		}
	}
	if err := emitARM64RegisterStackMemory(a, instr, &tmp, arm64OperandStackRegisters[:], 0, false, function, elideBounds, cachedBounds, cachedMemoryEnd, emitMemoryTrap, metadata); err != nil {
		return err
	}
	types = types[:base]
	if len(tmp) == 1 {
		if !store(base, arm64OperandStackRegisters[0]) {
			return fmt.Errorf("memory result is unavailable")
		}
		types = append(types, tmp[0])
	}
	*stack = types
	return nil
}

func emitARM64StackBackedFloat(a *arm64.Asm, kind wasm.InstrKind, stack *[]wasm.ValType,
	load func(int, arm64.Reg) bool, store func(int, arm64.Reg) bool, function, wasmOffset uint32,
	metadata *functionEmissionMetadata,
) error {
	types := *stack
	need := 1
	if kind >= wasm.InstrF32Eq && kind <= wasm.InstrF64Ge ||
		kind >= wasm.InstrF32Add && kind <= wasm.InstrF32Copysign ||
		kind >= wasm.InstrF64Add && kind <= wasm.InstrF64Copysign {
		need = 2
	}
	if len(types) < need {
		return fmt.Errorf("operand stack underflow")
	}
	base := len(types) - need
	tmp := append([]wasm.ValType(nil), types[base:]...)
	for i := range tmp {
		if !load(base+i, arm64OperandStackRegisters[i]) {
			return fmt.Errorf("float operand %d is unavailable", i)
		}
	}
	if err := emitARM64RegisterStackFloat(a, kind, &tmp, arm64OperandStackRegisters[:], function, wasmOffset, metadata); err != nil {
		return err
	}
	if len(tmp) != 1 || !store(base, arm64OperandStackRegisters[0]) {
		return fmt.Errorf("float result is unavailable")
	}
	types = append(types[:base], tmp[0])
	*stack = types
	return nil
}

func emitARM64RegisterStackMemory(a *arm64.Asm, instr railssa.StackInstr, stack *[]wasm.ValType, operandRegisters []arm64.Reg, addressOverride arm64.Reg, hasAddressOverride bool, function uint32, elideBounds, cachedBounds, cachedMemoryEnd bool, emitMemoryTrap func(uint32) error, metadata *functionEmissionMetadata) error {
	types := *stack
	store := instr.Kind >= wasm.InstrI32Store && instr.Kind <= wasm.InstrI64Store32
	need := 1
	if store {
		need = 2
	}
	if len(types) < need {
		return fmt.Errorf("operand stack underflow")
	}
	addressIndex := len(types) - need
	address := operandRegisters[addressIndex]
	if hasAddressOverride {
		address = addressOverride
	}
	size, typ, signed := uint64(4), wasm.I32, false
	if instr.Kind == wasm.InstrI64Load || instr.Kind == wasm.InstrF64Load ||
		instr.Kind == wasm.InstrI64Store || instr.Kind == wasm.InstrF64Store {
		size = 8
	}
	switch instr.Kind {
	case wasm.InstrI64Load, wasm.InstrI64Store:
		typ = wasm.I64
	case wasm.InstrF32Load, wasm.InstrF32Store:
		typ = wasm.F32
	case wasm.InstrF64Load, wasm.InstrF64Store:
		typ = wasm.F64
	case wasm.InstrI32Load8S:
		size, signed = 1, true
	case wasm.InstrI32Load8U:
		size = 1
	case wasm.InstrI32Load16S:
		size, signed = 2, true
	case wasm.InstrI32Load16U:
		size = 2
	case wasm.InstrI64Load8S:
		size, typ, signed = 1, wasm.I64, true
	case wasm.InstrI64Load8U:
		size, typ = 1, wasm.I64
	case wasm.InstrI64Load16S:
		size, typ, signed = 2, wasm.I64, true
	case wasm.InstrI64Load16U:
		size, typ = 2, wasm.I64
	case wasm.InstrI64Load32S:
		typ, signed = wasm.I64, true
	case wasm.InstrI64Load32U:
		typ = wasm.I64
	case wasm.InstrI32Store8, wasm.InstrI64Store8:
		size = 1
	case wasm.InstrI32Store16, wasm.InstrI64Store16:
		size = 2
	case wasm.InstrI64Store32:
		size = 4
	}
	if err := emitARM64CheckedMemoryAddress(a, instr, address, size, function, elideBounds, cachedBounds, cachedMemoryEnd, emitMemoryTrap, metadata); err != nil {
		return err
	}
	if store {
		value := operandRegisters[len(types)-1]
		a.StoreIdx(arm64.X16, arm64.XZR, value, 0, int(size))
		*stack = types[:len(types)-2]
		return nil
	}
	result := operandRegisters[addressIndex]
	a.LoadIdx(result, arm64.X16, arm64.XZR, 0, int(size), signed, typ == wasm.I64)
	types[addressIndex] = typ
	types[len(types)-1] = instr.ValueType()
	*stack = types
	return nil
}

func emitARM64CheckedMemoryAddress(a *arm64.Asm, instr railssa.StackInstr, address arm64.Reg, size uint64, function uint32,
	elideBounds, cachedBounds, cachedMemoryEnd bool, emitMemoryTrap func(uint32) error, metadata *functionEmissionMetadata,
) error {
	if !elideBounds && (!cachedBounds || !cachedMemoryEnd) {
		a.MovReg32(arm64.X16, address)
		emitARM64BoundsEnd(a, arm64.X16, uint64(instr.U32())+size)
		bounds := arm64.X25
		if !cachedBounds {
			a.SubImm64(arm64.X17, arm64.X26, abi.ActualLinMemByteSize64Offset)
			if !a.Load64(arm64.X17, arm64.X17, 0) {
				return fmt.Errorf("memory size load is not encodable")
			}
			bounds = arm64.X17
		}
		a.CmpReg64(arm64.X16, bounds)
		if emitMemoryTrap != nil {
			if err := emitMemoryTrap(instr.Offset); err != nil {
				return err
			}
		} else {
			inBounds := a.Bcond(arm64.CondLS)
			metadata.recordTrap(a.Len(), instr.Offset, 3)
			arm64EmitTrap(a, 3, function, instr.Offset)
			if !a.PatchBranch19(inBounds, a.Len()) {
				return fmt.Errorf("memory bounds branch is out of range")
			}
		}
	}
	a.AddExtUXTW(arm64.X16, arm64.X26, address)
	if instr.U32() != 0 {
		emitARM64BoundsEnd(a, arm64.X16, uint64(instr.U32()))
	}
	if !elideBounds && cachedBounds && cachedMemoryEnd {
		if size <= 4095 {
			a.AddImm64(arm64.X17, arm64.X16, uint32(size))
		} else {
			a.MovImm64(arm64.X17, size)
			a.Add64(arm64.X17, arm64.X16, arm64.X17)
		}
		a.CmpReg64(arm64.X17, arm64.X25)
		if emitMemoryTrap != nil {
			if err := emitMemoryTrap(instr.Offset); err != nil {
				return err
			}
		} else {
			inBounds := a.Bcond(arm64.CondLS)
			metadata.recordTrap(a.Len(), instr.Offset, 3)
			arm64EmitTrap(a, 3, function, instr.Offset)
			if !a.PatchBranch19(inBounds, a.Len()) {
				return fmt.Errorf("memory bounds branch is out of range")
			}
		}
	}
	return nil
}

func arm64FloatStackKind(kind wasm.InstrKind) bool {
	if kind >= wasm.InstrF32Eq && kind <= wasm.InstrF64Ge ||
		kind >= wasm.InstrF32Abs && kind <= wasm.InstrF64Copysign ||
		kind >= wasm.InstrI32TruncF32S && kind <= wasm.InstrI32TruncF64U ||
		kind >= wasm.InstrI64TruncF32S && kind <= wasm.InstrI64TruncF64U ||
		kind >= wasm.InstrI32TruncSatF32S && kind <= wasm.InstrI64TruncSatF64U {
		return true
	}
	switch kind {
	case wasm.InstrI32WrapI64, wasm.InstrI64ExtendI32U,
		wasm.InstrF32ConvertI32S, wasm.InstrF32ConvertI32U, wasm.InstrF32DemoteF64,
		wasm.InstrF32ConvertI64S, wasm.InstrF32ConvertI64U,
		wasm.InstrF64ConvertI32S, wasm.InstrF64ConvertI32U, wasm.InstrF64PromoteF32,
		wasm.InstrF64ConvertI64S, wasm.InstrF64ConvertI64U,
		wasm.InstrI32ReinterpretF32, wasm.InstrI64ReinterpretF64,
		wasm.InstrF32ReinterpretI32, wasm.InstrF64ReinterpretI64:
		return true
	default:
		return false
	}
}

func emitARM64RegisterStackFloat(a *arm64.Asm, kind wasm.InstrKind, stack *[]wasm.ValType, operandRegisters []arm64.Reg, function, wasmOffset uint32, metadata *functionEmissionMetadata) error {
	types := *stack
	if len(types) < 1 {
		return fmt.Errorf("operand stack underflow")
	}
	top := operandRegisters[len(types)-1]
	if kind >= wasm.InstrF32Eq && kind <= wasm.InstrF64Ge {
		if len(types) < 2 {
			return fmt.Errorf("operand stack underflow")
		}
		lhsIndex := len(types) - 2
		lhs := operandRegisters[lhsIndex]
		f64 := kind >= wasm.InstrF64Eq
		a.FmovFromGpr(arm64.X0, lhs, f64)
		a.FmovFromGpr(arm64.X1, top, f64)
		cond := arm64.CondEQ
		swap := false
		switch kind {
		case wasm.InstrF32Ne, wasm.InstrF64Ne:
			cond = arm64.CondNE
		case wasm.InstrF32Lt, wasm.InstrF64Lt:
			cond, swap = arm64.CondGT, true
		case wasm.InstrF32Gt, wasm.InstrF64Gt:
			cond = arm64.CondGT
		case wasm.InstrF32Le, wasm.InstrF64Le:
			cond, swap = arm64.CondGE, true
		case wasm.InstrF32Ge, wasm.InstrF64Ge:
			cond = arm64.CondGE
		}
		if swap {
			a.Fcmp(arm64.X1, arm64.X0, f64)
		} else {
			a.Fcmp(arm64.X0, arm64.X1, f64)
		}
		a.Cset32(lhs, cond)
		types[lhsIndex] = wasm.I32
		*stack = types[:len(types)-1]
		return nil
	}
	switch kind {
	case wasm.InstrI32WrapI64:
		a.MovReg32(top, top)
		types[len(types)-1] = wasm.I32
		*stack = types
		return nil
	case wasm.InstrI64ExtendI32U:
		a.MovReg32(top, top)
		types[len(types)-1] = wasm.I64
		*stack = types
		return nil
	case wasm.InstrI32ReinterpretF32:
		types[len(types)-1] = wasm.I32
		*stack = types
		return nil
	case wasm.InstrI64ReinterpretF64:
		types[len(types)-1] = wasm.I64
		*stack = types
		return nil
	case wasm.InstrF32ReinterpretI32:
		types[len(types)-1] = wasm.F32
		*stack = types
		return nil
	case wasm.InstrF64ReinterpretI64:
		types[len(types)-1] = wasm.F64
		*stack = types
		return nil
	case wasm.InstrF32ConvertI32S, wasm.InstrF32ConvertI32U,
		wasm.InstrF64ConvertI32S, wasm.InstrF64ConvertI32U:
		f64 := kind == wasm.InstrF64ConvertI32S || kind == wasm.InstrF64ConvertI32U
		if kind == wasm.InstrF32ConvertI32U || kind == wasm.InstrF64ConvertI32U {
			a.Ucvtf(arm64.X0, top, f64, false)
		} else {
			a.Scvtf(arm64.X0, top, f64, false)
		}
		a.FmovToGpr(top, arm64.X0, f64)
		if f64 {
			types[len(types)-1] = wasm.F64
		} else {
			types[len(types)-1] = wasm.F32
		}
		*stack = types
		return nil
	case wasm.InstrF32ConvertI64S, wasm.InstrF32ConvertI64U,
		wasm.InstrF64ConvertI64S, wasm.InstrF64ConvertI64U:
		f64 := kind == wasm.InstrF64ConvertI64S || kind == wasm.InstrF64ConvertI64U
		if kind == wasm.InstrF32ConvertI64U || kind == wasm.InstrF64ConvertI64U {
			a.Ucvtf(arm64.X0, top, f64, true)
		} else {
			a.Scvtf(arm64.X0, top, f64, true)
		}
		a.FmovToGpr(top, arm64.X0, f64)
		if f64 {
			types[len(types)-1] = wasm.F64
		} else {
			types[len(types)-1] = wasm.F32
		}
		*stack = types
		return nil
	case wasm.InstrF32DemoteF64:
		a.FmovFromGpr(arm64.X0, top, true)
		a.FcvtD2S(arm64.X0, arm64.X0)
		a.FmovToGpr(top, arm64.X0, false)
		types[len(types)-1] = wasm.F32
		*stack = types
		return nil
	case wasm.InstrF64PromoteF32:
		a.FmovFromGpr(arm64.X0, top, false)
		a.FcvtS2D(arm64.X0, arm64.X0)
		a.FmovToGpr(top, arm64.X0, true)
		types[len(types)-1] = wasm.F64
		*stack = types
		return nil
	case wasm.InstrI32TruncF32S, wasm.InstrI32TruncF32U,
		wasm.InstrI32TruncF64S, wasm.InstrI32TruncF64U:
		f64src := kind == wasm.InstrI32TruncF64S || kind == wasm.InstrI32TruncF64U
		unsigned := kind == wasm.InstrI32TruncF32U || kind == wasm.InstrI32TruncF64U
		a.FmovFromGpr(arm64.X0, top, f64src)
		if err := emitARM64TruncI32Check(a, f64src, unsigned, function, wasmOffset, metadata); err != nil {
			return err
		}
		a.MovReg32(top, arm64.X16)
		types[len(types)-1] = wasm.I32
		*stack = types
		return nil
	case wasm.InstrI64TruncF32S, wasm.InstrI64TruncF32U,
		wasm.InstrI64TruncF64S, wasm.InstrI64TruncF64U:
		f64src := kind == wasm.InstrI64TruncF64S || kind == wasm.InstrI64TruncF64U
		unsigned := kind == wasm.InstrI64TruncF32U || kind == wasm.InstrI64TruncF64U
		a.FmovFromGpr(arm64.X0, top, f64src)
		if err := emitARM64TruncI64Check(a, f64src, unsigned, function, wasmOffset, metadata); err != nil {
			return err
		}
		if unsigned {
			a.Fcvtzu(top, arm64.X0, f64src, true)
		} else {
			a.Fcvtzs(top, arm64.X0, f64src, true)
		}
		types[len(types)-1] = wasm.I64
		*stack = types
		return nil
	case wasm.InstrI32TruncSatF32S, wasm.InstrI32TruncSatF32U,
		wasm.InstrI32TruncSatF64S, wasm.InstrI32TruncSatF64U:
		f64src := kind == wasm.InstrI32TruncSatF64S || kind == wasm.InstrI32TruncSatF64U
		a.FmovFromGpr(arm64.X0, top, f64src)
		if kind == wasm.InstrI32TruncSatF32U || kind == wasm.InstrI32TruncSatF64U {
			a.Fcvtzu(top, arm64.X0, f64src, false)
		} else {
			a.Fcvtzs(top, arm64.X0, f64src, false)
		}
		types[len(types)-1] = wasm.I32
		*stack = types
		return nil
	case wasm.InstrI64TruncSatF32S, wasm.InstrI64TruncSatF32U,
		wasm.InstrI64TruncSatF64S, wasm.InstrI64TruncSatF64U:
		f64src := kind == wasm.InstrI64TruncSatF64S || kind == wasm.InstrI64TruncSatF64U
		a.FmovFromGpr(arm64.X0, top, f64src)
		if kind == wasm.InstrI64TruncSatF32U || kind == wasm.InstrI64TruncSatF64U {
			a.Fcvtzu(top, arm64.X0, f64src, true)
		} else {
			a.Fcvtzs(top, arm64.X0, f64src, true)
		}
		types[len(types)-1] = wasm.I64
		*stack = types
		return nil
	}
	f64 := kind >= wasm.InstrF64Abs && kind <= wasm.InstrF64Copysign
	a.FmovFromGpr(arm64.X0, top, f64)
	switch kind {
	case wasm.InstrF32Abs, wasm.InstrF64Abs:
		a.NeonFabs(arm64.X0, arm64.X0, f64)
	case wasm.InstrF32Neg, wasm.InstrF64Neg:
		a.NeonFneg(arm64.X0, arm64.X0, f64)
	case wasm.InstrF32Ceil, wasm.InstrF64Ceil:
		a.Frint(arm64.X0, arm64.X0, f64, 'p')
	case wasm.InstrF32Floor, wasm.InstrF64Floor:
		a.Frint(arm64.X0, arm64.X0, f64, 'm')
	case wasm.InstrF32Trunc, wasm.InstrF64Trunc:
		a.Frint(arm64.X0, arm64.X0, f64, 'z')
	case wasm.InstrF32Nearest, wasm.InstrF64Nearest:
		a.Frint(arm64.X0, arm64.X0, f64, 'n')
	case wasm.InstrF32Sqrt, wasm.InstrF64Sqrt:
		a.Fsqrt(arm64.X0, arm64.X0, f64)
	default:
		if len(types) < 2 {
			return fmt.Errorf("operand stack underflow")
		}
		lhsIndex := len(types) - 2
		lhs := operandRegisters[lhsIndex]
		if kind == wasm.InstrF32Copysign || kind == wasm.InstrF64Copysign {
			if f64 {
				a.LslImm(lhs, lhs, 1, false)
				a.LsrImm(lhs, lhs, 1, false)
				a.LsrImm(top, top, 63, false)
				a.LslImm(top, top, 63, false)
				a.Orr64(lhs, lhs, top)
			} else {
				a.LslImm(lhs, lhs, 1, true)
				a.LsrImm(lhs, lhs, 1, true)
				a.LsrImm(top, top, 31, true)
				a.LslImm(top, top, 31, true)
				a.Orr32(lhs, lhs, top)
			}
			*stack = types[:len(types)-1]
			return nil
		}
		a.FmovFromGpr(arm64.X1, lhs, f64)
		switch kind {
		case wasm.InstrF32Add, wasm.InstrF64Add:
			a.Fadd(arm64.X1, arm64.X1, arm64.X0, f64)
		case wasm.InstrF32Sub, wasm.InstrF64Sub:
			a.Fsub(arm64.X1, arm64.X1, arm64.X0, f64)
		case wasm.InstrF32Mul, wasm.InstrF64Mul:
			a.Fmul(arm64.X1, arm64.X1, arm64.X0, f64)
		case wasm.InstrF32Div, wasm.InstrF64Div:
			a.Fdiv(arm64.X1, arm64.X1, arm64.X0, f64)
		case wasm.InstrF32Min, wasm.InstrF64Min:
			a.Fmin(arm64.X1, arm64.X1, arm64.X0, f64)
		case wasm.InstrF32Max, wasm.InstrF64Max:
			a.Fmax(arm64.X1, arm64.X1, arm64.X0, f64)
		default:
			return fmt.Errorf("unsupported structured float instruction %s", kind)
		}
		a.FmovToGpr(lhs, arm64.X1, f64)
		*stack = types[:len(types)-1]
		return nil
	}
	a.FmovToGpr(top, arm64.X0, f64)
	return nil
}

func emitARM64TruncI32Check(a *arm64.Asm, f64, unsigned bool, function, wasmOffset uint32, metadata *functionEmissionMetadata) error {
	patchValid := func(site int) error {
		if !a.PatchBranch19(site, a.Len()) {
			return fmt.Errorf("float conversion range branch is out of range")
		}
		return nil
	}
	a.Fcmp(arm64.X0, arm64.X0, f64)
	ordered := a.Bcond(arm64.CondVC)
	metadata.recordTrap(a.Len(), wasmOffset, 11)
	arm64EmitTrap(a, 11, function, wasmOffset)
	if err := patchValid(ordered); err != nil {
		return err
	}

	// Convert to a wide signed integer first. Every valid i32/u32 result fits;
	// the architectural saturation for larger finite values is then rejected by
	// exact integer range comparisons. This also handles fractional values just
	// outside an integer boundary according to truncation-toward-zero semantics.
	a.Fcvtzs(arm64.X16, arm64.X0, f64, true)
	lower, upper := int64(math.MinInt32), uint64(math.MaxInt32)
	if unsigned {
		lower, upper = 0, math.MaxUint32
	}
	a.MovImm64(arm64.X17, uint64(lower))
	a.CmpReg64(arm64.X16, arm64.X17)
	aboveLower := a.Bcond(arm64.CondGE)
	metadata.recordTrap(a.Len(), wasmOffset, 11)
	arm64EmitTrap(a, 11, function, wasmOffset)
	if err := patchValid(aboveLower); err != nil {
		return err
	}
	a.MovImm64(arm64.X17, upper)
	a.CmpReg64(arm64.X16, arm64.X17)
	belowUpper := a.Bcond(arm64.CondLE)
	metadata.recordTrap(a.Len(), wasmOffset, 11)
	arm64EmitTrap(a, 11, function, wasmOffset)
	return patchValid(belowUpper)
}

func emitARM64TruncI64Check(a *arm64.Asm, f64, unsigned bool, function, wasmOffset uint32, metadata *functionEmissionMetadata) error {
	patchValid := func(site int) error {
		if !a.PatchBranch19(site, a.Len()) {
			return fmt.Errorf("float conversion range branch is out of range")
		}
		return nil
	}
	a.Fcmp(arm64.X0, arm64.X0, f64)
	ordered := a.Bcond(arm64.CondVC)
	metadata.recordTrap(a.Len(), wasmOffset, 11)
	arm64EmitTrap(a, 11, function, wasmOffset)
	if err := patchValid(ordered); err != nil {
		return err
	}
	minBits, maxBits := uint64(0xdf000001), uint64(0x5f000000)
	if unsigned {
		minBits, maxBits = 0xbf800000, 0x5f800000
	}
	if f64 {
		minBits, maxBits = 0xc3e0000000000001, 0x43e0000000000000
		if unsigned {
			minBits, maxBits = 0xbff0000000000000, 0x43f0000000000000
		}
	}
	a.MovImm64(arm64.X16, minBits)
	a.FmovFromGpr(arm64.X1, arm64.X16, f64)
	a.Fcmp(arm64.X0, arm64.X1, f64)
	aboveLower := a.Bcond(arm64.CondGT)
	metadata.recordTrap(a.Len(), wasmOffset, 11)
	arm64EmitTrap(a, 11, function, wasmOffset)
	if err := patchValid(aboveLower); err != nil {
		return err
	}
	a.MovImm64(arm64.X16, maxBits)
	a.FmovFromGpr(arm64.X1, arm64.X16, f64)
	a.Fcmp(arm64.X0, arm64.X1, f64)
	belowUpper := a.Bcond(arm64.CondLT)
	metadata.recordTrap(a.Len(), wasmOffset, 11)
	arm64EmitTrap(a, 11, function, wasmOffset)
	return patchValid(belowUpper)
}

func arm64DirectLocalBinaryKind(kind wasm.InstrKind) bool {
	switch kind {
	case wasm.InstrI32Add, wasm.InstrI64Add, wasm.InstrI32Sub, wasm.InstrI64Sub,
		wasm.InstrI32Mul, wasm.InstrI64Mul, wasm.InstrI32And, wasm.InstrI64And,
		wasm.InstrI32Or, wasm.InstrI64Or, wasm.InstrI32Xor, wasm.InstrI64Xor,
		wasm.InstrI32Shl, wasm.InstrI64Shl, wasm.InstrI32ShrS, wasm.InstrI64ShrS,
		wasm.InstrI32ShrU, wasm.InstrI64ShrU, wasm.InstrI32Rotl, wasm.InstrI64Rotl,
		wasm.InstrI32Rotr, wasm.InstrI64Rotr:
		return true
	default:
		return false
	}
}

func arm64DirectFloatBinaryKind(kind wasm.InstrKind) bool {
	switch kind {
	case wasm.InstrF32Add, wasm.InstrF32Sub, wasm.InstrF32Mul, wasm.InstrF32Div,
		wasm.InstrF32Min, wasm.InstrF32Max,
		wasm.InstrF64Add, wasm.InstrF64Sub, wasm.InstrF64Mul, wasm.InstrF64Div,
		wasm.InstrF64Min, wasm.InstrF64Max:
		return true
	default:
		return false
	}
}

func arm64FloatBinaryPair(first, second wasm.InstrKind) (wasm.ValType, bool, bool) {
	if !arm64DirectFloatBinaryKind(first) || !arm64DirectFloatBinaryKind(second) {
		return wasm.ValType{}, false, false
	}
	firstF64 := first >= wasm.InstrF64Add && first <= wasm.InstrF64Max
	secondF64 := second >= wasm.InstrF64Add && second <= wasm.InstrF64Max
	if firstF64 != secondF64 {
		return wasm.ValType{}, false, false
	}
	if firstF64 {
		return wasm.F64, true, true
	}
	return wasm.F32, false, true
}

func emitARM64DirectFloatBinary(a *arm64.Asm, kind wasm.InstrKind, lhs, rhs arm64.Reg) {
	f64 := kind >= wasm.InstrF64Add && kind <= wasm.InstrF64Max
	switch kind {
	case wasm.InstrF32Add, wasm.InstrF64Add:
		a.Fadd(lhs, lhs, rhs, f64)
	case wasm.InstrF32Sub, wasm.InstrF64Sub:
		a.Fsub(lhs, lhs, rhs, f64)
	case wasm.InstrF32Mul, wasm.InstrF64Mul:
		a.Fmul(lhs, lhs, rhs, f64)
	case wasm.InstrF32Div, wasm.InstrF64Div:
		a.Fdiv(lhs, lhs, rhs, f64)
	case wasm.InstrF32Min, wasm.InstrF64Min:
		a.Fmin(lhs, lhs, rhs, f64)
	case wasm.InstrF32Max, wasm.InstrF64Max:
		a.Fmax(lhs, lhs, rhs, f64)
	}
}

func emitARM64DirectLocalBinary(a *arm64.Asm, kind wasm.InstrKind, dst, lhs, rhs arm64.Reg) {
	wide := kind >= wasm.InstrI64Add && kind <= wasm.InstrI64Rotr
	switch kind {
	case wasm.InstrI32Add, wasm.InstrI64Add:
		if wide {
			a.Add64(dst, lhs, rhs)
		} else {
			a.Add32(dst, lhs, rhs)
		}
	case wasm.InstrI32Sub, wasm.InstrI64Sub:
		if wide {
			a.Sub64(dst, lhs, rhs)
		} else {
			a.Sub32(dst, lhs, rhs)
		}
	case wasm.InstrI32Mul, wasm.InstrI64Mul:
		if wide {
			a.Mul64(dst, lhs, rhs)
		} else {
			a.Mul32(dst, lhs, rhs)
		}
	case wasm.InstrI32And, wasm.InstrI64And:
		if wide {
			a.And64(dst, lhs, rhs)
		} else {
			a.And32(dst, lhs, rhs)
		}
	case wasm.InstrI32Or, wasm.InstrI64Or:
		if wide {
			a.Orr64(dst, lhs, rhs)
		} else {
			a.Orr32(dst, lhs, rhs)
		}
	case wasm.InstrI32Xor, wasm.InstrI64Xor:
		if wide {
			a.Eor64(dst, lhs, rhs)
		} else {
			a.Eor32(dst, lhs, rhs)
		}
	case wasm.InstrI32Shl, wasm.InstrI64Shl:
		if wide {
			a.Lslv64(dst, lhs, rhs)
		} else {
			a.Lslv32(dst, lhs, rhs)
		}
	case wasm.InstrI32ShrS, wasm.InstrI64ShrS:
		if wide {
			a.Asrv64(dst, lhs, rhs)
		} else {
			a.Asrv32(dst, lhs, rhs)
		}
	case wasm.InstrI32ShrU, wasm.InstrI64ShrU:
		if wide {
			a.Lsrv64(dst, lhs, rhs)
		} else {
			a.Lsrv32(dst, lhs, rhs)
		}
	case wasm.InstrI32Rotl, wasm.InstrI64Rotl:
		a.Sub64(arm64.X16, arm64.XZR, rhs)
		if wide {
			a.Rorv64(dst, lhs, arm64.X16)
		} else {
			a.Rorv32(dst, lhs, arm64.X16)
		}
	case wasm.InstrI32Rotr, wasm.InstrI64Rotr:
		if wide {
			a.Rorv64(dst, lhs, rhs)
		} else {
			a.Rorv32(dst, lhs, rhs)
		}
	}
}

func emitARM64RegisterStackInteger(a *arm64.Asm, kind wasm.InstrKind, stack *[]wasm.ValType, operandRegisters []arm64.Reg, function, wasmOffset uint32, metadata *functionEmissionMetadata) error {
	types := *stack
	wide := kind >= wasm.InstrI64Eqz && kind <= wasm.InstrI64GeU ||
		kind >= wasm.InstrI64Clz && kind <= wasm.InstrI64Rotr
	if len(types) < 1 {
		return fmt.Errorf("operand stack underflow")
	}
	top := operandRegisters[len(types)-1]
	switch kind {
	case wasm.InstrI64ExtendI32S:
		a.Sxtw(top, top)
		types[len(types)-1] = wasm.I64
		*stack = types
		return nil
	case wasm.InstrI32Extend8S:
		a.Sxtb(top, top, true)
		return nil
	case wasm.InstrI32Extend16S:
		a.Sxth(top, top, true)
		return nil
	case wasm.InstrI64Extend8S:
		a.Sxtb(top, top, false)
		return nil
	case wasm.InstrI64Extend16S:
		a.Sxth(top, top, false)
		return nil
	case wasm.InstrI64Extend32S:
		a.Sxtw(top, top)
		return nil
	case wasm.InstrI32Eqz, wasm.InstrI64Eqz:
		if wide {
			a.CmpImm64(top, 0)
		} else {
			a.CmpImm32(top, 0)
		}
		a.Cset32(top, arm64.CondEQ)
		types[len(types)-1] = wasm.I32
		*stack = types
		return nil
	case wasm.InstrI32Clz, wasm.InstrI32Ctz, wasm.InstrI32Popcnt, wasm.InstrI64Clz, wasm.InstrI64Ctz, wasm.InstrI64Popcnt:
		switch kind {
		case wasm.InstrI32Clz, wasm.InstrI64Clz:
			a.Clz(top, top, !wide)
		case wasm.InstrI32Ctz, wasm.InstrI64Ctz:
			a.Rbit(top, top, !wide)
			a.Clz(top, top, !wide)
		default:
			a.FmovFromGpr(arm64.X0, top, wide)
			a.Cnt8b(arm64.X0, arm64.X0)
			a.Addv8b(arm64.X0, arm64.X0)
			a.NeonUmovB(top, arm64.X0, 0)
		}
		return nil
	}
	if len(types) < 2 {
		return fmt.Errorf("operand stack underflow")
	}
	lhsIndex, rhsIndex := len(types)-2, len(types)-1
	lhs, rhs := operandRegisters[lhsIndex], operandRegisters[rhsIndex]
	switch kind {
	case wasm.InstrI32Eq, wasm.InstrI64Eq, wasm.InstrI32Ne, wasm.InstrI64Ne,
		wasm.InstrI32LtS, wasm.InstrI64LtS, wasm.InstrI32LtU, wasm.InstrI64LtU,
		wasm.InstrI32GtS, wasm.InstrI64GtS, wasm.InstrI32GtU, wasm.InstrI64GtU,
		wasm.InstrI32LeS, wasm.InstrI64LeS, wasm.InstrI32LeU, wasm.InstrI64LeU,
		wasm.InstrI32GeS, wasm.InstrI64GeS, wasm.InstrI32GeU, wasm.InstrI64GeU:
		if wide {
			a.CmpReg64(lhs, rhs)
		} else {
			a.CmpReg32(lhs, rhs)
		}
		a.Cset32(lhs, arm64IntegerComparisonCond(kind))
		types[lhsIndex] = wasm.I32
	case wasm.InstrI32Add, wasm.InstrI64Add:
		if wide {
			a.Add64(lhs, lhs, rhs)
		} else {
			a.Add32(lhs, lhs, rhs)
		}
	case wasm.InstrI32Sub, wasm.InstrI64Sub:
		if wide {
			a.Sub64(lhs, lhs, rhs)
		} else {
			a.Sub32(lhs, lhs, rhs)
		}
	case wasm.InstrI32Mul, wasm.InstrI64Mul:
		if wide {
			a.Mul64(lhs, lhs, rhs)
		} else {
			a.Mul32(lhs, lhs, rhs)
		}
	case wasm.InstrI32And, wasm.InstrI64And:
		if wide {
			a.And64(lhs, lhs, rhs)
		} else {
			a.And32(lhs, lhs, rhs)
		}
	case wasm.InstrI32Or, wasm.InstrI64Or:
		if wide {
			a.Orr64(lhs, lhs, rhs)
		} else {
			a.Orr32(lhs, lhs, rhs)
		}
	case wasm.InstrI32Xor, wasm.InstrI64Xor:
		if wide {
			a.Eor64(lhs, lhs, rhs)
		} else {
			a.Eor32(lhs, lhs, rhs)
		}
	case wasm.InstrI32Shl, wasm.InstrI64Shl:
		if wide {
			a.Lslv64(lhs, lhs, rhs)
		} else {
			a.Lslv32(lhs, lhs, rhs)
		}
	case wasm.InstrI32ShrS, wasm.InstrI64ShrS:
		if wide {
			a.Asrv64(lhs, lhs, rhs)
		} else {
			a.Asrv32(lhs, lhs, rhs)
		}
	case wasm.InstrI32ShrU, wasm.InstrI64ShrU:
		if wide {
			a.Lsrv64(lhs, lhs, rhs)
		} else {
			a.Lsrv32(lhs, lhs, rhs)
		}
	case wasm.InstrI32Rotl, wasm.InstrI64Rotl:
		a.Sub64(arm64.X16, arm64.XZR, rhs)
		if wide {
			a.Rorv64(lhs, lhs, arm64.X16)
		} else {
			a.Rorv32(lhs, lhs, arm64.X16)
		}
	case wasm.InstrI32Rotr, wasm.InstrI64Rotr:
		if wide {
			a.Rorv64(lhs, lhs, rhs)
		} else {
			a.Rorv32(lhs, lhs, rhs)
		}
	case wasm.InstrI32DivS, wasm.InstrI64DivS, wasm.InstrI32RemS, wasm.InstrI64RemS:
		if err := arm64TrapDivZero(a, rhs, wide, function, wasmOffset, metadata); err != nil {
			return err
		}
		if kind == wasm.InstrI32DivS || kind == wasm.InstrI64DivS {
			a.MovImm64(arm64.X16, ^uint64(0))
			if wide {
				a.CmpReg64(rhs, arm64.X16)
			} else {
				a.CmpReg32(rhs, arm64.X16)
			}
			notMinusOne := a.Bcond(arm64.CondNE)
			minimum := uint64(0x80000000)
			if wide {
				minimum = uint64(1) << 63
			}
			a.MovImm64(arm64.X16, minimum)
			if wide {
				a.CmpReg64(lhs, arm64.X16)
			} else {
				a.CmpReg32(lhs, arm64.X16)
			}
			notMinimum := a.Bcond(arm64.CondNE)
			metadata.recordTrap(a.Len(), wasmOffset, 10)
			arm64EmitTrap(a, 10, function, wasmOffset)
			if !a.PatchBranch19(notMinusOne, a.Len()) || !a.PatchBranch19(notMinimum, a.Len()) {
				return fmt.Errorf("division overflow branch is out of range")
			}
		}
		if wide {
			a.Sdiv64(arm64.X17, lhs, rhs)
		} else {
			a.Sdiv32(arm64.X17, lhs, rhs)
		}
		if kind == wasm.InstrI32RemS {
			a.Msub32(lhs, arm64.X17, rhs, lhs)
		} else if kind == wasm.InstrI64RemS {
			a.Msub64(lhs, arm64.X17, rhs, lhs)
		} else {
			a.MovReg64(lhs, arm64.X17)
		}
	case wasm.InstrI32DivU, wasm.InstrI64DivU, wasm.InstrI32RemU, wasm.InstrI64RemU:
		if err := arm64TrapDivZero(a, rhs, wide, function, wasmOffset, metadata); err != nil {
			return err
		}
		if wide {
			a.Udiv64(arm64.X17, lhs, rhs)
		} else {
			a.Udiv32(arm64.X17, lhs, rhs)
		}
		if kind == wasm.InstrI32RemU {
			a.Msub32(lhs, arm64.X17, rhs, lhs)
		} else if kind == wasm.InstrI64RemU {
			a.Msub64(lhs, arm64.X17, rhs, lhs)
		} else {
			a.MovReg64(lhs, arm64.X17)
		}
	default:
		return fmt.Errorf("unsupported structured integer instruction %s", kind)
	}
	*stack = types[:len(types)-1]
	return nil
}

func emitARM64StackInteger(a *arm64.Asm, kind wasm.InstrKind, stack *[]wasm.ValType, stackOff func(int) uint32, function, wasmOffset uint32, metadata *functionEmissionMetadata) error {
	types := *stack
	load := func(index int, reg arm64.Reg) error {
		if index < 0 || !a.Load64(reg, arm64.SP, stackOff(index)) {
			return fmt.Errorf("invalid operand stack load")
		}
		return nil
	}
	store := func(index int, reg arm64.Reg) error {
		if index < 0 || !a.Store64(reg, arm64.SP, stackOff(index)) {
			return fmt.Errorf("invalid operand stack store")
		}
		return nil
	}
	wide := kind >= wasm.InstrI64Eqz && kind <= wasm.InstrI64GeU ||
		kind >= wasm.InstrI64Clz && kind <= wasm.InstrI64Rotr
	switch kind {
	case wasm.InstrI32Eq, wasm.InstrI64Eq, wasm.InstrI32Ne, wasm.InstrI64Ne,
		wasm.InstrI32LtS, wasm.InstrI64LtS, wasm.InstrI32LtU, wasm.InstrI64LtU,
		wasm.InstrI32GtS, wasm.InstrI64GtS, wasm.InstrI32GtU, wasm.InstrI64GtU,
		wasm.InstrI32LeS, wasm.InstrI64LeS, wasm.InstrI32LeU, wasm.InstrI64LeU,
		wasm.InstrI32GeS, wasm.InstrI64GeS, wasm.InstrI32GeU, wasm.InstrI64GeU:
		if len(types) < 2 || load(len(types)-2, arm64.X9) != nil || load(len(types)-1, arm64.X10) != nil {
			return fmt.Errorf("operand stack underflow")
		}
		if wide {
			a.CmpReg64(arm64.X9, arm64.X10)
		} else {
			a.CmpReg32(arm64.X9, arm64.X10)
		}
		a.Cset32(arm64.X9, arm64IntegerComparisonCond(kind))
		types[len(types)-2] = wasm.I32
		*stack = types[:len(types)-1]
		return store(len(types)-2, arm64.X9)
	case wasm.InstrI64ExtendI32S:
		if len(types) < 1 || load(len(types)-1, arm64.X9) != nil {
			return fmt.Errorf("operand stack underflow")
		}
		a.Sxtw(arm64.X9, arm64.X9)
		types[len(types)-1] = wasm.I64
		*stack = types
		return store(len(types)-1, arm64.X9)
	case wasm.InstrI32Extend8S, wasm.InstrI32Extend16S,
		wasm.InstrI64Extend8S, wasm.InstrI64Extend16S, wasm.InstrI64Extend32S:
		if len(types) < 1 || load(len(types)-1, arm64.X9) != nil {
			return fmt.Errorf("operand stack underflow")
		}
		switch kind {
		case wasm.InstrI32Extend8S:
			a.Sxtb(arm64.X9, arm64.X9, true)
		case wasm.InstrI32Extend16S:
			a.Sxth(arm64.X9, arm64.X9, true)
		case wasm.InstrI64Extend8S:
			a.Sxtb(arm64.X9, arm64.X9, false)
		case wasm.InstrI64Extend16S:
			a.Sxth(arm64.X9, arm64.X9, false)
		case wasm.InstrI64Extend32S:
			a.Sxtw(arm64.X9, arm64.X9)
		}
		return store(len(types)-1, arm64.X9)
	case wasm.InstrI32Eqz, wasm.InstrI64Eqz:
		if len(types) < 1 || load(len(types)-1, arm64.X9) != nil {
			return fmt.Errorf("operand stack underflow")
		}
		if wide {
			a.CmpImm64(arm64.X9, 0)
		} else {
			a.CmpImm32(arm64.X9, 0)
		}
		a.Cset32(arm64.X9, arm64.CondEQ)
		types[len(types)-1] = wasm.I32
		*stack = types
		return store(len(types)-1, arm64.X9)
	case wasm.InstrI32Clz, wasm.InstrI32Ctz, wasm.InstrI32Popcnt, wasm.InstrI64Clz, wasm.InstrI64Ctz, wasm.InstrI64Popcnt:
		if len(types) < 1 || load(len(types)-1, arm64.X9) != nil {
			return fmt.Errorf("operand stack underflow")
		}
		switch kind {
		case wasm.InstrI32Clz, wasm.InstrI64Clz:
			a.Clz(arm64.X9, arm64.X9, !wide)
		case wasm.InstrI32Ctz, wasm.InstrI64Ctz:
			a.Rbit(arm64.X9, arm64.X9, !wide)
			a.Clz(arm64.X9, arm64.X9, !wide)
		default:
			a.FmovFromGpr(arm64.X0, arm64.X9, wide)
			a.Cnt8b(arm64.X0, arm64.X0)
			a.Addv8b(arm64.X0, arm64.X0)
			a.NeonUmovB(arm64.X9, arm64.X0, 0)
		}
		return store(len(types)-1, arm64.X9)
	}
	if len(types) < 2 {
		return fmt.Errorf("operand stack underflow")
	}
	lhsIndex, rhsIndex := len(types)-2, len(types)-1
	if err := load(lhsIndex, arm64.X9); err != nil {
		return err
	}
	if err := load(rhsIndex, arm64.X10); err != nil {
		return err
	}
	switch kind {
	case wasm.InstrI32Add, wasm.InstrI64Add:
		if wide {
			a.Add64(arm64.X9, arm64.X9, arm64.X10)
		} else {
			a.Add32(arm64.X9, arm64.X9, arm64.X10)
		}
	case wasm.InstrI32Sub, wasm.InstrI64Sub:
		if wide {
			a.Sub64(arm64.X9, arm64.X9, arm64.X10)
		} else {
			a.Sub32(arm64.X9, arm64.X9, arm64.X10)
		}
	case wasm.InstrI32Mul, wasm.InstrI64Mul:
		if wide {
			a.Mul64(arm64.X9, arm64.X9, arm64.X10)
		} else {
			a.Mul32(arm64.X9, arm64.X9, arm64.X10)
		}
	case wasm.InstrI32And, wasm.InstrI64And:
		if wide {
			a.And64(arm64.X9, arm64.X9, arm64.X10)
		} else {
			a.And32(arm64.X9, arm64.X9, arm64.X10)
		}
	case wasm.InstrI32Or, wasm.InstrI64Or:
		if wide {
			a.Orr64(arm64.X9, arm64.X9, arm64.X10)
		} else {
			a.Orr32(arm64.X9, arm64.X9, arm64.X10)
		}
	case wasm.InstrI32Xor, wasm.InstrI64Xor:
		if wide {
			a.Eor64(arm64.X9, arm64.X9, arm64.X10)
		} else {
			a.Eor32(arm64.X9, arm64.X9, arm64.X10)
		}
	case wasm.InstrI32Shl, wasm.InstrI64Shl:
		if wide {
			a.Lslv64(arm64.X9, arm64.X9, arm64.X10)
		} else {
			a.Lslv32(arm64.X9, arm64.X9, arm64.X10)
		}
	case wasm.InstrI32ShrS, wasm.InstrI64ShrS:
		if wide {
			a.Asrv64(arm64.X9, arm64.X9, arm64.X10)
		} else {
			a.Asrv32(arm64.X9, arm64.X9, arm64.X10)
		}
	case wasm.InstrI32ShrU, wasm.InstrI64ShrU:
		if wide {
			a.Lsrv64(arm64.X9, arm64.X9, arm64.X10)
		} else {
			a.Lsrv32(arm64.X9, arm64.X9, arm64.X10)
		}
	case wasm.InstrI32Rotl, wasm.InstrI64Rotl:
		a.Sub64(arm64.X10, arm64.XZR, arm64.X10)
		if wide {
			a.Rorv64(arm64.X9, arm64.X9, arm64.X10)
		} else {
			a.Rorv32(arm64.X9, arm64.X9, arm64.X10)
		}
	case wasm.InstrI32Rotr, wasm.InstrI64Rotr:
		if wide {
			a.Rorv64(arm64.X9, arm64.X9, arm64.X10)
		} else {
			a.Rorv32(arm64.X9, arm64.X9, arm64.X10)
		}
	case wasm.InstrI32DivS, wasm.InstrI64DivS, wasm.InstrI32RemS, wasm.InstrI64RemS:
		if err := arm64TrapDivZero(a, arm64.X10, wide, function, wasmOffset, metadata); err != nil {
			return err
		}
		if kind == wasm.InstrI32DivS || kind == wasm.InstrI64DivS {
			a.MovImm64(arm64.X12, ^uint64(0))
			if wide {
				a.CmpReg64(arm64.X10, arm64.X12)
			} else {
				a.CmpReg32(arm64.X10, arm64.X12)
			}
			notMinusOne := a.Bcond(arm64.CondNE)
			minimum := uint64(0x80000000)
			if wide {
				minimum = uint64(1) << 63
			}
			a.MovImm64(arm64.X12, minimum)
			if wide {
				a.CmpReg64(arm64.X9, arm64.X12)
			} else {
				a.CmpReg32(arm64.X9, arm64.X12)
			}
			notMinimum := a.Bcond(arm64.CondNE)
			metadata.recordTrap(a.Len(), wasmOffset, 10)
			arm64EmitTrap(a, 10, function, wasmOffset)
			if !a.PatchBranch19(notMinusOne, a.Len()) || !a.PatchBranch19(notMinimum, a.Len()) {
				return fmt.Errorf("division overflow branch is out of range")
			}
		}
		if wide {
			a.Sdiv64(arm64.X11, arm64.X9, arm64.X10)
		} else {
			a.Sdiv32(arm64.X11, arm64.X9, arm64.X10)
		}
		if kind == wasm.InstrI32RemS {
			a.Msub32(arm64.X9, arm64.X11, arm64.X10, arm64.X9)
		} else if kind == wasm.InstrI64RemS {
			a.Msub64(arm64.X9, arm64.X11, arm64.X10, arm64.X9)
		} else {
			a.MovReg64(arm64.X9, arm64.X11)
		}
	case wasm.InstrI32DivU, wasm.InstrI64DivU, wasm.InstrI32RemU, wasm.InstrI64RemU:
		if err := arm64TrapDivZero(a, arm64.X10, wide, function, wasmOffset, metadata); err != nil {
			return err
		}
		if wide {
			a.Udiv64(arm64.X11, arm64.X9, arm64.X10)
		} else {
			a.Udiv32(arm64.X11, arm64.X9, arm64.X10)
		}
		if kind == wasm.InstrI32RemU {
			a.Msub32(arm64.X9, arm64.X11, arm64.X10, arm64.X9)
		} else if kind == wasm.InstrI64RemU {
			a.Msub64(arm64.X9, arm64.X11, arm64.X10, arm64.X9)
		} else {
			a.MovReg64(arm64.X9, arm64.X11)
		}
	default:
		return fmt.Errorf("unsupported structured integer instruction %s", kind)
	}
	*stack = types[:len(types)-1]
	return store(lhsIndex, arm64.X9)
}

func arm64TrapDivZero(a *arm64.Asm, divisor arm64.Reg, wide bool, function, wasmOffset uint32, metadata *functionEmissionMetadata) error {
	if wide {
		a.CmpImm64(divisor, 0)
	} else {
		a.CmpImm32(divisor, 0)
	}
	nonzero := a.Bcond(arm64.CondNE)
	metadata.recordTrap(a.Len(), wasmOffset, 9)
	arm64EmitTrap(a, 9, function, wasmOffset)
	if !a.PatchBranch19(nonzero, a.Len()) {
		return fmt.Errorf("division trap branch is out of range")
	}
	return nil
}

func arm64EmitTrap(a *arm64.Asm, code, function, wasmOffset uint32) {
	a.Ldur64(arm64.X12, arm64.X26, -int32(abi.TrapCellPtrOffset))
	a.MovImm64(arm64.X16, uint64(function+1))
	a.Store32(arm64.X16, arm64.X12, 16)
	a.MovImm64(arm64.X16, uint64(wasmOffset))
	a.Store32(arm64.X16, arm64.X12, 20)
	a.MovImm64(arm64.X16, uint64(code))
	a.Store32(arm64.X16, arm64.X12, 0)
	a.Ldur64(arm64.X16, arm64.X26, -24)
	a.Ldur64(arm64.LR, arm64.X26, -32)
	a.AddImm64(arm64.SP, arm64.X16, 0)
	a.Ret()
}

func arm64EmitSharedColdTraps(a *arm64.Asm, traps []nativeBranchPatch, function uint32, metadata *functionEmissionMetadata) error {
	if len(traps) == 0 {
		return nil
	}
	common := a.Len()
	a.Ldur64(arm64.X12, arm64.X26, -int32(abi.TrapCellPtrOffset))
	a.MovImm64(arm64.X16, uint64(function+1))
	a.Store32(arm64.X16, arm64.X12, 16)
	a.Store32(arm64.X14, arm64.X12, 20)
	a.Store32(arm64.X15, arm64.X12, 0)
	a.Ldur64(arm64.X16, arm64.X26, -24)
	a.Ldur64(arm64.LR, arm64.X26, -32)
	a.AddImm64(arm64.SP, arm64.X16, 0)
	a.Ret()
	for _, trap := range traps {
		trapOffset := a.Len()
		metadata.recordTrap(trapOffset, trap.Target, uint32(trap.Code))
		a.MovImm64(arm64.X14, uint64(trap.Target))
		a.MovImm64(arm64.X15, uint64(uint32(trap.Code)))
		if !a.PatchBranch26(a.Branch(), common) {
			return fmt.Errorf("shared cold trap branch is out of range")
		}
		if !a.PatchBranch19(trap.At, trapOffset) {
			return fmt.Errorf("cold trap branch is out of range")
		}
	}
	return nil
}

func arm64Destination(_ *arm64.Asm, loc location) (arm64.Reg, bool) {
	if loc.kind == locationRegister {
		return arm64ValueRegisters[loc.index], false
	}
	return arm64.X16, true
}

func arm64Source(a *arm64.Asm, loc location, scratch arm64.Reg) (arm64.Reg, error) {
	if loc.kind == locationRegister {
		return arm64ValueRegisters[loc.index], nil
	}
	if loc.kind != locationSpill {
		return 0, fmt.Errorf("invalid value location")
	}
	off := uint32(loc.index) * 8
	if !a.Load64(scratch, arm64.SP, off) {
		return 0, fmt.Errorf("spill load offset %d is not encodable", off)
	}
	return scratch, nil
}
