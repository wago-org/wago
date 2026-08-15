//go:build amd64

package amd64

import (
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
	"github.com/wago-org/wago/tests/wasmtest"
)

func referenceTypeTestBooleanFactModuleAMD64(t testing.TB) *wasm.Module {
	t.Helper()
	body := []byte{0x01, 0x01, 0x7e}
	for range 16 {
		body = append(body,
			0x20, 0x01, 0x20, 0x00,
			0xfb, 0x14, 0x70, // ref.test func
			0xad, 0x7c, 0x21, 0x01,
		)
	}
	body = append(body, 0x20, 0x01, 0x0b)
	return decodeReferenceMergeModuleAMD64(t,
		wasmtest.FuncType([]wasm.ValType{wasm.FuncRef}, []wasm.ValType{wasm.I64}), body)
}

func selectBooleanFactModuleAMD64(t testing.TB, bothKnown bool) *wasm.Module {
	t.Helper()
	body := []byte{0x01, 0x01, 0x7e}
	for range 8 {
		body = append(body,
			0x20, 0x04, // local.get accumulator
			0x20, 0x00, 0x20, 0x01, 0xd3, // ref.eq
		)
		if bothKnown {
			body = append(body, 0x20, 0x00, 0xd1) // ref.is_null
		} else {
			body = append(body, 0x20, 0x03) // unknown i32 candidate
		}
		body = append(body,
			0x20, 0x02, // select condition
			0x1b, // select
			0xad, 0x7c, 0x21, 0x04,
		)
	}
	body = append(body, 0x20, 0x04, 0x0b)
	return decodeReferenceMergeModuleAMD64(t,
		wasmtest.FuncType([]wasm.ValType{wasm.EqRef, wasm.EqRef, wasm.I32, wasm.I32}, []wasm.ValType{wasm.I64}), body)
}

func decodeReferenceMergeModuleAMD64(t testing.TB, funcType, body []byte) *wasm.Module {
	t.Helper()
	data := wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(funcType)),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(0))),
		wasmtest.Section(10, wasmtest.Vec(append(wasmtest.ULEB(uint32(len(body))), body...))),
	)
	m, err := wasm.DecodeModule(data)
	if err != nil {
		t.Fatal(err)
	}
	if err := wasm.ValidateModule(m); err != nil {
		t.Fatal(err)
	}
	return m
}

func TestReferenceMergeFactsAMD64(t *testing.T) {
	for _, tc := range []struct {
		name string
		want int
		mod  func(testing.TB) *wasm.Module
	}{
		{name: "ref_test", want: 16, mod: referenceTypeTestBooleanFactModuleAMD64},
		{name: "select", want: 8, mod: func(t testing.TB) *wasm.Module { return selectBooleanFactModuleAMD64(t, true) }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			compile := func(m *wasm.Module, enabled bool) (*CodegenStats, int) {
				var stats ModuleStats
				compiled, err := CompileModuleWith(m, CompileOptions{
					Stats:         &stats,
					Optimizations: map[string]bool{"value-facts": enabled},
				})
				if err != nil {
					t.Fatal(err)
				}
				defer compiled.CodeImage.Close()
				return stats.Funcs[0], len(compiled.Code)
			}
			m := tc.mod(t)
			on, onBytes := compile(m, true)
			off, offBytes := compile(m, false)
			if got := on.Peephole["ext-elim"]; got != tc.want {
				t.Fatalf("extension elisions = %d, want %d (all: %v)", got, tc.want, on.Peephole)
			}
			if got := off.Peephole["ext-elim"]; got != 0 {
				t.Fatalf("disabled extension elisions = %d, want 0", got)
			}
			if onBytes >= offBytes {
				t.Fatalf("native bytes enabled/disabled = %d/%d, want reduction", onBytes, offBytes)
			}
		})
	}

	var stats ModuleStats
	compiled, err := CompileModuleWith(selectBooleanFactModuleAMD64(t, false), CompileOptions{Stats: &stats})
	if err != nil {
		t.Fatal(err)
	}
	compiled.CodeImage.Close()
	if got := stats.Funcs[0].Peephole["ext-elim"]; got != 0 {
		t.Fatalf("select near-miss extension elisions = %d, want 0", got)
	}
}

func BenchmarkReferenceMergeFactsCompileAMD64(b *testing.B) {
	for _, fixture := range []struct {
		name string
		mod  *wasm.Module
	}{
		{name: "ref_test", mod: referenceTypeTestBooleanFactModuleAMD64(b)},
		{name: "select", mod: selectBooleanFactModuleAMD64(b, true)},
	} {
		b.Run(fixture.name, func(b *testing.B) {
			for _, enabled := range []bool{false, true} {
				name := "off"
				if enabled {
					name = "on"
				}
				b.Run(name, func(b *testing.B) {
					opts := CompileOptions{Optimizations: map[string]bool{"value-facts": enabled}}
					compiled, err := CompileModuleWith(fixture.mod, opts)
					if err != nil {
						b.Fatal(err)
					}
					codeBytes := len(compiled.Code)
					compiled.CodeImage.Close()
					b.ReportAllocs()
					b.ResetTimer()
					for range b.N {
						cm, err := CompileModuleWith(fixture.mod, opts)
						if err != nil {
							b.Fatal(err)
						}
						cm.CodeImage.Close()
					}
					b.ReportMetric(float64(codeBytes), "code-B")
				})
			}
		})
	}
}
