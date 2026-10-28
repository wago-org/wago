//go:build linux && riscv64 && !tinygo && !wago_target_tinygo

package wago

import (
	"context"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"slices"
	"testing"

	gc "github.com/wago-org/wago/src/core/runtime/gc/native"
)

const rv64PublicGCArrayReferenceHex = "0061736d0100000001cd808080000f5e78005e6400005e6400015e6300005e6e0160000164016000016403600001640460037f7f6401017f60027f7f017f60047f7f64027f017f60037f7f7f017f6001646a017f6000017f600000038c808080000b0505060708090a0b0c0d0e07b88080800006036e657700000c6e65772d6f766572666c6f770001036765740005077365745f6765740007036c656e00090964726f705f73656773000a099680808000010564000241074103fb06000b41014102fb0800020b0abf818080000b8a808080000041004102fb0a01000b928080800000418080808078418080808078fb0a01000b8a808080000041004102fb0a03000b8a808080000041004102fb0a04000b8e808080000020022000fb0b012001fb0d000b8a808080000020002001100010040b9c80808000002002200020022003fb0b02fb0e0220022000fb0b022001fb0d000b9280808000002000200141004102fb0a0200200210060b8680808000002000fb0f0b868080800000100010080b858080800000fc0d000b"

func rv64PublicGCArrayReferenceBytes(t testing.TB) []byte {
	t.Helper()
	b, err := hex.DecodeString(rv64PublicGCArrayReferenceHex)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func rv64PublicGCArrayReferenceCompile(t *testing.T, artifact bool) *Compiled {
	t.Helper()

	original, err := rv64I31Config().Compile(rv64PublicGCArrayReferenceBytes(t))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = original.Close() })

	return original

}

func TestRV64PublicGCArrayReferenceExecutionAndArtifact(t *testing.T) {
	data := rv64PublicGCArrayReferenceBytes(t)
	wantSource := [sha256.Size]byte{0x19, 0x17, 0x8a, 0x5d, 0xb9, 0xc6, 0xde, 0xd4, 0x1e, 0x18, 0x5a, 0x94, 0x22, 0xc5, 0x58, 0xa6, 0x5d, 0x4b, 0xc1, 0xf1, 0x1e, 0x7b, 0x0d, 0xf1, 0x1a, 0x77, 0x62, 0x26, 0xf2, 0x28, 0x12, 0xa9}
	if len(data) != 396 || sha256.Sum256(data) != wantSource {
		t.Fatal("reference array source identity changed")
	}
	for _, artifact := range []bool{false, true} {
		t.Run(map[bool]string{false: "source", true: "artifact"}[artifact], func(t *testing.T) {
			compiled := rv64PublicGCArrayReferenceCompile(t, artifact)
			view := compiled.executionView()
			if !rv64SourceContractValid(view) || view.validate() != nil {
				t.Fatal("invalid public reference array execution contract")
			}
			for _, profile := range []struct {
				name   string
				config GCConfig
			}{{name: "default"}, {name: "stress", config: GCConfig{CollectEveryAlloc: true, StressNurseryBytes: 96, ForceMajorEveryMinor: true, VerifyAfterCollect: true, StressBarriers: true}}} {
				t.Run(profile.name, func(t *testing.T) {
					rt := NewRuntime()
					defer rt.Close()
					mod, err := rt.Module(compiled)
					if err != nil {
						t.Fatal(err)
					}
					in, err := rt.Instantiate(context.Background(), mod, WithImports(NewImports()), WithGC(profile.config))
					mod.Close()
					if err != nil {
						t.Fatal(err)
					}
					defer in.Close()
					descriptor, _ := in.gcRootedTableElementDescriptor(0)
					if binary.LittleEndian.Uint32(descriptor[8:]) != 2 || len(rv64ArrayReferenceRoots(t, in)) != 2 {
						t.Fatal("passive element roots are incomplete")
					}
					raw, err := in.Invoke("new")
					if err != nil || len(raw) != 1 || raw[0] == 0 {
						t.Fatalf("new=%v err=%v", raw, err)
					}
					exact, owner, _, ok := in.refStore.gcRefExactType(raw[0])
					if !ok || owner != in || exact.Kind != ValueTypeReference || !exact.Ref.Exact || !exact.Ref.Heap.Defined || exact.Ref.Heap.TypeIndex != 1 {
						t.Fatalf("reference token type=%#v owner=%p ok=%v", exact, owner, ok)
					}
					if err = in.ReleaseGCRef(ValueOf(ValAnyRef, raw[0]).GCRef()); err != nil {
						t.Fatal(err)
					}
					for _, action := range []struct {
						name       string
						args, want []uint64
					}{
						{name: "get", args: []uint64{0xaaaa000000000000, 0xbbbb000000000000}, want: []uint64{7}},
						{name: "get", args: []uint64{0xaaaa000000000001, 0xbbbb000000000000}, want: []uint64{1}},
						{name: "set_get", args: []uint64{0xaaaa000000000000, 0xbbbb000000000001, 0xcccc000000000001}, want: []uint64{2}},
						{name: "len", want: []uint64{2}},
					} {
						got, err := in.Invoke(action.name, action.args...)
						if err != nil || !slices.Equal(got, action.want) {
							t.Fatalf("%s%v=%v err=%v", action.name, action.args, got, err)
						}
					}
					before := in.gc.Stats()
					if _, err = in.Invoke("new-overflow"); err == nil {
						t.Fatal("overflowing array.new_elem succeeded")
					}
					after := in.gc.Stats()
					if after.Allocations != before.Allocations || after.LiveObjects != before.LiveObjects {
						t.Fatalf("overflow mutated collector: before=%+v after=%+v", before, after)
					}
					if _, err = in.Invoke("get", 10, 0); err == nil {
						t.Fatal("out-of-bounds reference get succeeded")
					}
					if _, err = in.Invoke("set_get", 10, 0, 0); err == nil {
						t.Fatal("out-of-bounds reference set succeeded")
					}
					if got, err := in.Invoke("drop_segs"); err != nil || len(got) != 0 {
						t.Fatalf("drop=%v err=%v", got, err)
					}
					if binary.LittleEndian.Uint32(descriptor[8:]) != 0 {
						t.Fatalf("dropped descriptor=%x", descriptor)
					}
					if roots := rv64ArrayReferenceRoots(t, in); len(roots) != 0 {
						t.Fatal("dropped segment retained roots", roots)
					}
					if _, err = in.Invoke("drop_segs"); err != nil {
						t.Fatal("repeated drop", err)
					}
					if _, err = in.Invoke("new"); err == nil {
						t.Fatal("array.new_elem after drop succeeded")
					}
				})
			}
		})
	}
}

func TestRV64PublicGCArrayReferenceCloseDropsOnlyOwnedSegmentRoots(t *testing.T) {
	// Exercise the decoded artifact form: its generic-array execution marker
	// selects the shared Runtime collector while the exact retained witness keeps
	// this specialized native lowering admitted.
	compiled := rv64PublicGCArrayReferenceCompile(t, true)
	store := newReferenceStore(false)
	defer store.closeRuntime()
	config := GCConfig{VerifyAfterCollect: true, StressBarriers: true}
	first, err := instantiateCore(compiled, InstantiateOptions{GC: config, store: store})
	if err != nil {
		t.Fatal(err)
	}
	second, err := instantiateCore(compiled, InstantiateOptions{GC: config, store: store})
	if err != nil {
		_ = first.Close()
		t.Fatal(err)
	}
	defer second.Close()
	if first.gc == nil || first.gc != second.gc || !store.ownsGCCollector(first.gc) {
		t.Fatalf("reference array collector domains first=%p second=%p store-owned=%t needs=%t safe=%t", first.gc, second.gc, store.ownsGCCollector(first.gc), compiled.needsRuntimeGCCollectorDomain(), compiled.sharedGCPersistentDomainSafe())
	}
	firstRoots, secondRoots := rv64ArrayReferenceRoots(t, first), rv64ArrayReferenceRoots(t, second)
	if len(firstRoots) != 2 || len(secondRoots) != 2 || firstRoots[0] == secondRoots[0] || firstRoots[1] == secondRoots[1] {
		t.Fatal("element roots are not instance-owned", firstRoots, secondRoots)
	}
	collector := second.gc
	if got := collector.Stats().LiveObjects; got != 4 {
		t.Fatalf("initial live objects = %d, want 4", got)
	}
	if err = first.Close(); err != nil {
		t.Fatal(err)
	}
	if err := first.WaitClosed(context.Background()); err != nil {
		t.Fatal(err)
	}
	if !first.resourcesClosed || first.existingGCArrayElementState() != nil {
		t.Fatalf("first physical teardown retained element state: closed=%t state=%p", first.resourcesClosed, first.existingGCArrayElementState())
	}
	roots := second.gcCollectFrameRoots(second.publicGCState())
	if err = collector.CollectFull(&roots); err != nil {
		t.Fatal(err)
	}
	if got := collector.Stats().LiveObjects; got != 2 {
		t.Fatalf("live objects after closed-instance collection = %d, want 2", got)
	}
	for i, rooted := range rv64ArrayReferenceRoots(t, second) {
		value, err := collector.ArrayGet(rooted, 0)
		if want := []uint64{7, 1}[i]; err != nil || value.Bits != want {
			t.Fatal("surviving element value", i, value, err)
		}
	}
}

func rv64ArrayReferenceRoots(t *testing.T, in *Instance) []gc.Ref {
	t.Helper()
	var roots []gc.Ref
	in.rangeLocalGCTableRoots(func(slot gc.RootSlot) bool {
		ref := slot.GetRef()
		if ref.IsNull() {
			t.Fatal("live passive element contains a null root")
		}
		roots = append(roots, ref)
		return true
	}, nil)
	return roots
}
