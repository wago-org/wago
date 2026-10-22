//go:build linux && riscv64 && !tinygo && !wago_target_tinygo && !wago_precompiled

package wago

import (
	"reflect"
	"testing"
)

func TestRV64SharedI31TableCollectorFreeArtifact(t *testing.T) {
	cfg := NewRuntimeConfig().WithCoreFeatures(CoreFeaturesV3)
	providerBase, err := Compile(cfg, gcSharedI31TableModule(false))
	if err != nil {
		t.Fatal(err)
	}
	defer providerBase.Close()
	consumerBase, err := Compile(cfg, gcSharedI31TableModule(true))
	if err != nil {
		t.Fatal(err)
	}
	defer consumerBase.Close()

	for _, codec := range []bool{false, true} {
		t.Run(map[bool]string{false: "compiled", true: "codec"}[codec], func(t *testing.T) {
			providerCode, consumerCode := providerBase, consumerBase
			if codec {
				providerCode = publicArtifactRoundTrip(t, providerBase)
				consumerCode = publicArtifactRoundTrip(t, consumerBase)
				defer providerCode.Close()
				defer consumerCode.Close()
			}
			for name, code := range map[string]*Compiled{"provider": providerCode, "consumer": consumerCode} {
				if code.nativeGCABIRequirement() != 0 || code.genericGCFrameRoots() != nil || code.stagedGCI31Product() != stagedGCI31ProductCore || !rv64GeneralI31Contract(code.executionView()) {
					t.Fatalf("%s lost collector-free general-i31 contract", name)
				}
			}

			store := newReferenceStore(false)
			defer store.closeRuntime()
			gcConfig := GCConfig{DisableCollection: true}
			provider, err := instantiateCore(providerCode, InstantiateOptions{GC: gcConfig, store: store})
			if err != nil {
				t.Fatal(err)
			}
			table, err := provider.ExportedTable("values")
			if err != nil {
				provider.Close()
				t.Fatal(err)
			}
			consumer, err := instantiateCore(consumerCode, InstantiateOptions{GC: gcConfig, store: store, Imports: testImports("provider.values", table)})
			if err != nil {
				provider.Close()
				t.Fatal(err)
			}
			defer consumer.Close()
			if provider.gc != nil || consumer.gc != nil {
				provider.Close()
				t.Fatal("shared i31 table acquired a collector")
			}

			if got, err := provider.Invoke("set", 0, 55); err != nil || !reflect.DeepEqual(got, []uint64{55}) {
				provider.Close()
				t.Fatalf("provider set = %v, %v", got, err)
			}
			if got, err := consumer.Invoke("read", 0); err != nil || !reflect.DeepEqual(got, []uint64{55}) {
				provider.Close()
				t.Fatalf("consumer read = %v, %v", got, err)
			}
			if got, err := consumer.Invoke("set", 0, 61); err != nil || !reflect.DeepEqual(got, []uint64{61}) {
				provider.Close()
				t.Fatalf("consumer set = %v, %v", got, err)
			}
			if got, err := provider.Invoke("read", 0); err != nil || !reflect.DeepEqual(got, []uint64{61}) {
				provider.Close()
				t.Fatalf("provider read = %v, %v", got, err)
			}
			if err := provider.Close(); err != nil {
				t.Fatal(err)
			}
			if got, err := consumer.Invoke("read", 0); err != nil || !reflect.DeepEqual(got, []uint64{61}) {
				t.Fatalf("consumer read after provider close = %v, %v", got, err)
			}
		})
	}
}
