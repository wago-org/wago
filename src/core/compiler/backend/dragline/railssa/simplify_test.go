package railssa

import (
	"testing"
	"unsafe"

	"github.com/wago-org/wago/src/core/compiler/wasm"
	"github.com/wago-org/wago/tests/wasmtest"
)

func TestIntegerFactRemainsCompact(t *testing.T) {
	if got := unsafe.Sizeof(IntegerFact{}); got != 40 {
		t.Fatalf("IntegerFact size = %d, want 40", got)
	}
}

func buildSimplifyTest(t *testing.T, m *wasm.Module) (*StackFunc, *CFG, *ValueFlow, *SemanticFunc, *Metadata, *SimplifyResult) {
	t.Helper()
	f, cfg, flow, semantic := buildSemanticTest(t, m)
	metadata, err := BuildMetadata(f, nil)
	if err != nil {
		t.Fatal(err)
	}
	result, err := SparseSimplify(f, cfg, flow, semantic, metadata, DefaultSimplifyConfig(), nil)
	if err != nil {
		t.Fatal(err)
	}
	return f, cfg, flow, semantic, metadata, result
}

func TestSparseSimplifyConstantBranchAndDeadBlock(t *testing.T) {
	m := scalarModule(nil, []wasm.ValType{wasm.I32}, []byte{
		0x41, 0x00,
		0x04, 0x7f,
		0x41, 0x01,
		0x05,
		0x41, 0x02,
		0x0b,
		0x0b,
	})
	_, _, _, semantic, _, result := buildSimplifyTest(t, m)
	if len(result.factIndex) != 0 || len(result.Facts) != len(result.Aliases) {
		t.Fatalf("integer-heavy fact storage is not dense: facts=%d index=%d values=%d", len(result.Facts), len(result.factIndex), len(result.Aliases))
	}
	if result.Metrics.BranchesSimplified != 1 || result.Metrics.DeadBlocks == 0 {
		t.Fatalf("metrics = %#v", result.Metrics)
	}
	ifID := semantic.InstructionMap[1] - 1
	if result.Branches[ifID] != BranchFalse {
		t.Fatalf("branch decision = %d", result.Branches[ifID])
	}
	trueConst := semantic.InstructionMap[2] - 1
	falseConst := semantic.InstructionMap[4] - 1
	if result.LiveInsts[trueConst] || !result.LiveInsts[falseConst] {
		t.Fatalf("live instructions = %v", result.LiveInsts)
	}
}

func TestSparseSimplifyEliminatesRedundantSignExtension(t *testing.T) {
	m := scalarModule([]wasm.ValType{wasm.I32}, []wasm.ValType{wasm.I32}, []byte{
		0x20, 0x00,
		0xc0, // i32.extend8_s
		0xc0, // redundant i32.extend8_s
		0x0b,
	})
	_, _, _, semantic, _, result := buildSimplifyTest(t, m)
	first := semantic.Insts[semantic.InstructionMap[1]-1].Result
	second := semantic.Insts[semantic.InstructionMap[2]-1].Result
	if got := resolveAlias(result.Aliases, second); got != first {
		t.Fatalf("redundant sign extension aliases to v%d, want v%d", got, first)
	}
}

func TestSparseSimplifyEliminatesAndVerifiesTrivialBlockArguments(t *testing.T) {
	m := scalarModule([]wasm.ValType{wasm.I32}, []wasm.ValType{wasm.I32}, []byte{
		0x02, 0x40,
		0x03, 0x40,
		0x20, 0x00,
		0x21, 0x00,
		0x20, 0x00,
		0x0d, 0x01,
		0x0c, 0x00,
		0x0b,
		0x0b,
		0x20, 0x00,
		0x0b,
	})
	f, cfg, flow, semantic, metadata, result := buildSimplifyTest(t, m)
	if result.Metrics.TrivialArguments == 0 {
		t.Fatalf("trivial arguments were not eliminated: metrics=%#v aliases=%#v", result.Metrics, result.Aliases)
	}
	result.Metrics.TrivialArguments--
	if err := VerifySimplify(f, cfg, flow, semantic, metadata, result); err == nil {
		t.Fatal("incorrect trivial-argument metric was accepted")
	}
}

func TestSparseSimplifyDischargesConstantDivideObligations(t *testing.T) {
	m := scalarModule(nil, []wasm.ValType{wasm.I32}, []byte{
		0x41, 0x0a,
		0x41, 0x02,
		0x6d,
		0x0b,
	})
	_, _, _, semantic, _, result := buildSimplifyTest(t, m)
	divide := semantic.InstructionMap[2] - 1
	if result.Remaining[divide]&ObligationNonzeroDivisor != 0 || result.Metrics.ObligationsRemoved == 0 {
		t.Fatalf("remaining=%#x metrics=%#v", result.Remaining[divide], result.Metrics)
	}
	if fact := result.IntegerFactAt(semantic.Insts[divide].Result); !fact.Known || fact.Min != 5 {
		t.Fatalf("divide fact = %#v", fact)
	}
}

func TestSparseSimplifyCreatesBoundsCertificate(t *testing.T) {
	typeSec := wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType(nil, []wasm.ValType{wasm.I32})))
	funcSec := wasmtest.Section(3, wasmtest.Vec([]byte{0}))
	memorySec := wasmtest.Section(5, wasmtest.Vec([]byte{0x00, 0x01}))
	codeSec := wasmtest.Section(10, wasmtest.Vec(wasmtest.Code([]byte{0x41, 0x00, 0x28, 0x02, 0x00, 0x0b})))
	m, err := wasm.DecodeModule(wasmtest.Module(typeSec, funcSec, memorySec, codeSec))
	if err != nil {
		t.Fatal(err)
	}
	if err := wasm.ValidateModule(m); err != nil {
		t.Fatal(err)
	}
	_, _, flow, semantic, metadata, result := buildSimplifyTest(t, m)
	load := semantic.InstructionMap[1] - 1
	if len(result.Bounds) != 1 || result.Remaining[load]&ObligationMemoryBounds != 0 || result.Bounds[0].End != 4 {
		t.Fatalf("bounds=%#v remaining=%#x", result.Bounds, result.Remaining[load])
	}
	engine, err := NewProofEngine(flow, semantic, metadata, result)
	if err != nil {
		t.Fatal(err)
	}
	proof, err := engine.DemandProof(ProofRequest{Kind: ProofBounds, Value: result.Bounds[0].Address, Aux: load, Fuel: 8})
	if err != nil || !proof.Proven || proof.Certificate != 1 || proof.Dependencies != HeapLinearMemory {
		t.Fatalf("bounds proof=%#v err=%v", proof, err)
	}
}

func TestSparseSimplifyCreatesMaskedRangeBoundsCertificate(t *testing.T) {
	typeSec := wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType([]wasm.ValType{wasm.I32}, []wasm.ValType{wasm.I32})))
	funcSec := wasmtest.Section(3, wasmtest.Vec([]byte{0}))
	memorySec := wasmtest.Section(5, wasmtest.Vec([]byte{0x00, 0x01}))
	body := []byte{0x20, 0x00, 0x41}
	body = append(body, wasmtest.SLEB32(65528)...)
	body = append(body, 0x71, 0x28, 0x02, 0x00, 0x0b)
	codeSec := wasmtest.Section(10, wasmtest.Vec(wasmtest.Code(body)))
	m, err := wasm.DecodeModule(wasmtest.Module(typeSec, funcSec, memorySec, codeSec))
	if err != nil {
		t.Fatal(err)
	}
	if err := wasm.ValidateModule(m); err != nil {
		t.Fatal(err)
	}
	_, _, _, semantic, _, result := buildSimplifyTest(t, m)
	andID, loadID := semantic.InstructionMap[2]-1, semantic.InstructionMap[3]-1
	fact := result.IntegerFactAt(semantic.Insts[andID].Result)
	if fact.Known || !fact.RangeKnown || fact.Min != 0 || fact.Max != 65528 || fact.KnownZero&7 != 7 {
		t.Fatalf("masked range fact = %#v", fact)
	}
	if len(result.Bounds) != 1 || result.Bounds[0].Instruction != loadID || result.Bounds[0].End != 65532 || result.Remaining[loadID]&ObligationMemoryBounds != 0 {
		t.Fatalf("bounds=%#v remaining=%#x", result.Bounds, result.Remaining[loadID])
	}
}

func TestSparseSimplifyCreatesMaskedInductionBoundsCertificate(t *testing.T) {
	m := maskedInductionModule(t, 8, 65535)
	f, cfg, flow, semantic, metadata, result := buildSimplifyTest(t, m)
	loadID := uint32(len(semantic.Insts))
	for id, instruction := range semantic.Insts {
		if instruction.Op == wasm.InstrI32Load {
			loadID = uint32(id)
			break
		}
	}
	if int(loadID) == len(semantic.Insts) {
		t.Fatal("masked induction load is absent")
	}
	address := resolveAlias(result.Aliases, semantic.Operands(loadID)[0])
	fact := result.IntegerFactAt(address)
	if fact.Known || !fact.RangeKnown || fact.Min != 0 || fact.Max != 65528 || fact.KnownZero&7 != 7 {
		t.Fatalf("masked induction fact = %#v", fact)
	}
	if len(result.Bounds) != 1 || result.Bounds[0].Instruction != loadID || result.Bounds[0].End != 65532 || result.Remaining[loadID]&ObligationMemoryBounds != 0 {
		t.Fatalf("bounds=%#v remaining=%#x", result.Bounds, result.Remaining[loadID])
	}
	result.integerFactPtr(address).Max--
	if err := VerifySimplify(f, cfg, flow, semantic, metadata, result); err == nil {
		t.Fatal("tampered masked induction fact was accepted")
	}
}

func TestSparseSimplifyRetainsUnsafeMaskedInductionCheck(t *testing.T) {
	_, _, _, semantic, _, result := buildSimplifyTest(t, maskedInductionModule(t, 1, 65535))
	for id, instruction := range semantic.Insts {
		if instruction.Op == wasm.InstrI32Load && result.Remaining[id]&ObligationMemoryBounds == 0 {
			t.Fatalf("unsafe masked induction load %d lost its bounds obligation", id)
		}
	}
}

func TestSparseSimplifyGVNsEquivalentPureInstructionsWithinBlock(t *testing.T) {
	m := scalarModule([]wasm.ValType{wasm.I32}, []wasm.ValType{wasm.I32}, []byte{
		0x20, 0x00, 0x41, 0x07, 0x6a, 0x1a,
		0x20, 0x00, 0x41, 0x07, 0x6a,
		0x0b,
	})
	_, _, flow, semantic, _, result := buildSimplifyTest(t, m)
	adds := make([]FlowValueID, 0, 2)
	for _, instruction := range semantic.Insts {
		if instruction.Op == wasm.InstrI32Add {
			adds = append(adds, instruction.Result)
		}
	}
	if len(adds) != 2 || resolveAlias(result.Aliases, adds[1]) != resolveAlias(result.Aliases, adds[0]) || result.Metrics.Aliases < 2 {
		t.Fatalf("adds=%v aliases=%v metrics=%#v values=%d", adds, result.Aliases, result.Metrics, len(flow.Values))
	}
}

func TestSparseSimplifyGVNsBitIdenticalFloatConstants(t *testing.T) {
	m := scalarModule(nil, []wasm.ValType{wasm.F32}, []byte{
		0x43, 0x00, 0x00, 0x80, 0x3f, 0x1a,
		0x43, 0x00, 0x00, 0x80, 0x3f,
		0x0b,
	})
	_, _, flow, semantic, _, result := buildSimplifyTest(t, m)
	if len(result.factIndex) != len(flow.Values) || len(result.Facts) != 0 {
		t.Fatalf("float-heavy fact storage is not sparse: facts=%d index=%d values=%d", len(result.Facts), len(result.factIndex), len(flow.Values))
	}
	constants := make([]FlowValueID, 0, 2)
	for _, instruction := range semantic.Insts {
		if instruction.Op == wasm.InstrF32Const {
			constants = append(constants, instruction.Result)
		}
	}
	if len(constants) != 2 || resolveAlias(result.Aliases, constants[1]) != resolveAlias(result.Aliases, constants[0]) {
		t.Fatalf("constants=%v aliases=%v", constants, result.Aliases)
	}
}

func TestSparseSimplifyGVNsMemorySizeWithinUnchangedBlock(t *testing.T) {
	body := []byte{
		0x3f, 0x00, 0x1a,
		0x3f, 0x00,
		0x0b,
	}
	source := wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType(nil, []wasm.ValType{wasm.I32}))),
		wasmtest.Section(3, wasmtest.Vec([]byte{0})),
		wasmtest.Section(5, wasmtest.Vec([]byte{0x00, 0x01})),
		wasmtest.Section(10, wasmtest.Vec(wasmtest.Code(body))),
	)
	m, err := wasm.DecodeModule(source)
	if err != nil {
		t.Fatal(err)
	}
	if err := wasm.ValidateModule(m); err != nil {
		t.Fatal(err)
	}
	_, _, _, semantic, _, result := buildSimplifyTest(t, m)
	var sizes []FlowValueID
	for _, instruction := range semantic.Insts {
		if instruction.Op == wasm.InstrMemorySize {
			sizes = append(sizes, instruction.Result)
		}
	}
	if len(sizes) != 2 || resolveAlias(result.Aliases, sizes[1]) != sizes[0] {
		t.Fatalf("memory sizes=%v aliases=%v", sizes, result.Aliases)
	}
}

func TestSparseSimplifyDischargesExactIntegerFloatRoundTrip(t *testing.T) {
	m := scalarModule([]wasm.ValType{wasm.I32}, []wasm.ValType{wasm.I32}, []byte{
		0x20, 0x00,
		0x41, 0xff, 0x07,
		0x71, // i32.and
		0xb3, // f32.convert_i32_u
		0xa9, // i32.trunc_f32_u
		0x0b,
	})
	f, cfg, flow, semantic, metadata, result := buildSimplifyTest(t, m)
	trunc := semantic.InstructionMap[4] - 1
	if result.Remaining[trunc]&ObligationFiniteConversion != 0 || result.Remaining[trunc]&ObligationTrapOrder == 0 {
		t.Fatalf("trunc obligations = %#x", result.Remaining[trunc])
	}
	result.Remaining[trunc] |= ObligationFiniteConversion
	if err := VerifySimplify(f, cfg, flow, semantic, metadata, result); err == nil {
		t.Fatal("tampered conversion obligation was accepted")
	}
}

func TestSparseSimplifyElidesExtendWrapRoundTrip(t *testing.T) {
	m := scalarModule([]wasm.ValType{wasm.I32}, []wasm.ValType{wasm.I32}, []byte{
		0x20, 0x00,
		0xac, // i64.extend_i32_s
		0xa7, // i32.wrap_i64
		0x0b,
	})
	_, _, flow, semantic, _, result := buildSimplifyTest(t, m)
	wrap := semantic.Insts[semantic.InstructionMap[2]-1].Result
	if got := resolveAlias(result.Aliases, wrap); got == wrap || flow.Values[got].Type != wasm.I32 {
		t.Fatalf("wrap alias v%d -> v%d", wrap, got)
	}
}

func TestSparseSimplifyGVNsAcrossUniquePredecessor(t *testing.T) {
	m := scalarModule([]wasm.ValType{wasm.I32}, []wasm.ValType{wasm.I32}, []byte{
		0x20, 0x00, 0x41, 0x07, 0x6a, 0x1a,
		0x20, 0x00,
		0x04, 0x7f,
		0x20, 0x00, 0x41, 0x07, 0x6a,
		0x05,
		0x41, 0x00,
		0x0b,
		0x0b,
	})
	_, _, _, _, _, result := buildSimplifyTest(t, m)
	if result.Metrics.CrossBlockAliases == 0 {
		t.Fatalf("cross-block aliases = 0; aliases=%#v", result.Aliases)
	}
}

func maskedInductionModule(t *testing.T, step, mask int32) *wasm.Module {
	return maskedInductionModuleWithPrefix(t, step, mask, nil)
}

func maskedInductionModuleWithPrefix(t *testing.T, step, mask int32, prefix []byte) *wasm.Module {
	t.Helper()
	typeSec := wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType([]wasm.ValType{wasm.I32}, []wasm.ValType{wasm.I32})))
	funcSec := wasmtest.Section(3, wasmtest.Vec([]byte{0}))
	memorySec := wasmtest.Section(5, wasmtest.Vec([]byte{0x00, 0x01}))
	body := append([]byte(nil), prefix...)
	body = append(body,
		0x02, 0x40,
		0x03, 0x40,
		0x20, 0x01,
		0x28, 0x02, 0x00,
		0x1a,
		0x20, 0x01,
		0x41,
	)
	body = append(body, wasmtest.SLEB32(step)...)
	body = append(body, 0x6a, 0x41)
	body = append(body, wasmtest.SLEB32(mask)...)
	body = append(body,
		0x71,
		0x21, 0x01,
		0x20, 0x00,
		0x0d, 0x00,
		0x0b,
		0x0b,
		0x41, 0x00,
		0x0b,
	)
	function := append([]byte{0x01, 0x01, 0x7f}, body...)
	code := append(wasmtest.ULEB(uint32(len(function))), function...)
	m, err := wasm.DecodeModule(wasmtest.Module(typeSec, funcSec, memorySec, wasmtest.Section(10, wasmtest.Vec(code))))
	if err != nil {
		t.Fatal(err)
	}
	if err := wasm.ValidateModule(m); err != nil {
		t.Fatal(err)
	}
	return m
}

func TestDemandProofVerifiesFactsAndBounds(t *testing.T) {
	m := scalarModule(nil, []wasm.ValType{wasm.I32}, []byte{0x41, 0x07, 0x0b})
	_, _, flow, semantic, metadata, result := buildSimplifyTest(t, m)
	flowValue := semantic.Insts[0].Result
	engine, err := NewProofEngine(flow, semantic, metadata, result)
	if err != nil {
		t.Fatal(err)
	}
	proof, err := engine.DemandProof(ProofRequest{Kind: ProofNonZero, Value: flowValue, Fuel: 8})
	if err != nil {
		t.Fatal(err)
	}
	if !proof.Proven || proof.Steps == 0 {
		t.Fatalf("nonzero proof = %#v", proof)
	}
	zeroUpper, err := engine.DemandProof(ProofRequest{Kind: ProofUpper32Zero, Value: flowValue, Fuel: 8})
	if err != nil || !zeroUpper.Proven {
		t.Fatalf("upper-zero proof=%#v err=%v", zeroUpper, err)
	}
	if len(engine.cache) != 2 {
		t.Fatalf("proof cache entries = %d", len(engine.cache))
	}

}
