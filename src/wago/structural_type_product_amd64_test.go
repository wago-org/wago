//go:build linux && amd64 && !tinygo && !wago_guardpage

package wago

import (
	"encoding/hex"
	"reflect"
	"strings"
	"testing"

	"github.com/wago-org/wago/src/core/runtime/gc"
)

func TestStagedStructuralMetadataProducts(t *testing.T) {
	for _, pin := range stagedTypeRecLeaderPins {
		if pin.Product == stagedStructuralCallIndirect {
			continue
		}
		t.Run(pin.Filename, func(t *testing.T) {
			data, err := hex.DecodeString(pin.Hex)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := Compile(NewRuntimeConfig(), data); err == nil || !strings.Contains(err.Error(), "gc type") {
				t.Fatalf("public compile = %v, want closed GC type gate", err)
			}
			c, err := compileStagedStructuralTypeProductForTest(data)
			if err != nil {
				t.Fatalf("staged compile: %v", err)
			}
			defer c.Close()
			if !gc.HasHeapObjectTypes(c.GCTypeDescs) {
				t.Fatalf("structural metadata lost heap-object descriptors: %#v", c.GCTypeDescs)
			}
			if !c.collectorFreeStructuralMetadata() {
				t.Fatal("collector-free structural sidecar is not set")
			}
			wantFeatures := CoreFeatureGC
			if pin.Product == stagedStructuralRefFuncGlobal {
				wantFeatures |= CoreFeatureReferenceTypes | CoreFeatureTypedFunctionReferences
			}
			if got := compiledStructuralRequiredFeatures(c); got&wantFeatures != wantFeatures {
				t.Fatalf("required features = %v, want at least %v", got, wantFeatures)
			}
			meta := (&Module{c: c}).Metadata()
			blob, err := marshalCompiled(c)
			if err != nil {
				t.Fatalf("marshal codec-v27: %v", err)
			}
			var loaded Compiled
			if err := unmarshalCompiled(&loaded, blob[5:]); err != nil {
				t.Fatalf("private codec-v27 reload: %v", err)
			}
			defer loaded.Close()
			loadedMeta := (&Module{c: &loaded}).Metadata()
			if !reflect.DeepEqual(loadedMeta.Types, meta.Types) || !reflect.DeepEqual(loadedMeta.Functions, meta.Functions) || !reflect.DeepEqual(loadedMeta.Globals, meta.Globals) {
				t.Fatalf("codec metadata changed\n got: %#v\nwant: %#v", loadedMeta, meta)
			}
			var public Compiled
			if err := public.UnmarshalBinary(blob); err == nil || !strings.Contains(err.Error(), "unknown required feature bits") {
				t.Fatalf("public codec load = %v, want unsupported GC/typed feature gate", err)
			}
			if _, err := Capture(c, SnapshotOptions{}); err == nil || !strings.Contains(err.Error(), "WasmGC reference products") {
				t.Fatalf("snapshot capture = %v, want explicit GC state gate", err)
			}
		})
	}
}

func TestStagedStructuralMetadataInstantiationHasNoCollector(t *testing.T) {
	for _, pin := range stagedTypeRecLeaderPins {
		if pin.Product != stagedStructuralRefFuncGlobal {
			continue
		}
		data, err := hex.DecodeString(pin.Hex)
		if err != nil {
			t.Fatal(err)
		}
		c, err := compileStagedStructuralTypeProductForTest(data)
		if err != nil {
			t.Fatalf("%s compile: %v", pin.Filename, err)
		}
		in, err := instantiateCore(c, InstantiateOptions{})
		if err != nil {
			_ = c.Close()
			t.Fatalf("%s instantiate: %v", pin.Filename, err)
		}
		if in.gc != nil {
			t.Fatalf("%s allocated a collector for metadata-only struct definitions", pin.Filename)
		}
		_ = in.Close()
		_ = c.Close()
	}
}

func TestStagedStructuralFunctionLinkLifecycle(t *testing.T) {
	providerData, _ := hex.DecodeString(stagedTypeRecLeaderPins[3].Hex)
	consumerData, _ := hex.DecodeString(stagedTypeRecLeaderPins[4].Hex)
	mismatchData, _ := hex.DecodeString(stagedTypeRecLeaderPins[5].Hex)

	providerCompiled, err := compileStagedStructuralTypeProductForTest(providerData)
	if err != nil {
		t.Fatal(err)
	}
	defer providerCompiled.Close()
	provider, err := instantiateCore(providerCompiled, InstantiateOptions{})
	if err != nil {
		t.Fatal(err)
	}
	exported, err := provider.ExportedFunc("f")
	if err != nil {
		t.Fatal(err)
	}
	consumerCompiled, err := compileStagedStructuralTypeProductForTest(consumerData)
	if err != nil {
		t.Fatal(err)
	}
	defer consumerCompiled.Close()
	consumer, err := instantiateCore(consumerCompiled, InstantiateOptions{Imports: Imports{"M.f": exported}})
	if err != nil {
		t.Fatalf("equivalent structural import: %v", err)
	}
	if provider.gc != nil || consumer.gc != nil {
		t.Fatal("structural function link allocated a collector")
	}

	mismatchCompiled, err := compileStagedStructuralTypeProductForTest(mismatchData)
	if err != nil {
		t.Fatal(err)
	}
	defer mismatchCompiled.Close()
	if _, err := instantiateCore(mismatchCompiled, InstantiateOptions{Imports: Imports{"M.f": exported}}); err == nil || !strings.Contains(err.Error(), "signature mismatch") {
		t.Fatalf("mismatched recursive group link = %v, want exact signature rejection", err)
	}
	if err := provider.Close(); err != nil {
		t.Fatal(err)
	}
	if !provider.hasPhysicalResources() {
		t.Fatal("consumer did not retain the logically closed structural producer")
	}
	if err := consumer.Close(); err != nil {
		t.Fatal(err)
	}
	if provider.hasResourceRoots() || provider.hasPhysicalResources() {
		t.Fatal("structural producer remained retained after consumer close")
	}
}

func TestStagedStructuralCallIndirectRemainsGated(t *testing.T) {
	for _, pin := range stagedTypeRecLeaderPins {
		if pin.Product != stagedStructuralCallIndirect {
			continue
		}
		data, _ := hex.DecodeString(pin.Hex)
		if _, err := compileStagedStructuralTypeProductForTest(data); err == nil || !strings.Contains(err.Error(), "call_indirect matching remains gated") {
			t.Fatalf("%s compile = %v, want dynamic structural-call gate", pin.Filename, err)
		}
	}
}
