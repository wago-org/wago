//go:build wago_regalloccheck

package wasm

import (
	"reflect"
	"sync"
	"testing"

	"github.com/wago-org/wago/internal/regalloccheck"
	enc "github.com/wago-org/wago/src/core/encoder/amd64"
)

func sourceModule(t *testing.T, params, results []ValType, instructions ...Instruction) *Module {
	t.Helper()
	body, err := EncodeExpr(Expr{Instrs: instructions})
	if err != nil {
		t.Fatal(err)
	}
	return &Module{Types: []RecType{ft(params, results)}, FuncTypes: []TypeIdx{{}}, Code: []Func{{BodyBytes: body}}}
}
func sourceAnalysis(t *testing.T, m *Module, features ValidationFeatures) *ValidatedModuleAnalysis {
	t.Helper()
	a := new(ValidatedModuleAnalysis)
	if err := ValidateModuleWithAnalysis(m, features, 1, ValidationLimits{}, a); err != nil {
		t.Fatal(err)
	}
	if !a.ValidFor(m) {
		t.Fatal("missing complete source validation")
	}
	return a
}
func sourceComplete(t *testing.T, m *Module, a *ValidatedModuleAnalysis, features ValidationFeatures) *SourceLedger {
	t.Helper()
	l, r, err := BuildSourceLedger(m, a, 0, features, SourceLedgerLimits{})
	if err != nil || r.Coverage != SourceContractsComplete || r.Reason != regalloccheck.NoFailure || l == nil {
		t.Fatalf("ledger %v, report %+v, validation %v", l, r, err)
	}
	t.Cleanup(l.Close)
	if !l.ValidFor(m, 0, features) {
		t.Fatal("source context lost")
	}
	return l
}

func TestSourceLedgerLocalAliasesAndIndependentResults(t *testing.T) {
	m := sourceModule(t, []ValType{I32}, []ValType{I32},
		Instruction{Kind: InstrLocalGet, Index: 0}, Instruction{Kind: InstrLocalTee, Index: 1},
		Instruction{Kind: InstrLocalGet, Index: 1}, Instruction{Kind: InstrI32Add},
		Instruction{Kind: InstrLocalSet, Index: 0}, Instruction{Kind: InstrLocalGet, Index: 0})
	m.Code[0].Locals.Runs = []LocalRun{{Count: 1, Type: I32}}
	l := sourceComplete(t, m, sourceAnalysis(t, m, ValidationFeatures{}), ValidationFeatures{})
	param := l.EntryLocal(0)
	if l.Value(param).Kind != SourceParameter || l.Value(l.EntryLocal(1)).Kind != SourceConstant {
		t.Fatal("wrong entry assumptions")
	}
	for _, event := range []int{0, 1, 2} {
		if l.Output(event, 0) != param {
			t.Fatal("local get/tee invented a new identity")
		}
	}
	if l.Input(3, 0) != param || l.Input(3, 1) != param {
		t.Fatal("original add operands lost aliases")
	}
	result := l.Output(3, 0)
	if result == param || l.Value(result).Kind != SourceResult || l.Output(5, 0) != result || l.Input(6, 0) != result {
		t.Fatal("fresh arithmetic result/local version lost")
	}
	if !l.Event(6).FunctionEnd || !l.Event(6).Terminal {
		t.Fatal("missing return contract")
	}
}

func TestSourceLedgerLiteralTypeAndExactBits(t *testing.T) {
	bits := []uint32{0, 0x80000000, 0x7fc00001, 0x7fc00002, 0x7fc00001}
	var instrs []Instruction
	for _, bit := range bits {
		instrs = append(instrs, Instruction{Kind: InstrF32Const, F32Bits: bit}, Instruction{Kind: InstrDrop})
	}
	instrs = append(instrs, Instruction{Kind: InstrI32Const}, Instruction{Kind: InstrDrop})
	m := sourceModule(t, nil, nil, instrs...)
	l := sourceComplete(t, m, sourceAnalysis(t, m, ValidationFeatures{}), ValidationFeatures{})
	seen := map[SourceValueID]bool{}
	for i, bit := range bits {
		id := l.Output(i*2, 0)
		if v := l.Value(id); v.Type != F32 || v.Bits != uint64(bit) || v.Kind != SourceConstant {
			t.Fatalf("literal %d = %+v", i, v)
		}
		if i < 4 && seen[id] {
			t.Fatal("different bit patterns aliased")
		}
		seen[id] = true
	}
	if l.Output(8, 0) != l.Output(4, 0) {
		t.Fatal("identical NaN payload lacks canonical identity")
	}
	if l.Output(10, 0) == l.Output(0, 0) {
		t.Fatal("i32/f32 zeros aliased")
	}
}

func TestSourceLedgerOriginalCoordinates(t *testing.T) {
	m := sourceModule(t, nil, nil)
	// Valid noncanonical LEB encodings must preserve their actual source spans.
	m.Code[0].BodyBytes = []byte{0x41, 0x81, 0x00, 0x1a, 0x43, 0, 0, 0, 0, 0xfc, 0x80, 0x00, 0x1a, 0x0b}
	l := sourceComplete(t, m, sourceAnalysis(t, m, ValidationFeatures{}), ValidationFeatures{})
	want := [][2]int{{0, 3}, {3, 4}, {4, 9}, {9, 12}, {12, 13}, {13, 14}}
	for i, span := range want {
		e := l.Event(i)
		if e.PC != span[0] || e.EndPC != span[1] {
			t.Fatalf("event %d span [%d,%d), want%v", i, e.PC, e.EndPC, span)
		}
	}
	if l.Event(3).Kind != InstrI32TruncSatF32S || l.Input(3, 0) != l.Output(2, 0) || l.Value(l.Output(3, 0)).Type != I32 {
		t.Fatal("prefixed conversion contract wrong")
	}
}

func TestSourceLedgerPrimitiveSignatures(t *testing.T) {
	for kind, effect := range opEffects {
		if effect.cat == effNone {
			continue
		}
		kind := InstrKind(kind)
		t.Run(kind.String(), func(t *testing.T) {
			a, b := effect.a.valType(), effect.b.valType()
			var instrs []Instruction
			var results []ValType
			inputs := 0
			switch effect.cat {
			case effUnary, effTest, effConv:
				inputs = 1
				instrs = []Instruction{constFor(a), {Kind: kind}}
				results = []ValType{a}
				if effect.cat == effTest {
					results[0] = I32
				}
				if effect.cat == effConv {
					results[0] = b
				}
			case effBinary, effCompare:
				inputs = 2
				instrs = []Instruction{constFor(a), constFor(a), {Kind: kind}}
				results = []ValType{a}
				if effect.cat == effCompare {
					results[0] = I32
				}
			case effLoad:
				inputs = 1
				instrs = []Instruction{{Kind: InstrI32Const}, {Kind: kind, ext: &instrExt{MemArg: MemArg{Offset: 37, Align: uint32(effect.align)}}}}
				results = []ValType{a}
			case effStore:
				inputs = 2
				instrs = []Instruction{{Kind: InstrI32Const}, constFor(a), {Kind: kind, ext: &instrExt{MemArg: MemArg{Offset: 37, Align: uint32(effect.align)}}}}
			}
			m := sourceModule(t, nil, results, instrs...)
			if effect.cat == effLoad || effect.cat == effStore {
				m.Memories = []MemType{{Limits: Limits{Min: 1}}}
			}
			l := sourceComplete(t, m, sourceAnalysis(t, m, ValidationFeatures{}), ValidationFeatures{})
			index := len(instrs) - 1
			event := l.Event(index)
			if event.Kind != kind || event.InputCount != inputs || event.OutputCount != len(results) {
				t.Fatalf("contract %+v", event)
			}
			for i := 0; i < inputs; i++ {
				if l.Input(index, i) != l.Output(i, 0) {
					t.Fatal("operand order changed")
				}
			}
			if len(results) != 0 && l.Value(l.Output(index, 0)).Type != results[0] {
				t.Fatal("result type changed")
			}
			if (effect.cat == effLoad || effect.cat == effStore) && (event.MemoryOffset != 37 || event.MemoryAlign != uint32(effect.align)) {
				t.Fatal("memory immediate lost")
			}
		})
	}
}

func TestSourceLedgerDirectAndIndirectCallOrdering(t *testing.T) {
	for _, indirect := range []bool{false, true} {
		for _, table64 := range []bool{false, true} {
			if !indirect && table64 {
				continue
			}
			t.Run(map[bool]string{false: "direct", true: "indirect"}[indirect]+map[bool]string{false: "32", true: "64"}[table64], func(t *testing.T) {
				instrs := []Instruction{{Kind: InstrLocalGet, Index: 0}, {Kind: InstrLocalGet, Index: 1}}
				kind := InstrCall
				if indirect {
					kind = InstrCallIndirect
					selector := Instruction{Kind: InstrI32Const}
					if table64 {
						selector.Kind = InstrI64Const
					}
					instrs = append(instrs, selector)
				}
				instrs = append(instrs, Instruction{Kind: kind})
				m := sourceModule(t, []ValType{I32, F64}, []ValType{I64, F32}, instrs...)
				if indirect {
					m.Tables = []Table{{Type: TableType{Ref: AbsRef(HeapFunc), Limits: Limits{Min: 1, Addr64: table64}}}}
				} else {
					m.Imports = []Import{{Module: "env", Name: "f", Type: NewFuncExternType(TypeIdx{})}}
				}
				l := sourceComplete(t, m, sourceAnalysis(t, m, ValidationFeatures{}), ValidationFeatures{})
				index := len(instrs) - 1
				e := l.Event(index)
				wantInputs := 2
				if indirect {
					wantInputs++
				}
				if e.InputCount != wantInputs || e.OutputCount != 2 || l.Input(index, 0) != l.EntryLocal(0) || l.Input(index, 1) != l.EntryLocal(1) {
					t.Fatalf("call contract %+v", e)
				}
				if indirect {
					want := I32
					if table64 {
						want = I64
					}
					if l.Value(l.Input(index, 2)).Type != want {
						t.Fatal("wrong indirect selector type")
					}
				}
				if l.Value(l.Output(index, 0)).Type != I64 || l.Value(l.Output(index, 1)).Type != F32 || l.Output(index, 0) == l.Output(index, 1) {
					t.Fatal("declared result order/identities lost")
				}
			})
		}
	}
}

func TestSourceLedgerTypedSelectAndExplicitReturn(t *testing.T) {
	m := sourceModule(t, []ValType{I32, I32, I32}, []ValType{I32}, Instruction{Kind: InstrLocalGet, Index: 0}, Instruction{Kind: InstrLocalGet, Index: 1}, Instruction{Kind: InstrLocalGet, Index: 2}, Instruction{Kind: InstrSelect, ext: &instrExt{ValTypes: []ValType{I32}}}, Instruction{Kind: InstrReturn})
	l := sourceComplete(t, m, sourceAnalysis(t, m, ValidationFeatures{}), ValidationFeatures{})
	for i := 0; i < 3; i++ {
		if l.Input(3, i) != l.EntryLocal(i) {
			t.Fatal("select order lost")
		}
	}
	if l.Output(3, 0) == l.EntryLocal(0) || l.Output(3, 0) == l.EntryLocal(1) || l.Input(4, 0) != l.Output(3, 0) || !l.Event(4).Terminal || l.Event(5).InputCount != 0 {
		t.Fatal("select/return contract lost")
	}
}

func TestSourceLedgerLimitsDropAllPartialState(t *testing.T) {
	m := sourceModule(t, []ValType{I32}, []ValType{I32}, Instruction{Kind: InstrLocalGet, Index: 0}, Instruction{Kind: InstrI32Const, I32: 1}, Instruction{Kind: InstrI32Add})
	a := sourceAnalysis(t, m, ValidationFeatures{})
	cases := []SourceLedgerLimits{{BodyBytes: 1}, {Metadata: 1}, {Locals: 1, Values: 1}, {Events: 1}, {Values: 1}, {Operands: 1}, {Stack: 1}, {Work: 1}}
	for _, limits := range cases {
		l, r, err := BuildSourceLedger(m, a, 0, ValidationFeatures{}, limits)
		if l != nil || err != nil || r.Coverage != SourceIncomplete || r.Reason != regalloccheck.ResourceLimit {
			t.Fatalf("limits%+v: ledger%v result%+v err%v", limits, l, r, err)
		}
	}
	if l, r, err := BuildSourceLedger(m, a, 0, ValidationFeatures{}, SourceLedgerLimits{Work: -1}); l != nil || err != nil || r.Coverage != SourceInvalid {
		t.Fatalf("negative limits: %+v %v", r, err)
	}
}

func TestSourceLedgerIncompleteControlAndProposals(t *testing.T) {
	for _, body := range [][]byte{{0x02, 0x40, 0x0b, 0x0b}, {0x03, 0x40, 0x0b, 0x0b}, {0x00, 0x0b}, {0x0f, 0x01, 0x0b}, {0x12, 0x00, 0x0b}} {
		m := sourceModule(t, nil, nil)
		m.Code[0].BodyBytes = body
		a := sourceAnalysis(t, m, ValidationFeatures{})
		l, r, err := BuildSourceLedger(m, a, 0, ValidationFeatures{}, SourceLedgerLimits{})
		if l != nil || err != nil || r.Coverage != SourceIncomplete || r.Reason != regalloccheck.UnsupportedOperation {
			t.Fatalf("body%x: %v %+v %v", body, l, r, err)
		}
	}
}

func TestSourceLedgerContextCleanupAndConcurrency(t *testing.T) {
	m := sourceModule(t, nil, nil, Instruction{Kind: InstrNop})
	a := sourceAnalysis(t, m, ValidationFeatures{})
	for _, other := range []*Module{nil, {}} {
		l, r, err := BuildSourceLedger(other, a, 0, ValidationFeatures{}, SourceLedgerLimits{})
		if l != nil || err != nil || r.Coverage != SourceInvalid {
			t.Fatalf("wrong context %+v %v", r, err)
		}
	}
	for _, index := range []int{-1, 1} {
		l, r, err := BuildSourceLedger(m, a, index, ValidationFeatures{}, SourceLedgerLimits{})
		if l != nil || err != nil || r.Coverage != SourceInvalid {
			t.Fatalf("wrong index %+v %v", r, err)
		}
	}
	l := sourceComplete(t, m, a, ValidationFeatures{})
	if l.ValidFor(m, 0, ValidationFeatures{MultiMemory: true}) {
		t.Fatal("feature identity ignored")
	}
	l.Close()
	if l.ValidFor(m, 0, ValidationFeatures{}) || l.ValidFor(nil, 0, ValidationFeatures{}) || !reflect.DeepEqual(*l, SourceLedger{}) {
		t.Fatal("closed ledger retained source storage")
	}
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			l, r, err := BuildSourceLedger(m, a, 0, ValidationFeatures{}, SourceLedgerLimits{})
			if err != nil || r.Coverage != SourceContractsComplete {
				t.Errorf("concurrent ledger %+v %v", r, err)
			}
			if l != nil {
				l.Close()
			}
		}()
	}
	wg.Wait()
}

func TestSourceLedgerFeatureMismatchAndInvalidBytes(t *testing.T) {
	m := sourceModule(t, nil, nil, Instruction{Kind: InstrNop})
	m.Memories = []MemType{{}, {}}
	a := sourceAnalysis(t, m, ValidationFeatures{MultiMemory: true})
	if l, r, err := BuildSourceLedger(m, a, 0, ValidationFeatures{}, SourceLedgerLimits{}); l != nil || err != nil || r.Coverage != SourceInvalid {
		t.Fatalf("feature mismatch %+v %v", r, err)
	}
	// Deliberately violate the immutable-module API precondition to check that
	// revalidation errors clear all partial source contracts.
	m.Code[0].BodyBytes = []byte{0x41, 0x0b}
	if l, r, err := BuildSourceLedger(m, a, 0, ValidationFeatures{MultiMemory: true}, SourceLedgerLimits{}); l != nil || err == nil || r.Coverage != SourceInvalid || r.Reason != regalloccheck.InvalidGraph {
		t.Fatalf("invalid body %+v %v", r, err)
	}
}

func TestSourceLedgerRejectsUnsupportedVectorsBeforeDecode(t *testing.T) {
	m := sourceModule(t, nil, nil)
	// Ordinary valid control, with a label vector. All source metadata credit
	// is consumed by the module shape; decoding that vector would fail the
	// zero remaining decode budget instead of reporting unsupported admission.
	m.Code[0].BodyBytes = []byte{0x41, 0, 0x0e, 3, 0, 0, 0, 0, 0x0b}
	a := sourceAnalysis(t, m, ValidationFeatures{})
	l, r, err := BuildSourceLedger(m, a, 0, ValidationFeatures{}, SourceLedgerLimits{Metadata: 4})
	if l != nil || err != nil || r.Coverage != SourceIncomplete || r.Reason != regalloccheck.UnsupportedOperation || r.PC != 2 {
		t.Fatalf("unsupported vector admission: %+v %v", r, err)
	}
	m = sourceModule(t, []ValType{I32, I32, I32}, []ValType{I32},
		Instruction{Kind: InstrLocalGet, Index: 0}, Instruction{Kind: InstrLocalGet, Index: 1},
		Instruction{Kind: InstrLocalGet, Index: 2}, Instruction{Kind: InstrSelect, ext: &instrExt{ValTypes: []ValType{I32}}})
	a = sourceAnalysis(t, m, ValidationFeatures{})
	l, r, err = BuildSourceLedger(m, a, 0, ValidationFeatures{}, SourceLedgerLimits{Metadata: 8})
	if l != nil || err != nil || r.Coverage != SourceIncomplete || r.Reason != regalloccheck.ResourceLimit || r.PC != 6 {
		t.Fatalf("typed select preallocation limit: %+v %v", r, err)
	}
}

// This composes original source IDs with real encoder transfer observations.
// It is a constructor/physical-contract control, not compiler integration or
// evidence for ABI mapping. The declared test ABI is two fixed frame homes.
func TestSourceLedgerContractsWithObservedTransport(t *testing.T) {
	m := sourceModule(t, []ValType{I32, I32}, []ValType{I32},
		Instruction{Kind: InstrLocalGet, Index: 0}, Instruction{Kind: InstrLocalGet, Index: 1}, Instruction{Kind: InstrI32Add})
	l := sourceComplete(t, m, sourceAnalysis(t, m, ValidationFeatures{}), ValidationFeatures{})
	for _, wrongSource := range []bool{false, true} {
		g := regalloccheck.Graph{Blocks: []regalloccheck.Block{{}}}
		for i := 1; i <= l.ValueCount(); i++ {
			g.Widths = append(g.Widths, 4)
		}
		g.Inputs = []regalloccheck.Binding{{Location: regalloccheck.Slot(0), Value: regalloccheck.ValueID(l.EntryLocal(0))},
			{Location: regalloccheck.Slot(8), Value: regalloccheck.ValueID(l.EntryLocal(1))}}
		appendOp := func(op regalloccheck.Operation) { g.Blocks[0].Operations = append(g.Blocks[0].Operations, op) }
		var a enc.Asm
		a.ObserveRegalloc(func(e regalloccheck.Effect) {
			appendOp(regalloccheck.Operation{Kind: regalloccheck.Machine, Effect: e})
		})
		displacement := int32(0)
		if wrongSource {
			displacement = 8
		}
		a.Load32(enc.RAX, enc.RSP, displacement)
		a.Load32(enc.RCX, enc.RSP, 8)
		for i, reg := range []enc.Reg{enc.RAX, enc.RCX} {
			appendOp(regalloccheck.Operation{Kind: regalloccheck.Use, Location: regalloccheck.Register(regalloccheck.GP, uint8(reg)), Value: regalloccheck.ValueID(l.Input(2, i))})
		}
		writes := uint32(0)
		a.ObserveGPWrites(func(mask uint32) { writes |= mask })
		a.Add32(enc.RAX, enc.RCX)
		if writes&(1<<enc.RAX) == 0 {
			t.Fatal("missing actual result write")
		}
		appendOp(regalloccheck.Operation{Kind: regalloccheck.Define, Location: regalloccheck.Register(regalloccheck.GP, uint8(enc.RAX)), Value: regalloccheck.ValueID(l.Output(2, 0))})
		appendOp(regalloccheck.Operation{Kind: regalloccheck.Use, Location: regalloccheck.Register(regalloccheck.GP, uint8(enc.RAX)), Value: regalloccheck.ValueID(l.Input(3, 0))})
		r := g.Verify(regalloccheck.Limits{})
		want := regalloccheck.Verified
		if wrongSource {
			want = regalloccheck.Rejected
		}
		if r.Verdict != want {
			t.Fatalf("wrong source %v: %+v", wrongSource, r)
		}
	}
}
