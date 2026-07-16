//go:build linux && amd64 && !tinygo && !wago_guardpage

package wago

import (
	"encoding/binary"
	"reflect"
	"strings"
	"testing"
	"unsafe"

	coreruntime "github.com/wago-org/wago/src/core/runtime"
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
	wantCodeBytes := []int{0, 0, 0, 0, 0, 0, 632, 592, 77, 77, 77, 77, 253, 253, 178, 178, 178, 178, 215, 448, 560}
	wantCodecBytes := []int{349, 385, 347, 219, 238, 386, 1019, 1128, 499, 657, 420, 755, 598, 852, 648, 806, 648, 569, 923, 786, 1096}
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
			if pin.Class.usesRefTest() {
				wantDescBytes := (len(c.FuncTypeID) + 1) * coreruntime.FuncRefDescBytes
				if len(in.funcRefDescs) != wantDescBytes {
					t.Fatalf("ref.test descriptor arena = %d bytes, want exact %d", len(in.funcRefDescs), wantDescBytes)
				}
				got, err := in.Invoke("run")
				if err != nil || !reflect.DeepEqual(got, pin.Results) {
					t.Fatalf("run = %v, %v; want %v", got, err, pin.Results)
				}
				var invokeErr error
				allocs := testing.AllocsPerRun(1000, func() {
					got, invokeErr = in.Invoke("run")
				})
				if invokeErr != nil || !reflect.DeepEqual(got, pin.Results) || allocs != 0 {
					t.Fatalf("steady run = %v, %v, allocs=%v; want %v, nil, 0", got, invokeErr, allocs, pin.Results)
				}
			}
			if pin.Class == stagedGCTypeSubtypingRefFuncGlobals {
				wantDescBytes := (len(c.FuncTypeID) + 1) * coreruntime.FuncRefDescBytes
				if len(in.funcRefDescs) != wantDescBytes {
					t.Fatalf("descriptor arena = %d bytes, want exact %d", len(in.funcRefDescs), wantDescBytes)
				}
				for globalIndex, global := range c.Globals {
					if global.Mutable || !global.HasInitFunc {
						t.Fatalf("global %d = %+v, want immutable ref.func initializer", globalIndex, global)
					}
					off := (int(global.InitFunc) + 1) * coreruntime.FuncRefDescBytes
					want := uint64(uintptr(unsafe.Pointer(&in.funcRefDescs[off])))
					if got := readGlobalObject(in.globalCells[globalIndex], ValFuncRef); got != want {
						t.Fatalf("global %d descriptor = %#x, want local canonical %#x", globalIndex, got, want)
					}
					if got := binary.LittleEndian.Uint64(in.funcRefDescs[off+coreruntime.TableEntryRefSlotOffset:]); got != want {
						t.Fatalf("global %d descriptor identity = %#x, want self-owned %#x", globalIndex, got, want)
					}
				}
			}
			meta := (&Module{c: c}).Metadata()
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
			loadedMeta := (&Module{c: &loaded}).Metadata()
			if !reflect.DeepEqual(loadedMeta.Types, meta.Types) || !reflect.DeepEqual(loadedMeta.Functions, meta.Functions) || !reflect.DeepEqual(loadedMeta.Globals, meta.Globals) {
				t.Fatalf("codec metadata changed\n got: %#v\nwant: %#v", loadedMeta, meta)
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
