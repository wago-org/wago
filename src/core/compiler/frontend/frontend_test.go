package frontend

import (
	"strings"
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
	"github.com/wago-org/wago/testutil/wasmtest"
)

func TestDecodeValidateAcceptsSupportedMVPModule(t *testing.T) {
	mod := wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType(nil, []wasm.ValType{wasm.I32}))),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(0))),
		wasmtest.Section(5, wasmtest.Vec([]byte{0x00, 0x01})),
		wasmtest.Section(7, wasmtest.Vec(
			wasmtest.ExportEntry("main", 0, 0),
			wasmtest.ExportEntry("memory", 2, 0),
		)),
		wasmtest.Section(10, wasmtest.Vec(wasmtest.Code([]byte{0x41, 0x2a, 0x0b}))),
	)
	m, err := DecodeValidate(mod)
	if err != nil {
		t.Fatalf("DecodeValidate supported MVP module: %v", err)
	}
	if len(m.Code) != 1 || len(m.Exports) != 2 {
		t.Fatalf("module summary = %d funcs, %d exports", len(m.Code), len(m.Exports))
	}
}

func TestDecodeValidateAcceptsSignExtensionOps(t *testing.T) {
	// i32.extend8_s/16_s (0xc0/0xc1) and i64.extend8_s/16_s/32_s (0xc2/0xc3/0xc4)
	// are MVP sign-extension ops the backend now lowers; the support pass must
	// accept them.
	cases := []struct {
		name   string
		params []wasm.ValType
		result wasm.ValType
		op     byte
	}{
		{"i32.extend8_s", []wasm.ValType{wasm.I32}, wasm.I32, 0xc0},
		{"i32.extend16_s", []wasm.ValType{wasm.I32}, wasm.I32, 0xc1},
		{"i64.extend8_s", []wasm.ValType{wasm.I64}, wasm.I64, 0xc2},
		{"i64.extend16_s", []wasm.ValType{wasm.I64}, wasm.I64, 0xc3},
		{"i64.extend32_s", []wasm.ValType{wasm.I64}, wasm.I64, 0xc4},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			mod := wasmtest.Module(
				wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType(tc.params, []wasm.ValType{tc.result}))),
				wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(0))),
				wasmtest.Section(5, wasmtest.Vec([]byte{0x00, 0x01})),
				wasmtest.Section(10, wasmtest.Vec(wasmtest.Code([]byte{0x20, 0x00, tc.op, 0x0b}))),
			)
			if _, err := DecodeValidate(mod); err != nil {
				t.Fatalf("DecodeValidate %s: %v", tc.name, err)
			}
		})
	}
}

// TestDecodeValidateAcceptsFloatRoundingOps proves the support pass (and thus
// the CLI/API path, not just the backend's CompileFunction) accepts the float
// rounding and copysign ops the backend now lowers.
func TestDecodeValidateAcceptsFloatRoundingOps(t *testing.T) {
	f64t := []wasm.ValType{wasm.F64, wasm.F64}
	f32t := []wasm.ValType{wasm.F32, wasm.F32}
	cases := []struct {
		name   string
		params []wasm.ValType
		result wasm.ValType
		body   []byte
	}{
		{"f64.ceil", f64t, wasm.F64, []byte{0x20, 0x00, 0x9b, 0x0b}},
		{"f64.nearest", f64t, wasm.F64, []byte{0x20, 0x00, 0x9e, 0x0b}},
		{"f64.copysign", f64t, wasm.F64, []byte{0x20, 0x00, 0x20, 0x01, 0xa6, 0x0b}},
		{"f32.floor", f32t, wasm.F32, []byte{0x20, 0x00, 0x8e, 0x0b}},
		{"f32.copysign", f32t, wasm.F32, []byte{0x20, 0x00, 0x20, 0x01, 0x98, 0x0b}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			mod := wasmtest.Module(
				wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType(c.params, []wasm.ValType{c.result}))),
				wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(0))),
				wasmtest.Section(10, wasmtest.Vec(wasmtest.Code(c.body))),
			)
			if _, err := DecodeValidate(mod); err != nil {
				t.Fatalf("DecodeValidate %s: %v", c.name, err)
			}
		})
	}
}

func TestRejectUnsupportedGlobalTypes(t *testing.T) {
	mod := wasmtest.Module(wasmtest.Section(2, wasmtest.Vec(wasmtest.GlobalImportEntry("env", "vec", wasm.V128, false))))
	_, err := DecodeValidate(mod)
	assertErrContains(t, err, "unsupported global type v128 at import 0")
}

func TestAcceptsMemoryImport(t *testing.T) {
	memImport := append(wasmtest.Name("env"), wasmtest.Name("mem")...)
	memImport = append(memImport, 0x02, 0x00, 0x01) // memory import, min 1 page
	mod := wasmtest.Module(wasmtest.Section(2, wasmtest.Vec(memImport)))
	if _, err := DecodeValidate(mod); err != nil {
		t.Fatalf("memory import (min 1) should be accepted: %v", err)
	}
}

func TestRejectUnsupportedImports(t *testing.T) {
	t.Run("memory min above the 65535-page cap", func(t *testing.T) {
		memImport := append(wasmtest.Name("env"), wasmtest.Name("mem")...)
		memImport = append(memImport, 0x02, 0x00, 0x80, 0x80, 0x04) // min 65536 pages (LEB)
		mod := wasmtest.Module(wasmtest.Section(2, wasmtest.Vec(memImport)))
		_, err := DecodeValidate(mod)
		assertErrContains(t, err, "exceeds 65535")
	})
	t.Run("table", func(t *testing.T) {
		tblImport := append(wasmtest.Name("env"), wasmtest.Name("t")...)
		tblImport = append(tblImport, 0x01, 0x70, 0x00, 0x01) // table funcref min 1
		mod := wasmtest.Module(wasmtest.Section(2, wasmtest.Vec(tblImport)))
		_, err := DecodeValidate(mod)
		assertErrContains(t, err, "unsupported import table")
	})
	t.Run("function result", func(t *testing.T) {
		// (i32) -> (i32): the replay model cannot return a value to wasm.
		mod := wasmtest.Module(
			wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType([]wasm.ValType{wasm.I32}, []wasm.ValType{wasm.I32}))),
			wasmtest.Section(2, wasmtest.Vec(funcImport("env", "f", 0))),
		)
		_, err := DecodeValidate(mod)
		assertErrContains(t, err, "unsupported import function result")
	})
	t.Run("non-i32 first param", func(t *testing.T) {
		// (f64) -> (): only the first arg is captured, and as an i32.
		mod := wasmtest.Module(
			wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType([]wasm.ValType{wasm.F64}, nil))),
			wasmtest.Section(2, wasmtest.Vec(funcImport("env", "f", 0))),
		)
		_, err := DecodeValidate(mod)
		assertErrContains(t, err, "unsupported import function signature")
	})
}

// funcImport builds a function import entry referencing type index typeIdx.
func funcImport(module, name string, typeIdx uint32) []byte {
	out := append(wasmtest.Name(module), wasmtest.Name(name)...)
	out = append(out, 0x00) // ExternFunc
	return append(out, wasmtest.ULEB(typeIdx)...)
}

// TestAcceptsMultiParamHostImport proves the support pass accepts host imports
// with several numeric args and no result — notably AssemblyScript's
// env.abort(msg, file, line, col), all i32 — which gates running real AS
// modules (e.g. json-as) on wago.
func TestAcceptsMultiParamHostImport(t *testing.T) {
	cases := []struct {
		name   string
		params []wasm.ValType
	}{
		{"no params", nil},
		{"single i32", []wasm.ValType{wasm.I32}},
		{"abort (4x i32)", []wasm.ValType{wasm.I32, wasm.I32, wasm.I32, wasm.I32}},
		{"mixed numeric tail", []wasm.ValType{wasm.I32, wasm.I64, wasm.F64}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			mod := wasmtest.Module(
				wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType(c.params, nil))),
				wasmtest.Section(2, wasmtest.Vec(funcImport("env", "abort", 0))),
			)
			if _, err := DecodeValidate(mod); err != nil {
				t.Fatalf("multi-param host import %q should be accepted: %v", c.name, err)
			}
		})
	}
}

func TestRejectUnsupportedReferenceTypes(t *testing.T) {
	t.Run("externref table", func(t *testing.T) {
		mod := wasmtest.Module(wasmtest.Section(4, wasmtest.Vec([]byte{0x6f, 0x00, 0x01})))
		_, err := DecodeValidate(mod)
		assertErrContains(t, err, "unsupported reference type externref at table 0")
	})
	t.Run("funcref parameter", func(t *testing.T) {
		mod := wasmtest.Module(wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType([]wasm.ValType{wasm.FuncRef}, nil))))
		_, err := DecodeValidate(mod)
		assertErrContains(t, err, "unsupported reference type funcref at type 0 params[0]")
	})
	t.Run("ref.null instruction", func(t *testing.T) {
		mod := wasmtest.Module(
			wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType(nil, nil))),
			wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(0))),
			wasmtest.Section(10, wasmtest.Vec(wasmtest.Code([]byte{0xd0, 0x70, 0x1a, 0x0b}))),
		)
		_, err := DecodeValidate(mod)
		assertErrContains(t, err, "unsupported reference instruction RefNull at function 0 instruction 0")
	})
}

func TestRejectUnsupportedGC(t *testing.T) {
	t.Run("struct type", func(t *testing.T) {
		mod := wasmtest.Module(wasmtest.Section(1, wasmtest.Vec([]byte{0x5f, 0x00})))
		_, err := DecodeValidate(mod)
		assertErrContains(t, err, "unsupported gc type struct type at type 0")
	})
	t.Run("array type", func(t *testing.T) {
		mod := wasmtest.Module(wasmtest.Section(1, wasmtest.Vec([]byte{0x5e, 0x7f, 0x00})))
		_, err := DecodeValidate(mod)
		assertErrContains(t, err, "unsupported gc type array type at type 0")
	})
}

func TestRejectUnsupportedTagForms(t *testing.T) {
	t.Run("tag section", func(t *testing.T) {
		mod := wasmtest.Module(
			wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType(nil, nil))),
			wasmtest.Section(13, wasmtest.Vec([]byte{0x00, 0x00})),
		)
		_, err := DecodeValidate(mod)
		assertErrContains(t, err, "unsupported tag section at tag section")
	})
}

func TestRejectUnsupportedCurrentBackendGaps(t *testing.T) {
	t.Run("memory.grow is now supported", func(t *testing.T) {
		mod := wasmtest.Module(
			wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType(nil, []wasm.ValType{wasm.I32}))),
			wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(0))),
			wasmtest.Section(5, wasmtest.Vec([]byte{0x00, 0x01})),
			wasmtest.Section(10, wasmtest.Vec(wasmtest.Code([]byte{0x41, 0x01, 0x40, 0x00, 0x0b}))),
		)
		if _, err := DecodeValidate(mod); err != nil {
			t.Fatalf("memory.grow should pass the support filter now, got %v", err)
		}
	})
	t.Run("indexed memory.size decodes before support filtering", func(t *testing.T) {
		mod := wasmtest.Module(
			wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType(nil, []wasm.ValType{wasm.I32}))),
			wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(0))),
			wasmtest.Section(5, wasmtest.Vec([]byte{0x00, 0x01}, []byte{0x00, 0x01})),
			wasmtest.Section(10, wasmtest.Vec(wasmtest.Code([]byte{0x3f, 0x01, 0x0b}))),
		)
		_, err := DecodeValidate(mod)
		assertErrContains(t, err, "unsupported memory multiple memories at module")
	})
}

// TestDecodeValidateAcceptsI64SubwidthMemOps covers the i64 narrow load/store
// ops the backend now lowers: i64.load8/16/32_s/u (0x30-0x35) and
// i64.store8/16/32 (0x3c-0x3e). The support pass must accept them.
func TestDecodeValidateAcceptsI64SubwidthMemOps(t *testing.T) {
	loads := []byte{0x30, 0x31, 0x32, 0x33, 0x34, 0x35}
	for _, op := range loads {
		mod := wasmtest.Module(
			wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType([]wasm.ValType{wasm.I32}, []wasm.ValType{wasm.I64}))),
			wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(0))),
			wasmtest.Section(5, wasmtest.Vec([]byte{0x00, 0x01})),
			wasmtest.Section(10, wasmtest.Vec(wasmtest.Code([]byte{0x20, 0x00, op, 0x00, 0x00, 0x0b}))),
		)
		if _, err := DecodeValidate(mod); err != nil {
			t.Fatalf("load op 0x%02x: %v", op, err)
		}
	}
	stores := []byte{0x3c, 0x3d, 0x3e}
	for _, op := range stores {
		mod := wasmtest.Module(
			wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType([]wasm.ValType{wasm.I32, wasm.I64}, nil))),
			wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(0))),
			wasmtest.Section(5, wasmtest.Vec([]byte{0x00, 0x01})),
			wasmtest.Section(10, wasmtest.Vec(wasmtest.Code([]byte{0x20, 0x00, 0x20, 0x01, op, 0x00, 0x00, 0x0b}))),
		)
		if _, err := DecodeValidate(mod); err != nil {
			t.Fatalf("store op 0x%02x: %v", op, err)
		}
	}
}

func TestDecodeValidateSupportPassScansRawBodies(t *testing.T) {
	unsupportedSIMDBody := append([]byte{0xfd, 0x0c}, make([]byte, 16)...)
	unsupportedSIMDBody = append(unsupportedSIMDBody, 0x41, 0x01, 0xfd, 0x6b, 0x1a, 0x0b) // v128.const 0; i32.const 1; i8x16.shl; drop; end

	cases := []struct {
		name         string
		mod          []byte
		wantCategory string
	}{
		{
			name: "supported memory.copy/fill",
			mod: wasmtest.Module(
				wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType([]wasm.ValType{wasm.I32, wasm.I32, wasm.I32}, nil))),
				wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(0))),
				wasmtest.Section(5, wasmtest.Vec([]byte{0x00, 0x01})),
				wasmtest.Section(10, wasmtest.Vec(wasmtest.Code([]byte{
					0x20, 0x00, 0x20, 0x01, 0x20, 0x02, 0xfc, 0x0a, 0x00, 0x00,
					0x20, 0x00, 0x41, 0x00, 0x20, 0x02, 0xfc, 0x0b, 0x00,
					0x0b,
				}))),
			),
		},
		{
			name:         "unsupported explicit memarg index",
			wantCategory: "memory",
			mod: wasmtest.Module(
				wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType([]wasm.ValType{wasm.I32}, []wasm.ValType{wasm.I32}))),
				wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(0))),
				wasmtest.Section(5, wasmtest.Vec([]byte{0x00, 0x01})),
				wasmtest.Section(10, wasmtest.Vec(wasmtest.Code([]byte{0x20, 0x00, 0x28, 0x42, 0x00, 0x00, 0x0b}))),
			),
		},
		{
			name:         "unsupported memory.init",
			wantCategory: "data",
			mod: wasmtest.Module(
				wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType(nil, nil))),
				wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(0))),
				wasmtest.Section(5, wasmtest.Vec([]byte{0x00, 0x01})),
				wasmtest.Section(12, wasmtest.ULEB(1)),
				wasmtest.Section(10, wasmtest.Vec(wasmtest.Code([]byte{0x41, 0x00, 0x41, 0x00, 0x41, 0x00, 0xfc, 0x08, 0x00, 0x00, 0x0b}))),
				wasmtest.Section(11, wasmtest.Vec([]byte{0x01, 0x00})),
			),
		},
		{
			name:         "unsupported table.copy",
			wantCategory: "instruction",
			mod: wasmtest.Module(
				wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType(nil, nil))),
				wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(0))),
				wasmtest.Section(4, wasmtest.Vec([]byte{0x70, 0x00, 0x01})),
				wasmtest.Section(10, wasmtest.Vec(wasmtest.Code([]byte{0x41, 0x00, 0x41, 0x00, 0x41, 0x00, 0xfc, 0x0e, 0x00, 0x00, 0x0b}))),
			),
		},
		{
			name:         "unsupported ref.null",
			wantCategory: "reference instruction",
			mod: wasmtest.Module(
				wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType(nil, nil))),
				wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(0))),
				wasmtest.Section(10, wasmtest.Vec(wasmtest.Code([]byte{0xd0, 0x70, 0x1a, 0x0b}))),
			),
		},
		{
			name:         "unsupported simd i8x16.shl",
			wantCategory: "instruction",
			mod: wasmtest.Module(
				wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType(nil, nil))),
				wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(0))),
				wasmtest.Section(10, wasmtest.Vec(wasmtest.Code(unsupportedSIMDBody))),
			),
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := DecodeValidate(tc.mod)
			if tc.wantCategory == "" {
				if err != nil {
					t.Fatalf("DecodeValidate: %v", err)
				}
				return
			}
			ue, ok := err.(*UnsupportedError)
			if !ok {
				t.Fatalf("DecodeValidate error = %T %v, want UnsupportedError", err, err)
			}
			if ue.Category != tc.wantCategory {
				t.Fatalf("unsupported category = %q (%v), want %q", ue.Category, err, tc.wantCategory)
			}
		})
	}
}

func TestSupportPassRawBodyPolicyErrorsKeepInstructionContext(t *testing.T) {
	cases := []struct {
		name string
		feat Features
		body []byte
		want string
	}{
		{"explicit memarg index", AllFeatures(), []byte{0x28, 0x42, 0x00, 0x00, 0x0b}, "unsupported memory explicit index 0 at function 0 instruction 0"},
		{"nonzero memory index", AllFeatures(), []byte{0x3f, 0x01, 0x0b}, "unsupported memory index 1 at function 0 instruction 0"},
		{"nonzero call_indirect table", AllFeatures(), []byte{0x11, 0x00, 0x01, 0x0b}, "unsupported table call_indirect table 1 at function 0 instruction 0"},
		{"sign extension disabled", Features{BulkMemory: true, SaturatingTrunc: true}, []byte{0xc0, 0x0b}, "unsupported instruction sign-extension-ops disabled at function 0 instruction 0"},
		{"bulk memory disabled", Features{SignExtension: true, SaturatingTrunc: true}, []byte{0xfc, 0x0a, 0x00, 0x00, 0x0b}, "unsupported instruction memory.copy (bulk-memory-operations disabled) at function 0 instruction 0"},
		{"saturating trunc disabled", Features{SignExtension: true, BulkMemory: true}, []byte{0xfc, 0x00, 0x0b}, "unsupported instruction nontrapping-float-to-int-conversion disabled at function 0 instruction 0"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := (supportPass{feat: tc.feat}).exprBytes(tc.body, "function 0")
			assertErrContains(t, err, tc.want)
		})
	}
}

func TestRejectUnsupportedExplicitMemargIndex(t *testing.T) {
	// Even memidx 0 uses the multi-memory memarg encoding. The backend consumes
	// BodyBytes directly, so accepting this form would desynchronize its MVP
	// memarg reader.
	mod := wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType([]wasm.ValType{wasm.I32}, []wasm.ValType{wasm.I32}))),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(0))),
		wasmtest.Section(5, wasmtest.Vec([]byte{0x00, 0x01})),
		wasmtest.Section(10, wasmtest.Vec(wasmtest.Code([]byte{0x20, 0x00, 0x28, 0x42, 0x00, 0x00, 0x0b}))),
	)
	_, err := DecodeValidate(mod)
	assertErrContains(t, err, "unsupported memory explicit index 0 at function 0 instruction 1")
}

func TestDecodeValidateAcceptsSupportedSIMDIntegerTranche(t *testing.T) {
	v128Const := func() []byte {
		return append([]byte{0xfd, 0x0c}, make([]byte, 16)...)
	}
	cases := []struct {
		name string
		sub  uint32
	}{
		{"i8x16.eq", 35}, {"i8x16.ne", 36}, {"i8x16.lt_s", 37}, {"i8x16.lt_u", 38}, {"i8x16.gt_s", 39}, {"i8x16.gt_u", 40}, {"i8x16.le_s", 41}, {"i8x16.le_u", 42}, {"i8x16.ge_s", 43}, {"i8x16.ge_u", 44},
		{"i16x8.eq", 45}, {"i16x8.ne", 46}, {"i16x8.lt_s", 47}, {"i16x8.lt_u", 48}, {"i16x8.gt_s", 49}, {"i16x8.gt_u", 50}, {"i16x8.le_s", 51}, {"i16x8.le_u", 52}, {"i16x8.ge_s", 53}, {"i16x8.ge_u", 54},
		{"i32x4.eq", 55}, {"i32x4.ne", 56}, {"i32x4.lt_s", 57}, {"i32x4.lt_u", 58}, {"i32x4.gt_s", 59}, {"i32x4.gt_u", 60}, {"i32x4.le_s", 61}, {"i32x4.le_u", 62}, {"i32x4.ge_s", 63}, {"i32x4.ge_u", 64},
		{"i8x16.add", 110}, {"i8x16.sub", 113}, {"i8x16.min_s", 118}, {"i8x16.min_u", 119}, {"i8x16.max_s", 120}, {"i8x16.max_u", 121}, {"i8x16.avgr_u", 123},
		{"i16x8.add", 142}, {"i16x8.sub", 145}, {"i16x8.mul", 149}, {"i16x8.min_s", 150}, {"i16x8.min_u", 151}, {"i16x8.max_s", 152}, {"i16x8.max_u", 153}, {"i16x8.avgr_u", 155},
		{"i32x4.add", 174}, {"i32x4.sub", 177}, {"i32x4.mul", 181}, {"i32x4.min_s", 182}, {"i32x4.min_u", 183}, {"i32x4.max_s", 184}, {"i32x4.max_u", 185},
		{"i64x2.add", 206}, {"i64x2.sub", 209}, {"i64x2.eq", 214}, {"i64x2.ne", 215},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			body := v128Const()
			body = append(body, v128Const()...)
			body = append(body, 0xfd)
			body = append(body, wasmtest.ULEB(tc.sub)...)
			body = append(body, 0x0b)
			mod := wasmtest.Module(
				wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType(nil, []wasm.ValType{wasm.V128}))),
				wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(0))),
				wasmtest.Section(10, wasmtest.Vec(wasmtest.Code(body))),
			)
			if _, err := DecodeValidate(mod); err != nil {
				t.Fatalf("DecodeValidate: %v", err)
			}
		})
	}
}

func TestDecodeValidateAcceptsSupportedSIMDUnaryTranche(t *testing.T) {
	v128Const := func() []byte {
		return append([]byte{0xfd, 0x0c}, make([]byte, 16)...)
	}
	cases := []struct {
		name string
		sub  uint32
	}{
		{"i8x16.abs", 96}, {"i8x16.neg", 97}, {"i8x16.popcnt", 98},
		{"i16x8.abs", 128}, {"i16x8.neg", 129},
		{"i32x4.abs", 160}, {"i32x4.neg", 161},
		{"i64x2.neg", 193},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			body := v128Const()
			body = append(body, 0xfd)
			body = append(body, wasmtest.ULEB(tc.sub)...)
			body = append(body, 0x0b)
			mod := wasmtest.Module(
				wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType(nil, []wasm.ValType{wasm.V128}))),
				wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(0))),
				wasmtest.Section(10, wasmtest.Vec(wasmtest.Code(body))),
			)
			if _, err := DecodeValidate(mod); err != nil {
				t.Fatalf("DecodeValidate: %v", err)
			}
		})
	}
}

func TestDecodeValidateAcceptsSupportedSIMDBooleanTranche(t *testing.T) {
	v128Const := func() []byte {
		return append([]byte{0xfd, 0x0c}, make([]byte, 16)...)
	}
	cases := []struct {
		name string
		sub  uint32
	}{
		{"v128.any_true", 83},
		{"i8x16.all_true", 99}, {"i8x16.bitmask", 100},
		{"i16x8.all_true", 131}, {"i16x8.bitmask", 132},
		{"i32x4.all_true", 163}, {"i32x4.bitmask", 164},
		{"i64x2.all_true", 195}, {"i64x2.bitmask", 196},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			body := v128Const()
			body = append(body, 0xfd)
			body = append(body, wasmtest.ULEB(tc.sub)...)
			body = append(body, 0x0b)
			mod := wasmtest.Module(
				wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType(nil, []wasm.ValType{wasm.I32}))),
				wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(0))),
				wasmtest.Section(10, wasmtest.Vec(wasmtest.Code(body))),
			)
			if _, err := DecodeValidate(mod); err != nil {
				t.Fatalf("DecodeValidate: %v", err)
			}
		})
	}
}

func TestDecodeValidateAcceptsSupportedSIMDPackedFloatTranche(t *testing.T) {
	v128Const := func() []byte {
		return append([]byte{0xfd, 0x0c}, make([]byte, 16)...)
	}
	cases := []struct {
		name  string
		sub   uint32
		unary bool
	}{
		{"f32x4.eq", 65, false}, {"f32x4.ne", 66, false}, {"f32x4.lt", 67, false}, {"f32x4.gt", 68, false}, {"f32x4.le", 69, false}, {"f32x4.ge", 70, false},
		{"f64x2.eq", 71, false}, {"f64x2.ne", 72, false}, {"f64x2.lt", 73, false}, {"f64x2.gt", 74, false}, {"f64x2.le", 75, false}, {"f64x2.ge", 76, false},
		{"f32x4.abs", 224, true}, {"f32x4.neg", 225, true}, {"f32x4.sqrt", 227, true},
		{"f32x4.add", 228, false}, {"f32x4.sub", 229, false}, {"f32x4.mul", 230, false}, {"f32x4.div", 231, false},
		{"f64x2.abs", 236, true}, {"f64x2.neg", 237, true}, {"f64x2.sqrt", 239, true},
		{"f64x2.add", 240, false}, {"f64x2.sub", 241, false}, {"f64x2.mul", 242, false}, {"f64x2.div", 243, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			body := v128Const()
			if !tc.unary {
				body = append(body, v128Const()...)
			}
			body = append(body, 0xfd)
			body = append(body, wasmtest.ULEB(tc.sub)...)
			body = append(body, 0x0b)
			mod := wasmtest.Module(
				wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType(nil, []wasm.ValType{wasm.V128}))),
				wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(0))),
				wasmtest.Section(10, wasmtest.Vec(wasmtest.Code(body))),
			)
			if _, err := DecodeValidate(mod); err != nil {
				t.Fatalf("DecodeValidate: %v", err)
			}
		})
	}
}

func TestDecodeValidateRejectsUnsupportedSIMDIntegerComparisons(t *testing.T) {
	body := append([]byte{0xfd, 0x0c}, make([]byte, 16)...)
	body = append(body, append([]byte{0xfd, 0x0c}, make([]byte, 16)...)...)
	body = append(body, 0xfd)
	body = append(body, wasmtest.ULEB(217)...) // i64x2.gt_s: not yet admitted under the SSE4.1 baseline.
	body = append(body, 0x0b)
	mod := wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType(nil, []wasm.ValType{wasm.V128}))),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(0))),
		wasmtest.Section(10, wasmtest.Vec(wasmtest.Code(body))),
	)
	_, err := DecodeValidate(mod)
	assertErrContains(t, err, "unsupported instruction I64x2GtS at function 0 instruction 2")
}

func TestRejectUnsupportedProposalFeaturesDecodedByWasm3(t *testing.T) {
	t.Run("memory64", func(t *testing.T) {
		mod := wasmtest.Module(wasmtest.Section(5, wasmtest.Vec([]byte{0x04, 0x00}))) // memory64 min 0
		_, err := DecodeValidate(mod)
		assertErrContains(t, err, "unsupported memory memory64 at memory 0")
	})
	t.Run("unsupported simd instruction", func(t *testing.T) {
		body := append([]byte{0xfd, 0x0c}, make([]byte, 16)...)
		body = append(body, 0x41, 0x01, 0xfd, 0x6b, 0x1a, 0x0b) // v128.const 0; i32.const 1; i8x16.shl; drop; end
		mod := wasmtest.Module(
			wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType(nil, nil))),
			wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(0))),
			wasmtest.Section(10, wasmtest.Vec(wasmtest.Code(body))),
		)
		_, err := DecodeValidate(mod)
		assertErrContains(t, err, "unsupported instruction I8x16Shl at function 0 instruction 2")
	})
}

func assertErrContains(t *testing.T, err error, want string) {
	t.Helper()
	if err == nil || !strings.Contains(err.Error(), want) {
		t.Fatalf("error = %v, want substring %q", err, want)
	}
}
