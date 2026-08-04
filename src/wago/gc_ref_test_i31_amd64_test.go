//go:build linux && amd64 && !tinygo && !wago_guardpage

package wago

import (
	"encoding/hex"
	"strings"
	"testing"
)

const stagedGCI31RefTestHex = "0061736d01000000018a80808000026000017f60017f017f038880808000070000010101010107ba8080800007096e756c6c2d646174610000086e756c6c2d693331000103693331000203616e790003026571000406737472756374000505617272617900060a9381808000078d8080800000d06efb146ed06efb156e6a0b8d8080800000d06cfb146cd06cfb156c6a0b9180808000002000fb1cfb146c2000fb1cfb156c6a0b9180808000002000fb1cfb146e2000fb1cfb156e6a0b9180808000002000fb1cfb146d2000fb1cfb156d6a0b9180808000002000fb1cfb146b2000fb1cfb156b6a0b9180808000002000fb1cfb146a2000fb1cfb156a6a0b"

func stagedGCI31RefTestBytes(t testing.TB) []byte {
	t.Helper()
	data, err := hex.DecodeString(stagedGCI31RefTestHex)
	if err != nil {
		t.Fatal(err)
	}
	return data
}

func TestStagedGCI31RefTestExecution(t *testing.T) {
	data := stagedGCI31RefTestBytes(t)
	if _, err := Compile(NewRuntimeConfig(), data); err == nil {
		t.Fatal("public Compile admitted staged ref.test product")
	}
	c, err := compileStagedGCI31(data)
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	if c.stagedGCI31Product() != stagedGCI31ProductRefTest || c.stagedFeatures()&CoreFeatureGC == 0 {
		t.Fatalf("ref.test product/features = %v/%v", c.stagedGCI31Product(), c.stagedFeatures())
	}
	t.Logf("i31 ref.test product: wasm=%d code=%d", len(data), len(c.Code))
	for _, tc := range []struct {
		name string
		cfg  GCConfig
	}{
		{name: "throughput", cfg: GCConfig{CollectEveryAlloc: true, VerifyAfterCollect: true}},
		{name: "tiny", cfg: GCConfig{Profile: GCProfileTiny, TinyHeapBytes: 16, TinyBlockBytes: 16, TinyCollectEveryAlloc: true, VerifyAfterCollect: true}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			in, err := instantiateCore(c, InstantiateOptions{GC: tc.cfg})
			if err != nil {
				t.Fatal(err)
			}
			defer in.Close()
			if in.gc != nil {
				t.Fatal("null/i31 ref.test product allocated a collector")
			}
			for _, call := range []struct {
				name string
				args []uint64
				want uint64
			}{
				{name: "null-data", want: 1},
				{name: "null-i31", want: 1},
				{name: "i31", args: []uint64{I32(-1)}, want: 2},
				{name: "any", args: []uint64{I32(7)}, want: 2},
				{name: "eq", args: []uint64{I32(7)}, want: 2},
				{name: "struct", args: []uint64{I32(7)}, want: 0},
				{name: "array", args: []uint64{I32(7)}, want: 0},
			} {
				got, err := in.Invoke(call.name, call.args...)
				if err != nil || len(got) != 1 || got[0] != call.want {
					t.Fatalf("%s%v=%v err=%v want=%d", call.name, call.args, got, err, call.want)
				}
			}
		})
	}
}

func TestStagedGCI31RefTestLifecycleClosure(t *testing.T) {
	data := stagedGCI31RefTestBytes(t)
	c, err := compileStagedGCI31(data)
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	blob, err := marshalCompiled(c)
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("i31 ref.test codec=%d", len(blob))
	var loaded Compiled
	if err := unmarshalCompiled(&loaded, blob[5:]); err != nil {
		t.Fatal(err)
	}
	defer loaded.Close()
	if loaded.stagedGCI31Product() != 0 || loaded.stagedFeatures().IsEnabled(CoreFeatureGC) {
		t.Fatalf("codec inherited ref.test admission: product=%v features=%v", loaded.stagedGCI31Product(), loaded.stagedFeatures())
	}
	if _, err := instantiateCore(&loaded, InstantiateOptions{}); err == nil || !strings.Contains(err.Error(), "required feature") {
		t.Fatalf("codec-loaded ref.test instantiate = %v", err)
	}

	unknown := append([]byte(nil), data...)
	for i := 0; i+9 <= len(unknown); i++ {
		if string(unknown[i:i+9]) == "null-data" {
			unknown[i+8] = 'x'
			break
		}
	}
	cfg := NewRuntimeConfig()
	features := cfg.frontendFeatures()
	features.TypedFunctionReferences = true
	features.GCI31Products = true
	widened, err := compileWithFrontendFeatures(cfg, unknown, features)
	if err != nil {
		t.Fatalf("generic i31 ref.test compile = %v", err)
	}
	_ = widened.Close()
}

func BenchmarkStagedGCI31RefTest(b *testing.B) {
	c, err := compileStagedGCI31(stagedGCI31RefTestBytes(b))
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
		if _, err := in.Invoke("i31", I32(int32(i))); err != nil {
			b.Fatal(err)
		}
	}
}
