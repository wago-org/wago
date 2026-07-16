//go:build linux && amd64 && !tinygo && !wago_guardpage

package wago

import (
	"context"
	"encoding/binary"
	"encoding/hex"
	"strings"
	"testing"

	"github.com/wago-org/wago/src/core/runtime/gc"
)

const stagedGCArrayPackedDataHex = "0061736d0100000001b3808080000a5e78005e7801600001640060027f6400017f60017f017f60037f64017f017f60027f7f017f6001646a017f6000017f600000038c808080000b020203040304050607080907c28080800007036e657700000c6e65772d6f766572666c6f770001056765745f750003056765745f730005077365745f6765740007036c656e00090964726f705f73656773000a0c8180808000010aa9818080000b8a808080000041014103fb0900000b928080800000418080808078418080808078fb0900000b89808080000020012000fb0d000b8880808000002000100010020b89808080000020012000fb0c000b8880808000002000100010040b928080800000200120002002fb0e0120012000fb0d010b908080800000200041014103fb090100200110060b8680808000002000fb0f0b868080800000100010080b858080800000fc09000b0b8880808000010105000102ff04"

func stagedGCArrayPackedDataBytes(t testing.TB) []byte {
	t.Helper()
	data, err := hex.DecodeString(stagedGCArrayPackedDataHex)
	if err != nil {
		t.Fatal(err)
	}
	return data
}

func TestStagedGCArrayPackedDataOfficialProduct(t *testing.T) {
	data := stagedGCArrayPackedDataBytes(t)
	if _, err := Compile(NewRuntimeConfig(), data); err == nil {
		t.Fatal("public compile unexpectedly admitted packed GC array data instructions")
	}
	profiles := []struct {
		name string
		cfg  GCConfig
	}{
		{name: "throughput", cfg: GCConfig{CollectEveryAlloc: true, StressNurseryBytes: 48, VerifyAfterCollect: true}},
		{name: "tiny", cfg: GCConfig{Profile: GCProfileTiny, TinyHeapBytes: 48, TinyBlockBytes: 8, TinyCollectEveryAlloc: true, VerifyAfterCollect: true}},
	}
	for _, tc := range profiles {
		t.Run(tc.name, func(t *testing.T) {
			c, err := compileStagedGCArray(data)
			if err != nil {
				t.Fatal(err)
			}
			defer c.Close()
			if c.stagedGCArrayProduct() != stagedGCArrayProductPackedData || !c.usesGCArrayHelpers() || len(c.PassiveData) != 1 {
				t.Fatalf("packed product/helper/data = %v/%v/%d", c.stagedGCArrayProduct(), c.usesGCArrayHelpers(), len(c.PassiveData))
			}
			in, err := instantiateCore(c, InstantiateOptions{GC: tc.cfg})
			if err != nil {
				t.Fatal(err)
			}
			defer in.Close()
			if len(in.passiveDataDesc) != 16 || binary.LittleEndian.Uint32(in.passiveDataDesc[8:]) != 5 {
				t.Fatalf("passive data descriptor = %x", in.passiveDataDesc)
			}

			raw, err := in.Invoke("new")
			if err != nil || len(raw) != 1 || raw[0] == 0 || raw[0]>>32 == 0 {
				t.Fatalf("new = %v, %v; want one public GC token", raw, err)
			}
			token := raw[0]
			exact, owner, _, ok := in.refStore.gcRefExactType(token)
			if !ok || owner != in || exact.Kind != ValueTypeReference || !exact.Ref.Exact || !exact.Ref.Heap.Defined || exact.Ref.Heap.TypeIndex != 0 {
				t.Fatalf("packed exact token = %#v owner=%p ok=%v", exact, owner, ok)
			}
			if _, err := in.Invoke("new"); err == nil || !strings.Contains(err.Error(), "one live token") {
				t.Fatalf("second live packed token = %v", err)
			}
			if err := in.ReleaseGCRef(ValueOf(ValAnyRef, token).GCRef()); err != nil {
				t.Fatal(err)
			}
			values, err := in.Call(context.Background(), "new")
			if err != nil || len(values) != 1 || values[0].GCRef().IsNull() {
				t.Fatalf("Call new = %v, %v", values, err)
			}
			if err := in.ReleaseGCRef(values[0].GCRef()); err != nil {
				t.Fatal(err)
			}

			if got, err := in.Invoke("get_u", 2); err != nil || len(got) != 1 || got[0] != 0xff {
				t.Fatalf("get_u = %v, %v; want [255]", got, err)
			}
			if got, err := in.Invoke("get_s", 2); err != nil || len(got) != 1 || got[0] != uint64(^uint32(0)) {
				t.Fatalf("get_s = %v, %v; want [-1]", got, err)
			}
			if got, err := in.Invoke("set_get", 1, 7); err != nil || len(got) != 1 || got[0] != 7 {
				t.Fatalf("set_get = %v, %v; want [7]", got, err)
			}
			if got, err := in.Invoke("len"); err != nil || len(got) != 1 || got[0] != 3 {
				t.Fatalf("len = %v, %v; want [3]", got, err)
			}

			beforeOverflow := in.gc.Stats()
			if _, err := in.Invoke("new-overflow"); err == nil {
				t.Fatal("overflowing array.new_data succeeded")
			}
			afterOverflow := in.gc.Stats()
			if afterOverflow.Allocations != beforeOverflow.Allocations || afterOverflow.LiveObjects != beforeOverflow.LiveObjects {
				t.Fatalf("overflowing array.new_data changed collector state: before=%+v after=%+v", beforeOverflow, afterOverflow)
			}
			if _, err := in.Invoke("get_u", 10); err == nil {
				t.Fatal("out-of-bounds packed get_u succeeded")
			}
			if _, err := in.Invoke("get_s", 10); err == nil {
				t.Fatal("out-of-bounds packed get_s succeeded")
			}
			if _, err := in.Invoke("set_get", 10, 7); err == nil {
				t.Fatal("out-of-bounds packed set succeeded")
			}

			if got, err := in.Invoke("drop_segs"); err != nil || len(got) != 0 {
				t.Fatalf("drop_segs = %v, %v", got, err)
			}
			if binary.LittleEndian.Uint32(in.passiveDataDesc[8:]) != 0 {
				t.Fatalf("dropped passive data descriptor = %x", in.passiveDataDesc)
			}
			beforeDroppedTraps := in.gc.Stats()
			if _, err := in.Invoke("new"); err == nil {
				t.Fatal("array.new_data after data.drop succeeded")
			}
			if _, err := in.Invoke("new-overflow"); err == nil {
				t.Fatal("overflowing array.new_data after data.drop succeeded")
			}
			afterDroppedTraps := in.gc.Stats()
			if afterDroppedTraps.Allocations != beforeDroppedTraps.Allocations || afterDroppedTraps.LiveObjects != beforeDroppedTraps.LiveObjects {
				t.Fatalf("dropped-segment traps changed collector state: before=%+v after=%+v", beforeDroppedTraps, afterDroppedTraps)
			}

			var zero [1]uint64
			in.dispatchGCArrayHelper(gcArrayAllocData, []uint64{0, 0, 0, 0}, zero[:])
			zeroRef := gc.Ref(uint32(zero[0]))
			if length, err := in.gc.ArrayLen(zeroRef); err != nil || length != 0 {
				t.Fatalf("zero-length array.new_data after drop = %d, %v", length, err)
			}
		})
	}
}

func TestStagedGCArrayPackedDataTinyCodecAndAllocation(t *testing.T) {
	data := stagedGCArrayPackedDataBytes(t)
	c, err := compileStagedGCArray(data)
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	for i := 0; i < 2; i++ {
		in, err := instantiateCore(c, InstantiateOptions{GC: GCConfig{Profile: GCProfileTiny, TinyHeapBytes: 16, TinyBlockBytes: 8}})
		if err != nil {
			t.Fatal(err)
		}
		if _, err := in.Invoke("new"); err == nil || !strings.Contains(err.Error(), "tiny heap exhausted") {
			_ = in.Close()
			t.Fatalf("packed Tiny exhaustion %d = %v", i, err)
		}
		if err := in.Close(); err != nil {
			t.Fatal(err)
		}
	}

	fast, err := instantiateCore(c, InstantiateOptions{})
	if err != nil {
		t.Fatal(err)
	}
	defer fast.Close()
	allocs := testing.AllocsPerRun(1000, func() {
		if got, err := fast.Invoke("get_u", 2); err != nil || len(got) != 1 || got[0] != 0xff {
			panic("packed array helper failed")
		}
	})
	if allocs != 0 {
		t.Fatalf("packed array helper allocations = %v, want 0", allocs)
	}

	blob, err := marshalCompiled(c)
	if err != nil {
		t.Fatal(err)
	}
	var loaded Compiled
	if err := unmarshalCompiled(&loaded, blob[5:]); err != nil {
		t.Fatal(err)
	}
	defer loaded.Close()
	if loaded.usesGCArrayHelpers() || loaded.stagedGCArrayProduct() != 0 {
		t.Fatal("codec reload inherited packed array helper/product admission")
	}
}

func BenchmarkStagedGCArrayPackedDataGetU(b *testing.B) {
	data := stagedGCArrayPackedDataBytes(b)
	c, err := compileStagedGCArray(data)
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
		if got, err := in.Invoke("get_u", 2); err != nil || len(got) != 1 || got[0] != 0xff {
			b.Fatalf("get_u = %v, %v", got, err)
		}
	}
}
