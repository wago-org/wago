package optimization

import (
	"sync"
	"testing"

	"github.com/wago-org/wago/internal/buildfeatures"
)

func TestProposalBindingAvailability(t *testing.T) {
	old := definitions()
	catalog = []Definition{
		{Name: "scalar", Architectures: []string{"test"}},
		{Name: "vector", Architectures: []string{"test"}, requiredFeatures: buildfeatures.SIMDBit},
	}
	t.Cleanup(func() { catalog = old })

	scalar, vector := true, true
	var option Option
	specs := []BindingSpec{Bind("scalar", &scalar)}
	simd := buildfeatures.SIMD != 0
	if simd {
		specs = append(specs, BindOption("vector", &vector, &option))
	}
	bindings := NewBindings("test", specs...)
	overrides := map[string]bool{"vector": true}
	selection, err := bindings.ResolveSnapshot(overrides, Snapshot{}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if !selection.Enabled("scalar") || selection.EnabledOption(option) != simd {
		t.Fatalf("selection does not match the registered bindings")
	}
	lease, err := bindings.Apply(overrides)
	if err != nil {
		t.Fatal(err)
	}
	lease.Restore()
	if _, ok := Lookup("test", "vector"); !ok {
		t.Fatal("portable configuration lost the vector option")
	}
}

func TestResolveSnapshotIsImmutableAndDoesNotInstallOverrides(t *testing.T) {
	a, b := true, false
	bindings := selectionTestBindings(t, &a, &b)

	selection, err := bindings.ResolveSnapshot(map[string]bool{"a": false, "b": true}, Snapshot{}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if selection.Enabled("a") || !selection.Enabled("b") {
		t.Fatalf("selection = a:%v b:%v", selection.Enabled("a"), selection.Enabled("b"))
	}
	if !a || b {
		t.Fatalf("resolve mutated bindings: a=%v b=%v", a, b)
	}

	bindings.Set("a", false)
	bindings.Set("b", true)
	if selection.Enabled("a") || !selection.Enabled("b") {
		t.Fatal("resolved selection changed after process defaults changed")
	}
}

func TestResolvedOptionUsesOwnedBitWithoutNameLookup(t *testing.T) {
	a, b := true, false
	bindings := selectionTestBindings(t, &a, &b)
	selection, err := bindings.ResolveSnapshot(map[string]bool{"a": false, "b": true}, Snapshot{}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if selection.EnabledOption(bindings.Option("a")) || !selection.EnabledOption(bindings.Option("b")) {
		t.Fatal("pre-resolved option disagrees with selection")
	}

	otherA, otherB := true, false
	other := selectionTestBindings(t, &otherA, &otherB)
	if selection.EnabledOption(other.Option("b")) {
		t.Fatal("selection accepted option owned by another binding catalog")
	}
	assertPanics(t, func() { bindings.Option("missing") })
}

func TestResolvedSelectionsAreIndependentAcrossConcurrentReaders(t *testing.T) {
	a, b := true, false
	bindings := selectionTestBindings(t, &a, &b)
	one, err := bindings.ResolveSnapshot(map[string]bool{"a": true, "b": false}, Snapshot{}, nil)
	if err != nil {
		t.Fatal(err)
	}
	two, err := bindings.ResolveSnapshot(map[string]bool{"a": false, "b": true}, Snapshot{}, nil)
	if err != nil {
		t.Fatal(err)
	}

	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(2)
		go func() {
			defer wg.Done()
			for j := 0; j < 1000; j++ {
				if !one.Enabled("a") || one.Enabled("b") {
					t.Errorf("selection one changed")
					return
				}
			}
		}()
		go func() {
			defer wg.Done()
			for j := 0; j < 1000; j++ {
				if two.Enabled("a") || !two.Enabled("b") {
					t.Errorf("selection two changed")
					return
				}
			}
		}()
	}
	wg.Wait()
}

func selectionTestBindings(t *testing.T, a, b *bool) *Bindings {
	t.Helper()
	old := catalog
	catalog = []Definition{
		{Name: "a", Default: true, Architectures: []string{"test"}},
		{Name: "b", Default: false, Architectures: []string{"test"}},
	}
	t.Cleanup(func() { catalog = old })
	return NewBindings("test", Bind("a", a), Bind("b", b))
}
