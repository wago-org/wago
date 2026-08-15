//go:build arm64

package arm64

import (
	"testing"

	"github.com/wago-org/wago/src/core/compiler/codegen"
	"github.com/wago-org/wago/src/core/compiler/frontend"
	"github.com/wago-org/wago/src/core/compiler/wasm"
	"github.com/wago-org/wago/tests/wasmtest"
)

func nativeScalarFactModuleARM64(t testing.TB) (*wasm.Module, codegen.ModuleInfo) {
	t.Helper()
	structType := []byte{0x5f}
	structType = append(structType, wasmtest.Vec([]byte{0x78, 0x01})...) // mutable packed i8
	body := []byte{
		0x01, 0x01, 0x7e, // one i64 accumulator local
		0x41, 0x7f, // i32.const -1
		0xfb, 0x00, 0x00, // struct.new type 0
		0x24, 0x00, // global.set 0
	}
	for range 8 {
		body = append(body,
			0x20, 0x00, // local.get accumulator
			0x23, 0x00, // global.get 0
			0xfb, 0x04, 0x00, 0x00, // struct.get_u type 0 field 0
			0xad,       // i64.extend_i32_u
			0x7c,       // i64.add
			0x21, 0x00, // local.set accumulator
		)
	}
	body = append(body, 0x20, 0x00, 0x0b)
	data := wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(structType, wasmtest.FuncType(nil, []wasm.ValType{wasm.I64}))),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(1))),
		wasmtest.Section(6, wasmtest.Vec([]byte{0x63, 0x00, 0x01, 0xd0, 0x00, 0x0b})),
		wasmtest.Section(10, wasmtest.Vec(append(wasmtest.ULEB(uint32(len(body))), body...))),
	)
	m, err := wasm.DecodeModule(data)
	if err != nil {
		t.Fatal(err)
	}
	if err := wasm.ValidateModule(m); err != nil {
		t.Fatal(err)
	}
	metadata, err := frontend.BuildGCTypeMetadata(m)
	if err != nil {
		t.Fatal(err)
	}
	return m, codegen.ModuleInfo{GCTypeLayouts: metadata.Layouts}
}

func TestNativeScalarFactsARM64(t *testing.T) {
	m, moduleInfo := nativeScalarFactModuleARM64(t)
	compile := func(enabled bool) (*CodegenStats, int) {
		var stats ModuleStats
		compiled, err := CompileModuleWith(m, CompileOptions{
			GCStructHelpers: true,
			Stats:           &stats,
			Optimizations: map[string]bool{
				"gc-native-final-scalar-get": true,
				"value-facts":                enabled,
			},
			Codegen: codegen.Options{Module: moduleInfo},
		})
		if err != nil {
			t.Fatal(err)
		}
		defer compiled.CodeImage.Close()
		return stats.Funcs[0], len(compiled.Code)
	}
	on, onBytes := compile(true)
	off, offBytes := compile(false)
	if got := on.Peephole["gc-native-final-struct-scalar-get"]; got != 8 {
		t.Fatalf("native scalar gets = %d, want 8", got)
	}
	if got := on.Peephole["ext-elim"]; got != 8 {
		t.Fatalf("native scalar extension elisions = %d, want 8 (all: %v)", got, on.Peephole)
	}
	if got := off.Peephole["ext-elim"]; got != 0 {
		t.Fatalf("disabled native scalar extension elisions = %d, want 0", got)
	}
	if onBytes >= offBytes {
		t.Fatalf("native bytes enabled/disabled = %d/%d, want reduction", onBytes, offBytes)
	}
}

func BenchmarkNativeScalarFactsCompileARM64(b *testing.B) {
	m, moduleInfo := nativeScalarFactModuleARM64(b)
	for _, enabled := range []bool{false, true} {
		name := "off"
		if enabled {
			name = "on"
		}
		b.Run(name, func(b *testing.B) {
			opts := CompileOptions{
				GCStructHelpers: true,
				Optimizations: map[string]bool{
					"gc-native-final-scalar-get": true,
					"value-facts":                enabled,
				},
				Codegen: codegen.Options{Module: moduleInfo},
			}
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
