//go:build arm64

package arm64

import (
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
	"github.com/wago-org/wago/tests/wasmtest"
)

func referenceBooleanFactModuleARM64(t testing.TB) *wasm.Module {
	t.Helper()
	body := []byte{0x01, 0x01, 0x7e} // one i64 accumulator local
	for range 8 {
		body = append(body,
			0x20, 0x02, 0x20, 0x00, 0xd1, 0xad, 0x7c, 0x21, 0x02,
			0x20, 0x02, 0x20, 0x00, 0x20, 0x01, 0xd3, 0xad, 0x7c, 0x21, 0x02,
		)
	}
	body = append(body, 0x20, 0x02, 0x0b)
	data := wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(
			wasmtest.FuncType([]wasm.ValType{wasm.EqRef, wasm.EqRef}, []wasm.ValType{wasm.I64}),
		)),
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

func TestReferenceBooleanFactsARM64(t *testing.T) {
	compile := func(enabled bool) (*CodegenStats, int) {
		var stats ModuleStats
		compiled, err := CompileModuleWith(referenceBooleanFactModuleARM64(t), CompileOptions{
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
	if got := on.Peephole["ext-elim"]; got != 16 {
		t.Fatalf("reference boolean extension elisions = %d, want 16", got)
	}
	if got := off.Peephole["ext-elim"]; got != 0 {
		t.Fatalf("disabled reference boolean extension elisions = %d, want 0", got)
	}
	if onBytes >= offBytes {
		t.Fatalf("native bytes enabled/disabled = %d/%d, want reduction", onBytes, offBytes)
	}
}

func BenchmarkReferenceBooleanFactsCompileARM64(b *testing.B) {
	m := referenceBooleanFactModuleARM64(b)
	for _, enabled := range []bool{false, true} {
		name := "off"
		if enabled {
			name = "on"
		}
		b.Run(name, func(b *testing.B) {
			opts := CompileOptions{Optimizations: map[string]bool{"value-facts": enabled}}
			compiled, err := CompileModuleWith(m, opts)
			if err != nil {
				b.Fatal(err)
			}
			codeBytes := len(compiled.Code)
			compiled.CodeImage.Close()
			b.ReportAllocs()
			b.ResetTimer()
			for range b.N {
				cm, err := CompileModuleWith(m, opts)
				if err != nil {
					b.Fatal(err)
				}
				cm.CodeImage.Close()
			}
			b.ReportMetric(float64(codeBytes), "code-B")
		})
	}
}

func referenceTypeTestBooleanFactModuleARM64(t testing.TB) *wasm.Module {
	t.Helper()
	body := []byte{0x01, 0x01, 0x7e} // one i64 accumulator local
	for range 16 {
		body = append(body,
			0x20, 0x01, // local.get accumulator
			0x20, 0x00, // local.get nullable funcref
			0xfb, 0x14, 0x70, // ref.test func
			0xad,       // i64.extend_i32_u
			0x7c,       // i64.add
			0x21, 0x01, // local.set accumulator
		)
	}
	body = append(body, 0x20, 0x01, 0x0b)
	data := wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(
			wasmtest.FuncType([]wasm.ValType{wasm.FuncRef}, []wasm.ValType{wasm.I64}),
		)),
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

func TestReferenceTypeTestBooleanFactsARM64(t *testing.T) {
	compile := func(enabled bool) (*CodegenStats, int) {
		var stats ModuleStats
		compiled, err := CompileModuleWith(referenceTypeTestBooleanFactModuleARM64(t), CompileOptions{
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
	if got := on.Peephole["ext-elim"]; got != 16 {
		t.Fatalf("ref.test extension elisions = %d, want 16", got)
	}
	if got := off.Peephole["ext-elim"]; got != 0 {
		t.Fatalf("disabled ref.test extension elisions = %d, want 0", got)
	}
	if onBytes >= offBytes {
		t.Fatalf("native bytes enabled/disabled = %d/%d, want reduction", onBytes, offBytes)
	}
}

func TestReferenceTypeTestHelperBooleanFactsARM64(t *testing.T) {
	body := []byte{
		0x00,       // no locals
		0x20, 0x00, // local.get nullable anyref
		0xfb, 0x14, 0x6b, // ref.test struct
		0xad, // i64.extend_i32_u
		0x0b,
	}
	data := wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(
			[]byte{0x5f, 0x00}, // empty struct type
			wasmtest.FuncType([]wasm.ValType{wasm.AnyRef}, []wasm.ValType{wasm.I64}),
		)),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(1))),
		wasmtest.Section(10, wasmtest.Vec(append(wasmtest.ULEB(uint32(len(body))), body...))),
	)
	m, err := wasm.DecodeModule(data)
	if err != nil {
		t.Fatal(err)
	}
	if err := wasm.ValidateModule(m); err != nil {
		t.Fatal(err)
	}
	compile := func(enabled bool) int {
		var stats ModuleStats
		compiled, err := CompileModuleWith(m, CompileOptions{
			GCStructHelpers: true,
			Stats:           &stats,
			Optimizations:   map[string]bool{"value-facts": enabled},
		})
		if err != nil {
			t.Fatal(err)
		}
		compiled.CodeImage.Close()
		return stats.Funcs[0].Peephole["ext-elim"]
	}
	if got := compile(true); got != 1 {
		t.Fatalf("helper ref.test extension elisions = %d, want 1", got)
	}
	if got := compile(false); got != 0 {
		t.Fatalf("disabled helper ref.test extension elisions = %d, want 0", got)
	}
}

func BenchmarkReferenceTypeTestBooleanFactsCompileARM64(b *testing.B) {
	m := referenceTypeTestBooleanFactModuleARM64(b)
	for _, enabled := range []bool{false, true} {
		name := "off"
		if enabled {
			name = "on"
		}
		b.Run(name, func(b *testing.B) {
			opts := CompileOptions{Optimizations: map[string]bool{"value-facts": enabled}}
			compiled, err := CompileModuleWith(m, opts)
			if err != nil {
				b.Fatal(err)
			}
			codeBytes := len(compiled.Code)
			compiled.CodeImage.Close()
			b.ReportAllocs()
			b.ResetTimer()
			for range b.N {
				cm, err := CompileModuleWith(m, opts)
				if err != nil {
					b.Fatal(err)
				}
				cm.CodeImage.Close()
			}
			b.ReportMetric(float64(codeBytes), "code-B")
		})
	}
}
