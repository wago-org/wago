//go:build wago_regalloccheck

package wasm

import (
	"encoding/binary"
	"testing"

	"github.com/wago-org/wago/internal/regalloccheck"
)

func sourceVector(low, high uint64) Instruction {
	b := make([]byte, 16)
	binary.LittleEndian.PutUint64(b[:8], low)
	binary.LittleEndian.PutUint64(b[8:], high)
	var lanes [16]LaneIdx
	for i := range b {
		lanes[i] = LaneIdx(b[i])
	}
	return Instruction{Kind: InstrV128Const, ext: &instrExt{Lanes: lanes}}
}

// EncodeExpr intentionally supports only the ordinary scalar test vocabulary.
// This fixture serializes FD using the decoder's opcode tables, then the source
// ledger must independently decode and validate those real bytes.
func sourceSIMDModule(t *testing.T, params, results []ValType, instructions ...Instruction) *Module {
	t.Helper()
	m := sourceModule(t, params, results)
	var body []byte
	for _, in := range instructions {
		if !IsSIMDValidationInstructionKind(in.Kind) {
			bytes, err := EncodeExpr(Expr{Instrs: []Instruction{in}})
			if err != nil {
				t.Fatal(err)
			}
			body = append(body, bytes[:len(bytes)-1]...)
			continue
		}
		sub, found := uint32(0), false
		for _, table := range [][]InstrKind{fdMem[:], fdLane[:], fdNoImm[:]} {
			for n, k := range table {
				if k == in.Kind {
					sub, found = uint32(n), true
				}
			}
		}
		if in.Kind == InstrV128Const {
			sub, found = 12, true
		}
		if in.Kind == InstrI8x16Shuffle {
			sub, found = 13, true
		}
		if !found {
			t.Fatal("missing authoritative SIMD opcode", in.Kind)
		}
		body = append(body, 0xfd)
		appendU32(&body, sub)
		if sub == 12 || sub == 13 {
			for _, lane := range in.Lanes() {
				body = append(body, byte(lane))
			}
			continue
		}
		if sourceMemoryInstruction(in.Kind) {
			arg := in.MemArg()
			align := arg.Align
			if arg.Mem != nil {
				align |= 0x40
			}
			appendU32(&body, align)
			if arg.Mem != nil {
				appendU32(&body, uint32(*arg.Mem))
			}
			for n := arg.Offset; ; {
				part := byte(n & 127)
				n >>= 7
				if n != 0 {
					part |= 128
				}
				body = append(body, part)
				if n == 0 {
					break
				}
			}
		}
		if simdEffects[in.Kind].laneLimit != 0 {
			body = append(body, byte(in.Lane))
		}
	}
	m.Code[0].BodyBytes = append(body, 0x0b)
	return m
}

func TestSourceSIMDFullLiteralIdentityAndLocalAliases(t *testing.T) {
	m := sourceSIMDModule(t, []ValType{V128}, []ValType{V128},
		Instruction{Kind: InstrLocalGet, Index: 0}, Instruction{Kind: InstrLocalTee, Index: 1},
		Instruction{Kind: InstrDrop}, Instruction{Kind: InstrLocalGet, Index: 1})
	m.Code[0].Locals.Runs = []LocalRun{{Count: 2, Type: V128}}
	l := sourceComplete(t, m, sourceAnalysis(t, m, ValidationFeatures{}), ValidationFeatures{})
	if l.Output(0, 0) != l.EntryLocal(0) || l.Output(1, 0) != l.EntryLocal(0) || l.Output(3, 0) != l.EntryLocal(0) {
		t.Fatal("vector aliases invented a definition")
	}
	z := l.Value(l.EntryLocal(1))
	if z.Type != V128 || z.Kind != SourceConstant || z.Bits != 0 || z.BitsHigh != 0 || l.EntryLocal(1) != l.EntryLocal(2) {
		t.Fatal("vector local zero is not exact canonical full128 zero", z)
	}
	m = sourceSIMDModule(t, nil, nil, sourceVector(7, 11), Instruction{Kind: InstrDrop},
		sourceVector(7, 12), Instruction{Kind: InstrDrop}, sourceVector(7, 11), Instruction{Kind: InstrDrop},
		Instruction{Kind: InstrI64Const, I64: 7}, Instruction{Kind: InstrDrop},
		sourceVector(8, 11), Instruction{Kind: InstrDrop},
		sourceVector(0x0706050403020100, 0x0f0e0d0c0b0a0908), Instruction{Kind: InstrDrop},
		sourceVector(0, 0), Instruction{Kind: InstrDrop})
	m.Code[0].Locals.Runs = []LocalRun{{Count: 1, Type: V128}}
	l = sourceComplete(t, m, sourceAnalysis(t, m, ValidationFeatures{}), ValidationFeatures{})
	a, b, again, scalar := l.Output(0, 0), l.Output(2, 0), l.Output(4, 0), l.Output(6, 0)
	if a == b || a != again || a == scalar || l.Value(a).BitsHigh != 11 || l.Value(b).BitsHigh != 12 || l.Value(scalar).BitsHigh != 0 {
		t.Fatal("high vector bits or primitive type lost their identity")
	}
	if a == l.Output(8, 0) || l.Value(l.Output(10, 0)).Bits != 0x0706050403020100 || l.Value(l.Output(10, 0)).BitsHigh != 0x0f0e0d0c0b0a0908 || l.EntryLocal(0) != l.Output(12, 0) {
		t.Fatal("full vector bytes or explicit/default zero identity lost")
	}
}

func TestSourceSIMDAuthoritativeSignatureInventory(t *testing.T) {
	count := 0
	for kind, effect := range simdEffects {
		if effect.cat == simdNone {
			continue
		}
		count++
		k := InstrKind(kind)
		t.Run(k.String(), func(t *testing.T) {
			var params, results []ValType
			results = []ValType{V128}
			switch effect.cat {
			case simdEffLoad:
				params = []ValType{I32}
			case simdEffStore, simdEffMemStoreLane:
				params, results = []ValType{I32, V128}, nil
			case simdEffMemLoadLane:
				params = []ValType{I32, V128}
			case simdEffSplat:
				params = []ValType{effect.scalar.valType()}
			case simdEffExtract:
				params, results = []ValType{V128}, []ValType{effect.scalar.valType()}
			case simdEffReplace:
				params = []ValType{V128, effect.scalar.valType()}
			case simdEffShift:
				params = []ValType{V128, I32}
			case simdEffUnary:
				params = []ValType{V128}
			case simdEffBinary:
				params = []ValType{V128, V128}
			case simdEffTernary, simdBitselect:
				params = []ValType{V128, V128, V128}
			case simdPopV128PushI32:
				params, results = []ValType{V128}, []ValType{I32}
			case simdConst:
			default:
				t.Fatal("new SIMD category requires source contract", effect.cat)
			}
			var instructions []Instruction
			for i := range params {
				instructions = append(instructions, Instruction{Kind: InstrLocalGet, Index: uint32(i)})
			}
			in := Instruction{Kind: k}
			if effect.laneLimit != 0 {
				in.Lane = effect.laneLimit - 1
			}
			memory := effect.cat == simdEffLoad || effect.cat == simdEffStore || effect.cat == simdEffMemLoadLane || effect.cat == simdEffMemStoreLane
			if memory {
				in.ext = &instrExt{MemArg: MemArg{Offset: 37, Align: uint32(effect.align)}}
			}
			if k == InstrV128Const {
				in = sourceVector(0x1234, 0x9876)
			}
			instructions = append(instructions, in)
			m := sourceSIMDModule(t, params, results, instructions...)
			if memory {
				m.Memories = []MemType{{Limits: Limits{Min: 1}}}
			}
			l := sourceComplete(t, m, sourceAnalysis(t, m, ValidationFeatures{}), ValidationFeatures{})
			e := l.Event(len(params))
			if e.Kind != k || e.InputCount != len(params) || e.OutputCount != len(results) || e.Lane != in.Lane {
				t.Fatal("wrong original SIMD signature", e)
			}
			for i := range params {
				if l.Input(len(params), i) != l.EntryLocal(i) {
					t.Fatal("original SIMD operand order lost", i)
				}
			}
			if memory && (e.MemoryOffset != 37 || e.MemoryAlign != uint32(effect.align)) {
				t.Fatal("SIMD memory coordinates lost", e)
			}
			for i, typ := range results {
				v := l.Value(l.Output(len(params), i))
				if v.Type != typ || k != InstrV128Const && v.Kind != SourceResult {
					t.Fatal("incorrect SIMD result identity/type", v)
				}
			}
		})
	}
	if count != len(SIMDValidationInstructionKinds()) {
		t.Fatal("SIMD source inventory drift", count)
	}
}

func TestSourceSIMDShuffleCopyAndOriginalCoordinates(t *testing.T) {
	lanes := [16]LaneIdx{31, 0, 17, 2, 19, 4, 21, 6, 23, 8, 25, 10, 27, 12, 29, 14}
	m := sourceSIMDModule(t, []ValType{V128, V128}, []ValType{V128},
		Instruction{Kind: InstrLocalGet, Index: 0}, Instruction{Kind: InstrLocalGet, Index: 1},
		Instruction{Kind: InstrI8x16Shuffle, ext: &instrExt{Lanes: lanes}},
		sourceVector(0xffeeddccbbaa9988, 0x7766554433221100), Instruction{Kind: InstrDrop})
	l := sourceComplete(t, m, sourceAnalysis(t, m, ValidationFeatures{}), ValidationFeatures{})
	e := l.Event(2)
	if e.Lanes != lanes || e.EndPC-e.PC != 18 {
		t.Fatal("shuffle coordinates or copied immediate lost", e)
	}
	e.Lanes[0] = 0
	if l.Event(2).Lanes != lanes {
		t.Fatal("returned shuffle record aliases the ledger")
	}
	m = sourceSIMDModule(t, []ValType{V128}, []ValType{V128})
	// FD subopcode 77 (v128.not) in a valid noncanonical two-byte LEB.
	m.Code[0].BodyBytes = []byte{0x20, 0, 0xfd, 0xcd, 0, 0x0b}
	l = sourceComplete(t, m, sourceAnalysis(t, m, ValidationFeatures{}), ValidationFeatures{})
	if e := l.Event(1); e.Kind != InstrV128Not || e.PC != 2 || e.EndPC != 5 {
		t.Fatal("noncanonical SIMD source span lost", e)
	}
}

func TestSourceSIMDLimitsAndClose(t *testing.T) {
	m := sourceSIMDModule(t, nil, []ValType{V128}, sourceVector(1, 2))
	a := sourceAnalysis(t, m, ValidationFeatures{})
	for _, limits := range []SourceLedgerLimits{{BodyBytes: 17}, {Values: 1}, {Work: 16}, {Operands: 1}, {Events: 1}} {
		l, r, err := BuildSourceLedger(m, a, 0, ValidationFeatures{}, limits)
		if l != nil || err != nil || r.Coverage != SourceIncomplete || r.Reason != regalloccheck.ResourceLimit {
			t.Fatal("SIMD quota did not fail closed", limits, r, err)
		}
	}
	m = sourceSIMDModule(t, []ValType{V128, V128}, []ValType{V128},
		Instruction{Kind: InstrLocalGet}, Instruction{Kind: InstrLocalGet, Index: 1}, Instruction{Kind: InstrV128And})
	a = sourceAnalysis(t, m, ValidationFeatures{})
	if l, r, err := BuildSourceLedger(m, a, 0, ValidationFeatures{}, SourceLedgerLimits{Stack: 1}); l != nil || err != nil || r.Reason != regalloccheck.ResourceLimit {
		t.Fatal("vector operand stack quota escaped", r, err)
	}
	l := sourceComplete(t, m, a, ValidationFeatures{})
	l.Close()
	if l.ValueCount() != 0 || l.EventCount() != 0 || l.ValidFor(m, 0, ValidationFeatures{}) {
		t.Fatal("closed SIMD source ledger retained facts")
	}
}

func TestSourceSIMDMemory64AndDeadImmediate(t *testing.T) {
	for _, kind := range []InstrKind{InstrV128Load, InstrV128Store, InstrV128Load8Lane, InstrV128Store8Lane} {
		t.Run(kind.String(), func(t *testing.T) {
			mem := MemIdx(1)
			in := Instruction{Kind: kind, Lane: 15, ext: &instrExt{MemArg: MemArg{Mem: &mem, Offset: 1<<40 + 37}}}
			if simdEffects[kind].laneLimit == 0 {
				in.Lane = 0
			}
			params, results := []ValType{I64}, []ValType{V128}
			if kind != InstrV128Load {
				params = append(params, V128)
			}
			if kind == InstrV128Store || kind == InstrV128Store8Lane {
				results = nil
			}
			var instructions []Instruction
			for i := range params {
				instructions = append(instructions, Instruction{Kind: InstrLocalGet, Index: uint32(i)})
			}
			instructions = append(instructions, in)
			m := sourceSIMDModule(t, params, results, instructions...)
			m.Memories = []MemType{{Limits: Limits{Min: 1}}, {Limits: Limits{Min: 1, Addr64: true}}}
			features := ValidationFeatures{MultiMemory: true}
			l := sourceComplete(t, m, sourceAnalysis(t, m, features), features)
			e := l.Event(len(params))
			if e.Index != 1 || e.MemoryOffset != 1<<40+37 || e.Lane != in.Lane || l.Value(l.Input(len(params), 0)).Type != I64 {
				t.Fatal("mixed-memory64 address/immediate lost", e)
			}
			// Dead instructions are still decoded and validated, and retain their
			// immediates without inventing unknown operand identities.
			dead := []Instruction{{Kind: InstrUnreachable}, in}
			if len(results) != 0 {
				dead = append(dead, Instruction{Kind: InstrDrop})
			}
			m = sourceSIMDModule(t, nil, nil, dead...)
			m.Memories = []MemType{{Limits: Limits{Min: 1}}, {Limits: Limits{Min: 1, Addr64: true}}}
			l = sourceComplete(t, m, sourceAnalysis(t, m, features), features)
			e = l.Event(1)
			if !e.Unreachable || e.InputCount != 0 || e.Index != 1 || e.MemoryOffset != 1<<40+37 {
				t.Fatal("dead SIMD immediate/operand policy lost", e)
			}
		})
	}
}

func TestSourceSIMDRejectsMalformedOriginalBytes(t *testing.T) {
	m := sourceSIMDModule(t, []ValType{V128}, []ValType{I32},
		Instruction{Kind: InstrLocalGet}, Instruction{Kind: InstrI8x16ExtractLaneU, Lane: 15})
	a := sourceAnalysis(t, m, ValidationFeatures{})
	valid := append([]byte(nil), m.Code[0].BodyBytes...)
	for name, body := range map[string][]byte{
		"first-invalid-lane": {0x20, 0, 0xfd, 0x16, 16, 0x0b},
		"truncated-lane":     {0x20, 0, 0xfd, 0x16},
		"unknown-subopcode":  {0xfd, 0xff, 0x7f, 0x0b},
		"truncated-prefix":   {0xfd},
		"scalar-type":        {0x41, 0, 0xfd, 0x16, 0, 0x0b},
	} {
		t.Run(name, func(t *testing.T) {
			// Break the immutable-module precondition only to exercise the
			// authoritative revalidation failure, never native execution.
			m.Code[0].BodyBytes = body
			l, r, err := BuildSourceLedger(m, a, 0, ValidationFeatures{}, SourceLedgerLimits{})
			if l != nil || err == nil || r.Coverage != SourceInvalid {
				t.Fatal("malformed SIMD published a partial ledger", r, err)
			}
		})
	}
	m.Code[0].BodyBytes = valid
	sourceComplete(t, m, a, ValidationFeatures{})
	shuffle := sourceSIMDModule(t, []ValType{V128, V128}, []ValType{V128},
		Instruction{Kind: InstrLocalGet}, Instruction{Kind: InstrLocalGet, Index: 1},
		Instruction{Kind: InstrI8x16Shuffle})
	a = sourceAnalysis(t, shuffle, ValidationFeatures{})
	shuffle.Code[0].BodyBytes[6] = 32
	if l, r, err := BuildSourceLedger(shuffle, a, 0, ValidationFeatures{}, SourceLedgerLimits{}); l != nil || err == nil || r.Coverage != SourceInvalid {
		t.Fatal("invalid shuffle lane admitted", r, err)
	}
	shuffle.Code[0].BodyBytes = shuffle.Code[0].BodyBytes[:20]
	if l, r, err := BuildSourceLedger(shuffle, a, 0, ValidationFeatures{}, SourceLedgerLimits{}); l != nil || err == nil || r.Coverage != SourceInvalid {
		t.Fatal("truncated shuffle payload admitted", r, err)
	}
}

func TestSourceSIMDTypedLoopAndOrdinarySignatures(t *testing.T) {
	m := sourceModule(t, []ValType{V128, I32}, []ValType{V128},
		Instruction{Kind: InstrLocalGet}, Instruction{Kind: InstrLoop, ext: &instrExt{
			BlockType: BlockType{Kind: BlockTypeIndex, Type: TypeIdx{Index: 1}},
			Body:      Expr{Instrs: []Instruction{{Kind: InstrLocalGet, Index: 1}, {Kind: InstrBrIf}}},
		}})
	m.Types = append(m.Types, ft([]ValType{V128}, []ValType{V128}))
	l := sourceComplete(t, m, sourceAnalysis(t, m, ValidationFeatures{}), ValidationFeatures{})
	checkSourceCFG(t, l)
	backedges := 0
	for i := 0; i < l.EdgeCount(); i++ {
		e := l.Edge(i)
		if e.Arm == SourceBranchIfTrue {
			backedges++
			arg := l.EdgeArgument(i, e.ArgumentCount-1)
			param := l.BlockParameter(e.To, l.Block(e.To).LocalCount)
			if arg != param || l.Value(arg).Type != V128 {
				t.Fatal("vector backedge lost simultaneous stack parameter")
			}
		}
	}
	if backedges != 1 {
		t.Fatal("missing vector loop backedge")
	}
	m = sourceModule(t, []ValType{V128, V128, I32}, []ValType{V128},
		Instruction{Kind: InstrLocalGet}, Instruction{Kind: InstrLocalGet, Index: 1},
		Instruction{Kind: InstrLocalGet, Index: 2}, Instruction{Kind: InstrSelect, ext: &instrExt{ValTypes: []ValType{V128}}})
	l = sourceComplete(t, m, sourceAnalysis(t, m, ValidationFeatures{}), ValidationFeatures{})
	if l.Input(3, 0) != l.EntryLocal(0) || l.Input(3, 1) != l.EntryLocal(1) || l.Value(l.Output(3, 0)).Type != V128 {
		t.Fatal("vector select source contract lost")
	}
	for _, indirect := range []bool{false, true} {
		instructions := []Instruction{{Kind: InstrLocalGet}}
		call := Instruction{Kind: InstrCall}
		if indirect {
			instructions = append(instructions, Instruction{Kind: InstrI32Const})
			call.Kind = InstrCallIndirect
		}
		instructions = append(instructions, call)
		m = sourceModule(t, []ValType{V128}, []ValType{V128}, instructions...)
		if indirect {
			m.Tables = []Table{{Type: TableType{Ref: AbsRef(HeapFunc), Limits: Limits{Min: 1}}}}
		} else {
			m.Imports = []Import{{Module: "env", Name: "f", Type: NewFuncExternType(TypeIdx{})}}
		}
		l = sourceComplete(t, m, sourceAnalysis(t, m, ValidationFeatures{}), ValidationFeatures{})
		event := len(instructions) - 1
		if l.Input(event, 0) != l.EntryLocal(0) || l.Value(l.Output(event, 0)).Type != V128 {
			t.Fatal("ordinary vector call source signature lost")
		}
	}
}
