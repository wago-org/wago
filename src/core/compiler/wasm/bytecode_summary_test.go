package wasm

import (
	"encoding/hex"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"testing"

	"github.com/wago-org/wago/tests/support/summaryfixtures"
)

type bytecodeSummaryCase struct {
	name, code string
	kind       InstrKind
	tail       bool
}

// Each byte count and kind below is specified directly. The shared decoder is
// a second comparison, not the sole oracle. Structured tails contain one end.
var bytecodeSummaryCases = []bytecodeSummaryCase{
	{"i32_signed", "418080808078", InstrI32Const, false},
	{"i64_signed", "428080808080808080807f", InstrI64Const, false},
	{"f32", "430000a03f", InstrF32Const, false},
	{"f64", "4400000000000004c0", InstrF64Const, false},
	{"local_index", "208001", InstrLocalGet, false},
	{"global_index", "238001", InstrGlobalGet, false},
	{"call", "108001", InstrCall, false},
	{"call_indirect", "11800101", InstrCallIndirect, false},
	{"tail_indirect", "13800101", InstrReturnCallIndirect, false},
	{"call_ref", "148001", InstrCallRef, false},
	{"tail_ref", "158001", InstrReturnCallRef, false},
	{"block_void", "0240", InstrBlock, true},
	{"loop_value", "037f", InstrLoop, true},
	{"if_type_index", "048001", InstrIf, true},
	{"block_ref", "026370", InstrBlock, true},
	{"br_table", "0e030080010200", InstrBrTable, false},
	{"typed_select", "1c016370", InstrSelect, false},
	{"memory32", "28028001", InstrI32Load, false},
	{"indexed_memory32", "2842008001", InstrI32Load, false},
	{"indexed_memory64", "2943018080808010", InstrI64Load, false},
	{"memory_size", "3f01", InstrMemorySize, false},
	{"memory_grow", "4001", InstrMemoryGrow, false},
	{"sat", "fc00", InstrI32TruncSatF32S, false},
	{"memory_init", "fc080100", InstrMemoryInit, false},
	{"data_drop", "fc0901", InstrDataDrop, false},
	{"memory_copy", "fc0a0100", InstrMemoryCopy, false},
	{"memory_fill", "fc0b01", InstrMemoryFill, false},
	{"table_init", "fc0c0100", InstrTableInit, false},
	{"elem_drop", "fc0d01", InstrElemDrop, false},
	{"table_copy", "fc0e0100", InstrTableCopy, false},
	{"table_grow", "fc0f01", InstrTableGrow, false},
	{"v128_const", "fd0c000102030405060708090a0b0c0d0e0f", InstrV128Const, false},
	{"shuffle", "fd0d000102030405060708090a0b0c0d0e0f", InstrI8x16Shuffle, false},
	{"lane", "fd1b02", InstrI32x4ExtractLane, false},
	{"simd_memory64", "fd0044018080808010", InstrV128Load, false},
	{"simd_memory_lane", "fd54000003", InstrV128Load8Lane, false},
	{"atomic_load", "fe100200", InstrI32AtomicLoad, false},
	{"atomic_fence", "fe0300", InstrAtomicFence, false},
	{"ref_null", "d06e", InstrRefNull, false},
	{"ref_func", "d200", InstrRefFunc, false},
	{"br_on_null", "d500", InstrBrOnNull, false},
	{"struct_get", "fb020001", InstrStructGet, false},
	{"array_fixed", "fb080002", InstrArrayNewFixed, false},
	{"ref_test", "fb146d", InstrRefTest, false},
	{"ref_cast", "fb176d", InstrRefCast, false},
	{"br_on_cast", "fb1803006e6d", InstrBrOnCast, false},
	{"throw", "0800", InstrThrow, false},
	{"try_table", "1f4001000000", InstrTryTable, true},
}

func bytecodeSummaryBytes(t testing.TB, s string) []byte {
	t.Helper()
	b, e := hex.DecodeString(s)
	if e != nil {
		t.Fatal(e)
	}
	return b
}
func bytecodeSummaryModule() *Module {
	return &Module{Memories: []MemType{{}, {Limits: Limits{Addr64: true}}}}
}
func bytecodeSummaryGate(c ModuleInstructionClassifier, tc bytecodeSummaryCase, data []byte, imm *InstructionImmediate, wrongSkip bool) error {
	r := ReaderFrom(data)
	op, _ := r.Byte()
	if err := c.ClassifyInto(&r, op, imm); err != nil {
		return err
	}
	if wrongSkip {
		_ = r.JumpTo(r.Offset() - 1)
	}
	wantKind := tc.kind
	// These two supported SIMD forms use Prefix/Subopcode as their class. The
	// existing scanner deliberately leaves Kind unset; its consumers use 0xfd.
	if tc.name == "v128_const" || tc.name == "shuffle" {
		wantKind = InstrInvalid
		if imm.Prefix != 0xfd || imm.Subopcode != uint32(data[1]) {
			return fmt.Errorf("missing SIMD class: %+v", imm)
		}
	}
	if r.Offset() != len(data) || imm.Kind != wantKind {
		return fmt.Errorf("%s: offset=%d want=%d kind=%v want=%v", tc.name, r.Offset(), len(data), imm.Kind, wantKind)
	}
	return nil
}
func TestBytecodeSummaryImmediateMatrix(t *testing.T) {
	m := bytecodeSummaryModule()
	c := NewModuleInstructionClassifier(m, true)
	widths := moduleMemargWidths(m)
	for _, tc := range bytecodeSummaryCases {
		t.Run(tc.name, func(t *testing.T) {
			data := bytecodeSummaryBytes(t, tc.code)
			var fresh InstructionImmediate
			if e := bytecodeSummaryGate(c, tc, data, &fresh, false); e != nil {
				t.Fatal(e)
			}
			full := append([]byte(nil), data...)
			if tc.tail {
				full = append(full, 0x0b)
			}
			r := newReader(full)
			in, e := decodeInstructionWithMemargWidths(r, 0, widths)
			if e != nil || r.pos != len(full) || in.Kind != tc.kind {
				t.Fatalf("full decode: %v offset=%d kind=%v", e, r.pos, in.Kind)
			}
			// Extra payload checks use hand values, rather than deriving expectations
			// from the shared full decoder.
			switch tc.name {
			case "local_index", "global_index", "call", "call_ref", "tail_ref":
				if fresh.Index != 128 {
					t.Fatal(fresh)
				}
			case "call_indirect", "tail_indirect":
				if fresh.Index != 128 || fresh.Index2 != 1 {
					t.Fatal(fresh)
				}
			case "memory32", "indexed_memory32":
				if fresh.MemIndex != 0 || fresh.MemAlign != 2 || fresh.MemOffset != 128 || !fresh.TouchesMemory {
					t.Fatal(fresh)
				}
			case "indexed_memory64", "simd_memory64":
				if fresh.MemIndex != 1 || fresh.MemOffset != 1<<32 || !fresh.HasMemIndex || !fresh.TouchesMemory {
					t.Fatal(fresh)
				}
			case "memory_grow", "table_grow", "data_drop", "elem_drop":
				if fresh.Index != 1 {
					t.Fatal(fresh)
				}
			case "memory_init", "table_init", "memory_copy", "table_copy":
				if fresh.Index != 1 || fresh.Index2 != 0 {
					t.Fatal(fresh)
				}
			}
			// A real prior classification populates scratch. Every predecessor is used.
			for _, before := range bytecodeSummaryCases {
				var reused InstructionImmediate
				b := bytecodeSummaryBytes(t, before.code)
				if e := bytecodeSummaryGate(c, before, b, &reused, false); e != nil {
					t.Fatal(e)
				}
				if e := bytecodeSummaryGate(c, tc, data, &reused, false); e != nil {
					t.Fatal(e)
				}
				if reused != fresh {
					t.Fatalf("after %s: %+v != %+v", before.name, reused, fresh)
				}
			}
		})
	}
}
func TestBytecodeSummaryWrongSkipControl(t *testing.T) {
	tc := bytecodeSummaryCases[0]
	var imm InstructionImmediate
	data := bytecodeSummaryBytes(t, tc.code)
	err := bytecodeSummaryGate(NewModuleInstructionClassifier(bytecodeSummaryModule(), true), tc, data, &imm, true)
	want := fmt.Sprintf("offset=%d want=%d", len(data)-1, len(data))
	if err == nil || !strings.Contains(err.Error(), want) {
		t.Fatalf("wrong-skip control error = %v, want %s", err, want)
	}
}

func TestBytecodeSummaryValidModules(t *testing.T) {
	for _, fixture := range summaryfixtures.Modules {
		t.Run(fixture.Name, func(t *testing.T) {
			data := bytecodeSummaryBytes(t, fixture.Hex)
			if len(data) > 512 {
				t.Fatal("fixture exceeds 512-byte budget")
			}
			fast, e := DecodeModule(data)
			if e != nil {
				t.Fatal(e)
			}
			ast, e := decodeModuleASTForTest(data)
			if e != nil {
				t.Fatal(e)
			}
			f := ValidationFeatures{MultiMemory: true}
			if e = ValidateModuleWithFeatures(fast, f); e != nil {
				t.Fatal(e)
			}
			if e = ValidateModuleWithFeatures(ast, f); e != nil {
				t.Fatal(e)
			}
			// Re-decode each raw expression with the full structured decoder. This also
			// checks that mixed-width immediates preserve the entire body boundary.
			widths := moduleMemargWidths(fast)
			for i, fn := range fast.Code {
				r := newReader(fn.BodyBytes)
				expr, e := decodeExprWithMemargWidths(r, 0, widths)
				if e != nil || r.pos != len(fn.BodyBytes) {
					t.Fatalf("body %d: %v offset=%d", i, e, r.pos)
				}
				if !reflect.DeepEqual(expr, ast.Code[i].Body) {
					t.Fatalf("body %d differs from AST", i)
				}
			}
		})
	}
}

func BenchmarkBytecodeSummary(b *testing.B) {
	var body []byte
	for _, tc := range bytecodeSummaryCases {
		body = append(body, bytecodeSummaryBytes(b, tc.code)...)
		if tc.tail {
			body = append(body, 0x0b)
		}
	}
	c := NewModuleInstructionClassifier(bytecodeSummaryModule(), true)
	b.Run("classify", func(b *testing.B) {
		b.ReportAllocs()
		b.SetBytes(int64(len(body)))
		for i := 0; i < b.N; i++ {
			r := ReaderFrom(body)
			var imm InstructionImmediate
			for r.HasNext() {
				op, _ := r.Byte()
				if e := c.ClassifyInto(&r, op, &imm); e != nil {
					b.Fatal(e)
				}
			}
		}
	})
	b.Run("decode", func(b *testing.B) {
		b.ReportAllocs()
		b.SetBytes(int64(len(body)))
		widths := moduleMemargWidths(bytecodeSummaryModule())
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			r := newReader(body)
			for r.has() {
				if _, e := decodeInstructionWithMemargWidths(r, 0, widths); e != nil {
					b.Fatal(e)
				}
			}
		}
	})
}

// Malformed fixtures are bounded parser inputs. They are never compiled or run.
func TestBytecodeSummaryMalformed(t *testing.T) {
	for _, tc := range []struct {
		name, code                                     string
		errorCode                                      DecodeErrorCode
		scanOffset, scanStop, decodeOffset, decodeStop int
	}{
		{"truncated_u32", "1080", ErrIndexOutOfBounds, 2, 2, 2, 2},
		{"overflow_u32", "10ffffffff10", ErrMalformedLEB, 6, 6, 6, 6},
		{"truncated_float", "430000", ErrIndexOutOfBounds, 1, 1, 1, 1},
		{"truncated_branch_vector", "0e0100", ErrIndexOutOfBounds, 3, 3, 3, 3},
		{"truncated_select_vector", "1c027f", ErrIndexOutOfBounds, 3, 3, 3, 3},
		{"truncated_try_vector", "1f40010000", ErrIndexOutOfBounds, 5, 5, 5, 5},
		{"bad_cast_flags", "fb1804006e6d", ErrInvalidInstruction, 2, 3, 2, 3},
		{"bad_fence", "fe0301", ErrInvalidInstruction, 2, 3, 2, 3},
		{"unknown_opcode", "ff", ErrInvalidInstruction, 0, 1, 0, 1},
		{"truncated_v128", "fd0c0001", ErrIndexOutOfBounds, 2, 2, 4, 4},
	} {
		t.Run(tc.name, func(t *testing.T) {
			data := bytecodeSummaryBytes(t, tc.code)
			r := ReaderFrom(data)
			op, _ := r.Byte()
			var imm InstructionImmediate
			fast := NewModuleInstructionClassifier(bytecodeSummaryModule(), true).ClassifyInto(&r, op, &imm)
			full := newReader(data)
			_, slow := decodeInstructionWithMemargWidths(full, 0, moduleMemargWidths(bytecodeSummaryModule()))
			if fast == nil || slow == nil {
				t.Fatalf("expected rejection: scan=%v full=%v", fast, slow)
			}

			var fe, se *DecodeError
			if !errors.As(fast, &fe) || !errors.As(slow, &se) || fe.Code != tc.errorCode || se.Code != tc.errorCode || fe.Offset != tc.scanOffset || r.Offset() != tc.scanStop || se.Offset != tc.decodeOffset || full.pos != tc.decodeStop {
				t.Fatalf("error contract: scan=%v stopped=%d full=%v stopped=%d want=%+v", fast, r.Offset(), slow, full.pos, tc)
			}
			m := modWithFunc(nil, nil)
			m.Code[0].BodyBytes = data
			ve := ValidateModule(m)
			var de *DecodeError
			if !errors.As(ve, &de) || de.Code != tc.errorCode || de.Offset != tc.decodeOffset {
				t.Fatalf("validator error=%v want code=%d offset=%d", ve, tc.errorCode, tc.decodeOffset)
			}
			t.Logf("scan=%v stopped=%d; full=%v stopped=%d; validator=%v", fast, r.Offset(), slow, full.pos, ve)
		})
	}
}
func TestBytecodeSummarySemanticRejection(t *testing.T) {
	// A lane value is one syntactically valid byte. Range checking belongs to
	// validation. Both decoders can consume it before validation rejects it.
	data := bytecodeSummaryBytes(t, "fd0c00000000000000000000000000000000fd1b041a0b")
	m := &Module{Types: []RecType{{SubTypes: []SubType{{Comp: CompType{Kind: CompFunc}}}}}, FuncTypes: []TypeIdx{{Index: 0}}, Code: []Func{{BodyBytes: data}}}
	r := newReader(data)
	expr, e := decodeExpr(r, 0)
	if e != nil {
		t.Fatal(e)
	}
	byteErr := ValidateModule(m)
	m.Code[0] = Func{Body: expr}
	astErr := ValidateModule(m)
	if byteErr == nil || astErr == nil {
		t.Fatalf("byte=%v tree=%v", byteErr, astErr)
	}
	var be, ae *ValidationError
	if !errors.As(byteErr, &be) || !errors.As(astErr, &ae) || be.Code != ErrTypeMismatch || ae.Code != ErrTypeMismatch || be.Func != 0 || ae.Func != 0 || be.Detail != "simd lane out of range" || ae.Detail != be.Detail {
		t.Fatalf("wrong validation errors: %v / %v", byteErr, astErr)
	}
	t.Logf("byte=%v; tree=%v", byteErr, astErr)
}

func TestBytecodeSummaryScanAllocations(t *testing.T) {
	c := NewModuleInstructionClassifier(bytecodeSummaryModule(), true)
	for _, tc := range bytecodeSummaryCases {
		data := bytecodeSummaryBytes(t, tc.code)
		n := testing.AllocsPerRun(100, func() {
			r := ReaderFrom(data)
			op, _ := r.Byte()
			var imm InstructionImmediate
			if e := c.ClassifyInto(&r, op, &imm); e != nil {
				panic(e)
			}
		})
		if n != 0 {
			t.Fatalf("%s: %v allocations", tc.name, n)
		}
	}
}
