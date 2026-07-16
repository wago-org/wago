//go:build linux && amd64 && !tinygo && !wago_guardpage

package wago

import (
	"encoding/binary"
	"encoding/hex"
	"strings"
	"testing"

	"github.com/wago-org/wago/src/core/compiler/frontend"
	"github.com/wago-org/wago/src/core/compiler/wasm"
	"github.com/wago-org/wago/src/core/runtime/gc"
)

const stagedGCArrayReferenceHex = "0061736d0100000001cd808080000f5e78005e6400005e6400015e6300005e6e0160000164016000016403600001640460037f7f6401017f60027f7f017f60047f7f64027f017f60037f7f7f017f6001646a017f6000017f600000038c808080000b0505060708090a0b0c0d0e07b88080800006036e657700000c6e65772d6f766572666c6f770001036765740005077365745f6765740007036c656e00090964726f705f73656773000a099680808000010564000241074103fb06000b41014102fb0800020b0abf818080000b8a808080000041004102fb0a01000b928080800000418080808078418080808078fb0a01000b8a808080000041004102fb0a03000b8a808080000041004102fb0a04000b8e808080000020022000fb0b012001fb0d000b8a808080000020002001100010040b9c80808000002002200020022003fb0b02fb0e0220022000fb0b022001fb0d000b9280808000002000200141004102fb0a0200200210060b8680808000002000fb0f0b868080800000100010080b858080800000fc0d000b"

func stagedGCArrayReferenceBytes(t testing.TB) []byte {
	t.Helper()
	data, err := hex.DecodeString(stagedGCArrayReferenceHex)
	if err != nil {
		t.Fatal(err)
	}
	return data
}

func TestStagedGCArrayReferenceElementSegmentRoots(t *testing.T) {
	data := stagedGCArrayReferenceBytes(t)
	m, err := wasm.DecodeModule(data)
	if err != nil {
		t.Fatal(err)
	}
	if err := wasm.ValidateModuleWithFeatures(m, wasm.ValidationFeatures{GCConstExpr: true}); err != nil {
		t.Fatal(err)
	}
	init, err := stagedGCArrayElementInitializer(m)
	if err != nil {
		t.Fatal(err)
	}
	if init.SegmentIndex != 0 || init.TypeID != 0 || init.Count != 2 || init.Values[0].Mode != gcArrayGlobalInitUniform || init.Values[0].Length != 3 || init.Values[0].Bits[0] != 7 || init.Values[1].Mode != gcArrayGlobalInitFixed || init.Values[1].Length != 2 || init.Values[1].Bits[0] != 1 || init.Values[1].Bits[1] != 2 {
		t.Fatalf("reference element initializer = %+v", init)
	}
	descs, err := frontend.BuildGCTypeDescs(m)
	if err != nil {
		t.Fatal(err)
	}
	profiles := []struct {
		name string
		cfg  gc.Config
	}{
		{name: "throughput", cfg: gc.Config{CollectEveryAlloc: true, StressNurseryBytes: 48, VerifyAfterCollect: true}},
		{name: "tiny", cfg: gc.Config{Profile: gc.ProfileTiny, TinyHeapBytes: 48, TinyBlockBytes: 8, TinyCollectEveryAlloc: true, VerifyAfterCollect: true}},
	}
	for _, tc := range profiles {
		t.Run(tc.name, func(t *testing.T) {
			collector, err := gc.NewCollector(tc.cfg, descs)
			if err != nil {
				t.Fatal(err)
			}
			defer collector.Close()
			descriptor := make([]byte, 16)
			state, err := instantiateGCArrayElementSegment(collector, descs, init, descriptor)
			if err != nil {
				t.Fatal(err)
			}
			if binary.LittleEndian.Uint64(descriptor) != 0 || binary.LittleEndian.Uint32(descriptor[8:]) != 2 || state.Count != 2 {
				t.Fatalf("segment descriptor/state = %x/%+v", descriptor, state)
			}
			for i := uint8(0); i < state.Count; i++ {
				rooted, err := collector.CheckedTableSlot(state.Slots[i])
				if err != nil || rooted != state.Refs[i] || !rooted.IsObj() {
					t.Fatalf("segment root %d = %v, %v; want %v", i, rooted, err, state.Refs[i])
				}
			}
			if got, err := collector.ArrayGet(state.Refs[0], 0); err != nil || got.Bits != 7 {
				t.Fatalf("uniform element = %+v, %v", got, err)
			}
			if got, err := collector.ArrayGet(state.Refs[1], 1); err != nil || got.Bits != 2 {
				t.Fatalf("fixed element = %+v, %v", got, err)
			}
			if err := collector.CollectFull(nil); err != nil {
				t.Fatal(err)
			}
			if collector.Stats().LiveObjects != 2 {
				t.Fatalf("rooted segment live objects = %d, want 2", collector.Stats().LiveObjects)
			}
			state.drop(collector)
			if binary.LittleEndian.Uint32(descriptor[8:]) != 0 {
				t.Fatalf("dropped descriptor = %x", descriptor)
			}
			for i := uint8(0); i < state.Count; i++ {
				if rooted, err := collector.CheckedTableSlot(state.Slots[i]); err != nil || !rooted.IsNull() {
					t.Fatalf("dropped segment root %d = %v, %v", i, rooted, err)
				}
			}
			if err := collector.CollectFull(nil); err != nil {
				t.Fatal(err)
			}
			if collector.Stats().LiveObjects != 0 {
				t.Fatalf("dropped segment live objects = %d, want 0", collector.Stats().LiveObjects)
			}
		})
	}
}

func TestStagedGCArrayReferenceElementSegmentTinyRollback(t *testing.T) {
	m, err := wasm.DecodeModule(stagedGCArrayReferenceBytes(t))
	if err != nil {
		t.Fatal(err)
	}
	init, err := stagedGCArrayElementInitializer(m)
	if err != nil {
		t.Fatal(err)
	}
	descs, err := frontend.BuildGCTypeDescs(m)
	if err != nil {
		t.Fatal(err)
	}
	collector, err := gc.NewCollector(gc.Config{Profile: gc.ProfileTiny, TinyHeapBytes: 24, TinyBlockBytes: 8}, descs)
	if err != nil {
		t.Fatal(err)
	}
	defer collector.Close()
	descriptor := make([]byte, 16)
	if _, err := instantiateGCArrayElementSegment(collector, descs, init, descriptor); err == nil || !strings.Contains(err.Error(), "tiny heap exhausted") {
		t.Fatalf("Tiny element rollback = %v", err)
	}
	if binary.LittleEndian.Uint32(descriptor[8:]) != 0 {
		t.Fatalf("failed descriptor = %x", descriptor)
	}
	if err := collector.CollectFull(nil); err != nil {
		t.Fatal(err)
	}
	if collector.Stats().LiveObjects != 0 {
		t.Fatalf("failed segment retained %d live objects", collector.Stats().LiveObjects)
	}
}
