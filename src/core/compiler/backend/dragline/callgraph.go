package dragline

import (
	"slices"
	"sync"
	"sync/atomic"

	"github.com/wago-org/wago/src/core/compiler/backend/dragline/railssa"
	"github.com/wago-org/wago/src/core/compiler/wasm"
)

// calleeFirstCompilationOrder returns local function indexes with every
// acyclic direct callee before its callers. Recursive SCC members retain source
// order. The scan consumes bytecode immediates without constructing function
// IR, so temporary storage is proportional only to the direct-call graph.
//
// If an immediate is malformed, compilation falls back to source order. The
// normal lowering pass then reports the precise staged error.
type compilationPlan struct {
	Order              []int
	Component          []int
	Recursive          []bool
	Level              []uint32
	SignalGuardFree    []bool
	BoundedContextFree []bool
	LocalCalls         []bool
	HasV128            bool
	PeakBytes          uint64
}

func calleeFirstCompilationPlan(m *wasm.Module) compilationPlan {
	count := len(m.Code)
	order := make([]int, count)
	for i := range order {
		order[i] = i
	}
	if count < 2 {
		components := make([]int, count)
		recursive := make([]bool, count)
		levels := make([]uint32, count)
		hasV128 := false
		signalGuardFree := make([]bool, count)
		localCalls := make([]bool, count)
		boundedContextFree := make([]bool, count)
		if count == 1 {
			signalGuardFree[0] = true
			hasLoop := false
			reader := wasm.NewReader(m.Code[0].BodyBytes)
			classifier := wasm.NewModuleInstructionClassifier(m, false)
			immutableTargets, immutableTargetsOK := nativeImmutableLocalTableTargets(m)
			var immediate wasm.InstructionImmediate
			for reader.HasNext() {
				opcode, err := reader.Byte()
				if err != nil || classifier.ClassifyInto(reader, opcode, &immediate) != nil {
					signalGuardFree[0] = false
					break
				}
				hasV128 = hasV128 || opcode == 0xfd
				if immediate.Kind == wasm.InstrInvalid && (opcode == 0x05 || opcode == 0x0b) {
					continue
				}
				if immediate.Kind == wasm.InstrCall {
					if int(immediate.Index) == m.ImportedFuncCount() {
						recursive[0] = true
						localCalls[0] = true
					} else {
						signalGuardFree[0] = false
					}
				} else if immediate.Kind == wasm.InstrCallIndirect && immutableTargetsOK {
					localCalls[0] = true
					for _, target := range immutableTargets {
						if target == nativeNullTableTarget {
							signalGuardFree[0] = false
							continue
						}
						if int(target) != m.ImportedFuncCount() {
							signalGuardFree[0] = false
							break
						}
						recursive[0] = true
					}
				} else if opcode == 0xfd || !railssa.ContextFreeTrapFreeKind(immediate.Kind) {
					signalGuardFree[0] = false
				}
				hasLoop = hasLoop || immediate.Kind == wasm.InstrLoop
			}
			boundedContextFree[0] = signalGuardFree[0] && !recursive[0] && !hasLoop && boundedContextFreeShape(m, 0)
		}
		return compilationPlan{Order: order, Component: components, Recursive: recursive, Level: levels, SignalGuardFree: signalGuardFree, BoundedContextFree: boundedContextFree, LocalCalls: localCalls, HasV128: hasV128, PeakBytes: sliceBytes(order) + sliceBytes(components) + sliceBytes(recursive) + sliceBytes(levels) + sliceBytes(signalGuardFree) + sliceBytes(boundedContextFree) + sliceBytes(localCalls)}
	}

	edges := make([][]int, count)
	localCalls := make([]bool, count)
	localSignalGuardFree := make([]bool, count)
	hasLoop := make([]bool, count)
	for i := range localSignalGuardFree {
		localSignalGuardFree[i] = true
	}
	imported := m.ImportedFuncCount()
	classifier := wasm.NewModuleInstructionClassifier(m, false)
	immutableTargets, immutableTargetsOK := nativeImmutableLocalTableTargets(m)
	hasV128 := false
	for caller := range m.Code {
		reader := wasm.NewReader(m.Code[caller].BodyBytes)
		var immediate wasm.InstructionImmediate
		for reader.HasNext() {
			opcode, err := reader.Byte()
			if err != nil || classifier.ClassifyInto(reader, opcode, &immediate) != nil {
				components := sourceComponents(count)
				recursive := make([]bool, count)
				levels := make([]uint32, count)
				return compilationPlan{Order: order, Component: components, Recursive: recursive, Level: levels, PeakBytes: sliceBytes(order) + sliceBytes(components) + sliceBytes(recursive) + sliceBytes(levels) + callGraphEdgeBytes(edges)}
			}
			hasV128 = hasV128 || opcode == 0xfd
			if immediate.Kind == wasm.InstrInvalid && (opcode == 0x05 || opcode == 0x0b) {
				continue
			}
			if immediate.Kind == wasm.InstrCall {
				callee := int(immediate.Index) - imported
				if callee >= 0 && callee < count {
					localCalls[caller] = true
					edges[caller] = append(edges[caller], callee)
				} else {
					localSignalGuardFree[caller] = false
				}
			} else if immediate.Kind == wasm.InstrCallIndirect && immutableTargetsOK {
				localCalls[caller] = true
				for _, target := range immutableTargets {
					if target == nativeNullTableTarget {
						localSignalGuardFree[caller] = false
						continue
					}
					callee := int(target) - imported
					if callee >= 0 && callee < count {
						edges[caller] = append(edges[caller], callee)
					} else {
						localSignalGuardFree[caller] = false
					}
				}
			} else if opcode == 0xfd || !railssa.ContextFreeTrapFreeKind(immediate.Kind) {
				localSignalGuardFree[caller] = false
			}
			hasLoop[caller] = hasLoop[caller] || immediate.Kind == wasm.InstrLoop
		}
		slices.Sort(edges[caller])
	}

	// Tarjan emits sink SCCs first for caller-to-callee edges, which is exactly
	// the desired compilation order. Source sorting makes recursive components
	// and unrelated roots deterministic.
	index := 0
	indices := make([]int, count)
	lowlink := make([]int, count)
	onStack := make([]bool, count)
	for i := range indices {
		indices[i] = -1
	}
	stack := make([]int, 0, count)
	result := order[:0]
	components := make([]int, count)
	recursive := make([]bool, count)
	component := 0
	var visit func(int)
	visit = func(function int) {
		indices[function], lowlink[function] = index, index
		index++
		stack = append(stack, function)
		onStack[function] = true
		for _, callee := range edges[function] {
			if indices[callee] < 0 {
				visit(callee)
				lowlink[function] = min(lowlink[function], lowlink[callee])
			} else if onStack[callee] {
				lowlink[function] = min(lowlink[function], indices[callee])
			}
		}
		if lowlink[function] != indices[function] {
			return
		}
		componentStart := len(result)
		for {
			last := len(stack) - 1
			member := stack[last]
			stack = stack[:last]
			onStack[member] = false
			result = append(result, member)
			if member == function {
				break
			}
		}
		slices.Sort(result[componentStart:])
		members := result[componentStart:]
		isRecursive := len(members) > 1
		if len(members) == 1 {
			isRecursive = slices.Contains(edges[members[0]], members[0])
		}
		for _, member := range members {
			components[member] = component
			recursive[member] = isRecursive
		}
		component++
	}
	for function := range count {
		if indices[function] < 0 {
			visit(function)
		}
	}
	componentLevels := make([]uint32, component)
	levels := make([]uint32, count)
	for _, caller := range result {
		callerComponent := components[caller]
		for _, callee := range edges[caller] {
			calleeComponent := components[callee]
			if calleeComponent != callerComponent {
				componentLevels[callerComponent] = max(componentLevels[callerComponent], componentLevels[calleeComponent]+1)
			}
		}
		levels[caller] = componentLevels[callerComponent]
	}
	signalGuardFree := signalGuardFreeCallClosures(result, components, edges, localSignalGuardFree)
	boundedContextFree := boundedContextFreeCallClosures(m, result, edges, localSignalGuardFree, recursive, hasLoop)
	peakBytes := sliceBytes(order) + sliceBytes(components) + sliceBytes(recursive) + sliceBytes(levels) + callGraphEdgeBytes(edges) +
		sliceBytes(signalGuardFree) + sliceBytes(boundedContextFree) + sliceBytes(localSignalGuardFree) + sliceBytes(localCalls) + sliceBytes(hasLoop) + sliceBytes(indices) + sliceBytes(lowlink) + sliceBytes(onStack) + sliceBytes(stack)
	return compilationPlan{Order: result, Component: components, Recursive: recursive, Level: levels, SignalGuardFree: signalGuardFree, BoundedContextFree: boundedContextFree, LocalCalls: localCalls, HasV128: hasV128, PeakBytes: peakBytes}
}

const (
	maxBoundedContextFreeCallDepth = 32
	maxBoundedContextFreeWorkBytes = 4 << 10
)

func boundedContextFreeShape(m *wasm.Module, local int) bool {
	if m == nil || local < 0 || local >= len(m.Code) || len(m.Code[local].BodyBytes) == 0 || len(m.Code[local].BodyBytes) > 96 {
		return false
	}
	sig, ok := m.LocalFuncType(local)
	if !ok {
		return false
	}
	locals := uint64(len(sig.Params))
	for _, run := range m.Code[local].Locals.Runs {
		locals += uint64(run.Count)
	}
	return locals <= 8
}

// boundedContextFreeCallClosures proves a finite scheduler-safe local call
// closure. Repeated direct call sites are retained in edges, so the work bound
// accounts for each invocation rather than only each distinct callee.
func boundedContextFreeCallClosures(m *wasm.Module, order []int, edges [][]int, localSafe, recursive, hasLoop []bool) []bool {
	bounded := make([]bool, len(localSafe))
	work := make([]int, len(localSafe))
	depth := make([]int, len(localSafe))
	for _, caller := range order {
		if caller < 0 || caller >= len(localSafe) || recursive[caller] || hasLoop[caller] || !localSafe[caller] || !boundedContextFreeShape(m, caller) {
			continue
		}
		candidateWork, candidateDepth, safe := len(m.Code[caller].BodyBytes), 1, true
		for _, callee := range edges[caller] {
			if callee < 0 || callee >= len(bounded) || !bounded[callee] {
				safe = false
				break
			}
			candidateWork += work[callee]
			candidateDepth = max(candidateDepth, depth[callee]+1)
		}
		if safe && candidateWork <= maxBoundedContextFreeWorkBytes && candidateDepth <= maxBoundedContextFreeCallDepth {
			bounded[caller], work[caller], depth[caller] = true, candidateWork, candidateDepth
		}
	}
	return bounded
}

// signalGuardFreeCallClosures proves that a local function and every function
// reachable through its closed local call graph cannot access linear memory.
// The caller separately proves closed indirect-call tables; SCCs make recursive
// closures all-or-nothing.
func signalGuardFreeCallClosures(order, components []int, edges [][]int, local []bool) []bool {
	signalGuardFree := make([]bool, len(local))
	for start := 0; start < len(order); {
		component := components[order[start]]
		end := start + 1
		for end < len(order) && components[order[end]] == component {
			end++
		}
		safe := true
		for _, member := range order[start:end] {
			safe = safe && local[member]
			for _, callee := range edges[member] {
				if components[callee] != component && !signalGuardFree[callee] {
					safe = false
				}
			}
		}
		for _, member := range order[start:end] {
			signalGuardFree[member] = safe
		}
		start = end
	}
	return signalGuardFree
}

type compilationComponent struct {
	members []int
	level   uint32
}

// runCompilationComponents executes independent call-graph components in
// parallel, but never starts a caller level before every callee level has
// completed. Members of a recursive component stay on one worker in source
// order. The caller remains responsible for deterministic final assembly.
func runCompilationComponents(plan compilationPlan, workers int, compile func(worker int, members []int) error) error {
	if workers <= 1 || len(plan.Order) <= 1 {
		return compile(0, plan.Order)
	}
	components := make([]compilationComponent, 0)
	for start := 0; start < len(plan.Order); {
		component := plan.Component[plan.Order[start]]
		end := start + 1
		for end < len(plan.Order) && plan.Component[plan.Order[end]] == component {
			end++
		}
		components = append(components, compilationComponent{members: plan.Order[start:end], level: plan.Level[plan.Order[start]]})
		start = end
	}
	var maxLevel uint32
	for _, component := range components {
		maxLevel = max(maxLevel, component.level)
	}
	errs := make([]error, len(components))
	tasks := make([]int, 0, len(components))
	for level := uint32(0); level <= maxLevel; level++ {
		tasks = tasks[:0]
		for index, component := range components {
			if component.level == level {
				tasks = append(tasks, index)
			}
		}
		var next atomic.Uint32
		active := min(workers, len(tasks))
		var wait sync.WaitGroup
		wait.Add(active)
		for worker := 0; worker < active; worker++ {
			go func(worker int) {
				defer wait.Done()
				for {
					position := int(next.Add(1)) - 1
					if position >= len(tasks) {
						return
					}
					index := tasks[position]
					errs[index] = compile(worker, components[index].members)
				}
			}(worker)
		}
		wait.Wait()
		for _, index := range tasks {
			if errs[index] != nil {
				return errs[index]
			}
		}
	}
	return nil
}

func callGraphEdgeBytes(edges [][]int) uint64 {
	bytes := sliceBytes(edges)
	for _, targets := range edges {
		bytes += sliceBytes(targets)
	}
	return bytes
}

func sourceComponents(count int) []int {
	components := make([]int, count)
	for i := range components {
		components[i] = i
	}
	return components
}
