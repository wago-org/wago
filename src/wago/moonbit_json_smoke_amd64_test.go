//go:build linux && amd64 && !tinygo && !wago_guardpage

package wago

import (
	"context"
	"crypto/sha256"
	"fmt"
	"os"
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
)

const (
	moonBitJSONSmokeWasmEnv    = "WAGO_MOONBIT_JSON_SMOKE_WASM"
	moonBitJSONSmokeWasmSize   = 44023
	moonBitJSONSmokeWasmSHA256 = "b4e33e0685aa5572516ab037be12a3ad1aee93ab9891ba4071c42c23a3e9ca2d"
)

func moonBitJSONSmokeWasm(tb testing.TB) []byte {
	tb.Helper()
	path := os.Getenv(moonBitJSONSmokeWasmEnv)
	if path == "" {
		tb.Skipf("set %s to the pinned MoonBit JSON wasm-gc artifact", moonBitJSONSmokeWasmEnv)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		tb.Fatalf("read MoonBit JSON smoke payload: %v", err)
	}
	if len(data) != moonBitJSONSmokeWasmSize {
		tb.Fatalf("MoonBit JSON smoke payload size = %d, want %d", len(data), moonBitJSONSmokeWasmSize)
	}
	if got := fmt.Sprintf("%x", sha256.Sum256(data)); got != moonBitJSONSmokeWasmSHA256 {
		tb.Fatalf("MoonBit JSON smoke payload SHA-256 = %s, want %s", got, moonBitJSONSmokeWasmSHA256)
	}
	return data
}

func BenchmarkMoonBitJSONWasmGCDecode(b *testing.B) {
	data := moonBitJSONSmokeWasm(b)
	b.ReportAllocs()
	b.SetBytes(int64(len(data)))
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := wasm.DecodeModule(data); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkMoonBitJSONWasmGCValidate(b *testing.B) {
	data := moonBitJSONSmokeWasm(b)
	module, err := wasm.DecodeModule(data)
	if err != nil {
		b.Fatal(err)
	}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if err := wasm.ValidateModuleWithFeatures(module, wasm.ValidationFeatures{GCConstExpr: true}); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkMoonBitJSONWasmGCCompile(b *testing.B) {
	data := moonBitJSONSmokeWasm(b)
	cfg := NewRuntimeConfig().
		WithCoreFeatures(CoreFeaturesV3).
		WithBoundsChecks(BoundsChecksExplicit)
	b.ReportAllocs()
	b.SetBytes(int64(len(data)))
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		compiled, err := Compile(cfg, data)
		if err != nil {
			b.Fatal(err)
		}
		if err := compiled.Close(); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkMoonBitJSONWasmGCInstantiate(b *testing.B) {
	data := moonBitJSONSmokeWasm(b)
	compiled, err := Compile(NewRuntimeConfig().
		WithCoreFeatures(CoreFeaturesV3).
		WithBoundsChecks(BoundsChecksExplicit), data)
	if err != nil {
		b.Fatal(err)
	}
	defer compiled.Close()
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		instance, err := Instantiate(compiled, InstantiateOptions{})
		if err != nil {
			b.Fatal(err)
		}
		if err := instance.Close(); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkMoonBitJSONWasmGCInstantiateRun(b *testing.B) {
	data := moonBitJSONSmokeWasm(b)
	compiled, err := Compile(NewRuntimeConfig().
		WithCoreFeatures(CoreFeaturesV3).
		WithBoundsChecks(BoundsChecksExplicit), data)
	if err != nil {
		b.Fatal(err)
	}
	defer compiled.Close()
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		instance, err := Instantiate(compiled, InstantiateOptions{})
		if err != nil {
			b.Fatal(err)
		}
		values, err := instance.Call(context.Background(), "run", ValueI32(1))
		if err != nil {
			_ = instance.Close()
			b.Fatal(err)
		}
		if len(values) != 1 || values[0].I64() != 1808148174 {
			_ = instance.Close()
			b.Fatalf("run(1) = %v", values)
		}
		if err := instance.Close(); err != nil {
			b.Fatal(err)
		}
	}
}

func TestMoonBitJSONWasmGCSmoke(t *testing.T) {
	data := moonBitJSONSmokeWasm(t)
	compiled, err := Compile(NewRuntimeConfig().
		WithCoreFeatures(CoreFeaturesV3).
		WithBoundsChecks(BoundsChecksExplicit), data)
	if err != nil {
		t.Fatalf("compile MoonBit JSON wasm-gc payload: %v", err)
	}
	defer compiled.Close()
	if len(compiled.Imports) != 0 {
		t.Fatalf("MoonBit JSON smoke imports = %v, want none", compiled.Imports)
	}
	gotExports := compiled.ExportedFunctions()
	if len(gotExports) != 2 || gotExports[0] != "_start" || gotExports[1] != "run" {
		t.Fatalf("MoonBit JSON smoke exports = %v, want [_start run]", gotExports)
	}

	instance, err := Instantiate(compiled, InstantiateOptions{})
	if err != nil {
		t.Fatalf("instantiate MoonBit JSON wasm-gc payload: %v", err)
	}
	defer instance.Close()

	for _, tc := range []struct {
		iterations int32
		want       int64
	}{
		{iterations: 1, want: 1808148174},
		{iterations: 2, want: 1512327905},
		{iterations: 8, want: 828453439},
	} {
		values, err := instance.Call(context.Background(), "run", ValueI32(tc.iterations))
		if err != nil {
			t.Fatalf("run(%d): %v", tc.iterations, err)
		}
		if len(values) != 1 || values[0].I64() != tc.want {
			t.Fatalf("run(%d) = %v, want [%d]", tc.iterations, values, tc.want)
		}
	}
	if instance.gc == nil || instance.gc.Stats().LiveObjects == 0 {
		t.Fatalf("MoonBit JSON execution GC state = %#v", instance.gc)
	}
}
