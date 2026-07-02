package ir

import (
	"strings"
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
	"github.com/wago-org/wago/testutil/wasmtest"
)

func TestBuildAllIntegerBinaryOpcodeMappings(t *testing.T) {
	names := []string{"add", "sub", "mul", "div_s", "div_u", "rem_s", "rem_u", "and", "or", "xor", "shl", "shr_s", "shr_u", "rotl", "rotr"}
	for i, name := range names {
		for _, tc := range []struct {
			prefix string
			typ    wasm.ValType
			base   byte
		}{{"i32", wasm.I32, 0x6a}, {"i64", wasm.I64, 0x7c}} {
			t.Run(tc.prefix+"."+name, func(t *testing.T) {
				op := tc.base + byte(i)
				f := buildOpcodeFunc(t, wasm.FuncType{Params: []wasm.ValType{tc.typ, tc.typ}, Results: []wasm.ValType{tc.typ}}, bytes(0x20, 0x00, 0x20, 0x01, op, 0x0b))
				dump := FormatFunc(f)
				if !strings.Contains(dump, "ibinary."+name) {
					t.Fatalf("dump missing ibinary.%s:\n%s", name, dump)
				}
				wantTrap := name == "div_s" || name == "div_u" || name == "rem_s" || name == "rem_u"
				if gotTrap := lastValueProducingInst(f).Effects&EffectCanTrap != 0; gotTrap != wantTrap {
					t.Fatalf("trap effect=%v want %v", gotTrap, wantTrap)
				}
			})
		}
	}
}

func TestBuildAllIntegerCompareOpcodeMappings(t *testing.T) {
	names := []string{"eq", "ne", "lt_s", "lt_u", "gt_s", "gt_u", "le_s", "le_u", "ge_s", "ge_u"}
	for i, name := range names {
		for _, tc := range []struct {
			prefix string
			typ    wasm.ValType
			base   byte
		}{{"i32", wasm.I32, 0x46}, {"i64", wasm.I64, 0x51}} {
			t.Run(tc.prefix+"."+name, func(t *testing.T) {
				f := buildOpcodeFunc(t, wasm.FuncType{Params: []wasm.ValType{tc.typ, tc.typ}, Results: []wasm.ValType{wasm.I32}}, bytes(0x20, 0x00, 0x20, 0x01, tc.base+byte(i), 0x0b))
				if dump := FormatFunc(f); !strings.Contains(dump, "icmp."+name) {
					t.Fatalf("dump missing icmp.%s:\n%s", name, dump)
				}
			})
		}
	}
}

func TestBuildAllIntegerUnaryOpcodeMappings(t *testing.T) {
	names := []string{"clz", "ctz", "popcnt"}
	for i, name := range names {
		for _, tc := range []struct {
			prefix string
			typ    wasm.ValType
			base   byte
		}{{"i32", wasm.I32, 0x67}, {"i64", wasm.I64, 0x79}} {
			t.Run(tc.prefix+"."+name, func(t *testing.T) {
				f := buildOpcodeFunc(t, wasm.FuncType{Params: []wasm.ValType{tc.typ}, Results: []wasm.ValType{tc.typ}}, bytes(0x20, 0x00, tc.base+byte(i), 0x0b))
				if dump := FormatFunc(f); !strings.Contains(dump, "iunary."+name) {
					t.Fatalf("dump missing iunary.%s:\n%s", name, dump)
				}
			})
		}
	}
	for _, tc := range []struct {
		name string
		typ  wasm.ValType
		op   byte
	}{{"extend8_s", wasm.I32, 0xc0}, {"extend16_s", wasm.I32, 0xc1}, {"extend8_s", wasm.I64, 0xc2}, {"extend16_s", wasm.I64, 0xc3}, {"extend32_s", wasm.I64, 0xc4}} {
		t.Run(tc.typ.String()+"."+tc.name, func(t *testing.T) {
			f := buildOpcodeFunc(t, wasm.FuncType{Params: []wasm.ValType{tc.typ}, Results: []wasm.ValType{tc.typ}}, bytes(0x20, 0x00, tc.op, 0x0b))
			if dump := FormatFunc(f); !strings.Contains(dump, "iunary."+tc.name) {
				t.Fatalf("dump missing iunary.%s:\n%s", tc.name, dump)
			}
		})
	}
}

func TestBuildAllFloatOpcodeMappings(t *testing.T) {
	for _, tc := range []struct {
		prefix                   string
		typ                      wasm.ValType
		cmpBase, unBase, binBase byte
	}{{"f32", wasm.F32, 0x5b, 0x8b, 0x92}, {"f64", wasm.F64, 0x61, 0x99, 0xa0}} {
		for i, name := range []string{"eq", "ne", "lt", "gt", "le", "ge"} {
			t.Run(tc.prefix+"."+name, func(t *testing.T) {
				f := buildOpcodeFunc(t, wasm.FuncType{Params: []wasm.ValType{tc.typ, tc.typ}, Results: []wasm.ValType{wasm.I32}}, bytes(0x20, 0x00, 0x20, 0x01, tc.cmpBase+byte(i), 0x0b))
				if dump := FormatFunc(f); !strings.Contains(dump, "fcmp."+name) {
					t.Fatalf("dump missing fcmp.%s:\n%s", name, dump)
				}
			})
		}
		for i, name := range []string{"abs", "neg", "ceil", "floor", "trunc", "nearest", "sqrt"} {
			t.Run(tc.prefix+"."+name, func(t *testing.T) {
				f := buildOpcodeFunc(t, wasm.FuncType{Params: []wasm.ValType{tc.typ}, Results: []wasm.ValType{tc.typ}}, bytes(0x20, 0x00, tc.unBase+byte(i), 0x0b))
				if dump := FormatFunc(f); !strings.Contains(dump, "funary."+name) {
					t.Fatalf("dump missing funary.%s:\n%s", name, dump)
				}
			})
		}
		for i, name := range []string{"add", "sub", "mul", "div", "min", "max", "copysign"} {
			t.Run(tc.prefix+"."+name, func(t *testing.T) {
				f := buildOpcodeFunc(t, wasm.FuncType{Params: []wasm.ValType{tc.typ, tc.typ}, Results: []wasm.ValType{tc.typ}}, bytes(0x20, 0x00, 0x20, 0x01, tc.binBase+byte(i), 0x0b))
				if dump := FormatFunc(f); !strings.Contains(dump, "fbinary."+name) {
					t.Fatalf("dump missing fbinary.%s:\n%s", name, dump)
				}
			})
		}
	}
}

func TestBuildAllConversionOpcodeMappings(t *testing.T) {
	tests := []struct {
		name     string
		src, dst wasm.ValType
		op       byte
		want     string
		traps    bool
	}{
		{"i32.wrap_i64", wasm.I64, wasm.I32, 0xa7, "convert.wrap_i64_i32", false},
		{"i32.trunc_f32_s", wasm.F32, wasm.I32, 0xa8, "convert.trunc_f_i_s", true},
		{"i32.trunc_f32_u", wasm.F32, wasm.I32, 0xa9, "convert.trunc_f_i_u", true},
		{"i32.trunc_f64_s", wasm.F64, wasm.I32, 0xaa, "convert.trunc_f_i_s", true},
		{"i32.trunc_f64_u", wasm.F64, wasm.I32, 0xab, "convert.trunc_f_i_u", true},
		{"i64.extend_i32_s", wasm.I32, wasm.I64, 0xac, "convert.extend_i32_s", false},
		{"i64.extend_i32_u", wasm.I32, wasm.I64, 0xad, "convert.extend_i32_u", false},
		{"i64.trunc_f32_s", wasm.F32, wasm.I64, 0xae, "convert.trunc_f_i_s", true},
		{"i64.trunc_f32_u", wasm.F32, wasm.I64, 0xaf, "convert.trunc_f_i_u", true},
		{"i64.trunc_f64_s", wasm.F64, wasm.I64, 0xb0, "convert.trunc_f_i_s", true},
		{"i64.trunc_f64_u", wasm.F64, wasm.I64, 0xb1, "convert.trunc_f_i_u", true},
		{"f32.convert_i32_s", wasm.I32, wasm.F32, 0xb2, "convert.convert_i_f_s", false},
		{"f32.convert_i32_u", wasm.I32, wasm.F32, 0xb3, "convert.convert_i_f_u", false},
		{"f32.convert_i64_s", wasm.I64, wasm.F32, 0xb4, "convert.convert_i_f_s", false},
		{"f32.convert_i64_u", wasm.I64, wasm.F32, 0xb5, "convert.convert_i_f_u", false},
		{"f32.demote_f64", wasm.F64, wasm.F32, 0xb6, "convert.demote_f64_f32", false},
		{"f64.convert_i32_s", wasm.I32, wasm.F64, 0xb7, "convert.convert_i_f_s", false},
		{"f64.convert_i32_u", wasm.I32, wasm.F64, 0xb8, "convert.convert_i_f_u", false},
		{"f64.convert_i64_s", wasm.I64, wasm.F64, 0xb9, "convert.convert_i_f_s", false},
		{"f64.convert_i64_u", wasm.I64, wasm.F64, 0xba, "convert.convert_i_f_u", false},
		{"f64.promote_f32", wasm.F32, wasm.F64, 0xbb, "convert.promote_f32_f64", false},
		{"i32.reinterpret_f32", wasm.F32, wasm.I32, 0xbc, "reinterpret.f32_to_i32", false},
		{"i64.reinterpret_f64", wasm.F64, wasm.I64, 0xbd, "reinterpret.f64_to_i64", false},
		{"f32.reinterpret_i32", wasm.I32, wasm.F32, 0xbe, "reinterpret.i32_to_f32", false},
		{"f64.reinterpret_i64", wasm.I64, wasm.F64, 0xbf, "reinterpret.i64_to_f64", false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			f := buildOpcodeFunc(t, wasm.FuncType{Params: []wasm.ValType{tc.src}, Results: []wasm.ValType{tc.dst}}, bytes(0x20, 0x00, tc.op, 0x0b))
			if dump := FormatFunc(f); !strings.Contains(dump, tc.want) {
				t.Fatalf("dump missing %q:\n%s", tc.want, dump)
			}
			if got := lastValueProducingInst(f).Effects&EffectCanTrap != 0; got != tc.traps {
				t.Fatalf("trap effect=%v want %v", got, tc.traps)
			}
		})
	}
}

func TestBuildAllSaturatingTruncOpcodeMappings(t *testing.T) {
	tests := []struct {
		sub      byte
		src, dst wasm.ValType
		want     string
	}{
		{0, wasm.F32, wasm.I32, "convert.trunc_sat_f_i_s"}, {1, wasm.F32, wasm.I32, "convert.trunc_sat_f_i_u"}, {2, wasm.F64, wasm.I32, "convert.trunc_sat_f_i_s"}, {3, wasm.F64, wasm.I32, "convert.trunc_sat_f_i_u"},
		{4, wasm.F32, wasm.I64, "convert.trunc_sat_f_i_s"}, {5, wasm.F32, wasm.I64, "convert.trunc_sat_f_i_u"}, {6, wasm.F64, wasm.I64, "convert.trunc_sat_f_i_s"}, {7, wasm.F64, wasm.I64, "convert.trunc_sat_f_i_u"},
	}
	for _, tc := range tests {
		t.Run(tc.want, func(t *testing.T) {
			f := buildOpcodeFunc(t, wasm.FuncType{Params: []wasm.ValType{tc.src}, Results: []wasm.ValType{tc.dst}}, bytes(0x20, 0x00, 0xfc, tc.sub, 0x0b))
			if dump := FormatFunc(f); !strings.Contains(dump, tc.want) {
				t.Fatalf("dump missing %q:\n%s", tc.want, dump)
			}
			if lastValueProducingInst(f).Effects&EffectCanTrap != 0 {
				t.Fatalf("saturating trunc should not be marked trapping")
			}
		})
	}
}

func TestMemoryDescriptorsCoverAllMemOps(t *testing.T) {
	for k := MemI32; k <= MemI64Store32; k++ {
		d, ok := lookupMemDesc(k)
		if !ok {
			t.Fatalf("missing descriptor for %d", k)
		}
		if d.name == "" {
			t.Fatalf("missing memory name for %d", k)
		}
	}
	if got := memName(MemI64Load32U); got != "i64.load32_u" {
		t.Fatalf("memName(MemI64Load32U) = %q", got)
	}
	if got := naturalMemAlign(MemI64Store32); got != 2 {
		t.Fatalf("naturalMemAlign(MemI64Store32) = %d", got)
	}
	if got, ok := memLoadResult(MemI32Load8S); !ok || got != wasm.I32 {
		t.Fatalf("memLoadResult(MemI32Load8S) = %s, %v", got, ok)
	}
	if got, ok := memStoreValue(MemI64Store16); !ok || got != wasm.I64 {
		t.Fatalf("memStoreValue(MemI64Store16) = %s, %v", got, ok)
	}
}

func buildOpcodeFunc(t *testing.T, ft wasm.FuncType, body []byte) *Func {
	t.Helper()
	m := decodeValidate(t, module([]wasm.FuncType{ft}, []uint32{0}, nil, nil, nil, [][]byte{wasmtest.Code(body)}))
	f, err := BuildFunc(m, 0)
	if err != nil {
		t.Fatal(err)
	}
	if err := VerifyFunc(f); err != nil {
		t.Fatal(err)
	}
	return f
}
