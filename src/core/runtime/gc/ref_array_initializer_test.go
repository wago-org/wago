package gc

import "testing"

func refArrayInitializerCollector(t testing.TB, profile Profile) *Collector {
	t.Helper()
	leaf, err := NewStructDesc(0, nil)
	if err != nil {
		t.Fatal(err)
	}
	array, err := NewArrayDesc(1, StorageRefNull)
	if err != nil {
		t.Fatal(err)
	}
	c, err := NewCollector(Config{Profile: profile, TinyHeapBytes: 4096, TinyBlockBytes: 16}, []TypeDesc{leaf, array})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(c.Close)
	return c
}

func TestRefArrayUsesInitializerAfterRootCallback(t *testing.T) {
	for _, profile := range []Profile{ProfileThroughput, ProfileTiny} {
		c := refArrayInitializerCollector(t, profile)
		first, err := c.NewStruct(0)
		if err != nil {
			t.Fatal(err)
		}
		current, err := c.NewStruct(0)
		if err != nil {
			t.Fatal(err)
		}
		initial := Root(first)
		roots := callbackRoots(func(visit func(RootSlot) bool) {
			initial = Root(current)
			visit(&initial)
		})
		array, err := c.NewRefArrayWithRoots(1, 1, &initial, roots)
		if err != nil {
			t.Fatal(err)
		}
		got, err := c.ArrayGet(array, 0)
		if err != nil || !RefEq(got.Ref, current) {
			t.Fatalf("profile %d array initializer = %v, %v; want current root", profile, got.Ref, err)
		}
	}
}

func BenchmarkRefArrayWithRoots(b *testing.B) {
	c := refArrayInitializerCollector(b, ProfileTiny)
	initial, err := c.NewStruct(0)
	if err != nil {
		b.Fatal(err)
	}
	root := Root(initial)
	roots := Slots{&root}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := c.NewRefArrayWithRoots(1, 1, &root, roots); err != nil {
			b.Fatal(err)
		}
		if err := c.CollectFull(roots); err != nil {
			b.Fatal(err)
		}
	}
}
