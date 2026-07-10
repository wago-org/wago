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

func TestDecodeValidateAcceptsReferenceTypesByDefault(t *testing.T) {
	mod := wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType(nil, []wasm.ValType{wasm.I32}))),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(0))),
		wasmtest.Section(4, wasmtest.Vec([]byte{0x70, 0x00, 0x00})),                       // funcref table min=0
		wasmtest.Section(10, wasmtest.Vec(wasmtest.Code([]byte{0xfc, 0x10, 0x00, 0x0b}))), // table.size 0
	)
	if _, err := DecodeValidate(mod); err != nil {
		t.Fatalf("DecodeValidate reference-types module with default features: %v", err)
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

func TestDecodeValidateAcceptsV128BlockAndSelectTypes(t *testing.T) {
	v128Const := func(fill byte) []byte { return append([]byte{0xfd, 0x0c}, bytesRepeat(fill, 16)...) }
	cases := []struct {
		name string
		body []byte
	}{
		{
			name: "block result v128 direct type",
			body: append(append([]byte{0x02, 0x7b}, v128Const(0x11)...), 0x0b, 0x0b),
		},
		{
			name: "if result v128 direct type",
			body: append(append(append(append([]byte{0x41, 0x01, 0x04, 0x7b}, v128Const(0x22)...), 0x05), v128Const(0x33)...), 0x0b, 0x0b),
		},
		{
			name: "select v128 typed",
			body: append(append(append(append(v128Const(0x44), v128Const(0x55)...), 0x41, 0x01), 0x1c, 0x01, 0x7b), 0x0b),
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			mod := wasmtest.Module(
				wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType(nil, []wasm.ValType{wasm.V128}))),
				wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(0))),
				wasmtest.Section(10, wasmtest.Vec(wasmtest.Code(tc.body))),
			)
			if _, err := DecodeValidate(mod); err != nil {
				t.Fatalf("DecodeValidate: %v", err)
			}
		})
	}
}

func TestRejectUnsupportedAndRequiresSIMDSeeV128ByteImmediates(t *testing.T) {
	cases := []struct {
		name string
		body []byte
	}{
		{
			name: "block result v128 direct type",
			body: []byte{0x02, 0x7b, 0x00, 0x0b, 0x1a, 0x0b}, // block (result v128); unreachable; end; drop; end
		},
		{
			name: "select v128 typed",
			body: []byte{0x00, 0x41, 0x01, 0x1c, 0x01, 0x7b, 0x1a, 0x0b}, // unreachable; i32.const 1; select (result v128); drop; end
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			modBytes := wasmtest.Module(
				wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType(nil, nil))),
				wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(0))),
				wasmtest.Section(10, wasmtest.Vec(wasmtest.Code(tc.body))),
			)
			m, err := wasm.DecodeModule(modBytes)
			if err != nil {
				t.Fatalf("DecodeModule: %v", err)
			}
			if err := wasm.ValidateModule(m); err != nil {
				t.Fatalf("ValidateModule: %v", err)
			}
			if !ModuleRequiresSIMD(m) {
				t.Fatal("ModuleRequiresSIMD = false, want true")
			}
			err = RejectUnsupportedWithFeatures(m, Features{SignExtension: true, BulkMemory: true, SaturatingTrunc: true})
			if err == nil || !strings.Contains(err.Error(), "v128 (simd disabled)") {
				t.Fatalf("want SIMD-disabled v128 rejection, got %v", err)
			}
		})
	}
}

func TestDecodeValidateAcceptsV128MultiValueBlockType(t *testing.T) {
	body := []byte{0x02, 0x01} // block using type index 1: () -> (v128, i32)
	body = append(body, 0xfd, 0x0c)
	body = append(body, bytesRepeat(0x66, 16)...)
	body = append(body, 0x41, 0x07, 0x0b, 0x0b)
	mod := wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(
			wasmtest.FuncType(nil, []wasm.ValType{wasm.V128, wasm.I32}),
			wasmtest.FuncType(nil, []wasm.ValType{wasm.V128, wasm.I32}),
		)),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(0))),
		wasmtest.Section(10, wasmtest.Vec(wasmtest.Code(body))),
	)
	if _, err := DecodeValidate(mod); err != nil {
		t.Fatalf("DecodeValidate multi-value v128 block type: %v", err)
	}
}

func bytesRepeat(b byte, n int) []byte {
	out := make([]byte, n)
	for i := range out {
		out[i] = b
	}
	return out
}

func TestAcceptsV128GlobalTypes(t *testing.T) {
	v128Const := append([]byte{0xfd, 0x0c}, bytesRepeat(0x7a, 16)...)
	v128Const = append(v128Const, 0x0b)
	mod := wasmtest.Module(
		wasmtest.Section(2, wasmtest.Vec(wasmtest.GlobalImportEntry("env", "vec", wasm.V128, false))),
		wasmtest.Section(6, wasmtest.Vec(wasmtest.GlobalEntry(wasm.V128, true, v128Const))),
	)
	if _, err := DecodeValidate(mod); err != nil {
		t.Fatalf("DecodeValidate v128 globals: %v", err)
	}
}

func TestReferenceGlobalSupportBoundaries(t *testing.T) {
	t.Run("imported funcref rejected", func(t *testing.T) {
		mod := wasmtest.Module(wasmtest.Section(2, wasmtest.Vec(wasmtest.GlobalImportEntry("env", "ref", wasm.FuncRef, false))))
		_, err := DecodeValidate(mod)
		assertErrContains(t, err, "unsupported imported global type funcref at import 0")
	})
	t.Run("local externref accepted", func(t *testing.T) {
		mod := wasmtest.Module(wasmtest.Section(6, wasmtest.Vec(wasmtest.GlobalEntry(wasm.ExternRef, false, []byte{0xd0, 0x6f, 0x0b}))))
		if _, err := DecodeValidate(mod); err != nil {
			t.Fatalf("DecodeValidate local externref global: %v", err)
		}
	})
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
	t.Run("funcref table accepted", func(t *testing.T) {
		// A funcref table import is accepted (cross-instance shared table).
		tblImport := append(wasmtest.Name("env"), wasmtest.Name("t")...)
		tblImport = append(tblImport, 0x01, 0x70, 0x00, 0x01) // table funcref min 1
		mod := wasmtest.Module(wasmtest.Section(2, wasmtest.Vec(tblImport)))
		if _, err := DecodeValidate(mod); err != nil {
			t.Fatalf("funcref table import should be accepted: %v", err)
		}
	})
	t.Run("numeric function result accepted", func(t *testing.T) {
		// (i32) -> (i32): a returning import is accepted (bound cross-instance at
		// link time); only the frontend support gate is exercised here.
		mod := wasmtest.Module(
			wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType([]wasm.ValType{wasm.I32}, []wasm.ValType{wasm.I32}))),
			wasmtest.Section(2, wasmtest.Vec(funcImport("env", "f", 0))),
		)
		if _, err := DecodeValidate(mod); err != nil {
			t.Fatalf("numeric returning import should be accepted: %v", err)
		}
	})
	t.Run("v128 function signature accepted", func(t *testing.T) {
		mod := wasmtest.Module(
			wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType([]wasm.ValType{wasm.I32, wasm.V128}, []wasm.ValType{wasm.V128}))),
			wasmtest.Section(2, wasmtest.Vec(funcImport("env", "f", 0))),
		)
		if _, err := DecodeValidate(mod); err != nil {
			t.Fatalf("v128 function import should be accepted: %v", err)
		}
	})
	t.Run("reference param", func(t *testing.T) {
		// (funcref) -> (): reference params are still rejected.
		mod := wasmtest.Module(
			wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType([]wasm.ValType{wasm.FuncRef}, nil))),
			wasmtest.Section(2, wasmtest.Vec(funcImport("env", "f", 0))),
		)
		_, err := DecodeValidate(mod)
		assertErrContains(t, err, "unsupported")
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
		{"f32 first (print_f32)", []wasm.ValType{wasm.F32}},
		{"f64 first (print_f64)", []wasm.ValType{wasm.F64}},
		{"f64 f64 (print_f64_f64)", []wasm.ValType{wasm.F64, wasm.F64}},
		{"i64 first", []wasm.ValType{wasm.I64}},
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
	t.Run("local externref table", func(t *testing.T) {
		mod := wasmtest.Module(wasmtest.Section(4, wasmtest.Vec([]byte{0x6f, 0x00, 0x01})))
		if _, err := DecodeValidate(mod); err != nil {
			t.Fatalf("local externref table should be accepted: %v", err)
		}
		m, err := wasm.DecodeModule(mod)
		if err != nil {
			t.Fatalf("DecodeModule: %v", err)
		}
		feat := AllFeatures()
		feat.ReferenceTypes = false
		if err := RejectUnsupportedWithFeatures(m, feat); err == nil || !strings.Contains(err.Error(), "reference-types disabled") {
			t.Fatalf("disabled local externref table error = %v, want reference-types gate", err)
		}
	})
	t.Run("nullable funcref function signature", func(t *testing.T) {
		mod := wasmtest.Module(wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType([]wasm.ValType{wasm.I32, wasm.FuncRef}, []wasm.ValType{wasm.FuncRef}))))
		if _, err := DecodeValidate(mod); err != nil {
			t.Fatalf("nullable funcref signature should be accepted: %v", err)
		}
	})
	t.Run("funcref signature disabled", func(t *testing.T) {
		mod := wasmtest.Module(wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType([]wasm.ValType{wasm.FuncRef}, nil))))
		m, err := wasm.DecodeModule(mod)
		if err != nil {
			t.Fatalf("DecodeModule: %v", err)
		}
		if err := wasm.ValidateModule(m); err != nil {
			t.Fatalf("ValidateModule: %v", err)
		}
		err = RejectUnsupportedWithFeatures(m, Features{SignExtension: true, BulkMemory: true, SaturatingTrunc: true})
		assertErrContains(t, err, "funcref (reference-types disabled)")
	})
	t.Run("ref.null instruction disabled", func(t *testing.T) {
		mod := wasmtest.Module(
			wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType(nil, nil))),
			wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(0))),
			wasmtest.Section(10, wasmtest.Vec(wasmtest.Code([]byte{0xd0, 0x70, 0x1a, 0x0b}))),
		)
		m, err := wasm.DecodeModule(mod)
		if err != nil {
			t.Fatalf("DecodeModule: %v", err)
		}
		if err := wasm.ValidateModule(m); err != nil {
			t.Fatalf("ValidateModule: %v", err)
		}
		err = RejectUnsupportedWithFeatures(m, Features{SignExtension: true, BulkMemory: true, SaturatingTrunc: true})
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
	t.Run("multiple memories fail validation before support filtering", func(t *testing.T) {
		mod := wasmtest.Module(
			wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType(nil, []wasm.ValType{wasm.I32}))),
			wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(0))),
			wasmtest.Section(5, wasmtest.Vec([]byte{0x00, 0x01}, []byte{0x00, 0x01})),
			wasmtest.Section(10, wasmtest.Vec(wasmtest.Code([]byte{0x3f, 0x00, 0x0b}))),
		)
		_, err := DecodeValidate(mod)
		assertErrContains(t, err, "unsupported feature: multiple memories")
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
			name: "supported passive data drop",
			mod: wasmtest.Module(
				wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType(nil, nil))),
				wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(0))),
				wasmtest.Section(12, wasmtest.ULEB(1)),
				wasmtest.Section(10, wasmtest.Vec(wasmtest.Code([]byte{0xfc, 0x09, 0x00, 0x0b}))),
				wasmtest.Section(11, wasmtest.Vec([]byte{0x01, 0x00})),
			),
		},
		{
			name: "supported simd packed rounding and lane-width conversions",
			mod: wasmtest.Module(
				wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType(nil, nil))),
				wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(0))),
				wasmtest.Section(10, wasmtest.Vec(wasmtest.Code([]byte{
					0xfd, 0x0c, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0,
					0xfd, 103, 0x1a,
					0xfd, 0x0c, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0,
					0xfd, 116, 0x1a,
					0xfd, 0x0c, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0,
					0xfd, 94, 0x1a,
					0xfd, 0x0c, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0,
					0xfd, 95, 0x1a,
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
			name: "supported memory.init",
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
			name: "supported table.copy",
			mod: wasmtest.Module(
				wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType(nil, nil))),
				wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(0))),
				wasmtest.Section(4, wasmtest.Vec([]byte{0x70, 0x00, 0x01})),
				wasmtest.Section(10, wasmtest.Vec(wasmtest.Code([]byte{0x41, 0x00, 0x41, 0x00, 0x41, 0x00, 0xfc, 0x0e, 0x00, 0x00, 0x0b}))),
			),
		},
		{
			name: "supported ref.null",
			mod: wasmtest.Module(
				wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType(nil, nil))),
				wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(0))),
				wasmtest.Section(10, wasmtest.Vec(wasmtest.Code([]byte{0xd0, 0x70, 0x1a, 0x0b}))),
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

func TestDataDropSupportPassEdges(t *testing.T) {
	passiveSegment := func(init ...byte) []byte {
		seg := append([]byte{0x01}, wasmtest.ULEB(uint32(len(init)))...)
		return append(seg, init...)
	}
	activeSegment := append([]byte{0x00, 0x41, 0x00, 0x0b}, wasmtest.Vec()...)

	t.Run("drop second passive segment", func(t *testing.T) {
		mod := wasmtest.Module(
			wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType(nil, nil))),
			wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(0))),
			wasmtest.Section(12, wasmtest.ULEB(2)),
			wasmtest.Section(10, wasmtest.Vec(wasmtest.Code([]byte{0xfc, 0x09, 0x01, 0x0b}))),
			wasmtest.Section(11, wasmtest.Vec(passiveSegment(), passiveSegment('a', 'b', 'c'))),
		)
		if _, err := DecodeValidate(mod); err != nil {
			t.Fatalf("DecodeValidate data.drop index 1: %v", err)
		}
	})

	t.Run("passive data remains gated without data.drop", func(t *testing.T) {
		mod := wasmtest.Module(
			wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType(nil, nil))),
			wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(0))),
			wasmtest.Section(12, wasmtest.ULEB(1)),
			wasmtest.Section(10, wasmtest.Vec(wasmtest.Code([]byte{0x0b}))),
			wasmtest.Section(11, wasmtest.Vec(passiveSegment('x'))),
		)
		m, err := wasm.DecodeModule(mod)
		if err != nil {
			t.Fatalf("DecodeModule: %v", err)
		}
		if err := wasm.ValidateModule(m); err != nil {
			t.Fatalf("ValidateModule: %v", err)
		}
		assertErrContains(t, RejectUnsupportedWithFeatures(m, Features{SignExtension: true, SaturatingTrunc: true}), "unsupported data passive segment")
	})

	t.Run("drop requires data count", func(t *testing.T) {
		mod := wasmtest.Module(
			wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType(nil, nil))),
			wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(0))),
			wasmtest.Section(10, wasmtest.Vec(wasmtest.Code([]byte{0xfc, 0x09, 0x00, 0x0b}))),
		)
		_, err := DecodeValidate(mod)
		assertErrContains(t, err, "decode: wasm decode: invalid module")
	})

	t.Run("drop accepts active segment", func(t *testing.T) {
		mod := wasmtest.Module(
			wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType(nil, nil))),
			wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(0))),
			wasmtest.Section(5, wasmtest.Vec([]byte{0x00, 0x01})),
			wasmtest.Section(12, wasmtest.ULEB(1)),
			wasmtest.Section(10, wasmtest.Vec(wasmtest.Code([]byte{0xfc, 0x09, 0x00, 0x0b}))),
			wasmtest.Section(11, wasmtest.Vec(activeSegment)),
		)
		if _, err := DecodeValidate(mod); err != nil {
			t.Fatalf("DecodeValidate active data.drop: %v", err)
		}
	})
}

func TestSupportPassRawBodyPolicyErrorsKeepInstructionContext(t *testing.T) {
	cases := []struct {
		name string
		feat Features
		body []byte
		want string
	}{
		{"explicit memarg index", AllFeatures(), []byte{0x28, 0x42, 0x00, 0x00, 0x0b}, "unsupported memory explicit index 0 at function 0 instruction 0"},
		{"nonzero memory reserved byte", AllFeatures(), []byte{0x3f, 0x01, 0x0b}, "wasm decode: invalid instruction at offset 1"},
		{"sign extension disabled", Features{BulkMemory: true, SaturatingTrunc: true}, []byte{0xc0, 0x0b}, "unsupported instruction sign-extension-ops disabled at function 0 instruction 0"},
		{"bulk memory disabled", Features{SignExtension: true, SaturatingTrunc: true}, []byte{0xfc, 0x0a, 0x00, 0x00, 0x0b}, "unsupported instruction memory.copy (bulk-memory-operations disabled) at function 0 instruction 0"},
		{"memory.init disabled", Features{SignExtension: true, SaturatingTrunc: true}, []byte{0xfc, 0x08, 0x00, 0x00, 0x0b}, "unsupported instruction memory.init (bulk-memory-operations disabled) at function 0 instruction 0"},
		{"data.drop disabled", Features{SignExtension: true, SaturatingTrunc: true}, []byte{0xfc, 0x09, 0x00, 0x0b}, "unsupported instruction data.drop (bulk-memory-operations disabled) at function 0 instruction 0"},
		{"saturating trunc disabled", Features{SignExtension: true, BulkMemory: true}, []byte{0xfc, 0x00, 0x0b}, "unsupported instruction nontrapping-float-to-int-conversion disabled at function 0 instruction 0"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := (supportPass{feat: tc.feat}).exprBytes(tc.body, "function 0")
			assertErrContains(t, err, tc.want)
		})
	}
}

func TestReferenceTableFeatureGatePolicy(t *testing.T) {
	refOnly := Features{SignExtension: true, SaturatingTrunc: true, ReferenceTypes: true}
	bulkOnly := Features{SignExtension: true, BulkMemory: true, SaturatingTrunc: true}
	cases := []struct {
		name       string
		feat       Features
		body       []byte
		wantErr    string
		wantAccept bool
	}{
		{"nonzero call_indirect needs reference-types", bulkOnly, []byte{0x11, 0x00, 0x01, 0x0b}, "call_indirect table 1 (reference-types disabled)", false},
		{"table.get needs reference-types", bulkOnly, []byte{0x41, 0x00, 0x25, 0x00, 0x1a, 0x0b}, "table.get (reference-types disabled)", false},
		{"table.set needs reference-types", bulkOnly, []byte{0x41, 0x00, 0x42, 0x00, 0x26, 0x00, 0x0b}, "table.set (reference-types disabled)", false},
		{"ref.null needs reference-types", bulkOnly, []byte{0xd0, 0x70, 0x1a, 0x0b}, "RefNull", false},
		{"ref.func needs reference-types", bulkOnly, []byte{0xd2, 0x00, 0x1a, 0x0b}, "RefFunc", false},
		{"ref.is_null needs reference-types", bulkOnly, []byte{0xd1, 0x1a, 0x0b}, "RefIsNull", false},
		{"ref.eq needs reference-types", bulkOnly, []byte{0xd3, 0x1a, 0x0b}, "RefEq", false},
		{"table.init needs bulk-memory", refOnly, []byte{0x41, 0x00, 0x41, 0x00, 0x41, 0x00, 0xfc, 0x0c, 0x00, 0x00, 0x0b}, "table.init (bulk-memory-operations disabled)", false},
		{"elem.drop needs bulk-memory", refOnly, []byte{0xfc, 0x0d, 0x00, 0x0b}, "elem.drop (bulk-memory-operations disabled)", false},
		{"table.copy needs bulk-memory", refOnly, []byte{0x41, 0x00, 0x41, 0x00, 0x41, 0x00, 0xfc, 0x0e, 0x00, 0x00, 0x0b}, "table.copy (bulk-memory-operations disabled)", false},
		{"table.grow is reference-types only", refOnly, []byte{0xd0, 0x70, 0x41, 0x00, 0xfc, 0x0f, 0x00, 0x1a, 0x0b}, "", true},
		{"table.size is reference-types only", refOnly, []byte{0xfc, 0x10, 0x00, 0x1a, 0x0b}, "", true},
		{"table.fill is reference-types only", refOnly, []byte{0x41, 0x00, 0xd0, 0x70, 0x41, 0x00, 0xfc, 0x11, 0x00, 0x0b}, "", true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := (supportPass{feat: tc.feat}).exprBytes(tc.body, "function 0")
			if tc.wantAccept {
				if err != nil {
					t.Fatalf("exprBytes accepted policy case: %v", err)
				}
				return
			}
			assertErrContains(t, err, tc.wantErr)
		})
	}
}

func TestRejectUnsupportedAllowsImportedTableRefEqIdentity(t *testing.T) {
	imp := append(wasmtest.Name("env"), wasmtest.Name("t")...)
	imp = append(imp, 0x01, 0x70, 0x00, 0x01) // import kind table, funcref, min=1
	mod := wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType(nil, []wasm.ValType{wasm.I32}))),
		wasmtest.Section(2, wasmtest.Vec(imp)),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(0))),
		wasmtest.Section(10, wasmtest.Vec(wasmtest.Code([]byte{0xd3, 0x0b}))), // raw ref.eq; support policy only, validation is separate.
	)
	m, err := wasm.DecodeModule(mod)
	if err != nil {
		t.Fatalf("DecodeModule: %v", err)
	}
	if err := RejectUnsupportedWithFeatures(m, AllFeatures()); err != nil {
		t.Fatalf("RejectUnsupportedWithFeatures imported-table ref.eq: %v", err)
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

func TestRejectUnsupportedSIMDExplicitMemargIndex(t *testing.T) {
	v128Const := func() []byte { return append([]byte{0xfd, 0x0c}, make([]byte, 16)...) }
	explicitMemarg := func(sub, align uint32, lane ...byte) []byte {
		body := []byte{0xfd}
		body = append(body, wasmtest.ULEB(sub)...)
		body = append(body, wasmtest.ULEB(64+align)...) // multi-memory memarg: align plus explicit memidx
		body = append(body, 0x00)                       // memidx 0 is still not MVP-style encoding
		body = append(body, 0x00)                       // offset 0
		body = append(body, lane...)
		return body
	}
	cases := []struct {
		name    string
		results []wasm.ValType
		body    []byte
	}{
		{"v128.load", []wasm.ValType{wasm.V128}, append([]byte{0x41, 0x00}, explicitMemarg(0, 4)...)},
		{"v128.store", nil, append(append([]byte{0x41, 0x00}, v128Const()...), explicitMemarg(11, 4)...)},
		{"v128.load16_lane", []wasm.ValType{wasm.V128}, append(append([]byte{0x41, 0x00}, v128Const()...), explicitMemarg(85, 1, 0x00)...)},
		{"v128.store16_lane", nil, append(append([]byte{0x41, 0x00}, v128Const()...), explicitMemarg(89, 1, 0x00)...)},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			body := append(append([]byte(nil), tc.body...), 0x0b)
			mod := wasmtest.Module(
				wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType(nil, tc.results))),
				wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(0))),
				wasmtest.Section(5, wasmtest.Vec([]byte{0x00, 0x01})),
				wasmtest.Section(10, wasmtest.Vec(wasmtest.Code(body))),
			)
			_, err := DecodeValidate(mod)
			assertErrContains(t, err, "unsupported memory explicit index 0")
		})
	}
}

func TestDecodeValidateAcceptsSIMDMVPMemarg(t *testing.T) {
	mod := wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType(nil, []wasm.ValType{wasm.V128}))),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(0))),
		wasmtest.Section(5, wasmtest.Vec([]byte{0x00, 0x01})),
		wasmtest.Section(10, wasmtest.Vec(wasmtest.Code([]byte{0x41, 0x00, 0xfd, 0x00, 0x04, 0x00, 0x0b}))),
	)
	if _, err := DecodeValidate(mod); err != nil {
		t.Fatalf("DecodeValidate MVP-style v128.load memarg: %v", err)
	}
}

func TestDecodeValidateAcceptsSupportedSIMDSwizzleTranche(t *testing.T) {
	v128Const := func() []byte {
		return append([]byte{0xfd, 0x0c}, make([]byte, 16)...)
	}
	body := v128Const()
	body = append(body, v128Const()...)
	body = append(body, 0xfd, 0x0e, 0x0b) // i8x16.swizzle; end
	mod := wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType(nil, []wasm.ValType{wasm.V128}))),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(0))),
		wasmtest.Section(10, wasmtest.Vec(wasmtest.Code(body))),
	)
	if _, err := DecodeValidate(mod); err != nil {
		t.Fatalf("DecodeValidate: %v", err)
	}
}

func TestDecodeValidateAcceptsSupportedRelaxedSIMDSwizzleTranche(t *testing.T) {
	v128Const := func() []byte {
		return append([]byte{0xfd, 0x0c}, make([]byte, 16)...)
	}
	body := v128Const()
	body = append(body, v128Const()...)
	body = append(body, 0xfd)
	body = append(body, wasmtest.ULEB(256)...)
	body = append(body, 0x0b) // i8x16.relaxed_swizzle; end
	mod := wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType(nil, []wasm.ValType{wasm.V128}))),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(0))),
		wasmtest.Section(10, wasmtest.Vec(wasmtest.Code(body))),
	)
	if _, err := DecodeValidate(mod); err != nil {
		t.Fatalf("DecodeValidate: %v", err)
	}
}

func TestDecodeValidateAcceptsSupportedRelaxedSIMDLaneSelectTranche(t *testing.T) {
	v128Const := func() []byte {
		return append([]byte{0xfd, 0x0c}, make([]byte, 16)...)
	}
	for _, sub := range []uint32{265, 266, 267, 268} {
		body := v128Const()
		body = append(body, v128Const()...)
		body = append(body, v128Const()...)
		body = append(body, 0xfd)
		body = append(body, wasmtest.ULEB(sub)...)
		body = append(body, 0x0b)
		mod := wasmtest.Module(
			wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType(nil, []wasm.ValType{wasm.V128}))),
			wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(0))),
			wasmtest.Section(10, wasmtest.Vec(wasmtest.Code(body))),
		)
		if _, err := DecodeValidate(mod); err != nil {
			t.Fatalf("DecodeValidate relaxed_laneselect opcode %d: %v", sub, err)
		}
	}
}

func TestDecodeValidateAcceptsSupportedRelaxedSIMDPackedFloatMinMaxTranche(t *testing.T) {
	v128Const := func() []byte {
		return append([]byte{0xfd, 0x0c}, make([]byte, 16)...)
	}
	for _, sub := range []uint32{269, 270, 271, 272} {
		body := v128Const()
		body = append(body, v128Const()...)
		body = append(body, 0xfd)
		body = append(body, wasmtest.ULEB(sub)...)
		body = append(body, 0x0b)
		mod := wasmtest.Module(
			wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType(nil, []wasm.ValType{wasm.V128}))),
			wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(0))),
			wasmtest.Section(10, wasmtest.Vec(wasmtest.Code(body))),
		)
		if _, err := DecodeValidate(mod); err != nil {
			t.Fatalf("DecodeValidate relaxed float min/max opcode %d: %v", sub, err)
		}
	}
}

func TestDecodeValidateAcceptsSupportedRelaxedSIMDQ15mulrTranche(t *testing.T) {
	v128Const := func() []byte {
		return append([]byte{0xfd, 0x0c}, make([]byte, 16)...)
	}
	body := v128Const()
	body = append(body, v128Const()...)
	body = append(body, 0xfd)
	body = append(body, wasmtest.ULEB(273)...)
	body = append(body, 0x0b)
	mod := wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType(nil, []wasm.ValType{wasm.V128}))),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(0))),
		wasmtest.Section(10, wasmtest.Vec(wasmtest.Code(body))),
	)
	if _, err := DecodeValidate(mod); err != nil {
		t.Fatalf("DecodeValidate: %v", err)
	}
}

func TestDecodeValidateAcceptsSupportedRelaxedSIMDDotProductTranche(t *testing.T) {
	v128Const := func() []byte {
		return append([]byte{0xfd, 0x0c}, make([]byte, 16)...)
	}
	cases := []struct {
		name  string
		sub   uint32
		arity int
	}{
		{"i16x8.relaxed_dot_i8x16_i7x16_s", 274, 2},
		{"i32x4.relaxed_dot_i8x16_i7x16_add_s", 275, 3},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			body := v128Const()
			for i := 1; i < tc.arity; i++ {
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

func TestDecodeValidateAcceptsSupportedSIMDShuffleTranche(t *testing.T) {
	v128Const := func() []byte {
		return append([]byte{0xfd, 0x0c}, make([]byte, 16)...)
	}
	body := v128Const()
	body = append(body, v128Const()...)
	body = append(body, 0xfd, 0x0d)
	body = append(body, 0, 16, 1, 17, 15, 31, 8, 24, 4, 20, 5, 21, 7, 23, 10, 26)
	body = append(body, 0x0b)
	mod := wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType(nil, []wasm.ValType{wasm.V128}))),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(0))),
		wasmtest.Section(10, wasmtest.Vec(wasmtest.Code(body))),
	)
	if _, err := DecodeValidate(mod); err != nil {
		t.Fatalf("DecodeValidate: %v", err)
	}
}

func TestDecodeValidateAcceptsSupportedSIMDLoadExtendTranche(t *testing.T) {
	cases := []struct {
		name string
		sub  uint32
	}{
		{"v128.load8x8_s", 1},
		{"v128.load8x8_u", 2},
		{"v128.load16x4_s", 3},
		{"v128.load16x4_u", 4},
		{"v128.load32x2_s", 5},
		{"v128.load32x2_u", 6},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			body := []byte{0x41, 0x00, 0xfd}
			body = append(body, wasmtest.ULEB(tc.sub)...)
			body = append(body, 0x03, 0x00, 0x0b) // align=8, offset=0, end
			mod := wasmtest.Module(
				wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType(nil, []wasm.ValType{wasm.V128}))),
				wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(0))),
				wasmtest.Section(5, wasmtest.Vec([]byte{0x00, 0x01})),
				wasmtest.Section(10, wasmtest.Vec(wasmtest.Code(body))),
			)
			if _, err := DecodeValidate(mod); err != nil {
				t.Fatalf("DecodeValidate: %v", err)
			}
		})
	}
}

func TestDecodeValidateAcceptsSupportedSIMDLoadSplatTranche(t *testing.T) {
	cases := []struct {
		name  string
		sub   uint32
		align uint32
	}{
		{"v128.load8_splat", 7, 0},
		{"v128.load16_splat", 8, 1},
		{"v128.load32_splat", 9, 2},
		{"v128.load64_splat", 10, 3},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			body := []byte{0x41, 0x00, 0xfd}
			body = append(body, wasmtest.ULEB(tc.sub)...)
			body = append(body, wasmtest.ULEB(tc.align)...)
			body = append(body, 0x00, 0x0b) // offset=0; end
			mod := wasmtest.Module(
				wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType(nil, []wasm.ValType{wasm.V128}))),
				wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(0))),
				wasmtest.Section(5, wasmtest.Vec([]byte{0x00, 0x01})),
				wasmtest.Section(10, wasmtest.Vec(wasmtest.Code(body))),
			)
			if _, err := DecodeValidate(mod); err != nil {
				t.Fatalf("DecodeValidate: %v", err)
			}
		})
	}
}

func TestDecodeValidateAcceptsSupportedSIMDLoadZeroTranche(t *testing.T) {
	cases := []struct {
		name  string
		sub   uint32
		align uint32
	}{
		{"v128.load32_zero", 92, 2},
		{"v128.load64_zero", 93, 3},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			body := []byte{0x41, 0x00, 0xfd}
			body = append(body, wasmtest.ULEB(tc.sub)...)
			body = append(body, wasmtest.ULEB(tc.align)...)
			body = append(body, 0x00, 0x0b) // offset=0; end
			mod := wasmtest.Module(
				wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType(nil, []wasm.ValType{wasm.V128}))),
				wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(0))),
				wasmtest.Section(5, wasmtest.Vec([]byte{0x00, 0x01})),
				wasmtest.Section(10, wasmtest.Vec(wasmtest.Code(body))),
			)
			if _, err := DecodeValidate(mod); err != nil {
				t.Fatalf("DecodeValidate: %v", err)
			}
		})
	}
}

func TestDecodeValidateAcceptsSupportedSIMDLaneMemoryTranche(t *testing.T) {
	laneMemarg := func(sub uint32, align, off uint32, lane byte) []byte {
		body := []byte{0xfd}
		body = append(body, wasmtest.ULEB(sub)...)
		body = append(body, wasmtest.ULEB(align)...)
		body = append(body, wasmtest.ULEB(off)...)
		body = append(body, lane)
		return body
	}
	cases := []struct {
		name    string
		sub     uint32
		align   uint32
		lane    byte
		results []wasm.ValType
	}{
		{"v128.load8_lane", 84, 0, 15, []wasm.ValType{wasm.V128}},
		{"v128.load16_lane", 85, 1, 7, []wasm.ValType{wasm.V128}},
		{"v128.load32_lane", 86, 2, 3, []wasm.ValType{wasm.V128}},
		{"v128.load64_lane", 87, 3, 1, []wasm.ValType{wasm.V128}},
		{"v128.store8_lane", 88, 0, 15, nil},
		{"v128.store16_lane", 89, 1, 7, nil},
		{"v128.store32_lane", 90, 2, 3, nil},
		{"v128.store64_lane", 91, 3, 1, nil},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			body := []byte{0x41, 0x00}
			body = append(body, append([]byte{0xfd, 0x0c}, make([]byte, 16)...)...)
			body = append(body, laneMemarg(tc.sub, tc.align, 0, tc.lane)...)
			body = append(body, 0x0b)
			mod := wasmtest.Module(
				wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType(nil, tc.results))),
				wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(0))),
				wasmtest.Section(5, wasmtest.Vec([]byte{0x00, 0x01})),
				wasmtest.Section(10, wasmtest.Vec(wasmtest.Code(body))),
			)
			if _, err := DecodeValidate(mod); err != nil {
				t.Fatalf("DecodeValidate: %v", err)
			}
		})
	}
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
		{"i8x16.narrow_i16x8_s", 101}, {"i8x16.narrow_i16x8_u", 102},
		{"i16x8.eq", 45}, {"i16x8.ne", 46}, {"i16x8.lt_s", 47}, {"i16x8.lt_u", 48}, {"i16x8.gt_s", 49}, {"i16x8.gt_u", 50}, {"i16x8.le_s", 51}, {"i16x8.le_u", 52}, {"i16x8.ge_s", 53}, {"i16x8.ge_u", 54},
		{"i32x4.eq", 55}, {"i32x4.ne", 56}, {"i32x4.lt_s", 57}, {"i32x4.lt_u", 58}, {"i32x4.gt_s", 59}, {"i32x4.gt_u", 60}, {"i32x4.le_s", 61}, {"i32x4.le_u", 62}, {"i32x4.ge_s", 63}, {"i32x4.ge_u", 64},
		{"i8x16.add", 110}, {"i8x16.add_sat_s", 111}, {"i8x16.add_sat_u", 112}, {"i8x16.sub", 113}, {"i8x16.sub_sat_s", 114}, {"i8x16.sub_sat_u", 115}, {"i8x16.min_s", 118}, {"i8x16.min_u", 119}, {"i8x16.max_s", 120}, {"i8x16.max_u", 121}, {"i8x16.avgr_u", 123},
		{"i16x8.q15mulr_sat_s", 130}, {"i16x8.narrow_i32x4_s", 133}, {"i16x8.narrow_i32x4_u", 134}, {"i16x8.add", 142}, {"i16x8.add_sat_s", 143}, {"i16x8.add_sat_u", 144}, {"i16x8.sub", 145}, {"i16x8.sub_sat_s", 146}, {"i16x8.sub_sat_u", 147}, {"i16x8.mul", 149}, {"i16x8.min_s", 150}, {"i16x8.min_u", 151}, {"i16x8.max_s", 152}, {"i16x8.max_u", 153}, {"i16x8.avgr_u", 155}, {"i16x8.extmul_low_i8x16_s", 156}, {"i16x8.extmul_high_i8x16_s", 157}, {"i16x8.extmul_low_i8x16_u", 158}, {"i16x8.extmul_high_i8x16_u", 159},
		{"i32x4.add", 174}, {"i32x4.sub", 177}, {"i32x4.mul", 181}, {"i32x4.min_s", 182}, {"i32x4.min_u", 183}, {"i32x4.max_s", 184}, {"i32x4.max_u", 185}, {"i32x4.dot_i16x8_s", 186}, {"i32x4.extmul_low_i16x8_s", 188}, {"i32x4.extmul_high_i16x8_s", 189}, {"i32x4.extmul_low_i16x8_u", 190}, {"i32x4.extmul_high_i16x8_u", 191},
		{"i64x2.add", 206}, {"i64x2.sub", 209}, {"i64x2.eq", 214}, {"i64x2.ne", 215}, {"i64x2.lt_s", 216}, {"i64x2.gt_s", 217}, {"i64x2.le_s", 218}, {"i64x2.ge_s", 219}, {"i64x2.extmul_low_i32x4_s", 220}, {"i64x2.extmul_high_i32x4_s", 221}, {"i64x2.extmul_low_i32x4_u", 222}, {"i64x2.extmul_high_i32x4_u", 223},
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

func TestDecodeValidateAcceptsSupportedSIMDShiftTranche(t *testing.T) {
	v128Const := func() []byte {
		return append([]byte{0xfd, 0x0c}, make([]byte, 16)...)
	}
	cases := []struct {
		name string
		sub  uint32
	}{
		{"i8x16.shl", 107}, {"i8x16.shr_s", 108}, {"i8x16.shr_u", 109},
		{"i16x8.shl", 139}, {"i16x8.shr_s", 140}, {"i16x8.shr_u", 141},
		{"i32x4.shl", 171}, {"i32x4.shr_s", 172}, {"i32x4.shr_u", 173},
		{"i64x2.shl", 203}, {"i64x2.shr_s", 204}, {"i64x2.shr_u", 205},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			body := v128Const()
			body = append(body, 0x41, 0x01, 0xfd)
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
		{"i16x8.extadd_pairwise_i8x16_s", 124}, {"i16x8.extadd_pairwise_i8x16_u", 125},
		{"i32x4.extadd_pairwise_i16x8_s", 126}, {"i32x4.extadd_pairwise_i16x8_u", 127},
		{"i16x8.abs", 128}, {"i16x8.neg", 129},
		{"i16x8.extend_low_i8x16_s", 135}, {"i16x8.extend_high_i8x16_s", 136}, {"i16x8.extend_low_i8x16_u", 137}, {"i16x8.extend_high_i8x16_u", 138},
		{"i32x4.abs", 160}, {"i32x4.neg", 161},
		{"i32x4.extend_low_i16x8_s", 167}, {"i32x4.extend_high_i16x8_s", 168}, {"i32x4.extend_low_i16x8_u", 169}, {"i32x4.extend_high_i16x8_u", 170},
		{"i64x2.abs", 192}, {"i64x2.neg", 193},
		{"i64x2.extend_low_i32x4_s", 199}, {"i64x2.extend_high_i32x4_s", 200}, {"i64x2.extend_low_i32x4_u", 201}, {"i64x2.extend_high_i32x4_u", 202},
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
		{"f32x4.min", 232, false}, {"f32x4.max", 233, false}, {"f32x4.pmin", 234, false}, {"f32x4.pmax", 235, false},
		{"f64x2.abs", 236, true}, {"f64x2.neg", 237, true}, {"f64x2.sqrt", 239, true},
		{"f64x2.add", 240, false}, {"f64x2.sub", 241, false}, {"f64x2.mul", 242, false}, {"f64x2.div", 243, false},
		{"f64x2.min", 244, false}, {"f64x2.max", 245, false}, {"f64x2.pmin", 246, false}, {"f64x2.pmax", 247, false},
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

func TestSupportedSIMDInstructionsMatchValidator(t *testing.T) {
	validator := wasm.SIMDValidationInstructionKinds()
	seen := make(map[wasm.InstrKind]struct{}, len(validator))
	for sub := uint32(0); sub < 512; sub++ {
		immBytes := append(wasmtest.ULEB(sub), make([]byte, 32)...)
		imm, err := wasm.ClassifyInstructionImmediate(wasm.NewReader(immBytes), 0xfd)
		if err != nil {
			continue
		}
		kind := simdClassifiedKind(sub, imm)
		if kind == wasm.InstrInvalid {
			continue
		}
		frontendOK := supportedSIMDInstruction(imm)
		_, validatorOK := validator[kind]
		if frontendOK != validatorOK {
			t.Fatalf("0xfd subopcode %d (%s): frontend supported=%v, validator admits=%v", sub, kind, frontendOK, validatorOK)
		}
		if frontendOK {
			seen[kind] = struct{}{}
		}
	}
	for kind := range validator {
		if _, ok := seen[kind]; !ok {
			t.Fatalf("validator admits %s, but no frontend-supported 0xfd opcode classified to it", kind)
		}
	}
}

func TestDecodedSIMDOpcodeCoverage(t *testing.T) {
	// The current core SIMD + relaxed SIMD 0xfd table ends at relaxed dot-product
	// opcode 275. The only invalid opcodes below that maximum are reserved holes
	// in the proposal table; every other decoded opcode must be admitted by both
	// the validator and the public frontend support gate.
	reservedHoles := map[uint32]struct{}{
		154: {}, 162: {}, 165: {}, 166: {}, 175: {}, 176: {}, 178: {}, 179: {}, 180: {}, 187: {},
		194: {}, 197: {}, 198: {}, 207: {}, 208: {}, 210: {}, 211: {}, 212: {}, 226: {}, 238: {},
	}
	validator := wasm.SIMDValidationInstructionKinds()
	decoded := 0
	for sub := uint32(0); sub <= 275; sub++ {
		immBytes := append(wasmtest.ULEB(sub), make([]byte, 32)...)
		imm, err := wasm.ClassifyInstructionImmediate(wasm.NewReader(immBytes), 0xfd)
		if _, hole := reservedHoles[sub]; hole {
			if err == nil {
				t.Fatalf("0xfd reserved hole %d decoded as %s", sub, simdClassifiedKind(sub, imm))
			}
			continue
		}
		if err != nil {
			t.Fatalf("0xfd subopcode %d should decode; got %v", sub, err)
		}
		decoded++
		kind := simdClassifiedKind(sub, imm)
		if _, ok := validator[kind]; !ok {
			t.Fatalf("0xfd subopcode %d (%s) decodes but validator does not admit it", sub, kind)
		}
		if !supportedSIMDInstruction(imm) {
			t.Fatalf("0xfd subopcode %d (%s) decodes but frontend rejects it", sub, kind)
		}
	}
	if decoded != 256 {
		t.Fatalf("decoded SIMD opcode count = %d, want 256", decoded)
	}
	for sub := uint32(276); sub < 512; sub++ {
		immBytes := append(wasmtest.ULEB(sub), make([]byte, 32)...)
		if imm, err := wasm.ClassifyInstructionImmediate(wasm.NewReader(immBytes), 0xfd); err == nil {
			t.Fatalf("0xfd subopcode %d unexpectedly decoded as %s", sub, simdClassifiedKind(sub, imm))
		}
	}
}

func simdClassifiedKind(sub uint32, imm wasm.InstructionImmediate) wasm.InstrKind {
	switch sub {
	case 12:
		return wasm.InstrV128Const
	case 13:
		return wasm.InstrI8x16Shuffle
	default:
		return imm.Kind
	}
}

func TestRejectUnsupportedProposalFeaturesDecodedByWasm3(t *testing.T) {
	t.Run("memory64", func(t *testing.T) {
		mod := wasmtest.Module(wasmtest.Section(5, wasmtest.Vec([]byte{0x04, 0x00}))) // memory64 min 0
		_, err := DecodeValidate(mod)
		assertErrContains(t, err, "unsupported memory memory64 at memory 0")
	})
	t.Run("invalid simd instruction", func(t *testing.T) {
		mod := wasmtest.Module(
			wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType(nil, nil))),
			wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(0))),
			wasmtest.Section(10, wasmtest.Vec(wasmtest.Code([]byte{0xfd, 226, 0x0b}))),
		)
		_, err := DecodeValidate(mod)
		assertErrContains(t, err, "decode: invalid instruction")
	})
}

func assertErrContains(t *testing.T, err error, want string) {
	t.Helper()
	if err == nil || !strings.Contains(err.Error(), want) {
		t.Fatalf("error = %v, want substring %q", err, want)
	}
}
