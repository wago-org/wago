//go:build linux && amd64 && !tinygo && !wago_guardpage

package wago

import (
	"os"
	"path/filepath"
	"testing"
)

func stagedGCBrOnCastBytes(t testing.TB, base string, class stagedGCBrOnCastClass) []byte {
	t.Helper()
	var script stagedSpecScript
	tmp := stagedOfficialTypedReferenceJSON(t, base, &script)
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
			pin, ok := stagedGCBrOnCastLeaderPinFor(base, latest, cmd.Line)
			if ok && pin.Class == class {
				return append([]byte(nil), latest...)
			}
		}
	}
	t.Fatalf("official %s %s leader not found", base, class)
	return nil
}

func stagedGCBrOnCastProductFor(base string, class stagedGCBrOnCastClass) stagedGCStructProduct {
	if base == "gc/br_on_cast" {
		switch class {
		case stagedGCBrOnCastAbstract:
			return stagedGCStructBrOnCastAbstract
		case stagedGCBrOnCastConcrete:
			return stagedGCStructBrOnCastConcrete
		case stagedGCBrOnCastNullability:
			return stagedGCStructBrOnCastNullability
		}
	}
	switch class {
	case stagedGCBrOnCastAbstract:
		return stagedGCStructBrOnCastFailAbstract
	case stagedGCBrOnCastConcrete:
		return stagedGCStructBrOnCastFailConcrete
	case stagedGCBrOnCastNullability:
		return stagedGCStructBrOnCastFailNullability
	default:
		return 0
	}
}

func compileStagedGCBrOnCastProduct(data []byte) (*Compiled, error) {
	cfg := compatibilityDefaultConfig()
	features := cfg.frontendFeatures()
	features.TypedFunctionReferences = true
	features.GCStructProducts = true
	if product, ok := stagedGCStructExecutionProduct(data); ok && (product == stagedGCStructBrOnCastAbstract || product == stagedGCStructBrOnCastFailAbstract) {
		features.GCArrayProducts = true
		features.GCI31Products = true
	}
	return compileWithFrontendFeatures(cfg, data, features)
}

func BenchmarkStagedGCBrOnCastStableI31(b *testing.B) {
	data := stagedGCBrOnCastBytes(b, "gc/br_on_cast", stagedGCBrOnCastAbstract)
	c, err := compileStagedGCBrOnCastProduct(data)
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
		got, err := in.Invoke("br_on_i31", 1)
		if err != nil || len(got) != 1 || uint32(got[0]) != 7 {
			b.Fatalf("br_on_i31(1) = %v, %v", got, err)
		}
	}
}
