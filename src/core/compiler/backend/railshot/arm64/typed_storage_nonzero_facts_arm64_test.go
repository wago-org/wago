//go:build arm64

package arm64

import (
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
	"github.com/wago-org/wago/tests/wasmtest"
)

func typedStorageNonZeroFactModuleARM64(t testing.TB, nullable bool) *wasm.Module {
	t.Helper()
	refCode := byte(0x64)
	if nullable {
		refCode = 0x63
	}
	globalImport := append(wasmtest.Name("env"), wasmtest.Name("global")...)
	globalImport = append(globalImport, 0x03, refCode, byte(wasm.HeapFunc), 0x00)
	tableImport := append(wasmtest.Name("env"), wasmtest.Name("table")...)
	tableImport = append(tableImport, 0x01, refCode, byte(wasm.HeapFunc), 0x00, 0x01)
	body := []byte{}
	for range 8 {
		body = append(body,
			0x23, 0x00, 0xd1, 0x1a,
			0x23, 0x00, 0xd4, 0x1a,
			0x41, 0x00, 0x25, 0x00, 0xd1, 0x1a,
			0x41, 0x00, 0x25, 0x00, 0xd4, 0x1a,
		)
	}
	body = append(body, 0x41, 0x00, 0x0b)
	data := wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType(nil, []wasm.ValType{wasm.I32}))),
		wasmtest.Section(2, wasmtest.Vec(globalImport, tableImport)),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(0))),
		wasmtest.Section(10, wasmtest.Vec(wasmtest.Code(body))),
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

func TestTypedStorageNonZeroFactsARM64(t *testing.T) {
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
	nonNull := typedStorageNonZeroFactModuleARM64(t, false)
	on, onBytes := compile(nonNull, true)
	off, offBytes := compile(nonNull, false)
	if got := on.Peephole["ref-is-null-fold"]; got != 16 {
		t.Fatalf("ref.is_null folds = %d, want 16", got)
	}
	if got := on.Peephole["gc-null-check-elide"]; got != 16 {
		t.Fatalf("null-check elisions = %d, want 16", got)
	}
	if got := off.Peephole["ref-is-null-fold"] + off.Peephole["gc-null-check-elide"]; got != 0 {
		t.Fatalf("disabled fact-driven rewrites = %d, want 0", got)
	}
	if onBytes >= offBytes {
		t.Fatalf("native bytes enabled/disabled = %d/%d, want reduction", onBytes, offBytes)
	}
	nullable, _ := compile(typedStorageNonZeroFactModuleARM64(t, true), true)
	if got := nullable.Peephole["ref-is-null-fold"] + nullable.Peephole["gc-null-check-elide"]; got != 0 {
		t.Fatalf("nullable storage fact-driven rewrites = %d, want 0", got)
	}
}

func BenchmarkTypedStorageNonZeroFactsCompileARM64(b *testing.B) {
	m := typedStorageNonZeroFactModuleARM64(b, false)
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
