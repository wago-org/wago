//go:build amd64

package amd64

import (
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
	"github.com/wago-org/wago/tests/wasmtest"
)

func referenceBooleanFactModuleAMD64(t testing.TB) *wasm.Module {
	t.Helper()
	body := []byte{0x01, 0x01, 0x7e}
	for range 8 {
		body = append(body,
			0x20, 0x02, 0x20, 0x00, 0xd1, 0xad, 0x7c, 0x21, 0x02,
			0x20, 0x02, 0x20, 0x00, 0x20, 0x01, 0xd3, 0xad, 0x7c, 0x21, 0x02,
		)
	}
	body = append(body, 0x20, 0x02, 0x0b)
	return decodeReferenceWidthModuleAMD64(t,
		wasmtest.FuncType([]wasm.ValType{wasm.EqRef, wasm.EqRef}, []wasm.ValType{wasm.I64}), body)
}

func i31UpperFactModuleAMD64(t testing.TB) *wasm.Module {
	t.Helper()
	body := []byte{0x01, 0x01, 0x7e}
	for range 8 {
		body = append(body,
			0x20, 0x01, 0x20, 0x00, 0xfb, 0x1d, 0xad, 0x7c, 0x21, 0x01,
			0x20, 0x01, 0x20, 0x00, 0xfb, 0x1e, 0xad, 0x7c, 0x21, 0x01,
		)
	}
	body = append(body, 0x20, 0x01, 0x0b)
	return decodeReferenceWidthModuleAMD64(t,
		wasmtest.FuncType([]wasm.ValType{wasm.I31Ref}, []wasm.ValType{wasm.I64}), body)
}

func decodeReferenceWidthModuleAMD64(t testing.TB, funcType, body []byte) *wasm.Module {
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

func TestReferenceWidthFactsAMD64(t *testing.T) {
	for _, tc := range []struct {
		name string
		want int
		mod  func(testing.TB) *wasm.Module
	}{
		{name: "boolean", want: 16, mod: referenceBooleanFactModuleAMD64},
		{name: "i31_get", want: 16, mod: i31UpperFactModuleAMD64},
	} {
		t.Run(tc.name, func(t *testing.T) {
			compile := func(enabled bool) (*CodegenStats, int) {
				var stats ModuleStats
				compiled, err := CompileModuleWith(tc.mod(t), CompileOptions{
					Stats:         &stats,
					Optimizations: map[string]bool{"value-facts": enabled},
				})
				if err != nil {
					t.Fatal(err)
				}
				defer compiled.CodeImage.Close()
				return stats.Funcs[0], len(compiled.Code)
			}
			on, onBytes := compile(true)
			off, offBytes := compile(false)
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
}

func BenchmarkReferenceWidthFactsCompileAMD64(b *testing.B) {
	for _, fixture := range []struct {
		name string
		mod  *wasm.Module
	}{
		{name: "boolean", mod: referenceBooleanFactModuleAMD64(b)},
		{name: "i31_get", mod: i31UpperFactModuleAMD64(b)},
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
