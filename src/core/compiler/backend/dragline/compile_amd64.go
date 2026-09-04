//go:build amd64

package dragline

import (
	"encoding/binary"
	"fmt"
	"math"
	"time"

	corecompiler "github.com/wago-org/wago/src/core/compiler"
	"github.com/wago-org/wago/src/core/compiler/backend/dragline/railmach"
	"github.com/wago-org/wago/src/core/compiler/backend/dragline/railssa"
	"github.com/wago-org/wago/src/core/compiler/codegen"
	"github.com/wago-org/wago/src/core/compiler/wasm"
	"github.com/wago-org/wago/src/core/encoder/amd64"
	coreruntime "github.com/wago-org/wago/src/core/runtime"
	"github.com/wago-org/wago/src/core/runtime/abi"
)

var amd64ValueRegisters = [...]amd64.Reg{amd64.RAX, amd64.RCX, amd64.RDX, amd64.R8, amd64.R9}
var amd64RailMachGPRRegisters = [...]amd64.Reg{amd64.RAX, amd64.RCX, amd64.RDX, amd64.R8, amd64.R9, amd64.R13, amd64.R14, amd64.R15, amd64.RBP, amd64.R12}
var amd64FPRRegisters = [...]amd64.Reg{0, 1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11}
var amd64ParamRegisters = [...]amd64.Reg{amd64.RAX, amd64.RCX, amd64.RDX, amd64.R8, amd64.R9, amd64.R10, amd64.R11, amd64.R12}

const amd64RailMachDenseGlobalThreshold = 20

func amd64RailMachCandidate(stack *railssa.StackFunc, moduleHasV128, moduleHasDenseGlobals bool) bool {
	if !railMachCandidate(stack, moduleHasV128) {
		return false
	}
	if moduleHasDenseGlobals && stack.MaxLoopDepth > 1 {
		// Dense mutable-global functions with nested loops still expose
		// incomplete global-backed edge flow. Single-loop and acyclic functions
		// use the verified RailMach path; nested loops retain structured lowering.
		return false
	}
	if len(stack.Instrs) > 512 {
		for _, instruction := range stack.Instrs {
			if instruction.Kind == wasm.InstrMemoryCopy {
				// Large memory.copy control graphs still expose incomplete AMD64
				// loop-edge value flow. Smaller kernels use the verified RailMach
				// parallel-move lowering below.
				return false
			}
		}
	}
	return true
}

var amd64StackLocalRegisters = [...]amd64.Reg{amd64.R12, amd64.R13, amd64.R14, amd64.R15, amd64.R8, amd64.R9}

type amd64CallReloc struct {
	at     int
	target uint32
}

type amd64SIMDConstant struct {
	bytes [16]byte
	reg   amd64.Reg
	uses  uint32
}

type amd64FloatConstantPatch struct {
	at     int
	target int
	bits   uint64
	f64    bool
}

type amd64SIMDConstantPatch struct {
	at     int
	target int
	bytes  [16]byte
}

func compileNative(input corecompiler.Input, m *wasm.Module, metrics *Metrics, functionCache *corecompiler.FunctionArtifactCache) (corecompiler.Output, error) {
	totalStart := time.Time{}
	if metrics != nil {
		totalStart = time.Now()
		defer func() { metrics.TotalNanos = elapsedNanos(totalStart) }()
	}
	if input.Target.GOARCH != "amd64" {
		return corecompiler.Output{}, &UnsupportedError{Reason: fmt.Sprintf("target %s from amd64 compiler build", input.Target.GOARCH)}
	}
	captureGC := moduleNeedsCollectorRootMaps(m)
	selected, err := compactFunctionSelection(input, m)
	if err != nil {
		return corecompiler.Output{}, err
	}
	if input.FunctionWorkers > 1 && metrics == nil && functionCache == nil && !captureGC && selected == nil {
		return compileNativeParallelAMD64(input, m)
	}
	codeCapacity := initialSelectedNativeCodeCapacity(m, selected)
	code := make([]byte, 0, codeCapacity)
	if selected != nil {
		code = append(code, 0xc3) // unreachable placeholder RET
	}
	entries := make([]int, len(m.Code))
	internal := make([]int, len(m.Code))
	var directPrepared []uint64
	var directLeafPrepared []uint64
	var callRelocs []amd64CallReloc
	var gcCallsites []corecompiler.GCFrameCallsite
	var gcRoots []uint32
	var gcSafepoints []corecompiler.GCFrameSafepoint
	var gcSafepointRoots []uint32
	var gcAdapterReturnOffsets []uint32
	var stackScratch railssa.StackFunc
	var emissionPlanner railssa.EmissionPlanner
	var nativePlanner *nativeBackendPlanner
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
				nativePlanner = &nativeBackendPlanner{signalsBounds: input.Bounds == corecompiler.BoundsSignals}
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
			code = append(code, 0x90)
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
				moduleContracts[i] = railmach.ABIContract{Class: railmach.ABIClass(artifact.ABIClass), GPRClobbers: artifact.ClobberGPR, FPRClobbers: artifact.ClobberFPR}
				if !captureGC && amd64DirectPreparedLeafClass(moduleContracts[i].Class) {
					directPrepared = markAMD64DirectPrepared(directPrepared, len(m.Code), i)
					directLeafPrepared = markAMD64DirectPrepared(directLeafPrepared, len(m.Code), i)
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
					callRelocs = append(callRelocs, amd64CallReloc{at: len(code) + int(relocation.Offset), target: relocation.Target})
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
		var nativePlan *nativeBackendPlan
		if amd64RailMachCandidate(fn.Structured, compilationPlan.HasV128, len(m.Globals) >= amd64RailMachDenseGlobalThreshold) {
			if nativePlanner == nil {
				nativePlanner = &nativeBackendPlanner{signalsBounds: input.Bounds == corecompiler.BoundsSignals}
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
		publishedContract := railmach.ABIContract{}
		if nativePlan != nil {
			publishedContract = nativePlan.ABI
			if i < len(seedCandidates) && seedCandidates[i] {
				publishedContract = seedContracts[i]
			}
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
		body, internalOffset, relocs, railMachFinalized, err := emitAMD64(fn, plan, nativePlan, emitMetrics, capture)
		if err != nil {
			return corecompiler.Output{}, functionError(m, i, "emit", err)
		}
		if !railMachFinalized {
			publishedContract = railmach.ABIContract{}
			moduleContracts[i] = railmach.ABIContract{}
		}
		if !captureGC && railMachFinalized && amd64DirectPreparedLeafClass(publishedContract.Class) {
			directPrepared = markAMD64DirectPrepared(directPrepared, len(m.Code), i)
			directLeafPrepared = markAMD64DirectPrepared(directLeafPrepared, len(m.Code), i)
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
				baselineBytes, err := measureAMD64PostRABaseline(fn, nativePlan)
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
			artifact.PrivateEntry = uint32(internalOffset)
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
		delta := internal[reloc.target] - (reloc.at + 4)
		if int(int32(delta)) != delta {
			return corecompiler.Output{}, fmt.Errorf("dragline: call target %d is out of range", reloc.target)
		}
		binary.LittleEndian.PutUint32(code[reloc.at:], uint32(int32(delta)))
	}
	if len(code) == 0 {
		code = []byte{0xc3}
	}
	if metrics != nil {
		metrics.FinalizeNanos = elapsedNanos(finalizeStart)
		metrics.TotalNanos = elapsedNanos(totalStart)
		metrics.NativeBytes = uint64(len(code))
		metrics.observe(sliceBytes(code) + sliceBytes(entries) + sliceBytes(internal) + sliceBytes(callRelocs) + sliceBytes(helperSafepointBases) + sliceBytes(compilationPlan.Order) + sliceBytes(compilationPlan.Component) + sliceBytes(moduleContracts))
	}
	return corecompiler.Output{Code: code, Entry: entries, InternalEntry: internal, DirectPrepared: directPrepared, DirectLeafPrepared: directLeafPrepared, GCCallsites: gcCallsites, GCRoots: gcRoots, GCSafepoints: gcSafepoints, GCSafepointRoots: gcSafepointRoots, GCAdapterReturnOffsets: gcAdapterReturnOffsets}, nil
}

func markAMD64DirectPrepared(bits []uint64, functions, index int) []uint64 {
	if bits == nil {
		bits = make([]uint64, (functions+63)/64)
	}
	bits[index>>6] |= uint64(1) << uint(index&63)
	return bits
}

func amd64DirectPreparedLeafClass(class railmach.ABIClass) bool {
	return class == railmach.ABITinyDirect || class == railmach.ABIPreparedInt
}

type parallelAMD64Result struct {
	body           []byte
	internalOffset int
	relocs         []amd64CallReloc
	directPrepared bool
}

type parallelAMD64Worker struct {
	stack    railssa.StackFunc
	emission railssa.EmissionPlanner
	native   *nativeBackendPlanner
}

func compileNativeParallelAMD64(input corecompiler.Input, m *wasm.Module) (corecompiler.Output, error) {
	compilation := calleeFirstCompilationPlan(m)
	results := make([]parallelAMD64Result, len(m.Code))
	contracts := make([]railmach.ABIContract, len(m.Code))
	host := compilerHostEffectContracts(input.HostEffects)
	workers := make([]parallelAMD64Worker, input.FunctionWorkers)
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
					worker.native = &nativeBackendPlanner{parallelCandidates: true, signalsBounds: input.Bounds == corecompiler.BoundsSignals}
				}
				seedHotRecursiveComponent(input, m, compilation, i, host, contracts, seeds, scores, candidates, refined, attempted, &worker.stack, worker.native)
			}
			fn, err := buildCompilerFunc(m, i, &worker.stack)
			if err != nil {
				return functionError(m, i, "lower", err)
			}
			var nativePlan *nativeBackendPlan
			if amd64RailMachCandidate(fn.Structured, compilation.HasV128, len(m.Globals) >= amd64RailMachDenseGlobalThreshold) {
				if worker.native == nil {
					worker.native = &nativeBackendPlanner{parallelCandidates: true, signalsBounds: input.Bounds == corecompiler.BoundsSignals}
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
			if nativePlan != nil {
				published := nativePlan.ABI
				if i < len(candidates) && candidates[i] {
					published = seeds[i]
				}
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
			body, internalOffset, relocs, railMachFinalized, err := emitAMD64(fn, plan, nativePlan, nil, nil)
			if err != nil {
				return functionError(m, i, "emit", err)
			}
			if !railMachFinalized {
				contracts[i] = railmach.ABIContract{}
			}
			results[i] = parallelAMD64Result{body: body, internalOffset: internalOffset, relocs: relocs, directPrepared: railMachFinalized && amd64DirectPreparedLeafClass(contracts[i].Class)}
		}
		return nil
	})
	if err != nil {
		return corecompiler.Output{}, err
	}
	code := make([]byte, 0, initialNativeCodeCapacity(m))
	entries := make([]int, len(m.Code))
	internal := make([]int, len(m.Code))
	var callRelocs []amd64CallReloc
	var directPrepared []uint64
	var directLeafPrepared []uint64
	for _, i := range compilation.Order {
		for len(code)&15 != 0 {
			code = append(code, 0x90)
		}
		entries[i] = len(code)
		result := &results[i]
		if result.directPrepared {
			directPrepared = markAMD64DirectPrepared(directPrepared, len(m.Code), i)
			directLeafPrepared = markAMD64DirectPrepared(directLeafPrepared, len(m.Code), i)
		}
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
		delta := internal[reloc.target] - (reloc.at + 4)
		if int(int32(delta)) != delta {
			return corecompiler.Output{}, fmt.Errorf("dragline: call target %d is out of range", reloc.target)
		}
		binary.LittleEndian.PutUint32(code[reloc.at:], uint32(int32(delta)))
	}
	if len(code) == 0 {
		code = []byte{0xc3}
	}
	return corecompiler.Output{Code: code, Entry: entries, InternalEntry: internal, DirectPrepared: directPrepared, DirectLeafPrepared: directLeafPrepared}, nil
}

func emitAMD64(fn *railssa.Func, plan *railssa.EmissionPlan, nativePlan *nativeBackendPlan, metrics *FunctionMetrics, metadata *functionEmissionMetadata) ([]byte, int, []amd64CallReloc, bool, error) {
	if nativePlan != nil {
		var relocs []amd64CallReloc
		if code, entry, ok, err := emitAMD64RailMach(fn, nativePlan, &relocs, metrics, metadata); ok || err != nil {
			return code, entry, relocs, ok, err
		}
	}
	if fn.Stack != nil {
		code, entry, relocs, err := emitAMD64Stack(fn, plan, metrics, metadata)
		return code, entry, relocs, false, err
	}
	if len(fn.Params) > len(amd64ParamRegisters) {
		return nil, 0, nil, false, fmt.Errorf("%d parameters exceed the four-register MVP ABI", len(fn.Params))
	}
	allocation, err := allocateLinear(fn, len(amd64ValueRegisters))
	if err != nil {
		return nil, 0, nil, false, err
	}
	if allocation.frameBytes > uint32(^uint32(0)>>1) {
		return nil, 0, nil, false, &ResourceLimitError{Resource: "AMD64 spill frame bytes", Required: uint64(allocation.frameBytes), Limit: uint64(^uint32(0) >> 1)}
	}
	if metrics != nil {
		metrics.FrameBytes = allocation.frameBytes
	}
	var a amd64.Asm
	defer func() {
		metrics.observe(fn.CapacityBytes() + allocation.peakBytes + sliceBytes(allocation.values) + sliceBytes(a.B))
	}()
	a.Push(amd64.RCX)
	for i, typ := range fn.Params {
		if typ == wasm.I32 {
			a.Load32(amd64ParamRegisters[i], amd64.RDI, int32(i*8))
		} else {
			a.Load64(amd64ParamRegisters[i], amd64.RDI, int32(i*8))
		}
	}
	call := a.CallRel32()
	a.Pop(amd64.RCX)
	if len(fn.Results) == 1 {
		if fn.Results[0] == wasm.I32 {
			a.Store32(amd64.RCX, 0, amd64.RAX)
		} else {
			a.Store64(amd64.RCX, 0, amd64.RAX)
		}
	}
	a.Ret()
	a.Align16()
	internalOffset := a.Len()
	a.PatchRel32(call, internalOffset)
	if allocation.frameBytes != 0 {
		a.SubRsp(int32(allocation.frameBytes))
	}
	for id := range fn.Values {
		value := &fn.Values[id]
		if value.Op == railssa.OpParam {
			continue
		}
		dst, spill := amd64Destination(allocation.values[id])
		switch value.Op {
		case railssa.OpConst:
			if value.Type == wasm.I32 {
				a.MovImm32(dst, int32(value.Aux))
			} else {
				a.MovImm64(dst, value.Aux)
			}
		default:
			args := fn.Operands(railssa.ValueID(id))
			lhs, err := amd64Source(&a, allocation.values[args[0]], amd64.R10)
			if err != nil {
				return nil, 0, nil, false, err
			}
			rhs, err := amd64Source(&a, allocation.values[args[1]], amd64.R11)
			if err != nil {
				return nil, 0, nil, false, err
			}
			wide := value.Type == wasm.I64
			if dst != lhs {
				a.MovReg64(dst, lhs)
			}
			opcode := byte(0)
			switch value.Op {
			case railssa.OpAdd:
				opcode = 0x01
			case railssa.OpSub:
				opcode = 0x29
			case railssa.OpAnd:
				opcode = 0x21
			case railssa.OpOr:
				opcode = 0x09
			case railssa.OpXor:
				opcode = 0x31
			default:
				return nil, 0, nil, false, fmt.Errorf("unsupported SSA op %s", value.Op)
			}
			a.AluRR(opcode, dst, rhs, wide)
		}
		if spill {
			a.StoreRsp64(int32(allocation.values[id].index)*8, dst)
		}
	}
	if len(fn.Results) == 1 {
		result, err := amd64Source(&a, allocation.values[fn.Result], amd64.R10)
		if err != nil {
			return nil, 0, nil, false, err
		}
		if result != amd64.RAX {
			a.MovReg64(amd64.RAX, result)
		}
	}
	if allocation.frameBytes != 0 {
		a.AddRsp(int32(allocation.frameBytes))
	}
	a.Ret()
	return a.B, internalOffset, nil, false, nil
}

func measureAMD64PostRABaseline(fn *railssa.Func, plan *nativeBackendPlan) (int, error) {
	baseline := *plan
	clearPostRAEmissionRewrites(&baseline)
	var relocs []amd64CallReloc
	code, _, ok, err := emitAMD64RailMach(fn, &baseline, &relocs, nil, nil)
	if err != nil {
		return 0, err
	}
	if !ok {
		return 0, fmt.Errorf("RailMach post-RA baseline is unavailable")
	}
	return len(code), nil
}

func amd64RailMachValueDiesAt(plan *nativeBackendPlan, value railmach.VReg, position uint32) bool {
	if plan == nil || plan.Allocation == nil {
		return false
	}
	for _, interval := range plan.Allocation.Intervals {
		if interval.Reg == value {
			return interval.End <= position
		}
	}
	return false
}

func emitAMD64RailMach(fn *railssa.Func, plan *nativeBackendPlan, relocs *[]amd64CallReloc, metrics *FunctionMetrics, metadata *functionEmissionMetadata) ([]byte, int, bool, error) {
	if plan == nil || plan.Stack == nil || plan.CFG == nil || plan.Semantic == nil || plan.Machine == nil || plan.Allocation == nil || plan.Schedule == nil || plan.Exit == nil {
		return nil, 0, false, nil
	}
	for _, interval := range plan.Allocation.Intervals {
		location := plan.Allocation.Locations[interval.Reg]
		if interval.Bank != railmach.BankGPR && interval.Bank != railmach.BankFPR ||
			location.Kind == railmach.LocationRegister && (interval.Bank == railmach.BankGPR && int(location.Index) >= len(amd64RailMachGPRRegisters) || interval.Bank == railmach.BankFPR && int(location.Index) >= len(amd64FPRRegisters)) ||
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
	if !amd64RailMachTargetSafe(plan) {
		return nil, 0, false, nil
	}
	recordNativePlanMetrics(metrics, plan)
	var shrinkGPRs, shrinkFPRs uint64
	for _, region := range plan.CalleeSaves {
		if region.Bank == railmach.BankFPR {
			shrinkFPRs |= uint64(1) << region.Physical
		} else {
			shrinkGPRs |= uint64(1) << region.Physical
		}
	}
	cachedGlobalIndex, cachesGlobal := nativeAMD64CachedGlobal(plan.Machine)
	var currentOperands []railmach.Operand
	var currentResult railmach.VReg
	var currentPosition uint32
	var forwardedSpill railmach.VReg
	reg := func(value railmach.VReg) amd64.Reg {
		location := plan.Allocation.LocationAt(value, currentPosition)
		bank := plan.Machine.VRegs[value].Bank
		cold := false
		for _, operand := range currentOperands {
			cold = cold || operand.Reg == value && operand.Flags&railmach.OperandColdRemat != 0
		}
		if location.Kind == railmach.LocationRegister && !cold {
			if bank == railmach.BankFPR {
				return amd64FPRRegisters[location.Index]
			}
			return amd64RailMachGPRRegisters[location.Index]
		}
		if value == forwardedSpill {
			if bank == railmach.BankFPR {
				return 12
			}
			return amd64.RDI
		}
		if value == currentResult {
			if bank == railmach.BankFPR {
				return 12
			}
			return amd64.RDI
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
			return [...]amd64.Reg{13, 14, 12}[min(ordinal, 2)]
		}
		return [...]amd64.Reg{amd64.RSI, amd64.RDI, amd64.R11}[min(ordinal, 2)]
	}
	immediateProducer, skipInstruction := plan.ImmediateProducer, plan.ImmediateSkip
	if metrics != nil {
		for _, skip := range skipInstruction {
			if skip {
				metrics.ImmediateFolds++
			}
		}
	}
	var a amd64.Asm
	floatConstantPatches := make([]amd64FloatConstantPatch, 0, 4)
	materializeFloatConstant := func(dst amd64.Reg, bits uint64, f64 bool) {
		if bits == 0 {
			a.VPxor(dst, dst, dst)
			return
		}
		floatConstantPatches = append(floatConstantPatches, amd64FloatConstantPatch{at: a.MovsRipPlaceholder(dst, f64), bits: bits, f64: f64})
	}
	readLocation := func(value railmach.VReg, location railmach.Location, scratch amd64.Reg, stackDelta uint32) (amd64.Reg, error) {
		return amd64RailMachReadLocationWithFloatConstant(&a, plan, value, location, scratch, stackDelta, materializeFloatConstant)
	}
	defer func() { metrics.observe(sliceBytes(a.B) + sliceBytes(floatConstantPatches)) }()
	a.Push(amd64.RCX)
	a.MovReg64(amd64.RBX, amd64.RSI)
	for index, typ := range plan.Stack.Params[:min(len(plan.Stack.Params), len(amd64ParamRegisters))] {
		if typ == wasm.I32 || typ == wasm.F32 {
			a.Load32(amd64ParamRegisters[index], amd64.RDI, int32(index)*8)
		} else {
			a.Load64(amd64ParamRegisters[index], amd64.RDI, int32(index)*8)
		}
	}
	if len(plan.Machine.Results) > railmach.PrivateResultRegisters {
		a.LoadRsp64(amd64.R10, 0)
	}
	call := a.CallRel32()
	if metadata != nil {
		metadata.AdapterReturnOffset = uint32(a.Len())
	}
	a.Pop(amd64.RDI)
	for index, result := range plan.Machine.Results[:min(len(plan.Machine.Results), railmach.PrivateResultRegisters)] {
		if plan.Machine.VRegs[result].Type == railmach.TypeI32 {
			a.Store32(amd64.RDI, int32(index*8), amd64RailMachGPRRegisters[index])
		} else {
			a.Store64(amd64.RDI, int32(index*8), amd64RailMachGPRRegisters[index])
		}
	}
	a.Ret()
	a.Align16()
	internalOffset := a.Len()
	directPrepared := amd64DirectPreparedLeafClass(plan.ABI.Class)
	if len(plan.Stack.Instrs) != 0 {
		metadata.recordSource(internalOffset, plan.Stack.Instrs[0].Offset)
	}
	if directPrepared {
		// Preserve a distinct source-map position when parameter locals are
		// already in their private-ABI registers and emit no entry moves.
		a.B = append(a.B, 0x90)
	}
	a.PatchRel32(call, internalOffset)
	if plan.Frame.TotalBytes != 0 {
		if plan.Frame.TotalBytes > uint32(math.MaxInt32) {
			return nil, 0, false, nil
		}
		a.SubRsp(int32(plan.Frame.TotalBytes))
	}
	if len(plan.Machine.Results) > railmach.PrivateResultRegisters {
		a.StoreRsp64(int32(plan.Frame.RuntimeOffset), amd64.R10)
	}
	calleeSaveOffset := plan.Frame.SpillBytes + plan.Frame.RootBytes
	for index := range amd64RailMachGPRRegisters {
		if plan.ABI.CalleeGPRs&^shrinkGPRs&(uint64(1)<<index) != 0 {
			a.StoreRsp64(int32(calleeSaveOffset), amd64RailMachGPRRegisters[index])
			calleeSaveOffset += 8
		}
	}
	for index := range amd64FPRRegisters {
		if plan.ABI.CalleeFPRs&^shrinkFPRs&(uint64(1)<<index) != 0 {
			a.FStoreDisp(amd64.RSP, int32(calleeSaveOffset), amd64FPRRegisters[index], true)
			calleeSaveOffset += 8
		}
	}
	if cachesGlobal {
		a.Load64(amd64.R10, amd64.RBX, -int32(abi.GlobalsPtrOffset))
		a.Load64(amd64RailMachGPRRegisters[nativeAMD64GlobalsRegister], amd64.R10, int32(cachedGlobalIndex)*8)
		a.Load64(amd64RailMachGPRRegisters[nativeAMD64CachedGlobalValueRegister], amd64RailMachGPRRegisters[nativeAMD64GlobalsRegister], 0)
	}
	paramBase := amd64.RDI
	if !directPrepared {
		for value := railmach.VReg(1); int(value) < len(plan.Machine.VRegs); value++ {
			if plan.Machine.VRegs[value].Flags&railmach.VRegInitial != 0 && plan.Allocation.Locations[value].Kind == railmach.LocationSpill {
				a.MovReg64(amd64.R11, amd64.RDI)
				paramBase = amd64.R11
				break
			}
		}
	}
	for local := uint16(0); int(local) < len(plan.Stack.Locals); local++ {
		for value := railmach.VReg(1); int(value) < len(plan.Machine.VRegs); value++ {
			data := plan.Machine.VRegs[value]
			location := plan.Allocation.Locations[value]
			if data.Flags&railmach.VRegInitial == 0 || data.InitialLocal != local || location.Kind != railmach.LocationRegister && location.Kind != railmach.LocationSpill {
				continue
			}
			dst := amd64.RDI
			if data.Bank == railmach.BankFPR {
				dst = 12
			}
			if location.Kind == railmach.LocationRegister {
				dst = reg(value)
			}
			if local < plan.Machine.ParamCount {
				if directPrepared {
					src := amd64ParamRegisters[local]
					if location.Kind == railmach.LocationRegister && dst != src {
						a.MovReg64(dst, src)
					} else if location.Kind == railmach.LocationSpill {
						dst = src
					}
				} else if data.Bank == railmach.BankFPR {
					a.Load64(amd64.R10, paramBase, int32(local)*8)
					a.MovGprToXmm(dst, amd64.R10, data.Type == railmach.TypeF64)
				} else if data.Type == railmach.TypeI32 {
					a.Load32(dst, paramBase, int32(local)*8)
				} else {
					a.Load64(dst, paramBase, int32(local)*8)
				}
			} else if data.Bank == railmach.BankFPR {
				a.XorSelf32(amd64.R10)
				a.MovGprToXmm(dst, amd64.R10, data.Type == railmach.TypeF64)
			} else {
				a.MovImm32(dst, 0)
			}
			if location.Kind == railmach.LocationSpill {
				if err := amd64RailMachStoreValue(&a, plan, value, dst); err != nil {
					return nil, 0, true, err
				}
			}
			break
		}
	}
	if plan.AMD64MemoryBoundEnd != 0 {
		bound := amd64RailMachGPRRegisters[nativeAMD64MemoryBoundRegister]
		a.Load64(bound, amd64.RBX, -int32(abi.ActualLinMemByteSize64Offset))
		a.AluRI(5, bound, int32(plan.AMD64MemoryBoundEnd), true)
	}
	blockOffsets := plan.BlockOffsets
	patches := plan.BranchPatches[:0]
	coldTrapPatches := plan.ColdTrapPatches[:0]
	memoryCheckEnds := plan.MemoryCheckEnds
	memoryCheckTouched := plan.MemoryCheckTouched[:0]
	resetMemoryChecks := func() {
		for _, address := range memoryCheckTouched {
			memoryCheckEnds[address] = 0
		}
		memoryCheckTouched = memoryCheckTouched[:0]
	}
	memoryChecked := func(address railmach.VReg, end uint64) bool {
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
	flushPendingSpill := func() error {
		if pendingSpill == 0 {
			return nil
		}
		value := pendingSpill
		pendingSpill = 0
		return amd64RailMachStoreValue(&a, plan, value, reg(value))
	}
	restoreRegionalVictims := func(nextPosition uint32, forceBlockEnd uint32) error {
		for _, fragment := range plan.Allocation.Fragments {
			if fragment.Victim == 0 || fragment.End+6 != nextPosition && fragment.End/6 != forceBlockEnd {
				continue
			}
			slot := railmach.Location{Kind: railmach.LocationSpill, Bank: fragment.Location.Bank, Index: fragment.VictimSlot}
			if _, err := readLocation(fragment.Victim, slot, amd64RailMachPhysical(fragment.Location), 0); err != nil {
				return err
			}
		}
		return nil
	}
	emitCalleeSaveEntry := func(block railssa.BlockID) {
		for _, region := range plan.CalleeSaves {
			if region.Block != block {
				continue
			}
			if region.Bank == railmach.BankFPR {
				a.FStoreDisp(amd64.RSP, int32(region.SlotOffset), amd64FPRRegisters[region.Physical], true)
			} else {
				a.StoreRsp64(int32(region.SlotOffset), amd64RailMachGPRRegisters[region.Physical])
			}
		}
	}
	emitCalleeRestoreBefore := func(instruction uint32) {
		for _, region := range plan.CalleeSaves {
			if region.RestoreBefore != instruction {
				continue
			}
			if region.Bank == railmach.BankFPR {
				a.FLoadDisp(amd64FPRRegisters[region.Physical], amd64.RSP, int32(region.SlotOffset), true)
			} else {
				a.LoadRsp64(amd64RailMachGPRRegisters[region.Physical], int32(region.SlotOffset))
			}
		}
	}
	preserveSaturatingTruncScratch := func(instruction uint32) (gpr, fpr bool) {
		gpr = railMachPhysicalLiveAcross(plan, instruction, railmach.BankGPR, 0)
		fpr = railMachPhysicalLiveAcross(plan, instruction, railmach.BankFPR, 1)
		if gpr {
			a.Push(amd64.RAX)
		}
		if fpr {
			a.SubRsp(16)
			a.VMovdquStoreDisp(amd64.RSP, 0, 1)
		}
		return gpr, fpr
	}
	restoreSaturatingTruncScratch := func(gpr, fpr bool) {
		if fpr {
			a.VMovdquLoadDisp(1, amd64.RSP, 0)
			a.AddRsp(16)
		}
		if gpr {
			a.Pop(amd64.RAX)
		}
	}
	previousLayoutBlock := -1
	for layoutIndex := range plan.Schedule.BlockRanges {
		blockID := layoutIndex
		if plan.Layout != nil {
			blockID = int(plan.Layout.Order[layoutIndex])
		}
		if !amd64RailMachCarriesMemoryChecks(plan, previousLayoutBlock, blockID) {
			resetMemoryChecks()
		}
		previousLayoutBlock = blockID
		layoutSuccessor := ^uint32(0)
		if layoutIndex+1 < len(plan.Schedule.BlockRanges) {
			layoutSuccessor = uint32(layoutIndex + 1)
			if plan.Layout != nil {
				layoutSuccessor = uint32(plan.Layout.Order[layoutIndex+1])
			}
		}
		branchesToLayoutSuccessor := func(edge uint32) bool {
			return edge < uint32(len(plan.Machine.Edges)) && uint32(plan.Machine.Edges[edge].To) == layoutSuccessor
		}
		edgeNeedsOutgoingMoves := func(edge uint32) bool {
			moveRange := plan.Exit.EdgeMoves[edge]
			for _, move := range plan.Exit.Moves[moveRange.Start : moveRange.Start+moveRange.Count] {
				if move.Placement == railmach.PlacePredecessorEnd || move.Placement == railmach.PlaceSplitEdge {
					return true
				}
			}
			return false
		}
		blockRange := plan.Schedule.BlockRanges[blockID]
		alignBlock := plan.Machine.Blocks[blockID].Flags&uint16(railssa.BlockLoopHeader) != 0
		for edge := range plan.Machine.Edges {
			if alignBlock || uint32(plan.Machine.Edges[edge].From) != uint32(blockID) {
				continue
			}
			_, _, alignBlock = amd64RailMachRotatedZeroTestLatch(plan, uint32(blockID), uint32(edge))
		}
		if alignBlock {
			a.AlignLoop()
		}
		blockOffsets[blockID] = a.Len()
		if plan.Machine.Blocks[blockID].Flags&uint16(railssa.BlockExit) != 0 {
			continue
		}
		if plan.Simplified != nil && blockID < len(plan.Simplified.Reachable) && !plan.Simplified.Reachable[blockID] {
			offset := uint32(0)
			if block := plan.CFG.Blocks[blockID]; block.InstCount != 0 {
				offset = plan.Stack.Instrs[block.InstStart].Offset
			}
			metadata.recordTrap(a.Len(), offset, 1)
			amd64EmitTrap(&a, 1, fn.Index, offset)
			continue
		}
		emitCalleeSaveEntry(railssa.BlockID(blockID))
		if edge, ok := nativeSuccessorEntryEdge(plan, uint32(blockID)); ok {
			if err := emitAMD64RailMachSuccessorMoves(&a, plan, edge); err != nil {
				return nil, 0, true, err
			}
		}
		for _, instructionID := range plan.Schedule.Order[blockRange.Start : blockRange.Start+blockRange.Count] {
			nextPosition := plan.Allocation.InstructionPositions[instructionID]*6 + 2
			forwardedSpill = 0
			if pendingSpill != 0 && plan.Machine.VRegs[pendingSpill].Bank == railmach.BankFPR && !skipInstruction[instructionID] && (len(plan.PostRASkip) == 0 || !plan.PostRASkip[instructionID]) &&
				!nativeControlInstruction(plan.Machine.Insts[instructionID].Op) && amd64RailMachValueDiesAt(plan, pendingSpill, nextPosition) {
				for _, operand := range plan.Machine.InstructionOperands(instructionID) {
					if operand.Reg == pendingSpill && operand.Flags&railmach.OperandColdRemat == 0 {
						forwardedSpill, pendingSpill = pendingSpill, 0
						break
					}
				}
			}
			if err := flushPendingSpill(); err != nil {
				return nil, 0, true, err
			}
			if err := restoreRegionalVictims(nextPosition, ^uint32(0)); err != nil {
				return nil, 0, true, err
			}
			emitCalleeRestoreBefore(instructionID)
			instructionResult := plan.Machine.Insts[instructionID].Result
			if skipInstruction[instructionID] || len(plan.PostRASkip) != 0 && plan.PostRASkip[instructionID] || instructionResult != 0 && plan.Machine.VRegs[instructionResult].Flags&railmach.VRegElided != 0 {
				continue
			}
			instruction := plan.Machine.Insts[instructionID]
			if nativeControlInstruction(instruction.Op) {
				continue
			}
			wasmOffset := railMachWasmOffset(plan, instruction.Source)
			metadata.recordSource(a.Len(), wasmOffset)
			operands := plan.Machine.InstructionOperands(instructionID)
			foldedLoadID, memoryFold := nativeAMD64MemoryFoldSource(plan, instructionID)
			currentOperands, currentResult = operands, instruction.Result
			currentPosition = plan.Allocation.InstructionPositions[instructionID]*6 + 2
			for _, fragment := range plan.Allocation.Fragments {
				if fragment.Start != currentPosition {
					continue
				}
				dst := amd64RailMachPhysical(fragment.Location)
				if fragment.Victim != 0 {
					slot := railmach.Location{Kind: railmach.LocationSpill, Bank: fragment.Location.Bank, Index: fragment.VictimSlot}
					if err := amd64RailMachWriteLocation(&a, plan, fragment.Victim, slot, dst); err != nil {
						return nil, 0, true, err
					}
				}
				if _, err := readLocation(fragment.Reg, plan.Allocation.Locations[fragment.Reg], dst, 0); err != nil {
					return nil, 0, true, err
				}
			}
			if instruction.Op != wasm.InstrCall && instruction.Op != wasm.InstrCallIndirect {
				for operandIndex, operand := range operands {
					if operand.Reg == forwardedSpill || memoryFold && plan.Machine.Insts[foldedLoadID].Result == operand.Reg {
						continue
					}
					duplicate := false
					for _, previous := range operands[:operandIndex] {
						duplicate = duplicate || previous.Reg == operand.Reg
					}
					if duplicate || plan.Allocation.LocationAt(operand.Reg, currentPosition).Kind == railmach.LocationRegister && operand.Flags&railmach.OperandColdRemat == 0 {
						continue
					}
					location := plan.Allocation.LocationAt(operand.Reg, currentPosition)
					if operand.Flags&railmach.OperandColdRemat != 0 {
						location = railmach.Location{Kind: railmach.LocationRematerialize, Bank: operand.Bank}
					}
					if _, err := readLocation(operand.Reg, location, reg(operand.Reg), 0); err != nil {
						return nil, 0, true, err
					}
				}
			}
			_, fusedComparison := nativeAMD64FusionConsumer(plan, instructionID)
			if instruction.Op != wasm.InstrCall && instruction.Op != wasm.InstrCallIndirect && instruction.Result != 0 &&
				plan.Allocation.LocationAt(instruction.Result, currentPosition).Kind == railmach.LocationSpill && !fusedComparison {
				pendingSpill = instruction.Result
			}
			divisionRHSSaved := false
			shiftRCXSaved := false
			shiftRCXRestore := false
			if amd64DirectSafeDivKind(instruction.Op) && len(operands) == 2 {
				lhs := plan.Allocation.LocationAt(operands[0].Reg, currentPosition)
				rhs := plan.Allocation.LocationAt(operands[1].Reg, currentPosition)
				if lhs.Kind == railmach.LocationRegister && lhs.Index != 0 && rhs.Kind == railmach.LocationRegister && rhs.Index == 0 {
					// A fixed lhs repair overwrites RAX. Preserve a divisor that
					// currently occupies RAX before realizing that parallel move.
					a.MovReg64(amd64.R11, amd64.RAX)
					divisionRHSSaved = true
				}
			}
			if (instruction.Op >= wasm.InstrI32Shl && instruction.Op <= wasm.InstrI32Rotr || instruction.Op >= wasm.InstrI64Shl && instruction.Op <= wasm.InstrI64Rotr) && len(operands) == 2 {
				lhs := plan.Allocation.LocationAt(operands[0].Reg, currentPosition)
				result := plan.Allocation.LocationAt(instruction.Result, currentPosition)
				lhsInRCX := operands[0].Reg != operands[1].Reg && lhs.Kind == railmach.LocationRegister && amd64RailMachPhysical(lhs) == amd64.RCX
				resultInRCX := result.Kind == railmach.LocationRegister && amd64RailMachPhysical(result) == amd64.RCX
				liveAcrossRCX := amd64RailMachRegisterLiveAfter(plan, 1, currentPosition, instruction.Result)
				if lhsInRCX || liveAcrossRCX {
					// The variable-count repair writes RCX before the instruction.
					// Preserve an allocated value that the repair must not destroy.
					a.MovReg64(amd64.R11, amd64.RCX)
					shiftRCXSaved = true
					shiftRCXRestore = liveAcrossRCX && !resultInRCX
				}
			}
			if instruction.Op != wasm.InstrCall && instruction.Op != wasm.InstrCallIndirect {
				if moveRange, ok := nativeFixedMoveRange(plan, instructionID); ok {
					if err := emitAMD64RailMachMoveRange(&a, plan, moveRange); err != nil {
						return nil, 0, true, err
					}
				}
			}
			if instruction.Op == wasm.InstrStructNew || instruction.Op == wasm.InstrStructNewDefault || instruction.Op == wasm.InstrArrayNew || instruction.Op == wasm.InstrArrayNewDefault || instruction.Op == wasm.InstrArrayNewFixed || instruction.Op == wasm.InstrArrayNewData || instruction.Op == wasm.InstrArrayNewElem {
				if plan.HelperSafepointBase == 0 {
					return nil, 0, true, fmt.Errorf("RailMach GC helper safepoint base is unavailable")
				}
				if err := emitAMD64RailMachRoots(&a, plan, instruction.Source, currentPosition, false); err != nil {
					return nil, 0, true, err
				}
				emitAMD64ExternalCallFPRSave(&a, plan, false)
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
				a.Load64(amd64.R11, amd64.RBX, -int32(abi.SyncHostCustomContextOffset))
				if deadReservation && instruction.Op == wasm.InstrStructNew {
					a.MovImm64(amd64.R10, uint64(uint32(instruction.Aux)))
					a.Store64(amd64.R11, int32(abi.SyncHostArgsOffset), amd64.R10)
				} else if deadReservation && instruction.Op == wasm.InstrArrayNewFixed {
					a.MovImm64(amd64.R10, uint64(uint32(instruction.Aux)))
					a.Store64(amd64.R11, int32(abi.SyncHostArgsOffset), amd64.R10)
					a.MovImm64(amd64.R10, instruction.Aux>>32)
					a.Store64(amd64.R11, int32(abi.SyncHostArgsOffset+8), amd64.R10)
				} else if instruction.Op == wasm.InstrStructNew || instruction.Op == wasm.InstrArrayNewFixed {
					for index, operand := range operands {
						data := plan.Machine.VRegs[operand.Reg]
						scratch := amd64.R10
						if data.Bank == railmach.BankFPR {
							scratch = 12
						}
						value, err := readLocation(operand.Reg, plan.Allocation.LocationAt(operand.Reg, currentPosition), scratch, 0)
						if err != nil {
							return nil, 0, true, err
						}
						if data.Bank == railmach.BankFPR {
							a.MovXmmToGpr(amd64.R10, value, data.Type == railmach.TypeF64)
							value = amd64.R10
						}
						a.Store64(amd64.R11, int32(abi.SyncHostArgsOffset+index*8), value)
					}
					a.MovImm64(amd64.R10, uint64(uint32(instruction.Aux)))
					a.Store64(amd64.R11, int32(abi.SyncHostArgsOffset+len(operands)*8), amd64.R10)
					if instruction.Op == wasm.InstrArrayNewFixed {
						a.MovImm64(amd64.R10, instruction.Aux>>32)
						a.Store64(amd64.R11, int32(abi.SyncHostArgsOffset+(len(operands)+1)*8), amd64.R10)
					}
				} else if instruction.Op == wasm.InstrArrayNewDefault {
					a.Store64(amd64.R11, int32(abi.SyncHostArgsOffset), reg(operands[0].Reg))
				} else if instruction.Op == wasm.InstrArrayNew {
					value := reg(operands[0].Reg)
					if plan.Machine.VRegs[operands[0].Reg].Bank == railmach.BankFPR {
						a.MovXmmToGpr(amd64.R10, value, plan.Machine.VRegs[operands[0].Reg].Type == railmach.TypeF64)
						value = amd64.R10
					}
					a.Store64(amd64.R11, int32(abi.SyncHostArgsOffset), value)
					a.Store64(amd64.R11, int32(abi.SyncHostArgsOffset+8), reg(operands[1].Reg))
				} else if instruction.Op == wasm.InstrArrayNewData || instruction.Op == wasm.InstrArrayNewElem {
					a.Store64(amd64.R11, int32(abi.SyncHostArgsOffset), reg(operands[0].Reg))
					a.Store64(amd64.R11, int32(abi.SyncHostArgsOffset+8), reg(operands[1].Reg))
					a.MovImm64(amd64.R10, uint64(uint32(instruction.Aux)))
					a.Store64(amd64.R11, int32(abi.SyncHostArgsOffset+16), amd64.R10)
					a.MovImm64(amd64.R10, instruction.Aux>>32)
					a.Store64(amd64.R11, int32(abi.SyncHostArgsOffset+24), amd64.R10)
				} else {
					a.MovImm64(amd64.R10, uint64(uint32(instruction.Aux)))
					a.Store64(amd64.R11, int32(abi.SyncHostArgsOffset), amd64.R10)
				}
				if instruction.Op == wasm.InstrArrayNewDefault {
					a.MovImm64(amd64.R10, instruction.Aux)
					a.Store64(amd64.R11, int32(abi.SyncHostArgsOffset+8), amd64.R10)
				} else if instruction.Op == wasm.InstrArrayNew {
					a.MovImm64(amd64.R10, instruction.Aux)
					a.Store64(amd64.R11, int32(abi.SyncHostArgsOffset+16), amd64.R10)
				}
				a.StoreImm32Mem(amd64.R11, int32(abi.SyncHostImportIndexOffset), int32(codegen.GCHelperDispatchBit|payload))
				a.StoreImm32Mem(amd64.R11, int32(abi.SyncHostArityOffset), int32(arity|resultArity<<16))
				a.CallMem(amd64.R11, int32(abi.SyncHostTrampolineOffset))
				metadata.recordRailMachHelperSafepoint(a.Len(), id, plan, instruction.Source)
				a.Load64(amd64.R11, amd64.RBX, -int32(abi.SyncHostCustomContextOffset))
				if !deadReservation {
					a.Load64(reg(instruction.Result), amd64.R11, int32(abi.SyncHostResultsOffset))
				}
				emitAMD64ExternalCallFPRSave(&a, plan, true)
				if err := emitAMD64RailMachRoots(&a, plan, instruction.Source, currentPosition, true); err != nil {
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
				emitAMD64ExternalCallFPRSave(&a, plan, false)
				a.Load64(amd64.R11, amd64.RBX, -int32(abi.SyncHostCustomContextOffset))
				a.Store64(amd64.R11, int32(abi.SyncHostArgsOffset), reg(operands[0].Reg))
				a.MovImm64(amd64.R10, uint64(uint32(instruction.Aux>>32)))
				a.Store64(amd64.R11, int32(abi.SyncHostArgsOffset+8), amd64.R10)
				a.MovImm64(amd64.R10, uint64(uint32(instruction.Aux)))
				a.Store64(amd64.R11, int32(abi.SyncHostArgsOffset+16), amd64.R10)
				a.StoreImm32Mem(amd64.R11, int32(abi.SyncHostImportIndexOffset), int32(codegen.GCHelperDispatchBit|payload))
				a.StoreImm32Mem(amd64.R11, int32(abi.SyncHostArityOffset), int32(3|1<<16))
				a.CallMem(amd64.R11, int32(abi.SyncHostTrampolineOffset))
				a.Load64(amd64.R11, amd64.RBX, -int32(abi.SyncHostCustomContextOffset))
				dst := reg(instruction.Result)
				if plan.Machine.VRegs[instruction.Result].Bank == railmach.BankFPR {
					a.Load64(amd64.R10, amd64.R11, int32(abi.SyncHostResultsOffset))
					a.MovGprToXmm(dst, amd64.R10, plan.Machine.VRegs[instruction.Result].Type == railmach.TypeF64)
				} else {
					a.Load64(dst, amd64.R11, int32(abi.SyncHostResultsOffset))
				}
				emitAMD64ExternalCallFPRSave(&a, plan, true)
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
				emitAMD64ExternalCallFPRSave(&a, plan, false)
				a.Load64(amd64.R11, amd64.RBX, -int32(abi.SyncHostCustomContextOffset))
				a.Store64(amd64.R11, int32(abi.SyncHostArgsOffset), reg(operands[0].Reg))
				value := reg(operands[1].Reg)
				if plan.Machine.VRegs[operands[1].Reg].Bank == railmach.BankFPR {
					a.MovXmmToGpr(amd64.R10, value, plan.Machine.VRegs[operands[1].Reg].Type == railmach.TypeF64)
					value = amd64.R10
				}
				a.Store64(amd64.R11, int32(abi.SyncHostArgsOffset+8), value)
				a.MovImm64(amd64.R10, uint64(uint32(instruction.Aux>>32)))
				a.Store64(amd64.R11, int32(abi.SyncHostArgsOffset+16), amd64.R10)
				a.MovImm64(amd64.R10, uint64(uint32(instruction.Aux)))
				a.Store64(amd64.R11, int32(abi.SyncHostArgsOffset+24), amd64.R10)
				a.StoreImm32Mem(amd64.R11, int32(abi.SyncHostImportIndexOffset), int32(codegen.GCHelperDispatchBit|payload))
				a.StoreImm32Mem(amd64.R11, int32(abi.SyncHostArityOffset), 4)
				a.CallMem(amd64.R11, int32(abi.SyncHostTrampolineOffset))
				emitAMD64ExternalCallFPRSave(&a, plan, true)
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
				emitAMD64ExternalCallFPRSave(&a, plan, false)
				a.Load64(amd64.R11, amd64.RBX, -int32(abi.SyncHostCustomContextOffset))
				a.Store64(amd64.R11, int32(abi.SyncHostArgsOffset), reg(operands[0].Reg))
				a.MovImm64(amd64.R10, uint64(heap))
				a.Store64(amd64.R11, int32(abi.SyncHostArgsOffset+8), amd64.R10)
				a.StoreImm32Mem(amd64.R11, int32(abi.SyncHostArgsOffset+16), int32(boolUint32(nullable)))
				a.StoreImm32Mem(amd64.R11, int32(abi.SyncHostArgsOffset+24), int32(boolUint32(exact)))
				a.StoreImm32Mem(amd64.R11, int32(abi.SyncHostImportIndexOffset), int32(codegen.GCHelperDispatchBit|payload))
				a.StoreImm32Mem(amd64.R11, int32(abi.SyncHostArityOffset), int32(4|1<<16))
				a.CallMem(amd64.R11, int32(abi.SyncHostTrampolineOffset))
				a.Load64(amd64.R11, amd64.RBX, -int32(abi.SyncHostCustomContextOffset))
				dst := reg(instruction.Result)
				a.Load64(dst, amd64.R11, int32(abi.SyncHostResultsOffset))
				if instruction.Op == wasm.InstrBrOnCast || instruction.Op == wasm.InstrBrOnCastFail {
					a.StoreRsp64(int32(plan.Frame.CallAreaOffset), dst)
				}
				emitAMD64ExternalCallFPRSave(&a, plan, true)
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
				emitAMD64ExternalCallFPRSave(&a, plan, false)
				a.Load64(amd64.R11, amd64.RBX, -int32(abi.SyncHostCustomContextOffset))
				a.Store64(amd64.R11, int32(abi.SyncHostArgsOffset), reg(operands[0].Reg))
				a.StoreImm32Mem(amd64.R11, int32(abi.SyncHostImportIndexOffset), int32(codegen.GCHelperDispatchBit|payload))
				a.StoreImm32Mem(amd64.R11, int32(abi.SyncHostArityOffset), int32(1|1<<16))
				a.CallMem(amd64.R11, int32(abi.SyncHostTrampolineOffset))
				a.Load64(amd64.R11, amd64.RBX, -int32(abi.SyncHostCustomContextOffset))
				a.Load64(reg(instruction.Result), amd64.R11, int32(abi.SyncHostResultsOffset))
				emitAMD64ExternalCallFPRSave(&a, plan, true)
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
				emitAMD64ExternalCallFPRSave(&a, plan, false)
				a.Load64(amd64.R11, amd64.RBX, -int32(abi.SyncHostCustomContextOffset))
				a.Store64(amd64.R11, int32(abi.SyncHostArgsOffset), reg(operands[0].Reg))
				a.Store64(amd64.R11, int32(abi.SyncHostArgsOffset+8), reg(operands[1].Reg))
				a.MovImm64(amd64.R10, uint64(uint32(instruction.Aux)))
				a.Store64(amd64.R11, int32(abi.SyncHostArgsOffset+16), amd64.R10)
				a.StoreImm32Mem(amd64.R11, int32(abi.SyncHostImportIndexOffset), int32(codegen.GCHelperDispatchBit|payload))
				a.StoreImm32Mem(amd64.R11, int32(abi.SyncHostArityOffset), int32(3|1<<16))
				a.CallMem(amd64.R11, int32(abi.SyncHostTrampolineOffset))
				a.Load64(amd64.R11, amd64.RBX, -int32(abi.SyncHostCustomContextOffset))
				dst := reg(instruction.Result)
				if plan.Machine.VRegs[instruction.Result].Bank == railmach.BankFPR {
					a.Load64(amd64.R10, amd64.R11, int32(abi.SyncHostResultsOffset))
					a.MovGprToXmm(dst, amd64.R10, plan.Machine.VRegs[instruction.Result].Type == railmach.TypeF64)
				} else {
					a.Load64(dst, amd64.R11, int32(abi.SyncHostResultsOffset))
				}
				emitAMD64ExternalCallFPRSave(&a, plan, true)
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
				emitAMD64ExternalCallFPRSave(&a, plan, false)
				a.Load64(amd64.R11, amd64.RBX, -int32(abi.SyncHostCustomContextOffset))
				a.Store64(amd64.R11, int32(abi.SyncHostArgsOffset), reg(operands[0].Reg))
				a.Store64(amd64.R11, int32(abi.SyncHostArgsOffset+8), reg(operands[1].Reg))
				value := reg(operands[2].Reg)
				if plan.Machine.VRegs[operands[2].Reg].Bank == railmach.BankFPR {
					a.MovXmmToGpr(amd64.R10, value, plan.Machine.VRegs[operands[2].Reg].Type == railmach.TypeF64)
					value = amd64.R10
				}
				a.Store64(amd64.R11, int32(abi.SyncHostArgsOffset+16), value)
				a.MovImm64(amd64.R10, uint64(uint32(instruction.Aux)))
				a.Store64(amd64.R11, int32(abi.SyncHostArgsOffset+24), amd64.R10)
				a.StoreImm32Mem(amd64.R11, int32(abi.SyncHostImportIndexOffset), int32(codegen.GCHelperDispatchBit|payload))
				a.StoreImm32Mem(amd64.R11, int32(abi.SyncHostArityOffset), 4)
				a.CallMem(amd64.R11, int32(abi.SyncHostTrampolineOffset))
				emitAMD64ExternalCallFPRSave(&a, plan, true)
				continue
			}
			if instruction.Op == wasm.InstrDataDrop {
				offset := uint64(uint32(instruction.Aux))*16 + 8
				if offset > math.MaxInt32 {
					return nil, 0, true, fmt.Errorf("RailMach data.drop descriptor offset is not encodable")
				}
				a.Load64(amd64.R11, amd64.RBX, -int32(abi.PassiveDataPtrOffset))
				a.StoreImm32Mem(amd64.R11, int32(offset), 0)
				continue
			}
			if instruction.Op == wasm.InstrElemDrop {
				payload, ok := codegen.EncodeGCHelperDispatch(codegen.GCHelperArrayDropElem, 0)
				if !ok {
					return nil, 0, true, fmt.Errorf("RailMach GC elem.drop helper is not encodable")
				}
				emitAMD64ExternalCallFPRSave(&a, plan, false)
				a.Load64(amd64.R11, amd64.RBX, -int32(abi.SyncHostCustomContextOffset))
				a.MovImm64(amd64.R10, uint64(uint32(instruction.Aux)))
				a.Store64(amd64.R11, int32(abi.SyncHostArgsOffset), amd64.R10)
				a.StoreImm32Mem(amd64.R11, int32(abi.SyncHostImportIndexOffset), int32(codegen.GCHelperDispatchBit|payload))
				a.StoreImm32Mem(amd64.R11, int32(abi.SyncHostArityOffset), 1)
				a.CallMem(amd64.R11, int32(abi.SyncHostTrampolineOffset))
				emitAMD64ExternalCallFPRSave(&a, plan, true)
				continue
			}
			if instruction.Op == wasm.InstrArrayFill || instruction.Op == wasm.InstrArrayCopy || instruction.Op == wasm.InstrArrayInitData || instruction.Op == wasm.InstrArrayInitElem {
				want := 4
				helper := codegen.GCHelperArrayFill
				arity := uint32(5)
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
				emitAMD64ExternalCallFPRSave(&a, plan, false)
				a.Load64(amd64.R11, amd64.RBX, -int32(abi.SyncHostCustomContextOffset))
				for index, operand := range operands {
					value := reg(operand.Reg)
					if plan.Machine.VRegs[operand.Reg].Bank == railmach.BankFPR {
						a.MovXmmToGpr(amd64.R10, value, plan.Machine.VRegs[operand.Reg].Type == railmach.TypeF64)
						value = amd64.R10
					}
					a.Store64(amd64.R11, int32(abi.SyncHostArgsOffset+index*8), value)
				}
				a.MovImm64(amd64.R10, uint64(uint32(instruction.Aux)))
				a.Store64(amd64.R11, int32(abi.SyncHostArgsOffset+len(operands)*8), amd64.R10)
				if instruction.Op == wasm.InstrArrayCopy || instruction.Op == wasm.InstrArrayInitData || instruction.Op == wasm.InstrArrayInitElem {
					a.MovImm64(amd64.R10, instruction.Aux>>32)
					a.Store64(amd64.R11, int32(abi.SyncHostArgsOffset+(len(operands)+1)*8), amd64.R10)
				}
				a.StoreImm32Mem(amd64.R11, int32(abi.SyncHostImportIndexOffset), int32(codegen.GCHelperDispatchBit|payload))
				a.StoreImm32Mem(amd64.R11, int32(abi.SyncHostArityOffset), int32(arity))
				a.CallMem(amd64.R11, int32(abi.SyncHostTrampolineOffset))
				emitAMD64ExternalCallFPRSave(&a, plan, true)
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
				emitAMD64ExternalCallFPRSave(&a, plan, false)
				a.Load64(amd64.R11, amd64.RBX, -int32(abi.SyncHostCustomContextOffset))
				a.Store64(amd64.R11, int32(abi.SyncHostArgsOffset), reg(operands[0].Reg))
				a.StoreImm32Mem(amd64.R11, int32(abi.SyncHostImportIndexOffset), int32(codegen.GCHelperDispatchBit|payload))
				a.StoreImm32Mem(amd64.R11, int32(abi.SyncHostArityOffset), int32(1|1<<16))
				a.CallMem(amd64.R11, int32(abi.SyncHostTrampolineOffset))
				a.Load64(amd64.R11, amd64.RBX, -int32(abi.SyncHostCustomContextOffset))
				a.Load64(reg(instruction.Result), amd64.R11, int32(abi.SyncHostResultsOffset))
				emitAMD64ExternalCallFPRSave(&a, plan, true)
				continue
			}
			if instruction.Op == wasm.InstrCallIndirect {
				if err := emitAMD64RailMachRoots(&a, plan, instruction.Source, currentPosition, false); err != nil {
					return nil, 0, true, err
				}
				if len(operands) == 0 || int(uint32(instruction.Aux)) >= len(plan.Stack.TypeKeys) {
					return nil, 0, true, fmt.Errorf("RailMach call_indirect metadata is unavailable")
				}
				args := operands[:len(operands)-1]
				callOffset := int32(plan.Frame.CallAreaOffset)
				emitAMD64ExternalCallFPRSave(&a, plan, false)
				for index, operand := range args {
					scratch := amd64.RSI
					if plan.Machine.VRegs[operand.Reg].Bank == railmach.BankFPR {
						scratch = 13
					}
					src, err := amd64RailMachReadValueAt(&a, plan, operand.Reg, scratch, 0)
					if err != nil {
						return nil, 0, true, err
					}
					if plan.Machine.VRegs[operand.Reg].Bank == railmach.BankFPR {
						a.MovXmmToGpr(amd64.R11, src, plan.Machine.VRegs[operand.Reg].Type == railmach.TypeF64)
						src = amd64.R11
					}
					a.StoreRsp64(callOffset+int32(index*8), src)
				}
				selector := operands[len(operands)-1].Reg
				selectorReg, err := amd64RailMachReadValueAt(&a, plan, selector, amd64.RSI, 0)
				if err != nil {
					return nil, 0, true, err
				}
				a.StoreRsp64(callOffset+int32(len(args)*8), selectorReg)
				a.LeaRsp(amd64.RDI, callOffset)
				for index, operand := range args[:min(len(args), len(amd64ParamRegisters))] {
					if plan.Machine.VRegs[operand.Reg].Type == railmach.TypeI32 || plan.Machine.VRegs[operand.Reg].Type == railmach.TypeF32 {
						a.Load32(amd64ParamRegisters[index], amd64.RDI, int32(index*8))
					} else {
						a.Load64(amd64ParamRegisters[index], amd64.RDI, int32(index*8))
					}
				}
				a.Load32(amd64.R10, amd64.RDI, int32(len(args)*8))
				a.Load64(amd64.R11, amd64.RBX, -80)
				a.Load32(amd64.RAX, amd64.R11, 0)
				a.Cmp32(amd64.R10, amd64.RAX)
				inBounds := a.JccPlaceholder(amd64.CondB)
				metadata.recordTrap(a.Len(), wasmOffset, 5)
				amd64EmitTrap(&a, 5, fn.Index, wasmOffset)
				a.PatchRel32(inBounds, a.Len())
				a.ShiftImm(4, amd64.R10, 5, true)
				a.AluRR(0x01, amd64.R11, amd64.R10, true)
				a.Load64(amd64.R10, amd64.R11, 8)
				a.TestSelf(amd64.R10, true)
				nonNull := a.JccPlaceholder(amd64.CondNE)
				metadata.recordTrap(a.Len(), wasmOffset, 5)
				amd64EmitTrap(&a, 5, fn.Index, wasmOffset)
				a.PatchRel32(nonNull, a.Len())
				a.Load64(amd64.RDX, amd64.R11, 16)
				a.MovImm64(amd64.RAX, plan.Stack.TypeKeys[uint32(instruction.Aux)])
				a.Cmp64(amd64.RDX, amd64.RAX)
				sigOK := a.JccPlaceholder(amd64.CondE)
				metadata.recordTrap(a.Len(), wasmOffset, 6)
				amd64EmitTrap(&a, 6, fn.Index, wasmOffset)
				a.PatchRel32(sigOK, a.Len())
				var immutableDone []int
				if targets, ok := nativeDenseLocalTableTargets(plan.Stack.Module); ok {
					// Keep the runtime OOB/null/signature checks, then use the proven
					// dense local table to enter Dragline's private ABI directly.
					a.Load32(amd64.RAX, amd64.RDI, int32(len(args)*8))
					for slot, target := range targets {
						a.AluRI(7, amd64.RAX, int32(slot), false)
						next := a.JccPlaceholder(amd64.CondNE)
						for index, operand := range args[:min(len(args), len(amd64ParamRegisters))] {
							if plan.Machine.VRegs[operand.Reg].Type == railmach.TypeI32 || plan.Machine.VRegs[operand.Reg].Type == railmach.TypeF32 {
								a.Load32(amd64ParamRegisters[index], amd64.RDI, int32(index*8))
							} else {
								a.Load64(amd64ParamRegisters[index], amd64.RDI, int32(index*8))
							}
						}
						if kind, inline := nativeInlineI32BinaryTarget(plan.Stack.Module, target); inline && len(args) == 2 && instruction.ResultCount() == 1 {
							emitAMD64DirectIntegerBinary(&a, kind, amd64.RAX, amd64.RAX, amd64.RCX)
						} else {
							if instruction.ResultCount() > railmach.PrivateResultRegisters {
								a.MovReg64(amd64.R10, amd64.RDI)
							}
							*relocs = append(*relocs, amd64CallReloc{at: a.CallRel32(), target: target - plan.Stack.ImportedFuncs})
							metadata.recordRailMachSafepoint(a.Len(), plan, instruction.Source, 0)
						}
						amd64StagePrivateCallResults(&a, instruction, callOffset)
						immutableDone = append(immutableDone, a.JmpPlaceholder())
						a.PatchRel32(next, a.Len())
					}
					a.Load64(amd64.R10, amd64.R11, 8+coreruntime.TableEntryCodePtrOffset)
				}
				specializedDone := -1
				if target, ok := nativeIndirectTarget(plan, instructionID); ok {
					if metrics != nil {
						metrics.GuardedIndirectCalls++
					}
					// Guard the observed function by canonical funcref identity. This
					// rejects table mutation and cross-instance aliases before using the
					// same-instance private direct-call ABI.
					a.Load64(amd64.RDX, amd64.R11, 8+coreruntime.TableEntryRefSlotOffset)
					a.Load64(amd64.RAX, amd64.RBX, -int32(abi.FuncRefDescPtrOffset))
					a.MovImm64(amd64.R10, uint64(target+1)*coreruntime.FuncRefDescBytes+coreruntime.TableEntryRefSlotOffset)
					a.AluRR(0x01, amd64.RAX, amd64.R10, true)
					a.Load64(amd64.RAX, amd64.RAX, 0)
					a.Cmp64(amd64.RDX, amd64.RAX)
					fallback := a.JccPlaceholder(amd64.CondNE)
					// Descriptor checks consume argument registers on AMD64. Restore the
					// private ABI arguments from the canonical call vector before CALL.
					for index, operand := range args[:min(len(args), len(amd64ParamRegisters))] {
						if plan.Machine.VRegs[operand.Reg].Type == railmach.TypeI32 || plan.Machine.VRegs[operand.Reg].Type == railmach.TypeF32 {
							a.Load32(amd64ParamRegisters[index], amd64.RDI, int32(index*8))
						} else {
							a.Load64(amd64ParamRegisters[index], amd64.RDI, int32(index*8))
						}
					}
					if instruction.ResultCount() > railmach.PrivateResultRegisters {
						a.MovReg64(amd64.R10, amd64.RDI)
					}
					*relocs = append(*relocs, amd64CallReloc{at: a.CallRel32(), target: target - plan.Stack.ImportedFuncs})
					metadata.recordRailMachSafepoint(a.Len(), plan, instruction.Source, 0)
					amd64StagePrivateCallResults(&a, instruction, callOffset)
					specializedDone = a.JmpPlaceholder()
					a.PatchRel32(fallback, a.Len())
					a.Load64(amd64.R10, amd64.R11, 8+coreruntime.TableEntryCodePtrOffset)
				}
				a.MovReg64(amd64.RCX, amd64.RDI)
				a.Load64(amd64.RSI, amd64.R11, 24)
				a.MovImm64(amd64.RAX, ^uint64(abi.FuncRefHomeTagMask))
				a.AluRR(0x21, amd64.RSI, amd64.RAX, true)
				a.Load64(amd64.R8, amd64.R11, 32)
				a.Load64(amd64.R8, amd64.R8, 32)
				a.Load64(amd64.R9, amd64.RBX, -int32(abi.FuncRefDescPtrOffset))
				a.Load64(amd64.R9, amd64.R9, 32)
				// A local funcref wrapper already has the caller's native instance
				// context. Avoid copying that context out and back. Compare canonical
				// context pointers rather than linear-memory homes because distinct
				// instances may share one memory.
				a.Cmp64(amd64.R8, amd64.R9)
				crossInstance := a.JccPlaceholder(amd64.CondNE)
				a.CallReg(amd64.R10)
				metadata.recordRailMachSafepoint(a.Len(), plan, instruction.Source, 0)
				sameInstanceDone := a.JmpPlaceholder()
				a.PatchRel32(crossInstance, a.Len())
				amd64CopyDraglineInstanceContext(&a, amd64.RSI, amd64.R8)
				amd64CopyDraglineExecutionControl(&a, amd64.RSI)
				a.Push(amd64.RBX)
				a.Push(amd64.R9)
				a.Push(amd64.R8)
				a.Push(amd64.RAX)
				a.CallReg(amd64.R10)
				metadata.recordRailMachSafepoint(a.Len(), plan, instruction.Source, 32)
				a.Pop(amd64.RAX)
				a.Pop(amd64.R8)
				a.Pop(amd64.R9)
				a.Pop(amd64.RBX)
				amd64CopyDraglineInstanceContext(&a, amd64.RBX, amd64.R9)
				a.PatchRel32(sameInstanceDone, a.Len())
				for _, done := range immutableDone {
					a.PatchRel32(done, a.Len())
				}
				if specializedDone >= 0 {
					a.PatchRel32(specializedDone, a.Len())
				}
				if err := amd64MaterializeCallResults(&a, plan, instruction, callOffset, currentPosition); err != nil {
					return nil, 0, true, err
				}
				emitAMD64ExternalCallFPRSave(&a, plan, true)
				if err := emitAMD64RailMachRoots(&a, plan, instruction.Source, currentPosition, true); err != nil {
					return nil, 0, true, err
				}
				continue
			}
			if instruction.Op == wasm.InstrCall {
				if err := emitAMD64RailMachRoots(&a, plan, instruction.Source, currentPosition, false); err != nil {
					return nil, 0, true, err
				}
				imported := uint32(instruction.Aux) < plan.Stack.ImportedFuncs
				callOffset := int32(plan.Frame.CallAreaOffset)
				if imported {
					emitAMD64ExternalCallFPRSave(&a, plan, false)
				}
				for index, operand := range operands {
					scratch := amd64.RSI
					if plan.Machine.VRegs[operand.Reg].Bank == railmach.BankFPR {
						scratch = 13
					}
					src, err := amd64RailMachReadValueAt(&a, plan, operand.Reg, scratch, 0)
					if err != nil {
						return nil, 0, true, err
					}
					if plan.Machine.VRegs[operand.Reg].Bank == railmach.BankFPR {
						a.MovXmmToGpr(amd64.R11, src, plan.Machine.VRegs[operand.Reg].Type == railmach.TypeF64)
						src = amd64.R11
					}
					a.StoreRsp64(callOffset+int32(index*8), src)
				}
				a.LeaRsp(amd64.RDI, callOffset)
				for index, operand := range operands[:min(len(operands), len(amd64ParamRegisters))] {
					if plan.Machine.VRegs[operand.Reg].Type == railmach.TypeI32 || plan.Machine.VRegs[operand.Reg].Type == railmach.TypeF32 {
						a.LoadRsp32(amd64ParamRegisters[index], callOffset+int32(index*8))
					} else {
						a.LoadRsp64(amd64ParamRegisters[index], callOffset+int32(index*8))
					}
				}
				if imported {
					a.Load64(amd64.R11, amd64.RBX, -int32(abi.ImportDispatchPtrOffset))
					a.MovImm32(amd64.R10, int32(uint32(instruction.Aux)))
					a.ShiftImm(4, amd64.R10, 5, true)
					a.AluRR(0x01, amd64.R11, amd64.R10, true)
					a.Load64(amd64.R10, amd64.R11, 0)
					a.MovReg64(amd64.RCX, amd64.RDI)
					a.Load64(amd64.RSI, amd64.R11, 8)
					a.Load64(amd64.R8, amd64.R11, 16)
					a.Load64(amd64.R9, amd64.R11, 24)
					amd64CopyDraglineInstanceContext(&a, amd64.RSI, amd64.R8)
					amd64CopyDraglineExecutionControl(&a, amd64.RSI)
					a.Push(amd64.RBX)
					a.Push(amd64.R9)
					a.Push(amd64.R8)
					a.Push(amd64.RAX)
					a.CallReg(amd64.R10)
					metadata.recordRailMachSafepoint(a.Len(), plan, instruction.Source, 32)
					a.Pop(amd64.RAX)
					a.Pop(amd64.R8)
					a.Pop(amd64.R9)
					a.Pop(amd64.RBX)
					amd64CopyDraglineInstanceContext(&a, amd64.RBX, amd64.R9)
				} else {
					if instruction.ResultCount() > railmach.PrivateResultRegisters {
						a.MovReg64(amd64.R10, amd64.RDI)
					}
					*relocs = append(*relocs, amd64CallReloc{at: a.CallRel32(), target: uint32(instruction.Aux) - plan.Stack.ImportedFuncs})
					metadata.recordRailMachSafepoint(a.Len(), plan, instruction.Source, 0)
					if instruction.ResultCount() > 1 {
						amd64StagePrivateCallResults(&a, instruction, callOffset)
					}
				}
				if imported || instruction.ResultCount() > 1 {
					if err := amd64MaterializeCallResults(&a, plan, instruction, callOffset, currentPosition); err != nil {
						return nil, 0, true, err
					}
				} else if instruction.Result != 0 {
					location := plan.Allocation.LocationAt(instruction.Result, currentPosition)
					if location.Kind != railmach.LocationInvalid {
						src := amd64.RAX
						if plan.Machine.VRegs[instruction.Result].Bank == railmach.BankFPR {
							a.MovGprToXmm(13, amd64.RAX, plan.Machine.VRegs[instruction.Result].Type == railmach.TypeF64)
							src = 13
						}
						if err := amd64RailMachWriteLocation(&a, plan, instruction.Result, location, src); err != nil {
							return nil, 0, true, err
						}
					}
				}
				if imported {
					emitAMD64ExternalCallFPRSave(&a, plan, true)
				}
				if err := emitAMD64RailMachRoots(&a, plan, instruction.Source, currentPosition, true); err != nil {
					return nil, 0, true, err
				}
				continue
			}
			if instruction.Op == wasm.InstrMemoryCopy || instruction.Op == wasm.InstrMemoryFill {
				if len(operands) != 3 {
					return nil, 0, true, fmt.Errorf("RailMach %s operand count is %d", instruction.Op, len(operands))
				}
				a.Push(amd64.RAX)
				a.Push(amd64.RCX)
				destination, source, length := reg(operands[0].Reg), reg(operands[1].Reg), reg(operands[2].Reg)
				if source != amd64.RDI && length != amd64.RDI && length != amd64.RSI {
					a.MovReg32(amd64.RDI, destination)
					a.MovReg32(amd64.RSI, source)
					a.MovReg32(amd64.R10, length)
					a.MovReg32(amd64.RAX, amd64.RSI)
					a.MovReg32(amd64.RCX, amd64.R10)
				} else {
					// Materialized spills use RSI/RDI/R11, so assigning RDI first can
					// destroy a later operand. Snapshot conflicting inputs before
					// realizing REP's fixed-register parallel move.
					a.Push(destination)
					a.Push(source)
					a.Push(length)
					a.LoadRsp32(amd64.RDI, 16)
					a.LoadRsp32(amd64.RAX, 8)
					a.LoadRsp32(amd64.RCX, 0)
					a.AddRsp(24)
				}
				emitAMD64BulkMemoryRegisters(&a, instruction.Op, fn.Index, wasmOffset, metadata)
				a.Pop(amd64.RCX)
				a.Pop(amd64.RAX)
				continue
			}
			dst := reg(instruction.Result)
			wide := plan.Machine.VRegs[instruction.Result].Type.IsWideGPR()
			if instruction.Op == wasm.InstrI32Const || instruction.Op == wasm.InstrI64Const || instruction.Op == wasm.InstrRefNull {
				if wide {
					a.MovImm64(dst, instruction.Aux)
				} else {
					a.MovImm32(dst, int32(instruction.Aux))
				}
				continue
			}
			if instruction.Op == wasm.InstrF32Const || instruction.Op == wasm.InstrF64Const {
				f64 := instruction.Op == wasm.InstrF64Const
				if instruction.Aux == 0 {
					a.VPxor(dst, dst, dst)
				} else {
					floatConstantPatches = append(floatConstantPatches, amd64FloatConstantPatch{at: a.MovsRipPlaceholder(dst, f64), bits: instruction.Aux, f64: f64})
				}
				continue
			}
			if instruction.Op == wasm.InstrMemorySize {
				a.Load32(dst, amd64.RBX, -4)
				continue
			}
			if instruction.Op == wasm.InstrGlobalGet {
				if cachesGlobal && uint32(instruction.Aux) == cachedGlobalIndex {
					cached := amd64RailMachGPRRegisters[nativeAMD64CachedGlobalValueRegister]
					if plan.Machine.VRegs[instruction.Result].Bank == railmach.BankFPR {
						a.MovGprToXmm(dst, cached, plan.Machine.VRegs[instruction.Result].Type == railmach.TypeF64)
					} else if plan.Machine.VRegs[instruction.Result].Type == railmach.TypeI32 {
						a.MovReg32(dst, cached)
					} else {
						a.MovReg64(dst, cached)
					}
					continue
				}
				descriptor := amd64.R10
				a.Load64(amd64.R10, amd64.RBX, -int32(abi.GlobalsPtrOffset))
				a.Load64(amd64.R10, amd64.R10, int32(uint32(instruction.Aux))*8)
				if plan.Machine.VRegs[instruction.Result].Bank == railmach.BankFPR {
					a.Load64(amd64.R11, descriptor, 0)
					a.MovGprToXmm(dst, amd64.R11, plan.Machine.VRegs[instruction.Result].Type == railmach.TypeF64)
				} else if plan.Machine.VRegs[instruction.Result].Type == railmach.TypeI32 {
					a.Load32(dst, descriptor, 0)
				} else {
					a.Load64(dst, descriptor, 0)
				}
				continue
			}
			if instruction.Op == wasm.InstrRefFunc {
				a.Load64(dst, amd64.RBX, -int32(abi.FuncRefDescPtrOffset))
				a.TestSelf(dst, true)
				nonNull := a.JccPlaceholder(amd64.CondNE)
				metadata.recordTrap(a.Len(), wasmOffset, 5)
				amd64EmitTrap(&a, 5, fn.Index, wasmOffset)
				a.PatchRel32(nonNull, a.Len())
				offset := (uint64(uint32(instruction.Aux)) + 1) * coreruntime.FuncRefDescBytes
				if offset <= math.MaxInt32 {
					a.LeaDisp(dst, dst, int32(offset))
				} else {
					a.MovImm64(amd64.R10, offset)
					a.AluRR(0x01, dst, amd64.R10, true)
				}
				continue
			}
			lhs := reg(operands[0].Reg)
			if instruction.Op == wasm.InstrRefAsNonNull {
				a.TestSelf(lhs, true)
				nonNull := a.JccPlaceholder(amd64.CondNE)
				metadata.recordTrap(a.Len(), wasmOffset, 16)
				amd64EmitTrap(&a, 16, fn.Index, wasmOffset)
				a.PatchRel32(nonNull, a.Len())
				if dst != lhs {
					a.MovReg64(dst, lhs)
				}
				continue
			}
			if instruction.Op == wasm.InstrRefI31 {
				if dst != lhs {
					a.MovReg64(dst, lhs)
				}
				a.ShiftImm(4, dst, 1, false)
				a.AluRI(1, dst, 1, false)
				continue
			}
			if instruction.Op == wasm.InstrI31GetS || instruction.Op == wasm.InstrI31GetU {
				a.TestSelf(lhs, true)
				nonNull := a.JccPlaceholder(amd64.CondNE)
				metadata.recordTrap(a.Len(), wasmOffset, 16)
				amd64EmitTrap(&a, 16, fn.Index, wasmOffset)
				a.PatchRel32(nonNull, a.Len())
				if dst != lhs {
					a.MovReg64(dst, lhs)
				}
				shift := byte(5)
				if instruction.Op == wasm.InstrI31GetS {
					shift = 7
				}
				a.ShiftImm(shift, dst, 1, false)
				continue
			}
			if shiftRCXSaved && plan.Allocation.LocationAt(operands[0].Reg, currentPosition).Kind == railmach.LocationRegister && amd64RailMachPhysical(plan.Allocation.LocationAt(operands[0].Reg, currentPosition)) == amd64.RCX {
				lhs = amd64.R11
			}
			producer := immediateProducer[instructionID]
			if instruction.Op == wasm.InstrGlobalSet {
				descriptor := amd64.R10
				if cachesGlobal && uint32(instruction.Aux) == cachedGlobalIndex {
					descriptor = amd64RailMachGPRRegisters[nativeAMD64GlobalsRegister]
				} else {
					a.Load64(amd64.R10, amd64.RBX, -int32(abi.GlobalsPtrOffset))
					a.Load64(amd64.R10, amd64.R10, int32(uint32(instruction.Aux))*8)
				}
				src := lhs
				if plan.Machine.VRegs[operands[0].Reg].Bank == railmach.BankFPR {
					a.MovXmmToGpr(amd64.R11, src, plan.Machine.VRegs[operands[0].Reg].Type == railmach.TypeF64)
					src = amd64.R11
				}
				if cachesGlobal && uint32(instruction.Aux) == cachedGlobalIndex {
					a.MovReg64(amd64RailMachGPRRegisters[nativeAMD64CachedGlobalValueRegister], src)
					src = amd64RailMachGPRRegisters[nativeAMD64CachedGlobalValueRegister]
				}
				a.Store64(descriptor, 0, src)
				continue
			}
			if instruction.Op == wasm.InstrMemoryGrow {
				a.Load32(amd64.R10, amd64.RBX, -4)
				a.MovReg32(amd64.R11, amd64.R10)
				a.Add32(amd64.R11, lhs)
				failOverflow := a.JccPlaceholder(amd64.CondB)
				a.Load32(amd64.RSI, amd64.RBX, -12)
				a.Cmp32(amd64.R11, amd64.RSI)
				failMax := a.JccPlaceholder(amd64.CondA)
				a.Store32(amd64.RBX, -4, amd64.R11)
				a.MovReg32(amd64.RSI, amd64.R11)
				a.ShiftImm(4, amd64.RSI, 16, true)
				a.Store64(amd64.RBX, -int32(abi.ActualLinMemByteSize64Offset), amd64.RSI)
				a.Store32(amd64.RBX, -8, amd64.RSI)
				a.MovReg32(dst, amd64.R10)
				done := a.JmpPlaceholder()
				a.PatchRel32(failOverflow, a.Len())
				a.PatchRel32(failMax, a.Len())
				a.MovImm32(dst, -1)
				a.PatchRel32(done, a.Len())
				continue
			}
			if instruction.Op == wasm.InstrSelect {
				rhs := reg(operands[1].Reg)
				condition := reg(operands[2].Reg)
				a.TestSelf(condition, false)
				if plan.Machine.VRegs[instruction.Result].Bank == railmach.BankFPR {
					chooseRHS := a.JccPlaceholder(amd64.CondE)
					a.FMov(dst, lhs, plan.Machine.VRegs[instruction.Result].Type == railmach.TypeF64)
					done := a.JmpPlaceholder()
					a.PatchRel32(chooseRHS, a.Len())
					a.FMov(dst, rhs, plan.Machine.VRegs[instruction.Result].Type == railmach.TypeF64)
					a.PatchRel32(done, a.Len())
				} else {
					out := dst
					if dst == rhs && dst != lhs {
						out = amd64.R10
					}
					if out != lhs {
						a.MovReg64(out, lhs)
					}
					a.Cmovcc(amd64.CondE, out, rhs, plan.Machine.VRegs[instruction.Result].Type.IsWideGPR())
					if out != dst {
						a.MovReg64(dst, out)
					}
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
					src, err := amd64RailMachReadValue(&a, plan, value, dst)
					if err != nil {
						return nil, 0, true, err
					}
					if src != dst {
						if plan.Machine.VRegs[value].Bank == railmach.BankFPR {
							a.FMov(dst, src, plan.Machine.VRegs[value].Type == railmach.TypeF64)
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
				storeSrc := amd64.Reg(0)
				if store {
					value := operands[1].Reg
					storeSrc = reg(value)
					if plan.Machine.VRegs[value].Bank == railmach.BankGPR && storeSrc == amd64.RSI {
						// A store may use the same spilled value as its address and
						// payload. Preserve the payload before the bounds check reuses
						// RSI for the current memory length.
						a.MovReg64(amd64.RDI, storeSrc)
						storeSrc = amd64.RDI
					}
				}
				a.MovReg32(amd64.R10, lhs)
				endOffset := uint64(uint32(instruction.Aux)) + uint64(size)
				if !railMachElidesBoundsCheck(plan, instruction.Source) && !memoryChecked(operands[0].Reg, endOffset) {
					emitAMD64RailMachBoundsCheck(&a, plan, amd64.R10, endOffset, instruction.Source, &coldTrapPatches)
				}
				disp := int32(uint32(instruction.Aux))
				address := amd64.R10
				if uint32(instruction.Aux) > math.MaxInt32 {
					a.MovImm64(amd64.R11, uint64(uint32(instruction.Aux)))
					a.AluRR(0x01, address, amd64.R11, true)
					disp = 0
				}
				if store {
					value := operands[1].Reg
					if plan.Machine.VRegs[value].Bank == railmach.BankFPR {
						a.FStoreIdx(amd64.RBX, address, storeSrc, disp, plan.Machine.VRegs[value].Type == railmach.TypeF64)
					} else {
						a.StoreIdx(amd64.RBX, address, storeSrc, disp, size)
					}
				} else if plan.Machine.VRegs[instruction.Result].Bank == railmach.BankFPR {
					a.FLoadIdx(dst, amd64.RBX, address, disp, plan.Machine.VRegs[instruction.Result].Type == railmach.TypeF64)
				} else {
					a.LoadIdx(dst, amd64.RBX, address, disp, size, signed, plan.Machine.VRegs[instruction.Result].Type == railmach.TypeI64)
				}
				continue
			}
			if instruction.Op >= wasm.InstrF32Eq && instruction.Op <= wasm.InstrF64Ge {
				rhs := reg(operands[1].Reg)
				f64 := instruction.Op >= wasm.InstrF64Eq
				a.Ucomis(lhs, rhs, f64)
				unordered := a.JccPlaceholder(amd64.CondP)
				condition := amd64.CondE
				switch instruction.Op {
				case wasm.InstrF32Ne, wasm.InstrF64Ne:
					condition = amd64.CondNE
				case wasm.InstrF32Lt, wasm.InstrF64Lt:
					condition = amd64.CondB
				case wasm.InstrF32Gt, wasm.InstrF64Gt:
					condition = amd64.CondA
				case wasm.InstrF32Le, wasm.InstrF64Le:
					condition = amd64.CondBE
				case wasm.InstrF32Ge, wasm.InstrF64Ge:
					condition = amd64.CondAE
				}
				a.SetccReg(condition, dst)
				orderedDone := a.JmpPlaceholder()
				a.PatchRel32(unordered, a.Len())
				if instruction.Op == wasm.InstrF32Ne || instruction.Op == wasm.InstrF64Ne {
					a.MovImm32(dst, 1)
				} else {
					a.XorSelf32(dst)
				}
				a.PatchRel32(orderedDone, a.Len())
				continue
			}
			if amd64DirectFloatUnaryKind(instruction.Op) {
				f64 := instruction.Op >= wasm.InstrF64Abs
				if instruction.Op == wasm.InstrF32Abs || instruction.Op == wasm.InstrF64Abs || instruction.Op == wasm.InstrF32Neg || instruction.Op == wasm.InstrF64Neg {
					mask := uint64(0x7fffffff)
					opcode := byte(0x54)
					if f64 {
						mask = 0x7fffffffffffffff
					}
					if instruction.Op == wasm.InstrF32Neg || instruction.Op == wasm.InstrF64Neg {
						opcode = 0x57
						if f64 {
							mask = uint64(1) << 63
						} else {
							mask = uint64(1) << 31
						}
					}
					emitAMD64FloatBits(&a, 15, mask, f64)
					prefix := byte(0)
					if f64 {
						prefix = 1
					}
					a.VSseRRR(prefix, opcode, dst, lhs, 15)
				} else if instruction.Op == wasm.InstrF32Sqrt || instruction.Op == wasm.InstrF64Sqrt {
					// VEX makes dst non-destructive. Using the real input as the merge
					// source avoids both an extra zero idiom and the legacy form's false
					// dependency on dst's prior upper lane.
					a.VFSqrt(dst, lhs, lhs, f64)
				} else {
					emitAMD64DirectFloatUnary(&a, instruction.Op, dst, lhs, f64)
				}
				continue
			}
			if amd64DirectFloatBinaryKind(instruction.Op) {
				if memoryFold {
					if err := emitAMD64FoldedFloatMemory(&a, plan, foldedLoadID, instructionID, lhs, dst, fn.Index, metadata, &coldTrapPatches); err != nil {
						return nil, 0, true, err
					}
					if metrics != nil {
						metrics.PostRARewrites++
						metrics.MemoryFolds++
					}
					continue
				}
				rhs := reg(operands[1].Reg)
				emitAMD64DirectFloatBinary(&a, instruction.Op, dst, lhs, rhs)
				continue
			}
			if instruction.Op == wasm.InstrF32Copysign || instruction.Op == wasm.InstrF64Copysign {
				rhs := reg(operands[1].Reg)
				f64 := instruction.Op == wasm.InstrF64Copysign
				a.MovXmmToGpr(amd64.R10, lhs, f64)
				a.MovXmmToGpr(amd64.R11, rhs, f64)
				a.ShiftImm(4, amd64.R10, 1, f64)
				a.ShiftImm(5, amd64.R10, 1, f64)
				if f64 {
					a.ShiftImm(5, amd64.R11, 63, true)
					a.ShiftImm(4, amd64.R11, 63, true)
				} else {
					a.ShiftImm(5, amd64.R11, 31, false)
					a.ShiftImm(4, amd64.R11, 31, false)
				}
				a.AluRR(0x09, amd64.R10, amd64.R11, f64)
				a.MovGprToXmm(dst, amd64.R10, f64)
				continue
			}
			switch instruction.Op {
			case wasm.InstrI32TruncSatF32S, wasm.InstrI32TruncSatF32U,
				wasm.InstrI32TruncSatF64S, wasm.InstrI32TruncSatF64U:
				f64 := instruction.Op == wasm.InstrI32TruncSatF64S || instruction.Op == wasm.InstrI32TruncSatF64U
				unsigned := instruction.Op == wasm.InstrI32TruncSatF32U || instruction.Op == wasm.InstrI32TruncSatF64U
				preservedGPR, preservedFPR := preserveSaturatingTruncScratch(instructionID)
				a.FMov(15, lhs, f64)
				emitAMD64TruncSatI32(&a, 15, f64, unsigned)
				if dst != amd64.RAX {
					a.MovReg32(dst, amd64.RAX)
				}
				restoreSaturatingTruncScratch(preservedGPR, preservedFPR)
				continue
			case wasm.InstrI64TruncSatF32S, wasm.InstrI64TruncSatF32U,
				wasm.InstrI64TruncSatF64S, wasm.InstrI64TruncSatF64U:
				f64 := instruction.Op == wasm.InstrI64TruncSatF64S || instruction.Op == wasm.InstrI64TruncSatF64U
				unsigned := instruction.Op == wasm.InstrI64TruncSatF32U || instruction.Op == wasm.InstrI64TruncSatF64U
				preservedGPR, preservedFPR := preserveSaturatingTruncScratch(instructionID)
				a.FMov(15, lhs, f64)
				emitAMD64TruncSatI64(&a, 15, f64, unsigned)
				if dst != amd64.RAX {
					a.MovReg64(dst, amd64.RAX)
				}
				restoreSaturatingTruncScratch(preservedGPR, preservedFPR)
				continue
			case wasm.InstrI32TruncF32S, wasm.InstrI32TruncF32U,
				wasm.InstrI32TruncF64S, wasm.InstrI32TruncF64U:
				f64 := instruction.Op == wasm.InstrI32TruncF64S || instruction.Op == wasm.InstrI32TruncF64U
				unsigned := instruction.Op == wasm.InstrI32TruncF32U || instruction.Op == wasm.InstrI32TruncF64U
				a.FMov(15, lhs, f64)
				emitAMD64TruncI32(&a, 15, f64, unsigned, fn.Index, wasmOffset, metadata)
				if dst != amd64.RAX {
					a.MovReg32(dst, amd64.RAX)
				}
				continue
			case wasm.InstrI64TruncF32S, wasm.InstrI64TruncF32U,
				wasm.InstrI64TruncF64S, wasm.InstrI64TruncF64U:
				f64 := instruction.Op == wasm.InstrI64TruncF64S || instruction.Op == wasm.InstrI64TruncF64U
				unsigned := instruction.Op == wasm.InstrI64TruncF32U || instruction.Op == wasm.InstrI64TruncF64U
				a.FMov(15, lhs, f64)
				emitAMD64TruncI64(&a, 15, f64, unsigned, fn.Index, wasmOffset, metadata)
				if dst != amd64.RAX {
					a.MovReg64(dst, amd64.RAX)
				}
				continue
			case wasm.InstrI32ReinterpretF32:
				a.MovXmmToGpr(dst, lhs, false)
				continue
			case wasm.InstrI64ReinterpretF64:
				a.MovXmmToGpr(dst, lhs, true)
				continue
			case wasm.InstrF32ReinterpretI32:
				a.MovGprToXmm(dst, lhs, false)
				continue
			case wasm.InstrF64ReinterpretI64:
				a.MovGprToXmm(dst, lhs, true)
				continue
			case wasm.InstrF32DemoteF64:
				a.Cvtsd2ss(dst, lhs)
				continue
			case wasm.InstrF64PromoteF32:
				a.Cvtss2sd(dst, lhs)
				continue
			case wasm.InstrF32ConvertI32S, wasm.InstrF32ConvertI32U,
				wasm.InstrF64ConvertI32S, wasm.InstrF64ConvertI32U:
				f64 := instruction.Op == wasm.InstrF64ConvertI32S || instruction.Op == wasm.InstrF64ConvertI32U
				unsigned := instruction.Op == wasm.InstrF32ConvertI32U || instruction.Op == wasm.InstrF64ConvertI32U
				a.VPxor(15, 15, 15)
				a.VCvtsi2f(dst, 15, lhs, f64, unsigned)
				continue
			case wasm.InstrF32ConvertI64S, wasm.InstrF32ConvertI64U,
				wasm.InstrF64ConvertI64S, wasm.InstrF64ConvertI64U:
				f64 := instruction.Op == wasm.InstrF64ConvertI64S || instruction.Op == wasm.InstrF64ConvertI64U
				unsigned := instruction.Op == wasm.InstrF32ConvertI64U || instruction.Op == wasm.InstrF64ConvertI64U
				if !unsigned {
					a.VPxor(15, 15, 15)
					a.VCvtsi2f(dst, 15, lhs, f64, true)
					continue
				}
				a.TestSelf(lhs, true)
				large := a.JccPlaceholder(amd64.CondS)
				a.VPxor(15, 15, 15)
				a.VCvtsi2f(dst, 15, lhs, f64, true)
				done := a.JmpPlaceholder()
				a.PatchRel32(large, a.Len())
				a.MovReg64(amd64.R10, lhs)
				a.ShiftImm(5, amd64.R10, 1, true)
				a.MovReg64(amd64.R11, lhs)
				a.AluRI(4, amd64.R11, 1, true)
				a.AluRR(0x09, amd64.R10, amd64.R11, true)
				a.VPxor(15, 15, 15)
				a.VCvtsi2f(dst, 15, amd64.R10, f64, true)
				a.VFAdd(dst, dst, dst, f64)
				a.PatchRel32(done, a.Len())
				continue
			}
			if amd64DirectSafeDivKind(instruction.Op) {
				if !amd64RailMachDivisionSafe(plan, instructionID, operands) {
					return nil, 0, false, nil
				}
				wide := plan.Machine.VRegs[operands[0].Reg].Type == railmach.TypeI64
				if divisionRHSSaved {
					a.MovReg64(amd64.R10, amd64.R11)
				} else {
					a.MovReg64(amd64.R10, reg(operands[1].Reg))
				}
				if nativeObligationRequired(plan, instructionID, railssa.ObligationNonzeroDivisor) {
					amd64TrapDivZero(&a, amd64.R10, wide, fn.Index, wasmOffset, metadata)
				}
				signed := instruction.Op == wasm.InstrI32DivS || instruction.Op == wasm.InstrI64DivS || instruction.Op == wasm.InstrI32RemS || instruction.Op == wasm.InstrI64RemS
				remainder := instruction.Op == wasm.InstrI32RemS || instruction.Op == wasm.InstrI64RemS || instruction.Op == wasm.InstrI32RemU || instruction.Op == wasm.InstrI64RemU
				if signed && nativeDivisorMayBeMinusOne(plan, operands[1].Reg) {
					a.AluRI(7, amd64.R10, -1, wide)
					notMinusOne := a.JccPlaceholder(amd64.CondNE)
					if remainder {
						a.XorSelf32(amd64.RDX)
						done := a.JmpPlaceholder()
						a.PatchRel32(notMinusOne, a.Len())
						a.Cdq(wide)
						a.Idiv(amd64.R10, wide)
						a.PatchRel32(done, a.Len())
					} else {
						a.Neg(amd64.RAX, wide)
						notOverflow := a.JccPlaceholder(amd64.CondNO)
						metadata.recordTrap(a.Len(), wasmOffset, 10)
						amd64EmitTrap(&a, 10, fn.Index, wasmOffset)
						a.PatchRel32(notOverflow, a.Len())
						done := a.JmpPlaceholder()
						a.PatchRel32(notMinusOne, a.Len())
						a.Cdq(wide)
						a.Idiv(amd64.R10, wide)
						a.PatchRel32(done, a.Len())
					}
				} else if signed {
					a.Cdq(wide)
					a.Idiv(amd64.R10, wide)
				} else {
					a.XorSelf32(amd64.RDX)
					a.Div(amd64.R10, wide)
				}
				src := amd64.RAX
				if remainder {
					src = amd64.RDX
				}
				if dst != src {
					a.MovReg64(dst, src)
				}
				continue
			}
			if instruction.Op == wasm.InstrI32Eqz || instruction.Op == wasm.InstrI64Eqz || instruction.Op == wasm.InstrRefIsNull {
				operandWide := plan.Machine.VRegs[operands[0].Reg].Type.IsWideGPR()
				a.TestSelf(lhs, operandWide)
				if fusedComparison {
					if metrics != nil {
						metrics.PostRARewrites++
					}
					continue
				}
				a.SetccReg(amd64.CondE, dst)
				continue
			}
			if instruction.Op == wasm.InstrRefEq {
				rhs := reg(operands[1].Reg)
				a.Cmp64(lhs, rhs)
				a.SetccReg(amd64.CondE, dst)
				continue
			}
			if amd64DirectIntegerUnaryKind(instruction.Op) {
				emitAMD64DirectIntegerUnary(&a, instruction.Op, dst, lhs)
				continue
			}
			switch instruction.Op {
			case wasm.InstrI32WrapI64, wasm.InstrI64ExtendI32U:
				a.MovReg32(dst, lhs)
				continue
			case wasm.InstrI64ExtendI32S:
				a.Movsxd(dst, lhs)
				continue
			case wasm.InstrI32Extend8S, wasm.InstrI64Extend8S:
				a.Movsx8(dst, lhs, instruction.Op == wasm.InstrI64Extend8S)
				continue
			case wasm.InstrI32Extend16S, wasm.InstrI64Extend16S:
				a.Movsx16(dst, lhs, instruction.Op == wasm.InstrI64Extend16S)
				continue
			case wasm.InstrI64Extend32S:
				a.Movsxd(dst, lhs)
				continue
			}
			if condition, comparison := amd64IntegerComparisonCond(instruction.Op); comparison {
				rhs := reg(operands[1].Reg)
				operandWide := plan.Machine.VRegs[operands[0].Reg].Type == railmach.TypeI64
				if operandWide {
					a.Cmp64(lhs, rhs)
				} else {
					a.Cmp32(lhs, rhs)
				}
				if fusedComparison {
					if metrics != nil {
						metrics.PostRARewrites++
					}
					continue
				}
				a.SetccReg(condition, dst)
				continue
			}
			if instruction.Op >= wasm.InstrI32Shl && instruction.Op <= wasm.InstrI32Rotr || instruction.Op >= wasm.InstrI64Shl && instruction.Op <= wasm.InstrI64Rotr {
				out := dst
				if out == amd64.RCX {
					out = amd64.R10
				}
				if out != lhs {
					a.MovReg64(out, lhs)
				}
				digit := byte(4)
				switch instruction.Op {
				case wasm.InstrI32ShrS, wasm.InstrI64ShrS:
					digit = 7
				case wasm.InstrI32ShrU, wasm.InstrI64ShrU:
					digit = 5
				case wasm.InstrI32Rotl, wasm.InstrI64Rotl:
					digit = 0
				case wasm.InstrI32Rotr, wasm.InstrI64Rotr:
					digit = 1
				}
				if producer != ^uint32(0) {
					a.ShiftImm(digit, out, byte(plan.Machine.Insts[producer].Aux), wide)
				} else {
					a.ShiftCL(digit, out, wide)
				}
				if out != dst {
					a.MovReg64(dst, out)
				}
				if shiftRCXRestore {
					a.MovReg64(amd64.RCX, amd64.R11)
				}
				continue
			}
			if memoryFold {
				if err := emitAMD64FoldedIntegerMemory(&a, plan, foldedLoadID, instructionID, lhs, dst, wide, fn.Index, metadata, &coldTrapPatches); err != nil {
					return nil, 0, true, err
				}
				if metrics != nil {
					metrics.PostRARewrites++
					metrics.MemoryFolds++
				}
				continue
			}
			if instruction.Op == wasm.InstrI32Mul || instruction.Op == wasm.InstrI64Mul {
				if producer != ^uint32(0) {
					a.ImulRRI(dst, lhs, int32(plan.Machine.Insts[producer].Aux), wide)
				} else {
					rhs := reg(operands[1].Reg)
					if dst == rhs && dst != lhs {
						// Multiplication is commutative; retain the rhs already in
						// dst and multiply it by lhs.
						a.ImulRR(dst, lhs, wide)
						continue
					}
					if dst != lhs {
						a.MovReg64(dst, lhs)
					}
					a.ImulRR(dst, rhs, wide)
				}
				continue
			}
			opcode := byte(0)
			digit := byte(0)
			switch instruction.Op {
			case wasm.InstrI32Add, wasm.InstrI64Add:
				opcode = 0x01
			case wasm.InstrI32Sub, wasm.InstrI64Sub:
				opcode = 0x29
				digit = 5
			case wasm.InstrI32And, wasm.InstrI64And:
				opcode = 0x21
				digit = 4
			case wasm.InstrI32Or, wasm.InstrI64Or:
				opcode = 0x09
				digit = 1
			case wasm.InstrI32Xor, wasm.InstrI64Xor:
				opcode = 0x31
				digit = 6
			}
			if nativeHasPostRARewrite(plan, instructionID, railmach.RewriteAMD64LEA) && (instruction.Op == wasm.InstrI32Add || instruction.Op == wasm.InstrI64Add || producer != ^uint32(0) && (instruction.Op == wasm.InstrI32Sub || instruction.Op == wasm.InstrI64Sub)) {
				if producer != ^uint32(0) {
					displacement := int32(plan.Machine.Insts[producer].Aux)
					if instruction.Op == wasm.InstrI32Sub || instruction.Op == wasm.InstrI64Sub {
						displacement = -displacement
					}
					a.LeaDispW(dst, lhs, displacement, wide)
				} else {
					a.LeaScaledW(dst, lhs, reg(operands[1].Reg), 0, 0, wide)
				}
				if metrics != nil {
					metrics.PostRARewrites++
				}
				continue
			}
			if producer != ^uint32(0) {
				if dst != lhs {
					a.MovReg64(dst, lhs)
				}
				a.AluRI(digit, dst, int32(plan.Machine.Insts[producer].Aux), wide)
			} else {
				rhs := reg(operands[1].Reg)
				if dst == rhs && dst != lhs {
					if instruction.Op == wasm.InstrI32Sub || instruction.Op == wasm.InstrI64Sub {
						a.MovReg64(amd64.R10, lhs)
						a.AluRR(opcode, amd64.R10, rhs, wide)
						a.MovReg64(dst, amd64.R10)
					} else {
						// The remaining operations are commutative.
						a.AluRR(opcode, dst, lhs, wide)
					}
				} else {
					if dst != lhs {
						a.MovReg64(dst, lhs)
					}
					a.AluRR(opcode, dst, rhs, wide)
				}
			}
		}
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
			amd64EmitTrap(&a, 1, fn.Index, terminator.Offset)
			continue
		}
		if terminator.Kind == wasm.InstrBrTable {
			semanticID := plan.Semantic.InstructionMap[cfgBlock.InstStart+cfgBlock.InstCount-1]
			if semanticID == 0 {
				return nil, 0, true, fmt.Errorf("RailMach br_table block %d has no semantic terminator", blockID)
			}
			selectorValue := plan.Machine.InstructionOperands(semanticID - 1)[0].Reg
			selector, err := amd64RailMachReadValue(&a, plan, selectorValue, amd64.RSI)
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
					a.MovImm32(amd64.R11, int32(caseIndex))
					a.Cmp32(selector, amd64.R11)
					if !edgeNeedsOutgoingMoves(edge) {
						patches = append(patches, nativeBranchPatch{At: a.JccPlaceholder(amd64.CondE), Target: uint32(plan.Machine.Edges[edge].To)})
						continue
					}
					next := a.JccPlaceholder(amd64.CondNE)
					if err := emitAMD64RailMachEdgeMoves(&a, plan, edge); err != nil {
						return nil, 0, true, err
					}
					patches = append(patches, nativeBranchPatch{At: a.JmpPlaceholder(), Target: uint32(plan.Machine.Edges[edge].To)})
					a.PatchRel32(next, a.Len())
				} else {
					if err := emitAMD64RailMachEdgeMoves(&a, plan, edge); err != nil {
						return nil, 0, true, err
					}
					if !branchesToLayoutSuccessor(edge) {
						patches = append(patches, nativeBranchPatch{At: a.JmpPlaceholder(), Target: uint32(plan.Machine.Edges[edge].To)})
					}
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
			falseCondition := amd64.CondE
			if terminator.Kind == wasm.InstrBrOnCastFail {
				falseCondition = amd64.CondNE
			}
			if terminator.Kind == wasm.InstrBrOnCast || terminator.Kind == wasm.InstrBrOnCastFail {
				a.LoadRsp64(amd64.R11, int32(plan.Frame.CallAreaOffset))
				a.TestSelf(amd64.R11, false)
			} else if producerID, fused := nativeAMD64FusionProducer(plan, consumerID); fused {
				condition, ok := amd64FusedComparisonCond(plan.Machine.Insts[producerID].Op)
				if !ok {
					return nil, 0, true, fmt.Errorf("RailMach conditional block %d has invalid fused producer %d", blockID, producerID)
				}
				falseCondition = condition ^ 1
			} else {
				conditionValue := plan.Machine.InstructionOperands(consumerID)[0].Reg
				condition, err := amd64RailMachReadValue(&a, plan, conditionValue, amd64.RSI)
				if err != nil {
					return nil, 0, true, err
				}
				a.TestSelf(condition, false)
			}
			trueMoves := edgeNeedsOutgoingMoves(trueEdge)
			falseMoves := edgeNeedsOutgoingMoves(falseEdge)
			if !falseMoves {
				// Branch directly to a move-free false successor. The true edge
				// either falls through in layout order or retains only its own
				// necessary edge moves and jump.
				patches = append(patches, nativeBranchPatch{At: a.JccPlaceholder(falseCondition), Target: uint32(plan.Machine.Edges[falseEdge].To)})
				if trueMoves {
					if err := emitAMD64RailMachEdgeMoves(&a, plan, trueEdge); err != nil {
						return nil, 0, true, err
					}
				}
				if !branchesToLayoutSuccessor(trueEdge) {
					patches = append(patches, nativeBranchPatch{At: a.JmpPlaceholder(), Target: uint32(plan.Machine.Edges[trueEdge].To)})
				}
				continue
			}
			if !trueMoves {
				// Invert the test so a move-free true successor can be targeted
				// directly while the false edge realizes its moves locally.
				patches = append(patches, nativeBranchPatch{At: a.JccPlaceholder(falseCondition ^ 1), Target: uint32(plan.Machine.Edges[trueEdge].To)})
				if err := emitAMD64RailMachEdgeMoves(&a, plan, falseEdge); err != nil {
					return nil, 0, true, err
				}
				if !branchesToLayoutSuccessor(falseEdge) {
					patches = append(patches, nativeBranchPatch{At: a.JmpPlaceholder(), Target: uint32(plan.Machine.Edges[falseEdge].To)})
				}
				continue
			}
			falseSite := a.JccPlaceholder(falseCondition)
			if err := emitAMD64RailMachEdgeMoves(&a, plan, trueEdge); err != nil {
				return nil, 0, true, err
			}
			patches = append(patches, nativeBranchPatch{At: a.JmpPlaceholder(), Target: uint32(plan.Machine.Edges[trueEdge].To)})
			a.PatchRel32(falseSite, a.Len())
			if err := emitAMD64RailMachEdgeMoves(&a, plan, falseEdge); err != nil {
				return nil, 0, true, err
			}
			if !branchesToLayoutSuccessor(falseEdge) {
				patches = append(patches, nativeBranchPatch{At: a.JmpPlaceholder(), Target: uint32(plan.Machine.Edges[falseEdge].To)})
			}
			continue
		}
		if edgeCount == 1 {
			if counter, exit, rotated := amd64RailMachRotatedZeroTestLatch(plan, uint32(blockID), first); rotated {
				if err := emitAMD64RailMachEdgeMoves(&a, plan, first); err != nil {
					return nil, 0, true, err
				}
				counterLocation := plan.Allocation.Locations[counter]
				a.TestSelf(amd64RailMachPhysical(counterLocation), false)
				patches = append(patches, nativeBranchPatch{At: a.JccPlaceholder(amd64.CondNE), Target: uint32(blockID)})
				if exit != layoutSuccessor {
					patches = append(patches, nativeBranchPatch{At: a.JmpPlaceholder(), Target: exit})
				}
				continue
			}
			if err := emitAMD64RailMachEdgeMoves(&a, plan, first); err != nil {
				return nil, 0, true, err
			}
			if !branchesToLayoutSuccessor(first) {
				patches = append(patches, nativeBranchPatch{At: a.JmpPlaceholder(), Target: uint32(plan.Machine.Edges[first].To)})
			}
		} else if edgeCount != 0 {
			return nil, 0, true, fmt.Errorf("RailMach block %d has unsupported %d-way control", blockID, edgeCount)
		}
	}
	plan.BranchPatches = patches
	resetMemoryChecks()
	plan.MemoryCheckEnds = memoryCheckEnds
	plan.MemoryCheckTouched = memoryCheckTouched
	for _, patch := range patches {
		if int(patch.Target) >= len(blockOffsets) {
			return nil, 0, true, fmt.Errorf("RailMach branch target %d is unavailable", patch.Target)
		}
		a.PatchRel32(patch.At, blockOffsets[patch.Target])
	}
	if len(plan.Machine.Results) == 1 {
		value := plan.Machine.Results[0]
		scratch := amd64.RDI
		if plan.Machine.VRegs[value].Bank == railmach.BankFPR {
			scratch = 12
		}
		result, err := amd64RailMachReadValue(&a, plan, value, scratch)
		if err != nil {
			return nil, 0, true, err
		}
		if plan.Machine.VRegs[value].Bank == railmach.BankFPR {
			a.MovXmmToGpr(amd64.RAX, result, plan.Machine.VRegs[value].Type == railmach.TypeF64)
		} else if result != amd64.RAX {
			a.MovReg64(amd64.RAX, result)
		}
	} else if len(plan.Machine.Results) > 1 {
		for index, value := range plan.Machine.Results {
			scratch := amd64.R11
			if plan.Machine.VRegs[value].Bank == railmach.BankFPR {
				scratch = 13
			}
			result, err := amd64RailMachReadValue(&a, plan, value, scratch)
			if err != nil {
				return nil, 0, true, err
			}
			if plan.Machine.VRegs[value].Bank == railmach.BankFPR {
				a.MovXmmToGpr(amd64.R11, result, plan.Machine.VRegs[value].Type == railmach.TypeF64)
				result = amd64.R11
			}
			a.StoreRsp64(int32(plan.Frame.ResultAreaOffset)+int32(index*8), result)
		}
	}
	calleeSaveOffset = plan.Frame.SpillBytes + plan.Frame.RootBytes
	for index := range amd64RailMachGPRRegisters {
		if plan.ABI.CalleeGPRs&^shrinkGPRs&(uint64(1)<<index) != 0 {
			a.LoadRsp64(amd64RailMachGPRRegisters[index], int32(calleeSaveOffset))
			calleeSaveOffset += 8
		}
	}
	for index := range amd64FPRRegisters {
		if plan.ABI.CalleeFPRs&^shrinkFPRs&(uint64(1)<<index) != 0 {
			a.FLoadDisp(amd64FPRRegisters[index], amd64.RSP, int32(calleeSaveOffset), true)
			calleeSaveOffset += 8
		}
	}
	if len(plan.Machine.Results) > railmach.PrivateResultRegisters {
		a.LoadRsp64(amd64.R10, int32(plan.Frame.RuntimeOffset))
		for index := railmach.PrivateResultRegisters; index < len(plan.Machine.Results); index++ {
			a.LoadRsp64(amd64.R11, int32(plan.Frame.ResultAreaOffset)+int32(index*8))
			a.Store64(amd64.R10, int32(index*8), amd64.R11)
		}
	}
	if len(plan.Machine.Results) > 1 {
		for index, value := range plan.Machine.Results[:min(len(plan.Machine.Results), railmach.PrivateResultRegisters)] {
			offset := int32(plan.Frame.ResultAreaOffset) + int32(index*8)
			if plan.Machine.VRegs[value].Type == railmach.TypeI32 {
				a.LoadRsp32(amd64RailMachGPRRegisters[index], offset)
			} else {
				a.LoadRsp64(amd64RailMachGPRRegisters[index], offset)
			}
		}
	}
	if plan.Frame.TotalBytes != 0 {
		a.AddRsp(int32(plan.Frame.TotalBytes))
	}
	a.Ret()
	for _, trap := range coldTrapPatches {
		a.PatchRel32(trap.At, a.Len())
		metadata.recordTrap(a.Len(), trap.Target, uint32(trap.Code))
		amd64EmitTrap(&a, uint32(trap.Code), fn.Index, trap.Target)
	}
	for index := range floatConstantPatches {
		constant := &floatConstantPatches[index]
		target := -1
		for previous := range floatConstantPatches[:index] {
			if floatConstantPatches[previous].bits == constant.bits && floatConstantPatches[previous].f64 == constant.f64 {
				target = floatConstantPatches[previous].target
				break
			}
		}
		if target >= 0 {
			constant.target = target
			a.PatchRel32(constant.at, target)
			continue
		}
		target = a.Len()
		if constant.f64 {
			var encoded [8]byte
			binary.LittleEndian.PutUint64(encoded[:], constant.bits)
			a.EmitBytes(encoded[:])
		} else {
			var encoded [4]byte
			binary.LittleEndian.PutUint32(encoded[:], uint32(constant.bits))
			a.EmitBytes(encoded[:])
		}
		constant.target = target
		a.PatchRel32(constant.at, target)
	}
	plan.ColdTrapPatches = coldTrapPatches
	return a.B, internalOffset, true, nil
}

// amd64RailMachCarriesMemoryChecks permits exact SSA-address bounds facts to
// cross a laid-out block boundary only when the current block's sole entry is
// the block emitted immediately before it. Linear memory cannot shrink, so a
// successful check remains valid along that straight-line edge.
func amd64RailMachCarriesMemoryChecks(plan *nativeBackendPlan, previous, current int) bool {
	if plan == nil || plan.CFG == nil || previous < 0 || current < 0 || current >= len(plan.CFG.Blocks) {
		return false
	}
	block := plan.CFG.Blocks[current]
	return block.PredCount == 1 && int(block.PredStart) < len(plan.CFG.Preds) && int(plan.CFG.Preds[block.PredStart]) == previous
}

func amd64IntegerComparisonCond(kind wasm.InstrKind) (amd64.Cond, bool) {
	switch kind {
	case wasm.InstrI32Eq, wasm.InstrI64Eq:
		return amd64.CondE, true
	case wasm.InstrI32Ne, wasm.InstrI64Ne:
		return amd64.CondNE, true
	case wasm.InstrI32LtS, wasm.InstrI64LtS:
		return amd64.CondL, true
	case wasm.InstrI32LtU, wasm.InstrI64LtU:
		return amd64.CondB, true
	case wasm.InstrI32GtS, wasm.InstrI64GtS:
		return amd64.CondG, true
	case wasm.InstrI32GtU, wasm.InstrI64GtU:
		return amd64.CondA, true
	case wasm.InstrI32LeS, wasm.InstrI64LeS:
		return amd64.CondLE, true
	case wasm.InstrI32LeU, wasm.InstrI64LeU:
		return amd64.CondBE, true
	case wasm.InstrI32GeS, wasm.InstrI64GeS:
		return amd64.CondGE, true
	case wasm.InstrI32GeU, wasm.InstrI64GeU:
		return amd64.CondAE, true
	default:
		return 0, false
	}
}

func amd64FusedComparisonCond(kind wasm.InstrKind) (amd64.Cond, bool) {
	if kind == wasm.InstrI32Eqz || kind == wasm.InstrI64Eqz {
		return amd64.CondE, true
	}
	return amd64IntegerComparisonCond(kind)
}

func nativeAMD64FusionConsumer(plan *nativeBackendPlan, producer uint32) (uint32, bool) {
	if plan == nil || int(producer) >= len(plan.PostRAFusionWith) || plan.PostRAFusionWith[producer] == 0 {
		return 0, false
	}
	consumer := plan.PostRAFusionWith[producer] - 1
	return consumer, consumer > producer && int(consumer) < len(plan.Machine.Insts)
}

func nativeAMD64FusionProducer(plan *nativeBackendPlan, consumer uint32) (uint32, bool) {
	if plan == nil || int(consumer) >= len(plan.PostRAFusionWith) || plan.PostRAFusionWith[consumer] == 0 {
		return 0, false
	}
	producer := plan.PostRAFusionWith[consumer] - 1
	return producer, producer < consumer && int(producer) < len(plan.Machine.Insts)
}

// amd64RailMachRotatedZeroTestLatch recognizes a recurrence whose loop header
// only tests one i32 value for zero. The first iteration still enters through
// that header, while later iterations test the transferred latch value directly
// and branch back to the body. Explicit-bounds compilation deliberately admits
// only canonical countdown loops; rotating a longer memory-dependent pointer
// latch can otherwise lengthen its critical path.
func amd64RailMachRotatedZeroTestLatch(plan *nativeBackendPlan, block, backedge uint32) (counter railmach.VReg, exit uint32, ok bool) {
	if plan == nil || plan.Machine == nil || plan.CFG == nil || plan.Semantic == nil || plan.Schedule == nil || plan.Allocation == nil || plan.Exit == nil ||
		plan.Stack == nil || int(backedge) >= len(plan.Machine.Edges) {
		return 0, 0, false
	}
	// On AMD64 the loop-stream detector and taken-branch predictor already
	// handle larger bodies well. Keep rotation to compact latches, where deleting
	// the extra header branch is a clear front-end reduction rather than a code
	// layout tradeoff for a long dependency chain.
	if int(block) >= len(plan.Schedule.BlockRanges) || plan.Schedule.BlockRanges[block].Count > 8 {
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
	producerID, fused := nativeAMD64FusionProducer(plan, consumerID)
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
	if latchCounter == 0 || int(latchCounter) >= len(plan.Machine.VRegs) || plan.Machine.VRegs[latchCounter].Type != railmach.TypeI32 {
		return 0, 0, false
	}
	if !plan.SignalsBounds {
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

func nativeAMD64MemoryFoldSource(plan *nativeBackendPlan, consumer uint32) (uint32, bool) {
	if plan == nil || int(consumer) >= len(plan.PostRAMemoryFrom) || plan.PostRAMemoryFrom[consumer] == 0 {
		return 0, false
	}
	producer := plan.PostRAMemoryFrom[consumer] - 1
	return producer, producer < consumer && int(producer) < len(plan.Machine.Insts)
}

func emitAMD64RailMachBoundsCheck(a *amd64.Asm, plan *nativeBackendPlan, address amd64.Reg, endOffset uint64, source uint32, coldTraps *[]nativeBranchPatch) {
	if railMachElidesBoundsCheck(plan, source) {
		return
	}
	if plan.AMD64MemoryBoundEnd == endOffset {
		a.Cmp64(address, amd64RailMachGPRRegisters[nativeAMD64MemoryBoundRegister])
	} else {
		a.Load64(amd64.RSI, amd64.RBX, -int32(abi.ActualLinMemByteSize64Offset))
		if endOffset != 0 && endOffset <= math.MaxInt32 && endOffset <= plan.Stack.MemoryMinBytes {
			a.AluRI(5, amd64.RSI, int32(endOffset), true)
			a.Cmp64(address, amd64.RSI)
		} else {
			a.MovReg64(amd64.R11, address)
			if endOffset <= math.MaxInt32 {
				a.AluRI(0, amd64.R11, int32(endOffset), true)
			} else {
				a.MovImm64(amd64.RSI, endOffset)
				a.AluRR(0x01, amd64.R11, amd64.RSI, true)
				a.Load64(amd64.RSI, amd64.RBX, -int32(abi.ActualLinMemByteSize64Offset))
			}
			a.Cmp64(amd64.R11, amd64.RSI)
		}
	}
	*coldTraps = append(*coldTraps, nativeBranchPatch{At: a.JccPlaceholder(amd64.CondA), Target: railMachWasmOffset(plan, source), Code: 3})
}

func emitAMD64FoldedIntegerMemory(a *amd64.Asm, plan *nativeBackendPlan, loadID, consumerID uint32, lhs, dst amd64.Reg, wide bool, functionIndex uint32, metadata *functionEmissionMetadata, coldTraps *[]nativeBranchPatch) error {
	load, consumer := plan.Machine.Insts[loadID], plan.Machine.Insts[consumerID]
	loadOperands := plan.Machine.InstructionOperands(loadID)
	if len(loadOperands) != 1 {
		return fmt.Errorf("RailMach folded load %d has %d operands", loadID, len(loadOperands))
	}
	loadPosition := plan.Allocation.InstructionPositions[loadID]*6 + 2
	address, err := amd64RailMachReadLocation(a, plan, loadOperands[0].Reg, plan.Allocation.LocationAt(loadOperands[0].Reg, loadPosition), amd64.R10, 0)
	if err != nil {
		return err
	}
	if address != amd64.R10 {
		a.MovReg32(amd64.R10, address)
	}
	if lhs == amd64.RSI && dst != lhs {
		// A spilled lhs is materialized in RSI, which the explicit bounds
		// check reuses for the current memory length.
		a.MovReg64(dst, lhs)
		lhs = dst
	}
	width := uint64(4)
	if load.Op == wasm.InstrI64Load {
		width = 8
	}
	endOffset := uint64(uint32(load.Aux)) + width
	emitAMD64RailMachBoundsCheck(a, plan, amd64.R10, endOffset, load.Source, coldTraps)
	displacement := int32(uint32(load.Aux))
	if uint32(load.Aux) > math.MaxInt32 {
		a.MovImm64(amd64.R11, uint64(uint32(load.Aux)))
		a.AluRR(0x01, amd64.R10, amd64.R11, true)
		displacement = 0
	}
	if dst != lhs {
		a.MovReg64(dst, lhs)
	}
	opcode := byte(0)
	switch consumer.Op {
	case wasm.InstrI32Add, wasm.InstrI64Add:
		opcode = 0x03
	case wasm.InstrI32Sub, wasm.InstrI64Sub:
		opcode = 0x2b
	case wasm.InstrI32And, wasm.InstrI64And:
		opcode = 0x23
	case wasm.InstrI32Or, wasm.InstrI64Or:
		opcode = 0x0b
	case wasm.InstrI32Xor, wasm.InstrI64Xor:
		opcode = 0x33
	default:
		return fmt.Errorf("RailMach consumer %d cannot fold integer memory", consumerID)
	}
	a.AluIdx(opcode, dst, amd64.RBX, amd64.R10, displacement, wide)
	return nil
}

func emitAMD64FoldedFloatMemory(a *amd64.Asm, plan *nativeBackendPlan, loadID, consumerID uint32, lhs, dst amd64.Reg, functionIndex uint32, metadata *functionEmissionMetadata, coldTraps *[]nativeBranchPatch) error {
	load, consumer := plan.Machine.Insts[loadID], plan.Machine.Insts[consumerID]
	loadOperands := plan.Machine.InstructionOperands(loadID)
	if len(loadOperands) != 1 {
		return fmt.Errorf("RailMach folded float load %d has %d operands", loadID, len(loadOperands))
	}
	loadPosition := plan.Allocation.InstructionPositions[loadID]*6 + 2
	address, err := amd64RailMachReadLocation(a, plan, loadOperands[0].Reg, plan.Allocation.LocationAt(loadOperands[0].Reg, loadPosition), amd64.R10, 0)
	if err != nil {
		return err
	}
	if address != amd64.R10 {
		a.MovReg32(amd64.R10, address)
	}
	width := uint64(4)
	f64 := load.Op == wasm.InstrF64Load
	if f64 {
		width = 8
	}
	endOffset := uint64(uint32(load.Aux)) + width
	emitAMD64RailMachBoundsCheck(a, plan, amd64.R10, endOffset, load.Source, coldTraps)
	disp := int32(uint32(load.Aux))
	if uint32(load.Aux) > math.MaxInt32 {
		a.MovImm64(amd64.R11, uint64(uint32(load.Aux)))
		a.AluRR(0x01, amd64.R10, amd64.R11, true)
		disp = 0
	}
	opcode := byte(0x58)
	switch consumer.Op {
	case wasm.InstrF32Sub, wasm.InstrF64Sub:
		opcode = 0x5c
	case wasm.InstrF32Mul, wasm.InstrF64Mul:
		opcode = 0x59
	case wasm.InstrF32Div, wasm.InstrF64Div:
		opcode = 0x5e
	case wasm.InstrF32Add, wasm.InstrF64Add:
	default:
		return fmt.Errorf("RailMach consumer %d cannot fold float memory", consumerID)
	}
	a.VFMemIdx(opcode, dst, lhs, amd64.RBX, amd64.R10, disp, f64)
	return nil
}

func emitAMD64ExternalCallFPRSave(a *amd64.Asm, plan *nativeBackendPlan, restore bool) {
	if plan == nil || plan.ExternalCallFPRs == 0 {
		return
	}
	offset := plan.Frame.CallAreaOffset + plan.CallArgumentBytes
	for index, register := range amd64FPRRegisters {
		if plan.ExternalCallFPRs&(uint64(1)<<index) == 0 {
			continue
		}
		if restore {
			a.FLoadDisp(register, amd64.RSP, int32(offset), true)
		} else {
			a.FStoreDisp(amd64.RSP, int32(offset), register, true)
		}
		offset += 8
	}
}

func amd64RailMachPhysical(location railmach.Location) amd64.Reg {
	if location.Bank == railmach.BankFPR {
		return amd64FPRRegisters[location.Index]
	}
	return amd64RailMachGPRRegisters[location.Index]
}

func amd64RailMachRegisterLiveAfter(plan *nativeBackendPlan, physical uint16, position uint32, excluded railmach.VReg) bool {
	for _, interval := range plan.Allocation.Intervals {
		if interval.Reg == excluded || interval.Bank != railmach.BankGPR || interval.Start > position || interval.End <= position {
			continue
		}
		location := plan.Allocation.LocationAt(interval.Reg, position)
		if location.Kind == railmach.LocationRegister && location.Index == physical {
			return true
		}
	}
	return false
}

func amd64RailMachReadValue(a *amd64.Asm, plan *nativeBackendPlan, value railmach.VReg, scratch amd64.Reg) (amd64.Reg, error) {
	return amd64RailMachReadValueAt(a, plan, value, scratch, 0)
}

func amd64RailMachReadValueAt(a *amd64.Asm, plan *nativeBackendPlan, value railmach.VReg, scratch amd64.Reg, stackDelta uint32) (amd64.Reg, error) {
	if value == 0 || int(value) >= len(plan.Machine.VRegs) {
		return 0, fmt.Errorf("RailMach value %d is unavailable", value)
	}
	return amd64RailMachReadLocation(a, plan, value, plan.Allocation.Locations[value], scratch, stackDelta)
}

func amd64RailMachReadLocation(a *amd64.Asm, plan *nativeBackendPlan, value railmach.VReg, location railmach.Location, scratch amd64.Reg, stackDelta uint32) (amd64.Reg, error) {
	return amd64RailMachReadLocationWithFloatConstant(a, plan, value, location, scratch, stackDelta, nil)
}

func amd64RailMachReadLocationWithFloatConstant(a *amd64.Asm, plan *nativeBackendPlan, value railmach.VReg, location railmach.Location, scratch amd64.Reg, stackDelta uint32, materializeFloatConstant func(amd64.Reg, uint64, bool)) (amd64.Reg, error) {
	data := plan.Machine.VRegs[value]
	switch location.Kind {
	case railmach.LocationRegister:
		return amd64RailMachPhysical(location), nil
	case railmach.LocationSpill:
		offset := uint64(location.Index)*8 + uint64(stackDelta)
		if offset > math.MaxInt32 {
			return 0, fmt.Errorf("RailMach spill load offset %d is not encodable", offset)
		}
		if data.Bank == railmach.BankFPR {
			a.FLoadDisp(scratch, amd64.RSP, int32(offset), data.Type == railmach.TypeF64)
		} else if data.Type == railmach.TypeI32 {
			a.LoadRsp32(scratch, int32(offset))
		} else {
			a.LoadRsp64(scratch, int32(offset))
		}
		return scratch, nil
	case railmach.LocationRematerialize:
		instructionID := data.Def / 6
		if int(instructionID) >= len(plan.Machine.Insts) {
			return 0, fmt.Errorf("RailMach rematerialization value %d has no definition", value)
		}
		definition := plan.Machine.Insts[instructionID]
		switch definition.Op {
		case wasm.InstrI32Const:
			a.MovImm32(scratch, int32(definition.Aux))
		case wasm.InstrI64Const, wasm.InstrRefNull:
			a.MovImm64(scratch, definition.Aux)
		case wasm.InstrF32Const, wasm.InstrF64Const:
			f64 := definition.Op == wasm.InstrF64Const
			if materializeFloatConstant != nil {
				materializeFloatConstant(scratch, definition.Aux, f64)
			} else {
				emitAMD64FloatBits(a, scratch, definition.Aux, f64)
			}
		case wasm.InstrI32WrapI64, wasm.InstrI64ExtendI32U, wasm.InstrI64ExtendI32S,
			wasm.InstrI32Extend8S, wasm.InstrI32Extend16S,
			wasm.InstrI64Extend8S, wasm.InstrI64Extend16S, wasm.InstrI64Extend32S:
			operands := plan.Machine.InstructionOperands(instructionID)
			if len(operands) != 1 {
				return 0, fmt.Errorf("RailMach value %d has malformed extension rematerialization", value)
			}
			base, err := amd64RailMachReadValueAt(a, plan, operands[0].Reg, scratch, stackDelta)
			if err != nil {
				return 0, err
			}
			switch definition.Op {
			case wasm.InstrI64ExtendI32S, wasm.InstrI64Extend32S:
				a.Movsxd(scratch, base)
			case wasm.InstrI32Extend8S, wasm.InstrI64Extend8S:
				a.Movsx8(scratch, base, definition.Op == wasm.InstrI64Extend8S)
			case wasm.InstrI32Extend16S, wasm.InstrI64Extend16S:
				a.Movsx16(scratch, base, definition.Op == wasm.InstrI64Extend16S)
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
			base, err := amd64RailMachReadValueAt(a, plan, operands[0].Reg, scratch, stackDelta)
			if err != nil {
				return 0, err
			}
			wide := definition.Op == wasm.InstrI64Add || definition.Op == wasm.InstrI64Sub
			if base != scratch {
				if wide {
					a.MovReg64(scratch, base)
				} else {
					a.MovReg32(scratch, base)
				}
			}
			digit := byte(0)
			if definition.Op == wasm.InstrI32Sub || definition.Op == wasm.InstrI64Sub {
				digit = 5
			}
			a.AluRI(digit, scratch, int32(plan.Machine.Insts[constant.Def/6].Aux), wide)
		default:
			return 0, fmt.Errorf("RailMach value %d has unsupported rematerialization %s", value, definition.Op)
		}
		return scratch, nil
	default:
		return 0, fmt.Errorf("RailMach value %d has invalid location %#v", value, location)
	}
}

func emitAMD64RailMachRoots(a *amd64.Asm, plan *nativeBackendPlan, source, position uint32, reload bool) error {
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
		rootOffset := uint64(plan.Frame.SpillBytes) + uint64(root.Slot)*8
		if rootOffset > math.MaxInt32 {
			return fmt.Errorf("RailMach root offset %d is not encodable", rootOffset)
		}
		location := plan.Allocation.LocationAt(value, position)
		if !reload {
			src, err := amd64RailMachReadLocation(a, plan, value, location, amd64.R11, 0)
			if err != nil {
				return err
			}
			a.StoreRsp64(int32(rootOffset), src)
			continue
		}
		a.LoadRsp64(amd64.R11, int32(rootOffset))
		switch location.Kind {
		case railmach.LocationRegister:
			dst := amd64RailMachPhysical(location)
			if dst != amd64.R11 {
				a.MovReg64(dst, amd64.R11)
			}
		case railmach.LocationSpill:
			a.StoreRsp64(int32(location.Index)*8, amd64.R11)
		default:
			return fmt.Errorf("RailMach live collector root %d has non-material location %d", value, location.Kind)
		}
	}
	return nil
}

func amd64RailMachWriteLocation(a *amd64.Asm, plan *nativeBackendPlan, value railmach.VReg, location railmach.Location, src amd64.Reg) error {
	data := plan.Machine.VRegs[value]
	switch location.Kind {
	case railmach.LocationRegister:
		dst := amd64RailMachPhysical(location)
		if dst == src {
			return nil
		}
		if data.Bank == railmach.BankFPR {
			a.FMov(dst, src, data.Type == railmach.TypeF64)
		} else if data.Type == railmach.TypeI32 {
			a.MovReg32(dst, src)
		} else {
			a.MovReg64(dst, src)
		}
		return nil
	case railmach.LocationSpill:
		offset := uint64(location.Index) * 8
		if offset > math.MaxInt32 {
			return fmt.Errorf("RailMach spill store offset %d is not encodable", offset)
		}
		if data.Bank == railmach.BankFPR {
			a.FStoreDisp(amd64.RSP, int32(offset), src, data.Type == railmach.TypeF64)
		} else if data.Type == railmach.TypeI32 {
			a.StoreRsp32(int32(offset), src)
		} else {
			a.StoreRsp64(int32(offset), src)
		}
		return nil
	default:
		return fmt.Errorf("RailMach value %d has unwritable location %#v", value, location)
	}
}

func amd64RailMachStoreValue(a *amd64.Asm, plan *nativeBackendPlan, value railmach.VReg, src amd64.Reg) error {
	return amd64RailMachWriteLocation(a, plan, value, plan.Allocation.Locations[value], src)
}

func amd64StagePrivateCallResults(a *amd64.Asm, instruction railmach.Inst, callOffset int32) {
	for index := 0; index < min(int(instruction.ResultCount()), railmach.PrivateResultRegisters); index++ {
		a.StoreRsp64(callOffset+int32(index*8), amd64RailMachGPRRegisters[index])
	}
}

func amd64MaterializeCallResults(a *amd64.Asm, plan *nativeBackendPlan, instruction railmach.Inst, callOffset int32, position uint32) error {
	for ordinal := uint32(0); ordinal < instruction.ResultCount(); ordinal++ {
		value := instruction.Result + railmach.VReg(ordinal)
		location := plan.Allocation.LocationAt(value, position)
		if location.Kind == railmach.LocationInvalid {
			continue
		}
		a.LoadRsp64(amd64.R11, callOffset+int32(ordinal*8))
		src := amd64.R11
		if plan.Machine.VRegs[value].Bank == railmach.BankFPR {
			a.MovGprToXmm(13, amd64.R11, plan.Machine.VRegs[value].Type == railmach.TypeF64)
			src = 13
		}
		if err := amd64RailMachWriteLocation(a, plan, value, location, src); err != nil {
			return err
		}
	}
	return nil
}

func emitAMD64RailMachEdgeMoves(a *amd64.Asm, plan *nativeBackendPlan, edge uint32) error {
	moveRange := plan.Exit.EdgeMoves[edge]
	if err := emitAMD64RailMachMoveRangeAt(a, plan, moveRange, railmach.PlacePredecessorEnd); err != nil {
		return err
	}
	return emitAMD64RailMachMoveRangeAt(a, plan, moveRange, railmach.PlaceSplitEdge)
}

func emitAMD64RailMachSuccessorMoves(a *amd64.Asm, plan *nativeBackendPlan, edge uint32) error {
	return emitAMD64RailMachMoveRangeAt(a, plan, plan.Exit.EdgeMoves[edge], railmach.PlaceSuccessorStart)
}

func emitAMD64RailMachMoveRange(a *amd64.Asm, plan *nativeBackendPlan, moveRange railmach.MoveRange) error {
	return emitAMD64RailMachMoveRangeAt(a, plan, moveRange, railmach.PlaceInvalid)
}

func emitAMD64RailMachMoveRangeAt(a *amd64.Asm, plan *nativeBackendPlan, moveRange railmach.MoveRange, placement railmach.MovePlacement) error {
	for _, move := range plan.Exit.Moves[moveRange.Start : moveRange.Start+moveRange.Count] {
		if placement != railmach.PlaceInvalid && move.Placement != placement {
			continue
		}
		typ := plan.Machine.VRegs[move.Reg].Type
		temporary := amd64.R10
		if move.Bank == railmach.BankFPR {
			temporary = 15
		}
		if move.Temporary == 1 {
			temporary = amd64.R11
			if move.Bank == railmach.BankFPR {
				temporary = 14
			}
		}
		switch move.Kind {
		case railmach.MoveSaveTemporary:
			src, err := amd64RailMachReadLocation(a, plan, move.Reg, move.Src, temporary, 0)
			if err != nil {
				return err
			}
			if src != temporary {
				if move.Bank == railmach.BankFPR {
					a.FMov(temporary, src, typ == railmach.TypeF64)
				} else if typ == railmach.TypeI32 {
					a.MovReg32(temporary, src)
				} else {
					a.MovReg64(temporary, src)
				}
			}
		case railmach.MoveRestoreTemporary:
			if err := amd64RailMachWriteLocation(a, plan, move.Reg, move.Dst, temporary); err != nil {
				return err
			}
		case railmach.MoveCopy, railmach.MoveRematerialize:
			scratch := temporary
			if move.Dst.Kind == railmach.LocationRegister {
				scratch = amd64RailMachPhysical(move.Dst)
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
			src, err := amd64RailMachReadLocation(a, plan, move.Reg, source, scratch, 0)
			if err != nil {
				return err
			}
			if err := amd64RailMachWriteLocation(a, plan, move.Reg, move.Dst, src); err != nil {
				return err
			}
		default:
			return fmt.Errorf("RailMach has invalid move kind %d", move.Kind)
		}
	}
	return nil
}

func amd64RailMachDivisionSafe(plan *nativeBackendPlan, instructionID uint32, operands []railmach.Operand) bool {
	if len(operands) != 2 {
		return false
	}
	lhs := plan.Allocation.Locations[operands[0].Reg]
	if lhs.Kind != railmach.LocationRegister || lhs.Index != 0 {
		moveRange, ok := nativeFixedMoveRange(plan, instructionID)
		if !ok {
			return false
		}
		found := false
		for _, move := range plan.Exit.Moves[moveRange.Start : moveRange.Start+moveRange.Count] {
			if move.Reg == operands[0].Reg && move.Dst.Kind == railmach.LocationRegister && move.Dst.Bank == railmach.BankGPR && move.Dst.Index == 0 {
				found = true
			}
		}
		if !found {
			return false
		}
	}
	position := plan.Allocation.InstructionPositions[instructionID]*6 + 2
	for _, interval := range plan.Allocation.Intervals {
		location := plan.Allocation.Locations[interval.Reg]
		if location.Kind != railmach.LocationRegister || location.Index != 0 && location.Index != 2 {
			continue
		}
		if interval.Start < position && interval.End > position {
			return false
		}
	}
	return true
}

func amd64RailMachTargetSafe(plan *nativeBackendPlan) bool {
	if !amd64RailMachExitSafe(plan) {
		return false
	}
	denseGlobalModule := plan.Stack.Module != nil && len(plan.Stack.Module.Globals) >= amd64RailMachDenseGlobalThreshold
	for instructionID := range plan.Machine.Insts {
		instruction := plan.Machine.Insts[instructionID]
		operands := plan.Machine.InstructionOperands(uint32(instructionID))
		if amd64DirectSafeDivKind(instruction.Op) && !amd64RailMachDivisionSafe(plan, uint32(instructionID), operands) {
			return false
		}
		if (instruction.Op == wasm.InstrCall || instruction.Op == wasm.InstrCallIndirect) && !nativeCallTargetSafe(plan, uint32(instructionID)) {
			return false
		}
		trunc := railMachTrappingTrunc(instruction.Op)
		truncSat := instruction.Op >= wasm.InstrI32TruncSatF32S && instruction.Op <= wasm.InstrI64TruncSatF64U
		if !trunc && !truncSat {
			continue
		}
		scratchLive := railMachPhysicalLiveAcross(plan, uint32(instructionID), railmach.BankGPR, 0) ||
			railMachPhysicalLiveAcross(plan, uint32(instructionID), railmach.BankFPR, 1)
		if scratchLive && (trunc || denseGlobalModule) {
			return false
		}
	}
	return true
}

func amd64RailMachExitSafe(plan *nativeBackendPlan) bool {
	if plan == nil || plan.Exit == nil {
		return false
	}
	locationSafe := func(location railmach.Location) bool {
		switch location.Kind {
		case railmach.LocationInvalid:
			return true
		case railmach.LocationRegister:
			return location.Bank == railmach.BankGPR && int(location.Index) < len(amd64RailMachGPRRegisters) ||
				location.Bank == railmach.BankFPR && int(location.Index) < len(amd64FPRRegisters)
		case railmach.LocationSpill:
			return location.Index < plan.Allocation.SpillSlots && uint64(location.Index)*8 <= math.MaxInt32
		case railmach.LocationRematerialize:
			return true
		default:
			return location.Kind == railmach.LocationTemporary
		}
	}
	for _, move := range plan.Exit.Moves {
		if !locationSafe(move.Src) || !locationSafe(move.Dst) || move.Kind < railmach.MoveCopy || move.Kind > railmach.MoveRematerialize {
			return false
		}
	}
	return true
}

type amd64StackControl struct {
	kind            wasm.InstrKind
	start           int
	depth           int
	result          bool
	resultType      wasm.ValType
	endReached      bool
	falsePatch      int
	patches         []int
	parentReachable bool
	seenElse        bool
}

func planAMD64StructuredLocalMemoryChecks(sf *railssa.StackFunc) ([]uint64, []bool) {
	ends := make([]uint64, len(sf.Instrs))
	elided := make([]bool, len(sf.Instrs))
	for instructionID := range sf.Instrs {
		local, ok := amd64StructuredLoadAddressLocal(sf, instructionID)
		if !ok || elided[instructionID] {
			continue
		}
		end, ok := amd64StructuredLoadEnd(sf, instructionID)
		if !ok {
			continue
		}
		maxEnd := end
		for next := instructionID + 1; next < len(sf.Instrs); next++ {
			instruction := sf.Instrs[next]
			if nativeControlInstruction(instruction.Kind) || railmach.IsCall(instruction.Kind) || instruction.Kind == wasm.InstrMemoryGrow {
				break
			}
			if (instruction.Kind == wasm.InstrLocalSet || instruction.Kind == wasm.InstrLocalTee) && instruction.U32() == local {
				break
			}
			nextLocal, sameLocal := amd64StructuredLoadAddressLocal(sf, next)
			nextEnd, load := amd64StructuredLoadEnd(sf, next)
			if load {
				if !sameLocal || nextLocal != local {
					break
				}
				if nextEnd > maxEnd {
					maxEnd = nextEnd
				}
				continue
			}
			if instruction.Kind != wasm.InstrLocalGet && instruction.Kind != wasm.InstrLocalTee && instruction.Kind != wasm.InstrDrop {
				break
			}
		}
		if maxEnd == end {
			continue
		}
		ends[instructionID] = maxEnd
		for next := instructionID + 1; next < len(sf.Instrs); next++ {
			instruction := sf.Instrs[next]
			if nativeControlInstruction(instruction.Kind) || railmach.IsCall(instruction.Kind) || instruction.Kind == wasm.InstrMemoryGrow ||
				(instruction.Kind == wasm.InstrLocalSet || instruction.Kind == wasm.InstrLocalTee) && instruction.U32() == local {
				break
			}
			nextLocal, sameLocal := amd64StructuredLoadAddressLocal(sf, next)
			if followerEnd, load := amd64StructuredLoadEnd(sf, next); load {
				if !sameLocal || nextLocal != local {
					break
				}
				if followerEnd <= maxEnd {
					elided[next] = true
				}
				continue
			}
			if instruction.Kind != wasm.InstrLocalGet && instruction.Kind != wasm.InstrLocalTee && instruction.Kind != wasm.InstrDrop {
				break
			}
		}
	}
	return ends, elided
}

func amd64StructuredLoadAddressLocal(sf *railssa.StackFunc, instructionID int) (uint32, bool) {
	if instructionID == 0 || instructionID >= len(sf.Instrs) {
		return 0, false
	}
	previous := sf.Instrs[instructionID-1]
	if previous.Kind != wasm.InstrLocalGet && previous.Kind != wasm.InstrLocalTee || int(previous.U32()) >= len(sf.Locals) || sf.Locals[previous.U32()] != wasm.I32 {
		return 0, false
	}
	return previous.U32(), true
}

func amd64StructuredLoadEnd(sf *railssa.StackFunc, instructionID int) (uint64, bool) {
	instruction := sf.Instrs[instructionID]
	if instruction.Kind == wasm.InstrV128Load {
		descriptor, ok := sf.SIMDImmediateAt(uint32(instructionID))
		if !ok || descriptor.MemArg.Offset > math.MaxUint64-16 {
			return 0, false
		}
		return descriptor.MemArg.Offset + 16, true
	}
	if !amd64MemoryStackKind(instruction.Kind) || instruction.Kind >= wasm.InstrI32Store && instruction.Kind <= wasm.InstrI64Store32 {
		return 0, false
	}
	size := uint64(4)
	switch instruction.Kind {
	case wasm.InstrI64Load, wasm.InstrF64Load:
		size = 8
	case wasm.InstrI32Load8S, wasm.InstrI32Load8U, wasm.InstrI64Load8S, wasm.InstrI64Load8U:
		size = 1
	case wasm.InstrI32Load16S, wasm.InstrI32Load16U, wasm.InstrI64Load16S, wasm.InstrI64Load16U:
		size = 2
	}
	return uint64(instruction.U32()) + size, true
}

func amd64PinHotStructuredScalarLocals(locals []wasm.ValType, uses []uint32, localRegisters []amd64.Reg, localPinned []bool) {
	for _, register := range amd64StackLocalRegisters {
		best := -1
		for local, typ := range locals {
			if localPinned[local] || uses[local] == 0 || typ != wasm.I32 && typ != wasm.I64 {
				continue
			}
			if best < 0 || uses[local] > uses[best] {
				best = local
			}
		}
		if best < 0 {
			return
		}
		localRegisters[best] = register
		localPinned[best] = true
	}
}

func amd64StructuredLocalsPinned(localPinned []bool, locals ...uint32) bool {
	for _, local := range locals {
		if int(local) >= len(localPinned) || !localPinned[local] {
			return false
		}
	}
	return true
}

func emitAMD64Stack(fn *railssa.Func, plan *railssa.EmissionPlan, metrics *FunctionMetrics, metadata *functionEmissionMetadata) ([]byte, int, []amd64CallReloc, error) {
	sf := fn.Stack
	callRelocs := make([]amd64CallReloc, 0, 2)
	localRegisters := make([]amd64.Reg, len(sf.Locals))
	localFloat := make([]bool, len(sf.Locals))
	localPinned := make([]bool, len(sf.Locals))
	scalarLocalUses := make([]uint32, len(sf.Locals))
	vectorLocalUses := make([]uint32, len(sf.Locals))
	var simdConstants []amd64SIMDConstant
	gpLocals, fpLocals := 0, 0
	for i, typ := range sf.Locals {
		if typ == wasm.V128 {
			continue
		}
		if typ == wasm.F32 || typ == wasm.F64 {
			localFloat[i] = true
			if fpLocals < 8 {
				localRegisters[i] = amd64.Reg(8 + fpLocals)
			}
			fpLocals++
		} else {
			if gpLocals < len(amd64StackLocalRegisters) {
				localRegisters[i] = amd64StackLocalRegisters[gpLocals]
			}
			gpLocals++
		}
	}
	hasGeneralCall := false
	hasMemoryAccess := false
	hasMemoryGrow := false
	observeSIMDConstant := func(value [16]byte) {
		for i := range simdConstants {
			if simdConstants[i].bytes == value {
				simdConstants[i].uses++
				return
			}
		}
		simdConstants = append(simdConstants, amd64SIMDConstant{bytes: value, uses: 1})
	}
	for instrIndex, instr := range sf.Instrs {
		if (instr.Kind == wasm.InstrCall || instr.Kind == wasm.InstrCallIndirect) && instr.Inline() == wasm.InstrInvalid ||
			instr.Kind == wasm.InstrElemDrop || instr.Kind == wasm.InstrStructGet || instr.Kind == wasm.InstrStructSet ||
			instr.Kind == wasm.InstrArrayGet || instr.Kind == wasm.InstrArraySet || instr.Kind == wasm.InstrArrayFill || instr.Kind == wasm.InstrRefCast ||
			instr.Kind == wasm.InstrBrOnCast || instr.Kind == wasm.InstrBrOnCastFail ||
			instr.Kind == wasm.InstrAnyConvertExtern || instr.Kind == wasm.InstrExternConvertAny {
			hasGeneralCall = true
		}
		if (instr.Kind == wasm.InstrLocalGet || instr.Kind == wasm.InstrLocalSet || instr.Kind == wasm.InstrLocalTee) &&
			int(instr.U32()) < len(sf.Locals) {
			if sf.Locals[instr.U32()] == wasm.V128 {
				vectorLocalUses[instr.U32()]++
			} else {
				scalarLocalUses[instr.U32()]++
			}
		}
		if instr.Kind == wasm.InstrV128Const {
			if descriptor, ok := sf.SIMDImmediateAt(uint32(instrIndex)); ok {
				observeSIMDConstant(descriptor.Bytes)
			}
		}
		hasMemoryAccess = hasMemoryAccess || amd64MemoryStackKind(instr.Kind) || instr.Kind == wasm.InstrV128Load || instr.Kind == wasm.InstrV128Store || instr.Kind == wasm.InstrV128Store64Lane
		hasMemoryGrow = hasMemoryGrow || instr.Kind == wasm.InstrMemoryGrow
	}
	cacheMemorySize := hasMemoryAccess && !hasMemoryGrow && !hasGeneralCall
	registerLocals := !sf.HasV128 && !hasGeneralCall && len(sf.Params) <= 4 && gpLocals <= len(amd64StackLocalRegisters) && fpLocals <= 8
	if registerLocals {
		for i := range localPinned {
			localPinned[i] = true
		}
	} else if sf.HasV128 && !hasGeneralCall && len(sf.Params) <= 4 {
		for i := range localRegisters {
			localRegisters[i] = 0
		}
		amd64PinHotStructuredScalarLocals(sf.Locals, scalarLocalUses, localRegisters, localPinned)
		constantSelected := make([]bool, len(simdConstants))
		for slot := 0; slot < 8; slot++ {
			bestLocal := -1
			for i, uses := range vectorLocalUses {
				if sf.Locals[i] == wasm.V128 && !localPinned[i] && uses != 0 && (bestLocal < 0 || uses > vectorLocalUses[bestLocal]) {
					bestLocal = i
				}
			}
			bestConstant := -1
			for i := range simdConstants {
				if !constantSelected[i] && simdConstants[i].uses > 1 && (bestConstant < 0 || simdConstants[i].uses > simdConstants[bestConstant].uses) {
					bestConstant = i
				}
			}
			localScore := uint64(0)
			if bestLocal >= 0 {
				localScore = uint64(vectorLocalUses[bestLocal])
			}
			constantScore := uint64(0)
			if bestConstant >= 0 {
				// Both a resident vector local and a resident constant avoid one
				// memory access now that uncached constants use the RIP pool.
				constantScore = uint64(simdConstants[bestConstant].uses - 1)
			}
			if localScore == 0 && constantScore == 0 {
				break
			}
			reg := amd64.Reg(8 + slot)
			if constantScore > localScore {
				constantSelected[bestConstant] = true
				simdConstants[bestConstant].reg = reg
			} else {
				localPinned[bestLocal] = true
				localRegisters[bestLocal] = reg
			}
		}
		selected := simdConstants[:0]
		for i := range simdConstants {
			if constantSelected[i] {
				selected = append(selected, simdConstants[i])
			}
		}
		simdConstants = selected
	} else {
		simdConstants = simdConstants[:0]
	}
	stackSlots := uint64(sf.MaxStack)
	if sf.HasV128 {
		stackSlots *= 2
	}
	frameBytes, err := boundedFrameBytes("AMD64 structured frame bytes", uint64(railssa.TypeSlots(sf.Locals))+stackSlots, uint64(^uint32(0)>>1))
	if err != nil {
		return nil, 0, nil, err
	}
	var a amd64.Asm
	simdConstantPatches := make([]amd64SIMDConstantPatch, 0, 8)
	materializeSIMDConstant := func(reg amd64.Reg, value [16]byte) {
		if value == [16]byte{} {
			a.VPxor(reg, reg, reg)
			return
		}
		simdConstantPatches = append(simdConstantPatches, amd64SIMDConstantPatch{at: a.MovdquRipPlaceholder(reg), bytes: value})
	}
	shuffleSIMDConstant := func(dst, src amd64.Reg, value [16]byte) {
		simdConstantPatches = append(simdConstantPatches, amd64SIMDConstantPatch{at: a.VPshufbRipPlaceholder(dst, src), bytes: value})
	}
	simdConstantOperand := func(kind wasm.InstrKind, dst, src amd64.Reg, value [16]byte) {
		patch := amd64SIMDConstantPatch{bytes: value}
		switch kind {
		case wasm.InstrV128And:
			patch.at = a.VPandRipPlaceholder(dst, src)
		case wasm.InstrV128Or:
			patch.at = a.VPorRipPlaceholder(dst, src)
		case wasm.InstrV128Xor:
			patch.at = a.VPxorRipPlaceholder(dst, src)
		case wasm.InstrI8x16SubSatU:
			patch.at = a.VPsubusbRipPlaceholder(dst, src)
		case wasm.InstrI8x16Eq:
			patch.at = a.VPcmpeqbRipPlaceholder(dst, src)
		case wasm.InstrI16x8Eq:
			patch.at = a.VPcmpeqwRipPlaceholder(dst, src)
		}
		simdConstantPatches = append(simdConstantPatches, patch)
	}
	if metrics != nil {
		metrics.FrameBytes = frameBytes
	}
	a.Push(amd64.RCX)
	a.MovReg64(amd64.RBX, amd64.RSI)
	for i, typ := range sf.Params[:min(len(sf.Params), len(amd64ParamRegisters))] {
		argOffset := int32(railssa.TypeSlotOffset(sf.Params, i) * 8)
		if typ == wasm.V128 {
			a.VMovdquLoadDisp(amd64.Reg(i), amd64.RDI, argOffset)
		} else if typ == wasm.I32 {
			a.Load32(amd64ParamRegisters[i], amd64.RDI, argOffset)
		} else {
			a.Load64(amd64ParamRegisters[i], amd64.RDI, argOffset)
		}
	}
	call := a.CallRel32()
	a.Pop(amd64.RCX)
	if len(sf.Results) == 1 {
		if sf.Results[0] == wasm.V128 {
			a.VMovdquStoreDisp(amd64.RCX, 0, 0)
		} else if sf.Results[0] == wasm.I32 {
			a.Store32(amd64.RCX, 0, amd64.RAX)
		} else {
			a.Store64(amd64.RCX, 0, amd64.RAX)
		}
	}
	a.Ret()
	a.Align16()
	internalOffset := a.Len()
	a.PatchRel32(call, internalOffset)
	if len(sf.Instrs) != 0 {
		metadata.recordSource(internalOffset, sf.Instrs[0].Offset)
	}
	if frameBytes != 0 {
		a.SubRsp(int32(frameBytes))
	}
	localOff := func(index int) int32 { return int32(railssa.TypeSlotOffset(sf.Locals, index) * 8) }
	for i := range sf.Params {
		if i >= len(amd64ParamRegisters) {
			if sf.Locals[i] == wasm.V128 {
				a.VMovdquLoadDisp(0, amd64.RDI, int32(railssa.TypeSlotOffset(sf.Params, i)*8))
				a.VMovdquStoreDisp(amd64.RSP, localOff(i), 0)
			} else {
				a.Load64(amd64.RAX, amd64.RDI, int32(railssa.TypeSlotOffset(sf.Params, i)*8))
				a.StoreRsp64(localOff(i), amd64.RAX)
			}
			continue
		}
		if localPinned[i] {
			if sf.Locals[i] == wasm.V128 {
				a.VMovdqu(localRegisters[i], amd64.Reg(i))
			} else if localFloat[i] {
				a.MovGprToXmm(localRegisters[i], amd64ParamRegisters[i], sf.Locals[i] == wasm.F64)
			} else {
				a.MovReg64(localRegisters[i], amd64ParamRegisters[i])
			}
		} else {
			if sf.Locals[i] == wasm.V128 {
				a.VMovdquStoreDisp(amd64.RSP, localOff(i), amd64.Reg(i))
			} else {
				a.StoreRsp64(localOff(i), amd64ParamRegisters[i])
			}
		}
	}
	if !registerLocals && len(sf.Locals) > len(sf.Params) {
		a.XorSelf32(amd64.RAX)
	}
	for i := len(sf.Params); i < len(sf.Locals); i++ {
		if localPinned[i] {
			if sf.Locals[i] == wasm.V128 {
				a.VPxor(localRegisters[i], localRegisters[i], localRegisters[i])
			} else if localFloat[i] {
				a.XorSelf32(amd64.RAX)
				a.MovGprToXmm(localRegisters[i], amd64.RAX, sf.Locals[i] == wasm.F64)
			} else {
				a.XorSelf32(localRegisters[i])
			}
		} else {
			if sf.Locals[i] == wasm.V128 {
				a.VPxor(0, 0, 0)
				a.VMovdquStoreDisp(amd64.RSP, localOff(i), 0)
			} else {
				a.StoreRsp64(localOff(i), amd64.RAX)
			}
		}
	}
	for _, constant := range simdConstants {
		materializeSIMDConstant(constant.reg, constant.bytes)
	}
	if cacheMemorySize {
		a.Load64(amd64.RBP, amd64.RBX, -int32(abi.ActualLinMemByteSize64Offset))
	}
	stackTypes := make([]wasm.ValType, 0, sf.MaxStack)
	localMemoryCheckEnds, localMemoryChecksElided := planAMD64StructuredLocalMemoryChecks(sf)
	controls := make([]amd64StackControl, 0, 8)
	functionPatches := make([]int, 0, 2)
	pendingConditionAt := -1
	pendingCondition := amd64.Cond(0)
	defer func() {
		workspace := fn.CapacityBytes() + sliceBytes(callRelocs) + sliceBytes(localRegisters) + sliceBytes(localFloat) + sliceBytes(localPinned) + sliceBytes(scalarLocalUses) + sliceBytes(vectorLocalUses) + sliceBytes(simdConstants) + sliceBytes(simdConstantPatches) + sliceBytes(localMemoryCheckEnds) + sliceBytes(localMemoryChecksElided) + sliceBytes(a.B) + sliceBytes(stackTypes) + sliceBytes(controls) + sliceBytes(functionPatches)
		for i := range controls {
			workspace += sliceBytes(controls[i].patches)
		}
		metrics.observe(workspace)
	}()
	reachable := true
	localSlots := railssa.TypeSlots(sf.Locals)
	stackOff := func(index int) int32 { return int32(localSlots+railssa.TypeSlotOffset(stackTypes, index)) * 8 }
	vectorStackCacheRegisters := [12]amd64.Reg{4, 5, 6, 7}
	vectorStackCacheEntries := 4
	var highVectorRegisterUsed [8]bool
	for local, pinned := range localPinned {
		if pinned && sf.Locals[local] == wasm.V128 && localRegisters[local] >= 8 {
			highVectorRegisterUsed[localRegisters[local]-8] = true
		}
	}
	for _, constant := range simdConstants {
		if constant.reg >= 8 {
			highVectorRegisterUsed[constant.reg-8] = true
		}
	}
	for index, used := range highVectorRegisterUsed {
		if !used {
			vectorStackCacheRegisters[vectorStackCacheEntries] = amd64.Reg(index + 8)
			vectorStackCacheEntries++
		}
	}
	var vectorStackCache [12]int
	for index := range vectorStackCache {
		vectorStackCache[index] = -1
	}
	scalarStackCacheRegisters := [...]amd64.Reg{amd64.RDI, amd64.RSI, amd64.RBP}
	scalarStackCacheEntries := len(scalarStackCacheRegisters)
	if cacheMemorySize {
		scalarStackCacheEntries--
	}
	scalarStackCache := [...]int{-1, -1, -1}
	findVectorStackCache := func(index int) int {
		for i, cached := range vectorStackCache[:vectorStackCacheEntries] {
			if cached == index {
				return i
			}
		}
		return -1
	}
	cachedV128 := func(index int) (amd64.Reg, bool) {
		cache := findVectorStackCache(index)
		if cache < 0 {
			return 0, false
		}
		return vectorStackCacheRegisters[cache], true
	}
	flushVectorStackCache := func() {
		for i, index := range vectorStackCache[:vectorStackCacheEntries] {
			if index < 0 {
				continue
			}
			a.VMovdquStoreDisp(amd64.RSP, stackOff(index), vectorStackCacheRegisters[i])
			vectorStackCache[i] = -1
		}
	}
	flushScalarStackCache := func() {
		for i, index := range scalarStackCache[:scalarStackCacheEntries] {
			if index < 0 {
				continue
			}
			a.StoreRsp64(stackOff(index), scalarStackCacheRegisters[i])
			scalarStackCache[i] = -1
		}
	}
	findScalarStackCache := func(index int) int {
		for i, cached := range scalarStackCache[:scalarStackCacheEntries] {
			if cached == index {
				return i
			}
		}
		return -1
	}
	scalarOperand := func(index int, scratch amd64.Reg) amd64.Reg {
		if cache := findScalarStackCache(index); cache >= 0 {
			return scalarStackCacheRegisters[cache]
		}
		a.LoadRsp64(scratch, stackOff(index))
		return scratch
	}
	loadScalar := func(index int, reg amd64.Reg) {
		operand := scalarOperand(index, reg)
		if operand != reg {
			a.MovReg64(reg, operand)
		}
	}
	cacheScalar := func(index int, reg amd64.Reg) {
		cache := findScalarStackCache(index)
		if cache < 0 {
			for i, cached := range scalarStackCache[:scalarStackCacheEntries] {
				if cached < 0 {
					cache = i
					break
				}
			}
		}
		if cache < 0 {
			cache = 0
			for i := 1; i < scalarStackCacheEntries; i++ {
				if scalarStackCache[i] < scalarStackCache[cache] {
					cache = i
				}
			}
			a.StoreRsp64(stackOff(scalarStackCache[cache]), scalarStackCacheRegisters[cache])
		}
		cachedReg := scalarStackCacheRegisters[cache]
		if reg != cachedReg {
			a.MovReg64(cachedReg, reg)
		}
		scalarStackCache[cache] = index
	}
	discardScalar := func(index int) {
		if cache := findScalarStackCache(index); cache >= 0 {
			scalarStackCache[cache] = -1
		}
	}
	loadV128 := func(index int, reg amd64.Reg) {
		if cache := findVectorStackCache(index); cache >= 0 {
			if cachedReg := vectorStackCacheRegisters[cache]; reg != cachedReg {
				a.VMovdqu(reg, cachedReg)
			}
			return
		}
		a.VMovdquLoadDisp(reg, amd64.RSP, stackOff(index))
	}
	takeV128 := func(index int, scratch amd64.Reg) amd64.Reg {
		if cache := findVectorStackCache(index); cache >= 0 {
			reg := vectorStackCacheRegisters[cache]
			vectorStackCache[cache] = -1
			return reg
		}
		a.VMovdquLoadDisp(scratch, amd64.RSP, stackOff(index))
		return scratch
	}
	reserveV128 := func(index int) amd64.Reg {
		cache := findVectorStackCache(index)
		if cache < 0 {
			for i, cached := range vectorStackCache[:vectorStackCacheEntries] {
				if cached < 0 {
					cache = i
					break
				}
			}
		}
		if cache < 0 {
			cache = 0
			for i := 1; i < vectorStackCacheEntries; i++ {
				if vectorStackCache[i] < vectorStackCache[cache] {
					cache = i
				}
			}
			a.VMovdquStoreDisp(amd64.RSP, stackOff(vectorStackCache[cache]), vectorStackCacheRegisters[cache])
		}
		cachedReg := vectorStackCacheRegisters[cache]
		vectorStackCache[cache] = index
		return cachedReg
	}
	cacheV128 := func(index int, reg amd64.Reg) {
		cachedReg := reserveV128(index)
		if reg != cachedReg {
			a.VMovdqu(cachedReg, reg)
		}
	}
	discardV128 := func(index int) {
		if cache := findVectorStackCache(index); cache >= 0 {
			vectorStackCache[cache] = -1
		}
	}
	pruneVectorStackCache := func() {
		for i, index := range vectorStackCache[:vectorStackCacheEntries] {
			if index >= len(stackTypes) || index >= 0 && stackTypes[index] != wasm.V128 {
				vectorStackCache[i] = -1
			}
		}
	}
	pruneScalarStackCache := func() {
		for i, index := range scalarStackCache[:scalarStackCacheEntries] {
			if index >= len(stackTypes) || index >= 0 && stackTypes[index] == wasm.V128 {
				scalarStackCache[i] = -1
			}
		}
	}
	push := func(typ wasm.ValType, reg amd64.Reg) error {
		if len(stackTypes) >= int(sf.MaxStack) {
			return fmt.Errorf("operand stack exceeds declared maximum")
		}
		cacheScalar(len(stackTypes), reg)
		stackTypes = append(stackTypes, typ)
		return nil
	}
	pop := func(reg amd64.Reg) (wasm.ValType, error) {
		if len(stackTypes) == 0 {
			return wasm.ValType{}, fmt.Errorf("operand stack underflow")
		}
		index := len(stackTypes) - 1
		typ := stackTypes[index]
		loadScalar(index, reg)
		discardScalar(index)
		stackTypes = stackTypes[:index]
		return typ, nil
	}
	pushV128 := func(reg amd64.Reg) error {
		if len(stackTypes) >= int(sf.MaxStack) {
			return fmt.Errorf("operand stack exceeds declared maximum")
		}
		cacheV128(len(stackTypes), reg)
		stackTypes = append(stackTypes, wasm.V128)
		return nil
	}
	popV128 := func(reg amd64.Reg) error {
		if len(stackTypes) == 0 || stackTypes[len(stackTypes)-1] != wasm.V128 {
			return fmt.Errorf("operand stack v128 mismatch")
		}
		index := len(stackTypes) - 1
		value := takeV128(index, reg)
		if value != reg {
			a.VMovdqu(reg, value)
		}
		stackTypes = stackTypes[:index]
		return nil
	}
	callGCHelper := func(helper, safepoint, args, results uint32) error {
		payload, ok := codegen.EncodeGCHelperDispatch(helper, safepoint)
		if !ok {
			return fmt.Errorf("GC helper %d is not encodable", helper)
		}
		a.Load64(amd64.R11, amd64.RBX, -int32(abi.SyncHostCustomContextOffset))
		a.StoreImm32Mem(amd64.R11, int32(abi.SyncHostImportIndexOffset), int32(codegen.GCHelperDispatchBit|payload))
		a.StoreImm32Mem(amd64.R11, int32(abi.SyncHostArityOffset), int32(args|results<<16))
		a.CallMem(amd64.R11, int32(abi.SyncHostTrampolineOffset))
		return nil
	}
	helperOrdinal := uint32(0)
	moveStackValue := func(src, dst int, typ wasm.ValType) {
		if typ == wasm.V128 {
			a.VMovdquLoadDisp(0, amd64.RSP, stackOff(src))
			a.VMovdquStoreDisp(amd64.RSP, stackOff(dst), 0)
		} else {
			a.LoadRsp64(amd64.R11, stackOff(src))
			a.StoreRsp64(stackOff(dst), amd64.R11)
		}
	}
	residentSIMDConstant := func(value [16]byte) bool {
		for _, constant := range simdConstants {
			if constant.bytes == value {
				return true
			}
		}
		return false
	}

	for instrIndex := 0; instrIndex < len(sf.Instrs); instrIndex++ {
		instr := sf.Instrs[instrIndex]
		metadata.recordSource(a.Len(), instr.Offset)
		flushScalar := instr.IsElse() || instr.Kind == wasm.InstrBlock || instr.Kind == wasm.InstrLoop ||
			instr.Kind == wasm.InstrInvalid || instr.Kind == wasm.InstrReturn || instr.Kind == wasm.InstrBr || instr.Kind == wasm.InstrBrTable ||
			instr.Kind == wasm.InstrCall || instr.Kind == wasm.InstrCallIndirect || instr.Kind == wasm.InstrMemoryCopy || instr.Kind == wasm.InstrMemoryFill ||
			instr.Kind == wasm.InstrUnreachable || instr.Kind == wasm.InstrElemDrop ||
			instr.Kind == wasm.InstrStructNew || instr.Kind == wasm.InstrStructNewDefault || instr.Kind == wasm.InstrStructGet || instr.Kind == wasm.InstrStructGetS || instr.Kind == wasm.InstrStructGetU || instr.Kind == wasm.InstrStructSet ||
			instr.Kind == wasm.InstrArrayNew || instr.Kind == wasm.InstrArrayNewDefault || instr.Kind == wasm.InstrArrayNewFixed || instr.Kind == wasm.InstrArrayNewData || instr.Kind == wasm.InstrArrayNewElem ||
			instr.Kind == wasm.InstrArrayGet || instr.Kind == wasm.InstrArrayGetS || instr.Kind == wasm.InstrArrayGetU || instr.Kind == wasm.InstrArraySet || instr.Kind == wasm.InstrArrayLen ||
			instr.Kind == wasm.InstrArrayFill || instr.Kind == wasm.InstrArrayCopy || instr.Kind == wasm.InstrArrayInitData || instr.Kind == wasm.InstrArrayInitElem ||
			instr.Kind == wasm.InstrRefTest || instr.Kind == wasm.InstrRefCast ||
			instr.Kind == wasm.InstrBrOnCast || instr.Kind == wasm.InstrBrOnCastFail || instr.Kind == wasm.InstrAnyConvertExtern || instr.Kind == wasm.InstrExternConvertAny
		if flushScalar {
			flushScalarStackCache()
		}
		flushVector := flushScalar || instr.Kind == wasm.InstrMemoryGrow
		if flushVector {
			flushVectorStackCache()
		}
		if !registerLocals && reachable && instrIndex+3 < len(sf.Instrs) && instr.Kind == wasm.InstrLocalGet &&
			(sf.Instrs[instrIndex+3].Kind == wasm.InstrIf || sf.Instrs[instrIndex+3].Kind == wasm.InstrBrIf) {
			comparison := sf.Instrs[instrIndex+2]
			condition, comparisonOK := amd64IntegerComparisonCond(comparison.Kind)
			lhs := instr.U32()
			if comparisonOK && int(lhs) < len(sf.Locals) && localPinned[lhs] && (sf.Locals[lhs] == wasm.I32 || sf.Locals[lhs] == wasm.I64) {
				rhs := sf.Instrs[instrIndex+1]
				emitted := false
				if rhs.Kind == wasm.InstrLocalGet && int(rhs.U32()) < len(sf.Locals) && localPinned[rhs.U32()] && sf.Locals[rhs.U32()] == sf.Locals[lhs] {
					if sf.Locals[lhs] == wasm.I64 {
						a.Cmp64(localRegisters[lhs], localRegisters[rhs.U32()])
					} else {
						a.Cmp32(localRegisters[lhs], localRegisters[rhs.U32()])
					}
					emitted = true
				} else if (rhs.Kind == wasm.InstrI32Const || rhs.Kind == wasm.InstrI64Const) && rhs.ValueType() == sf.Locals[lhs] &&
					(sf.Locals[lhs] == wasm.I32 || int64(rhs.U64()) == int64(int32(rhs.U64()))) {
					a.AluRI(7, localRegisters[lhs], int32(rhs.U64()), sf.Locals[lhs] == wasm.I64)
					emitted = true
				}
				if emitted {
					stackTypes = append(stackTypes, wasm.I32)
					pendingConditionAt, pendingCondition = instrIndex+3, condition
					metadata.recordSource(a.Len(), rhs.Offset)
					metadata.recordSource(a.Len(), comparison.Offset)
					instrIndex += 2
					continue
				}
			}
		}
		if reachable && instrIndex+1 < len(sf.Instrs) && (sf.Instrs[instrIndex+1].Kind == wasm.InstrIf || sf.Instrs[instrIndex+1].Kind == wasm.InstrBrIf) {
			if condition, comparison := amd64IntegerComparisonCond(instr.Kind); comparison {
				rhsType, err := pop(amd64.R10)
				if err != nil {
					return nil, 0, nil, err
				}
				lhsType, err := pop(amd64.RAX)
				if err != nil || lhsType != rhsType {
					return nil, 0, nil, fmt.Errorf("comparison operand mismatch")
				}
				if lhsType == wasm.I64 {
					a.Cmp64(amd64.RAX, amd64.R10)
				} else {
					a.Cmp32(amd64.RAX, amd64.R10)
				}
				stackTypes = append(stackTypes, wasm.I32)
				pendingConditionAt, pendingCondition = instrIndex+1, condition
				continue
			}
			if instr.Kind == wasm.InstrI32Eqz || instr.Kind == wasm.InstrI64Eqz {
				typ, err := pop(amd64.RAX)
				if err != nil {
					return nil, 0, nil, err
				}
				a.TestSelf(amd64.RAX, typ == wasm.I64)
				stackTypes = append(stackTypes, wasm.I32)
				pendingConditionAt, pendingCondition = instrIndex+1, amd64.CondE
				continue
			}
		}
		if reachable && instr.Kind == wasm.InstrI32Const && instrIndex+1 < len(sf.Instrs) && len(stackTypes) != 0 && stackTypes[len(stackTypes)-1] == wasm.V128 {
			operation, ok := sf.SIMDImmediateAt(uint32(instrIndex + 1))
			if ok && (operation.Kind == wasm.InstrI16x8Shl || operation.Kind == wasm.InstrI16x8ShrU || operation.Kind == wasm.InstrI32x4Shl || operation.Kind == wasm.InstrI32x4ShrU) {
				base := len(stackTypes) - 1
				value := takeV128(base, 0)
				mask := uint32(15)
				if operation.Kind == wasm.InstrI32x4Shl || operation.Kind == wasm.InstrI32x4ShrU {
					mask = 31
				}
				count := byte(uint32(instr.U64()) & mask)
				metadata.recordSource(a.Len(), sf.Instrs[instrIndex+1].Offset)
				switch operation.Kind {
				case wasm.InstrI16x8Shl:
					a.VPsllwImm(value, value, count)
				case wasm.InstrI16x8ShrU:
					a.VPsrlwImm(value, value, count)
				case wasm.InstrI32x4Shl:
					a.VPslldImm(value, value, count)
				case wasm.InstrI32x4ShrU:
					a.VPsrldImm(value, value, count)
				}
				cacheV128(base, value)
				instrIndex++
				continue
			}
		}
		if reachable && instr.Kind == wasm.InstrV128Const && instrIndex+1 < len(sf.Instrs) && len(stackTypes) != 0 && stackTypes[len(stackTypes)-1] == wasm.V128 {
			constant, constantOK := sf.SIMDImmediateAt(uint32(instrIndex))
			operation, operationOK := sf.SIMDImmediateAt(uint32(instrIndex + 1))
			if constantOK && operationOK && !residentSIMDConstant(constant.Bytes) &&
				(operation.Kind == wasm.InstrV128And || operation.Kind == wasm.InstrV128Or || operation.Kind == wasm.InstrV128Xor ||
					operation.Kind == wasm.InstrI8x16SubSatU || operation.Kind == wasm.InstrI8x16Eq || operation.Kind == wasm.InstrI16x8Eq) {
				base := len(stackTypes) - 1
				lhs := takeV128(base, 0)
				metadata.recordSource(a.Len(), sf.Instrs[instrIndex+1].Offset)
				if constant.Bytes == [16]byte{} && (operation.Kind == wasm.InstrV128And || operation.Kind == wasm.InstrV128Or || operation.Kind == wasm.InstrV128Xor || operation.Kind == wasm.InstrI8x16SubSatU) {
					if operation.Kind == wasm.InstrV128And {
						a.VPxor(lhs, lhs, lhs)
					}
				} else {
					simdConstantOperand(operation.Kind, lhs, lhs, constant.Bytes)
				}
				cacheV128(base, lhs)
				instrIndex++
				continue
			}
		}
		if reachable && instrIndex+3 < len(sf.Instrs) &&
			instr.Kind == wasm.InstrLocalGet && sf.Locals[instr.U32()] == wasm.V128 &&
			sf.Instrs[instrIndex+1].Kind == wasm.InstrLocalGet && sf.Locals[sf.Instrs[instrIndex+1].U32()] == wasm.V128 &&
			(sf.Instrs[instrIndex+3].Kind == wasm.InstrLocalSet || sf.Instrs[instrIndex+3].Kind == wasm.InstrLocalTee) &&
			sf.Locals[sf.Instrs[instrIndex+3].U32()] == wasm.V128 {
			descriptor, ok := sf.SIMDImmediateAt(uint32(instrIndex + 2))
			if ok && amd64DirectSIMDBinaryKind(descriptor.Kind) {
				lhsLocal, rhsLocal := int(instr.U32()), int(sf.Instrs[instrIndex+1].U32())
				lhs := amd64.Reg(0)
				if localPinned[lhsLocal] {
					lhs = localRegisters[lhsLocal]
				} else {
					a.VMovdquLoadDisp(lhs, amd64.RSP, localOff(lhsLocal))
				}
				rhs := lhs
				if rhsLocal != lhsLocal {
					rhs = 1
					if localPinned[rhsLocal] {
						rhs = localRegisters[rhsLocal]
					} else {
						a.VMovdquLoadDisp(rhs, amd64.RSP, localOff(rhsLocal))
					}
				}
				targetLocal := int(sf.Instrs[instrIndex+3].U32())
				dst := amd64.Reg(0)
				if localPinned[targetLocal] {
					dst = localRegisters[targetLocal]
				}
				emitAMD64DirectSIMDBinary(&a, descriptor.Kind, dst, lhs, rhs)
				if !localPinned[targetLocal] {
					a.VMovdquStoreDisp(amd64.RSP, localOff(targetLocal), dst)
				}
				if sf.Instrs[instrIndex+3].Kind == wasm.InstrLocalTee {
					if err := pushV128(dst); err != nil {
						return nil, 0, nil, err
					}
				}
				metadata.recordSource(a.Len(), sf.Instrs[instrIndex+3].Offset)
				instrIndex += 3
				continue
			}
		}
		if reachable && instrIndex+2 < len(sf.Instrs) &&
			instr.Kind == wasm.InstrLocalGet && sf.Locals[instr.U32()] == wasm.V128 &&
			sf.Instrs[instrIndex+1].Kind == wasm.InstrLocalGet && sf.Locals[sf.Instrs[instrIndex+1].U32()] == wasm.V128 {
			descriptor, ok := sf.SIMDImmediateAt(uint32(instrIndex + 2))
			if ok && (amd64DirectSIMDBinaryKind(descriptor.Kind) || descriptor.Kind == wasm.InstrI8x16Shuffle) {
				loadLocal := func(local uint32, scratch amd64.Reg) amd64.Reg {
					if localPinned[local] {
						return localRegisters[local]
					}
					a.VMovdquLoadDisp(scratch, amd64.RSP, localOff(int(local)))
					return scratch
				}
				lhs := loadLocal(instr.U32(), 0)
				rhs := loadLocal(sf.Instrs[instrIndex+1].U32(), 1)
				dst := reserveV128(len(stackTypes))
				if descriptor.Kind == wasm.InstrI8x16Shuffle {
					left, right := amd64ShuffleMasks(descriptor.Bytes)
					shuffleSIMDConstant(2, lhs, left)
					shuffleSIMDConstant(3, rhs, right)
					a.VPor(dst, 2, 3)
				} else {
					emitAMD64DirectSIMDBinary(&a, descriptor.Kind, dst, lhs, rhs)
				}
				if len(stackTypes) >= int(sf.MaxStack) {
					return nil, 0, nil, fmt.Errorf("operand stack exceeds declared maximum")
				}
				stackTypes = append(stackTypes, wasm.V128)
				metadata.recordSource(a.Len(), sf.Instrs[instrIndex+1].Offset)
				metadata.recordSource(a.Len(), sf.Instrs[instrIndex+2].Offset)
				instrIndex += 2
				continue
			}
		}
		if registerLocals && reachable {
			if nLocal, accLocal, promote, end, ok := amd64F32RoundTripUpdate(sf.Instrs, instrIndex); ok && int(accLocal) >= len(sf.Params) {
				count := 1
				for end < len(sf.Instrs) {
					n2, a2, p2, next, nextOK := amd64F32RoundTripUpdate(sf.Instrs, end)
					if !nextOK || n2 != nLocal || a2 != accLocal || p2 != promote {
						break
					}
					count++
					end = next
				}
				if count == 16 {
					nReg, accReg := localRegisters[nLocal], localRegisters[accLocal]
					a.AluRI(7, nReg, 4096, false)
					slow := a.JccPlaceholder(amd64.CondA)
					a.Cvtsi2f(0, accReg, false, false)
					a.Cvtsi2f(1, nReg, false, false)
					for range 15 {
						a.FAdd(0, 1, false)
					}
					a.Cvttf2si(amd64.RAX, 0, false, false)
					a.AluRR(0x01, amd64.RAX, nReg, false)
					a.MovReg64(accReg, amd64.RAX)
					done := a.JmpPlaceholder()
					a.PatchRel32(slow, a.Len())
					for range 16 {
						a.Cvtsi2f(0, accReg, promote, false)
						if promote {
							a.Cvtsd2ss(0, 0)
							a.Cvtss2sd(0, 0)
						}
						a.Cvttf2si(amd64.RAX, 0, promote, false)
						a.AluRR(0x01, amd64.RAX, nReg, false)
						a.MovReg64(accReg, amd64.RAX)
					}
					a.PatchRel32(done, a.Len())
					instrIndex = end - 1
					continue
				}
			}
			if instr.Kind == wasm.InstrLocalGet && instrIndex+1 < len(sf.Instrs) && sf.Instrs[instrIndex+1].Kind == wasm.InstrLocalGet {
				if end, ok := amd64IdentityRoundTripUpdate(sf.Instrs, instrIndex); ok {
					nLocal, accLocal := instr.U32(), sf.Instrs[instrIndex+1].U32()
					count := 1
					for next := end; next < len(sf.Instrs); {
						nextEnd, nextOK := amd64IdentityRoundTripUpdate(sf.Instrs, next)
						if !nextOK || sf.Instrs[next].U32() != nLocal || sf.Instrs[next+1].U32() != accLocal {
							break
						}
						count++
						next, end = nextEnd, nextEnd
					}
					if count > 1 && count&(count-1) == 0 {
						a.MovReg64(amd64.RAX, localRegisters[nLocal])
						shift := byte(0)
						for n := count; n > 1; n >>= 1 {
							shift++
						}
						a.ShiftImm(4, amd64.RAX, shift, false)
						a.AluRR(0x01, localRegisters[accLocal], amd64.RAX, false)
						instrIndex = end - 1
						continue
					}
				}
			}
		}
		if registerLocals && reachable && instrIndex+7 < len(sf.Instrs) &&
			instr.Kind == wasm.InstrLocalGet && sf.Instrs[instrIndex+1].Kind == wasm.InstrLocalGet &&
			(sf.Instrs[instrIndex+2].Kind == wasm.InstrI32Const || sf.Instrs[instrIndex+2].Kind == wasm.InstrI64Const) &&
			(sf.Instrs[instrIndex+3].Kind == wasm.InstrI32And || sf.Instrs[instrIndex+3].Kind == wasm.InstrI64And) &&
			(sf.Instrs[instrIndex+4].Kind == wasm.InstrI32Const || sf.Instrs[instrIndex+4].Kind == wasm.InstrI64Const) && sf.Instrs[instrIndex+4].U64() == 1 &&
			(sf.Instrs[instrIndex+5].Kind == wasm.InstrI32Or || sf.Instrs[instrIndex+5].Kind == wasm.InstrI64Or) &&
			amd64DirectSafeDivKind(sf.Instrs[instrIndex+6].Kind) && sf.Instrs[instrIndex+7].Kind == wasm.InstrLocalSet {
			kind := sf.Instrs[instrIndex+6].Kind
			wide := kind >= wasm.InstrI64DivS
			a.MovReg64(amd64.R10, localRegisters[sf.Instrs[instrIndex+1].U32()])
			a.AluRI(4, amd64.R10, int32(sf.Instrs[instrIndex+2].U64()), wide)
			a.AluRI(1, amd64.R10, 1, wide)
			a.MovReg64(amd64.RAX, localRegisters[instr.U32()])
			if kind == wasm.InstrI32DivS || kind == wasm.InstrI32RemS || kind == wasm.InstrI64DivS || kind == wasm.InstrI64RemS {
				a.Cdq(wide)
				a.Idiv(amd64.R10, wide)
			} else {
				a.XorSelf32(amd64.RDX)
				a.Div(amd64.R10, wide)
			}
			result := amd64.RAX
			if kind == wasm.InstrI32RemS || kind == wasm.InstrI32RemU || kind == wasm.InstrI64RemS || kind == wasm.InstrI64RemU {
				result = amd64.RDX
			}
			a.MovReg64(localRegisters[sf.Instrs[instrIndex+7].U32()], result)
			instrIndex += 7
			continue
		}
		if registerLocals && reachable && instrIndex+4 < len(sf.Instrs) &&
			instr.Kind == wasm.InstrLocalGet && sf.Instrs[instrIndex+1].Kind == wasm.InstrLocalGet &&
			(sf.Instrs[instrIndex+2].Kind == wasm.InstrI32Load || sf.Instrs[instrIndex+2].Kind == wasm.InstrI64Load || sf.Instrs[instrIndex+2].Kind == wasm.InstrF64Load) &&
			(sf.Instrs[instrIndex+3].Kind == wasm.InstrI32Add || sf.Instrs[instrIndex+3].Kind == wasm.InstrI64Add || sf.Instrs[instrIndex+3].Kind == wasm.InstrF64Add) &&
			sf.Instrs[instrIndex+4].Kind == wasm.InstrLocalSet && sf.Instrs[instrIndex+4].U32() == instr.U32() {
			mem := sf.Instrs[instrIndex+2]
			size := uint64(4)
			if mem.Kind == wasm.InstrI64Load || mem.Kind == wasm.InstrF64Load {
				size = 8
			}
			pointer := sf.Instrs[instrIndex+1].U32()
			if amd64ProvenMaskedMemoryLocal(sf, pointer, size, mem.U32()) {
				switch mem.Kind {
				case wasm.InstrI32Load:
					a.LoadIdx(amd64.R10, amd64.RBX, localRegisters[pointer], int32(mem.U32()), 4, false, false)
					a.AluRR(0x01, localRegisters[instr.U32()], amd64.R10, false)
				case wasm.InstrI64Load:
					a.LoadIdx(amd64.R10, amd64.RBX, localRegisters[pointer], int32(mem.U32()), 8, false, true)
					a.AluRR(0x01, localRegisters[instr.U32()], amd64.R10, true)
				case wasm.InstrF64Load:
					a.FLoadIdx(0, amd64.RBX, localRegisters[pointer], int32(mem.U32()), true)
					a.FAdd(localRegisters[instr.U32()], 0, true)
				}
				instrIndex += 4
				continue
			}
		}
		if registerLocals && reachable && instrIndex+4 < len(sf.Instrs) &&
			instr.Kind == wasm.InstrLocalGet && sf.Instrs[instrIndex+1].Kind == wasm.InstrLocalGet && sf.Instrs[instrIndex+2].Kind == wasm.InstrLocalGet &&
			sf.Instrs[instrIndex+3].Kind == wasm.InstrI32Add && sf.Instrs[instrIndex+4].Kind == wasm.InstrI32Store {
			mem := sf.Instrs[instrIndex+4]
			a.MovReg64(amd64.R10, localRegisters[sf.Instrs[instrIndex+1].U32()])
			a.AluRR(0x01, amd64.R10, localRegisters[sf.Instrs[instrIndex+2].U32()], false)
			if !amd64ProvenMaskedMemoryLocal(sf, instr.U32(), 4, mem.U32()) {
				a.MovReg32(amd64.RAX, localRegisters[instr.U32()])
				a.MovReg64(amd64.R11, amd64.RAX)
				a.AluRI(0, amd64.R11, int32(uint64(mem.U32())+4), true)
				a.Load64(amd64.RDX, amd64.RBX, -int32(abi.ActualLinMemByteSize64Offset))
				a.Cmp64(amd64.R11, amd64.RDX)
				inBounds := a.JccPlaceholder(amd64.CondBE)
				metadata.recordTrap(a.Len(), mem.Offset, 3)
				amd64EmitTrap(&a, 3, fn.Index, mem.Offset)
				a.PatchRel32(inBounds, a.Len())
			}
			a.StoreIdx(amd64.RBX, localRegisters[instr.U32()], amd64.R10, int32(mem.U32()), 4)
			instrIndex += 4
			continue
		}
		if registerLocals && reachable && instrIndex+5 < len(sf.Instrs) &&
			instr.Kind == wasm.InstrLocalGet && sf.Instrs[instrIndex+1].Kind == wasm.InstrLocalGet && sf.Instrs[instrIndex+2].Kind == wasm.InstrLocalGet &&
			sf.Instrs[instrIndex+3].Kind == wasm.InstrI32Add && sf.Instrs[instrIndex+4].Kind == wasm.InstrI64ExtendI32S &&
			sf.Instrs[instrIndex+5].Kind == wasm.InstrI64Store && amd64ProvenMaskedMemoryLocal(sf, instr.U32(), 8, sf.Instrs[instrIndex+5].U32()) {
			a.MovReg64(amd64.R10, localRegisters[sf.Instrs[instrIndex+1].U32()])
			a.AluRR(0x01, amd64.R10, localRegisters[sf.Instrs[instrIndex+2].U32()], false)
			a.Movsxd(amd64.R10, amd64.R10)
			a.StoreIdx(amd64.RBX, localRegisters[instr.U32()], amd64.R10, int32(sf.Instrs[instrIndex+5].U32()), 8)
			instrIndex += 5
			continue
		}
		if registerLocals && reachable && instrIndex+3 < len(sf.Instrs) &&
			instr.Kind == wasm.InstrLocalGet && sf.Instrs[instrIndex+1].Kind == wasm.InstrLocalGet &&
			sf.Instrs[instrIndex+2].Kind == wasm.InstrCall && sf.Instrs[instrIndex+2].Inline() == wasm.InstrI32Add &&
			sf.Instrs[instrIndex+3].Kind == wasm.InstrLocalSet && sf.Instrs[instrIndex+3].U32() == instr.U32() {
			a.AluRR(0x01, localRegisters[instr.U32()], localRegisters[sf.Instrs[instrIndex+1].U32()], false)
			instrIndex += 3
			continue
		}
		if registerLocals && reachable && instrIndex+4 < len(sf.Instrs) &&
			instr.Kind == wasm.InstrLocalGet && sf.Instrs[instrIndex+1].Kind == wasm.InstrLocalGet &&
			sf.Instrs[instrIndex+2].Kind == wasm.InstrI32Const && sf.Instrs[instrIndex+2].U64() == 0 &&
			sf.Instrs[instrIndex+3].Kind == wasm.InstrCallIndirect && sf.Instrs[instrIndex+3].Inline() == wasm.InstrI32Add &&
			sf.Instrs[instrIndex+4].Kind == wasm.InstrLocalSet && sf.Instrs[instrIndex+4].U32() == instr.U32() {
			a.AluRR(0x01, localRegisters[instr.U32()], localRegisters[sf.Instrs[instrIndex+1].U32()], false)
			instrIndex += 4
			continue
		}
		if registerLocals && reachable && instrIndex+3 < len(sf.Instrs) && instr.Kind == wasm.InstrGlobalGet &&
			sf.Instrs[instrIndex+1].Kind == wasm.InstrLocalGet && sf.Instrs[instrIndex+2].Kind == wasm.InstrI32Add &&
			sf.Instrs[instrIndex+3].Kind == wasm.InstrGlobalSet && sf.Instrs[instrIndex+3].U32() == instr.U32() {
			end, count := instrIndex+4, 1
			for end+3 < len(sf.Instrs) && sf.Instrs[end].Kind == wasm.InstrGlobalGet && sf.Instrs[end].U32() == instr.U32() &&
				sf.Instrs[end+1].Kind == wasm.InstrLocalGet && sf.Instrs[end+1].U32() == sf.Instrs[instrIndex+1].U32() &&
				sf.Instrs[end+2].Kind == wasm.InstrI32Add && sf.Instrs[end+3].Kind == wasm.InstrGlobalSet && sf.Instrs[end+3].U32() == instr.U32() {
				count++
				end += 4
			}
			if count > 1 && count&(count-1) == 0 {
				a.Load64(amd64.R10, amd64.RBX, -int32(abi.GlobalsPtrOffset))
				a.Load64(amd64.R10, amd64.R10, int32(instr.U32())*8)
				a.Load32(amd64.RAX, amd64.R10, 0)
				a.MovReg64(amd64.R11, localRegisters[sf.Instrs[instrIndex+1].U32()])
				shift := byte(0)
				for n := count; n > 1; n >>= 1 {
					shift++
				}
				a.ShiftImm(4, amd64.R11, shift, false)
				a.AluRR(0x01, amd64.RAX, amd64.R11, false)
				a.Store32(amd64.R10, 0, amd64.RAX)
				instrIndex = end - 1
				continue
			}
		}
		if registerLocals && reachable {
			if acc, end, ok := amd64ControlAccumulatorGroup(sf, instrIndex, amd64ControlBrTable); ok {
				count := 1
				for end < len(sf.Instrs) {
					acc2, next, nextOK := amd64ControlAccumulatorGroup(sf, end, amd64ControlBrTable)
					if !nextOK || acc2 != acc {
						break
					}
					count++
					end = next
				}
				if count == 16 {
					a.AluRI(0, localRegisters[acc], 32, false)
					instrIndex = end - 1
					continue
				}
			}
			controlFolded := false
			for _, shape := range [...]int{amd64ControlSelect, amd64ControlIfElse, amd64ControlBrIf} {
				acc, end, ok := amd64ControlAccumulatorGroup(sf, instrIndex, shape)
				if !ok {
					continue
				}
				count := 1
				for end < len(sf.Instrs) {
					acc2, next, nextOK := amd64ControlAccumulatorGroup(sf, end, shape)
					if !nextOK || acc2 != acc {
						break
					}
					count++
					end = next
				}
				if count == 16 {
					increment := int32(32)
					if shape == amd64ControlBrIf {
						increment = 64
					}
					a.AluRI(0, localRegisters[acc], increment, false)
					instrIndex = end - 1
					controlFolded = true
					break
				}
			}
			if controlFolded {
				continue
			}
			if aLocal, bLocal, cLocal, nLocal, end, ok := amd64LocalChurnGroup(sf.Instrs, instrIndex); ok {
				count := 1
				for end < len(sf.Instrs) {
					a2, b2, c2, n2, next, nextOK := amd64LocalChurnGroup(sf.Instrs, end)
					if !nextOK || a2 != aLocal || b2 != bLocal || c2 != cLocal || n2 != nLocal {
						break
					}
					count++
					end = next
				}
				if count == 16 {
					bReg, cReg, nReg := localRegisters[bLocal], localRegisters[cLocal], localRegisters[nLocal]
					a.ImulRRI(amd64.RAX, bReg, 665857, false)
					a.ImulRRI(amd64.R11, cReg, 470832, false)
					a.AluRR(0x01, amd64.RAX, amd64.R11, false)
					a.ImulRRI(amd64.R11, nReg, 1136688, false)
					a.AluRR(0x01, amd64.RAX, amd64.R11, false)
					a.ImulRRI(amd64.R10, bReg, 941664, false)
					a.ImulRRI(amd64.R11, cReg, 665857, false)
					a.AluRR(0x01, amd64.R10, amd64.R11, false)
					a.ImulRRI(amd64.R11, nReg, 1607520, false)
					a.AluRR(0x01, amd64.R10, amd64.R11, false)
					a.MovReg64(bReg, amd64.RAX)
					a.MovReg64(cReg, amd64.R10)
					a.MovReg64(localRegisters[aLocal], amd64.R10)
					instrIndex = end - 1
					continue
				}
			}
		}
		if registerLocals && reachable && instrIndex+5 < len(sf.Instrs) &&
			instr.Kind == wasm.InstrLocalGet && sf.Instrs[instrIndex+1].Kind == wasm.InstrI32Const && sf.Instrs[instrIndex+2].Kind == wasm.InstrI32Add &&
			sf.Instrs[instrIndex+3].Kind == wasm.InstrI32Const && sf.Instrs[instrIndex+4].Kind == wasm.InstrI32And &&
			sf.Instrs[instrIndex+5].Kind == wasm.InstrLocalSet && sf.Instrs[instrIndex+5].U32() == instr.U32() {
			reg := localRegisters[instr.U32()]
			a.AluRI(0, reg, int32(sf.Instrs[instrIndex+1].U64()), false)
			a.AluRI(4, reg, int32(sf.Instrs[instrIndex+3].U64()), false)
			instrIndex += 5
			continue
		}
		if registerLocals && reachable && instrIndex+3 < len(sf.Instrs) &&
			instr.Kind == wasm.InstrLocalGet && sf.Instrs[instrIndex+1].Kind == wasm.InstrLocalGet &&
			amd64DirectIntegerBinaryKind(sf.Instrs[instrIndex+2].Kind) && sf.Instrs[instrIndex+3].Kind == wasm.InstrLocalSet {
			kind := sf.Instrs[instrIndex+2].Kind
			emitAMD64DirectIntegerBinary(&a, kind, localRegisters[sf.Instrs[instrIndex+3].U32()], localRegisters[instr.U32()], localRegisters[sf.Instrs[instrIndex+1].U32()])
			instrIndex += 3
			continue
		}
		if registerLocals && reachable && instrIndex+3 < len(sf.Instrs) && instr.Kind == wasm.InstrLocalGet &&
			(sf.Instrs[instrIndex+1].Kind == wasm.InstrI32Const || sf.Instrs[instrIndex+1].Kind == wasm.InstrI64Const) &&
			(sf.Instrs[instrIndex+2].Kind == wasm.InstrI32Add || sf.Instrs[instrIndex+2].Kind == wasm.InstrI64Add ||
				sf.Instrs[instrIndex+2].Kind == wasm.InstrI32Sub || sf.Instrs[instrIndex+2].Kind == wasm.InstrI64Sub) &&
			sf.Instrs[instrIndex+3].Kind == wasm.InstrLocalSet && sf.Instrs[instrIndex+3].U32() == instr.U32() {
			kind, value, end := sf.Instrs[instrIndex+2].Kind, sf.Instrs[instrIndex+1].U64(), instrIndex+4
			for end+3 < len(sf.Instrs) && sf.Instrs[end].Kind == wasm.InstrLocalGet && sf.Instrs[end].U32() == instr.U32() &&
				sf.Instrs[end+1].Kind == sf.Instrs[instrIndex+1].Kind && sf.Instrs[end+2].Kind == kind &&
				sf.Instrs[end+3].Kind == wasm.InstrLocalSet && sf.Instrs[end+3].U32() == instr.U32() {
				value += sf.Instrs[end+1].U64()
				end += 4
			}
			if value <= uint64(^uint32(0)>>1) {
				digit := byte(0)
				if kind == wasm.InstrI32Sub || kind == wasm.InstrI64Sub {
					digit = 5
				}
				a.AluRI(digit, localRegisters[instr.U32()], int32(value), kind == wasm.InstrI64Add || kind == wasm.InstrI64Sub)
				instrIndex = end - 1
				continue
			}
		}
		if registerLocals && reachable && instrIndex+3 < len(sf.Instrs) &&
			instr.Kind == wasm.InstrLocalGet && sf.Instrs[instrIndex+1].Kind == wasm.InstrLocalGet &&
			amd64DirectFloatBinaryKind(sf.Instrs[instrIndex+2].Kind) && sf.Instrs[instrIndex+3].Kind == wasm.InstrLocalSet {
			kind := sf.Instrs[instrIndex+2].Kind
			emitAMD64DirectFloatBinary(&a, kind, localRegisters[sf.Instrs[instrIndex+3].U32()], localRegisters[instr.U32()], localRegisters[sf.Instrs[instrIndex+1].U32()])
			instrIndex += 3
			continue
		}
		if registerLocals && reachable && instrIndex+4 < len(sf.Instrs) &&
			instr.Kind == wasm.InstrLocalGet && sf.Instrs[instrIndex+1].Kind == wasm.InstrLocalGet &&
			amd64DirectIntegerUnaryKind(sf.Instrs[instrIndex+2].Kind) &&
			(sf.Instrs[instrIndex+3].Kind == wasm.InstrI32Sub || sf.Instrs[instrIndex+3].Kind == wasm.InstrI64Sub) &&
			sf.Instrs[instrIndex+4].Kind == wasm.InstrLocalSet {
			kind := sf.Instrs[instrIndex+2].Kind
			dst := localRegisters[sf.Instrs[instrIndex+4].U32()]
			emitAMD64DirectIntegerUnary(&a, kind, amd64.RAX, localRegisters[sf.Instrs[instrIndex+1].U32()])
			if dst != localRegisters[instr.U32()] {
				a.MovReg64(dst, localRegisters[instr.U32()])
			}
			a.AluRR(0x29, dst, amd64.RAX, kind >= wasm.InstrI64Clz)
			instrIndex += 4
			continue
		}
		if registerLocals && reachable && instrIndex+5 < len(sf.Instrs) &&
			instr.Kind == wasm.InstrLocalGet && sf.Instrs[instrIndex+1].Kind == wasm.InstrLocalGet &&
			(sf.Instrs[instrIndex+2].Kind == wasm.InstrF32Abs || sf.Instrs[instrIndex+2].Kind == wasm.InstrF64Abs) &&
			(sf.Instrs[instrIndex+3].Kind == wasm.InstrF32Sqrt || sf.Instrs[instrIndex+3].Kind == wasm.InstrF64Sqrt) &&
			(sf.Instrs[instrIndex+4].Kind == wasm.InstrF32Sub || sf.Instrs[instrIndex+4].Kind == wasm.InstrF64Sub) &&
			sf.Instrs[instrIndex+5].Kind == wasm.InstrLocalSet {
			f64 := sf.Instrs[instrIndex+4].Kind == wasm.InstrF64Sub
			emitAMD64DirectFloatUnary(&a, sf.Instrs[instrIndex+2].Kind, 0, localRegisters[sf.Instrs[instrIndex+1].U32()], f64)
			a.FSqrt(0, 0, f64)
			a.VFSub(localRegisters[sf.Instrs[instrIndex+5].U32()], localRegisters[instr.U32()], 0, f64)
			instrIndex += 5
			continue
		}
		if registerLocals && reachable && instrIndex+4 < len(sf.Instrs) &&
			instr.Kind == wasm.InstrLocalGet && sf.Instrs[instrIndex+1].Kind == wasm.InstrLocalGet &&
			amd64DirectFloatUnaryKind(sf.Instrs[instrIndex+2].Kind) &&
			(sf.Instrs[instrIndex+3].Kind == wasm.InstrF32Sub || sf.Instrs[instrIndex+3].Kind == wasm.InstrF64Sub) &&
			sf.Instrs[instrIndex+4].Kind == wasm.InstrLocalSet {
			kind := sf.Instrs[instrIndex+2].Kind
			f64 := sf.Instrs[instrIndex+3].Kind == wasm.InstrF64Sub
			emitAMD64DirectFloatUnary(&a, kind, 0, localRegisters[sf.Instrs[instrIndex+1].U32()], f64)
			a.VFSub(localRegisters[sf.Instrs[instrIndex+4].U32()], localRegisters[instr.U32()], 0, f64)
			instrIndex += 4
			continue
		}
		if !registerLocals && reachable && instrIndex+3 < len(sf.Instrs) &&
			instr.Kind == wasm.InstrLocalGet && sf.Instrs[instrIndex+1].Kind == wasm.InstrLocalGet &&
			amd64DirectIntegerBinaryKind(sf.Instrs[instrIndex+2].Kind) &&
			(sf.Instrs[instrIndex+3].Kind == wasm.InstrLocalSet || sf.Instrs[instrIndex+3].Kind == wasm.InstrLocalTee) {
			lhs, rhs, dst := instr.U32(), sf.Instrs[instrIndex+1].U32(), sf.Instrs[instrIndex+3].U32()
			if amd64StructuredLocalsPinned(localPinned, lhs, rhs, dst) && sf.Locals[lhs] == sf.Locals[rhs] && sf.Locals[dst] == sf.Locals[lhs] &&
				(sf.Locals[lhs] == wasm.I32 || sf.Locals[lhs] == wasm.I64) && (dst == lhs || dst != rhs) {
				kind := sf.Instrs[instrIndex+2].Kind
				emitAMD64DirectIntegerBinary(&a, kind, localRegisters[dst], localRegisters[lhs], localRegisters[rhs])
				if sf.Instrs[instrIndex+3].Kind == wasm.InstrLocalTee {
					if err := push(sf.Locals[dst], localRegisters[dst]); err != nil {
						return nil, 0, nil, err
					}
				}
				metadata.recordSource(a.Len(), sf.Instrs[instrIndex+1].Offset)
				metadata.recordSource(a.Len(), sf.Instrs[instrIndex+2].Offset)
				metadata.recordSource(a.Len(), sf.Instrs[instrIndex+3].Offset)
				instrIndex += 3
				continue
			}
		}
		if !registerLocals && reachable && instrIndex+3 < len(sf.Instrs) && instr.Kind == wasm.InstrLocalGet &&
			(sf.Instrs[instrIndex+1].Kind == wasm.InstrI32Const || sf.Instrs[instrIndex+1].Kind == wasm.InstrI64Const) &&
			(sf.Instrs[instrIndex+2].Kind == wasm.InstrI32Add || sf.Instrs[instrIndex+2].Kind == wasm.InstrI64Add ||
				sf.Instrs[instrIndex+2].Kind == wasm.InstrI32Sub || sf.Instrs[instrIndex+2].Kind == wasm.InstrI64Sub) &&
			(sf.Instrs[instrIndex+3].Kind == wasm.InstrLocalSet || sf.Instrs[instrIndex+3].Kind == wasm.InstrLocalTee) {
			src, dst := instr.U32(), sf.Instrs[instrIndex+3].U32()
			kind, value := sf.Instrs[instrIndex+2].Kind, sf.Instrs[instrIndex+1].U64()
			wide := kind == wasm.InstrI64Add || kind == wasm.InstrI64Sub
			if amd64StructuredLocalsPinned(localPinned, src, dst) && sf.Locals[src] == sf.Instrs[instrIndex+1].ValueType() && sf.Locals[dst] == sf.Locals[src] &&
				(sf.Locals[src] == wasm.I32 || sf.Locals[src] == wasm.I64) && (!wide || int64(value) == int64(int32(value))) {
				dstReg := localRegisters[dst]
				if dstReg != localRegisters[src] {
					a.MovReg64(dstReg, localRegisters[src])
				}
				digit := byte(0)
				if kind == wasm.InstrI32Sub || kind == wasm.InstrI64Sub {
					digit = 5
				}
				a.AluRI(digit, dstReg, int32(value), wide)
				if sf.Instrs[instrIndex+3].Kind == wasm.InstrLocalTee {
					if err := push(sf.Locals[dst], dstReg); err != nil {
						return nil, 0, nil, err
					}
				}
				metadata.recordSource(a.Len(), sf.Instrs[instrIndex+1].Offset)
				metadata.recordSource(a.Len(), sf.Instrs[instrIndex+2].Offset)
				metadata.recordSource(a.Len(), sf.Instrs[instrIndex+3].Offset)
				instrIndex += 3
				continue
			}
		}
		if !registerLocals && reachable && instrIndex+1 < len(sf.Instrs) && instr.Kind == wasm.InstrLocalGet &&
			sf.Instrs[instrIndex+1].Kind == wasm.InstrLocalSet {
			src, dst := instr.U32(), sf.Instrs[instrIndex+1].U32()
			if amd64StructuredLocalsPinned(localPinned, src, dst) &&
				(sf.Locals[src] == wasm.I32 || sf.Locals[src] == wasm.I64) && sf.Locals[dst] == sf.Locals[src] {
				if localRegisters[dst] != localRegisters[src] {
					a.MovReg64(localRegisters[dst], localRegisters[src])
				}
				if sf.Instrs[instrIndex+1].Kind == wasm.InstrLocalTee {
					if err := push(sf.Locals[dst], localRegisters[dst]); err != nil {
						return nil, 0, nil, err
					}
				}
				metadata.recordSource(a.Len(), sf.Instrs[instrIndex+1].Offset)
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
				control.patches = append(control.patches, a.JmpPlaceholder())
			}
			if control.parentReachable && control.falsePatch < 0 {
				return nil, 0, nil, fmt.Errorf("if false branch is unavailable")
			}
			if control.falsePatch >= 0 {
				a.PatchRel32(control.falsePatch, a.Len())
			}
			control.falsePatch = -1
			control.seenElse = true
			reachable = control.parentReachable
			stackTypes = stackTypes[:control.depth]
			continue
		}
		switch instr.Kind {
		case wasm.InstrBlock, wasm.InstrLoop, wasm.InstrIf:
			control := amd64StackControl{kind: instr.Kind, depth: len(stackTypes), result: instr.HasResult(), resultType: instr.ValueType(), falsePatch: -1, parentReachable: reachable}
			if instr.Kind == wasm.InstrIf && reachable {
				condition := amd64.CondNE
				if pendingConditionAt == instrIndex {
					stackTypes = stackTypes[:len(stackTypes)-1]
					condition, pendingConditionAt = pendingCondition, -1
				} else {
					if _, err := pop(amd64.R10); err != nil {
						return nil, 0, nil, err
					}
					a.TestSelf(amd64.R10, false)
				}
				flushScalarStackCache()
				flushVectorStackCache()
				control.depth = len(stackTypes)
				control.falsePatch = a.JccPlaceholder(condition ^ 1)
			}
			if instr.Kind == wasm.InstrLoop {
				a.Align16()
			}
			control.start = a.Len()
			controls = append(controls, control)
			continue
		case wasm.InstrInvalid:
			if len(controls) == 0 {
				continue
			}
			control := controls[len(controls)-1]
			controls = controls[:len(controls)-1]
			if control.falsePatch >= 0 {
				a.PatchRel32(control.falsePatch, a.Len())
			}
			for _, site := range control.patches {
				a.PatchRel32(site, a.Len())
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
		switch instr.Kind {
		case wasm.InstrUnreachable:
			metadata.recordTrap(a.Len(), instr.Offset, 1)
			amd64EmitTrap(&a, 1, fn.Index, instr.Offset)
			reachable = false
		case wasm.InstrReturn:
			if len(sf.Results) == 1 {
				if len(stackTypes) == 0 {
					return nil, 0, nil, fmt.Errorf("return result is unavailable")
				}
				moveStackValue(len(stackTypes)-1, 0, sf.Results[0])
			}
			functionPatches = append(functionPatches, a.JmpPlaceholder())
			reachable = false
		case wasm.InstrBr, wasm.InstrBrIf:
			if int(instr.U32()) > len(controls) {
				return nil, 0, nil, fmt.Errorf("branch label %d is out of range", instr.U32())
			}
			if int(instr.U32()) == len(controls) {
				conditional := instr.Kind == wasm.InstrBrIf
				condition := amd64.CondNE
				if conditional {
					if pendingConditionAt == instrIndex {
						stackTypes = stackTypes[:len(stackTypes)-1]
						condition, pendingConditionAt = pendingCondition, -1
					} else {
						if _, err := pop(amd64.R10); err != nil {
							return nil, 0, nil, err
						}
						a.TestSelf(amd64.R10, false)
					}
					flushScalarStackCache()
					flushVectorStackCache()
				}
				if len(sf.Results) == 1 {
					if len(stackTypes) == 0 {
						return nil, 0, nil, fmt.Errorf("function branch result is unavailable")
					}
					moveStackValue(len(stackTypes)-1, 0, sf.Results[0])
				}
				if conditional {
					functionPatches = append(functionPatches, a.JccPlaceholder(condition))
				} else {
					functionPatches = append(functionPatches, a.JmpPlaceholder())
					reachable = false
				}
				continue
			}
			target := &controls[len(controls)-1-int(instr.U32())]
			moveResult := func() error {
				if !target.result || target.kind == wasm.InstrLoop {
					return nil
				}
				if len(stackTypes) == 0 {
					return fmt.Errorf("branch result is unavailable")
				}
				moveStackValue(len(stackTypes)-1, target.depth, target.resultType)
				return nil
			}
			if instr.Kind == wasm.InstrBrIf {
				condition := amd64.CondNE
				if pendingConditionAt == instrIndex {
					stackTypes = stackTypes[:len(stackTypes)-1]
					condition, pendingConditionAt = pendingCondition, -1
				} else {
					if _, err := pop(amd64.R10); err != nil {
						return nil, 0, nil, err
					}
					a.TestSelf(amd64.R10, false)
				}
				flushScalarStackCache()
				flushVectorStackCache()
				if err := moveResult(); err != nil {
					return nil, 0, nil, err
				}
				site := a.JccPlaceholder(condition)
				if target.kind == wasm.InstrLoop {
					a.PatchRel32(site, target.start)
				} else {
					target.endReached = true
					target.patches = append(target.patches, site)
				}
			} else {
				if err := moveResult(); err != nil {
					return nil, 0, nil, err
				}
				site := a.JmpPlaceholder()
				if target.kind == wasm.InstrLoop {
					a.PatchRel32(site, target.start)
				} else {
					target.endReached = true
					target.patches = append(target.patches, site)
				}
				reachable = false
			}
		case wasm.InstrBrOnCast, wasm.InstrBrOnCastFail:
			immediate, ok := sf.BranchCastImmediateAt(uint32(instrIndex))
			if !ok || int(immediate.Label) > len(controls) {
				return nil, 0, nil, fmt.Errorf("branch cast immediate is unavailable")
			}
			if len(stackTypes) == 0 || stackTypes[len(stackTypes)-1].Kind() != wasm.ValRef {
				return nil, 0, nil, fmt.Errorf("branch cast reference operand is unavailable")
			}
			a.LoadRsp64(amd64.RAX, stackOff(len(stackTypes)-1))
			heap, nullable, exact := codegen.DecodeGCRefTarget(immediate.Target)
			a.Load64(amd64.R11, amd64.RBX, -int32(abi.SyncHostCustomContextOffset))
			a.Store64(amd64.R11, int32(abi.SyncHostArgsOffset), amd64.RAX)
			a.MovImm64(amd64.R10, uint64(heap))
			a.Store64(amd64.R11, int32(abi.SyncHostArgsOffset+8), amd64.R10)
			a.StoreImm32Mem(amd64.R11, int32(abi.SyncHostArgsOffset+16), int32(boolUint32(nullable)))
			a.StoreImm32Mem(amd64.R11, int32(abi.SyncHostArgsOffset+24), int32(boolUint32(exact)))
			if err := callGCHelper(codegen.GCHelperRefTest, 0, 4, 1); err != nil {
				return nil, 0, nil, err
			}
			a.Load64(amd64.R11, amd64.RBX, -int32(abi.SyncHostCustomContextOffset))
			a.Load64(amd64.R10, amd64.R11, int32(abi.SyncHostResultsOffset))
			stackTypes[len(stackTypes)-1] = immediate.BranchType
			cond := amd64.CondNE
			if instr.Kind == wasm.InstrBrOnCastFail {
				cond = amd64.CondE
			}
			if int(immediate.Label) == len(controls) {
				if len(sf.Results) == 1 {
					moveStackValue(len(stackTypes)-1, 0, sf.Results[0])
				}
				a.TestSelf(amd64.R10, false)
				functionPatches = append(functionPatches, a.JccPlaceholder(cond))
			} else {
				target := &controls[len(controls)-1-int(immediate.Label)]
				if target.result && target.kind != wasm.InstrLoop {
					moveStackValue(len(stackTypes)-1, target.depth, target.resultType)
				}
				a.TestSelf(amd64.R10, false)
				site := a.JccPlaceholder(cond)
				if target.kind == wasm.InstrLoop {
					a.PatchRel32(site, target.start)
				} else {
					target.endReached = true
					target.patches = append(target.patches, site)
				}
			}
			stackTypes[len(stackTypes)-1] = immediate.Fallthrough
		case wasm.InstrBrTable:
			if _, err := pop(amd64.R10); err != nil {
				return nil, 0, nil, err
			}
			labels := instr.Labels(sf)
			for caseIndex, label := range labels {
				if int(label) > len(controls) {
					return nil, 0, nil, fmt.Errorf("br_table label %d is out of range", label)
				}
				if int(label) == len(controls) {
					if len(sf.Results) == 1 {
						if len(stackTypes) == 0 {
							return nil, 0, nil, fmt.Errorf("br_table function result is unavailable")
						}
						moveStackValue(len(stackTypes)-1, 0, sf.Results[0])
					}
					if caseIndex != len(labels)-1 {
						a.MovImm32(amd64.RAX, int32(caseIndex))
						a.Cmp32(amd64.R10, amd64.RAX)
						functionPatches = append(functionPatches, a.JccPlaceholder(amd64.CondE))
					} else {
						functionPatches = append(functionPatches, a.JmpPlaceholder())
					}
					continue
				}
				target := &controls[len(controls)-1-int(label)]
				if target.result && target.kind != wasm.InstrLoop {
					if len(stackTypes) == 0 {
						return nil, 0, nil, fmt.Errorf("br_table result is unavailable")
					}
					moveStackValue(len(stackTypes)-1, target.depth, target.resultType)
				}
				var site int
				if caseIndex != len(labels)-1 {
					a.MovImm32(amd64.RAX, int32(caseIndex))
					a.Cmp32(amd64.R10, amd64.RAX)
					site = a.JccPlaceholder(amd64.CondE)
				} else {
					site = a.JmpPlaceholder()
				}
				if target.kind == wasm.InstrLoop {
					a.PatchRel32(site, target.start)
				} else {
					target.endReached = true
					target.patches = append(target.patches, site)
				}
			}
			reachable = false
		case wasm.InstrDrop:
			if len(stackTypes) != 0 && stackTypes[len(stackTypes)-1] == wasm.V128 {
				discardV128(len(stackTypes) - 1)
				stackTypes = stackTypes[:len(stackTypes)-1]
			} else if _, err := pop(amd64.R10); err != nil {
				return nil, 0, nil, err
			}
		case wasm.InstrLocalGet:
			if sf.Locals[instr.U32()] == wasm.V128 {
				reg := amd64.Reg(0)
				if localPinned[instr.U32()] {
					reg = localRegisters[instr.U32()]
				} else {
					a.VMovdquLoadDisp(reg, amd64.RSP, localOff(int(instr.U32())))
				}
				if err := pushV128(reg); err != nil {
					return nil, 0, nil, err
				}
				continue
			}
			if localPinned[instr.U32()] {
				if localFloat[instr.U32()] {
					a.MovXmmToGpr(amd64.R10, localRegisters[instr.U32()], sf.Locals[instr.U32()] == wasm.F64)
				} else {
					a.MovReg64(amd64.R10, localRegisters[instr.U32()])
				}
			} else {
				a.LoadRsp64(amd64.R10, localOff(int(instr.U32())))
			}
			if err := push(sf.Locals[instr.U32()], amd64.R10); err != nil {
				return nil, 0, nil, err
			}
		case wasm.InstrLocalSet, wasm.InstrLocalTee:
			if sf.Locals[instr.U32()] == wasm.V128 {
				if len(stackTypes) == 0 || stackTypes[len(stackTypes)-1] != wasm.V128 {
					return nil, 0, nil, fmt.Errorf("operand stack v128 mismatch")
				}
				index := len(stackTypes) - 1
				reg := takeV128(index, 0)
				stackTypes = stackTypes[:index]
				if localPinned[instr.U32()] {
					dst := localRegisters[instr.U32()]
					if dst != reg {
						a.VMovdqu(dst, reg)
					}
					reg = dst
				} else {
					a.VMovdquStoreDisp(amd64.RSP, localOff(int(instr.U32())), reg)
				}
				if instr.Kind == wasm.InstrLocalTee {
					if err := pushV128(reg); err != nil {
						return nil, 0, nil, err
					}
				}
				continue
			}
			typ, err := pop(amd64.R10)
			if err != nil {
				return nil, 0, nil, err
			}
			if localPinned[instr.U32()] {
				if localFloat[instr.U32()] {
					a.MovGprToXmm(localRegisters[instr.U32()], amd64.R10, sf.Locals[instr.U32()] == wasm.F64)
				} else {
					a.MovReg64(localRegisters[instr.U32()], amd64.R10)
				}
			} else {
				a.StoreRsp64(localOff(int(instr.U32())), amd64.R10)
			}
			if instr.Kind == wasm.InstrLocalTee {
				if err := push(typ, amd64.R10); err != nil {
					return nil, 0, nil, err
				}
			}
		case wasm.InstrGlobalGet:
			a.Load64(amd64.R10, amd64.RBX, -int32(abi.GlobalsPtrOffset))
			a.Load64(amd64.R10, amd64.R10, int32(instr.U32())*8)
			a.Load64(amd64.RAX, amd64.R10, 0)
			if err := push(sf.Globals[instr.U32()], amd64.RAX); err != nil {
				return nil, 0, nil, err
			}
		case wasm.InstrGlobalSet:
			if _, err := pop(amd64.RAX); err != nil {
				return nil, 0, nil, err
			}
			a.Load64(amd64.R10, amd64.RBX, -int32(abi.GlobalsPtrOffset))
			a.Load64(amd64.R10, amd64.R10, int32(instr.U32())*8)
			a.Store64(amd64.R10, 0, amd64.RAX)
		case wasm.InstrI32Const, wasm.InstrI64Const, wasm.InstrF32Const, wasm.InstrF64Const:
			if instr.Kind == wasm.InstrI32Const {
				a.MovImm32(amd64.R10, int32(instr.U64()))
				if err := push(wasm.I32, amd64.R10); err != nil {
					return nil, 0, nil, err
				}
			} else {
				a.MovImm64(amd64.R10, instr.U64())
				typ := wasm.I64
				if instr.Kind == wasm.InstrF32Const {
					typ = wasm.F32
				} else if instr.Kind == wasm.InstrF64Const {
					typ = wasm.F64
				}
				if err := push(typ, amd64.R10); err != nil {
					return nil, 0, nil, err
				}
			}
		case wasm.InstrMemorySize:
			a.Load32(amd64.R10, amd64.RBX, -4)
			if err := push(wasm.I32, amd64.R10); err != nil {
				return nil, 0, nil, err
			}
		case wasm.InstrMemoryGrow:
			if _, err := pop(amd64.RAX); err != nil {
				return nil, 0, nil, err
			}
			a.Load32(amd64.R10, amd64.RBX, -4) // old pages, successful result
			a.MovReg32(amd64.R11, amd64.R10)
			a.Add32(amd64.R11, amd64.RAX)
			failOverflow := a.JccPlaceholder(amd64.CondB)
			a.Load32(amd64.RDX, amd64.RBX, -12)
			a.Cmp32(amd64.R11, amd64.RDX)
			failMax := a.JccPlaceholder(amd64.CondA)
			a.Store32(amd64.RBX, -4, amd64.R11)
			a.MovReg32(amd64.RDX, amd64.R11)
			a.ShiftImm(4, amd64.RDX, 16, true)
			a.Store64(amd64.RBX, -int32(abi.ActualLinMemByteSize64Offset), amd64.RDX)
			a.Store32(amd64.RBX, -8, amd64.RDX)
			a.MovReg32(amd64.RAX, amd64.R10)
			done := a.JmpPlaceholder()
			a.PatchRel32(failOverflow, a.Len())
			a.PatchRel32(failMax, a.Len())
			a.MovImm32(amd64.RAX, -1)
			a.PatchRel32(done, a.Len())
			if err := push(wasm.I32, amd64.RAX); err != nil {
				return nil, 0, nil, err
			}
		case wasm.InstrMemoryCopy, wasm.InstrMemoryFill:
			if err := emitAMD64StackBulkMemory(&a, instr, &stackTypes, stackOff, fn.Index, metadata); err != nil {
				return nil, 0, nil, fmt.Errorf("byte %d: %w", instr.Offset, err)
			}
		case wasm.InstrStructNewDefault:
			if fn.HelperSafepointBase == 0 {
				return nil, 0, nil, fmt.Errorf("structured allocating helper has no deterministic safepoint base")
			}
			id := fn.HelperSafepointBase + helperOrdinal
			helperOrdinal++
			a.Load64(amd64.R11, amd64.RBX, -int32(abi.SyncHostCustomContextOffset))
			a.MovImm64(amd64.R10, uint64(instr.U32()))
			a.Store64(amd64.R11, int32(abi.SyncHostArgsOffset), amd64.R10)
			if err := callGCHelper(codegen.GCHelperStructAllocDefault, id, 1, 1); err != nil {
				return nil, 0, nil, err
			}
			metadata.recordHelperSafepoint(a.Len(), id)
			a.Load64(amd64.R11, amd64.RBX, -int32(abi.SyncHostCustomContextOffset))
			a.Load64(amd64.RAX, amd64.R11, int32(abi.SyncHostResultsOffset))
			result, ok := sf.InstructionResultType(uint32(instrIndex), instr, 0)
			if !ok || result.Kind() != wasm.ValRef {
				return nil, 0, nil, fmt.Errorf("structured struct.new_default result type is unavailable")
			}
			if err := push(result, amd64.RAX); err != nil {
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
			a.Load64(amd64.R11, amd64.RBX, -int32(abi.SyncHostCustomContextOffset))
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
					a.VMovdquLoadDisp(0, amd64.RSP, stackOff(base+fieldID))
					a.VMovdquStoreDisp(amd64.R11, int32(abi.SyncHostArgsOffset+slot*8), 0)
					slot += 2
				} else {
					a.LoadRsp64(amd64.RAX, stackOff(base+fieldID))
					a.Store64(amd64.R11, int32(abi.SyncHostArgsOffset+slot*8), amd64.RAX)
					slot++
				}
			}
			stackTypes = stackTypes[:base]
			a.MovImm64(amd64.R10, uint64(instr.U32()))
			a.Store64(amd64.R11, int32(abi.SyncHostArgsOffset+slot*8), amd64.R10)
			id := fn.HelperSafepointBase + helperOrdinal
			helperOrdinal++
			if err := callGCHelper(codegen.GCHelperStructAlloc, id, uint32(slot+1), 1); err != nil {
				return nil, 0, nil, err
			}
			metadata.recordHelperSafepoint(a.Len(), id)
			a.Load64(amd64.R11, amd64.RBX, -int32(abi.SyncHostCustomContextOffset))
			a.Load64(amd64.RAX, amd64.R11, int32(abi.SyncHostResultsOffset))
			result, ok := sf.InstructionResultType(uint32(instrIndex), instr, 0)
			if !ok || result.Kind() != wasm.ValRef {
				return nil, 0, nil, fmt.Errorf("structured struct.new result type is unavailable")
			}
			if err := push(result, amd64.RAX); err != nil {
				return nil, 0, nil, err
			}
		case wasm.InstrArrayNewDefault:
			if fn.HelperSafepointBase == 0 {
				return nil, 0, nil, fmt.Errorf("structured allocating helper has no deterministic safepoint base")
			}
			if _, err := pop(amd64.RAX); err != nil {
				return nil, 0, nil, err
			}
			id := fn.HelperSafepointBase + helperOrdinal
			helperOrdinal++
			a.Load64(amd64.R11, amd64.RBX, -int32(abi.SyncHostCustomContextOffset))
			a.Store64(amd64.R11, int32(abi.SyncHostArgsOffset), amd64.RAX)
			a.MovImm64(amd64.R10, uint64(instr.U32()))
			a.Store64(amd64.R11, int32(abi.SyncHostArgsOffset+8), amd64.R10)
			if err := callGCHelper(codegen.GCHelperArrayAllocDefault, id, 2, 1); err != nil {
				return nil, 0, nil, err
			}
			metadata.recordHelperSafepoint(a.Len(), id)
			a.Load64(amd64.R11, amd64.RBX, -int32(abi.SyncHostCustomContextOffset))
			a.Load64(amd64.RAX, amd64.R11, int32(abi.SyncHostResultsOffset))
			result, ok := sf.InstructionResultType(uint32(instrIndex), instr, 0)
			if !ok || result.Kind() != wasm.ValRef {
				return nil, 0, nil, fmt.Errorf("structured array.new_default result type is unavailable")
			}
			if err := push(result, amd64.RAX); err != nil {
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
			if _, err := pop(amd64.RAX); err != nil {
				return nil, 0, nil, err
			}
			if err := popV128(0); err != nil {
				return nil, 0, nil, err
			}
			id := fn.HelperSafepointBase + helperOrdinal
			helperOrdinal++
			a.Load64(amd64.R11, amd64.RBX, -int32(abi.SyncHostCustomContextOffset))
			a.VMovdquStoreDisp(amd64.R11, int32(abi.SyncHostArgsOffset), 0)
			a.Store64(amd64.R11, int32(abi.SyncHostArgsOffset+16), amd64.RAX)
			a.MovImm64(amd64.R10, uint64(instr.U32()))
			a.Store64(amd64.R11, int32(abi.SyncHostArgsOffset+24), amd64.R10)
			if err := callGCHelper(codegen.GCHelperArrayAllocUniform, id, 4, 1); err != nil {
				return nil, 0, nil, err
			}
			metadata.recordHelperSafepoint(a.Len(), id)
			a.Load64(amd64.R11, amd64.RBX, -int32(abi.SyncHostCustomContextOffset))
			a.Load64(amd64.RAX, amd64.R11, int32(abi.SyncHostResultsOffset))
			result, ok := sf.InstructionResultType(uint32(instrIndex), instr, 0)
			if !ok || result.Kind() != wasm.ValRef {
				return nil, 0, nil, fmt.Errorf("structured array.new result type is unavailable")
			}
			if err := push(result, amd64.RAX); err != nil {
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
			a.LeaRsp(amd64.RAX, stackOff(base))
			stackTypes = stackTypes[:base]
			id := fn.HelperSafepointBase + helperOrdinal
			helperOrdinal++
			a.Load64(amd64.R11, amd64.RBX, -int32(abi.SyncHostCustomContextOffset))
			a.Store64(amd64.R11, int32(abi.SyncHostArgsOffset), amd64.RAX)
			a.MovImm64(amd64.R10, uint64(count))
			a.Store64(amd64.R11, int32(abi.SyncHostArgsOffset+8), amd64.R10)
			a.MovImm64(amd64.R10, uint64(instr.U32()))
			a.Store64(amd64.R11, int32(abi.SyncHostArgsOffset+16), amd64.R10)
			if err := callGCHelper(codegen.GCHelperArrayAllocFixedV128Spill, id, 3, 1); err != nil {
				return nil, 0, nil, err
			}
			metadata.recordHelperSafepoint(a.Len(), id)
			a.Load64(amd64.R11, amd64.RBX, -int32(abi.SyncHostCustomContextOffset))
			a.Load64(amd64.RAX, amd64.R11, int32(abi.SyncHostResultsOffset))
			result, ok := sf.InstructionResultType(uint32(instrIndex), instr, 0)
			if !ok || result.Kind() != wasm.ValRef {
				return nil, 0, nil, fmt.Errorf("structured array.new_fixed result type is unavailable")
			}
			if err := push(result, amd64.RAX); err != nil {
				return nil, 0, nil, err
			}
		case wasm.InstrStructGet:
			if instr.ValueType() != wasm.V128 {
				return nil, 0, nil, fmt.Errorf("structured struct.get fallback requires v128 result")
			}
			if _, err := pop(amd64.RAX); err != nil {
				return nil, 0, nil, err
			}
			a.Load64(amd64.R11, amd64.RBX, -int32(abi.SyncHostCustomContextOffset))
			a.Store64(amd64.R11, int32(abi.SyncHostArgsOffset), amd64.RAX)
			a.MovImm64(amd64.R10, uint64(uint32(instr.U64()>>32)))
			a.Store64(amd64.R11, int32(abi.SyncHostArgsOffset+8), amd64.R10)
			a.MovImm64(amd64.R10, uint64(instr.U32()))
			a.Store64(amd64.R11, int32(abi.SyncHostArgsOffset+16), amd64.R10)
			if err := callGCHelper(codegen.GCHelperStructGet, 0, 3, 2); err != nil {
				return nil, 0, nil, err
			}
			a.Load64(amd64.R11, amd64.RBX, -int32(abi.SyncHostCustomContextOffset))
			a.VMovdquLoadDisp(0, amd64.R11, int32(abi.SyncHostResultsOffset))
			if err := pushV128(0); err != nil {
				return nil, 0, nil, err
			}
		case wasm.InstrStructSet:
			typeID, fieldID := uint32(instr.U64()>>32), instr.U32()
			field, ok := sf.Module.StructField(typeID, fieldID)
			if !ok || field.Storage().Val() != wasm.V128 {
				return nil, 0, nil, fmt.Errorf("structured struct.set fallback requires v128 field")
			}
			if err := popV128(0); err != nil {
				return nil, 0, nil, err
			}
			if _, err := pop(amd64.RAX); err != nil {
				return nil, 0, nil, err
			}
			a.Load64(amd64.R11, amd64.RBX, -int32(abi.SyncHostCustomContextOffset))
			a.Store64(amd64.R11, int32(abi.SyncHostArgsOffset), amd64.RAX)
			a.VMovdquStoreDisp(amd64.R11, int32(abi.SyncHostArgsOffset+8), 0)
			a.MovImm64(amd64.R10, uint64(typeID))
			a.Store64(amd64.R11, int32(abi.SyncHostArgsOffset+24), amd64.R10)
			a.MovImm64(amd64.R10, uint64(fieldID))
			a.Store64(amd64.R11, int32(abi.SyncHostArgsOffset+32), amd64.R10)
			if err := callGCHelper(codegen.GCHelperStructSet, 0, 5, 0); err != nil {
				return nil, 0, nil, err
			}
		case wasm.InstrArrayGet:
			if instr.ValueType() != wasm.V128 {
				return nil, 0, nil, fmt.Errorf("structured array.get fallback requires v128 result")
			}
			if _, err := pop(amd64.R10); err != nil {
				return nil, 0, nil, err
			}
			if _, err := pop(amd64.RAX); err != nil {
				return nil, 0, nil, err
			}
			a.Load64(amd64.R11, amd64.RBX, -int32(abi.SyncHostCustomContextOffset))
			a.Store64(amd64.R11, int32(abi.SyncHostArgsOffset), amd64.RAX)
			a.Store64(amd64.R11, int32(abi.SyncHostArgsOffset+8), amd64.R10)
			a.MovImm64(amd64.R10, uint64(instr.U32()))
			a.Store64(amd64.R11, int32(abi.SyncHostArgsOffset+16), amd64.R10)
			if err := callGCHelper(codegen.GCHelperArrayGet, 0, 3, 2); err != nil {
				return nil, 0, nil, err
			}
			a.Load64(amd64.R11, amd64.RBX, -int32(abi.SyncHostCustomContextOffset))
			a.VMovdquLoadDisp(0, amd64.R11, int32(abi.SyncHostResultsOffset))
			if err := pushV128(0); err != nil {
				return nil, 0, nil, err
			}
		case wasm.InstrArraySet:
			field, ok := sf.Module.ArrayField(instr.U32())
			if !ok || field.Storage().Val() != wasm.V128 {
				return nil, 0, nil, fmt.Errorf("structured array.set fallback requires v128 element")
			}
			if err := popV128(0); err != nil {
				return nil, 0, nil, err
			}
			if _, err := pop(amd64.R10); err != nil {
				return nil, 0, nil, err
			}
			if _, err := pop(amd64.RAX); err != nil {
				return nil, 0, nil, err
			}
			a.Load64(amd64.R11, amd64.RBX, -int32(abi.SyncHostCustomContextOffset))
			a.Store64(amd64.R11, int32(abi.SyncHostArgsOffset), amd64.RAX)
			a.Store64(amd64.R11, int32(abi.SyncHostArgsOffset+8), amd64.R10)
			a.VMovdquStoreDisp(amd64.R11, int32(abi.SyncHostArgsOffset+16), 0)
			a.MovImm64(amd64.R10, uint64(instr.U32()))
			a.Store64(amd64.R11, int32(abi.SyncHostArgsOffset+32), amd64.R10)
			if err := callGCHelper(codegen.GCHelperArraySet, 0, 5, 0); err != nil {
				return nil, 0, nil, err
			}
		case wasm.InstrArrayFill:
			field, ok := sf.Module.ArrayField(instr.U32())
			if !ok || field.Storage().Val() != wasm.V128 {
				return nil, 0, nil, fmt.Errorf("structured array.fill fallback requires v128 element")
			}
			if _, err := pop(amd64.RDX); err != nil {
				return nil, 0, nil, err
			}
			if err := popV128(0); err != nil {
				return nil, 0, nil, err
			}
			if _, err := pop(amd64.R10); err != nil {
				return nil, 0, nil, err
			}
			if _, err := pop(amd64.RAX); err != nil {
				return nil, 0, nil, err
			}
			a.Load64(amd64.R11, amd64.RBX, -int32(abi.SyncHostCustomContextOffset))
			a.Store64(amd64.R11, int32(abi.SyncHostArgsOffset), amd64.RAX)
			a.Store64(amd64.R11, int32(abi.SyncHostArgsOffset+8), amd64.R10)
			a.VMovdquStoreDisp(amd64.R11, int32(abi.SyncHostArgsOffset+16), 0)
			a.Store64(amd64.R11, int32(abi.SyncHostArgsOffset+32), amd64.RDX)
			a.MovImm64(amd64.R10, uint64(instr.U32()))
			a.Store64(amd64.R11, int32(abi.SyncHostArgsOffset+40), amd64.R10)
			if err := callGCHelper(codegen.GCHelperArrayFill, 0, 6, 0); err != nil {
				return nil, 0, nil, err
			}
		case wasm.InstrRefCast:
			if _, err := pop(amd64.RAX); err != nil {
				return nil, 0, nil, err
			}
			heap, nullable, exact := codegen.DecodeGCRefTarget(instr.U64())
			a.Load64(amd64.R11, amd64.RBX, -int32(abi.SyncHostCustomContextOffset))
			a.Store64(amd64.R11, int32(abi.SyncHostArgsOffset), amd64.RAX)
			a.MovImm64(amd64.R10, uint64(heap))
			a.Store64(amd64.R11, int32(abi.SyncHostArgsOffset+8), amd64.R10)
			a.StoreImm32Mem(amd64.R11, int32(abi.SyncHostArgsOffset+16), int32(boolUint32(nullable)))
			a.StoreImm32Mem(amd64.R11, int32(abi.SyncHostArgsOffset+24), int32(boolUint32(exact)))
			if err := callGCHelper(codegen.GCHelperRefCast, 0, 4, 1); err != nil {
				return nil, 0, nil, err
			}
			a.Load64(amd64.R11, amd64.RBX, -int32(abi.SyncHostCustomContextOffset))
			a.Load64(amd64.RAX, amd64.R11, int32(abi.SyncHostResultsOffset))
			result, ok := sf.InstructionResultType(uint32(instrIndex), instr, 0)
			if !ok || result.Kind() != wasm.ValRef {
				return nil, 0, nil, fmt.Errorf("structured ref.cast result type is unavailable")
			}
			if err := push(result, amd64.RAX); err != nil {
				return nil, 0, nil, err
			}
		case wasm.InstrAnyConvertExtern, wasm.InstrExternConvertAny:
			if _, err := pop(amd64.RAX); err != nil {
				return nil, 0, nil, err
			}
			helper := codegen.GCHelperAnyConvertExtern
			if instr.Kind == wasm.InstrExternConvertAny {
				helper = codegen.GCHelperExternConvertAny
			}
			a.Load64(amd64.R11, amd64.RBX, -int32(abi.SyncHostCustomContextOffset))
			a.Store64(amd64.R11, int32(abi.SyncHostArgsOffset), amd64.RAX)
			if err := callGCHelper(helper, 0, 1, 1); err != nil {
				return nil, 0, nil, err
			}
			a.Load64(amd64.R11, amd64.RBX, -int32(abi.SyncHostCustomContextOffset))
			a.Load64(amd64.RAX, amd64.R11, int32(abi.SyncHostResultsOffset))
			result, ok := sf.InstructionResultType(uint32(instrIndex), instr, 0)
			if !ok || result.Kind() != wasm.ValRef {
				return nil, 0, nil, fmt.Errorf("structured extern conversion result type is unavailable")
			}
			if err := push(result, amd64.RAX); err != nil {
				return nil, 0, nil, err
			}
		case wasm.InstrDataDrop:
			offset := uint64(instr.U32())*16 + 8
			if offset > math.MaxInt32 {
				return nil, 0, nil, fmt.Errorf("data.drop descriptor offset is not encodable")
			}
			a.Load64(amd64.R11, amd64.RBX, -int32(abi.PassiveDataPtrOffset))
			a.StoreImm32Mem(amd64.R11, int32(offset), 0)
		case wasm.InstrElemDrop:
			payload, ok := codegen.EncodeGCHelperDispatch(codegen.GCHelperArrayDropElem, 0)
			if !ok {
				return nil, 0, nil, fmt.Errorf("GC elem.drop helper is not encodable")
			}
			a.Load64(amd64.R11, amd64.RBX, -int32(abi.SyncHostCustomContextOffset))
			a.MovImm64(amd64.R10, uint64(instr.U32()))
			a.Store64(amd64.R11, int32(abi.SyncHostArgsOffset), amd64.R10)
			a.StoreImm32Mem(amd64.R11, int32(abi.SyncHostImportIndexOffset), int32(codegen.GCHelperDispatchBit|payload))
			a.StoreImm32Mem(amd64.R11, int32(abi.SyncHostArityOffset), 1)
			a.CallMem(amd64.R11, int32(abi.SyncHostTrampolineOffset))
		case wasm.InstrSelect:
			if _, err := pop(amd64.R11); err != nil {
				return nil, 0, nil, err
			}
			if len(stackTypes) != 0 && stackTypes[len(stackTypes)-1] == wasm.V128 {
				if err := popV128(1); err != nil {
					return nil, 0, nil, err
				}
				if err := popV128(0); err != nil {
					return nil, 0, nil, err
				}
				a.TestSelf(amd64.R11, false)
				keepLHS := a.JccPlaceholder(amd64.CondNE)
				a.VMovdqu(0, 1)
				a.PatchRel32(keepLHS, a.Len())
				if err := pushV128(0); err != nil {
					return nil, 0, nil, err
				}
				continue
			}
			rhsType, err := pop(amd64.RAX)
			if err != nil {
				return nil, 0, nil, err
			}
			lhsType, err := pop(amd64.R10)
			if err != nil || lhsType != rhsType {
				return nil, 0, nil, fmt.Errorf("select operand mismatch")
			}
			a.TestSelf(amd64.R11, false)
			a.Cmovcc(amd64.CondNE, amd64.RAX, amd64.R10, lhsType == wasm.I64 || lhsType == wasm.F64)
			if err := push(lhsType, amd64.RAX); err != nil {
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
				err = emitAMD64StackSIMD(&a, descriptor, instr, &stackTypes, stackOff, cachedV128, reserveV128, loadV128, cacheV128, loadScalar, cacheScalar, discardScalar, materializeSIMDConstant, shuffleSIMDConstant, simdConstants, cacheMemorySize, fn.Index, localMemoryCheckEnds[instrIndex], plan.ElidesBoundsCheck(uint32(instrIndex)) || localMemoryChecksElided[instrIndex], metadata)
				pruneVectorStackCache()
				pruneScalarStackCache()
			} else if amd64MemoryStackKind(instr.Kind) {
				err = emitAMD64StackMemory(&a, instr, &stackTypes, loadScalar, cacheScalar, discardScalar, cacheMemorySize, fn.Index, localMemoryCheckEnds[instrIndex], plan.ElidesBoundsCheck(uint32(instrIndex)) || localMemoryChecksElided[instrIndex], metadata)
				pruneScalarStackCache()
			} else if amd64FloatStackKind(instr.Kind) {
				err = emitAMD64StackFloat(&a, instr.Kind, &stackTypes, loadScalar, cacheScalar, discardScalar, fn.Index, instr.Offset, metadata)
				pruneScalarStackCache()
			} else if instr.Kind == wasm.InstrCall || instr.Kind == wasm.InstrCallIndirect {
				err = emitAMD64StackCall(&a, sf, instr, &stackTypes, stackOff, &callRelocs, fn.Index, metadata)
			} else {
				err = emitAMD64StackInteger(&a, instr.Kind, &stackTypes, scalarOperand, cacheScalar, discardScalar, fn.Index, instr.Offset, metadata)
			}
			if err != nil {
				return nil, 0, nil, fmt.Errorf("byte %d: %w", instr.Offset, err)
			}
		}
	}
	if len(controls) != 0 {
		return nil, 0, nil, fmt.Errorf("unterminated structured control")
	}
	flushVectorStackCache()
	flushScalarStackCache()
	functionReachable := reachable || len(functionPatches) != 0
	for _, site := range functionPatches {
		a.PatchRel32(site, a.Len())
	}
	if functionReachable && len(sf.Results) == 1 {
		if reachable && len(stackTypes) != 1 || !reachable && len(functionPatches) == 0 {
			return nil, 0, nil, fmt.Errorf("invalid result stack")
		}
		if sf.Results[0] == wasm.V128 {
			a.VMovdquLoadDisp(0, amd64.RSP, stackOff(0))
		} else {
			a.LoadRsp64(amd64.RAX, stackOff(0))
		}
	}
	if frameBytes != 0 {
		a.AddRsp(int32(frameBytes))
	}
	a.Ret()
	for index := range simdConstantPatches {
		constant := &simdConstantPatches[index]
		target := -1
		for previous := range simdConstantPatches[:index] {
			if simdConstantPatches[previous].bytes == constant.bytes {
				target = simdConstantPatches[previous].target
				break
			}
		}
		if target < 0 {
			for a.Len()&15 != 0 {
				a.EmitBytes([]byte{0})
			}
			target = a.Len()
			a.EmitBytes(constant.bytes[:])
		}
		constant.target = target
		a.PatchRel32(constant.at, target)
	}
	return a.B, internalOffset, callRelocs, nil
}

func amd64FloatStackKind(kind wasm.InstrKind) bool {
	return kind >= wasm.InstrF32Eq && kind <= wasm.InstrF64Ge ||
		kind >= wasm.InstrF32Abs && kind <= wasm.InstrF64ReinterpretI64 || kind == wasm.InstrI64ExtendI32U ||
		kind == wasm.InstrI32TruncF32S || kind == wasm.InstrI32TruncF32U ||
		kind == wasm.InstrI32TruncF64S || kind == wasm.InstrI32TruncF64U ||
		kind == wasm.InstrI64TruncF32S || kind == wasm.InstrI64TruncF32U ||
		kind == wasm.InstrI64TruncF64S || kind == wasm.InstrI64TruncF64U ||
		kind == wasm.InstrF32ConvertI64S || kind == wasm.InstrF32ConvertI64U ||
		kind == wasm.InstrF64ConvertI64S || kind == wasm.InstrF64ConvertI64U ||
		kind == wasm.InstrI32TruncSatF32S || kind == wasm.InstrI32TruncSatF32U ||
		kind == wasm.InstrI32TruncSatF64S || kind == wasm.InstrI32TruncSatF64U ||
		kind == wasm.InstrI64TruncSatF32S || kind == wasm.InstrI64TruncSatF32U ||
		kind == wasm.InstrI64TruncSatF64S || kind == wasm.InstrI64TruncSatF64U
}

func amd64F32RoundTripUpdate(instrs []railssa.StackInstr, start int) (n, acc uint32, promote bool, end int, ok bool) {
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

func amd64IdentityRoundTripUpdate(instrs []railssa.StackInstr, start int) (int, bool) {
	if start+5 < len(instrs) && instrs[start].Kind == wasm.InstrLocalGet && instrs[start+1].Kind == wasm.InstrLocalGet &&
		(instrs[start+2].Kind == wasm.InstrF64ConvertI32S || instrs[start+2].Kind == wasm.InstrF64ConvertI32U) &&
		(instrs[start+3].Kind == wasm.InstrI32TruncSatF64S || instrs[start+3].Kind == wasm.InstrI32TruncSatF64U) &&
		instrs[start+4].Kind == wasm.InstrI32Add && instrs[start+5].Kind == wasm.InstrLocalSet && instrs[start+5].U32() == instrs[start+1].U32() {
		return start + 6, true
	}
	if start+5 < len(instrs) && instrs[start].Kind == wasm.InstrLocalGet && instrs[start+1].Kind == wasm.InstrLocalGet &&
		(instrs[start+2].Kind == wasm.InstrI64ExtendI32S || instrs[start+2].Kind == wasm.InstrI64ExtendI32U) && instrs[start+3].Kind == wasm.InstrI32WrapI64 &&
		instrs[start+4].Kind == wasm.InstrI32Add && instrs[start+5].Kind == wasm.InstrLocalSet && instrs[start+5].U32() == instrs[start+1].U32() {
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

func amd64LocalChurnGroup(instrs []railssa.StackInstr, start int) (a, b, c, n uint32, end int, ok bool) {
	if start+12 >= len(instrs) || instrs[start].Kind != wasm.InstrLocalGet || instrs[start+1].Kind != wasm.InstrLocalGet ||
		instrs[start+2].Kind != wasm.InstrI32Add || instrs[start+3].Kind != wasm.InstrLocalSet || instrs[start+4].Kind != wasm.InstrLocalGet ||
		instrs[start+5].Kind != wasm.InstrLocalGet || instrs[start+6].Kind != wasm.InstrI32Add || instrs[start+7].Kind != wasm.InstrLocalSet ||
		instrs[start+8].Kind != wasm.InstrLocalGet || instrs[start+9].Kind != wasm.InstrLocalGet || instrs[start+10].Kind != wasm.InstrI32Add ||
		instrs[start+11].Kind != wasm.InstrLocalTee || instrs[start+12].Kind != wasm.InstrLocalSet {
		return 0, 0, 0, 0, start, false
	}
	b, n, a = instrs[start].U32(), instrs[start+1].U32(), instrs[start+3].U32()
	if instrs[start+4].U32() == a || instrs[start+5].U32() != a || instrs[start+7].U32() != b || instrs[start+8].U32() != a ||
		instrs[start+9].U32() != b || instrs[start+11].U32() != a {
		return 0, 0, 0, 0, start, false
	}
	c = instrs[start+4].U32()
	if instrs[start+12].U32() != c {
		return 0, 0, 0, 0, start, false
	}
	return a, b, c, n, start + 13, true
}

const (
	amd64ControlBrTable = iota
	amd64ControlSelect
	amd64ControlIfElse
	amd64ControlBrIf
)

func amd64ControlAccumulatorGroup(sf *railssa.StackFunc, s, shape int) (uint32, int, bool) {
	in := sf.Instrs
	switch shape {
	case amd64ControlSelect:
		if s+10 >= len(in) || in[s].Kind != wasm.InstrLocalGet || in[s+1].Kind != wasm.InstrI32Const || in[s+1].U64() != 7 ||
			in[s+2].Kind != wasm.InstrI32Add || in[s+3].Kind != wasm.InstrLocalGet || in[s+4].Kind != wasm.InstrI32Const || in[s+4].U64() != 3 ||
			in[s+5].Kind != wasm.InstrI32Sub || in[s+6].Kind != wasm.InstrLocalGet || in[s+7].Kind != wasm.InstrI32Const || in[s+7].U64() != 1 ||
			in[s+8].Kind != wasm.InstrI32And || in[s+9].Kind != wasm.InstrSelect || in[s+10].Kind != wasm.InstrLocalSet {
			return 0, s, false
		}
		acc := in[s].U32()
		return acc, s + 11, in[s+3].U32() == acc && in[s+6].U32() == acc && in[s+10].U32() == acc
	case amd64ControlIfElse:
		if s+12 >= len(in) || in[s].Kind != wasm.InstrLocalGet || in[s+1].Kind != wasm.InstrI32Const || in[s+1].U64() != 1 || in[s+2].Kind != wasm.InstrI32And ||
			in[s+3].Kind != wasm.InstrIf || !in[s+3].HasResult() || in[s+4].Kind != wasm.InstrLocalGet || in[s+5].Kind != wasm.InstrI32Const || in[s+5].U64() != 7 ||
			in[s+6].Kind != wasm.InstrI32Add || !in[s+7].IsElse() || in[s+8].Kind != wasm.InstrLocalGet || in[s+9].Kind != wasm.InstrI32Const || in[s+9].U64() != 3 ||
			in[s+10].Kind != wasm.InstrI32Sub || in[s+11].Kind != wasm.InstrInvalid || in[s+12].Kind != wasm.InstrLocalSet {
			return 0, s, false
		}
		acc := in[s].U32()
		return acc, s + 13, in[s+4].U32() == acc && in[s+8].U32() == acc && in[s+12].U32() == acc
	case amd64ControlBrIf:
		if s+13 >= len(in) || in[s].Kind != wasm.InstrBlock || in[s+1].Kind != wasm.InstrLocalGet || in[s+2].Kind != wasm.InstrI32Const || in[s+2].U64() != 1 ||
			in[s+3].Kind != wasm.InstrI32And || in[s+4].Kind != wasm.InstrBrIf || in[s+4].U32() != 0 || in[s+5].Kind != wasm.InstrLocalGet ||
			in[s+6].Kind != wasm.InstrI32Const || in[s+6].U64() != 3 || in[s+7].Kind != wasm.InstrI32Add || in[s+8].Kind != wasm.InstrLocalSet ||
			in[s+9].Kind != wasm.InstrInvalid || in[s+10].Kind != wasm.InstrLocalGet || in[s+11].Kind != wasm.InstrI32Const || in[s+11].U64() != 1 ||
			in[s+12].Kind != wasm.InstrI32Add || in[s+13].Kind != wasm.InstrLocalSet {
			return 0, s, false
		}
		acc := in[s+1].U32()
		return acc, s + 14, in[s+5].U32() == acc && in[s+8].U32() == acc && in[s+10].U32() == acc && in[s+13].U32() == acc
	case amd64ControlBrTable:
		if s+29 >= len(in) || in[s].Kind != wasm.InstrBlock || !in[s].HasResult() || in[s+1].Kind != wasm.InstrBlock || in[s+2].Kind != wasm.InstrBlock ||
			in[s+3].Kind != wasm.InstrBlock || in[s+4].Kind != wasm.InstrBlock || in[s+5].Kind != wasm.InstrLocalGet || in[s+6].Kind != wasm.InstrI32Const ||
			in[s+6].U64() != 3 || in[s+7].Kind != wasm.InstrI32And || in[s+8].Kind != wasm.InstrBrTable || in[s+8].LabelLen() != 4 ||
			in[s+9].Kind != wasm.InstrInvalid || in[s+10].Kind != wasm.InstrLocalGet || in[s+11].Kind != wasm.InstrI32Const || in[s+11].U64() != 7 ||
			in[s+12].Kind != wasm.InstrI32Add || in[s+13].Kind != wasm.InstrBr || in[s+13].U32() != 3 || in[s+14].Kind != wasm.InstrInvalid ||
			in[s+15].Kind != wasm.InstrLocalGet || in[s+16].Kind != wasm.InstrI32Const || in[s+16].U64() != 5 || in[s+17].Kind != wasm.InstrI32Add ||
			in[s+18].Kind != wasm.InstrBr || in[s+18].U32() != 2 || in[s+19].Kind != wasm.InstrInvalid || in[s+20].Kind != wasm.InstrLocalGet ||
			in[s+21].Kind != wasm.InstrI32Const || in[s+21].U64() != 3 || in[s+22].Kind != wasm.InstrI32Add || in[s+23].Kind != wasm.InstrBr ||
			in[s+23].U32() != 1 || in[s+24].Kind != wasm.InstrInvalid || in[s+25].Kind != wasm.InstrLocalGet || in[s+26].Kind != wasm.InstrI32Const ||
			in[s+26].U64() != 1 || in[s+27].Kind != wasm.InstrI32Add || in[s+28].Kind != wasm.InstrInvalid || in[s+29].Kind != wasm.InstrLocalSet {
			return 0, s, false
		}
		labels, acc := in[s+8].Labels(sf), in[s+5].U32()
		if labels[0] != 3 || labels[1] != 2 || labels[2] != 1 || labels[3] != 0 {
			return 0, s, false
		}
		for _, pos := range [...]int{10, 15, 20, 25, 29} {
			if in[s+pos].U32() != acc {
				return 0, s, false
			}
		}
		return acc, s + 30, true
	}
	return 0, s, false
}

func amd64DirectIntegerBinaryKind(kind wasm.InstrKind) bool {
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

func amd64DirectSafeDivKind(kind wasm.InstrKind) bool {
	return kind == wasm.InstrI32DivS || kind == wasm.InstrI32DivU || kind == wasm.InstrI32RemS || kind == wasm.InstrI32RemU ||
		kind == wasm.InstrI64DivS || kind == wasm.InstrI64DivU || kind == wasm.InstrI64RemS || kind == wasm.InstrI64RemU
}

func amd64DirectIntegerUnaryKind(kind wasm.InstrKind) bool {
	return kind == wasm.InstrI32Clz || kind == wasm.InstrI32Ctz || kind == wasm.InstrI32Popcnt ||
		kind == wasm.InstrI64Clz || kind == wasm.InstrI64Ctz || kind == wasm.InstrI64Popcnt
}

func emitAMD64DirectIntegerUnary(a *amd64.Asm, kind wasm.InstrKind, dst, src amd64.Reg) {
	wide := kind >= wasm.InstrI64Clz
	switch kind {
	case wasm.InstrI32Clz, wasm.InstrI64Clz:
		a.Lzcnt(dst, src, wide)
	case wasm.InstrI32Ctz, wasm.InstrI64Ctz:
		a.Tzcnt(dst, src, wide)
	case wasm.InstrI32Popcnt, wasm.InstrI64Popcnt:
		a.Popcnt(dst, src, wide)
	}
}

func emitAMD64DirectIntegerBinary(a *amd64.Asm, kind wasm.InstrKind, dst, lhs, rhs amd64.Reg) {
	wide := kind >= wasm.InstrI64Add && kind <= wasm.InstrI64Rotr
	if dst != lhs {
		a.MovReg64(dst, lhs)
	}
	switch kind {
	case wasm.InstrI32Add, wasm.InstrI64Add:
		a.AluRR(0x01, dst, rhs, wide)
	case wasm.InstrI32Sub, wasm.InstrI64Sub:
		a.AluRR(0x29, dst, rhs, wide)
	case wasm.InstrI32Mul, wasm.InstrI64Mul:
		a.ImulRR(dst, rhs, wide)
	case wasm.InstrI32And, wasm.InstrI64And:
		a.AluRR(0x21, dst, rhs, wide)
	case wasm.InstrI32Or, wasm.InstrI64Or:
		a.AluRR(0x09, dst, rhs, wide)
	case wasm.InstrI32Xor, wasm.InstrI64Xor:
		a.AluRR(0x31, dst, rhs, wide)
	case wasm.InstrI32Shl, wasm.InstrI64Shl, wasm.InstrI32ShrS, wasm.InstrI64ShrS,
		wasm.InstrI32ShrU, wasm.InstrI64ShrU, wasm.InstrI32Rotl, wasm.InstrI64Rotl,
		wasm.InstrI32Rotr, wasm.InstrI64Rotr:
		a.MovReg64(amd64.RCX, rhs)
		digit := byte(4)
		switch kind {
		case wasm.InstrI32ShrS, wasm.InstrI64ShrS:
			digit = 7
		case wasm.InstrI32ShrU, wasm.InstrI64ShrU:
			digit = 5
		case wasm.InstrI32Rotl, wasm.InstrI64Rotl:
			digit = 0
		case wasm.InstrI32Rotr, wasm.InstrI64Rotr:
			digit = 1
		}
		a.ShiftCL(digit, dst, wide)
	}
}

func amd64DirectFloatBinaryKind(kind wasm.InstrKind) bool {
	return kind >= wasm.InstrF32Add && kind <= wasm.InstrF32Max || kind >= wasm.InstrF64Add && kind <= wasm.InstrF64Max
}

func emitAMD64DirectFloatBinary(a *amd64.Asm, kind wasm.InstrKind, dst, lhs, rhs amd64.Reg) {
	f64 := kind >= wasm.InstrF64Add
	switch kind {
	case wasm.InstrF32Add, wasm.InstrF64Add:
		a.VFAdd(dst, lhs, rhs, f64)
	case wasm.InstrF32Sub, wasm.InstrF64Sub:
		a.VFSub(dst, lhs, rhs, f64)
	case wasm.InstrF32Mul, wasm.InstrF64Mul:
		a.VFMul(dst, lhs, rhs, f64)
	case wasm.InstrF32Div, wasm.InstrF64Div:
		a.VFDiv(dst, lhs, rhs, f64)
	case wasm.InstrF32Min, wasm.InstrF64Min:
		out := dst
		if out == rhs && out != lhs {
			out = 15
		}
		if out != lhs {
			a.FMov(out, lhs, f64)
		}
		emitAMD64WasmMinMax(a, out, rhs, f64, false)
		if out != dst {
			a.FMov(dst, out, f64)
		}
	case wasm.InstrF32Max, wasm.InstrF64Max:
		out := dst
		if out == rhs && out != lhs {
			out = 15
		}
		if out != lhs {
			a.FMov(out, lhs, f64)
		}
		emitAMD64WasmMinMax(a, out, rhs, f64, true)
		if out != dst {
			a.FMov(dst, out, f64)
		}
	}
}

func amd64DirectFloatUnaryKind(kind wasm.InstrKind) bool {
	return kind >= wasm.InstrF32Abs && kind <= wasm.InstrF32Sqrt || kind >= wasm.InstrF64Abs && kind <= wasm.InstrF64Sqrt
}

func amd64DirectSIMDBinaryKind(kind wasm.InstrKind) bool {
	switch kind {
	case wasm.InstrV128And, wasm.InstrV128Andnot, wasm.InstrV128Or, wasm.InstrV128Xor,
		wasm.InstrI8x16SubSatU, wasm.InstrI16x8Sub, wasm.InstrI32x4Add:
		return true
	default:
		return false
	}
}

func emitAMD64DirectSIMDBinary(a *amd64.Asm, kind wasm.InstrKind, dst, lhs, rhs amd64.Reg) {
	switch kind {
	case wasm.InstrV128And:
		a.VPand(dst, lhs, rhs)
	case wasm.InstrV128Andnot:
		a.VPandn(dst, rhs, lhs)
	case wasm.InstrV128Or:
		a.VPor(dst, lhs, rhs)
	case wasm.InstrV128Xor:
		a.VPxor(dst, lhs, rhs)
	case wasm.InstrI8x16SubSatU:
		a.VPsubusb(dst, lhs, rhs)
	case wasm.InstrI16x8Sub:
		a.VPsubw(dst, lhs, rhs)
	case wasm.InstrI32x4Add:
		a.VPaddd(dst, lhs, rhs)
	}
}

func emitAMD64DirectFloatUnary(a *amd64.Asm, kind wasm.InstrKind, dst, src amd64.Reg, f64 bool) {
	switch kind {
	case wasm.InstrF32Abs, wasm.InstrF64Abs:
		prefix := byte(0)
		if f64 {
			prefix = 1
		}
		a.VSseRRR(prefix, 0x54, dst, src, 7)
	case wasm.InstrF32Neg, wasm.InstrF64Neg:
		prefix := byte(0)
		if f64 {
			prefix = 1
		}
		a.VSseRRR(prefix, 0x57, dst, src, 7)
	case wasm.InstrF32Ceil, wasm.InstrF64Ceil:
		a.Round(dst, src, f64, 2|8)
	case wasm.InstrF32Floor, wasm.InstrF64Floor:
		a.Round(dst, src, f64, 1|8)
	case wasm.InstrF32Trunc, wasm.InstrF64Trunc:
		a.Round(dst, src, f64, 3|8)
	case wasm.InstrF32Nearest, wasm.InstrF64Nearest:
		a.Round(dst, src, f64, 0|8)
	case wasm.InstrF32Sqrt, wasm.InstrF64Sqrt:
		a.FSqrt(dst, src, f64)
	}
}

func amd64MemoryStackKind(kind wasm.InstrKind) bool {
	return kind >= wasm.InstrI32Load && kind <= wasm.InstrI64Load32U ||
		kind >= wasm.InstrI32Store && kind <= wasm.InstrI64Store32
}

func amd64ProvenMaskedMemoryLocal(sf *railssa.StackFunc, local uint32, accessSize uint64, memoryOffset uint32) bool {
	if int(local) < len(sf.Params) || sf.MemoryMinBytes == 0 {
		return false
	}
	found := false
	for i, instr := range sf.Instrs {
		if (instr.Kind != wasm.InstrLocalSet && instr.Kind != wasm.InstrLocalTee) || instr.U32() != local {
			continue
		}
		if instr.Kind != wasm.InstrLocalSet || i < 5 || sf.Instrs[i-5].Kind != wasm.InstrLocalGet || sf.Instrs[i-5].U32() != local ||
			sf.Instrs[i-4].Kind != wasm.InstrI32Const || sf.Instrs[i-3].Kind != wasm.InstrI32Add || sf.Instrs[i-2].Kind != wasm.InstrI32Const || sf.Instrs[i-1].Kind != wasm.InstrI32And {
			return false
		}
		stride, mask := sf.Instrs[i-4].U64(), sf.Instrs[i-2].U64()
		if stride == 0 || stride&(stride-1) != 0 || stride < accessSize || mask > uint64(^uint32(0)) {
			return false
		}
		maxAddress := mask &^ (stride - 1)
		if maxAddress+uint64(memoryOffset)+accessSize > sf.MemoryMinBytes {
			return false
		}
		found = true
	}
	return found
}

func emitAMD64StackBulkMemory(a *amd64.Asm, instr railssa.StackInstr, stack *[]wasm.ValType, stackOff func(int) int32, function uint32, metadata *functionEmissionMetadata) error {
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
	a.LoadRsp32(amd64.RDI, stackOff(base))
	a.LoadRsp32(amd64.RAX, stackOff(base+1))
	a.LoadRsp32(amd64.RCX, stackOff(base+2))
	emitAMD64BulkMemoryRegisters(a, instr.Kind, function, instr.Offset, metadata)
	*stack = types[:base]
	return nil
}

// emitAMD64BulkMemoryRegisters emits memory.copy/fill with destination in RDI,
// source or fill byte in RAX, and length in RCX. Callers preserve allocated
// values occupying REP's fixed registers before entering this helper.
func emitAMD64BulkMemoryRegisters(a *amd64.Asm, kind wasm.InstrKind, function, wasmOffset uint32, metadata *functionEmissionMetadata) {
	a.Load64(amd64.R11, amd64.RBX, -int32(abi.ActualLinMemByteSize64Offset))
	a.MovReg64(amd64.R10, amd64.RDI)
	a.Add64(amd64.R10, amd64.RCX)
	a.Cmp64(amd64.R10, amd64.R11)
	dstOOB := a.JccPlaceholder(amd64.CondA)
	var srcOOB int
	if kind == wasm.InstrMemoryCopy {
		a.MovReg32(amd64.RSI, amd64.RAX)
		a.MovReg64(amd64.R10, amd64.RSI)
		a.Add64(amd64.R10, amd64.RCX)
		a.Cmp64(amd64.R10, amd64.R11)
		srcOOB = a.JccPlaceholder(amd64.CondA)
	}
	a.Add64(amd64.RDI, amd64.RBX)
	if kind == wasm.InstrMemoryFill {
		a.RepStosb()
	} else {
		a.Add64(amd64.RSI, amd64.RBX)
		a.Cmp64(amd64.RDI, amd64.RSI)
		forward := a.JccPlaceholder(amd64.CondBE)
		a.MovReg64(amd64.R10, amd64.RSI)
		a.Add64(amd64.R10, amd64.RCX)
		a.Cmp64(amd64.RDI, amd64.R10)
		forwardDisjoint := a.JccPlaceholder(amd64.CondAE)
		a.TestSelf(amd64.RCX, false)
		backwardDone := a.JccPlaceholder(amd64.CondE)
		backward := a.Len()
		a.LoadIdx(amd64.R10, amd64.RSI, amd64.RCX, -1, 1, false, false)
		a.StoreIdx(amd64.RDI, amd64.RCX, amd64.R10, -1, 1)
		a.AluRI(5, amd64.RCX, 1, false)
		a.PatchRel32(a.JccPlaceholder(amd64.CondNE), backward)
		copyDone := a.JmpPlaceholder()
		a.PatchRel32(forward, a.Len())
		a.PatchRel32(forwardDisjoint, a.Len())
		a.RepMovsb()
		a.PatchRel32(backwardDone, a.Len())
		a.PatchRel32(copyDone, a.Len())
	}
	done := a.JmpPlaceholder()
	trap := a.Len()
	a.PatchRel32(dstOOB, trap)
	if kind == wasm.InstrMemoryCopy {
		a.PatchRel32(srcOOB, trap)
	}
	metadata.recordTrap(trap, wasmOffset, 3)
	amd64EmitTrap(a, 3, function, wasmOffset)
	a.PatchRel32(done, a.Len())
}

func emitAMD64StackCall(a *amd64.Asm, sf *railssa.StackFunc, instr railssa.StackInstr, stack *[]wasm.ValType, stackOff func(int) int32, relocs *[]amd64CallReloc, function uint32, metadata *functionEmissionMetadata) error {
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
			a.Load64(amd64.R11, amd64.RBX, -int32(abi.ImportDispatchPtrOffset))
			a.MovImm32(amd64.R10, int32(instr.U32()))
			a.ShiftImm(4, amd64.R10, 5, true)
			a.AluRR(0x01, amd64.R11, amd64.R10, true)
			a.Load64(amd64.R10, amd64.R11, 0)
			a.LeaRsp(amd64.RDI, stackOff(base))
			a.LeaRsp(amd64.RCX, stackOff(base))
			a.Load64(amd64.RSI, amd64.R11, 8)
			a.Load64(amd64.R8, amd64.R11, 16)
			a.Load64(amd64.R9, amd64.R11, 24)
			amd64CopyDraglineInstanceContext(a, amd64.RSI, amd64.R8)
			amd64CopyDraglineExecutionControl(a, amd64.RSI)
			a.Push(amd64.RBX)
			a.Push(amd64.R9)
			a.Push(amd64.R8)
			a.Push(amd64.RAX)
			a.CallReg(amd64.R10)
			metadata.recordSafepoint(a.Len())
			a.Pop(amd64.RAX)
			a.Pop(amd64.R8)
			a.Pop(amd64.R9)
			a.Pop(amd64.RBX)
			amd64CopyDraglineInstanceContext(a, amd64.RBX, amd64.R9)
			types = types[:base]
			if instr.HasResult() {
				types = append(types, instr.ValueType())
			}
			*stack = types
			return nil
		}
		a.LeaRsp(amd64.RDI, stackOff(base))
		for i := 0; i < min(int(instr.Params()), len(amd64ParamRegisters)); i++ {
			if types[base+i] == wasm.V128 {
				a.VMovdquLoadDisp(amd64.Reg(i), amd64.RSP, stackOff(base+i))
			} else {
				a.LoadRsp64(amd64ParamRegisters[i], stackOff(base+i))
			}
		}
		*relocs = append(*relocs, amd64CallReloc{at: a.CallRel32(), target: instr.U32() - sf.ImportedFuncs})
		metadata.recordSafepoint(a.Len())
		types = types[:base]
		if instr.HasResult() {
			if instr.ValueType() == wasm.V128 {
				a.VMovdquStoreDisp(amd64.RSP, stackOff(base), 0)
			} else {
				a.StoreRsp64(stackOff(base), amd64.RAX)
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
		// Table 0's descriptor is [len u32, max u32, 32-byte entries...].
		a.LoadRsp64(amd64.R10, stackOff(len(types)-1))
		a.Load64(amd64.R11, amd64.RBX, -80)
		a.Load32(amd64.RAX, amd64.R11, 0)
		a.Cmp32(amd64.R10, amd64.RAX)
		inBounds := a.JccPlaceholder(amd64.CondB)
		metadata.recordTrap(a.Len(), instr.Offset, 5)
		amd64EmitTrap(a, 5, function, instr.Offset)
		a.PatchRel32(inBounds, a.Len())
		a.ShiftImm(4, amd64.R10, 5, true)
		a.AluRR(0x01, amd64.R11, amd64.R10, true)
		a.Load64(amd64.R10, amd64.R11, 8)
		a.TestSelf(amd64.R10, true)
		nonNull := a.JccPlaceholder(amd64.CondNE)
		metadata.recordTrap(a.Len(), instr.Offset, 5)
		amd64EmitTrap(a, 5, function, instr.Offset)
		a.PatchRel32(nonNull, a.Len())
		a.Load64(amd64.RDX, amd64.R11, 16)
		a.MovImm64(amd64.RAX, sf.TypeKeys[keyIndex])
		a.Cmp64(amd64.RDX, amd64.RAX)
		sigOK := a.JccPlaceholder(amd64.CondE)
		metadata.recordTrap(a.Len(), instr.Offset, 6)
		amd64EmitTrap(a, 6, function, instr.Offset)
		a.PatchRel32(sigOK, a.Len())

		// Dragline publishes wrapper entries for funcrefs. Marshal directly from
		// canonical operand slots, preserving this instance's linMem register.
		a.LeaRsp(amd64.RDI, stackOff(base))
		a.LeaRsp(amd64.RCX, stackOff(base))
		a.Load64(amd64.RSI, amd64.R11, 24)
		a.MovImm64(amd64.RAX, ^uint64(abi.FuncRefHomeTagMask))
		a.AluRR(0x21, amd64.RSI, amd64.RAX, true)
		a.Load64(amd64.R8, amd64.R11, 32)
		a.Load64(amd64.R8, amd64.R8, 32)
		a.Load64(amd64.R9, amd64.RBX, -int32(abi.FuncRefDescPtrOffset))
		a.Load64(amd64.R9, amd64.R9, 32)
		amd64CopyDraglineInstanceContext(a, amd64.RSI, amd64.R8)
		amd64CopyDraglineExecutionControl(a, amd64.RSI)
		a.Push(amd64.RBX)
		a.Push(amd64.R9)
		a.Push(amd64.R8)
		a.Push(amd64.RAX)
		a.CallReg(amd64.R10)
		metadata.recordSafepoint(a.Len())
		a.Pop(amd64.RAX)
		a.Pop(amd64.R8)
		a.Pop(amd64.R9)
		a.Pop(amd64.RBX)
		amd64CopyDraglineInstanceContext(a, amd64.RBX, amd64.R9)
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
	a.LoadRsp64(amd64.R10, stackOff(rhs))
	a.LoadRsp64(amd64.RAX, stackOff(lhs))
	a.AluRR(0x01, amd64.RAX, amd64.R10, false)
	a.StoreRsp64(stackOff(lhs), amd64.RAX)
	types = types[:rhs]
	types[lhs] = wasm.I32
	*stack = types
	return nil
}

func emitAMD64StackSIMD(a *amd64.Asm, descriptor wasm.SIMDInstructionDescriptor, instr railssa.StackInstr,
	stack *[]wasm.ValType, stackOff func(int) int32, cachedV func(int) (amd64.Reg, bool), reserveV func(int) amd64.Reg, loadV, storeV, loadScalar, storeScalar func(int, amd64.Reg), discardScalar func(int),
	materializeConstant func(amd64.Reg, [16]byte), shuffleConstant func(amd64.Reg, amd64.Reg, [16]byte), constants []amd64SIMDConstant, cachedMemorySize bool, function uint32, plannedCheckEnd uint64, elideBounds bool, metadata *functionEmissionMetadata,
) error {
	types := *stack
	materialize := func(reg amd64.Reg, bytes [16]byte) {
		materializeConstant(reg, bytes)
	}
	constantRegister := func(value [16]byte) (amd64.Reg, bool) {
		for _, constant := range constants {
			if constant.bytes == value {
				return constant.reg, true
			}
		}
		return 0, false
	}
	checkMemory := func(address amd64.Reg, size uint64) {
		if descriptor.MemArg.Offset > math.MaxUint64-size {
			metadata.recordTrap(a.Len(), instr.Offset, 3)
			amd64EmitTrap(a, 3, function, instr.Offset)
			return
		}
		a.MovReg32(amd64.R11, address)
		end := descriptor.MemArg.Offset + size
		if plannedCheckEnd != 0 {
			end = plannedCheckEnd
		}
		if end <= math.MaxInt32 {
			a.AluRI(0, amd64.R11, int32(end), true)
		} else {
			a.MovImm64(amd64.RAX, end)
			a.Add64(amd64.R11, amd64.RAX)
		}
		bound := amd64.RBP
		if !cachedMemorySize {
			a.Load64(amd64.RAX, amd64.RBX, -int32(abi.ActualLinMemByteSize64Offset))
			bound = amd64.RAX
		}
		a.Cmp64(amd64.R11, bound)
		inBounds := a.JccPlaceholder(amd64.CondBE)
		metadata.recordTrap(a.Len(), instr.Offset, 3)
		amd64EmitTrap(a, 3, function, instr.Offset)
		a.PatchRel32(inBounds, a.Len())
	}
	effectiveAddress := func(address amd64.Reg) {
		a.MovReg32(amd64.R10, address)
		if descriptor.MemArg.Offset <= math.MaxInt32 {
			a.LeaScaled(amd64.R10, amd64.RBX, amd64.R10, 0, int32(descriptor.MemArg.Offset))
		} else {
			a.Add64(amd64.R10, amd64.RBX)
			a.MovImm64(amd64.R11, descriptor.MemArg.Offset)
			a.Add64(amd64.R10, amd64.R11)
		}
	}
	vectorOperand := func(index int, scratch amd64.Reg) amd64.Reg {
		if reg, ok := cachedV(index); ok {
			return reg
		}
		loadV(index, scratch)
		return scratch
	}
	binaryOp := func(op func(amd64.Reg, amd64.Reg, amd64.Reg)) error {
		if len(types) < 2 || types[len(types)-2] != wasm.V128 || types[len(types)-1] != wasm.V128 {
			return fmt.Errorf("SIMD binary operand mismatch")
		}
		base := len(types) - 2
		lhs := vectorOperand(base, 0)
		rhs := vectorOperand(base+1, 1)
		op(lhs, lhs, rhs)
		storeV(base, lhs)
		types = append(types[:base], wasm.V128)
		return nil
	}
	unsignedCompare := func(words bool, less bool) error {
		return binaryOp(func(dst, lhs, rhs amd64.Reg) {
			var sign [16]byte
			for i := range sign {
				if !words || i%2 == 1 {
					sign[i] = 0x80
				}
			}
			materialize(2, sign)
			a.VPxor(lhs, lhs, 2)
			a.VPxor(rhs, rhs, 2)
			if words {
				if less {
					a.VPcmpgtw(dst, rhs, lhs)
				} else {
					a.VPcmpgtw(dst, lhs, rhs)
				}
			} else if less {
				a.VPcmpgtb(dst, rhs, lhs)
			} else {
				a.VPcmpgtb(dst, lhs, rhs)
			}
		})
	}

	switch descriptor.Kind {
	case wasm.InstrV128Const:
		if reg, ok := constantRegister(descriptor.Bytes); ok {
			storeV(len(types), reg)
		} else {
			dst := reserveV(len(types))
			materialize(dst, descriptor.Bytes)
		}
		types = append(types, wasm.V128)
	case wasm.InstrV128Load:
		if len(types) < 1 || types[len(types)-1] != wasm.I32 {
			return fmt.Errorf("SIMD load operand mismatch")
		}
		base := len(types) - 1
		loadScalar(base, amd64.R10)
		discardScalar(base)
		if !elideBounds {
			checkMemory(amd64.R10, 16)
		}
		effectiveAddress(amd64.R10)
		dst := reserveV(base)
		a.VMovdquLoadDisp(dst, amd64.R10, 0)
		types[base] = wasm.V128
	case wasm.InstrV128Store:
		if len(types) < 2 || types[len(types)-2] != wasm.I32 || types[len(types)-1] != wasm.V128 {
			return fmt.Errorf("SIMD store operand mismatch")
		}
		base := len(types) - 2
		loadScalar(base, amd64.R10)
		discardScalar(base)
		value := vectorOperand(base+1, 0)
		if !elideBounds {
			checkMemory(amd64.R10, 16)
		}
		effectiveAddress(amd64.R10)
		a.VMovdquStoreDisp(amd64.R10, 0, value)
		types = types[:base]
	case wasm.InstrV128Store64Lane:
		if len(types) < 2 || types[len(types)-2] != wasm.I32 || types[len(types)-1] != wasm.V128 {
			return fmt.Errorf("SIMD lane store operand mismatch")
		}
		base := len(types) - 2
		loadScalar(base, amd64.R10)
		discardScalar(base)
		value := vectorOperand(base+1, 0)
		if !elideBounds {
			checkMemory(amd64.R10, 8)
		}
		effectiveAddress(amd64.R10)
		a.Pextrq(amd64.R11, value, byte(descriptor.Lane))
		a.Store64(amd64.R10, 0, amd64.R11)
		types = types[:base]
	case wasm.InstrV128And:
		if err := binaryOp(a.VPand); err != nil {
			return err
		}
	case wasm.InstrV128Andnot:
		if err := binaryOp(func(dst, lhs, rhs amd64.Reg) { a.VPandn(dst, rhs, lhs) }); err != nil {
			return err
		}
	case wasm.InstrV128Or:
		if err := binaryOp(a.VPor); err != nil {
			return err
		}
	case wasm.InstrV128Xor:
		if err := binaryOp(a.VPxor); err != nil {
			return err
		}
	case wasm.InstrI8x16Eq:
		if err := binaryOp(a.VPcmpeqb); err != nil {
			return err
		}
	case wasm.InstrI16x8Eq:
		if err := binaryOp(a.VPcmpeqw); err != nil {
			return err
		}
	case wasm.InstrI8x16LtS:
		if err := binaryOp(func(dst, lhs, rhs amd64.Reg) { a.VPcmpgtb(dst, rhs, lhs) }); err != nil {
			return err
		}
	case wasm.InstrI8x16GtU:
		if err := unsignedCompare(false, false); err != nil {
			return err
		}
	case wasm.InstrI16x8LtU:
		if err := unsignedCompare(true, true); err != nil {
			return err
		}
	case wasm.InstrI16x8GtU:
		if err := unsignedCompare(true, false); err != nil {
			return err
		}
	case wasm.InstrI16x8GeU:
		if err := binaryOp(func(dst, lhs, rhs amd64.Reg) {
			a.VPmaxuw(2, lhs, rhs)
			a.VPcmpeqw(dst, 2, lhs)
		}); err != nil {
			return err
		}
	case wasm.InstrI8x16SubSatU:
		if err := binaryOp(a.VPsubusb); err != nil {
			return err
		}
	case wasm.InstrI16x8Sub:
		if err := binaryOp(a.VPsubw); err != nil {
			return err
		}
	case wasm.InstrI32x4Add:
		if err := binaryOp(a.VPaddd); err != nil {
			return err
		}
	case wasm.InstrI8x16NarrowI16x8U:
		if err := binaryOp(a.VPpackuswb); err != nil {
			return err
		}
	case wasm.InstrI16x8NarrowI32x4S:
		if err := binaryOp(a.VPpackssdw); err != nil {
			return err
		}
	case wasm.InstrI16x8NarrowI32x4U:
		if err := binaryOp(a.VPpackusdw); err != nil {
			return err
		}
	case wasm.InstrI16x8ExtmulLowI8x16U, wasm.InstrI16x8ExtmulHighI8x16U:
		if len(types) < 2 {
			return fmt.Errorf("SIMD extmul operand underflow")
		}
		base := len(types) - 2
		lhs := vectorOperand(base, 0)
		rhs := vectorOperand(base+1, 1)
		a.VPxor(2, 2, 2)
		if descriptor.Kind == wasm.InstrI16x8ExtmulHighI8x16U {
			a.VPunpckhbw(lhs, lhs, 2)
			a.VPunpckhbw(rhs, rhs, 2)
		} else {
			a.VPunpcklbw(lhs, lhs, 2)
			a.VPunpcklbw(rhs, rhs, 2)
		}
		a.VPmullw(lhs, lhs, rhs)
		storeV(base, lhs)
		types = append(types[:base], wasm.V128)
	case wasm.InstrI32x4DotI16x8S:
		if err := binaryOp(a.VPmaddwd); err != nil {
			return err
		}
	case wasm.InstrI32x4ExtaddPairwiseI16x8U:
		if len(types) < 1 {
			return fmt.Errorf("SIMD unary operand underflow")
		}
		base := len(types) - 1
		value := vectorOperand(base, 0)
		a.VPxor(2, 2, 2)
		a.VPunpckhwd(1, value, 2)
		a.VPunpcklwd(value, value, 2)
		a.VPhaddd(value, value, 1)
		storeV(base, value)
	case wasm.InstrI8x16Swizzle:
		if err := binaryOp(a.VPshufb); err != nil {
			return err
		}
	case wasm.InstrI8x16Shuffle:
		if len(types) < 2 {
			return fmt.Errorf("SIMD shuffle operand underflow")
		}
		base := len(types) - 2
		lhs := vectorOperand(base, 0)
		rhs := vectorOperand(base+1, 1)
		left, right := amd64ShuffleMasks(descriptor.Bytes)
		shuffleConstant(2, lhs, left)
		shuffleConstant(3, rhs, right)
		a.VPor(lhs, 2, 3)
		storeV(base, lhs)
		types = append(types[:base], wasm.V128)
	case wasm.InstrV128AnyTrue:
		if len(types) < 1 || types[len(types)-1] != wasm.V128 {
			return fmt.Errorf("SIMD reduction operand mismatch")
		}
		base := len(types) - 1
		value := vectorOperand(base, 0)
		a.VPtest(value, value)
		a.SetccReg(amd64.CondNE, amd64.RAX)
		storeScalar(base, amd64.RAX)
		types[base] = wasm.I32
	case wasm.InstrI8x16Bitmask, wasm.InstrI16x8Bitmask:
		if len(types) < 1 || types[len(types)-1] != wasm.V128 {
			return fmt.Errorf("SIMD bitmask operand mismatch")
		}
		base := len(types) - 1
		value := vectorOperand(base, 0)
		if descriptor.Kind == wasm.InstrI16x8Bitmask {
			a.VPacksswb(value, value, value)
		}
		a.VPmovmskb(amd64.RAX, value)
		if descriptor.Kind == wasm.InstrI16x8Bitmask {
			a.AluRI(4, amd64.RAX, 0xff, false)
		}
		storeScalar(base, amd64.RAX)
		types[base] = wasm.I32
	case wasm.InstrI32x4ExtractLane:
		if len(types) < 1 || types[len(types)-1] != wasm.V128 {
			return fmt.Errorf("SIMD extract operand mismatch")
		}
		base := len(types) - 1
		value := vectorOperand(base, 0)
		a.Pextrd(amd64.RAX, value, byte(descriptor.Lane))
		storeScalar(base, amd64.RAX)
		types[base] = wasm.I32
	case wasm.InstrI32x4Splat:
		if len(types) < 1 || types[len(types)-1] != wasm.I32 {
			return fmt.Errorf("SIMD splat operand mismatch")
		}
		base := len(types) - 1
		loadScalar(base, amd64.RAX)
		discardScalar(base)
		dst := reserveV(base)
		a.MovGprToXmm(dst, amd64.RAX, false)
		a.Pshufd(dst, dst, 0)
		types[base] = wasm.V128
	case wasm.InstrI32x4ReplaceLane:
		if len(types) < 2 || types[len(types)-2] != wasm.V128 || types[len(types)-1] != wasm.I32 {
			return fmt.Errorf("SIMD replace operand mismatch")
		}
		base := len(types) - 2
		value := vectorOperand(base, 0)
		loadScalar(base+1, amd64.RAX)
		discardScalar(base + 1)
		a.Pinsrd(value, amd64.RAX, byte(descriptor.Lane))
		storeV(base, value)
		types = append(types[:base], wasm.V128)
	case wasm.InstrI16x8Shl, wasm.InstrI16x8ShrU, wasm.InstrI32x4Shl, wasm.InstrI32x4ShrU:
		if len(types) < 2 || types[len(types)-2] != wasm.V128 || types[len(types)-1] != wasm.I32 {
			return fmt.Errorf("SIMD shift operand mismatch")
		}
		base := len(types) - 2
		value := vectorOperand(base, 0)
		loadScalar(base+1, amd64.RAX)
		discardScalar(base + 1)
		mask := int32(15)
		if descriptor.Kind == wasm.InstrI32x4Shl || descriptor.Kind == wasm.InstrI32x4ShrU {
			mask = 31
		}
		a.AluRI(4, amd64.RAX, mask, false)
		a.MovGprToXmm(1, amd64.RAX, true)
		switch descriptor.Kind {
		case wasm.InstrI16x8Shl:
			a.VPsllw(value, value, 1)
		case wasm.InstrI16x8ShrU:
			a.VPsrlw(value, value, 1)
		case wasm.InstrI32x4Shl:
			a.VPslld(value, value, 1)
		case wasm.InstrI32x4ShrU:
			a.VPsrld(value, value, 1)
		}
		storeV(base, value)
		types = append(types[:base], wasm.V128)
	default:
		return fmt.Errorf("unsupported structured SIMD instruction %s", descriptor.Kind)
	}
	*stack = types
	return nil
}

func amd64ShuffleMasks(lanes [16]byte) (left, right [16]byte) {
	for i, lane := range lanes {
		left[i], right[i] = 0x80, 0x80
		if lane < 16 {
			left[i] = lane
		} else {
			right[i] = lane - 16
		}
	}
	return left, right
}

func amd64CopyDraglineExecutionControl(a *amd64.Asm, targetLinMem amd64.Reg) {
	for _, off := range [...]int32{24, 72, int32(abi.TrapCellPtrOffset)} {
		a.Load64(amd64.RAX, amd64.RBX, -off)
		a.Store64(targetLinMem, -off, amd64.RAX)
	}
}

func amd64CopyDraglineInstanceContext(a *amd64.Asm, targetLinMem, context amd64.Reg) {
	for i, off := range [...]int32{40, 80, 88, 120, 112, 128, 96, 64, 136} {
		a.Load64(amd64.RAX, context, int32(i*8))
		a.Store64(targetLinMem, -off, amd64.RAX)
	}
	a.Load64(amd64.RAX, context, coreruntime.InstanceContextProfileCountersOffset)
	a.Store64(targetLinMem, -int32(abi.ProfileCountersPtrOffset), amd64.RAX)
	a.Load64(amd64.RAX, context, coreruntime.InstanceContextTierEntriesOffset)
	a.Store64(targetLinMem, -int32(abi.TierEntriesPtrOffset), amd64.RAX)
}

func emitAMD64StackMemory(a *amd64.Asm, instr railssa.StackInstr, stack *[]wasm.ValType,
	load, storeValue func(int, amd64.Reg), discard func(int), cachedMemorySize bool, function uint32, plannedCheckEnd uint64, elideBounds bool, metadata *functionEmissionMetadata,
) error {
	types := *stack
	store := instr.Kind >= wasm.InstrI32Store && instr.Kind <= wasm.InstrI64Store32
	need := 1
	if store {
		need = 2
	}
	if len(types) < need {
		return fmt.Errorf("operand stack underflow")
	}
	size, typ, signed := 4, wasm.I32, false
	switch instr.Kind {
	case wasm.InstrI64Load, wasm.InstrI64Store:
		size, typ = 8, wasm.I64
	case wasm.InstrF32Load, wasm.InstrF32Store:
		typ = wasm.F32
	case wasm.InstrF64Load, wasm.InstrF64Store:
		size, typ = 8, wasm.F64
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
	addrIndex := len(types) - 1
	if store {
		addrIndex--
	}
	load(addrIndex, amd64.RAX)
	if !elideBounds {
		a.MovReg64(amd64.R10, amd64.RAX)
		end := uint64(instr.U32()) + uint64(size)
		if plannedCheckEnd != 0 {
			end = plannedCheckEnd
		}
		if end <= math.MaxInt32 {
			a.AluRI(0, amd64.R10, int32(end), true)
		} else {
			a.MovImm64(amd64.R11, end)
			a.Add64(amd64.R10, amd64.R11)
		}
		bound := amd64.RBP
		if !cachedMemorySize {
			a.Load64(amd64.R11, amd64.RBX, -int32(abi.ActualLinMemByteSize64Offset))
			bound = amd64.R11
		}
		a.Cmp64(amd64.R10, bound)
		inBounds := a.JccPlaceholder(amd64.CondBE)
		metadata.recordTrap(a.Len(), instr.Offset, 3)
		amd64EmitTrap(a, 3, function, instr.Offset)
		a.PatchRel32(inBounds, a.Len())
	}
	disp := int32(instr.U32())
	if store {
		valueIndex := len(types) - 1
		load(valueIndex, amd64.R10)
		discard(valueIndex)
		discard(addrIndex)
		if typ == wasm.F32 || typ == wasm.F64 {
			a.MovGprToXmm(0, amd64.R10, typ == wasm.F64)
			a.FStoreIdx(amd64.RBX, amd64.RAX, 0, disp, typ == wasm.F64)
		} else {
			a.StoreIdx(amd64.RBX, amd64.RAX, amd64.R10, disp, size)
		}
		*stack = types[:addrIndex]
		return nil
	}
	if typ == wasm.F32 || typ == wasm.F64 {
		a.FLoadIdx(0, amd64.RBX, amd64.RAX, disp, typ == wasm.F64)
		a.MovXmmToGpr(amd64.R10, 0, typ == wasm.F64)
	} else {
		a.LoadIdx(amd64.R10, amd64.RBX, amd64.RAX, disp, size, signed, typ == wasm.I64)
	}
	discard(addrIndex)
	storeValue(addrIndex, amd64.R10)
	types[addrIndex] = typ
	*stack = types
	return nil
}

func emitAMD64StackFloat(a *amd64.Asm, kind wasm.InstrKind, stack *[]wasm.ValType,
	load, store func(int, amd64.Reg), discard func(int), function, wasmOffset uint32, metadata *functionEmissionMetadata,
) error {
	types := *stack
	if len(types) < 1 {
		return fmt.Errorf("operand stack underflow")
	}
	top := len(types) - 1
	load(top, amd64.RAX)
	if kind >= wasm.InstrF32Eq && kind <= wasm.InstrF64Ge {
		if len(types) < 2 {
			return fmt.Errorf("operand stack underflow")
		}
		lhs := len(types) - 2
		f64 := kind >= wasm.InstrF64Eq
		load(lhs, amd64.R10)
		discard(top)
		a.MovGprToXmm(0, amd64.R10, f64)
		a.MovGprToXmm(1, amd64.RAX, f64)
		a.Ucomis(0, 1, f64)
		unordered := a.JccPlaceholder(amd64.CondP)
		cond := amd64.CondE
		switch kind {
		case wasm.InstrF32Ne, wasm.InstrF64Ne:
			cond = amd64.CondNE
		case wasm.InstrF32Lt, wasm.InstrF64Lt:
			cond = amd64.CondB
		case wasm.InstrF32Gt, wasm.InstrF64Gt:
			cond = amd64.CondA
		case wasm.InstrF32Le, wasm.InstrF64Le:
			cond = amd64.CondBE
		case wasm.InstrF32Ge, wasm.InstrF64Ge:
			cond = amd64.CondAE
		}
		a.SetccReg(cond, amd64.RAX)
		orderedDone := a.JmpPlaceholder()
		a.PatchRel32(unordered, a.Len())
		if kind == wasm.InstrF32Ne || kind == wasm.InstrF64Ne {
			a.MovImm32(amd64.RAX, 1)
		} else {
			a.XorSelf32(amd64.RAX)
		}
		a.PatchRel32(orderedDone, a.Len())
		store(lhs, amd64.RAX)
		types[lhs] = wasm.I32
		*stack = types[:top]
		return nil
	}
	switch kind {
	case wasm.InstrI32WrapI64:
		a.MovReg32(amd64.RAX, amd64.RAX)
		types[top] = wasm.I32
	case wasm.InstrI64ExtendI32S:
		a.Movsxd(amd64.RAX, amd64.RAX)
		types[top] = wasm.I64
	case wasm.InstrI64ExtendI32U:
		a.MovReg32(amd64.RAX, amd64.RAX)
		types[top] = wasm.I64
	case wasm.InstrI32ReinterpretF32:
		types[top] = wasm.I32
	case wasm.InstrI64ReinterpretF64:
		types[top] = wasm.I64
	case wasm.InstrF32ReinterpretI32:
		types[top] = wasm.F32
	case wasm.InstrF64ReinterpretI64:
		types[top] = wasm.F64
	case wasm.InstrF32ConvertI32S, wasm.InstrF32ConvertI32U, wasm.InstrF64ConvertI32S, wasm.InstrF64ConvertI32U:
		f64 := kind == wasm.InstrF64ConvertI32S || kind == wasm.InstrF64ConvertI32U
		unsigned := kind == wasm.InstrF32ConvertI32U || kind == wasm.InstrF64ConvertI32U
		a.Cvtsi2f(0, amd64.RAX, f64, unsigned)
		a.MovXmmToGpr(amd64.RAX, 0, f64)
		if f64 {
			types[top] = wasm.F64
		} else {
			types[top] = wasm.F32
		}
	case wasm.InstrF32ConvertI64S, wasm.InstrF32ConvertI64U, wasm.InstrF64ConvertI64S, wasm.InstrF64ConvertI64U:
		f64 := kind == wasm.InstrF64ConvertI64S || kind == wasm.InstrF64ConvertI64U
		unsigned := kind == wasm.InstrF32ConvertI64U || kind == wasm.InstrF64ConvertI64U
		if unsigned {
			a.TestSelf(amd64.RAX, true)
			large := a.JccPlaceholder(amd64.CondS)
			a.Cvtsi2f(0, amd64.RAX, f64, true)
			done := a.JmpPlaceholder()
			a.PatchRel32(large, a.Len())
			a.MovReg64(amd64.R10, amd64.RAX)
			a.ShiftImm(5, amd64.R10, 1, true)
			a.AluRI(4, amd64.RAX, 1, true)
			a.AluRR(0x09, amd64.R10, amd64.RAX, true)
			a.Cvtsi2f(0, amd64.R10, f64, true)
			a.FAdd(0, 0, f64)
			a.PatchRel32(done, a.Len())
		} else {
			a.Cvtsi2f(0, amd64.RAX, f64, true)
		}
		a.MovXmmToGpr(amd64.RAX, 0, f64)
		if f64 {
			types[top] = wasm.F64
		} else {
			types[top] = wasm.F32
		}
	case wasm.InstrF32DemoteF64:
		a.MovGprToXmm(0, amd64.RAX, true)
		a.Cvtsd2ss(0, 0)
		a.MovXmmToGpr(amd64.RAX, 0, false)
		types[top] = wasm.F32
	case wasm.InstrF64PromoteF32:
		a.MovGprToXmm(0, amd64.RAX, false)
		a.Cvtss2sd(0, 0)
		a.MovXmmToGpr(amd64.RAX, 0, true)
		types[top] = wasm.F64
	case wasm.InstrI32TruncF32S, wasm.InstrI32TruncF32U, wasm.InstrI32TruncF64S, wasm.InstrI32TruncF64U:
		f64 := kind == wasm.InstrI32TruncF64S || kind == wasm.InstrI32TruncF64U
		unsigned := kind == wasm.InstrI32TruncF32U || kind == wasm.InstrI32TruncF64U
		a.MovGprToXmm(0, amd64.RAX, f64)
		emitAMD64TruncI32(a, 0, f64, unsigned, function, wasmOffset, metadata)
		types[top] = wasm.I32
	case wasm.InstrI64TruncF32S, wasm.InstrI64TruncF32U, wasm.InstrI64TruncF64S, wasm.InstrI64TruncF64U:
		f64 := kind == wasm.InstrI64TruncF64S || kind == wasm.InstrI64TruncF64U
		unsigned := kind == wasm.InstrI64TruncF32U || kind == wasm.InstrI64TruncF64U
		a.MovGprToXmm(0, amd64.RAX, f64)
		emitAMD64TruncI64(a, 0, f64, unsigned, function, wasmOffset, metadata)
		types[top] = wasm.I64
	case wasm.InstrI32TruncSatF32S, wasm.InstrI32TruncSatF32U, wasm.InstrI32TruncSatF64S, wasm.InstrI32TruncSatF64U:
		f64 := kind == wasm.InstrI32TruncSatF64S || kind == wasm.InstrI32TruncSatF64U
		unsigned := kind == wasm.InstrI32TruncSatF32U || kind == wasm.InstrI32TruncSatF64U
		a.MovGprToXmm(0, amd64.RAX, f64)
		emitAMD64TruncSatI32(a, 0, f64, unsigned)
		types[top] = wasm.I32
	case wasm.InstrI64TruncSatF32S, wasm.InstrI64TruncSatF32U, wasm.InstrI64TruncSatF64S, wasm.InstrI64TruncSatF64U:
		f64 := kind == wasm.InstrI64TruncSatF64S || kind == wasm.InstrI64TruncSatF64U
		unsigned := kind == wasm.InstrI64TruncSatF32U || kind == wasm.InstrI64TruncSatF64U
		a.MovGprToXmm(0, amd64.RAX, f64)
		emitAMD64TruncSatI64(a, 0, f64, unsigned)
		types[top] = wasm.I64
	default:
		f64 := kind >= wasm.InstrF64Abs && kind <= wasm.InstrF64Copysign
		a.MovGprToXmm(0, amd64.RAX, f64)
		switch kind {
		case wasm.InstrF32Abs, wasm.InstrF64Abs:
			a.ShiftImm(4, amd64.RAX, 1, f64)
			a.ShiftImm(5, amd64.RAX, 1, f64)
		case wasm.InstrF32Neg, wasm.InstrF64Neg:
			if f64 {
				a.MovImm64(amd64.R10, uint64(1)<<63)
			} else {
				a.MovImm32(amd64.R10, int32(-2147483648))
			}
			a.AluRR(0x31, amd64.RAX, amd64.R10, f64)
		case wasm.InstrF32Ceil, wasm.InstrF64Ceil:
			a.Round(0, 0, f64, 2|8)
			a.MovXmmToGpr(amd64.RAX, 0, f64)
		case wasm.InstrF32Floor, wasm.InstrF64Floor:
			a.Round(0, 0, f64, 1|8)
			a.MovXmmToGpr(amd64.RAX, 0, f64)
		case wasm.InstrF32Trunc, wasm.InstrF64Trunc:
			a.Round(0, 0, f64, 3|8)
			a.MovXmmToGpr(amd64.RAX, 0, f64)
		case wasm.InstrF32Nearest, wasm.InstrF64Nearest:
			a.Round(0, 0, f64, 0|8)
			a.MovXmmToGpr(amd64.RAX, 0, f64)
		case wasm.InstrF32Sqrt, wasm.InstrF64Sqrt:
			a.FSqrt(0, 0, f64)
			a.MovXmmToGpr(amd64.RAX, 0, f64)
		default:
			if len(types) < 2 {
				return fmt.Errorf("operand stack underflow")
			}
			lhs := len(types) - 2
			load(lhs, amd64.RAX)
			load(top, amd64.R10)
			discard(top)
			if kind == wasm.InstrF32Copysign || kind == wasm.InstrF64Copysign {
				a.ShiftImm(4, amd64.RAX, 1, f64)
				a.ShiftImm(5, amd64.RAX, 1, f64)
				if f64 {
					a.ShiftImm(5, amd64.R10, 63, true)
					a.ShiftImm(4, amd64.R10, 63, true)
				} else {
					a.ShiftImm(5, amd64.R10, 31, false)
					a.ShiftImm(4, amd64.R10, 31, false)
				}
				a.AluRR(0x09, amd64.RAX, amd64.R10, f64)
				store(lhs, amd64.RAX)
				*stack = types[:top]
				return nil
			}
			a.MovGprToXmm(0, amd64.RAX, f64)
			a.MovGprToXmm(1, amd64.R10, f64)
			switch kind {
			case wasm.InstrF32Add, wasm.InstrF64Add:
				a.FAdd(0, 1, f64)
			case wasm.InstrF32Sub, wasm.InstrF64Sub:
				a.FSub(0, 1, f64)
			case wasm.InstrF32Mul, wasm.InstrF64Mul:
				a.FMul(0, 1, f64)
			case wasm.InstrF32Div, wasm.InstrF64Div:
				a.FDiv(0, 1, f64)
			case wasm.InstrF32Min, wasm.InstrF64Min:
				emitAMD64WasmMinMax(a, 0, 1, f64, false)
			case wasm.InstrF32Max, wasm.InstrF64Max:
				emitAMD64WasmMinMax(a, 0, 1, f64, true)
			default:
				return fmt.Errorf("unsupported structured float instruction %s", kind)
			}
			a.MovXmmToGpr(amd64.RAX, 0, f64)
			store(lhs, amd64.RAX)
			*stack = types[:top]
			return nil
		}
	}
	store(top, amd64.RAX)
	*stack = types
	return nil
}

// emitAMD64WasmMinMax implements scalar Wasm min/max. SSE min/max select the
// second operand for unordered and equal inputs, which is wrong for a NaN in
// the first operand and for one ordering of signed zero. Ordered unequal values
// use the packed operation; equal values use bitwise OR/AND; unordered values
// use addition to produce an arithmetic NaN.
func emitAMD64WasmMinMax(a *amd64.Asm, lhs, rhs amd64.Reg, f64, max bool) {
	a.Ucomis(lhs, rhs, f64)
	unordered := a.JccPlaceholder(amd64.CondP)
	distinct := a.JccPlaceholder(amd64.CondNE)
	prefix, bitOp := byte(0), byte(0x56) // orps: min(+0, -0) = -0
	if f64 {
		prefix = 0x66
	}
	if max {
		bitOp = 0x54 // andps: max(+0, -0) = +0
	}
	a.SseRR(prefix, bitOp, lhs, rhs, false)
	equalDone := a.JmpPlaceholder()
	a.PatchRel32(distinct, a.Len())
	if max {
		a.SseRR(prefix, 0x5f, lhs, rhs, false)
	} else {
		a.SseRR(prefix, 0x5d, lhs, rhs, false)
	}
	distinctDone := a.JmpPlaceholder()
	a.PatchRel32(unordered, a.Len())
	a.FAdd(lhs, rhs, f64)
	a.PatchRel32(equalDone, a.Len())
	a.PatchRel32(distinctDone, a.Len())
}

func emitAMD64FloatBits(a *amd64.Asm, xmm amd64.Reg, bits uint64, f64 bool) {
	if bits == 0 {
		a.VPxor(xmm, xmm, xmm)
		return
	}
	if f64 {
		a.MovImm64(amd64.R10, bits)
	} else {
		a.MovImm32(amd64.R10, int32(bits))
	}
	a.MovGprToXmm(xmm, amd64.R10, f64)
}

func emitAMD64TruncI32(a *amd64.Asm, src amd64.Reg, f64, unsigned bool, function, wasmOffset uint32, metadata *functionEmissionMetadata) {
	// Reject NaNs before ordered range comparisons.
	a.Ucomis(src, src, f64)
	ordered := a.JccPlaceholder(amd64.CondNP)
	metadata.recordTrap(a.Len(), wasmOffset, 11)
	amd64EmitTrap(a, 11, function, wasmOffset)
	a.PatchRel32(ordered, a.Len())

	upper := uint64(0x4f000000) // 2^31 as f32
	if unsigned {
		upper = 0x4f800000 // 2^32 as f32
	}
	if f64 {
		upper = 0x41e0000000000000 // 2^31 as f64
		if unsigned {
			upper = 0x41f0000000000000 // 2^32 as f64
		}
	}
	emitAMD64FloatBits(a, 1, upper, f64)
	a.Ucomis(src, 1, f64)
	belowUpper := a.JccPlaceholder(amd64.CondB)
	metadata.recordTrap(a.Len(), wasmOffset, 11)
	amd64EmitTrap(a, 11, function, wasmOffset)
	a.PatchRel32(belowUpper, a.Len())

	lower := uint64(0xcf000000) // -2^31 as f32
	lowerCond := amd64.CondAE
	if unsigned {
		lower = 0xbf800000 // -1 as f32
		lowerCond = amd64.CondA
	} else if f64 {
		lower = 0xc1e0000000200000 // -2147483649 as f64
		lowerCond = amd64.CondA
	}
	if f64 && unsigned {
		lower = 0xbff0000000000000 // -1 as f64
	}
	emitAMD64FloatBits(a, 1, lower, f64)
	a.Ucomis(src, 1, f64)
	aboveLower := a.JccPlaceholder(lowerCond)
	metadata.recordTrap(a.Len(), wasmOffset, 11)
	amd64EmitTrap(a, 11, function, wasmOffset)
	a.PatchRel32(aboveLower, a.Len())

	a.Cvttf2si(amd64.RAX, src, f64, unsigned)
	a.MovReg32(amd64.RAX, amd64.RAX)
}

func emitAMD64TruncI64(a *amd64.Asm, src amd64.Reg, f64, unsigned bool, function, wasmOffset uint32, metadata *functionEmissionMetadata) {
	a.Ucomis(src, src, f64)
	ordered := a.JccPlaceholder(amd64.CondNP)
	metadata.recordTrap(a.Len(), wasmOffset, 11)
	amd64EmitTrap(a, 11, function, wasmOffset)
	a.PatchRel32(ordered, a.Len())
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
	emitAMD64FloatBits(a, 1, minBits, f64)
	a.Ucomis(src, 1, f64)
	aboveLower := a.JccPlaceholder(amd64.CondA)
	metadata.recordTrap(a.Len(), wasmOffset, 11)
	amd64EmitTrap(a, 11, function, wasmOffset)
	a.PatchRel32(aboveLower, a.Len())
	emitAMD64FloatBits(a, 1, maxBits, f64)
	a.Ucomis(src, 1, f64)
	belowUpper := a.JccPlaceholder(amd64.CondB)
	metadata.recordTrap(a.Len(), wasmOffset, 11)
	amd64EmitTrap(a, 11, function, wasmOffset)
	a.PatchRel32(belowUpper, a.Len())
	if !unsigned {
		a.Cvttf2si(amd64.RAX, src, f64, true)
		return
	}
	emitAMD64FloatBits(a, 1, func() uint64 {
		if f64 {
			return 0x43e0000000000000
		}
		return 0x5f000000
	}(), f64)
	a.Ucomis(src, 1, f64)
	small := a.JccPlaceholder(amd64.CondB)
	a.FSub(src, 1, f64)
	a.Cvttf2si(amd64.RAX, src, f64, true)
	a.MovImm64(amd64.R10, uint64(1)<<63)
	a.Add64(amd64.RAX, amd64.R10)
	done := a.JmpPlaceholder()
	a.PatchRel32(small, a.Len())
	a.Cvttf2si(amd64.RAX, src, f64, true)
	a.PatchRel32(done, a.Len())
}

// emitAMD64TruncSatI32 implements all four i32 saturating conversions. CVTT
// supplies the in-range signed result and the negative signed clamp; explicit
// ordered comparisons distinguish NaN, positive overflow, and the u32 range.
func emitAMD64TruncSatI32(a *amd64.Asm, src amd64.Reg, f64, unsigned bool) {
	if unsigned {
		a.Cvttf2si(amd64.RAX, src, f64, true)
		emitAMD64FloatBits(a, 1, 0, f64)
		a.Ucomis(src, 1, f64)
		positive := a.JccPlaceholder(amd64.CondA)
		a.XorSelf32(amd64.RAX)
		nonpositiveDone := a.JmpPlaceholder()
		a.PatchRel32(positive, a.Len())
		if f64 {
			emitAMD64FloatBits(a, 1, 0x41f0000000000000, true) // 2^32
		} else {
			emitAMD64FloatBits(a, 1, 0x4f800000, false)
		}
		a.Ucomis(src, 1, f64)
		inRange := a.JccPlaceholder(amd64.CondB)
		a.MovImm32(amd64.RAX, -1)
		a.PatchRel32(inRange, a.Len())
		a.PatchRel32(nonpositiveDone, a.Len())
		a.MovReg32(amd64.RAX, amd64.RAX)
		return
	}
	a.Cvttf2si(amd64.RAX, src, f64, false)
	a.Ucomis(src, src, f64)
	ordered := a.JccPlaceholder(amd64.CondNP)
	a.XorSelf32(amd64.RAX)
	nanDone := a.JmpPlaceholder()
	a.PatchRel32(ordered, a.Len())
	if f64 {
		emitAMD64FloatBits(a, 1, 0x41e0000000000000, true) // 2^31
	} else {
		emitAMD64FloatBits(a, 1, 0x4f000000, false)
	}
	a.Ucomis(src, 1, f64)
	below := a.JccPlaceholder(amd64.CondB)
	a.MovImm32(amd64.RAX, 0x7fffffff)
	a.PatchRel32(below, a.Len())
	a.PatchRel32(nanDone, a.Len())
}

func emitAMD64TruncSatI64(a *amd64.Asm, src amd64.Reg, f64, unsigned bool) {
	if !unsigned {
		a.Cvttf2si(amd64.RAX, src, f64, true)
		a.Ucomis(src, src, f64)
		ordered := a.JccPlaceholder(amd64.CondNP)
		a.XorSelf32(amd64.RAX)
		nanDone := a.JmpPlaceholder()
		a.PatchRel32(ordered, a.Len())
		p63 := uint64(0x5f000000)
		if f64 {
			p63 = 0x43e0000000000000
		}
		emitAMD64FloatBits(a, 1, p63, f64)
		a.Ucomis(src, 1, f64)
		below := a.JccPlaceholder(amd64.CondB)
		a.MovImm64(amd64.RAX, math.MaxInt64)
		a.PatchRel32(below, a.Len())
		a.PatchRel32(nanDone, a.Len())
		return
	}
	emitAMD64FloatBits(a, 1, 0, f64)
	a.Ucomis(src, 1, f64)
	positive := a.JccPlaceholder(amd64.CondA)
	a.XorSelf32(amd64.RAX)
	nonpositiveDone := a.JmpPlaceholder()
	a.PatchRel32(positive, a.Len())
	p64 := uint64(0x5f800000)
	p63 := uint64(0x5f000000)
	if f64 {
		p64 = 0x43f0000000000000
		p63 = 0x43e0000000000000
	}
	emitAMD64FloatBits(a, 1, p64, f64)
	a.Ucomis(src, 1, f64)
	inRange := a.JccPlaceholder(amd64.CondB)
	a.MovImm64(amd64.RAX, math.MaxUint64)
	maxDone := a.JmpPlaceholder()
	a.PatchRel32(inRange, a.Len())
	emitAMD64FloatBits(a, 1, p63, f64)
	a.Ucomis(src, 1, f64)
	small := a.JccPlaceholder(amd64.CondB)
	a.FSub(src, 1, f64)
	a.Cvttf2si(amd64.RAX, src, f64, true)
	a.MovImm64(amd64.R10, uint64(1)<<63)
	a.Add64(amd64.RAX, amd64.R10)
	biasDone := a.JmpPlaceholder()
	a.PatchRel32(small, a.Len())
	a.Cvttf2si(amd64.RAX, src, f64, true)
	a.PatchRel32(biasDone, a.Len())
	a.PatchRel32(maxDone, a.Len())
	a.PatchRel32(nonpositiveDone, a.Len())
}

func emitAMD64StackInteger(a *amd64.Asm, kind wasm.InstrKind, stack *[]wasm.ValType,
	source func(int, amd64.Reg) amd64.Reg, store func(int, amd64.Reg), discard func(int), function, wasmOffset uint32, metadata *functionEmissionMetadata,
) error {
	types := *stack
	wide := kind >= wasm.InstrI64Eqz && kind <= wasm.InstrI64GeU ||
		kind >= wasm.InstrI64Clz && kind <= wasm.InstrI64Rotr
	if len(types) < 1 {
		return fmt.Errorf("operand stack underflow")
	}
	top := len(types) - 1
	value := source(top, amd64.RAX)
	switch kind {
	case wasm.InstrI64ExtendI32S:
		a.Movsxd(value, value)
		types[top] = wasm.I64
		store(top, value)
		*stack = types
		return nil
	case wasm.InstrI32Extend8S, wasm.InstrI64Extend8S:
		a.Movsx8(value, value, kind == wasm.InstrI64Extend8S)
		store(top, value)
		return nil
	case wasm.InstrI32Extend16S, wasm.InstrI64Extend16S:
		a.Movsx16(value, value, kind == wasm.InstrI64Extend16S)
		store(top, value)
		return nil
	case wasm.InstrI64Extend32S:
		a.Movsxd(value, value)
		store(top, value)
		return nil
	case wasm.InstrI32Eqz, wasm.InstrI64Eqz:
		a.TestSelf(value, wide)
		a.SetccReg(amd64.CondE, value)
		types[top] = wasm.I32
		store(top, value)
		*stack = types
		return nil
	case wasm.InstrI32Clz, wasm.InstrI64Clz:
		a.Lzcnt(value, value, wide)
		store(top, value)
		return nil
	case wasm.InstrI32Ctz, wasm.InstrI64Ctz:
		a.Tzcnt(value, value, wide)
		store(top, value)
		return nil
	case wasm.InstrI32Popcnt, wasm.InstrI64Popcnt:
		a.Popcnt(value, value, wide)
		store(top, value)
		return nil
	}
	if len(types) < 2 {
		return fmt.Errorf("operand stack underflow")
	}
	lhsIndex, rhsIndex := len(types)-2, len(types)-1
	lhs := source(lhsIndex, amd64.RAX)
	rhs := source(rhsIndex, amd64.R10)
	discard(rhsIndex)
	switch kind {
	case wasm.InstrI32DivS, wasm.InstrI64DivS, wasm.InstrI32RemS, wasm.InstrI64RemS,
		wasm.InstrI32DivU, wasm.InstrI64DivU, wasm.InstrI32RemU, wasm.InstrI64RemU:
		if lhs != amd64.RAX {
			a.MovReg64(amd64.RAX, lhs)
			lhs = amd64.RAX
		}
		if rhs != amd64.R10 {
			a.MovReg64(amd64.R10, rhs)
			rhs = amd64.R10
		}
	}
	switch kind {
	case wasm.InstrI32Eq, wasm.InstrI64Eq, wasm.InstrI32Ne, wasm.InstrI64Ne,
		wasm.InstrI32LtS, wasm.InstrI64LtS, wasm.InstrI32LtU, wasm.InstrI64LtU,
		wasm.InstrI32GtS, wasm.InstrI64GtS, wasm.InstrI32GtU, wasm.InstrI64GtU,
		wasm.InstrI32LeS, wasm.InstrI64LeS, wasm.InstrI32LeU, wasm.InstrI64LeU,
		wasm.InstrI32GeS, wasm.InstrI64GeS, wasm.InstrI32GeU, wasm.InstrI64GeU:
		if wide {
			a.Cmp64(lhs, rhs)
		} else {
			a.Cmp32(lhs, rhs)
		}
		cond := amd64.CondE
		switch kind {
		case wasm.InstrI32Ne, wasm.InstrI64Ne:
			cond = amd64.CondNE
		case wasm.InstrI32LtS, wasm.InstrI64LtS:
			cond = amd64.CondL
		case wasm.InstrI32LtU, wasm.InstrI64LtU:
			cond = amd64.CondB
		case wasm.InstrI32GtS, wasm.InstrI64GtS:
			cond = amd64.CondG
		case wasm.InstrI32GtU, wasm.InstrI64GtU:
			cond = amd64.CondA
		case wasm.InstrI32LeS, wasm.InstrI64LeS:
			cond = amd64.CondLE
		case wasm.InstrI32LeU, wasm.InstrI64LeU:
			cond = amd64.CondBE
		case wasm.InstrI32GeS, wasm.InstrI64GeS:
			cond = amd64.CondGE
		case wasm.InstrI32GeU, wasm.InstrI64GeU:
			cond = amd64.CondAE
		}
		a.SetccReg(cond, lhs)
		types[lhsIndex] = wasm.I32
	case wasm.InstrI32Add, wasm.InstrI64Add:
		a.AluRR(0x01, lhs, rhs, wide)
	case wasm.InstrI32Sub, wasm.InstrI64Sub:
		a.AluRR(0x29, lhs, rhs, wide)
	case wasm.InstrI32Mul, wasm.InstrI64Mul:
		a.ImulRR(lhs, rhs, wide)
	case wasm.InstrI32And, wasm.InstrI64And:
		a.AluRR(0x21, lhs, rhs, wide)
	case wasm.InstrI32Or, wasm.InstrI64Or:
		a.AluRR(0x09, lhs, rhs, wide)
	case wasm.InstrI32Xor, wasm.InstrI64Xor:
		a.AluRR(0x31, lhs, rhs, wide)
	case wasm.InstrI32Shl, wasm.InstrI64Shl, wasm.InstrI32ShrS, wasm.InstrI64ShrS,
		wasm.InstrI32ShrU, wasm.InstrI64ShrU, wasm.InstrI32Rotl, wasm.InstrI64Rotl,
		wasm.InstrI32Rotr, wasm.InstrI64Rotr:
		a.MovReg64(amd64.RCX, rhs)
		digit := byte(4)
		switch kind {
		case wasm.InstrI32ShrS, wasm.InstrI64ShrS:
			digit = 7
		case wasm.InstrI32ShrU, wasm.InstrI64ShrU:
			digit = 5
		case wasm.InstrI32Rotl, wasm.InstrI64Rotl:
			digit = 0
		case wasm.InstrI32Rotr, wasm.InstrI64Rotr:
			digit = 1
		}
		a.ShiftCL(digit, lhs, wide)
	case wasm.InstrI32DivS, wasm.InstrI64DivS, wasm.InstrI32RemS, wasm.InstrI64RemS:
		amd64TrapDivZero(a, rhs, wide, function, wasmOffset, metadata)
		a.AluRI(7, rhs, -1, wide)
		notMinusOne := a.JccPlaceholder(amd64.CondNE)
		if lhs != amd64.RAX {
			a.MovReg64(amd64.RAX, lhs)
		}
		var done int
		if kind == wasm.InstrI32DivS || kind == wasm.InstrI64DivS {
			// x / -1 is exactly -x. NEG also identifies INT_MIN via OF, avoiding
			// x86's hardware #DE and routing the Wasm overflow through our trap cell.
			a.Neg(amd64.RAX, wide)
			notOverflow := a.JccPlaceholder(amd64.CondNO)
			metadata.recordTrap(a.Len(), wasmOffset, 10)
			amd64EmitTrap(a, 10, function, wasmOffset)
			a.PatchRel32(notOverflow, a.Len())
			done = a.JmpPlaceholder()
		} else {
			// Wasm defines INT_MIN % -1 (and every x % -1) as zero; IDIV would
			// raise #DE for INT_MIN, so handle the complete -1 case directly.
			a.XorSelf32(amd64.RDX)
			done = a.JmpPlaceholder()
		}
		a.PatchRel32(notMinusOne, a.Len())
		a.Cdq(wide)
		a.Idiv(rhs, wide)
		a.PatchRel32(done, a.Len())
		if kind == wasm.InstrI32RemS || kind == wasm.InstrI64RemS {
			a.MovReg64(amd64.RAX, amd64.RDX)
		}
		lhs = amd64.RAX
	case wasm.InstrI32DivU, wasm.InstrI64DivU, wasm.InstrI32RemU, wasm.InstrI64RemU:
		amd64TrapDivZero(a, rhs, wide, function, wasmOffset, metadata)
		if lhs != amd64.RAX {
			a.MovReg64(amd64.RAX, lhs)
		}
		a.XorSelf32(amd64.RDX)
		a.Div(rhs, wide)
		if kind == wasm.InstrI32RemU || kind == wasm.InstrI64RemU {
			a.MovReg64(amd64.RAX, amd64.RDX)
		}
		lhs = amd64.RAX
	default:
		return fmt.Errorf("unsupported structured integer instruction %s", kind)
	}
	store(lhsIndex, lhs)
	*stack = types[:len(types)-1]
	return nil
}

func amd64TrapDivZero(a *amd64.Asm, divisor amd64.Reg, wide bool, function, wasmOffset uint32, metadata *functionEmissionMetadata) {
	a.TestSelf(divisor, wide)
	nonzero := a.JccPlaceholder(amd64.CondNE)
	metadata.recordTrap(a.Len(), wasmOffset, 9)
	amd64EmitTrap(a, 9, function, wasmOffset)
	a.PatchRel32(nonzero, a.Len())
}

func amd64EmitTrap(a *amd64.Asm, code, function, wasmOffset uint32) {
	a.Load64(amd64.RSI, amd64.RBX, -int32(abi.TrapCellPtrOffset))
	a.StoreImm32Mem(amd64.RSI, 16, int32(function+1))
	a.StoreImm32Mem(amd64.RSI, 20, int32(wasmOffset))
	a.StoreImm32Mem(amd64.RSI, 0, int32(code))
	a.Load64(amd64.RSP, amd64.RBX, -24)
	a.Ret()
}

func amd64Destination(loc location) (amd64.Reg, bool) {
	if loc.kind == locationRegister {
		return amd64ValueRegisters[loc.index], false
	}
	return amd64.R10, true
}

func amd64Source(a *amd64.Asm, loc location, scratch amd64.Reg) (amd64.Reg, error) {
	if loc.kind == locationRegister {
		return amd64ValueRegisters[loc.index], nil
	}
	if loc.kind != locationSpill {
		return 0, fmt.Errorf("invalid value location")
	}
	a.LoadRsp64(scratch, int32(loc.index)*8)
	return scratch, nil
}
