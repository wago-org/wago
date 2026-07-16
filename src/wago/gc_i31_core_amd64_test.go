//go:build linux && amd64 && !tinygo && !wago_guardpage

package wago

import (
	"context"
	"encoding/hex"
	"strings"
	"testing"
)

const stagedGCI31CoreHex = "0061736d010000000199808080000560017f01646c60017f017f6000017f6000027f7f60017f00038880808000070001010202030406918080800002646c004102fb1c0b646c014103fb1c0b07cc8080800007036e65770000056765745f750001056765745f7300020a6765745f752d6e756c6c00030a6765745f732d6e756c6c00040b6765745f676c6f62616c7300050a7365745f676c6f62616c00060ad880808000078680808000002000fb1c0b8880808000002000fb1cfb1e0b8880808000002000fb1cfb1d0b868080800000d06cfb1e0b868080800000d06cfb1d0b8a80808000002300fb1e2301fb1e0b8880808000002000fb1c24010b"

func stagedGCI31CoreBytes(t testing.TB) []byte {
	t.Helper()
	data, err := hex.DecodeString(stagedGCI31CoreHex)
	if err != nil {
		t.Fatal(err)
	}
	return data
}

func TestStagedGCI31CoreExecutionAndPublicCategory(t *testing.T) {
	data := stagedGCI31CoreBytes(t)
	if _, err := Compile(NewRuntimeConfig(), data); err == nil || !strings.Contains(strings.ToLower(err.Error()), "i31") {
		t.Fatalf("public Compile i31 product = %v", err)
	}
	c, err := compileStagedGCI31(data)
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	if c.stagedGCI31Product() != stagedGCI31ProductCore || c.stagedFeatures()&CoreFeatureGC == 0 {
		t.Fatalf("i31 product/features = %v/%v", c.stagedGCI31Product(), c.stagedFeatures())
	}
	t.Logf("i31 core product: wasm=%d code=%d", len(data), len(c.Code))
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
				t.Fatal("i31-only product allocated a collector")
			}
			for _, sample := range []struct {
				in    int32
				wantU uint32
				wantS int32
			}{
				{0, 0, 0}, {100, 100, 100}, {-1, 0x7fffffff, -1},
				{0x3fffffff, 0x3fffffff, 0x3fffffff},
				{0x40000000, 0x40000000, -0x40000000},
				{int32(0x7fffffff), 0x7fffffff, -1},
				{-0x55555556, 0x2aaaaaaa, 0x2aaaaaaa},
				{-0x35555556, 0x4aaaaaaa, -0x35555556},
			} {
				got, err := in.Invoke("get_u", I32(sample.in))
				if err != nil || len(got) != 1 || uint32(got[0]) != sample.wantU {
					t.Fatalf("get_u(%#x)=%v err=%v want=%#x", uint32(sample.in), got, err, sample.wantU)
				}
				got, err = in.Invoke("get_s", I32(sample.in))
				if err != nil || len(got) != 1 || int32(got[0]) != sample.wantS {
					t.Fatalf("get_s(%#x)=%v err=%v want=%#x", uint32(sample.in), got, err, uint32(sample.wantS))
				}
			}
			for _, name := range []string{"get_u-null", "get_s-null"} {
				if _, err := in.Invoke(name); err == nil || !strings.Contains(err.Error(), "null reference") {
					t.Fatalf("%s trap = %v", name, err)
				}
			}
			if got, err := in.Invoke("get_globals"); err != nil || len(got) != 2 || got[0] != 2 || got[1] != 3 {
				t.Fatalf("initial globals=%v err=%v", got, err)
			}
			if _, err := in.Invoke("set_global", I32(1234)); err != nil {
				t.Fatal(err)
			}
			if got, err := in.Invoke("get_globals"); err != nil || len(got) != 2 || got[0] != 2 || got[1] != 1234 {
				t.Fatalf("updated globals=%v err=%v", got, err)
			}
			raw, err := in.Invoke("new", I32(-1))
			if err != nil || len(raw) != 1 || raw[0] != uint64(uint32(0xffffffff)) {
				t.Fatalf("raw new(-1)=%#v err=%v", raw, err)
			}
			values, err := in.Call(context.Background(), "new", ValueI32(-1))
			if err != nil || len(values) != 1 || values[0].Type() != ValI31Ref || values[0].I31Ref().IsNull() || values[0].I31Ref().Signed() != -1 || values[0].I31Ref().Unsigned() != 0x7fffffff {
				t.Fatalf("typed new(-1)=%v err=%v", values, err)
			}
			if err := in.ReleaseGCRef(ValueOf(ValAnyRef, raw[0]).GCRef()); err == nil {
				t.Fatal("raw i31 immediate was accepted as an opaque GCRef token")
			}
		})
	}
}

func TestStagedGCI31CoreCodecLosesAdmission(t *testing.T) {
	c, err := compileStagedGCI31(stagedGCI31CoreBytes(t))
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	blob, err := marshalCompiled(c)
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("i31 core codec=%d", len(blob))
	var loaded Compiled
	if err := unmarshalCompiled(&loaded, blob[5:]); err != nil {
		t.Fatal(err)
	}
	defer loaded.Close()
	if loaded.stagedGCI31Product() != 0 || loaded.stagedFeatures().IsEnabled(CoreFeatureGC) {
		t.Fatalf("codec inherited i31 admission: product=%v features=%v", loaded.stagedGCI31Product(), loaded.stagedFeatures())
	}
	if _, err := instantiateCore(&loaded, InstantiateOptions{}); err == nil || !strings.Contains(err.Error(), "required feature") {
		t.Fatalf("codec-loaded i31 instantiate = %v", err)
	}
}

func TestI31RefPublicValueSemantics(t *testing.T) {
	for _, v := range []int32{0, 1, -1, 0x3fffffff, 0x40000000, -0x55555556} {
		r := NewI31Ref(v)
		if r.IsNull() || r.Signed() != int32(uint32(v)<<1)>>1 || r.Unsigned() != uint32(v)&0x7fffffff {
			t.Fatalf("NewI31Ref(%#x) signed=%#x unsigned=%#x null=%t", uint32(v), uint32(r.Signed()), r.Unsigned(), r.IsNull())
		}
		value := ValueI31Ref(r)
		if value.Type() != ValI31Ref || value.I31Ref() != r || value.GCRef().token == 0 {
			t.Fatalf("ValueI31Ref(%#x)=%v", uint32(v), value)
		}
	}
	if !NullI31Ref().IsNull() {
		t.Fatal("NullI31Ref is non-null")
	}
}

func BenchmarkStagedGCI31GetU(b *testing.B) {
	c, err := compileStagedGCI31(stagedGCI31CoreBytes(b))
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
		if _, err := in.Invoke("get_u", I32(int32(i))); err != nil {
			b.Fatal(err)
		}
	}
}
