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
