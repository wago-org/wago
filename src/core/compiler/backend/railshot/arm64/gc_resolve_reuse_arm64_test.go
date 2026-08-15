//go:build arm64

package arm64

import (
	"testing"

	"github.com/wago-org/wago/src/core/compiler/codegen"
	"github.com/wago-org/wago/src/core/compiler/frontend"
	"github.com/wago-org/wago/src/core/compiler/wasm"
	"github.com/wago-org/wago/tests/wasmtest"
)

func nativeGCResolveReuseModuleARM64(t testing.TB, casts bool, between ...byte) *wasm.Module {
	t.Helper()
	structType := []byte{0x5f}
	structType = append(structType, wasmtest.Vec([]byte{0x7f, 0x01}, []byte{0x7f, 0x01})...)
	initBody := []byte{
		0x00,
		0x41, 0x28,
		0x41, 0x02,
		0xfb, 0x00, 0x00, // struct.new type 0
		0x24, 0x00,
		0x0b,
	}
	runBody := []byte{
		0x01, 0x01, 0x63, 0x00, // one (ref null 0) local
		0x23, 0x00,
		0x21, 0x00,
		0x20, 0x00,
	}
	if casts {
		runBody = append(runBody, 0xfb, 0x16, 0x00) // ref.cast (ref type 0)
	}
	runBody = append(runBody, 0xfb, 0x02, 0x00, 0x00) // struct.get type 0 field 0
	runBody = append(runBody, between...)
	for i := 1; i < 8; i++ {
		runBody = append(runBody, 0x20, 0x00)
		if casts {
			runBody = append(runBody, 0xfb, 0x16, 0x00)
		}
		runBody = append(runBody, 0xfb, 0x02, 0x00, byte(i&1))
	}
	for i := 1; i < 8; i++ {
		runBody = append(runBody, 0x6a)
	}
	runBody = append(runBody, 0x0b)
	data := wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(structType, wasmtest.FuncType(nil, nil), wasmtest.FuncType(nil, []wasm.ValType{wasm.I32}))),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(1), wasmtest.ULEB(2))),
		wasmtest.Section(6, wasmtest.Vec([]byte{0x63, 0x00, 0x01, 0xd0, 0x00, 0x0b})),
		wasmtest.Section(10, wasmtest.Vec(
			append(wasmtest.ULEB(uint32(len(initBody))), initBody...),
			append(wasmtest.ULEB(uint32(len(runBody))), runBody...),
		)),
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

func compileNativeGCResolveReuseARM64(t testing.TB, m *wasm.Module, enabled bool) (*CodegenStats, int) {
	t.Helper()
	metadata, err := frontend.BuildGCTypeMetadata(m)
	if err != nil {
		t.Fatal(err)
	}
	var stats ModuleStats
	compiled, err := CompileModuleWith(m, CompileOptions{
		GCStructHelpers: true,
		Stats:           &stats,
		Optimizations: map[string]bool{
			"gc-native-final-scalar-get": true,
			"gc-native-resolve-reuse":    enabled,
		},
		Codegen: codegen.Options{Module: codegen.ModuleInfo{GCTypeLayouts: metadata.Layouts}},
	})
	if err != nil {
		t.Fatal(err)
	}
	codeBytes := len(compiled.Code)
	if err := compiled.CodeImage.Close(); err != nil {
		t.Fatal(err)
	}
	return stats.Funcs[1], codeBytes
}

func TestNativeGCResolveReuseARM64(t *testing.T) {
	m := nativeGCResolveReuseModuleARM64(t, false)
	on, onBytes := compileNativeGCResolveReuseARM64(t, m, true)
	off, offBytes := compileNativeGCResolveReuseARM64(t, m, false)
	if got := on.Peephole["gc-native-resolve-reuse"]; got != 7 {
		t.Fatalf("gc-native-resolve-reuse = %d, want 7 (all: %v)", got, on.Peephole)
	}
	if got := off.Peephole["gc-native-resolve-reuse"]; got != 0 {
		t.Fatalf("disabled gc-native-resolve-reuse = %d, want 0", got)
	}
	if got := on.Peephole["gc-native-final-struct-scalar-get"]; got != 8 {
		t.Fatalf("native scalar reads = %d, want 8", got)
	}
	if onBytes >= offBytes {
		t.Fatalf("reuse code bytes = %d, want less than disabled %d", onBytes, offBytes)
	}
}

func TestNativeGCResolveReuseAfterFinalCastsARM64(t *testing.T) {
	stats, _ := compileNativeGCResolveReuseARM64(t, nativeGCResolveReuseModuleARM64(t, true), true)
	if got := stats.Peephole["gc-native-resolve-reuse"]; got != 7 {
		t.Fatalf("reuse after final casts = %d, want 7", got)
	}
	if got := stats.Peephole["final-cast-struct-get-fuse"]; got != 8 {
		t.Fatalf("final-cast struct fusions = %d, want 8", got)
	}
}

func TestNativeGCResolveReuseStopsAtNearMissARM64(t *testing.T) {
	// A constructor helper is a GC safepoint: the raw address from the first
	// field read must be discarded, after which the remaining run may cache anew.
	stats, _ := compileNativeGCResolveReuseARM64(t, nativeGCResolveReuseModuleARM64(t, false, 0xfb, 0x01, 0x00, 0x1a), true)
	if got := stats.Peephole["gc-native-resolve-reuse"]; got != 6 {
		t.Fatalf("reuse across one GC safepoint = %d, want 6", got)
	}
}

func BenchmarkNativeGCResolveReuseCompileARM64(b *testing.B) {
	for _, shape := range []struct {
		name  string
		casts bool
	}{{"typed", false}, {"final-casts", true}} {
		b.Run(shape.name, func(b *testing.B) {
			m := nativeGCResolveReuseModuleARM64(b, shape.casts)
			metadata, err := frontend.BuildGCTypeMetadata(m)
			if err != nil {
				b.Fatal(err)
			}
			for _, enabled := range []bool{false, true} {
				name := "off"
				if enabled {
					name = "on"
				}
				b.Run(name, func(b *testing.B) {
					opts := CompileOptions{GCStructHelpers: true, Optimizations: map[string]bool{"gc-native-final-scalar-get": true, "gc-native-resolve-reuse": enabled}, Codegen: codegen.Options{Module: codegen.ModuleInfo{GCTypeLayouts: metadata.Layouts}}}
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
		})
	}
}
