//go:build linux && !tinygo

package riscv64

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"sync"

	"github.com/wago-org/wago/src/core/compiler/backend/railshot/shared"
	"github.com/wago-org/wago/src/core/compiler/wasm"
	enc "github.com/wago-org/wago/src/core/encoder/riscv64"
	"github.com/wago-org/wago/src/core/runtime/abi"
	native "github.com/wago-org/wago/src/core/runtime/riscv64"
)

const (
	MaxProductionMemories = int(wasm.DefaultMaxMemoriesPerModule)
	MaxModuleBytes        = 1 << 20
	MaxFunctions          = 1024
	MaxCodeBytes          = 16 << 20
	MaxFrameBytes         = 128 << 10
)

var ErrUnsupported = errors.New("rv64: unsupported experimental integer shape")
var ErrCancelable = errors.New("rv64: cancelable invocation is not implemented")

// FunctionInfo describes the emitted whole-function path and frame.
type FunctionInfo struct {
	Entry, CodeBytes, Params, Results, FrameBytes, SpillSlots, Spills, Reloads int
	Result64                                                                   bool
	Fallback                                                                   bool
	FrameHeaderBytes, DirectCalls                                              int
	mode                                                                       emissionMode
}

// Compiled owns immutable heap code and metadata, never executable mappings.
// Every function/type/export was admitted before any instruction was emitted.
type Compiled struct {
	profileModuleState
	code      []byte
	functions []FunctionInfo
	exports   map[string]int
	state     moduleState
	// Selected staging compares the caller's symbolic plans with the directory
	// that actually emitted tail sites, including zero sites and source homes.
	// This bounded compile-time witness never becomes runtime metadata.
	finalizedReferenceImportTails []tailImportPlan
	referenceImportTailsFinalized bool
}

func (c *Compiled) Functions() []FunctionInfo {
	if c == nil {
		return nil
	}
	return append([]FunctionInfo(nil), c.functions...)
}

// Compile snapshots a bounded Wasm binary, normally decodes and validates it,
// admits the complete module, then selects shared scalar or integer fallback
// lowering for each whole function before emitting any code.
// Caller input must not be mutated concurrently with the initial snapshot.
func Compile(data []byte) (*Compiled, error) { return CompileWithOptions(data, CompileOptions{}) }

// CompileWithOptions retains the isolated experimental guest-context ABI.
// CompileModuleWith is the production shared compiler entry point.
func CompileWithOptions(data []byte, opts CompileOptions) (*Compiled, error) {
	return compileOptions(data, scalarRegisters[:], opts)
}

func compile(data []byte, regs []uint8) (*Compiled, error) {
	return compileOptions(data, regs, CompileOptions{})
}
func compileOptions(data []byte, regs []uint8, opts CompileOptions) (*Compiled, error) {
	if opts.referenceFunctions || opts.NativeReferenceFunctions {
		return nil, unsupported("private reference functions require decoded closed-module admission")
	}
	if opts.NativeReferenceCalls && (!opts.production || !opts.InvocationContext || !opts.Interruptible || !opts.ForeignTableCalls) {
		return nil, unsupported("call_ref requires retained cooperative production ABI")
	}

	if opts.localTailCalls && (!opts.production || !opts.Interruptible) {
		return nil, unsupported("local tails require the production cooperative runtime ABI")
	}
	if opts.ForeignTableCalls && (!opts.production || !opts.Interruptible) {
		return nil, unsupported("foreign table calls require the production cooperative runtime ABI")
	}
	if opts.Interruptible && !opts.production {
		return nil, unsupported("cooperative polls require the production trap ABI")
	}
	if len(data) > MaxModuleBytes {
		return nil, fmt.Errorf("rv64: module exceeds %d bytes", MaxModuleBytes)
	}
	owned := append([]byte(nil), data...)
	decoded, err := wasm.DecodeModuleByteBackedWithLimits(owned, wasm.ValidationFeatures{}, wasm.DecodeLimits{MaxTypes: MaxFunctions, MaxMetadataBytes: 16 << 20})
	if err != nil {
		return nil, err
	}
	m := decoded.Module
	if err = wasm.ValidateModuleWithConfig(m, wasm.ValidationFeatures{}, 1, wasm.ValidationLimits{MaxFunctionLocals: 256}); err != nil {
		return nil, err
	}
	if err = admitWireMode(owned, opts.production); err != nil {
		return nil, err
	}
	if opts.production {
		if err = admitProductionWire(owned); err != nil {
			return nil, err
		}
	}
	return compileDecodedModule(m, regs, opts)
}

// The public frontend has already validated its configured limits and retains
// the decoded source. Lowering still enforces the bounded RV64 module policy.
func compileDecodedModule(m *wasm.Module, regs []uint8, opts CompileOptions) (*Compiled, error) {
	if opts.NativeReferenceCalls && (!opts.production || !opts.InvocationContext || !opts.Interruptible || !opts.ForeignTableCalls) {
		return nil, unsupported("call_ref requires retained cooperative production ABI")
	}

	if opts.dynamicTailCalls && (!opts.importTailCalls || !opts.InvocationContext || !opts.ForeignTableCalls) {
		return nil, unsupported("private dynamic tails require invocation/foreign factory contract")
	}
	if opts.InvocationContext && (!opts.production || !opts.Interruptible) {
		return nil, unsupported("invocation views require cooperative production")
	}
	if opts.importTailCalls && (!opts.localTailCalls || opts.tailPlans == nil || len(*opts.tailPlans) != 0 || opts.tailSiteBase > tailRAMaxSites) {
		return nil, unsupported("private imported-tail plan contract")
	}
	if opts.localTailCalls && (!opts.production || !opts.Interruptible || opts.Profile || opts.SourceMaps || opts.UnwindMaps) {
		return nil, unsupported("local tails require cooperative production without profiling metadata")
	}
	if m == nil {
		return nil, fmt.Errorf("rv64: nil module")
	}
	if opts.GCFrameRoots != nil && !validateGCFrameRootPlans(m, opts.GCFrameRoots) {
		return nil, unsupported("GC frame-root source plan")
	}
	if opts.referenceFunctions {
		if !opts.production || !opts.publicStarts || !opts.Interruptible || !opts.ForeignTableCalls || !opts.InvocationContext || opts.dynamicTailCalls && !opts.NativeTailCalls || opts.Profile || opts.SourceMaps || opts.UnwindMaps {
			return nil, unsupported("closed reference module requires private cooperative production")
		}
		if err := admitClosedReferenceFunctionModule(m, false); err != nil {
			return nil, err
		}
		if err := wasm.ValidateModuleWithConfig(m, wasm.ValidationFeatures{ExtendedConstGlobals: opts.publicStarts, MultiMemory: opts.publicStarts}, 1, wasm.ValidationLimits{MaxFunctionLocals: 256}); err != nil {
			return nil, err
		}
	}
	var err error
	if err = admitShapeProfileFunctions(m, opts.production, opts.publicStarts, opts.production && opts.publicStarts, opts.NativeReferenceCalls || opts.gcRootedScalar || opts.gcTypeSubtypingRecursiveFunctions || opts.gcTypeSubtypingRefFuncGlobals || opts.gcTypeSubtypingRuntimeCallCast || opts.gcTypeSubtypingRefTestSingle || opts.gcTypeSubtypingDuplicateRecursive != 0 || opts.gcTypeSubtypingExtendedRecursive != 0 || opts.i31Core || opts.gcRefEq || opts.gcRefTestTable || opts.gcRefTestConcrete || opts.gcRefTestAbstract || opts.gcStructGet || opts.gcStructMutation || opts.gcStructNull || opts.gcStructPacked || opts.gcStructBasic || opts.gcStructGlobals || opts.gcArrayLocal || opts.gcArrayUniform || opts.gcArrayFixedLocal || opts.gcArrayFixedOfficial || opts.gcArrayPackedData || opts.gcArrayReference || opts.gcArrayInitElem || opts.gcArrayInitData || opts.gcArrayFill || opts.gcArrayCopy, opts.referenceFunctions || opts.gcRootedScalar || opts.gcTypeSubtypingRecursiveFunctions || opts.gcTypeSubtypingRefFuncGlobals || opts.gcTypeSubtypingRuntimeCallCast || opts.gcTypeSubtypingRefTestSingle || opts.gcTypeSubtypingDuplicateRecursive != 0 || opts.gcTypeSubtypingExtendedRecursive != 0 || opts.i31Core || opts.gcRefEq || opts.gcRefTestTable || opts.gcRefTestConcrete || opts.gcRefTestAbstract || opts.gcStructGet || opts.gcStructMutation || opts.gcStructNull || opts.gcStructPacked || opts.gcStructBasic || opts.gcStructGlobals || opts.gcArrayLocal || opts.gcArrayUniform || opts.gcArrayFixedLocal || opts.gcArrayFixedOfficial || opts.gcArrayPackedData || opts.gcArrayReference || opts.gcArrayInitElem || opts.gcArrayInitData || opts.gcArrayFill || opts.gcArrayCopy); err != nil {
		return nil, err
	}
	state, err := admitStatePolicy(m, opts.production, opts.hostImports, opts.publicStarts, opts.production && opts.publicStarts && opts.Interruptible && opts.ForeignTableCalls)
	if err != nil {
		return nil, err
	}
	state.localTailCalls = opts.localTailCalls
	state.foreignTableCalls = opts.ForeignTableCalls
	state.importTailCalls = opts.importTailCalls
	state.dynamicTailCalls = opts.dynamicTailCalls
	state.referenceCalls = opts.NativeReferenceCalls || opts.gcRootedScalar || opts.gcTypeSubtypingRecursiveFunctions || opts.gcRefTestConcrete || opts.gcRefTestAbstract || opts.gcStructBasic || opts.gcArrayFixedOfficial || opts.gcArrayPackedData || opts.gcArrayReference || opts.gcArrayInitElem || opts.gcArrayInitData || opts.gcArrayFill || opts.gcArrayCopy
	state.referenceFunctions = opts.referenceFunctions || opts.gcTypeSubtypingRecursiveFunctions || opts.gcTypeSubtypingRefFuncGlobals || opts.gcTypeSubtypingRuntimeCallCast || opts.gcTypeSubtypingRefTestSingle || opts.gcTypeSubtypingDuplicateRecursive != 0 || opts.gcTypeSubtypingExtendedRecursive != 0
	state.gcRootedScalar = opts.gcRootedScalar
	state.publicReferenceCarriers = opts.referenceFunctions && (admitPublicReferenceCarrierBoundary(m) && publicReferenceCarrierTarget(&m.Types[0].SubTypes[0].Comp) || admitMutableFunctionGlobalLocalCall(m) && publicReferenceVectorCarrierTarget(&m.Types[0].SubTypes[0].Comp))
	state.publicDynamicHostReferenceCarriers = state.publicReferenceCarriers && admitPublicDynamicHostReferenceBoundary(m)
	state.i31Core = opts.i31Core || opts.gcRefEq || opts.gcRefTestTable || opts.gcRefTestConcrete || opts.gcRefTestAbstract || opts.gcStructGet || opts.gcStructMutation || opts.gcStructNull || opts.gcStructPacked || opts.gcStructBasic || opts.gcStructGlobals || opts.gcArrayLocal || opts.gcArrayUniform || opts.gcArrayFixedLocal || opts.gcArrayFixedOfficial || opts.gcArrayPackedData || opts.gcArrayReference || opts.gcArrayInitElem || opts.gcArrayInitData || opts.gcArrayFill || opts.gcArrayCopy
	state.i31RefTest = opts.i31Core && admitRV64I31OfficialRefTest(m)
	state.i31RefEq = opts.gcRootedScalar || (opts.i31Core && admitRV64I31RefEqPilot(m)) || opts.gcRefEq
	state.gcRefEq = opts.gcRefEq
	state.gcRefTestTable = opts.gcRefTestTable
	state.gcRefTestConcrete = opts.gcRefTestConcrete
	state.gcRefTestAbstract = opts.gcRefTestAbstract
	state.gcBrOnCastNullability = opts.gcBrOnCastNullability
	state.gcStructGet = opts.gcStructGet
	state.gcStructMutation = opts.gcStructMutation
	state.gcStructNull = opts.gcStructNull
	state.gcStructPacked = opts.gcStructPacked
	state.gcStructBasic = opts.gcStructBasic
	state.gcStructGlobals = opts.gcStructGlobals
	state.gcArrayLocal = opts.gcArrayLocal
	state.gcArrayUniform = opts.gcArrayUniform
	state.gcArrayFixedLocal = opts.gcArrayFixedLocal
	state.gcArrayFixedOfficial = opts.gcArrayFixedOfficial
	state.gcArrayPackedData = opts.gcArrayPackedData
	state.gcArrayReference = opts.gcArrayReference
	state.gcArrayInitElem = opts.gcArrayInitElem
	state.gcArrayInitData = opts.gcArrayInitData
	state.gcArrayInitDataWide = opts.gcArrayInitData && admitRV64GCArrayInitDataWide(m)
	state.gcArrayFill = opts.gcArrayFill
	state.gcArrayCopy = opts.gcArrayCopy
	state.gcTypeSubtypingRecursiveFunctions = opts.gcTypeSubtypingRecursiveFunctions
	state.gcTypeSubtypingRefFuncGlobals = opts.gcTypeSubtypingRefFuncGlobals
	state.gcTypeSubtypingRuntimeCallCast = opts.gcTypeSubtypingRuntimeCallCast
	state.gcTypeSubtypingFinalityCallCast = opts.gcTypeSubtypingFinalityCallCast
	state.gcTypeSubtypingTypedTableCall = opts.gcTypeSubtypingTypedTableCall
	state.gcTypeSubtypingRefTestSingle = opts.gcTypeSubtypingRefTestSingle
	state.gcTypeSubtypingRefTestVariant = opts.gcTypeSubtypingRefTestVariant
	state.gcTypeSubtypingLink = opts.gcTypeSubtypingLink
	state.gcTypeSubtypingDuplicateRecursive = opts.gcTypeSubtypingDuplicateRecursive != 0 || opts.gcTypeSubtypingExtendedRecursive != 0
	// Internal GC bridges stage their arguments in the same serialized arena as
	// ordinary Wasm calls. A module whose public functions take fewer arguments
	// must still retain the complete helper shape; otherwise helper staging can
	// overwrite the adjacent result and trap cells before the host park.
	helperSlots := 0
	switch {
	case opts.gcRootedScalar:
		helperSlots = 4
	case opts.gcArrayCopy:
		helperSlots = 7
	case opts.gcArrayInitElem || opts.gcArrayInitData:
		helperSlots = 6
	case opts.gcArrayFixedLocal || opts.gcArrayFixedOfficial || opts.gcArrayFill:
		helperSlots = 5
	case opts.gcRefTestTable || opts.gcRefTestConcrete || opts.gcRefTestAbstract || opts.gcStructMutation || opts.gcStructNull || opts.gcStructPacked || opts.gcStructBasic || opts.gcArrayLocal || opts.gcArrayUniform || opts.gcArrayPackedData || opts.gcArrayReference:
		helperSlots = 4
	case opts.gcStructGet:
		helperSlots = 3
	case opts.gcRefEq:
		helperSlots = 2
	}
	state.capacity = max(state.capacity, helperSlots)
	state.core3RefIsNullExecution = opts.referenceFunctions && isCore3RefIsNullExecution(m)
	state.declarationExternRefs = opts.referenceFunctions && (admitCore3RefDeclarations(m) || admitCore3BrOnNullDeclarations(m) || admitCore3BrOnNonNullDeclarations(m))
	state.declarationNonnullRefs = opts.referenceFunctions && (admitCore3RefDeclarations(m) || admitCore3RefAsNonNullDeclarations(m) || admitCore3RefIsNullDeclarations(m))
	summaries := make([]shared.ScalarSummary, len(m.Code))
	importTailCount := 0
	integerSummaries := make([]integerSummary, len(m.Code))
	fallback := make([]bool, len(m.Code))
	for i := range m.Code {
		ft, _ := m.LocalFuncType(i)
		summaries[i] = shared.AdmitScalarFunction(&m.Code[i], ft)
		fallback[i] = opts.ForceFallback || opts.gcRootedScalar || opts.referenceFunctions || opts.gcTypeSubtypingRecursiveFunctions || opts.gcTypeSubtypingRefFuncGlobals || opts.gcTypeSubtypingRuntimeCallCast || opts.gcTypeSubtypingRefTestSingle || opts.gcTypeSubtypingDuplicateRecursive != 0 || opts.gcTypeSubtypingExtendedRecursive != 0 || opts.i31Core || opts.gcRefEq || opts.gcRefTestTable || opts.gcRefTestConcrete || opts.gcRefTestAbstract || opts.gcStructGet || opts.gcStructMutation || opts.gcStructNull || opts.gcStructPacked || opts.gcStructBasic || opts.gcArrayLocal || opts.gcArrayUniform || opts.gcArrayFixedLocal || opts.gcArrayFixedOfficial || opts.gcArrayPackedData || opts.gcArrayReference || opts.gcArrayInitElem || opts.gcArrayInitData || opts.gcArrayFill || opts.gcArrayCopy || !summaries[i].Eligible
		if fallback[i] {
			integerSummaries[i], err = walkInteger(&m.Code[i], ft, &state, nil)
			if err != nil {
				return nil, fmt.Errorf("rv64: function %d: %w", i, err)
			}
			importTailCount += integerSummaries[i].importTails
			if opts.importTailCalls && importTailCount > tailRAMaxSites-int(opts.tailSiteBase) {
				return nil, unsupported("private imported-tail module site bound")
			}
		}
	}
	c := &Compiled{profileModuleState: newProfileModule(opts.production && opts.Profile, opts.SourceMaps, len(m.Code)), state: state, functions: make([]FunctionInfo, len(m.Code)), exports: make(map[string]int, len(m.Exports))}
	var worker shared.ScalarState
	var relocations []directCall
	defer worker.FinishWorker()
	for i := range m.Code {
		ft, _ := m.LocalFuncType(i)
		var code []byte
		var info FunctionInfo
		var calls []directCall
		mode := emissionMode{profileMode: c.newFunctionProfile(&m.Code[i], uint32(i+state.importedFunctions)), production: opts.production, interruptible: opts.Interruptible, foreignTableCalls: opts.ForeignTableCalls, invocationContext: opts.InvocationContext, localTailCalls: opts.localTailCalls, importTailCalls: opts.importTailCalls, dynamicTailCalls: opts.dynamicTailCalls, referenceCalls: opts.NativeReferenceCalls || opts.gcRootedScalar, gcTypeSubtypingRuntimeCallCast: opts.gcTypeSubtypingRuntimeCallCast, referenceFunctions: opts.referenceFunctions || opts.gcTypeSubtypingRecursiveFunctions || opts.gcTypeSubtypingRefFuncGlobals || opts.gcTypeSubtypingRuntimeCallCast || opts.gcTypeSubtypingRefTestSingle || opts.gcTypeSubtypingDuplicateRecursive != 0 || opts.gcTypeSubtypingExtendedRecursive != 0, tailPlans: opts.tailPlans, tailSiteBase: opts.tailSiteBase}
		if opts.production {
			mode.functionIndex = uint32(i + state.importedFunctions)
			mode.entryPC = uint32(m.Code[i].LocalDeclBytes)
		}
		if opts.GCFrameRoots != nil {
			mode.gcFrameRoots = opts.GCFrameRoots.Function(i)
		}
		if mode.gcFrameRoots != nil && !fallback[i] {
			return nil, unsupported("GC frame roots require framed integer lowering")
		}
		if fallback[i] {
			code, info, calls, err = compileIntegerMode(&m.Code[i], ft, &state, integerSummaries[i], mode)
		} else {
			code, info, err = compileFunctionMode(&worker, &m.Code[i], ft, summaries[i], regs, mode)
		}
		if err != nil {
			return nil, fmt.Errorf("rv64: function %d: %w", i, err)
		}
		padding := 0
		if opts.production {
			padding = (-len(c.code)) & 15
		}
		if len(code) > MaxCodeBytes-len(c.code)-padding {
			return nil, fmt.Errorf("rv64: emitted code exceeds %d bytes", MaxCodeBytes)
		}
		for ; padding > 0; padding -= 4 {
			c.code = append(c.code, 0x13, 0, 0, 0) // fixed-width RV64 NOP
		}
		info.Entry = len(c.code)
		info.CodeBytes = len(code)
		if err := c.retainProfile(&info); err != nil {
			return nil, err
		}
		c.code = append(c.code, code...)
		c.functions[i] = info
		for j := range calls {
			calls[j].site += info.Entry
		}
		relocations = append(relocations, calls...)
	}
	// Append exact internal helper bridges only when an admitted checkpoint
	// references them. They are ordinary image-relative RV64 code;
	// no executable address or alternate-backend implementation is retained.
	type helperBridge struct {
		helper    uint32
		slots     int
		safepoint uint32
	}
	helperEntries := make(map[helperBridge]int, 12)
	bridges := []helperBridge{{helper: gcStructAllocDefault, slots: 1}, {helper: gcStructAllocOne, slots: 2}, {helper: gcStructAllocOne, slots: 4}, {helper: gcStructGet, slots: 3}, {helper: gcStructSet, slots: 4}, {helper: gcStructGetS, slots: 3}, {helper: gcStructGetU, slots: 3}, {helper: gcStructRefTest, slots: 4}, {helper: gcStructRefCast, slots: 4}, {helper: gcStructTableSet, slots: 3}, {helper: gcAnyConvertExtern, slots: 1}, {helper: gcExternConvertAny, slots: 1}, {helper: gcArrayAllocDefault, slots: 2}, {helper: gcArrayAllocFixed, slots: 4}, {helper: gcArrayAllocFixed, slots: 5}, {helper: gcArrayAllocUniform, slots: 3}, {helper: gcArrayAllocData, slots: 4}, {helper: gcArrayAllocElem, slots: 4}, {helper: gcArrayDropElem, slots: 1}, {helper: gcArrayFill, slots: 5}, {helper: gcArrayCopy, slots: 7}, {helper: gcArrayGet, slots: 3}, {helper: gcArrayGetS, slots: 3}, {helper: gcArrayGetU, slots: 3}, {helper: gcArraySet, slots: 4}, {helper: gcArrayLen, slots: 1}, {helper: gcArrayInitData, slots: 6}, {helper: gcArrayInitElem, slots: 6}, {helper: guestGlobalClear}, {helper: guestGlobalStore, slots: 1}, {helper: guestGlobalStoreIndexed, slots: 2}}
	// Keep the historical zero-safepoint bridge order byte-for-byte stable. Exact
	// root plans add only the distinct helper/safepoint pairs actually referenced
	// by this image.
	for _, call := range relocations {
		if call.helper == 0 || call.safepoint == 0 {
			continue
		}
		slots, ok := gcAllocHelperSlots(call.helper, call.helperSlots)
		if !ok {
			return nil, unsupported("missing canonical GC helper bridge shape")
		}
		key := helperBridge{helper: call.helper, slots: slots, safepoint: call.safepoint}
		seen := false
		for _, bridge := range bridges {
			if bridge == key {
				seen = true
				break
			}
		}
		if !seen {
			bridges = append(bridges, key)
		}
	}
	for _, bridge := range bridges {
		used := false
		for _, call := range relocations {
			used = used || call.helper == bridge.helper && call.safepoint == bridge.safepoint && (call.helper != gcArrayAllocFixed && call.helper != gcStructAllocOne || call.helperSlots == bridge.slots)
		}
		if !used {
			continue
		}
		padding := (-len(c.code)) & 15
		for ; padding > 0; padding -= 4 {
			c.code = append(c.code, 0x13, 0, 0, 0)
		}
		paramSlots, resultSlots := bridge.slots, 1
		if bridge.helper == gcStructSet || bridge.helper == gcStructTableSet || bridge.helper == gcArraySet || bridge.helper == gcArrayDropElem || bridge.helper == gcArrayFill || bridge.helper == gcArrayCopy || bridge.helper == gcArrayInitData || bridge.helper == gcArrayInitElem {
			resultSlots = 0
		}
		dispatch := guestGlobalStoreDispatch
		if bridge.helper != guestGlobalClear && bridge.helper != guestGlobalStore && bridge.helper != guestGlobalStoreIndexed {
			payload, ok := shared.EncodeGCDispatch(bridge.helper, bridge.safepoint)
			if !ok {
				return nil, unsupported("GC helper dispatch encoding")
			}
			dispatch = gcStructDispatchBit | payload
		} else {
			resultSlots = 0
		}
		stub := HostIndirectOwnedSyncThunk(dispatch, paramSlots, resultSlots)
		if len(stub) == 0 || len(stub) > MaxCodeBytes-len(c.code) {
			return nil, unsupported("internal helper bridge image bound")
		}
		key := bridge
		if key.helper != gcArrayAllocFixed && key.helper != gcStructAllocOne {
			key.slots = 0
		}
		helperEntries[key] = len(c.code)
		c.code = append(c.code, stub...)
	}
	// All framed entries are now known. These remain image-relative transfers;
	// no executable addresses or alternate-backend code are retained.
	image := enc.Asm{B: c.code}
	for _, call := range relocations {
		target := 0
		if call.helper != 0 {
			var ok bool
			keySlots := 0
			if call.helper == gcArrayAllocFixed || call.helper == gcStructAllocOne {
				keySlots = call.helperSlots
			}
			target, ok = helperEntries[helperBridge{helper: call.helper, slots: keySlots, safepoint: call.safepoint}]
			if !ok {
				return nil, unsupported("missing GC helper bridge")
			}
		} else {
			if uint64(call.callee) >= uint64(len(c.functions)) {
				return nil, unsupported("direct call relocation target")
			}
			target = c.functions[call.callee].Entry
		}
		if err := image.PatchLong(call.site, int64(target)); err != nil {
			return nil, fmt.Errorf("rv64: direct call relocation: %w", err)
		}
	}
	for _, ex := range m.Exports {
		if ex.Index.Kind == wasm.ExternFunc && ex.Index.Index >= uint32(state.importedFunctions) {
			c.exports[ex.Name] = int(ex.Index.Index) - state.importedFunctions
		}
	}
	if opts.referenceFunctions && opts.importTailCalls {
		c.finalizedReferenceImportTails = slices.Clone(*opts.tailPlans)
		c.referenceImportTailsFinalized = true
	}
	return c, nil
}

func unsupported(detail string) error { return fmt.Errorf("%w: %s", ErrUnsupported, detail) }

// The wire allowlist also rejects present-but-empty unsupported sections and
// explicit singleton rec groups whose provenance the decoded Module loses.
// Normal decoding/validation always runs first; this is an execution policy.
func admitWire(data []byte) error { return admitWireMode(data, false) }

func admitWireMode(data []byte, floating bool) error {
	return admitWireProfile(data, floating, false)
}

func admitWireProfile(data []byte, floating, vectors bool) error {
	return admitWireStartProfile(data, floating, vectors, false)
}

func admitWireStartProfile(data []byte, floating, vectors, publicStarts bool) error {
	r := wasm.ReaderFrom(data[8:])
	for r.HasNext() {
		id, err := r.Byte()
		if err != nil {
			return err
		}
		n, err := r.U32()
		if err != nil {
			return err
		}
		if uint64(n) > uint64(r.BytesLeft()) {
			return fmt.Errorf("rv64: truncated section")
		}
		start := r.Offset()
		end := start + int(n)
		switch id {
		case 0, 2, 3, 5, 6, 7, 10, 11, 12:
		case 8:
			if !publicStarts {
				return unsupported("defined start execution is not enabled")
			}
		case 4, 9:
			if !floating {
				return unsupported("isolated table/element sections")
			}
		case 1:
			s := wasm.ReaderFrom(data[8+start : 8+end])
			count, err := s.U32()
			if err != nil {
				return err
			}
			for i := uint32(0); i < count; i++ {
				kind, err := s.Byte()
				if err != nil {
					return err
				}
				explicitSingleton := false
				if kind == 0x4e {
					// Public decoding flattens an explicit one-member rec
					// wrapper to the same final function type as the compact
					// 0x60 form. Keep private wire profiles closed, and do not
					// admit subtype prefixes or reference-bearing recursive
					// syntax through this representation-only exception.
					if !publicStarts {
						return unsupported("explicit recursive type group requires public production")
					}
					members, err := s.U32()
					if err != nil || members != 1 {
						return unsupported("explicit recursive type group must contain one function")
					}
					kind, err = s.Byte()
					if err != nil {
						return err
					}
					explicitSingleton = true
				}
				if kind != 0x60 {
					return unsupported("type grammar is not a plain function")
				}
				for v := 0; v < 2; v++ {
					n, err := s.U32()
					if err != nil {
						return err
					}
					if n > 256 {
						return unsupported("signature arity")
					}
					for j := uint32(0); j < n; j++ {
						t, err := s.Byte()
						if err != nil {
							return err
						}
						// Wire admission preserves plain function grammar. Declared
						// internal references are checked against the decoded type
						// directory and feature permission before staging.
						if publicStarts && t == 0x70 {
							continue
						}
						if publicStarts && (t == 0x63 || t == 0x64) {
							heap, err := s.S33()
							if err != nil {
								return err
							}
							// An explicit singleton wrapper adds no recursive
							// identity to an abstract function reference. Keep self or
							// forward indices and every non-function heap form outside
							// this representation-only exception; decoded feature
							// admission still distinguishes nullable from typed control.
							if explicitSingleton {
								if (t == 0x63 || t == 0x64) && heap == -16 {
									continue
								}
								// All admitted preceding groups are singletons, so a
								// strictly earlier group index is also its flattened
								// absolute type index. Decoded admission still proves
								// that it names a numeric function signature.
								if heap >= 0 && uint64(heap) < uint64(i) {
									continue
								}
								return unsupported("singleton recursive reference type")
							}
							if heap == -16 || heap >= 0 && uint64(heap) < uint64(count) {
								continue
							}
							return unsupported("declared function reference heap")
						}
						if t != 0x7f && t != 0x7e && (!floating || t != 0x7d && t != 0x7c) && (!vectors || t != 0x7b) {
							return unsupported("noninteger type")
						}
					}
				}
			}
			if s.HasNext() {
				return unsupported("extra type bytes")
			}
		default:
			return unsupported(fmt.Sprintf("section %d", id))
		}
		if err = r.JumpTo(end); err != nil {
			return err
		}
	}
	return nil
}

func admitShape(m *wasm.Module) error { return admitShapeMode(m, false) }

func admitShapeMode(m *wasm.Module, floating bool) error {
	return admitShapeProfile(m, floating, false)
}

func admitShapeProfile(m *wasm.Module, floating, publicStarts bool) error {
	return admitShapeProfileReferences(m, floating, publicStarts, false, false)
}
func admitShapeProfileReferences(m *wasm.Module, floating, publicStarts, referencesAllowed, typed bool) error {
	return admitShapeProfileFunctions(m, floating, publicStarts, referencesAllowed, typed, false)
}
func admitShapeProfileFunctions(m *wasm.Module, floating, publicStarts, referencesAllowed, typed, referenceFunctions bool) error {
	if referenceFunctions && admitRV64GCRootedScalarModule(m) {
		return nil
	}
	if admitRV64GCTypeSubtypingDeclarations(m) != 0 || admitRV64GCTypeSubtypingRecursiveFunctions(m) != 0 || admitRV64GCTypeSubtypingRefFuncGlobals(m) != 0 || admitRV64GCTypeSubtypingRuntimeCallCast(m) || admitRV64GCTypeSubtypingRuntimeFinalityCallCast(m) || admitRV64GCTypeSubtypingRuntimeTypedTableCall(m) || admitRV64GCTypeSubtypingRefTestSingle(m) || admitRV64GCTypeSubtypingLink(m) != 0 || admitRV64GCTypeSubtypingFinalityLink(m) != 0 || admitRV64GCTypeSubtypingStructLink(m) != 0 || admitRV64GCTypeSubtypingStructProjectionLink(m) != 0 || admitRV64GCTypeSubtypingStructMismatchLink(m) != 0 || admitRV64GCTypeSubtypingIndependentStructLink(m) != 0 || admitRV64GCTypeSubtypingExtendedProjectionLink(m) != 0 || admitRV64GCTypeSubtypingDuplicateRecursiveLink(m) != 0 || admitRV64GCTypeSubtypingExtendedRecursiveLink(m) != 0 || admitRV64GCTypeSubtypingCrossGroupMismatchLink(m) != 0 || admitRV64GCTypeSubtypingTransitiveMismatchLink(m) != 0 || admitRV64GCTypeSubtypingNonFlatExport(m) {
		return nil
	}
	if referenceFunctions && (admitRV64GCRefEq(m) || admitRV64GCRefTestTable(m) || admitRV64GCRefTestConcrete(m) || admitRV64GCRefCastConcrete(m) || admitRV64GCBrOnCastConcrete(m) || admitRV64GCBrOnCastFailConcrete(m) || admitRV64GCBrOnCastNullability(m) || admitRV64GCBrOnCastFailNullability(m) || admitRV64GCRefTestAbstract(m) || admitRV64GCExtern(m) || admitRV64GCRefCastAbstract(m) || admitRV64GCBrOnCastAbstract(m) || admitRV64GCBrOnCastFailAbstract(m) || admitRV64GCStructGet(m) || admitRV64GCStructMutation(m) || admitRV64GCStructNull(m) || admitRV64GCStructPacked(m) || admitRV64GCStructBasic(m) || admitRV64GCStructNumericGlobals(m) || admitRV64GCArrayLocal(m) || admitRV64GCArrayUniform(m) || admitRV64GCArrayFixedLocal(m) || admitRV64GCArrayOfficial(m) || admitRV64GCArrayPackedData(m) || admitRV64GCArrayReference(m) || admitRV64GCArrayInitElem(m) || admitRV64GCArrayInitData(m) || admitRV64GCArrayFill(m) || admitRV64GCArrayCopy(m)) {
		return nil
	}
	i31Core := referenceFunctions && admitRV64I31Core(m)
	declarationExtern := referenceFunctions && (admitCore3RefDeclarations(m) || admitCore3BrOnNullDeclarations(m) || admitCore3BrOnNonNullDeclarations(m))
	executionExtern := referenceFunctions && isCore3RefIsNullExecution(m)
	declarationNonNullExtern := referenceFunctions && (admitCore3RefDeclarations(m) || admitCore3RefAsNonNullDeclarations(m) || admitCore3RefIsNullDeclarations(m))
	publicReferenceCarriers := referenceFunctions && (admitPublicReferenceCarrierBoundary(m) && publicReferenceCarrierTarget(&m.Types[0].SubTypes[0].Comp) || admitMutableFunctionGlobalLocalCall(m) && publicReferenceVectorCarrierTarget(&m.Types[0].SubTypes[0].Comp))
	// Public modules may contain only state. Import-only modules execute through
	// instance-owned bridges; neither shape needs placeholder instructions.
	if len(m.Code) > MaxFunctions || len(m.Code) == 0 && (!floating || !publicStarts) {
		return unsupported("function count outside 1..1024")
	}
	// Public decoding has already flattened compact groups into the ordinary
	// ordered import list. Private pilots retain their narrower wire contract.
	if m.UsesCompactImports && !publicStarts {
		return unsupported("compact imports require public production")
	}
	if publicStarts {
		if err := admitProductionStart(m); err != nil {
			return err
		}
	}
	if (!floating && (len(m.Tables) != 0 || len(m.Elements) != 0)) || len(m.Tags) != 0 || len(m.StringRefs) != 0 || m.Start != nil && !publicStarts || !publicStarts && m.BranchHints != nil {
		return unsupported("imports, state, initializers or feature metadata")
	}
	for _, custom := range m.Customs {
		// Public compilation ignores opaque custom payloads after structured
		// branch hints have been reconciled and validated. Private pilots keep
		// their narrow custom-section allowlist.
		if !publicStarts && custom.Name != "name" && custom.Name != "producers" {
			return unsupported("custom section " + custom.Name)
		}
	}
	for _, rec := range m.Types {
		if len(rec.SubTypes) != 1 {
			return unsupported("recursive type group")
		}
		s := &rec.SubTypes[0]
		if !s.Final || s.HasPrefix || len(s.Supers) != 0 || s.Metadata.Describes.Present() || s.Metadata.Descriptor.Present() || s.Comp.Kind != wasm.CompFunc {
			return unsupported("unsupported type metadata")
		}
		if len(s.Comp.Params) > 256 || len(s.Comp.Results) > 256 {
			return unsupported("signature arity")
		}
		if referencesAllowed {
			if _, _, err := admitDeclaredControlSignature(&s.Comp, internalReferenceTypes{decoded: m.Types, declarationExtern: declarationExtern, executionExtern: executionExtern, declarationNonNullExtern: declarationNonNullExtern, publicReferenceCarriers: publicReferenceCarriers, i31: i31Core}, true, typed, false); err != nil {
				return err
			}
		} else {
			for _, list := range [][]wasm.ValType{s.Comp.Params, s.Comp.Results} {
				for _, v := range list {
					if !scalarType(v, floating) {
						return unsupported("noninteger signature")
					}
				}
			}
		}
	}
	if referencesAllowed {
		imported := m.ImportedFuncCount()
		for i := 0; i < m.FuncCount(); i++ {
			ft, ok := m.FuncSignature(uint32(i))
			if !ok {
				return unsupported("numeric function binding")
			}
			if referenceFunctions && (i >= imported || publicReferenceCarriers && publicReferenceCarrierTarget(ft)) {
				if _, _, err := admitDeclaredControlSignature(ft, internalReferenceTypes{decoded: m.Types, declarationExtern: declarationExtern, executionExtern: executionExtern, declarationNonNullExtern: declarationNonNullExtern, publicReferenceCarriers: publicReferenceCarriers, i31: i31Core}, true, typed, false); err != nil {
					return err
				}
			} else if err := integerNumericSignature(ft, floating); err != nil {
				return err
			}
		}
	}

	return nil
}

func compileFunction(worker *shared.ScalarState, fn *wasm.Func, ft *wasm.CompType, summary shared.ScalarSummary, regs []uint8) ([]byte, FunctionInfo, error) {
	return compileFunctionMode(worker, fn, ft, summary, regs, emissionMode{})
}

func compileFunctionMode(worker *shared.ScalarState, fn *wasm.Func, ft *wasm.CompType, summary shared.ScalarSummary, regs []uint8, mode emissionMode) ([]byte, FunctionInfo, error) {
	var widths [256]bool
	nlocals := len(ft.Params)
	for i, t := range ft.Params {
		widths[i] = wasm.EqualValType(t, wasm.I64)
	}
	for _, run := range fn.Locals.Runs {
		for i := uint32(0); i < run.Count; i++ {
			widths[nlocals] = wasm.EqualValType(run.Type, wasm.I64)
			nlocals++
		}
	}
	f := target{regs: regs, nlocals: nlocals, mode: mode}
	if regallocCheckEnabled {
		defer f.checkEndLifetimes()
	}
	var slots int
	var err error
	if profileEnabled {
		slots, err = f.compileProfileScalar(worker, fn, summary, widths[:nlocals], len(ft.Params))
	} else {
		// Keep the ordinary call path direct even when the profiling wrapper
		// exceeds the Go compiler's inlining budget.
		slots, err = worker.CompileScalar(fn.BodyBytes, summary, widths[:nlocals], len(ft.Params), &f)
	}
	if err != nil {
		return nil, FunctionInfo{}, err
	}
	if f.err != nil {
		return nil, FunctionInfo{}, f.err
	}
	if f.a.Err() != nil {
		return nil, FunctionInfo{}, f.a.Err()
	}
	if err := f.finishProfileBody(); err != nil {
		return nil, FunctionInfo{}, err
	}
	info := FunctionInfo{Params: len(ft.Params), Results: 1, SpillSlots: slots, Spills: worker.Spills, Reloads: worker.Reloads, Result64: wasm.EqualValType(ft.Results[0], wasm.I64), mode: mode}
	return frameFunction(f.a.B, widths[:nlocals], len(ft.Params), slots, false, -1, info)
}

func frameFunction(body []byte, widths []bool, nparams, slots int, initialize bool, contextOffset int, info FunctionInfo) ([]byte, FunctionInfo, error) {
	return frameFunctionCalls(body, widths, nparams, slots, initialize, contextOffset, info, nil)
}
func frameFunctionCalls(body []byte, widths []bool, nparams, slots int, initialize bool, contextOffset int, info FunctionInfo, calls []directCall) ([]byte, FunctionInfo, error) {
	if info.mode.invocationContext && (!info.mode.production || !info.mode.interruptible) {
		return nil, FunctionInfo{}, unsupported("invocation views require cooperative production ABI")
	}
	nlocals := len(widths)
	if info.FrameHeaderBytes == 0 {
		info.FrameHeaderBytes = 16
	}
	frame, err := checkedFrameBytes(info.FrameHeaderBytes, nlocals, slots)
	if err != nil {
		return nil, FunctionInfo{}, err
	}
	entry := target{header: info.FrameHeaderBytes, mode: info.mode}
	fence := enc.X13
	if info.mode.production {
		entry.a.Ld(enc.X29, enc.X25, -abi.StackFenceOffset)
		fence = enc.X29
	}
	reversed := entry.a.BranchLong(enc.LTU, enc.SP, fence)
	entry.a.Constant64(enc.Scratch0, int64(frame))
	entry.a.Sub(enc.Scratch1, enc.SP, fence)
	bad := entry.a.BranchLong(enc.LTU, enc.Scratch1, enc.Scratch0)
	entry.emitProductionEntryPoll()
	entry.a.Sub(enc.SP, enc.SP, enc.Scratch0)
	entry.a.Sd(enc.RA, enc.SP, 0)
	result := enc.X11
	if info.mode.production {
		result = enc.X13
	}
	entry.a.Sd(result, enc.SP, 8)
	if info.FrameHeaderBytes == callFrameHeader {
		entry.a.Sd(enc.X10, enc.SP, 16)
		entry.a.Sd(enc.X12, enc.SP, 24)
		if info.mode.production {
			entry.a.Sd(enc.X11, enc.SP, 32)
		} else {
			entry.a.Sd(enc.X13, enc.SP, 32)
		}
	}
	viewGuards := entry.initializeNativeInvocation(frame, info)
	if contextOffset != -1 && !info.mode.production {
		if contextOffset <= 2047 {
			entry.a.Ld(enc.X24, enc.X10, int32(contextOffset))
		} else {
			entry.a.Constant64(enc.Scratch1, int64(contextOffset))
			entry.a.Add(enc.Scratch1, enc.X10, enc.Scratch1)
			entry.a.Ld(enc.X24, enc.Scratch1, 0)
		}
	}
	for i := 0; i < nparams; i++ {
		entry.a.Ld(enc.Scratch0, enc.X10, int32(i*8))
		entry.Store(entry.LocalOffset(i), uint8(enc.Scratch0), widths[i])
	}
	if initialize {
		for i := nparams; i < nlocals; i++ {
			entry.Store(entry.LocalOffset(i), uint8(enc.Zero), widths[i])
		}
	}
	for j := range calls {
		calls[j].site += entry.Position()
	}
	prefixBytes := entry.Position()
	entry.a.B = append(entry.a.B, body...)
	entry.a.Ld(enc.RA, enc.SP, 0)
	entry.a.Constant64(enc.Scratch0, int64(frame))
	entry.a.Add(enc.SP, enc.SP, enc.Scratch0)
	entry.a.Ret()
	epilogueBytes := entry.Position() - prefixBytes - len(body)
	if err := entry.a.PatchLong(bad, int64(entry.a.Len())); err != nil {
		return nil, FunctionInfo{}, err
	}
	if err := entry.a.PatchLong(reversed, int64(entry.a.Len())); err != nil {
		return nil, FunctionInfo{}, err
	}
	// The failure edge occurs before body allocation; original a2 and RA/SP
	// are still valid. No stack writes or SP changes precede this edge.
	if info.mode.production {
		entry.emitProductionTrap(abi.TrapStackFenceBreached, info.mode.entryPC)
	} else {
		entry.a.Addi(enc.Scratch0, enc.Zero, 1)
		entry.a.Sw(enc.Scratch0, enc.X12, 0)
		entry.a.Addi(enc.X10, enc.Zero, 0)
		entry.a.Ret()
	}
	if len(viewGuards) != 0 {
		for _, site := range viewGuards {
			if err := entry.a.PatchLong(site, int64(entry.a.Len())); err != nil {
				return nil, FunctionInfo{}, err
			}
		}
		entry.emitProductionTrap(abi.TrapCalledFnNotLinked, info.mode.entryPC)
	}
	if entry.err != nil {
		return nil, FunctionInfo{}, entry.err
	}
	if entry.a.Err() != nil {
		return nil, FunctionInfo{}, entry.a.Err()
	}
	if err := info.mode.finishProfileFrame(prefixBytes, len(body), epilogueBytes, entry.Position()); err != nil {
		return nil, FunctionInfo{}, err
	}
	info.FrameBytes = frame
	return entry.a.B, info, nil
}

// Instance owns all executable and call resources. It accepts noncancelable
// contexts; deadlines and host reentry are unsupported.
type Instance struct {
	mu        sync.Mutex
	closed    bool
	program   *native.Program
	engine    *native.Engine
	call      *native.CallFrame
	compiled  *Compiled
	memory    *native.LinearMemory
	lease     *native.MemoryLease
	context   *native.GuestContext
	ownMemory bool
}

// Instantiate publishes only previously admitted RV64 code. On error a nonnil
// Instance is closed and retains failed cleanup ownership; call Close to retry.
func (c *Compiled) Instantiate(stackBytes int) (*Instance, error) {
	return c.InstantiateWithOptions(stackBytes, InstantiateOptions{})
}
func (c *Compiled) InstantiateWithOptions(stackBytes int, opts InstantiateOptions) (*Instance, error) {
	if c == nil || len(c.code) == 0 {
		return nil, fmt.Errorf("rv64: empty compiled module")
	}
	if err := admitProductionFunctionABI(c); err != nil {
		return nil, err
	}
	i := &Instance{compiled: c}
	var err error
	i.memory, i.lease, i.ownMemory, err = c.state.bindMemory(opts)
	if err == nil {
		i.engine, err = native.NewEngine(stackBytes)
	}
	if err == nil {
		i.call, err = native.NewCallFrameLayout(c.state.capacity, c.state.resultCapacity)
	}
	if err == nil && (i.memory != nil || len(c.state.globals) != 0 || len(c.state.passiveData) != 0) {
		var descriptor uint64
		if i.lease != nil {
			descriptor, err = i.lease.Descriptor()
		}
		if err == nil {
			values := make([]uint64, len(c.state.globals))
			for j, g := range c.state.globals {
				values[j] = g.value
			}
			i.context, err = native.NewGuestContextWithData(descriptor, values, c.state.passiveData)
		}
	}
	if err == nil && i.lease != nil {
		err = i.lease.Initialize(c.state.data)
	}
	if err == nil {
		i.program, err = native.MapCode(c.code)
	}
	if err != nil {
		if cleanup := i.Close(); cleanup != nil {
			return i, errors.Join(err, cleanup)
		}
		return nil, err
	}
	return i, nil
}

// Invoke is the single-result convenience entry. It rejects other result arities
// before executing guest code; use InvokeInto for void/multivalue functions.
func (i *Instance) Invoke(ctx context.Context, name string, args ...uint64) (uint64, error) {
	var result [1]uint64
	err := i.InvokeInto(ctx, name, args, result[:])
	return result[0], err
}

// InvokeInto uses caller-owned results, with exact arity checked before entry.
// On errors, including guest traps, results remain unchanged.
func (i *Instance) InvokeInto(ctx context.Context, name string, args, results []uint64) error {
	if i == nil {
		return native.ErrClosed
	}
	i.mu.Lock()
	defer i.mu.Unlock()
	if i.closed || i.compiled == nil || i.engine == nil || i.program == nil || i.call == nil {
		return native.ErrClosed
	}
	if ctx == nil {
		return fmt.Errorf("rv64: nil invocation context")
	}
	_, deadline := ctx.Deadline()
	if ctx.Done() != nil || deadline || ctx.Err() != nil {
		return ErrCancelable
	}
	index, ok := i.compiled.exports[name]
	if !ok {
		return fmt.Errorf("rv64: unknown integer export %q", name)
	}
	f := i.compiled.functions[index]
	if len(args) != f.Params {
		return fmt.Errorf("rv64: export %q expects %d arguments, got %d", name, f.Params, len(args))
	}
	if len(results) != f.Results {
		return fmt.Errorf("rv64: export %q expects %d results, got %d", name, f.Results, len(results))
	}
	if i.context != nil {
		return i.context.CallInto(i.lease, i.call, i.engine, i.program, f.Entry, args, results, f.FrameBytes)
	}
	return i.call.CallIntoWithContext(i.engine, i.program, f.Entry, args, results, f.FrameBytes, 0)
}

// Close waits for invocation completion and retries any retained failed unmaps.
func (i *Instance) Close() error {
	if i == nil {
		return nil
	}
	i.mu.Lock()
	defer i.mu.Unlock()
	i.closed = true
	var memoryErr error
	if i.ownMemory {
		memoryErr = i.memory.Close()
	}
	return errors.Join(i.program.Close(), i.engine.Close(), i.call.Close(), i.context.Close(), i.lease.Close(), memoryErr)
}
