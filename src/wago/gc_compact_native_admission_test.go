//go:build linux && amd64 && wago_gc_compact_reads

package wago

import "testing"

func TestCompactNativeReceiptIsolation(t *testing.T) {
	for _, mode := range []string{"version", "mixed", "direct", "helper", "frozen"} {
		t.Run(mode, func(t *testing.T) {
			c, e := Compile(NewRuntimeConfig().WithCoreFeatures(CoreFeaturesV3), helperInitializerRewriteModule("struct"))
			if e != nil {
				t.Fatal(e)
			}
			defer c.Close()
			rt := NewRuntime()
			defer rt.Close()
			v := c.executionView()
			switch mode {
			case "version":
				v.gcCompactNativeVersion = 2
			case "mixed":
				v.gcCompactHelperVersion = 1
			case "direct":
				v.gcDirectReadABIVersion = 1
			case "helper":
				v.gcHelperOnlyReceipt = true
			case "frozen":
				v.gcCompactNativeVersion = 0
			}
			if in, e := Instantiate(c, InstantiateOptions{store: rt.refStore, GC: GCConfig{ExperimentalCompactOffsets: true}}); e == nil {
				in.Close()
				t.Fatal("incompatible receipt admitted")
			}
		})
	}
}
