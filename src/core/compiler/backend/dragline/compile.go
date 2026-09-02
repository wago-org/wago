// Package dragline contains Wago's optimizing sibling compiler engine.
package dragline

import (
	"crypto/sha256"
	"encoding/binary"
	"errors"
	"fmt"
	"hash"
	"slices"

	corecompiler "github.com/wago-org/wago/src/core/compiler"
	"github.com/wago-org/wago/src/core/compiler/backend/dragline/railmach"
	"github.com/wago-org/wago/src/core/compiler/backend/dragline/railssa"
	"github.com/wago-org/wago/src/core/compiler/codegen"
	"github.com/wago-org/wago/src/core/compiler/wasm"
)

func boolUint32(value bool) uint32 {
	if value {
		return 1
	}
	return 0
}

// UnsupportedError reports a validated module feature outside the current
// Dragline MVP. Selection remains strict and never delegates to Railshot.
type UnsupportedError struct {
	Reason string
}

func (e *UnsupportedError) Error() string { return "MVP unsupported: " + e.Reason }

// ResourceLimitError reports valid Wasm whose compilation exceeds a bounded
// Dragline data structure. It is recoverable only through an explicit
// whole-module fallback policy; Dragline itself never changes engines.
type ResourceLimitError struct {
	Resource string
	Required uint64
	Limit    uint64
	Err      error
}

func (e *ResourceLimitError) Error() string {
	return fmt.Sprintf("MVP resource limit: %s requires %d, exceeds %d", e.Resource, e.Required, e.Limit)
}

func (e *ResourceLimitError) Unwrap() error { return e.Err }

func boundedFrameBytes(resource string, slots, limit uint64) (uint32, error) {
	const max = ^uint64(0)
	if slots > (max-15)/8 {
		return 0, &ResourceLimitError{Resource: resource, Required: max, Limit: limit}
	}
	required := (slots*8 + 15) &^ 15
	if required > limit {
		return 0, &ResourceLimitError{Resource: resource, Required: required, Limit: limit}
	}
	return uint32(required), nil
}

// Compiler is the independent Dragline backend.
type Compiler struct {
	// Replay receives a deterministic single-function artifact whenever
	// compilation reaches a function-specific failure and exact source bytes
	// are available.
	Replay func(corecompiler.ReplayArtifact) error
	// Metrics receives opt-in per-function compiler measurements. A Compiler
	// without Metrics does not read the clock or retain measurement rows.
	Metrics *Metrics
	// FunctionCache enables bounded per-function reuse. Nil preserves the
	// allocation-free cache-disabled production path.
	FunctionCache *corecompiler.FunctionArtifactCache
}

const draglinePrivateABIRevision = 2

var draglineCompilerRevision = sha256.Sum256([]byte("wago-dragline-function-artifact-v54"))

func (c Compiler) Compile(input corecompiler.Input) (corecompiler.Output, error) {
	if c.Metrics != nil {
		c.Metrics.reset(input.Target.Fingerprint())
	}
	if input.Target.GOARCH != "amd64" && input.Target.GOARCH != "arm64" {
		return corecompiler.Output{}, &UnsupportedError{Reason: fmt.Sprintf("target architecture %s", input.Target.GOARCH)}
	}
	m := input.Module
	if m.MemCount() > 1 || m.TableCount() > 1 || len(m.Tags) != 0 {
		return corecompiler.Output{}, &UnsupportedError{Reason: "multiple memories or tables, or tags"}
	}
	for i := range m.Globals {
		if typ := wasm.GlobalValueType(m.Globals[i].Type); typ != wasm.I32 && typ != wasm.I64 {
			if typ != wasm.F32 && typ != wasm.F64 {
				return corecompiler.Output{}, &UnsupportedError{Reason: fmt.Sprintf("global %d has unsupported type %s", i, typ)}
			}
		}
	}
	output, err := compileNative(input, m, c.Metrics, c.FunctionCache)
	err = classifyResourceLimit(err)
	if err == nil || c.Replay == nil || len(input.Source) == 0 {
		return output, err
	}
	var functionErr *FunctionError
	if !errors.As(err, &functionErr) {
		return output, err
	}
	replay := corecompiler.NewReplayArtifact(corecompiler.EngineDragline, input, functionErr.Function, functionErr.Stage, err.Error())
	if replayErr := c.Replay(replay); replayErr != nil {
		return corecompiler.Output{}, errors.Join(err, fmt.Errorf("dragline: record replay: %w", replayErr))
	}
	return output, err
}

func classifyResourceLimit(err error) error {
	if err == nil {
		return nil
	}
	var classified *ResourceLimitError
	if errors.As(err, &classified) {
		return err
	}
	var ssaBudget *railssa.BudgetError
	if errors.As(err, &ssaBudget) {
		return &ResourceLimitError{Resource: ssaBudget.Resource, Required: ssaBudget.Required, Limit: ssaBudget.Limit, Err: err}
	}
	var machineBudget *railmach.BudgetError
	if errors.As(err, &machineBudget) {
		return &ResourceLimitError{Resource: machineBudget.Resource, Required: machineBudget.Required, Limit: machineBudget.Limit, Err: err}
	}
	return err
}

type functionModuleDependencies struct {
	module              [32]byte
	callee              [][32]byte
	specialization      [][32]byte
	helperSafepointBase []uint32
}

func compilerHostEffectContracts(bindings []corecompiler.HostEffectBinding) []railssa.HostEffectContract {
	if len(bindings) == 0 {
		return nil
	}
	contracts := make([]railssa.HostEffectContract, len(bindings))
	for index, binding := range bindings {
		contracts[index] = railssa.HostEffectContract{
			Reads: railssa.HeapMask(binding.Contract.Reads), Writes: railssa.HeapMask(binding.Contract.Writes),
			Flags: railssa.EffectFlags(binding.Contract.Flags), Declared: binding.Declared,
		}
	}
	return contracts
}

// compactFunctionSelection validates the strict native-clone boundary and
// returns a local-function mask. Direct calls may not escape the clone because
// their relocations are resolved inside its compact code image. Imported and
// indirect calls continue through the instance dispatch boundary.
func compactFunctionSelection(input corecompiler.Input, m *wasm.Module) ([]bool, error) {
	if len(input.SelectedFunctions) == 0 {
		return nil, nil
	}
	if !slices.IsSorted(input.SelectedFunctions) {
		return nil, fmt.Errorf("dragline: selected functions are not sorted")
	}
	imports := uint32(m.ImportedFuncCount())
	mask := make([]bool, len(m.Code))
	for index, function := range input.SelectedFunctions {
		if index != 0 && input.SelectedFunctions[index-1] == function {
			return nil, fmt.Errorf("dragline: selected function %d is repeated", function)
		}
		if function < imports || function-imports >= uint32(len(m.Code)) {
			return nil, fmt.Errorf("dragline: selected function %d is not local", function)
		}
		mask[function-imports] = true
	}
	var scratch railssa.StackFunc
	for local, selected := range mask {
		if !selected {
			continue
		}
		stack, err := railssa.BuildStackFuncInto(m, local, &scratch)
		if err != nil {
			return nil, functionError(m, local, "clone-selection", err)
		}
		for _, instruction := range stack.Instrs {
			if instruction.Kind != wasm.InstrCall && instruction.Kind != wasm.InstrReturnCall {
				continue
			}
			target := instruction.U32()
			if target >= imports && (target-imports >= uint32(len(mask)) || !mask[target-imports]) {
				return nil, fmt.Errorf("dragline: selected function %d directly calls unselected local function %d", uint32(local)+imports, target)
			}
		}
	}
	return mask, nil
}

// initialNativeCodeCapacity covers source bytes plus a small per-function
// allowance without speculating about the module's native expansion ratio.
func initialNativeCodeCapacity(m *wasm.Module) int {
	bodyBytes := 0
	for i := range m.Code {
		bodyBytes += len(m.Code[i].BodyBytes)
	}
	return len(m.Code)*64 + bodyBytes
}

func initialSelectedNativeCodeCapacity(m *wasm.Module, selected []bool) int {
	if selected == nil {
		return initialNativeCodeCapacity(m)
	}
	bodyBytes, functions := 0, 0
	for local, include := range selected {
		if include {
			bodyBytes += len(m.Code[local].BodyBytes)
			functions++
		}
	}
	return 16 + functions*64 + bodyBytes
}

// growNativeCodeFromObservation uses the first compiled function as a bounded
// module-local expansion sample. Compact outputs keep the conservative initial
// capacity; instruction-dense outputs avoid repeated aggregate-buffer growth.
func growNativeCodeFromObservation(code []byte, m *wasm.Module, wasmBytes, nativeBytes int) []byte {
	if wasmBytes <= 0 || nativeBytes <= wasmBytes {
		return code
	}
	bodyBytes := 0
	for i := range m.Code {
		bodyBytes += len(m.Code[i].BodyBytes)
	}
	const maxExpansion = 192 << 10
	const minExpansion = 16 << 10
	expansion := maxExpansion
	expansionPerFunction := nativeBytes - wasmBytes
	if bodyBytes <= maxExpansion {
		scaled := uint64(bodyBytes) * uint64(expansionPerFunction) / uint64(wasmBytes)
		if scaled < maxExpansion {
			expansion = int(scaled)
		}
	}
	if expansion < minExpansion {
		return code
	}
	want := len(m.Code)*64 + bodyBytes + expansion
	if want <= cap(code) {
		return code
	}
	grown := make([]byte, len(code), want)
	copy(grown, code)
	return grown
}

func functionArtifactDependencies(input corecompiler.Input, m *wasm.Module, cache *corecompiler.FunctionArtifactCache) (functionModuleDependencies, bool) {
	if cache == nil || len(input.Source) == 0 || input.ConfigurationFingerprint == [32]byte{} || input.Runtime.ABIRevision == 0 {
		return functionModuleDependencies{}, false
	}
	module, ok := moduleDependencyHash(input.Source)
	if !ok {
		return functionModuleDependencies{}, false
	}
	moduleCalleeHash := sha256.New()
	moduleCalleeHash.Write([]byte("wago-dragline-callee-contract-fallback-v2\x00"))
	writeDependencyU32(moduleCalleeHash, uint32(len(m.Code)))
	for i := range m.Code {
		writeFunctionContractDependency(moduleCalleeHash, m, i)
	}
	var fallback [32]byte
	copy(fallback[:], moduleCalleeHash.Sum(nil))
	callee := make([][32]byte, len(m.Code))
	specialization := make([][32]byte, len(m.Code))
	edges := make([][]int, len(m.Code))
	var stackScratch railssa.StackFunc
	for localIndex := range m.Code {
		stack, err := railssa.BuildStackFuncInto(m, localIndex, &stackScratch)
		if err != nil {
			hostFallback := hostEffectSpecializationFallback(input.HostEffects)
			for index := range callee {
				callee[index] = fallback
				specialization[index] = hostFallback
			}
			return functionModuleDependencies{module: module, callee: callee, specialization: specialization}, true
		}
		hostHash := sha256.New()
		hostHash.Write([]byte("wago-dragline-host-effect-dependencies-v1\x00"))
		hasHostDependency := false
		for _, instruction := range stack.Instrs {
			if instruction.Kind != wasm.InstrCall {
				continue
			}
			if target := instruction.U32(); target < uint32(len(input.HostEffects)) {
				binding := input.HostEffects[target]
				if binding.Declared {
					writeHostEffectDependency(hostHash, target, binding.Contract)
					hasHostDependency = true
				}
			}
			if instruction.U32() < uint32(m.ImportedFuncCount()) {
				continue
			}
			calleeLocal := int(instruction.U32()) - m.ImportedFuncCount()
			if calleeLocal >= 0 && calleeLocal < len(m.Code) {
				edges[localIndex] = append(edges[localIndex], calleeLocal)
			}
		}
		if hasHostDependency {
			copy(specialization[localIndex][:], hostHash.Sum(nil))
		}
	}
	marks := make([]uint32, len(m.Code))
	work := make([]int, 0, len(m.Code))
	for localIndex := range m.Code {
		generation := uint32(localIndex + 1)
		work = work[:0]
		work = append(work, edges[localIndex]...)
		for len(work) != 0 {
			last := len(work) - 1
			dependency := work[last]
			work = work[:last]
			if marks[dependency] == generation {
				continue
			}
			marks[dependency] = generation
			work = append(work, edges[dependency]...)
		}
		h := sha256.New()
		h.Write([]byte("wago-dragline-caller-contract-closure-v2\x00"))
		for dependency := range m.Code {
			if marks[dependency] == generation {
				writeFunctionContractDependency(h, m, dependency)
			}
		}
		copy(callee[localIndex][:], h.Sum(nil))
	}
	return functionModuleDependencies{module: module, callee: callee, specialization: specialization}, true
}

func hostEffectSpecializationFallback(bindings []corecompiler.HostEffectBinding) [32]byte {
	if len(bindings) == 0 {
		return [32]byte{}
	}
	h := sha256.New()
	h.Write([]byte("wago-dragline-host-effect-fallback-v1\x00"))
	used := false
	for index, binding := range bindings {
		if binding.Declared {
			writeHostEffectDependency(h, uint32(index), binding.Contract)
			used = true
		}
	}
	if !used {
		return [32]byte{}
	}
	var out [32]byte
	copy(out[:], h.Sum(nil))
	return out
}

func writeHostEffectDependency(h hash.Hash, target uint32, contract corecompiler.HostEffectContract) {
	writeDependencyU32(h, target)
	writeDependencyU32(h, uint32(contract.Reads))
	writeDependencyU32(h, uint32(contract.Writes))
	writeDependencyU32(h, uint32(contract.Flags))
}

func writeFunctionContractDependency(h hash.Hash, m *wasm.Module, localIndex int) {
	writeDependencyU32(h, uint32(localIndex))
	for _, run := range m.Code[localIndex].Locals.Runs {
		writeDependencyU32(h, run.Count)
		h.Write([]byte{byte(run.Type.Kind()), byte(run.Type.Num())})
	}
	h.Write([]byte{0xff})
	writeDependencyU64(h, uint64(len(m.Code[localIndex].BodyBytes)))
	h.Write(m.Code[localIndex].BodyBytes)
}

func moduleDependencyHash(source []byte) ([32]byte, bool) {
	if len(source) < 8 || string(source[:4]) != "\x00asm" {
		return [32]byte{}, false
	}
	h := sha256.New()
	h.Write([]byte("wago-dragline-module-dependencies-v1\x00"))
	h.Write(source[:8])
	for offset := 8; offset < len(source); {
		sectionID := source[offset]
		offset++
		size, next, ok := readDependencyULEB(source, offset)
		if !ok || size > uint64(len(source)-next) {
			return [32]byte{}, false
		}
		end := next + int(size)
		if sectionID != 10 { // Function bodies are keyed independently.
			h.Write([]byte{sectionID})
			writeDependencyU64(h, size)
			h.Write(source[next:end])
		}
		offset = end
	}
	var out [32]byte
	copy(out[:], h.Sum(nil))
	return out, true
}

func readDependencyULEB(source []byte, offset int) (uint64, int, bool) {
	var value uint64
	for shift := uint(0); shift < 64 && offset < len(source); shift += 7 {
		b := source[offset]
		offset++
		if shift == 63 && b > 1 {
			return 0, 0, false
		}
		value |= uint64(b&0x7f) << shift
		if b&0x80 == 0 {
			return value, offset, true
		}
	}
	return 0, 0, false
}

func writeDependencyU32(h hash.Hash, value uint32) {
	var encoded [4]byte
	binary.LittleEndian.PutUint32(encoded[:], value)
	h.Write(encoded[:])
}

func writeDependencyU64(h hash.Hash, value uint64) {
	var encoded [8]byte
	binary.LittleEndian.PutUint64(encoded[:], value)
	h.Write(encoded[:])
}

func functionArtifactIdentity(input corecompiler.Input, m *wasm.Module, localIndex int, dependencies functionModuleDependencies) (corecompiler.FunctionArtifactIdentity, error) {
	typeHash := sha256.New()
	typeHash.Write([]byte("wago-dragline-function-type-dependencies-v1\x00"))
	typeHash.Write(dependencies.module[:])
	for _, run := range m.Code[localIndex].Locals.Runs {
		writeDependencyU32(typeHash, run.Count)
		typeHash.Write([]byte{byte(run.Type.Kind()), byte(run.Type.Num())})
	}
	if localIndex >= 0 && localIndex < len(dependencies.helperSafepointBase) {
		writeDependencyU32(typeHash, dependencies.helperSafepointBase[localIndex])
	}
	var typeDependencies [32]byte
	copy(typeDependencies[:], typeHash.Sum(nil))
	calleeContracts := [32]byte{}
	if localIndex >= 0 && localIndex < len(dependencies.callee) {
		calleeContracts = dependencies.callee[localIndex]
	}
	specialization := [32]byte{}
	if localIndex >= 0 && localIndex < len(dependencies.specialization) {
		specialization = dependencies.specialization[localIndex]
	}
	identity, err := corecompiler.NewFunctionArtifactIdentity(input, corecompiler.EngineDragline, uint32(m.ImportedFuncCount()+localIndex), m.Code[localIndex].BodyBytes, typeDependencies, specialization, calleeContracts, draglineCompilerRevision, draglinePrivateABIRevision)
	return identity, err
}

func allocatingHelperSafepointBases(m *wasm.Module, order []int) ([]uint32, error) {
	if m == nil || len(m.Code) == 0 {
		return nil, nil
	}
	hasAggregateType := false
	for _, group := range m.Types {
		for _, subtype := range group.SubTypes {
			hasAggregateType = hasAggregateType || subtype.Comp.Kind == wasm.CompStruct || subtype.Comp.Kind == wasm.CompArray
		}
	}
	if !hasAggregateType {
		return nil, nil
	}
	var counts []uint32
	var total uint64
	classifier := wasm.NewModuleInstructionClassifier(m, true)
	for local := range m.Code {
		r := wasm.ReaderFrom(m.Code[local].BodyBytes)
		var immediate wasm.InstructionImmediate
		for r.HasNext() {
			op, err := r.Byte()
			if err != nil {
				return nil, functionError(m, local, "helper-safepoints", err)
			}
			if err := classifier.ClassifyInto(&r, op, &immediate); err != nil {
				return nil, functionError(m, local, "helper-safepoints", err)
			}
			if immediate.Kind == wasm.InstrStructNew || immediate.Kind == wasm.InstrStructNewDefault || immediate.Kind == wasm.InstrArrayNew || immediate.Kind == wasm.InstrArrayNewDefault || immediate.Kind == wasm.InstrArrayNewFixed || immediate.Kind == wasm.InstrArrayNewData || immediate.Kind == wasm.InstrArrayNewElem {
				if counts == nil {
					counts = make([]uint32, len(m.Code))
				}
				counts[local]++
				total++
			}
		}
	}
	if total == 0 {
		return nil, nil
	}
	if total > uint64(codegen.GCSafepointIDMax) {
		return nil, &ResourceLimitError{Resource: "GC helper safepoints", Required: total, Limit: uint64(codegen.GCSafepointIDMax)}
	}
	bases := make([]uint32, len(m.Code))
	next := uint64(1)
	for _, local := range order {
		if local < 0 || local >= len(counts) {
			return nil, fmt.Errorf("dragline: helper safepoint order contains function %d", local)
		}
		count := counts[local]
		if count == 0 {
			continue
		}
		last := next + uint64(count) - 1
		if last > uint64(codegen.GCSafepointIDMax) {
			return nil, &ResourceLimitError{Resource: "GC helper safepoints", Required: last, Limit: uint64(codegen.GCSafepointIDMax)}
		}
		bases[local] = uint32(next)
		next = last + 1
	}
	return bases, nil
}

// functionArtifactEntrySource preserves the first original-Wasm location at
// the private entry boundary. The finalizer owns the native offset; keeping
// this coarse mapping here avoids making the cache infer source identity from
// machine bytes. Instruction-granular mappings can extend the same flat slab.
func functionArtifactEntrySource(fn *railssa.Func, privateEntry int) []corecompiler.FunctionSourceMap {
	if fn == nil || privateEntry < 0 {
		return nil
	}
	stack := fn.Structured
	if stack == nil {
		stack = fn.Stack
	}
	if stack == nil || len(stack.Instrs) == 0 {
		return nil
	}
	return []corecompiler.FunctionSourceMap{{NativeOffset: uint32(privateEntry), WasmOffset: stack.Instrs[0].Offset}}
}

// functionEmissionMetadata is populated only when a function artifact is being
// produced. Native offsets are observed directly at finalization boundaries;
// adjacent eliminated instructions that share an offset collapse to one entry
// so the artifact's flat slabs remain strictly ordered and bounded by emitted
// instructions/traps.
type functionEmissionMetadata struct {
	AdapterReturnOffset uint32
	Sources             []corecompiler.FunctionSourceMap
	Traps               []corecompiler.FunctionTrap
	Safepoints          []corecompiler.FunctionSafepoint
	Roots               []corecompiler.RootLocation
}

func (m *functionEmissionMetadata) prepare(machineInstructions int) {
	if m == nil || machineInstructions < 0 {
		return
	}
	m.Sources = make([]corecompiler.FunctionSourceMap, 0, machineInstructions+1)
	// Current Dragline instructions emit at most three distinct direct trap
	// stubs (the scalar conversion range checks). The source-instruction budget
	// therefore bounds this slab without a process-wide cache or growth policy.
	trapCapacity := machineInstructions
	if machineInstructions <= int(^uint(0)>>1)/3 {
		trapCapacity *= 3
	}
	m.Traps = make([]corecompiler.FunctionTrap, 0, trapCapacity)
	m.Safepoints = make([]corecompiler.FunctionSafepoint, 0, machineInstructions)
	m.Roots = make([]corecompiler.RootLocation, 0)
}

func (m *functionEmissionMetadata) recordSource(nativeOffset int, wasmOffset uint32) {
	if m == nil || nativeOffset < 0 {
		return
	}
	offset := uint32(nativeOffset)
	if n := len(m.Sources); n != 0 {
		if m.Sources[n-1].NativeOffset >= offset {
			return
		}
	}
	m.Sources = append(m.Sources, corecompiler.FunctionSourceMap{NativeOffset: offset, WasmOffset: wasmOffset})
}

func (m *functionEmissionMetadata) recordTrap(nativeOffset int, wasmOffset, code uint32) {
	if m == nil || nativeOffset < 0 || code == 0 || code > uint32(^uint16(0)) {
		return
	}
	offset := uint32(nativeOffset)
	if n := len(m.Traps); n != 0 && m.Traps[n-1].Offset >= offset {
		return
	}
	m.Traps = append(m.Traps, corecompiler.FunctionTrap{Offset: offset, WasmOffset: wasmOffset, Code: uint16(code)})
}

// recordSafepoint records the return PC and its canonically packed stack roots.
func (m *functionEmissionMetadata) recordSafepoint(nativeOffset int, roots ...corecompiler.RootLocation) {
	if m == nil || nativeOffset < 0 {
		return
	}
	offset := uint32(nativeOffset)
	if n := len(m.Safepoints); n != 0 && m.Safepoints[n-1].Offset >= offset {
		return
	}
	start := uint32(len(m.Roots))
	m.Roots = append(m.Roots, roots...)
	m.Safepoints = append(m.Safepoints, corecompiler.FunctionSafepoint{Offset: offset, RootStart: start, RootCount: uint16(len(roots))})
}

func (m *functionEmissionMetadata) recordRailMachSafepoint(nativeOffset int, plan *nativeBackendPlan, source, stackDelta uint32) {
	if m == nil || plan == nil || nativeOffset < 0 {
		return
	}
	uses := plan.Roots.RootsAtSource(source)
	start := uint32(len(m.Roots))
	for _, root := range uses {
		offset := plan.Frame.SpillBytes + uint32(root.Slot)*8
		m.Roots = append(m.Roots, corecompiler.RootLocation{Index: int32(offset), Kind: corecompiler.RootLocationStack, Bank: corecompiler.RootBankCollector})
	}
	offset := uint32(nativeOffset)
	if n := len(m.Safepoints); n != 0 && m.Safepoints[n-1].Offset >= offset {
		m.Roots = m.Roots[:start]
		return
	}
	m.Safepoints = append(m.Safepoints, corecompiler.FunctionSafepoint{Offset: offset, RootStart: start, StackAdjust: stackDelta, RootCount: uint16(len(uses))})
}

func (m *functionEmissionMetadata) recordRailMachHelperSafepoint(nativeOffset int, id uint32, plan *nativeBackendPlan, source uint32) {
	if m == nil || plan == nil || nativeOffset < 0 || id == 0 || id > codegen.GCSafepointIDMax {
		return
	}
	uses := plan.Roots.RootsAtSource(source)
	start := uint32(len(m.Roots))
	for _, root := range uses {
		offset := plan.Frame.SpillBytes + uint32(root.Slot)*8
		m.Roots = append(m.Roots, corecompiler.RootLocation{Index: int32(offset), Kind: corecompiler.RootLocationStack, Bank: corecompiler.RootBankCollector})
	}
	offset := uint32(nativeOffset)
	if n := len(m.Safepoints); n != 0 && m.Safepoints[n-1].Offset >= offset {
		m.Roots = m.Roots[:start]
		return
	}
	m.Safepoints = append(m.Safepoints, corecompiler.FunctionSafepoint{Offset: offset, ID: id, RootStart: start, RootCount: uint16(len(uses))})
}

func (m *functionEmissionMetadata) recordHelperSafepoint(nativeOffset int, id uint32) {
	if m == nil || nativeOffset < 0 || id == 0 || id > codegen.GCSafepointIDMax {
		return
	}
	offset := uint32(nativeOffset)
	if n := len(m.Safepoints); n != 0 && m.Safepoints[n-1].Offset >= offset {
		return
	}
	m.Safepoints = append(m.Safepoints, corecompiler.FunctionSafepoint{Offset: offset, ID: id, RootStart: uint32(len(m.Roots))})
}

func appendGCFrameCallsites(callsites *[]corecompiler.GCFrameCallsite, roots *[]uint32, functionBase, frameBytes uint32, safepoints []corecompiler.FunctionSafepoint, locations []corecompiler.RootLocation) {
	for _, safepoint := range safepoints {
		if safepoint.ID != 0 {
			continue
		}
		start := uint32(len(*roots))
		for _, root := range locations[safepoint.RootStart : safepoint.RootStart+uint32(safepoint.RootCount)] {
			*roots = append(*roots, uint32(root.Index))
		}
		*callsites = append(*callsites, corecompiler.GCFrameCallsite{
			ReturnOffset: functionBase + safepoint.Offset,
			FrameBytes:   frameBytes,
			StackAdjust:  safepoint.StackAdjust,
			RootStart:    start,
			RootCount:    safepoint.RootCount,
		})
	}
}

func appendGCFrameSafepoints(sites *[]corecompiler.GCFrameSafepoint, roots *[]uint32, frameBytes uint32, safepoints []corecompiler.FunctionSafepoint, locations []corecompiler.RootLocation) {
	for _, safepoint := range safepoints {
		if safepoint.ID == 0 {
			continue
		}
		start := uint32(len(*roots))
		for _, root := range locations[safepoint.RootStart : safepoint.RootStart+uint32(safepoint.RootCount)] {
			*roots = append(*roots, uint32(root.Index))
		}
		*sites = append(*sites, corecompiler.GCFrameSafepoint{ID: safepoint.ID, FrameBytes: frameBytes, RootStart: start, RootCount: safepoint.RootCount})
	}
}

func moduleNeedsCollectorRootMaps(m *wasm.Module) bool {
	if m == nil {
		return false
	}
	for _, group := range m.Types {
		for _, subtype := range group.SubTypes {
			if subtype.Comp.Kind == wasm.CompStruct || subtype.Comp.Kind == wasm.CompArray {
				return true
			}
			if subtype.Comp.Kind == wasm.CompFunc {
				for _, typ := range subtype.Comp.Params {
					if codegen.IsCollectorReferenceType(m, typ) {
						return true
					}
				}
				for _, typ := range subtype.Comp.Results {
					if codegen.IsCollectorReferenceType(m, typ) {
						return true
					}
				}
			}
		}
	}
	for index := uint32(0); index < uint32(m.GlobalCount()); index++ {
		if typ, ok := m.GlobalTypeByIndex(index); ok && codegen.IsCollectorReferenceType(m, wasm.GlobalValueType(typ)) {
			return true
		}
	}
	for index := uint32(0); index < uint32(m.TableCount()); index++ {
		if typ, ok := m.TableType(index); ok && codegen.IsCollectorReferenceType(m, wasm.RefVal(typ.Ref)) {
			return true
		}
	}
	return false
}

func nativeAllocatingHelperCount(plan *nativeBackendPlan) uint32 {
	if plan == nil || plan.Machine == nil {
		return 0
	}
	var count uint32
	for _, instruction := range plan.Machine.Insts {
		if instruction.Op == wasm.InstrStructNew || instruction.Op == wasm.InstrStructNewDefault || instruction.Op == wasm.InstrArrayNew || instruction.Op == wasm.InstrArrayNewDefault || instruction.Op == wasm.InstrArrayNewFixed || instruction.Op == wasm.InstrArrayNewData || instruction.Op == wasm.InstrArrayNewElem {
			count++
		}
	}
	return count
}

func railMachWasmOffset(plan *nativeBackendPlan, source uint32) uint32 {
	if plan == nil || plan.Stack == nil || int(source) >= len(plan.Stack.Instrs) {
		return 0
	}
	return plan.Stack.Instrs[source].Offset
}

// railMachElidesBoundsCheck consumes only source-stable proof decisions. The
// RailMach lowering preserves SemanticInst.Source, which is the original
// StackFunc instruction index used by EmissionPlan; schedules and post-RA
// rewrites may move machine instructions but do not change that identity.
func railMachElidesBoundsCheck(plan *nativeBackendPlan, source uint32) bool {
	return plan != nil && plan.Stack != nil && int(source) < len(plan.Stack.Instrs) &&
		(plan.SignalsBounds || plan.Emission != nil && plan.Emission.ElidesBoundsCheck(source))
}

// FunctionError identifies the original Wasm function and compiler stage that
// failed, allowing deterministic one-function replay capture.
type FunctionError struct {
	Function uint32
	Stage    string
	Err      error
}

func (e *FunctionError) Error() string {
	return fmt.Sprintf("function %d %s: %v", e.Function, e.Stage, e.Err)
}

func (e *FunctionError) Unwrap() error { return e.Err }

func functionError(m *wasm.Module, localIndex int, stage string, err error) error {
	return &FunctionError{Function: uint32(m.ImportedFuncCount() + localIndex), Stage: stage, Err: err}
}

func buildCompilerFunc(m *wasm.Module, localIndex int, scratch *railssa.StackFunc) (*railssa.Func, error) {
	stack, stackErr := railssa.BuildStackFuncInto(m, localIndex, scratch)
	if stackErr == nil && (stackHasControl(stack) || len(stack.Params) > 4) {
		return &railssa.Func{Index: uint32(m.ImportedFuncCount() + localIndex), Params: stack.Params, Results: stack.Results, Stack: stack, Structured: stack}, nil
	}
	fn, err := railssa.BuildFunc(m, localIndex)
	if err == nil {
		fn.Structured = stack
		return fn, nil
	}
	if stackErr != nil {
		var budget *railssa.BudgetError
		if errors.As(stackErr, &budget) {
			return nil, &ResourceLimitError{Resource: budget.Resource, Required: budget.Required, Limit: budget.Limit, Err: stackErr}
		}
		return nil, &UnsupportedError{Reason: stackErr.Error()}
	}
	return &railssa.Func{Index: uint32(m.ImportedFuncCount() + localIndex), Params: stack.Params, Results: stack.Results, Stack: stack, Structured: stack}, nil
}

func planCompilerFunc(fn *railssa.Func, planner *railssa.EmissionPlanner) (*railssa.EmissionPlan, error) {
	if fn == nil || fn.Stack == nil || !railssa.NeedsEmissionPlan(fn.Stack) {
		return nil, nil
	}
	return planner.Plan(fn.Stack)
}

func applyBoundsMode(mode corecompiler.BoundsMode, plan *railssa.EmissionPlan, native *nativeBackendPlan) *railssa.EmissionPlan {
	if mode != corecompiler.BoundsSignals {
		return plan
	}
	if native != nil {
		native.SignalsBounds = true
		return plan
	}
	if plan == nil {
		plan = new(railssa.EmissionPlan)
	}
	plan.ElideAllMemoryBounds()
	return plan
}

func stackHasControl(f *railssa.StackFunc) bool {
	for _, instr := range f.Instrs {
		if instr.Kind == wasm.InstrBlock || instr.Kind == wasm.InstrLoop || instr.Kind == wasm.InstrIf || instr.IsElse() {
			return true
		}
	}
	return false
}

func railMachTrappingTrunc(kind wasm.InstrKind) bool {
	switch kind {
	case wasm.InstrI32TruncF32S, wasm.InstrI32TruncF32U,
		wasm.InstrI32TruncF64S, wasm.InstrI32TruncF64U,
		wasm.InstrI64TruncF32S, wasm.InstrI64TruncF32U,
		wasm.InstrI64TruncF64S, wasm.InstrI64TruncF64U:
		return true
	default:
		return false
	}
}

func hotRecursiveComponent(input corecompiler.Input, m *wasm.Module, compilation compilationPlan, local int) bool {
	if input.Profile == nil || local < 0 || local >= len(compilation.Recursive) || !compilation.Recursive[local] {
		return false
	}
	component := compilation.Component[local]
	for member, memberComponent := range compilation.Component {
		if memberComponent != component {
			continue
		}
		function := m.ImportedFuncCount() + member
		if function < len(input.Profile.FunctionCounts) && input.Profile.FunctionCounts[function] != 0 {
			return true
		}
	}
	return false
}

func hasHotRecursiveComponent(input corecompiler.Input, m *wasm.Module, compilation compilationPlan) bool {
	if input.Profile == nil {
		return false
	}
	for local, recursive := range compilation.Recursive {
		function := m.ImportedFuncCount() + local
		if recursive && function < len(input.Profile.FunctionCounts) && input.Profile.FunctionCounts[function] != 0 {
			return true
		}
	}
	return false
}

// seedHotRecursiveComponent performs the conservative half of the bounded
// hot-SCC recompile. No contract is published until every RailMach-eligible
// member has planned successfully. Because an SCC is strongly connected, the
// transitive caller-register mask is the union of every member's intrinsic and
// completed outgoing-call masks; each member retains only its own callee-save
// clobbers outside that shared caller mask.
func seedHotRecursiveComponent(input corecompiler.Input, m *wasm.Module, compilation compilationPlan, local int, host []railssa.HostEffectContract, contracts, seeds []railmach.ABIContract, scores []railmach.ScheduleScore, candidates, refined, attempted []bool, stackScratch *railssa.StackFunc, planner *nativeBackendPlanner) {
	if !hotRecursiveComponent(input, m, compilation, local) || local >= len(refined) || refined[local] || attempted[local] {
		return
	}
	component := compilation.Component[local]
	for member, memberComponent := range compilation.Component {
		if memberComponent == component {
			attempted[member] = true
			seeds[member] = railmach.ABIContract{}
			scores[member] = railmach.ScheduleScore{}
			candidates[member] = false
		}
	}
	abort := func() {
		for member, memberComponent := range compilation.Component {
			if memberComponent == component {
				seeds[member] = railmach.ABIContract{}
				scores[member] = railmach.ScheduleScore{}
				candidates[member] = false
			}
		}
	}
	machineTarget := railmach.TargetARM64
	if input.Target.GOARCH == "amd64" {
		machineTarget = railmach.TargetAMD64
	}
	config := railmach.DefaultGreedyConfig(machineTarget)
	callerGPRMask := config.CallerMask(railmach.BankGPR)
	callerFPRMask := config.CallerMask(railmach.BankFPR)
	for _, member := range compilation.Order {
		if compilation.Component[member] != component {
			continue
		}
		fn, err := buildCompilerFunc(m, member, stackScratch)
		if err != nil || !railMachCandidate(fn.Structured, compilation.HasV128) {
			// A partial component contract is never published. Mixed-emitter
			// recursion retains the conservative private ABI.
			abort()
			return
		}
		plan, err := planner.PlanProfileIPRA(fn.Structured, input.Target, input.Objective, fn.Index, input.Profile, host, contracts, compilation.Component, refined, member)
		if err != nil {
			abort()
			return
		}
		contract := plan.LocalABI
		for _, call := range plan.Calls {
			instruction := plan.Machine.Insts[call.Instruction]
			if instruction.Op == wasm.InstrCall && call.Callee >= plan.Stack.ImportedFuncs {
				callee := int(call.Callee - plan.Stack.ImportedFuncs)
				if callee >= 0 && callee < len(compilation.Component) && compilation.Component[callee] == component {
					continue
				}
			}
			contract.GPRClobbers |= call.GPRClobbers & callerGPRMask
			contract.FPRClobbers |= call.FPRClobbers & callerFPRMask
		}
		contract.CalleeGPRs = contract.GPRClobbers &^ callerGPRMask
		contract.CalleeFPRs = contract.FPRClobbers &^ callerFPRMask
		seeds[member] = contract
		scores[member] = plan.Score
		candidates[member] = true
	}
	var recursiveGPR, recursiveFPR uint64
	for member, memberComponent := range compilation.Component {
		if memberComponent == component && candidates[member] {
			recursiveGPR |= seeds[member].GPRClobbers & callerGPRMask
			recursiveFPR |= seeds[member].FPRClobbers & callerFPRMask
		}
	}
	for member, memberComponent := range compilation.Component {
		if memberComponent != component || !candidates[member] {
			continue
		}
		contract := seeds[member]
		contract.GPRClobbers |= recursiveGPR
		contract.FPRClobbers |= recursiveFPR
		contract.CalleeGPRs = contract.GPRClobbers &^ callerGPRMask
		contract.CalleeFPRs = contract.FPRClobbers &^ callerFPRMask
		contracts[member] = contract
		refined[member] = true
	}
	if !verifyRecursiveContractClosure(compilation, component, seeds, candidates, contracts, config) {
		for member, memberComponent := range compilation.Component {
			if memberComponent == component {
				contracts[member] = railmach.ABIContract{}
				candidates[member] = false
				refined[member] = false
			}
		}
	}
}

func verifyRecursiveContractClosure(compilation compilationPlan, component int, seeds []railmach.ABIContract, candidates []bool, contracts []railmach.ABIContract, config railmach.GreedyConfig) bool {
	callerGPRMask := config.CallerMask(railmach.BankGPR)
	callerFPRMask := config.CallerMask(railmach.BankFPR)
	var recursiveGPR, recursiveFPR uint64
	for member, memberComponent := range compilation.Component {
		if memberComponent != component {
			continue
		}
		if member >= len(candidates) || !candidates[member] || member >= len(seeds) || member >= len(contracts) {
			return false
		}
		recursiveGPR |= seeds[member].GPRClobbers & callerGPRMask
		recursiveFPR |= seeds[member].FPRClobbers & callerFPRMask
	}
	for member, memberComponent := range compilation.Component {
		if memberComponent != component {
			continue
		}
		expected := seeds[member]
		expected.GPRClobbers |= recursiveGPR
		expected.FPRClobbers |= recursiveFPR
		expected.CalleeGPRs = expected.GPRClobbers &^ callerGPRMask
		expected.CalleeFPRs = expected.FPRClobbers &^ callerFPRMask
		if contracts[member] != expected {
			return false
		}
	}
	return true
}

func recursiveRefinementFitsContract(plan *nativeBackendPlan, contract railmach.ABIContract) bool {
	return plan != nil && plan.LocalABI.GPRClobbers&^contract.GPRClobbers == 0 && plan.LocalABI.FPRClobbers&^contract.FPRClobbers == 0
}

func recursiveRefinementPreferred(plan *nativeBackendPlan, contract railmach.ABIContract, conservative railmach.ScheduleScore) bool {
	return recursiveRefinementFitsContract(plan, contract) && !conservative.BetterThan(plan.Score)
}
