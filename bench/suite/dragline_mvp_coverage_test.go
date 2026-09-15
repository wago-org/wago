package wagobench

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	wago "github.com/wago-org/wago"
	"github.com/wago-org/wago/src/core/compiler/backend/dragline/railmach"
	"github.com/wago-org/wago/src/core/compiler/backend/dragline/railspec"
	"github.com/wago-org/wago/src/core/compiler/backend/dragline/railssa"
	"github.com/wago-org/wago/src/core/compiler/wasm"
)

// TestDraglineMVPCoverage compiles every valid module emitted by the pinned
// pre-reference-types spec corpus through strict Dragline. It is opt-in while
// coverage is incomplete because its failure list is the implementation ledger:
//
//	WAGO_DRAGLINE_MVP_COVERAGE=1 go test -run TestDraglineMVPCoverage
func TestDraglineMVPCoverage(t *testing.T) {
	if os.Getenv("WAGO_DRAGLINE_MVP_COVERAGE") != "1" {
		t.Skip("set WAGO_DRAGLINE_MVP_COVERAGE=1 to run the strict MVP coverage gate")
	}
	wast2json, err := exec.LookPath("wast2json")
	if err != nil {
		t.Fatal("wast2json is required")
	}
	root := filepath.Clean("../tests/spec")
	files := []string{
		"address", "align", "block", "br", "br_if", "br_table", "call", "call_indirect", "comments", "const", "conversions", "custom", "data", "endianness", "exports", "f32", "f32_bitwise", "f32_cmp", "f64", "f64_bitwise", "f64_cmp", "fac", "float_exprs", "float_literals", "float_memory", "float_misc", "forward", "func", "func_ptrs", "global", "i32", "i64", "if", "imports", "inline-module", "int_exprs", "int_literals", "labels", "left-to-right", "linking", "load", "local_get", "local_set", "local_tee", "loop", "memory", "memory_grow", "memory_redundancy", "memory_size", "memory_trap", "names", "nop", "return", "select", "stack", "start", "store", "switch", "token", "traps", "type", "unreachable", "unreached-invalid", "unwind",
	}
	type command struct {
		Type     string `json:"type"`
		Filename string `json:"filename"`
	}
	var compiled, postMVPRejected, mvpRejected int
	var phase2Functions, phase2Blocks, phase2LocalParams, phase2StackParams, phase2SemanticInsts, phase2SemanticArgs, phase2Effectful, phase2Trapping int
	var phase3MachineInsts, phase3MachineOperands, phase3EdgeTransfers, phase3AMD64Spills, phase3ARM64Spills, phase3AMD64FixedMoves, phase3ARM64FixedMoves int
	var phase3AMD64MaxFrame, phase3ARM64MaxFrame uint32
	var phase4AMD64Copies, phase4ARM64Copies, phase4AMD64Coalesced, phase4ARM64Coalesced, phase4AMD64Cycles, phase4ARM64Cycles, phase4AMD64Motion, phase4ARM64Motion int
	var phase5Aliases, phase5TrivialArgs, phase5Constants, phase5DeadInsts, phase5DeadBlocks, phase5Branches, phase5Obligations, phase5Bounds, phase5Proofs int
	var phase6Sinks, phase6CommittedSinks, phase6CommittedInductions, phase6CommittedLICM, phase6Remats, phase6Affine, phase6AffineProfitable, phase6Inductions, phase6LICM, phase6ColdUses int
	var phase6MaxGPR, phase6MaxFPR uint16
	var phase7AMD64Spills, phase7ARM64Spills, phase7AMD64Promotions, phase7ARM64Promotions, phase7AMD64Evictions, phase7ARM64Evictions, phase7AMD64CalleeSaved, phase7ARM64CalleeSaved int
	var phase7AMD64Debt, phase7ARM64Debt uint64
	var phase7AMD64Preservation, phase7ARM64Preservation uint64
	var phase8AMD64Immediate, phase8ARM64Immediate, phase8AMD64Fixed, phase8ARM64Fixed, phase8AMD64Address, phase8ARM64Address, phase8AMD64Flags, phase8ARM64Flags, phase8AMD64Combines, phase8ARM64Combines int
	var phase9AMD64Dependencies, phase9ARM64Dependencies, phase9AMD64Retries, phase9ARM64Retries, phase9AMD64Fusions, phase9ARM64Fusions int
	var phase9AMD64Winners, phase9ARM64Winners [4]int
	var phase10AMD64Clobbers, phase10ARM64Clobbers, phase10AMD64CalleeSaves, phase10ARM64CalleeSaves, phase10AMD64RefinedCalls, phase10ARM64RefinedCalls int
	var phase10AMD64MaxFrame, phase10ARM64MaxFrame uint32
	var phase11AMD64Rewrites, phase11ARM64Rewrites, phase11AMD64LEA, phase11AMD64Fusion, phase11AMD64Fixed, phase11AMD64Memory, phase11ARM64Pairs, phase11ARM64Fusion, phase11ARM64Forward, phase11ARM64Rename int
	var phase12SameInstance, phase12PreservedBounds, phase12HostEffects, phase12IndirectTargets int
	reasons := map[string]int{}
	for _, base := range files {
		t.Run(base, func(t *testing.T) {
			tmp := t.TempDir()
			jsonPath := filepath.Join(tmp, base+".json")
			out, err := exec.Command(wast2json, "--enable-all", filepath.Join(root, base+".wast"), "-o", jsonPath).CombinedOutput()
			if err != nil {
				t.Fatalf("wast2json: %v: %s", err, out)
			}
			raw, err := os.ReadFile(jsonPath)
			if err != nil {
				t.Fatal(err)
			}
			var script struct {
				Commands []command `json:"commands"`
			}
			if err := json.Unmarshal(raw, &script); err != nil {
				t.Fatal(err)
			}
			for _, command := range script.Commands {
				if command.Type != "module" {
					continue
				}
				module, err := os.ReadFile(filepath.Join(tmp, command.Filename))
				if err != nil {
					t.Fatal(err)
				}
				artifact, err := wago.NewRuntimeConfig().WithCompiler(wago.CompilerDragline).Compile(module)
				if err == nil {
					compiled++
					artifact.Close()
					decoded, decodeErr := wasm.DecodeModule(module)
					if decodeErr != nil {
						t.Fatalf("decode admitted module %s/%s: %v", base, command.Filename, decodeErr)
					}
					var stackScratch railssa.StackFunc
					var cfgScratch railssa.CFG
					var localScratch railssa.LocalSSA
					var valueScratch railssa.ValueFlow
					var semanticScratch railssa.SemanticFunc
					var amd64MachineScratch, arm64MachineScratch railmach.Func
					var amd64AllocationScratch, arm64AllocationScratch railmach.Allocation
					var amd64ExitScratch, arm64ExitScratch railmach.SSAExit
					var amd64GreedyScratch, arm64GreedyScratch railmach.GreedyAllocation
					var amd64GreedyExitScratch, arm64GreedyExitScratch railmach.SSAExit
					var amd64ScheduledGreedyScratch, arm64ScheduledGreedyScratch railmach.GreedyAllocation
					var amd64ScheduledExitScratch, arm64ScheduledExitScratch railmach.SSAExit
					var metadataScratch railssa.Metadata
					var simplifyScratch railssa.SimplifyResult
					var pressureScratch railssa.PressurePlan
					var amd64SelectionScratch, arm64SelectionScratch railmach.SelectionPlan
					var amd64RematScratch, arm64RematScratch railmach.RematPlan
					var amd64DAGScratch, arm64DAGScratch railmach.DependencyDAG
					var amd64ScheduleScratch, arm64ScheduleScratch railmach.Schedule
					var amd64PostRAScratch, arm64PostRAScratch railmach.PostRAPlan
					var specializationScratch railssa.SpecializationPlan
					amd64Contracts := make([]railmach.ABIContract, len(decoded.Code))
					arm64Contracts := make([]railmach.ABIContract, len(decoded.Code))
					amd64CallSets := make([][]railmach.CallContract, len(decoded.Code))
					arm64CallSets := make([][]railmach.CallContract, len(decoded.Code))
					for function := range decoded.Code {
						stack, buildErr := railssa.BuildStackFuncInto(decoded, function, &stackScratch)
						if buildErr != nil {
							t.Fatalf("phase2 lower admitted module %s/%s function %d: %v", base, command.Filename, function, buildErr)
						}
						cfg, cfgErr := railssa.BuildCFG(stack, &cfgScratch)
						if cfgErr != nil {
							t.Fatalf("phase2 CFG admitted module %s/%s function %d: %v", base, command.Filename, function, cfgErr)
						}
						locals, localErr := railssa.BuildLocalSSA(stack, cfg, &localScratch)
						if localErr != nil {
							t.Fatalf("phase2 local SSA admitted module %s/%s function %d: %v", base, command.Filename, function, localErr)
						}
						values, valueErr := railssa.BuildValueFlow(stack, cfg, locals, &valueScratch)
						if valueErr != nil {
							t.Fatalf("phase2 value flow admitted module %s/%s function %d: %v", base, command.Filename, function, valueErr)
						}
						semantic, semanticErr := railssa.BuildSemanticFunc(stack, cfg, values, &semanticScratch)
						if semanticErr != nil {
							t.Fatalf("phase2 semantic SSA admitted module %s/%s function %d: %v", base, command.Filename, function, semanticErr)
						}
						amd64Machine, amd64MachineErr := railmach.Build(railmach.TargetAMD64, cfg, values, semantic, &amd64MachineScratch)
						if amd64MachineErr != nil {
							t.Fatalf("phase3 AMD64 RailMach admitted module %s/%s function %d: %v", base, command.Filename, function, amd64MachineErr)
						}
						arm64Machine, arm64MachineErr := railmach.Build(railmach.TargetARM64, cfg, values, semantic, &arm64MachineScratch)
						if arm64MachineErr != nil {
							t.Fatalf("phase3 ARM64 RailMach admitted module %s/%s function %d: %v", base, command.Filename, function, arm64MachineErr)
						}
						amd64Allocation, amd64AllocationErr := railmach.AllocateLinearQ(amd64Machine, railmach.DefaultLinearQConfig(railmach.TargetAMD64), &amd64AllocationScratch)
						if amd64AllocationErr != nil {
							t.Fatalf("phase3 AMD64 RALinearQ admitted module %s/%s function %d: %v", base, command.Filename, function, amd64AllocationErr)
						}
						arm64Allocation, arm64AllocationErr := railmach.AllocateLinearQ(arm64Machine, railmach.DefaultLinearQConfig(railmach.TargetARM64), &arm64AllocationScratch)
						if arm64AllocationErr != nil {
							t.Fatalf("phase3 ARM64 RALinearQ admitted module %s/%s function %d: %v", base, command.Filename, function, arm64AllocationErr)
						}
						amd64Exit, amd64ExitErr := railmach.LateSSAExit(amd64Machine, amd64Allocation, &amd64ExitScratch)
						if amd64ExitErr != nil {
							t.Fatalf("phase4 AMD64 late SSA exit admitted module %s/%s function %d: %v", base, command.Filename, function, amd64ExitErr)
						}
						arm64Exit, arm64ExitErr := railmach.LateSSAExit(arm64Machine, arm64Allocation, &arm64ExitScratch)
						if arm64ExitErr != nil {
							t.Fatalf("phase4 ARM64 late SSA exit admitted module %s/%s function %d: %v", base, command.Filename, function, arm64ExitErr)
						}
						amd64Greedy, amd64GreedyErr := railmach.AllocateGreedyP(amd64Machine, railmach.DefaultGreedyConfig(railmach.TargetAMD64), &amd64GreedyScratch)
						if amd64GreedyErr != nil {
							t.Fatalf("phase7 AMD64 RAGreedyP admitted module %s/%s function %d: %v", base, command.Filename, function, amd64GreedyErr)
						}
						arm64Greedy, arm64GreedyErr := railmach.AllocateGreedyP(arm64Machine, railmach.DefaultGreedyConfig(railmach.TargetARM64), &arm64GreedyScratch)
						if arm64GreedyErr != nil {
							t.Fatalf("phase7 ARM64 RAGreedyP admitted module %s/%s function %d: %v", base, command.Filename, function, arm64GreedyErr)
						}
						amd64GreedyExit, greedyExitErr := railmach.LateSSAExit(amd64Machine, &amd64Greedy.Allocation, &amd64GreedyExitScratch)
						if greedyExitErr != nil {
							t.Fatalf("phase7 AMD64 greedy SSA exit admitted module %s/%s function %d: %v", base, command.Filename, function, greedyExitErr)
						}
						arm64GreedyExit, greedyExitErr := railmach.LateSSAExit(arm64Machine, &arm64Greedy.Allocation, &arm64GreedyExitScratch)
						if greedyExitErr != nil {
							t.Fatalf("phase7 ARM64 greedy SSA exit admitted module %s/%s function %d: %v", base, command.Filename, function, greedyExitErr)
						}
						metadata, metadataErr := railssa.BuildMetadata(stack, &metadataScratch)
						if metadataErr != nil {
							t.Fatalf("phase2 metadata admitted module %s/%s function %d: %v", base, command.Filename, function, metadataErr)
						}
						amd64Contract, amd64Calls, amd64ABIErr := railmach.AnalyzeABI(amd64Machine, amd64Greedy, metadata, stack.ImportedFuncs)
						if amd64ABIErr != nil {
							t.Fatalf("phase10 AMD64 ABI admitted module %s/%s function %d: %v", base, command.Filename, function, amd64ABIErr)
						}
						arm64Contract, arm64Calls, arm64ABIErr := railmach.AnalyzeABI(arm64Machine, arm64Greedy, metadata, stack.ImportedFuncs)
						if arm64ABIErr != nil {
							t.Fatalf("phase10 ARM64 ABI admitted module %s/%s function %d: %v", base, command.Filename, function, arm64ABIErr)
						}
						amd64Contracts[function], amd64CallSets[function] = amd64Contract, amd64Calls
						arm64Contracts[function], arm64CallSets[function] = arm64Contract, arm64Calls
						_, amd64Frame, amd64FrameErr := railmach.FrameForAllocation(amd64Contract, amd64Greedy, coverageCallSlots(amd64Machine))
						if amd64FrameErr != nil {
							t.Fatalf("phase10 AMD64 frame admitted module %s/%s function %d: %v", base, command.Filename, function, amd64FrameErr)
						}
						_, arm64Frame, arm64FrameErr := railmach.FrameForAllocation(arm64Contract, arm64Greedy, coverageCallSlots(arm64Machine))
						if arm64FrameErr != nil {
							t.Fatalf("phase10 ARM64 frame admitted module %s/%s function %d: %v", base, command.Filename, function, arm64FrameErr)
						}
						simplified, simplifyErr := railssa.SparseSimplify(stack, cfg, values, semantic, metadata, railssa.DefaultSimplifyConfig(), &simplifyScratch)
						if simplifyErr != nil {
							t.Fatalf("phase5 SparseSimplify admitted module %s/%s function %d: %v", base, command.Filename, function, simplifyErr)
						}
						specialization, specializationErr := railssa.PlanSpecialization(stack, semantic, metadata, simplified, railssa.SpecializationInputs{FunctionIndex: uint32(decoded.ImportedFuncCount() + function)}, &specializationScratch)
						if specializationErr != nil {
							t.Fatalf("phase12 specialization admitted module %s/%s function %d: %v", base, command.Filename, function, specializationErr)
						}
						proofs, proofEngineErr := railssa.NewProofEngine(values, semantic, metadata, simplified)
						if proofEngineErr != nil {
							t.Fatalf("phase5 proof engine admitted module %s/%s function %d: %v", base, command.Filename, function, proofEngineErr)
						}
						for _, certificate := range simplified.Bounds {
							proof, proofErr := proofs.DemandProof(railssa.ProofRequest{Kind: railssa.ProofBounds, Value: certificate.Address, Aux: certificate.Instruction, Fuel: 64})
							if proofErr != nil || !proof.Proven {
								t.Fatalf("phase5 bounds proof admitted module %s/%s function %d: proof=%#v err=%v", base, command.Filename, function, proof, proofErr)
							}
							phase5Proofs++
						}
						pressure, pressureErr := railssa.PressureShape(stack, cfg, values, semantic, metadata, simplified, &pressureScratch)
						if pressureErr != nil {
							t.Fatalf("phase6 pressure shape admitted module %s/%s function %d: %v", base, command.Filename, function, pressureErr)
						}
						amd64Selection, amd64SelectionErr := railmach.SelectOrder(railmach.TargetAMD64, values, semantic, simplified, &amd64SelectionScratch)
						if amd64SelectionErr != nil {
							t.Fatalf("phase8 AMD64 selection admitted module %s/%s function %d: %v", base, command.Filename, function, amd64SelectionErr)
						}
						arm64Selection, arm64SelectionErr := railmach.SelectOrder(railmach.TargetARM64, values, semantic, simplified, &arm64SelectionScratch)
						if arm64SelectionErr != nil {
							t.Fatalf("phase8 ARM64 selection admitted module %s/%s function %d: %v", base, command.Filename, function, arm64SelectionErr)
						}
						amd64Remat, rematErr := railmach.PriceAffineRematerialization(amd64Machine, amd64Selection, pressure, &amd64RematScratch)
						if rematErr != nil {
							t.Fatalf("phase6 AMD64 affine rematerialization admitted module %s/%s function %d: %v", base, command.Filename, function, rematErr)
						}
						arm64Remat, rematErr := railmach.PriceAffineRematerialization(arm64Machine, arm64Selection, pressure, &arm64RematScratch)
						if rematErr != nil {
							t.Fatalf("phase6 ARM64 affine rematerialization admitted module %s/%s function %d: %v", base, command.Filename, function, rematErr)
						}
						phase6Affine += len(amd64Remat.Decisions)
						for _, decision := range amd64Remat.Decisions {
							if decision.Profitable {
								phase6AffineProfitable++
							}
						}
						for _, decision := range arm64Remat.Decisions {
							if decision.Profitable {
								phase6AffineProfitable++
							}
						}
						amd64DAG, amd64DAGErr := railmach.BuildDependencyDAG(amd64Machine, amd64Selection, metadata, &amd64DAGScratch)
						if amd64DAGErr != nil {
							t.Fatalf("phase9 AMD64 dependency DAG admitted module %s/%s function %d: %v", base, command.Filename, function, amd64DAGErr)
						}
						arm64DAG, arm64DAGErr := railmach.BuildDependencyDAG(arm64Machine, arm64Selection, metadata, &arm64DAGScratch)
						if arm64DAGErr != nil {
							t.Fatalf("phase9 ARM64 dependency DAG admitted module %s/%s function %d: %v", base, command.Filename, function, arm64DAGErr)
						}
						var amd64Best, arm64Best railmach.ScheduleScore
						haveAMD64Best, haveARM64Best := false, false
						for _, kind := range []railmach.ScheduleKind{railmach.ScheduleKindSourceStable, railmach.ScheduleKindLatencyFusion, railmach.ScheduleKindPressure} {
							amd64Candidate, scheduleErr := railmach.BuildScheduleWithPressure(amd64Machine, amd64Selection, amd64DAG, kind, pressure, &amd64ScheduleScratch)
							if scheduleErr != nil {
								t.Fatalf("phase9 AMD64 schedule %d admitted module %s/%s function %d: %v", kind, base, command.Filename, function, scheduleErr)
							}
							if kind == railmach.ScheduleKindPressure {
								phase6CommittedSinks += int(amd64Candidate.CommittedSinks)
								phase6CommittedInductions += int(amd64Candidate.CommittedInductions)
								phase6CommittedLICM += int(amd64Candidate.CommittedLICM)
							}
							amd64CandidateAllocation, candidateErr := railmach.AllocateGreedyPForSchedule(amd64Machine, amd64Candidate, railmach.DefaultGreedyConfig(railmach.TargetAMD64), &amd64ScheduledGreedyScratch)
							if candidateErr != nil {
								t.Fatalf("phase9 AMD64 candidate allocation %d admitted module %s/%s function %d: %v", kind, base, command.Filename, function, candidateErr)
							}
							amd64CandidateExit, candidateErr := railmach.LateSSAExit(amd64Machine, &amd64CandidateAllocation.Allocation, &amd64ScheduledExitScratch)
							if candidateErr != nil {
								t.Fatalf("phase9 AMD64 candidate exit %d admitted module %s/%s function %d: %v", kind, base, command.Filename, function, candidateErr)
							}
							amd64Score, candidateErr := railmach.ScoreScheduleCandidate(amd64Machine, amd64Selection, amd64DAG, amd64Candidate, amd64CandidateAllocation, amd64CandidateExit)
							if candidateErr != nil {
								t.Fatalf("phase9 AMD64 candidate score %d admitted module %s/%s function %d: %v", kind, base, command.Filename, function, candidateErr)
							}
							if !haveAMD64Best || amd64Score.BetterThan(amd64Best) {
								amd64Best, haveAMD64Best = amd64Score, true
							}

							arm64Candidate, scheduleErr := railmach.BuildScheduleWithPressure(arm64Machine, arm64Selection, arm64DAG, kind, pressure, &arm64ScheduleScratch)
							if scheduleErr != nil {
								t.Fatalf("phase9 ARM64 schedule %d admitted module %s/%s function %d: %v", kind, base, command.Filename, function, scheduleErr)
							}
							if kind == railmach.ScheduleKindPressure {
								phase6CommittedSinks += int(arm64Candidate.CommittedSinks)
								phase6CommittedInductions += int(arm64Candidate.CommittedInductions)
								phase6CommittedLICM += int(arm64Candidate.CommittedLICM)
							}
							arm64CandidateAllocation, candidateErr := railmach.AllocateGreedyPForSchedule(arm64Machine, arm64Candidate, railmach.DefaultGreedyConfig(railmach.TargetARM64), &arm64ScheduledGreedyScratch)
							if candidateErr != nil {
								t.Fatalf("phase9 ARM64 candidate allocation %d admitted module %s/%s function %d: %v", kind, base, command.Filename, function, candidateErr)
							}
							arm64CandidateExit, candidateErr := railmach.LateSSAExit(arm64Machine, &arm64CandidateAllocation.Allocation, &arm64ScheduledExitScratch)
							if candidateErr != nil {
								t.Fatalf("phase9 ARM64 candidate exit %d admitted module %s/%s function %d: %v", kind, base, command.Filename, function, candidateErr)
							}
							arm64Score, candidateErr := railmach.ScoreScheduleCandidate(arm64Machine, arm64Selection, arm64DAG, arm64Candidate, arm64CandidateAllocation, arm64CandidateExit)
							if candidateErr != nil {
								t.Fatalf("phase9 ARM64 candidate score %d admitted module %s/%s function %d: %v", kind, base, command.Filename, function, candidateErr)
							}
							if !haveARM64Best || arm64Score.BetterThan(arm64Best) {
								arm64Best, haveARM64Best = arm64Score, true
							}
						}
						if _, scheduledErr := railmach.BuildScheduleWithPressure(amd64Machine, amd64Selection, amd64DAG, amd64Best.Kind, pressure, &amd64ScheduleScratch); scheduledErr != nil {
							t.Fatalf("phase9 AMD64 winning schedule admitted module %s/%s function %d: %v", base, command.Filename, function, scheduledErr)
						}
						if _, scheduledErr := railmach.BuildScheduleWithPressure(arm64Machine, arm64Selection, arm64DAG, arm64Best.Kind, pressure, &arm64ScheduleScratch); scheduledErr != nil {
							t.Fatalf("phase9 ARM64 winning schedule admitted module %s/%s function %d: %v", base, command.Filename, function, scheduledErr)
						}
						amd64ScheduledGreedy, scheduledErr := railmach.AllocateGreedyPForSchedule(amd64Machine, &amd64ScheduleScratch, railmach.DefaultGreedyConfig(railmach.TargetAMD64), &amd64ScheduledGreedyScratch)
						if scheduledErr != nil {
							t.Fatalf("phase9 AMD64 scheduled allocation admitted module %s/%s function %d: %v", base, command.Filename, function, scheduledErr)
						}
						arm64ScheduledGreedy, scheduledErr := railmach.AllocateGreedyPForSchedule(arm64Machine, &arm64ScheduleScratch, railmach.DefaultGreedyConfig(railmach.TargetARM64), &arm64ScheduledGreedyScratch)
						if scheduledErr != nil {
							t.Fatalf("phase9 ARM64 scheduled allocation admitted module %s/%s function %d: %v", base, command.Filename, function, scheduledErr)
						}
						amd64ScheduledExit, scheduledErr := railmach.LateSSAExit(amd64Machine, &amd64ScheduledGreedy.Allocation, &amd64ScheduledExitScratch)
						if scheduledErr != nil {
							t.Fatalf("phase9 AMD64 scheduled SSA exit admitted module %s/%s function %d: %v", base, command.Filename, function, scheduledErr)
						}
						arm64ScheduledExit, scheduledErr := railmach.LateSSAExit(arm64Machine, &arm64ScheduledGreedy.Allocation, &arm64ScheduledExitScratch)
						if scheduledErr != nil {
							t.Fatalf("phase9 ARM64 scheduled SSA exit admitted module %s/%s function %d: %v", base, command.Filename, function, scheduledErr)
						}
						amd64PostRA, amd64PostRAErr := railmach.PlanPostRA(railmach.TargetAMD64, amd64Machine, amd64Selection, &amd64ScheduleScratch, amd64ScheduledGreedy, amd64ScheduledExit, &amd64PostRAScratch)
						if amd64PostRAErr != nil {
							t.Fatalf("phase11 AMD64 post-RA admitted module %s/%s function %d: %v", base, command.Filename, function, amd64PostRAErr)
						}
						arm64PostRA, arm64PostRAErr := railmach.PlanPostRA(railmach.TargetARM64, arm64Machine, arm64Selection, &arm64ScheduleScratch, arm64ScheduledGreedy, arm64ScheduledExit, &arm64PostRAScratch)
						if arm64PostRAErr != nil {
							t.Fatalf("phase11 ARM64 post-RA admitted module %s/%s function %d: %v", base, command.Filename, function, arm64PostRAErr)
						}
						phase2Functions++
						phase2Blocks += len(cfg.Blocks)
						phase2LocalParams += len(locals.Params)
						phase2StackParams += len(values.Params)
						phase2SemanticInsts += len(semantic.Insts)
						phase2SemanticArgs += len(semantic.Args)
						phase3MachineInsts += len(amd64Machine.Insts)
						phase3MachineOperands += len(amd64Machine.Operands)
						phase3EdgeTransfers += len(amd64Machine.Transfers)
						phase3AMD64Spills += int(amd64Allocation.SpillSlots)
						phase3ARM64Spills += int(arm64Allocation.SpillSlots)
						phase3AMD64FixedMoves += len(amd64Allocation.FixedMoves)
						phase3ARM64FixedMoves += len(arm64Allocation.FixedMoves)
						phase3AMD64MaxFrame = max(phase3AMD64MaxFrame, amd64Allocation.FrameBytes)
						phase3ARM64MaxFrame = max(phase3ARM64MaxFrame, arm64Allocation.FrameBytes)
						phase4AMD64Copies += int(amd64Exit.Debt.Physical)
						phase4ARM64Copies += int(arm64Exit.Debt.Physical)
						phase4AMD64Coalesced += int(amd64Exit.Debt.Coalesced)
						phase4ARM64Coalesced += int(arm64Exit.Debt.Coalesced)
						phase4AMD64Cycles += int(amd64Exit.Debt.Cycles)
						phase4ARM64Cycles += int(arm64Exit.Debt.Cycles)
						phase4AMD64Motion += int(amd64Exit.Debt.Motion)
						phase4ARM64Motion += int(arm64Exit.Debt.Motion)
						phase5Aliases += int(simplified.Metrics.Aliases)
						phase5TrivialArgs += int(simplified.Metrics.TrivialArguments)
						phase5Constants += int(simplified.Metrics.Constants)
						phase5DeadInsts += int(simplified.Metrics.DeadInstructions)
						phase5DeadBlocks += int(simplified.Metrics.DeadBlocks)
						phase5Branches += int(simplified.Metrics.BranchesSimplified)
						phase5Obligations += int(simplified.Metrics.ObligationsRemoved)
						phase5Bounds += int(simplified.Metrics.BoundsCertificates)
						phase6Sinks += len(pressure.Sinks)
						phase6Remats += len(pressure.Remats)
						phase6Inductions += len(pressure.Inductions)
						phase6LICM += len(pressure.LICM)
						phase6ColdUses += len(pressure.ColdUses)
						for _, blockPressure := range pressure.Blocks {
							phase6MaxGPR = max(phase6MaxGPR, blockPressure.PeakGPR)
							phase6MaxFPR = max(phase6MaxFPR, blockPressure.PeakFPR)
						}
						phase7AMD64Spills += int(amd64Greedy.SpillSlots)
						phase7ARM64Spills += int(arm64Greedy.SpillSlots)
						phase7AMD64Promotions += int(amd64Greedy.Metrics.Promotions)
						phase7ARM64Promotions += int(arm64Greedy.Metrics.Promotions)
						phase7AMD64Evictions += int(amd64Greedy.Metrics.Evictions)
						phase7ARM64Evictions += int(arm64Greedy.Metrics.Evictions)
						phase7AMD64CalleeSaved += int(amd64Greedy.Metrics.CalleeSaved)
						phase7ARM64CalleeSaved += int(arm64Greedy.Metrics.CalleeSaved)
						phase7AMD64Debt += amd64Greedy.Metrics.WeightedDebt
						phase7ARM64Debt += arm64Greedy.Metrics.WeightedDebt
						phase7AMD64Preservation += amd64Greedy.Metrics.PreservationCost
						phase7ARM64Preservation += arm64Greedy.Metrics.PreservationCost
						phase8AMD64Combines += len(amd64Selection.Combinations)
						phase8ARM64Combines += len(arm64Selection.Combinations)
						for _, selection := range amd64Selection.Selections {
							switch selection.Rule {
							case railspec.RuleAMD64Imm32:
								phase8AMD64Immediate++
							case railspec.RuleAMD64ShiftCL, railspec.RuleAMD64DivFixed:
								phase8AMD64Fixed++
							case railspec.RuleFoldedMemoryAddress:
								phase8AMD64Address++
							case railspec.RuleCompareBranchFlags:
								phase8AMD64Flags++
							}
						}
						for _, selection := range arm64Selection.Selections {
							switch selection.Rule {
							case railspec.RuleARM64Imm12:
								phase8ARM64Immediate++
							case railspec.RuleFoldedMemoryAddress:
								phase8ARM64Address++
							case railspec.RuleCompareBranchFlags:
								phase8ARM64Flags++
							}
						}
						phase9AMD64Dependencies += len(amd64DAG.Dependencies)
						phase9ARM64Dependencies += len(arm64DAG.Dependencies)
						phase9AMD64Winners[amd64Best.Kind]++
						phase9ARM64Winners[arm64Best.Kind]++
						phase9AMD64Fusions += int(amd64ScheduleScratch.CommittedFusions)
						phase9ARM64Fusions += int(arm64ScheduleScratch.CommittedFusions)
						if railmach.DecideRetry(0, amd64Greedy, amd64GreedyExit.Debt).Retry {
							phase9AMD64Retries++
						}
						if railmach.DecideRetry(0, arm64Greedy, arm64GreedyExit.Debt).Retry {
							phase9ARM64Retries++
						}
						phase10AMD64Clobbers += popcount(amd64Contract.GPRClobbers) + popcount(amd64Contract.FPRClobbers)
						phase10ARM64Clobbers += popcount(arm64Contract.GPRClobbers) + popcount(arm64Contract.FPRClobbers)
						phase10AMD64CalleeSaves += popcount(amd64Contract.CalleeGPRs) + popcount(amd64Contract.CalleeFPRs)
						phase10ARM64CalleeSaves += popcount(arm64Contract.CalleeGPRs) + popcount(arm64Contract.CalleeFPRs)
						phase10AMD64MaxFrame = max(phase10AMD64MaxFrame, amd64Frame.TotalBytes)
						phase10ARM64MaxFrame = max(phase10ARM64MaxFrame, arm64Frame.TotalBytes)
						phase11AMD64Rewrites += len(amd64PostRA.Rewrites)
						phase11ARM64Rewrites += len(arm64PostRA.Rewrites)
						for _, rewrite := range amd64PostRA.Rewrites {
							switch rewrite.Kind {
							case railmach.RewriteAMD64LEA:
								phase11AMD64LEA++
							case railmach.RewriteAMD64FusionRepair:
								phase11AMD64Fusion++
							case railmach.RewriteAMD64FixedRepair:
								phase11AMD64Fixed++
							case railmach.RewriteAMD64MemoryFold:
								phase11AMD64Memory++
							}
						}
						for _, rewrite := range arm64PostRA.Rewrites {
							switch rewrite.Kind {
							case railmach.RewriteARM64Pair:
								phase11ARM64Pairs++
							case railmach.RewriteARM64CompareBranch:
								phase11ARM64Fusion++
							case railmach.RewriteLoadStoreForward:
								phase11ARM64Forward++
							case railmach.RewritePhysicalRename:
								phase11ARM64Rename++
							}
						}
						for _, entry := range specialization.Entries {
							switch entry.Kind {
							case railssa.SpecializeSameInstanceCall:
								phase12SameInstance++
							case railssa.SpecializeHostEffects:
								phase12HostEffects++
							case railssa.SpecializePreservedBounds:
								phase12PreservedBounds++
							case railssa.SpecializeIndirectTarget:
								phase12IndirectTargets++
							}
						}
						for _, instruction := range metadata.Instructions {
							if instruction.Reads != 0 || instruction.Writes != 0 || instruction.Flags != 0 {
								phase2Effectful++
							}
							if instruction.Traps != 0 {
								phase2Trapping++
							}
						}
					}
					for function := range decoded.Code {
						phase10AMD64RefinedCalls += int(railmach.RefineCallContracts(amd64CallSets[function], amd64Contracts, uint32(decoded.ImportedFuncCount())))
						phase10ARM64RefinedCalls += int(railmach.RefineCallContracts(arm64CallSets[function], arm64Contracts, uint32(decoded.ImportedFuncCount())))
					}
					continue
				}
				t.Logf("rejected module %s/%s: %v", base, command.Filename, err)
				reason := err.Error()
				if draglinePostMVPShape(reason) {
					postMVPRejected++
				} else {
					mvpRejected++
				}
				if _, tail, ok := strings.Cut(reason, "MVP unsupported: "); ok {
					reason = tail
				}
				reasons[reason]++
			}
		})
	}
	keys := make([]string, 0, len(reasons))
	for reason := range reasons {
		keys = append(keys, reason)
	}
	sort.Slice(keys, func(i, j int) bool {
		if reasons[keys[i]] != reasons[keys[j]] {
			return reasons[keys[i]] > reasons[keys[j]]
		}
		return keys[i] < keys[j]
	})
	for _, reason := range keys {
		t.Logf("rejected=%d reason=%s", reasons[reason], reason)
	}
	if mvpRejected != 0 {
		t.Fatalf("strict Dragline MVP coverage: compiled=%d MVP-rejected=%d post-MVP-excluded=%d", compiled, mvpRejected, postMVPRejected)
	}
	t.Logf("strict Dragline MVP coverage: compiled=%d MVP-rejected=0 post-MVP-excluded=%d", compiled, postMVPRejected)
	t.Logf("phase2 structural coverage: functions=%d blocks=%d local-block-params=%d stack-block-params=%d semantic-instructions=%d semantic-arguments=%d effectful-instructions=%d trapping-instructions=%d", phase2Functions, phase2Blocks, phase2LocalParams, phase2StackParams, phase2SemanticInsts, phase2SemanticArgs, phase2Effectful, phase2Trapping)
	t.Logf("phase3 RailMach echo coverage per target: instructions=%d operands=%d edge-transfers=%d", phase3MachineInsts, phase3MachineOperands, phase3EdgeTransfers)
	t.Logf("phase3 RALinearQ coverage: amd64-spill-slots=%d amd64-fixed-moves=%d amd64-max-frame=%d arm64-spill-slots=%d arm64-fixed-moves=%d arm64-max-frame=%d", phase3AMD64Spills, phase3AMD64FixedMoves, phase3AMD64MaxFrame, phase3ARM64Spills, phase3ARM64FixedMoves, phase3ARM64MaxFrame)
	t.Logf("phase4 late SSA exit coverage: amd64-physical-copies=%d amd64-coalesced=%d amd64-cycles=%d amd64-motion=%d arm64-physical-copies=%d arm64-coalesced=%d arm64-cycles=%d arm64-motion=%d", phase4AMD64Copies, phase4AMD64Coalesced, phase4AMD64Cycles, phase4AMD64Motion, phase4ARM64Copies, phase4ARM64Coalesced, phase4ARM64Cycles, phase4ARM64Motion)
	t.Logf("phase5 sparse/proof coverage: aliases=%d trivial-arguments=%d constants=%d dead-instructions=%d dead-blocks=%d simplified-branches=%d discharged-obligations=%d bounds-certificates=%d verified-proofs=%d", phase5Aliases, phase5TrivialArgs, phase5Constants, phase5DeadInsts, phase5DeadBlocks, phase5Branches, phase5Obligations, phase5Bounds, phase5Proofs)
	t.Logf("phase6 pressure coverage: sink-candidates=%d committed-candidate-sinks=%d rematerializations=%d affine=%d profitable-target-decisions=%d inductions=%d committed-candidate-inductions=%d licm=%d committed-candidate-licm=%d cold-uses=%d max-gpr=%d max-fpr=%d", phase6Sinks, phase6CommittedSinks, phase6Remats, phase6Affine, phase6AffineProfitable, phase6Inductions, phase6CommittedInductions, phase6LICM, phase6CommittedLICM, phase6ColdUses, phase6MaxGPR, phase6MaxFPR)
	t.Logf("phase7 RAGreedyP coverage: amd64-spills=%d amd64-promotions=%d amd64-evictions=%d amd64-callee-saved=%d amd64-preservation-cost=%d amd64-weighted-debt=%d arm64-spills=%d arm64-promotions=%d arm64-evictions=%d arm64-callee-saved=%d arm64-preservation-cost=%d arm64-weighted-debt=%d", phase7AMD64Spills, phase7AMD64Promotions, phase7AMD64Evictions, phase7AMD64CalleeSaved, phase7AMD64Preservation, phase7AMD64Debt, phase7ARM64Spills, phase7ARM64Promotions, phase7ARM64Evictions, phase7ARM64CalleeSaved, phase7ARM64Preservation, phase7ARM64Debt)
	t.Logf("phase8 selection coverage: amd64-immediate=%d amd64-fixed=%d amd64-address=%d amd64-flags=%d amd64-combines=%d arm64-immediate=%d arm64-fixed=%d arm64-address=%d arm64-flags=%d arm64-combines=%d", phase8AMD64Immediate, phase8AMD64Fixed, phase8AMD64Address, phase8AMD64Flags, phase8AMD64Combines, phase8ARM64Immediate, phase8ARM64Fixed, phase8ARM64Address, phase8ARM64Flags, phase8ARM64Combines)
	t.Logf("phase9 scheduling coverage: amd64-dependencies=%d amd64-committed-fusions=%d amd64-retry-decisions=%d amd64-winners=%v arm64-dependencies=%d arm64-committed-fusions=%d arm64-retry-decisions=%d arm64-winners=%v candidates-per-function=3", phase9AMD64Dependencies, phase9AMD64Fusions, phase9AMD64Retries, phase9AMD64Winners[1:], phase9ARM64Dependencies, phase9ARM64Fusions, phase9ARM64Retries, phase9ARM64Winners[1:])
	t.Logf("phase10 ABI/IPRA coverage: amd64-clobbers=%d amd64-callee-saves=%d amd64-refined-calls=%d amd64-max-frame=%d arm64-clobbers=%d arm64-callee-saves=%d arm64-refined-calls=%d arm64-max-frame=%d", phase10AMD64Clobbers, phase10AMD64CalleeSaves, phase10AMD64RefinedCalls, phase10AMD64MaxFrame, phase10ARM64Clobbers, phase10ARM64CalleeSaves, phase10ARM64RefinedCalls, phase10ARM64MaxFrame)
	t.Logf("phase11 post-RA coverage: amd64-rewrites=%d amd64-lea=%d amd64-fusion=%d amd64-fixed=%d amd64-memory=%d arm64-rewrites=%d arm64-pairs=%d arm64-fusion=%d arm64-forward=%d arm64-rename=%d scan-limit=%d", phase11AMD64Rewrites, phase11AMD64LEA, phase11AMD64Fusion, phase11AMD64Fixed, phase11AMD64Memory, phase11ARM64Rewrites, phase11ARM64Pairs, phase11ARM64Fusion, phase11ARM64Forward, phase11ARM64Rename, railmach.PostRAScanLimit)
	t.Logf("phase12 specialization coverage: same-instance-calls=%d host-effect-contracts=%d preserved-bounds=%d dominant-indirect-targets=%d", phase12SameInstance, phase12HostEffects, phase12PreservedBounds, phase12IndirectTargets)
}

func coverageCallSlots(machine *railmach.Func) uint32 {
	maxSlots := uint32(0)
	for instructionID, instruction := range machine.Insts {
		if instruction.Op != wasm.InstrCall && instruction.Op != wasm.InstrCallIndirect {
			continue
		}
		slots := uint32(len(machine.InstructionOperands(uint32(instructionID))))
		if instruction.Result != 0 && slots == 0 {
			slots = 1
		}
		maxSlots = max(maxSlots, slots)
	}
	return maxSlots
}

func popcount(value uint64) int {
	count := 0
	for value != 0 {
		value &= value - 1
		count++
	}
	return count
}

func draglinePostMVPShape(reason string) bool {
	// Multiple function results and type-index block signatures were introduced
	// by the multi-value proposal. The pinned translator emits those shapes under
	// --enable-all even though this gate's compatibility boundary is Wasm 1.0.
	return strings.Contains(reason, " has 2 results;") ||
		strings.Contains(reason, " has 3 results;") ||
		(strings.Contains(reason, "block type 0x") && strings.Contains(reason, "outside the structured scalar baseline"))
}
