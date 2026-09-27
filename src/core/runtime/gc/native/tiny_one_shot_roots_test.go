package gc

import (
	"fmt"
	"testing"
)

type oneShotDirectRoots struct {
	ref   Ref
	calls int
}

func (r *oneShotDirectRoots) RangeRoots(func(RootSlot) bool) {}

func (r *oneShotDirectRoots) RangeRootRefs(sink RootRefSink) bool {
	r.calls++
	if r.calls == 1 {
		return sink.VisitRootRef(r.ref)
	}
	return true
}

func TestTinyKeepsOneShotDirectRoot(t *testing.T) {
	leaf, err := NewStructDesc(0, nil)
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name    string
		profile Profile
	}{{"tiny", ProfileTiny}, {"throughput", ProfileThroughput}} {
		t.Run(tc.name, func(t *testing.T) {
			c, err := NewCollector(Config{Profile: tc.profile, TinyHeapBytes: 4096, TinyBlockBytes: 16}, []TypeDesc{leaf})
			if err != nil {
				t.Fatal(err)
			}
			defer c.Close()
			object, err := c.NewStructDefault(0)
			if err != nil {
				t.Fatal(err)
			}
			roots := &oneShotDirectRoots{ref: object}
			if err := c.CollectFull(roots); err != nil {
				t.Fatal(err)
			}
			if !c.validObjectRef(object) {
				t.Fatalf("profile %d freed one-shot root after %d walks", tc.profile, roots.calls)
			}
		})
	}
}

type oneShotClassifiedRoots struct {
	ref   Ref
	calls int
}

func (r *oneShotClassifiedRoots) RangeRoots(func(RootSlot) bool) {}
func (r *oneShotClassifiedRoots) RangeClassifiedRootRefs(sink ClassifiedRootRefSink) bool {
	r.calls++
	if r.calls == 1 {
		return sink.VisitClassifiedRootRef(RootSnapshotTemporary, r.ref)
	}
	return true
}

func TestTinyOneShotRootsKeepGraphAndReleaseNextCycle(t *testing.T) {
	leaf, err := NewStructDesc(0, nil)
	if err != nil {
		t.Fatal(err)
	}
	parentType, err := NewStructDesc(1, []StorageKind{StorageRefNull})
	if err != nil {
		t.Fatal(err)
	}
	for _, measured := range []bool{false, true} {
		for _, form := range []string{"direct", "classified", "composite"} {
			t.Run(fmt.Sprintf("%s/telemetry=%v", form, measured), func(t *testing.T) {
				cfg := Config{Profile: ProfileTiny, TinyHeapBytes: 4096, TinyBlockBytes: 16}
				if measured {
					cfg.Telemetry = new(Telemetry)
				}
				c := newTestCollectorWithTypes(t, cfg, []TypeDesc{leaf, parentType})
				parent, err := c.NewStructDefault(1)
				if err != nil {
					t.Fatal(err)
				}
				child, err := c.NewStructDefault(0)
				if err != nil {
					t.Fatal(err)
				}
				garbage, err := c.NewStructDefault(0)
				if err != nil {
					t.Fatal(err)
				}
				if err := c.StructSet(parent, 0, RefValue(child)); err != nil {
					t.Fatal(err)
				}
				direct := &oneShotDirectRoots{ref: parent}
				classified := &oneShotClassifiedRoots{ref: parent}
				var roots RootSet = direct
				calls := &direct.calls
				if form == "classified" {
					roots, calls = classified, &classified.calls
				} else if form == "composite" {
					roots = RootGroups{{Class: RootSnapshotTemporary, Roots: combineRootSets(direct, EmptyRoots{})}}
				}
				if err := c.CollectFull(roots); err != nil {
					t.Fatal(err)
				}
				wantWalks := 1
				if tinyIncrementalBuild {
					wantWalks = 2
				} // Initial roots, then remark.
				if *calls != wantWalks {
					t.Fatalf("root walks = %d, want %d", *calls, wantWalks)
				}
				field, err := c.StructGet(parent, 0)
				if !c.validObjectRef(parent) || !c.validObjectRef(child) || c.validObjectRef(garbage) || err != nil || field.Ref != child {
					t.Fatalf("one-shot graph lost or garbage retained: field=%v error=%v", field, err)
				}
				if err := c.Verify(RefSliceRoots{parent}); err != nil {
					t.Fatal(err)
				}
				if snapshot, ok := c.TelemetrySnapshot(); ok {
					want := RootTelemetry{NativeFrames: 1}
					if form != "direct" {
						want = RootTelemetry{SnapshotTemporaries: 1}
					}
					got := snapshot.Full.Roots
					if got.NativeFrames != want.NativeFrames || got.SnapshotTemporaries != want.SnapshotTemporaries ||
						got.Globals != 0 || got.Tables != 0 || got.PublicTokens != 0 || got.ForeignInstances != 0 {
						t.Fatalf("root accounting = %+v, want counts %+v", got, want)
					}
				}
				if err := c.CollectFull(nil); err != nil {
					t.Fatal(err)
				}
				if c.validObjectRef(parent) || c.validObjectRef(child) {
					t.Fatal("staging retained roots into the next cycle")
				}
				if err := c.Verify(nil); err != nil {
					t.Fatal(err)
				}
			})
		}
	}
}

func TestTinyTransientRootBufferReuse(t *testing.T) {
	leaf, err := NewStructDesc(0, nil)
	if err != nil {
		t.Fatal(err)
	}
	c := newTestCollectorWithTypes(t, Config{Profile: ProfileTiny, TinyHeapBytes: 1 << 20, TinyBlockBytes: 16}, []TypeDesc{leaf})
	refs := make(RefSliceRoots, 4096)
	for i := range refs {
		refs[i], err = c.NewStructDefault(0)
		if err != nil {
			t.Fatal(err)
		}
	}
	var roots RootSet = refs
	if err := c.CollectFull(roots); err != nil {
		t.Fatal(err)
	}
	for _, ref := range refs {
		if !c.validObjectRef(ref) {
			t.Fatal("large root set lost an object")
		}
	}
	capacity := cap(c.markStack)
	if capacity < len(refs) || len(c.markStack) != 0 {
		t.Fatal("staging buffer was not retained empty for reuse")
	}
	if allocs := testing.AllocsPerRun(20, func() {
		if err := c.CollectFull(roots); err != nil {
			t.Fatal(err)
		}
	}); allocs != 0 {
		t.Fatalf("warm collection allocations = %v, want zero", allocs)
	}
	if cap(c.markStack) != capacity {
		t.Fatal("warm collection grew the staging buffer")
	}
	// Reuse a much smaller root set; stale buffer entries must not retain objects.
	if err := c.CollectFull(RefSliceRoots{refs[0], refs[0], Null(), I31New(7), Ref(0xfffffffe)}); err != nil {
		t.Fatal(err)
	}
	if !c.validObjectRef(refs[0]) {
		t.Fatal("small root set lost its object")
	}
	for _, ref := range refs[1:] {
		if c.validObjectRef(ref) {
			t.Fatal("stale staging entry retained an object")
		}
	}
	if err := c.Verify(RefSliceRoots{refs[0]}); err != nil {
		t.Fatal(err)
	}
}

// Cold measures allocation of only the reusable root buffer; object tracing
// storage is warmed in both cases. Roots are boxed once outside the timed loop.
func BenchmarkTinyTransientRootStaging(b *testing.B) {
	leaf, err := NewStructDesc(0, nil)
	if err != nil {
		b.Fatal(err)
	}
	for _, cold := range []bool{false, true} {
		for _, count := range []int{0, 1, 256, 4096} {
			b.Run(fmt.Sprintf("cold=%v/roots=%d", cold, count), func(b *testing.B) {
				c, err := NewCollector(Config{Profile: ProfileTiny, TinyHeapBytes: 4096, TinyBlockBytes: 16}, []TypeDesc{leaf})
				if err != nil {
					b.Fatal(err)
				}
				defer c.Close()
				refs := make(RefSliceRoots, count)
				if count != 0 {
					object, err := c.NewStructDefault(0)
					if err != nil {
						b.Fatal(err)
					}
					for i := range refs {
						refs[i] = object
					}
				}
				var roots RootSet = refs
				if err := c.CollectFull(roots); err != nil {
					b.Fatal(err)
				}
				b.ReportAllocs()
				b.ResetTimer()
				for i := 0; i < b.N; i++ {
					if cold {
						c.markStack = nil
					}
					if err := c.CollectFull(roots); err != nil {
						b.Fatal(err)
					}
				}
				b.StopTimer()
				b.ReportMetric(float64(cap(c.markStack)*4), "root-buffer-bytes")
			})
		}
	}
}

func BenchmarkTinyFullWithDirectRoot(b *testing.B) {
	leaf, err := NewStructDesc(0, nil)
	if err != nil {
		b.Fatal(err)
	}
	c, err := NewCollector(Config{Profile: ProfileTiny, TinyHeapBytes: 4096, TinyBlockBytes: 16}, []TypeDesc{leaf})
	if err != nil {
		b.Fatal(err)
	}
	defer c.Close()
	object, err := c.NewStructDefault(0)
	if err != nil {
		b.Fatal(err)
	}
	root := Root(object)
	roots := Slots{&root}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if err := c.CollectFull(roots); err != nil {
			b.Fatal(err)
		}
	}
}
