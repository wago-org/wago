//go:build linux && amd64 && !tinygo && !wago_guardpage

package wago

import (
	"os"
	"testing"
)

const starshineSmokeWasmEnv = "WAGO_STARSHINE_SMOKE_WASM"

func starshineSmokeWasm(tb testing.TB) []byte {
	tb.Helper()
	path := os.Getenv(starshineSmokeWasmEnv)
	if path == "" {
		tb.Skipf("set %s to a MoonBit wasm-gc Starshine CLI artifact", starshineSmokeWasmEnv)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		tb.Fatalf("read Starshine smoke payload: %v", err)
	}
	return data
}

func starshineSmokeConfig() *RuntimeConfig {
	return NewRuntimeConfig().
		WithCoreFeatures(CoreFeaturesV3).
		WithBoundsChecks(BoundsChecksExplicit)
}

func starshineSmokeImports(compiled *Compiled) Imports {
	imports := make(Imports, len(compiled.Imports))
	for _, key := range compiled.Imports {
		imports[key] = HostFunc(func(HostModule, []uint64, []uint64) {})
	}
	return imports
}

func TestMoonBitStarshineWasmGCSmokeCompile(t *testing.T) {
	data := starshineSmokeWasm(t)
	compiled, err := Compile(starshineSmokeConfig(), data)
	if err != nil {
		t.Fatalf("compile MoonBit Starshine wasm-gc payload: %v", err)
	}
	defer compiled.Close()
	if len(compiled.FuncTypeID) < 10_000 || len(compiled.Imports) == 0 {
		t.Fatalf("decoded Starshine footprint = functions %d imports %d", len(compiled.FuncTypeID), len(compiled.Imports))
	}
	if err := compiled.validateImportBindings(starshineSmokeImports(compiled), nil); err != nil {
		t.Fatalf("validate MoonBit Starshine wasm-gc imports: %v", err)
	}
	if len(compiled.Code) == 0 || len(compiled.Entry) < 10_000 {
		t.Fatalf("compiled Starshine footprint = code %d entries %d", len(compiled.Code), len(compiled.Entry))
	}
}

func TestMoonBitStarshineWasmGCSmokeInstantiate(t *testing.T) {
	data := starshineSmokeWasm(t)
	compiled, err := Compile(starshineSmokeConfig(), data)
	if err != nil {
		t.Fatalf("compile MoonBit Starshine wasm-gc payload: %v", err)
	}
	defer compiled.Close()
	if len(compiled.PassiveData) != 1 {
		t.Fatalf("Starshine passive data segments = %d, want 1", len(compiled.PassiveData))
	}
	t.Logf("Starshine passive data bytes = %d", len(compiled.PassiveData[0].Bytes))
	instance, err := Instantiate(compiled, InstantiateOptions{Imports: starshineSmokeImports(compiled)})
	if err != nil {
		t.Fatalf("instantiate MoonBit Starshine wasm-gc payload: %v", err)
	}
	defer instance.Close()
	if instance.gc == nil || instance.gc.Stats().LiveObjects < 400 {
		t.Fatalf("Starshine startup GC state = %#v", instance.gc)
	}
}

func BenchmarkMoonBitStarshineWasmGCCompile(b *testing.B) {
	data := starshineSmokeWasm(b)
	cfg := starshineSmokeConfig()
	b.ReportAllocs()
	b.SetBytes(int64(len(data)))
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

func BenchmarkMoonBitStarshineWasmGCCompileLink(b *testing.B) {
	data := starshineSmokeWasm(b)
	cfg := starshineSmokeConfig()
	b.ReportAllocs()
	b.SetBytes(int64(len(data)))
	for i := 0; i < b.N; i++ {
		compiled, err := Compile(cfg, data)
		if err != nil {
			b.Fatal(err)
		}
		if err := compiled.validateImportBindings(starshineSmokeImports(compiled), nil); err != nil {
			_ = compiled.Close()
			b.Fatal(err)
		}
		if err := compiled.Close(); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkMoonBitStarshineWasmGCLinkCold(b *testing.B) {
	data := starshineSmokeWasm(b)
	cfg := starshineSmokeConfig()
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		b.StopTimer()
		compiled, err := Compile(cfg, data)
		if err != nil {
			b.Fatal(err)
		}
		imports := starshineSmokeImports(compiled)
		b.StartTimer()
		err = compiled.validateImportBindings(imports, nil)
		b.StopTimer()
		if err != nil {
			_ = compiled.Close()
			b.Fatal(err)
		}
		if err := compiled.Close(); err != nil {
			b.Fatal(err)
		}
		b.StartTimer()
	}
}

func BenchmarkMoonBitStarshineWasmGCInstantiate(b *testing.B) {
	data := starshineSmokeWasm(b)
	compiled, err := Compile(starshineSmokeConfig(), data)
	if err != nil {
		b.Fatal(err)
	}
	defer compiled.Close()
	imports := starshineSmokeImports(compiled)
	if err := compiled.validateImportBindings(imports, nil); err != nil {
		b.Fatal(err)
	}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		instance, err := Instantiate(compiled, InstantiateOptions{Imports: imports})
		if err != nil {
			b.Fatal(err)
		}
		if instance.gc == nil || instance.gc.Stats().LiveObjects < 400 {
			_ = instance.Close()
			b.Fatalf("Starshine startup GC state = %#v", instance.gc)
		}
		if err := instance.Close(); err != nil {
			b.Fatal(err)
		}
	}
}
