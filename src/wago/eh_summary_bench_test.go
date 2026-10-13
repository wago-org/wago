//go:build ((linux && amd64) || ((linux || darwin) && arm64)) && !tinygo && !wago_guardpage && !wago_precompiled

package wago

import (
	"fmt"
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
	"github.com/wago-org/wago/tests/support/wasmtest"
)

func compileExceptionSummaryModule(n int) []byte {
	imports := make([][]byte, 0, n+1)
	for i := 0; i < n; i++ {
		entry := append(wasmtest.Name("env"), wasmtest.Name(fmt.Sprintf("tag%d", i))...)
		imports = append(imports, append(entry, byte(wasm.ExternTag), 0, 0))
	}
	entry := append(wasmtest.Name("env"), wasmtest.Name("observe")...)
	imports = append(imports, append(entry, byte(wasm.ExternFunc), 1))
	funcs, bodies := make([][]byte, n), make([][]byte, n)
	// The collector-reference host call requires exact native root planning.
	body := []byte{0x02, 0x69, 0x1f, 0x40, 0x01, byte(wasm.CatchAllRef), 0x00,
		0x20, 0x00, 0x10, 0x00, 0x0b, 0xd0, 0x69, 0x0b, 0x1a, 0x0b}
	for i := range funcs {
		funcs[i], bodies[i] = []byte{1}, wasmtest.Code(body)
	}
	return wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(
			wasmtest.FuncType([]wasm.ValType{wasm.I32}, nil),
			wasmtest.FuncType([]wasm.ValType{wasm.AnyRef}, nil),
		)),
		wasmtest.Section(2, wasmtest.Vec(imports...)),
		wasmtest.Section(3, wasmtest.Vec(funcs...)),
		wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("run", 0, 1))),
		wasmtest.Section(10, wasmtest.Vec(bodies...)),
	)
}

func checkExceptionSummaryRootAdmission(t testing.TB, compiled *Compiled, functions int) {
	t.Helper()
	status := compiled.GCNativeRootAdmission()
	if !status.Required || !status.Exact || status.Callsites < uint32(functions) || status.MaximumRoots < 8 {
		t.Fatalf("exception summary fixture did not obtain exact EH roots: %+v", status)
	}
}

func TestCompileExceptionSummaryExactRoots(t *testing.T) {
	compiled, err := Compile(NewRuntimeConfig().WithCoreFeatures(CoreFeaturesV3), compileExceptionSummaryModule(3))
	if err != nil {
		t.Fatal(err)
	}
	defer compiled.Close()
	checkExceptionSummaryRootAdmission(t, compiled, 3)
}

func BenchmarkCompileExceptionModuleSummary(b *testing.B) {
	for _, functions := range []int{1, 1000} {
		b.Run(fmt.Sprint(functions), func(b *testing.B) {
			source := compileExceptionSummaryModule(functions)
			cfg := NewRuntimeConfig().WithCoreFeatures(CoreFeaturesV3)
			compiled, err := Compile(cfg, source)
			if err != nil {
				b.Fatal(err)
			}
			checkExceptionSummaryRootAdmission(b, compiled, functions)
			if err := compiled.Close(); err != nil {
				b.Fatal(err)
			}
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				compiled, err := Compile(cfg, source)
				if err != nil {
					b.Fatal(err)
				}
				if err := compiled.Close(); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}
