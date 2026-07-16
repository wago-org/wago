//go:build linux && amd64 && !tinygo && !wago_guardpage

package wago

import (
	"strings"
	"testing"
	"unsafe"

	"github.com/wago-org/wago/src/core/runtime/gc"
)

func compileStagedGCTypeSubtypingProductForTest(data []byte) (*Compiled, error) {
	cfg := NewRuntimeConfig()
	features := cfg.frontendFeatures()
	features.TypedFunctionReferences = true
	features.GCTypeSubtypingProducts = true
	return compileWithFrontendFeatures(cfg, data, features)
}

func TestStagedGCTypeSubtypingProductsCompile(t *testing.T) {
	if got := unsafe.Sizeof(compiledCodeCache{}); got != 64 {
		t.Fatalf("compiledCodeCache size = %d, want 64 bytes", got)
	}
	wantCodeBytes := []int{0, 0, 0, 0, 0, 0, 632, 592}
	wantCodecBytes := []int{348, 384, 346, 218, 237, 385, 1018, 1127}
	for i, pin := range stagedGCTypeSubtypingProductPins {
		t.Run(pin.Filename, func(t *testing.T) {
			data := stagedGCTypeSubtypingProductData(t, pin)
			if _, err := Compile(NewRuntimeConfig(), data); err == nil || !strings.Contains(err.Error(), "gc type") {
				t.Fatalf("public compile = %v, want closed GC type gate", err)
			}
			c, err := compileStagedGCTypeSubtypingProductForTest(data)
			if err != nil {
				t.Fatalf("staged compile: %v", err)
			}
			defer c.Close()
			if c.stagedGCTypeSubtypingProduct() != pin.Class || c.collectorFreeStructuralMetadata() || c.stagedFeatures()&CoreFeatureGC == 0 {
				t.Fatalf("product/legacy-marker/features = %v/%v/%v", c.stagedGCTypeSubtypingProduct(), c.collectorFreeStructuralMetadata(), c.stagedFeatures())
			}
			if got := compiledStructuralRequiredFeatures(c); got&CoreFeatureGC == 0 {
				t.Fatalf("required features = %v, want GC", got)
			}
			if pin.Class == stagedGCTypeSubtypingDeclarations && !gc.HasHeapObjectTypes(c.GCTypeDescs) {
				t.Fatalf("declaration product lost heap-object descriptors: %#v", c.GCTypeDescs)
			}
			in, err := instantiateCore(c, InstantiateOptions{})
			if err != nil {
				t.Fatalf("instantiate: %v", err)
			}
			if in.gc != nil {
				t.Fatal("no-object type-subtyping product allocated a collector")
			}
			if err := in.Close(); err != nil {
				t.Fatal(err)
			}
			blob, err := marshalCompiled(c)
			if err != nil {
				t.Fatalf("marshal codec-v27: %v", err)
			}
			if len(c.Code) != wantCodeBytes[i] || len(blob) != wantCodecBytes[i] {
				t.Fatalf("product sizes code/codec = %d/%d, want %d/%d", len(c.Code), len(blob), wantCodeBytes[i], wantCodecBytes[i])
			}
			t.Logf("product size: wasm=%d code=%d codec=%d types=%d gcdescs=%d", len(data), len(c.Code), len(blob), len(c.Types), len(c.GCTypeDescs))
			var loaded Compiled
			if err := unmarshalCompiled(&loaded, blob[5:]); err != nil {
				t.Fatalf("private codec-v27 reload: %v", err)
			}
			defer loaded.Close()
			if loaded.stagedGCTypeSubtypingProduct() != 0 || loaded.stagedFeatures().IsEnabled(CoreFeatureGC) {
				t.Fatalf("codec inherited type-subtyping admission: product=%v features=%v", loaded.stagedGCTypeSubtypingProduct(), loaded.stagedFeatures())
			}
			if _, err := instantiateCore(&loaded, InstantiateOptions{}); err == nil || !strings.Contains(err.Error(), "required feature") {
				t.Fatalf("codec-loaded instantiate = %v, want required-feature rejection", err)
			}
			var public Compiled
			if err := public.UnmarshalBinary(blob); err == nil || !strings.Contains(err.Error(), "unknown required feature bits") {
				t.Fatalf("public codec load = %v, want unsupported GC gate", err)
			}
			if _, err := Capture(c, SnapshotOptions{}); err == nil || !strings.Contains(err.Error(), "WasmGC reference products") {
				t.Fatalf("snapshot capture = %v, want explicit GC state gate", err)
			}
		})
	}
}
