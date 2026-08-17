//go:build amd64

package amd64

import (
	"bytes"
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
)

func TestBMI2RorxSelectionAndRequirement(t *testing.T) {
	m := mod1(t, []wasm.ValType{wasm.I32}, []wasm.ValType{wasm.I32},
		[]byte{0x00, 0x20, 0x00, 0x41, 0x07, 0x78, 0x0b})

	off, err := CompileModuleWith(m, CompileOptions{
		Optimizations: map[string]bool{"bmi2-rorx": false},
	})
	if err != nil {
		t.Fatal(err)
	}
	var stats ModuleStats
	on, err := CompileModuleWith(m, CompileOptions{
		Optimizations: map[string]bool{"bmi2-rorx": true},
		Stats:         &stats,
	})
	if err != nil {
		t.Fatal(err)
	}
	if off.RequiresBMI2 {
		t.Fatal("baseline rotate unexpectedly requires BMI2")
	}
	if !on.RequiresBMI2 {
		t.Fatal("RORX code did not propagate its BMI2 requirement")
	}
	if bytes.Equal(off.Code, on.Code) {
		t.Fatal("BMI2 selection did not change generated code")
	}
	if len(stats.Funcs) != 1 || stats.Funcs[0].Peephole["bmi2-rorx"] != 1 {
		t.Fatalf("RORX selection stats = %#v", stats.Funcs)
	}
}

func TestBMI2VariableShiftSelectionAndExecution(t *testing.T) {
	tests := []struct {
		name string
		typ  wasm.ValType
		op   byte
		want func(uint64, uint64) uint64
	}{
		{"i32-shl", wasm.I32, 0x74, func(x, n uint64) uint64 { return uint64(uint32(x) << (n & 31)) }},
		{"i32-shr-s", wasm.I32, 0x75, func(x, n uint64) uint64 { return uint64(uint32(int32(x) >> (n & 31))) }},
		{"i32-shr-u", wasm.I32, 0x76, func(x, n uint64) uint64 { return uint64(uint32(x) >> (n & 31)) }},
		{"i64-shl", wasm.I64, 0x86, func(x, n uint64) uint64 { return x << (n & 63) }},
		{"i64-shr-s", wasm.I64, 0x87, func(x, n uint64) uint64 { return uint64(int64(x) >> (n & 63)) }},
		{"i64-shr-u", wasm.I64, 0x88, func(x, n uint64) uint64 { return x >> (n & 63) }},
	}
	inputs := [][2]uint64{
		{0, 0}, {1, 1}, {0x80000000, 31}, {0x8000000000000000, 63},
		{0x9e3779b97f4a7c15, 65}, {0xffffffffffffffff, 127},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			m := mod1(t, []wasm.ValType{tc.typ, tc.typ}, []wasm.ValType{tc.typ},
				[]byte{0x00, 0x20, 0x00, 0x20, 0x01, tc.op, 0x0b})
			off, err := CompileModuleWith(m, CompileOptions{Optimizations: map[string]bool{"bmi2-shifts": false}})
			if err != nil {
				t.Fatal(err)
			}
			var stats ModuleStats
			on, err := CompileModuleWith(m, CompileOptions{
				Optimizations: map[string]bool{"bmi2-shifts": true},
				Stats:         &stats,
			})
			if err != nil {
				t.Fatal(err)
			}
			if off.RequiresBMI2 || !on.RequiresBMI2 {
				t.Fatalf("BMI2 requirements: off=%v on=%v", off.RequiresBMI2, on.RequiresBMI2)
			}
			if got := stats.Funcs[0].Peephole["bmi2-variable-shift"]; got != 1 {
				t.Fatalf("bmi2-variable-shift = %d, want 1", got)
			}
			if len(on.Code) >= len(off.Code) {
				t.Fatalf("BMI2 code = %d bytes, legacy = %d; want smaller", len(on.Code), len(off.Code))
			}
			for _, in := range inputs {
				want := tc.want(in[0], in[1])
				if got := runCompiledAmd64u(t, on, in[0], in[1]); got != want {
					t.Fatalf("%#x shift %#x = %#x, want %#x", in[0], in[1], got, want)
				}
			}
		})
	}
}

func TestBMI2VariableShiftDoesNotSelectVariableRotate(t *testing.T) {
	m := mod1(t, []wasm.ValType{wasm.I64, wasm.I64}, []wasm.ValType{wasm.I64},
		[]byte{0x00, 0x20, 0x00, 0x20, 0x01, 0x89, 0x0b})
	cm, err := CompileModuleWith(m, CompileOptions{Optimizations: map[string]bool{"bmi2-shifts": true}})
	if err != nil {
		t.Fatal(err)
	}
	if cm.RequiresBMI2 {
		t.Fatal("variable rotate unexpectedly selected a BMI2 instruction")
	}
}
