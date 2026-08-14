package optimization

import (
	"fmt"
	"sync"
	"testing"
)

func TestResolveIsImmutableAndDoesNotInstallOverrides(t *testing.T) {
	a, b := true, false
	bindings := selectionTestBindings(t, &a, &b)

	selection, err := bindings.Resolve(map[string]bool{"a": false, "b": true})
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
	selection, err := bindings.Resolve(map[string]bool{"a": false, "b": true})
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
	one, err := bindings.Resolve(map[string]bool{"a": true, "b": false})
	if err != nil {
		t.Fatal(err)
	}
	two, err := bindings.Resolve(map[string]bool{"a": false, "b": true})
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

func TestSelectionUsesBothBoundedWords(t *testing.T) {
	old := catalog
	values := make([]bool, 65)
	specs := make([]BindingSpec, len(values))
	catalog = make([]Definition, len(values))
	for index := range values {
		name := fmt.Sprintf("option-%d", index)
		catalog[index] = Definition{Name: name, Architectures: []string{"wide"}}
		specs[index] = Bind(name, &values[index])
	}
	t.Cleanup(func() { catalog = old })

	bindings := NewBindings("wide", specs...)
	selection, err := bindings.Resolve(map[string]bool{
		"option-0":  true,
		"option-63": true,
		"option-64": true,
	})
	if err != nil {
		t.Fatal(err)
	}
	for _, index := range []int{0, 63, 64} {
		name := fmt.Sprintf("option-%d", index)
		if !selection.Enabled(name) || !selection.EnabledOption(bindings.Option(name)) {
			t.Fatalf("%s is disabled", name)
		}
	}
	if selection.Enabled("option-62") {
		t.Fatal("unset option in the first word is enabled")
	}
}

func TestBindingsRejectSelectionBeyondBoundedWords(t *testing.T) {
	old := catalog
	values := make([]bool, 129)
	specs := make([]BindingSpec, len(values))
	catalog = make([]Definition, len(values))
	for index := range values {
		name := fmt.Sprintf("option-%d", index)
		catalog[index] = Definition{Name: name, Architectures: []string{"too-wide"}}
		specs[index] = Bind(name, &values[index])
	}
	t.Cleanup(func() { catalog = old })
	assertPanics(t, func() { NewBindings("too-wide", specs...) })
}

func TestResolveAllocationBudget(t *testing.T) {
	a, b := true, false
	bindings := selectionTestBindings(t, &a, &b)
	overrides := map[string]bool{"a": false, "b": true}
	allocs := testing.AllocsPerRun(1000, func() {
		selection, err := bindings.Resolve(overrides)
		if err != nil || selection.Enabled("a") || !selection.Enabled("b") {
			panic("invalid selection")
		}
	})
	if allocs != 0 {
		t.Fatalf("Resolve allocations = %.0f, want 0", allocs)
	}
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
