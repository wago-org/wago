package wago

import (
	"strings"
	"testing"
	"unsafe"

	"github.com/wago-org/wago/src/core/runtime/gc"
)

func newGCExternConversionFixture(t testing.TB) (*referenceStore, *gc.Collector, *gcExternConversionState, gc.Ref) {
	t.Helper()
	desc, err := gc.NewStructDesc(0, nil)
	if err != nil {
		t.Fatal(err)
	}
	collector, err := gc.NewCollector(gc.Config{}, []gc.TypeDesc{desc})
	if err != nil {
		t.Fatal(err)
	}
	object, err := collector.NewStructDefaultWithRoots(0, gc.EmptyRoots{})
	if err != nil {
		t.Fatal(err)
	}
	store := newReferenceStore(true)
	state, err := newGCExternConversionState(store, collector)
	if err != nil {
		t.Fatal(err)
	}
	return store, collector, state, object
}

func TestGCExternConversionStableIdentityAndOwnership(t *testing.T) {
	store, collector, state, object := newGCExternConversionFixture(t)
	defer collector.Close()
	defer state.close()

	foreign, err := store.issueExternref("foreign")
	if err != nil {
		t.Fatal(err)
	}
	foreignAny, err := state.anyFromExtern(foreign)
	if err != nil {
		t.Fatal(err)
	}
	if foreignAny == 0 || foreignAny>>32 == 0 || foreignAny == foreign {
		t.Fatalf("foreign any identity = %#x from extern %#x", foreignAny, foreign)
	}
	if again, err := state.anyFromExtern(foreign); err != nil || again != foreignAny {
		t.Fatalf("repeated any.convert_extern = %#x, %v; want %#x", again, err, foreignAny)
	}
	if roundTrip, err := state.externFromAny(foreignAny); err != nil || roundTrip != foreign {
		t.Fatalf("foreign extern round trip = %#x, %v; want %#x", roundTrip, err, foreign)
	}
	if ok, err := state.isForeignAny(foreignAny); err != nil || !ok {
		t.Fatalf("isForeignAny = %v, %v; want true", ok, err)
	}

	i31 := gc.I31New(-7)
	i31Extern, err := state.externFromAny(uint64(i31))
	if err != nil {
		t.Fatal(err)
	}
	if i31Extern == 0 || i31Extern>>32 == 0 || i31Extern == uint64(i31) {
		t.Fatalf("i31 extern identity = %#x", i31Extern)
	}
	if again, err := state.externFromAny(uint64(i31)); err != nil || again != i31Extern {
		t.Fatalf("repeated extern.convert_any(i31) = %#x, %v; want %#x", again, err, i31Extern)
	}
	if roundTrip, err := state.anyFromExtern(i31Extern); err != nil || roundTrip != uint64(i31) {
		t.Fatalf("i31 any round trip = %#x, %v; want %#x", roundTrip, err, i31)
	}

	objectExtern, err := state.externFromAny(uint64(object))
	if err != nil {
		t.Fatal(err)
	}
	if roundTrip, err := state.anyFromExtern(objectExtern); err != nil || roundTrip != uint64(object) {
		t.Fatalf("object any round trip = %#x, %v; want %#x", roundTrip, err, object)
	}
	var rooted gc.Ref
	for i := uint8(0); i < state.count; i++ {
		entry := state.entries[i]
		if entry.kind == gcExternConversionData && entry.ref == object {
			if !entry.hasRoot {
				t.Fatal("object conversion has no collector root")
			}
			rooted, err = collector.CheckedTableSlot(entry.rootSlot)
			if err != nil {
				t.Fatal(err)
			}
		}
	}
	if rooted != object {
		t.Fatalf("converted object root = %#x, want %#x", rooted, object)
	}
	if err := collector.CollectFull(nil); err != nil {
		t.Fatal(err)
	}
	if stats := collector.Stats(); stats.LiveObjects != 1 {
		t.Fatalf("converted object collection live objects = %d, want 1", stats.LiveObjects)
	}

	if allocs := testing.AllocsPerRun(1000, func() {
		if got, err := state.externFromAny(uint64(object)); err != nil || got != objectExtern {
			panic("unstable object conversion")
		}
		if got, err := state.anyFromExtern(objectExtern); err != nil || got != uint64(object) {
			panic("unstable object reverse conversion")
		}
	}); allocs != 0 {
		t.Fatalf("stable object round trip allocations = %v, want 0", allocs)
	}

	t.Logf("GC extern conversion layouts: state=%d entry=%d capacity=%d", unsafe.Sizeof(gcExternConversionState{}), unsafe.Sizeof(gcExternConversionEntry{}), maxGCExternConversions)
}

func TestGCExternConversionRejectsForeignForgedFullAndClosed(t *testing.T) {
	_, collector, state, object := newGCExternConversionFixture(t)
	defer collector.Close()

	foreignStore := newReferenceStore(true)
	foreign, err := foreignStore.issueExternref("other-store")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := state.anyFromExtern(foreign); err == nil || !strings.Contains(err.Error(), "foreign externref") {
		t.Fatalf("foreign any.convert_extern error = %v", err)
	}
	if _, err := state.anyFromExtern(0xfedcba9876543210); err == nil {
		t.Fatal("forged externref token converted")
	}
	if _, err := state.externFromAny(0xfedcba9876543210); err == nil || !strings.Contains(err.Error(), "forged internal anyref") {
		t.Fatalf("forged anyref error = %v", err)
	}
	if _, err := state.externFromAny(uint64(gc.Ref(0xfffe))); err == nil {
		t.Fatal("forged compact object converted")
	}

	for i := int32(0); i < maxGCExternConversions; i++ {
		if _, err := state.externFromAny(uint64(gc.I31New(i))); err != nil {
			t.Fatalf("fill conversion %d: %v", i, err)
		}
	}
	before := state.count
	if _, err := state.externFromAny(uint64(object)); err == nil || !strings.Contains(err.Error(), "capacity") {
		t.Fatalf("capacity error = %v", err)
	}
	if state.count != before {
		t.Fatalf("rejected conversion changed count %d -> %d", before, state.count)
	}

	if err := state.close(); err != nil {
		t.Fatal(err)
	}
	if state.count != 0 || state.store != nil || state.collector != nil {
		t.Fatalf("closed conversion state retained ownership: count=%d store=%p collector=%p", state.count, state.store, state.collector)
	}
	if _, err := state.anyFromExtern(0); err == nil || !strings.Contains(err.Error(), "closed") {
		t.Fatalf("closed any.convert_extern error = %v", err)
	}
	if _, err := state.externFromAny(0); err == nil || !strings.Contains(err.Error(), "closed") {
		t.Fatalf("closed extern.convert_any error = %v", err)
	}
	if err := state.close(); err != nil {
		t.Fatalf("idempotent close: %v", err)
	}
}

func TestGCExternConversionCollectorFirstCloseFailsClosed(t *testing.T) {
	_, collector, state, object := newGCExternConversionFixture(t)
	if _, err := state.externFromAny(uint64(object)); err != nil {
		t.Fatal(err)
	}
	collector.Close()
	if err := state.close(); err == nil || !strings.Contains(err.Error(), "collector closed") {
		t.Fatalf("conversion close after collector = %v, want collector-closed error", err)
	}
	if _, err := state.externFromAny(uint64(object)); err == nil || !strings.Contains(err.Error(), "closed") {
		t.Fatalf("conversion after failed close = %v, want closed", err)
	}
}

func BenchmarkGCExternConversionRoundTrip(b *testing.B) {
	_, collector, state, object := newGCExternConversionFixture(b)
	defer collector.Close()
	defer state.close()
	extern, err := state.externFromAny(uint64(object))
	if err != nil {
		b.Fatal(err)
	}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		gotExtern, err := state.externFromAny(uint64(object))
		if err != nil || gotExtern != extern {
			b.Fatalf("extern conversion = %#x, %v", gotExtern, err)
		}
		gotAny, err := state.anyFromExtern(extern)
		if err != nil || gotAny != uint64(object) {
			b.Fatalf("any conversion = %#x, %v", gotAny, err)
		}
	}
}
