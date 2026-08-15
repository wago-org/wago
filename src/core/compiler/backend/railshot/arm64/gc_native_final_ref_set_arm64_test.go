//go:build arm64

package arm64

import (
	"testing"

	"github.com/wago-org/wago/src/core/compiler/codegen"
	"github.com/wago-org/wago/src/core/compiler/frontend"
	"github.com/wago-org/wago/src/core/compiler/wasm"
	"github.com/wago-org/wago/tests/wasmtest"
)

func nativeFinalRefSetModuleARM64(t testing.TB, array bool) *wasm.Module {
	t.Helper()
	childType := []byte{0x5f, 0x01, 0x7f, 0x01}
	containerType := []byte{0x5f, 0x01, 0x63, 0x00, 0x01}
	localType := byte(0x01)
	body := []byte{0x01, 0x01, 0x63, localType}
	if array {
		containerType = []byte{0x5e, 0x63, 0x00, 0x01}
		body = append(body,
			0x41, 0x01, 0xfb, 0x00, 0x00, // struct.new child
			0x41, 0x08, 0xfb, 0x06, 0x01, // array.new container
			0x21, 0x00,
		)
		for i := 0; i < 8; i++ {
			body = append(body,
				0x20, 0x00, 0x41, byte(i), 0xd0, 0x00, // array, index, ref.null child
				0xfb, 0x0e, 0x01, // array.set container
			)
		}
		body = append(body,
			0x20, 0x00, 0x41, 0x07, 0xfb, 0x0b, 0x01, // array.get container
			0xd1, 0x0b, // ref.is_null; end
		)
	} else {
		body = append(body,
			0x41, 0x01, 0xfb, 0x00, 0x00, // struct.new child
			0xfb, 0x00, 0x01, 0x21, 0x00, // struct.new container; local.set
		)
		for range 8 {
			body = append(body,
				0x20, 0x00, 0xd0, 0x00, // container, ref.null child
				0xfb, 0x05, 0x01, 0x00, // struct.set container field 0
			)
		}
		body = append(body,
			0x20, 0x00, 0xfb, 0x02, 0x01, 0x00, // struct.get container field 0
			0xd1, 0x0b, // ref.is_null; end
		)
	}
	data := wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(childType, containerType, wasmtest.FuncType(nil, []wasm.ValType{wasm.I32}))),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(2))),
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

func compileNativeFinalRefSetARM64(t testing.TB, array, enabled bool, objective *OptimizationObjective) (*CodegenStats, int) {
	t.Helper()
	m := nativeFinalRefSetModuleARM64(t, array)
	metadata, err := frontend.BuildGCTypeMetadata(m)
	if err != nil {
		t.Fatal(err)
	}
	var stats ModuleStats
	compiled, err := CompileModuleWith(m, CompileOptions{
		GCStructHelpers: true,
		GCArrayHelpers:  true,
		Stats:           &stats,
		Optimizations:   map[string]bool{"gc-native-final-ref-set": enabled},
		Objective:       objective,
		Codegen:         codegen.Options{Module: codegen.ModuleInfo{GCTypeLayouts: metadata.Layouts}},
	})
	if err != nil {
		t.Fatal(err)
	}
	codeBytes := len(compiled.Code)
	if err := compiled.CodeImage.Close(); err != nil {
		t.Fatal(err)
	}
	return stats.Funcs[0], codeBytes
}

func TestNativeFinalRefSetNullARM64(t *testing.T) {
	for _, tc := range []struct {
		name  string
		array bool
		peep  string
	}{
		{"struct", false, "gc-native-final-struct-ref-set-null"},
		{"array", true, "gc-native-final-array-ref-set-null"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			on, onBytes := compileNativeFinalRefSetARM64(t, tc.array, true, nil)
			off, offBytes := compileNativeFinalRefSetARM64(t, tc.array, false, nil)
			if got := on.Peephole[tc.peep]; got != 8 {
				t.Fatalf("native null reference sets = %d, want 8", got)
			}
			if got := on.Peephole["gc-barrier-elide"]; got != 8 {
				t.Fatalf("barrier elisions = %d, want 8", got)
			}
			if got := off.Peephole[tc.peep]; got != 0 {
				t.Fatalf("disabled native null reference sets = %d, want 0", got)
			}
			if growth := onBytes - offBytes; growth > 1024 {
				t.Fatalf("native null reference set code growth = %d bytes, want <=1024 (%d versus %d)", growth, onBytes, offBytes)
			}
			size := OptimizeSize
			compact, _ := compileNativeFinalRefSetARM64(t, tc.array, true, &size)
			if got := compact.Peephole[tc.peep]; got != 0 {
				t.Fatalf("Size native null reference sets = %d, want 0", got)
			}
		})
	}
}

func TestNativeFinalRefSetRejectsNonNullChildARM64(t *testing.T) {
	childType := []byte{0x5f, 0x01, 0x7f, 0x01}
	parentType := []byte{0x5f, 0x01, 0x63, 0x00, 0x01}
	body := []byte{
		0x01, 0x01, 0x63, 0x01, // one nullable parent local
		0xd0, 0x00, 0xfb, 0x00, 0x01, 0x21, 0x00, // parent(ref.null child)
		0x20, 0x00, 0x41, 0x01, 0xfb, 0x00, 0x00, // parent, non-null child
		0xfb, 0x05, 0x01, 0x00, // struct.set parent field 0
		0x0b,
	}
	data := wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(childType, parentType, wasmtest.FuncType(nil, nil))),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(2))),
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
	var stats ModuleStats
	compiled, err := CompileModuleWith(m, CompileOptions{
		GCStructHelpers: true,
		Stats:           &stats,
		Optimizations:   map[string]bool{"gc-native-final-ref-set": true},
		Codegen:         codegen.Options{Module: codegen.ModuleInfo{GCTypeLayouts: metadata.Layouts}},
	})
	if err != nil {
		t.Fatal(err)
	}
	defer compiled.CodeImage.Close()
	if got := stats.Funcs[0].Peephole["gc-native-final-struct-ref-set-null"]; got != 0 {
		t.Fatalf("non-null child native reference sets = %d, want 0", got)
	}
	if got := stats.Funcs[0].Calls[callKindHostSync]; got != 3 {
		t.Fatalf("non-null child helper calls = %d, want 3", got)
	}
}

func BenchmarkNativeFinalRefSetCompileARM64(b *testing.B) {
	for _, array := range []bool{false, true} {
		kind := "struct"
		if array {
			kind = "array"
		}
		m := nativeFinalRefSetModuleARM64(b, array)
		metadata, err := frontend.BuildGCTypeMetadata(m)
		if err != nil {
			b.Fatal(err)
		}
		for _, enabled := range []bool{false, true} {
			name := kind + "/off"
			if enabled {
				name = kind + "/on"
			}
			b.Run(name, func(b *testing.B) {
				opts := CompileOptions{GCStructHelpers: true, GCArrayHelpers: true, Optimizations: map[string]bool{"gc-native-final-ref-set": enabled}, Codegen: codegen.Options{Module: codegen.ModuleInfo{GCTypeLayouts: metadata.Layouts}}}
				compiled, err := CompileModuleWith(m, opts)
				if err != nil {
					b.Fatal(err)
				}
				codeBytes := len(compiled.Code)
				if err := compiled.CodeImage.Close(); err != nil {
					b.Fatal(err)
				}
				b.ReportAllocs()
				b.ResetTimer()
				for range b.N {
					cm, err := CompileModuleWith(m, opts)
					if err != nil {
						b.Fatal(err)
					}
					if err := cm.CodeImage.Close(); err != nil {
						b.Fatal(err)
					}
				}
				b.ReportMetric(float64(codeBytes), "code-B")
			})
		}
	}
}
