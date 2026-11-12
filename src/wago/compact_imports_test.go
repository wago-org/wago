//go:build ((linux && amd64) || ((linux || darwin) && arm64)) && !tinygo

package wago

import (
	"bytes"
	"errors"
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
	"github.com/wago-org/wago/tests/support/wasmtest"
)

// Both encodings expand to two ordinary function imports. The local function
// calls them by index so execution also checks expansion order.
func compactFunctionImportsModule(sameKind bool) []byte {
	group := []byte{1, 3, 'e', 'n', 'v', 0}
	if sameKind {
		group = append(group, 0x7e, 0, 0, 2, 1, 'a', 1, 'b')
	} else {
		group = append(group, 0x7f, 2, 1, 'a', 0, 0, 1, 'b', 0, 0)
	}
	return functionImportsModule(group)
}

func functionImportsModule(group []byte) []byte {
	return wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType(nil, []wasm.ValType{wasm.I32}))),
		wasmtest.Section(2, group),
		wasmtest.Section(3, wasmtest.Vec([]byte{0})),
		wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("sum", 0, 2))),
		wasmtest.Section(10, wasmtest.Vec(wasmtest.Code([]byte{0x10, 0, 0x10, 1, 0x6a, 0x0b}))),
	)
}

func TestCompactImportsIndependentOfMultiMemory(t *testing.T) {
	for _, sameKind := range []bool{false, true} {
		name := "mixed"
		if sameKind {
			name = "same-kind"
		}
		t.Run(name, func(t *testing.T) {
			data := compactFunctionImportsModule(sameKind)
			cfg := NewRuntimeConfig().WithCoreFeatures(CoreFeaturesV2).WithBoundsChecks(BoundsChecksExplicit)
			_, err := cfg.Compile(data)
			var validation *wasm.ValidationError
			if !errors.As(err, &validation) || validation.Code != wasm.ErrUnsupportedFeature {
				t.Fatalf("disabled compact imports: %v", err)
			}
			cfg = cfg.WithFeature(CoreFeatureCompactImports, true)
			if cfg.CoreFeatures().IsEnabled(CoreFeatureMultiMemory) {
				t.Fatal("compact imports enabled multi-memory")
			}
			compiled, err := cfg.Compile(data)
			if err != nil {
				t.Fatal(err)
			}
			defer compiled.Close()
			ordinary, err := NewRuntimeConfig().WithCoreFeatures(CoreFeaturesV2).WithBoundsChecks(BoundsChecksExplicit).Compile(functionImportsModule([]byte{2, 3, 'e', 'n', 'v', 1, 'a', 0, 0, 3, 'e', 'n', 'v', 1, 'b', 0, 0}))
			if err != nil {
				t.Fatal(err)
			}
			defer ordinary.Close()
			if !bytes.Equal(compiled.code, ordinary.code) {
				t.Fatal("compact imports changed generated native code")
			}

			// The compact wire encoding is erased at compilation; an ordinary Core 2
			// runtime can execute the persisted, expanded import metadata.
			for _, product := range []*Compiled{compiled} {
				instance, err := Instantiate(product, InstantiateOptions{Imports: testImports(
					"env.a", slotHostFunc(func(_ HostModule, _, results []uint64) { results[0] = 17 }),
					"env.b", slotHostFunc(func(_ HostModule, _, results []uint64) { results[0] = 25 }),
				)})
				if err != nil {
					t.Fatal(err)
				}
				result, err := instance.Invoke("sum")
				instance.Close()
				if err != nil || len(result) != 1 || result[0] != 42 {
					t.Fatalf("sum = %v, %v", result, err)
				}
			}
		})
	}
}

func TestCompactImportFeatureDiscovery(t *testing.T) {
	if CoreFeaturesV3.IsEnabled(CoreFeatureCompactImports) {
		t.Fatal("compact imports are not part of Core 3")
	}
	if !NewRuntimeConfig().CoreFeatures().IsEnabled(CoreFeatureCompactImports) {
		t.Fatal("default omits compact imports")
	}
	for _, info := range FeatureInfos() {
		if info.Feature == CoreFeatureCompactImports {
			if info.Name != "compact-imports" || !info.Available || !info.Default {
				t.Fatalf("feature info: %+v", info)
			}
			return
		}
	}
	t.Fatal("compact imports missing from CLI feature discovery")
}

func BenchmarkCompileCompactImports(b *testing.B) {
	data := compactFunctionImportsModule(true)
	cfg := NewRuntimeConfig().WithCoreFeatures(CoreFeaturesV2 | CoreFeatureCompactImports)
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		compiled, err := cfg.Compile(data)
		if err != nil {
			b.Fatal(err)
		}
		compiled.Close()
	}
}
