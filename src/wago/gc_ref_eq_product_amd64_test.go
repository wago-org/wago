//go:build linux && amd64 && !tinygo && !wago_guardpage

package wago

import (
	"os"
	"path/filepath"
	"testing"
)

func stagedGCRefEqBytes(t testing.TB) []byte {
	t.Helper()
	var script stagedSpecScript
	tmp := stagedOfficialTypedReferenceJSON(t, "gc/ref_eq", &script)
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
			if _, ok := stagedGCRefEqLeaderPinFor(latest, cmd.Line); ok {
				return append([]byte(nil), latest...)
			}
		}
	}
	t.Fatal("official gc/ref_eq leader not found")
	return nil
}

func compileStagedGCRefEqProduct(data []byte) (*Compiled, error) {
	cfg := compatibilityDefaultConfig()
	features := cfg.frontendFeatures()
	features.TypedFunctionReferences = true
	features.GCStructProducts = true
	features.GCArrayProducts = true
	features.GCI31Products = true
	return compileWithFrontendFeatures(cfg, data, features)
}

func BenchmarkStagedGCRefEqStableIdentity(b *testing.B) {
	c, err := compileStagedGCRefEqProduct(stagedGCRefEqBytes(b))
	if err != nil {
		b.Fatal(err)
	}
	defer c.Close()
	in, err := instantiateCore(c, InstantiateOptions{})
	if err != nil {
		b.Fatal(err)
	}
	defer in.Close()
	if _, err := in.Invoke("init"); err != nil {
		b.Fatal(err)
	}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		got, err := in.Invoke("eq", 2, 3)
		if err != nil || len(got) != 1 || got[0] != 1 {
			b.Fatalf("eq(2,3) = %v, %v", got, err)
		}
	}
}
