//go:build linux && amd64 && !tinygo && !wago_guardpage

package wago

import (
	"encoding/hex"
	"strings"
	"testing"
	"unsafe"
)

var stagedGCI31ProductHex = map[stagedGCI31Product]string{
	stagedGCI31ProductTable:                  "0061736d01000000019680808000046000017f60017f017f60027f7f017f60037f7f7f0003878080800006000102030303048580808000016c01030a07aa80808000060473697a6500000367657400010467726f7700020466696c6c000304636f7079000404696e6974000509af8080800002060041000b6c0341e707fb1c0b41f806fb1c0b418906fb1c0b056c0341fb00fb1c0b41c803fb1c0b419506fb1c0b0adc8080800006858080800000fc10000b88808080000020002500fb1e0b8b80808000002001fb1c2000fc0f000b8d808080000020002001fb1c2002fc11000b8c8080800000200020012002fc0e00000b8c8080800000200020012002fc0c01000b",
	stagedGCI31ProductTableGlobalInitializer: "0061736d010000000186808080000160017f017f028a808080000103656e760167037f000382808080000100048d80808000014000646c0103032300fb1c0b078780808000010367657400000a8e808080000188808080000020002500fb1e0b",
	stagedGCI31ProductGlobalInitializer:      "0061736d01000000018580808000016000017f028a808080000103656e760167037f000382808080000100068880808000016c002300fb1c0b078780808000010367657400000a8c80808000018680808000002301fb1e0b",
	stagedGCI31ProductAnyGlobal:              "0061736d01000000018a80808000026000027f7f60017f00038380808000020001069180808000026e0041d209fb1c0b6e0141ae2cfb1c0b079c80808000020b6765745f676c6f62616c7300000a7365745f676c6f62616c00010aa380808000029080808000002300fb176cfb1e2301fb176cfb1e0b8880808000002000fb1c24010b",
	stagedGCI31ProductAnyTable:               "0061736d01000000019680808000046000017f60017f017f60027f7f017f60037f7f7f0003878080800006000102030303048580808000016e01030a07aa80808000060473697a6500000367657400010467726f7700020466696c6c000304636f7079000404696e6974000509af8080800002060041000b6c0341e707fb1c0b41f806fb1c0b418906fb1c0b056c0341fb00fb1c0b41c803fb1c0b419506fb1c0b0adf8080800006858080800000fc10000b8b808080000020002500fb176cfb1e0b8b80808000002001fb1c2000fc0f000b8d808080000020002001fb1c2002fc11000b8c8080800000200020012002fc0e00000b8c8080800000200020012002fc0c01000b",
}

const stagedGCI31EnvHex = "0061736d01000000068680808000017f00412a0b0785808080000101670300"

func stagedGCI31ProductBytes(t testing.TB, product stagedGCI31Product) []byte {
	t.Helper()
	data, err := hex.DecodeString(stagedGCI31ProductHex[product])
	if err != nil {
		t.Fatal(err)
	}
	return data
}

func TestStagedGCI31RemainingProductsLifecycle(t *testing.T) {
	if got := unsafe.Sizeof(gcI31TableInitializer{}); got != 8 {
		t.Fatalf("gcI31TableInitializer size=%d, want 8", got)
	}
	if got := unsafe.Sizeof(compiledMemoryDirectory{}); got != 136 {
		t.Fatalf("compiledMemoryDirectory size=%d, want 136", got)
	}
	envBytes, err := hex.DecodeString(stagedGCI31EnvHex)
	if err != nil {
		t.Fatal(err)
	}
	envCompiled, err := Compile(NewRuntimeConfig(), envBytes)
	if err != nil {
		t.Fatal(err)
	}
	defer envCompiled.Close()
	env, err := instantiateCore(envCompiled, InstantiateOptions{})
	if err != nil {
		t.Fatal(err)
	}
	defer env.Close()
	registered := map[string]stagedSpecModule{"env": {in: env, c: envCompiled}}

	for _, product := range []stagedGCI31Product{
		stagedGCI31ProductTable,
		stagedGCI31ProductTableGlobalInitializer,
		stagedGCI31ProductGlobalInitializer,
		stagedGCI31ProductAnyGlobal,
		stagedGCI31ProductAnyTable,
	} {
		t.Run(product.String(), func(t *testing.T) {
			data := stagedGCI31ProductBytes(t, product)
			if _, err := Compile(NewRuntimeConfig(), data); err == nil {
				t.Fatal("public Compile admitted staged i31 product")
			}
			c, err := compileStagedGCI31(data)
			if err != nil {
				t.Fatal(err)
			}
			defer c.Close()
			if c.stagedGCI31Product() != product || c.stagedFeatures()&CoreFeatureGC == 0 {
				t.Fatalf("product/features=%v/%v", c.stagedGCI31Product(), c.stagedFeatures())
			}
			imports, err := stagedSpecImports(c, registered, nil)
			if err != nil {
				t.Fatal(err)
			}
			in, err := instantiateCore(c, InstantiateOptions{Imports: imports, GC: GCConfig{Profile: GCProfileTiny, TinyHeapBytes: 16, TinyBlockBytes: 16, TinyCollectEveryAlloc: true}})
			if err != nil {
				t.Fatal(err)
			}
			defer in.Close()
			if in.gc != nil {
				t.Fatal("i31 product allocated a collector")
			}
			switch product {
			case stagedGCI31ProductTable, stagedGCI31ProductAnyTable:
				if got, err := in.Invoke("get", I32(0)); err != nil || len(got) != 1 || got[0] != 999 {
					t.Fatalf("initial table get=%v err=%v", got, err)
				}
				if _, err := in.Invoke("fill", I32(0), I32(77), I32(1)); err != nil {
					t.Fatal(err)
				}
				if got, err := in.Invoke("get", I32(0)); err != nil || len(got) != 1 || got[0] != 77 {
					t.Fatalf("filled table get=%v err=%v", got, err)
				}
			case stagedGCI31ProductTableGlobalInitializer:
				if got, err := in.Invoke("get", I32(2)); err != nil || len(got) != 1 || got[0] != 42 {
					t.Fatalf("table global initializer=%v err=%v", got, err)
				}
			case stagedGCI31ProductGlobalInitializer:
				if got, err := in.Invoke("get"); err != nil || len(got) != 1 || got[0] != 42 {
					t.Fatalf("global initializer=%v err=%v", got, err)
				}
			case stagedGCI31ProductAnyGlobal:
				if got, err := in.Invoke("get_globals"); err != nil || len(got) != 2 || got[0] != 1234 || got[1] != 5678 {
					t.Fatalf("anyref globals=%v err=%v", got, err)
				}
			}
			if _, err := Capture(c, SnapshotOptions{}); err == nil || !strings.Contains(err.Error(), "WasmGC") {
				t.Fatalf("snapshot gate=%v", err)
			}
			blob, err := marshalCompiled(c)
			if err != nil {
				t.Fatal(err)
			}
			t.Logf("%s product: wasm=%d code=%d codec=%d", product, len(data), len(c.Code), len(blob))
			var loaded Compiled
			if err := unmarshalCompiled(&loaded, blob[5:]); err != nil {
				t.Fatal(err)
			}
			defer loaded.Close()
			if loaded.stagedGCI31Product() != 0 || loaded.stagedFeatures().IsEnabled(CoreFeatureGC) || (loaded.memoryDir != nil && loaded.memoryDir.gcI31TableInit != nil) {
				t.Fatalf("codec inherited i31 admission: product=%v features=%v init=%v", loaded.stagedGCI31Product(), loaded.stagedFeatures(), loaded.memoryDir.gcI31TableInit)
			}
			if _, err := instantiateCore(&loaded, InstantiateOptions{Imports: imports}); err == nil || !strings.Contains(err.Error(), "required feature") {
				t.Fatalf("codec-loaded instantiate=%v", err)
			}
		})
	}
}

func BenchmarkStagedGCI31AnyTableGet(b *testing.B) {
	c, err := compileStagedGCI31(stagedGCI31ProductBytes(b, stagedGCI31ProductAnyTable))
	if err != nil {
		b.Fatal(err)
	}
	defer c.Close()
	in, err := instantiateCore(c, InstantiateOptions{})
	if err != nil {
		b.Fatal(err)
	}
	defer in.Close()
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := in.Invoke("get", 0); err != nil {
			b.Fatal(err)
		}
	}
}
