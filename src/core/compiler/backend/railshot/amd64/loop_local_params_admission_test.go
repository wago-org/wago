//go:build (linux || darwin || windows) && amd64

package amd64

import (
	"github.com/wago-org/wago/src/core/compiler/wasm"
	"testing"
)

func TestLoopLocalParametersDefaultAdmission(t *testing.T) {
	requireCompilerDiagnostics(t)
	m := modFuncs(t, funcDef{[]wasm.ValType{wasm.I32, wasm.I32}, []wasm.ValType{wasm.I32}, []byte{0, 0x20, 0, 0x20, 1, 0x03, 0, 0x21, 1, 0x21, 0, 0x20, 0, 0x45, 0x04, 0x7f, 0x20, 1, 0x05, 0x20, 0, 0x41, 1, 0x6b, 0x20, 1, 0x41, 3, 0x6a, 0x0c, 1, 0x0b, 0x0b, 0x0b}})
	var stats ModuleStats
	got, _, err := runMemAmd64WithOptions(t, m, CompileOptions{Workers: 1, Stats: &stats}, nil, 7, 11)
	if err != nil || got != 32 {
		t.Fatalf("got=%d err=%v", got, err)
	}
	if !profileEnabled && stats.Funcs[0].Peephole["loop-local-params"] == 0 {
		t.Fatal("loop parameters still use canonical stack transfers")
	}
}
