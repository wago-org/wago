//go:build linux && amd64 && !tinygo && !wago_guardpage

package wago

import (
	"os"
	"path/filepath"
	"testing"
)

func stagedGCRefCastBytes(t testing.TB, class stagedGCRefCastClass) []byte {
	t.Helper()
	var script stagedSpecScript
	tmp := stagedOfficialTypedReferenceJSON(t, "gc/ref_cast", &script)
	var latest []byte
	for _, cmd := range script.Commands {
		switch cmd.Type {
		case "module_definition":
			data, err := os.ReadFile(filepath.Join(tmp, cmd.Filename))
			if err != nil {
				t.Fatal(err)
			}
			latest = data
		case "module_instance":
			pin, ok := stagedGCRefCastLeaderPinFor(latest, cmd.Line)
			if ok && pin.Class == class {
				return append([]byte(nil), latest...)
			}
		}
	}
	t.Fatalf("official %s gc/ref_cast leader not found", class)
	return nil
}

func compileStagedGCRefCastProduct(data []byte) (*Compiled, error) {
	cfg := compatibilityDefaultConfig()
	features := cfg.frontendFeatures()
	features.TypedFunctionReferences = true
	features.GCStructProducts = true
	features.GCI31Products = true
	if product, ok := stagedGCStructExecutionProduct(data); ok && product == stagedGCStructRefCastAbstract {
		features.GCArrayProducts = true
	}
	return compileWithFrontendFeatures(cfg, data, features)
}

func BenchmarkStagedGCRefCastStableI31(b *testing.B) {
	c, err := compileStagedGCRefCastProduct(stagedGCRefCastBytes(b, stagedGCRefCastAbstract))
	if err != nil {
		b.Fatal(err)
	}
	defer c.Close()
	in, err := instantiateCore(c, InstantiateOptions{})
	if err != nil {
		b.Fatal(err)
	}
	defer in.Close()
	extern, err := in.NewExternRef("benchmark")
	if err != nil {
		b.Fatal(err)
	}
	if _, err := in.Invoke("init", ValueExternRef(extern).Bits()); err != nil {
		b.Fatal(err)
	}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		got, err := in.Invoke("ref_cast_i31", 1)
		if err != nil || len(got) != 0 {
			b.Fatalf("ref_cast_i31(1) = %v, %v", got, err)
		}
	}
}
