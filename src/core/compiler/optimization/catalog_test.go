package optimization

import "testing"

func TestCatalogRegistrationIsUniqueAndArchitectureScoped(t *testing.T) {
	seen := map[string]bool{}
	for _, definition := range All() {
		if definition.Name == "" || definition.Label == "" || definition.Description == "" {
			t.Fatalf("incomplete optimization registration: %#v", definition)
		}
		if seen[definition.Name] {
			t.Fatalf("duplicate optimization registration %q", definition.Name)
		}
		seen[definition.Name] = true
		if len(definition.Architectures) == 0 {
			t.Fatalf("optimization %q has no target architecture", definition.Name)
		}
		if definition.Experimental && definition.Default {
			t.Fatalf("experimental optimization %q defaults on", definition.Name)
		}
	}
	for _, arch := range []string{"amd64", "arm64"} {
		for _, definition := range ForArch(arch) {
			if _, ok := Lookup(arch, definition.Name); !ok {
				t.Fatalf("%s optimization %q cannot be looked up", arch, definition.Name)
			}
		}
	}
}

func TestBindingsRequireEveryArchitectureDefinition(t *testing.T) {
	value := true
	definitions := ForArch("amd64")
	specs := make([]BindingSpec, 0, len(definitions))
	for _, definition := range definitions {
		specs = append(specs, Bind(definition.Name, &value))
	}
	bindings := NewBindings("amd64", specs...)
	if got := len(bindings.Infos()); got != len(definitions) {
		t.Fatalf("bindings = %d, want %d", got, len(definitions))
	}
	assertPanics(t, func() { NewBindings("amd64", specs[:len(specs)-1]...) })
	assertPanics(t, func() { NewBindings("amd64", append(specs, Bind("missing", &value))...) })
}

func TestBindingsApplyAndRestoreSelection(t *testing.T) {
	definitions := ForArch("amd64")
	values := make([]bool, len(definitions))
	specs := make([]BindingSpec, len(definitions))
	for index, definition := range definitions {
		values[index] = true
		specs[index] = Bind(definition.Name, &values[index])
	}
	bindings := NewBindings("amd64", specs...)
	restore, err := bindings.Apply(map[string]bool{definitions[0].Name: false})
	if err != nil {
		t.Fatal(err)
	}
	if values[0] {
		t.Fatal("selection was not applied")
	}
	restore()
	if !values[0] {
		t.Fatal("selection was not restored")
	}
}

func TestBindingsApplySnapshotUsesDeltasAtMatchingRevision(t *testing.T) {
	bindings, values, definitions := testBindings(t, "amd64", true)
	infos, snapshot := bindings.Snapshot()
	selection := infoValues(infos)
	name := definitions[0].Name
	selection[name] = false

	restore, err := bindings.ApplySnapshot(selection, snapshot, map[string]bool{name: false})
	if err != nil {
		t.Fatal(err)
	}
	if values[0] {
		t.Fatal("matching snapshot delta was not applied")
	}
	restore()
	if !values[0] {
		t.Fatal("matching snapshot delta was not restored")
	}
}

func TestBindingsApplySnapshotRevisionMismatchUsesCapturedSelection(t *testing.T) {
	bindings, values, definitions := testBindings(t, "amd64", true)
	infos, snapshot := bindings.Snapshot()
	selection := infoValues(infos)
	name := definitions[0].Name
	if !bindings.Set(name, false) {
		t.Fatalf("Set(%q) failed", name)
	}

	restore, err := bindings.ApplySnapshot(selection, snapshot, nil)
	if err != nil {
		t.Fatal(err)
	}
	if !values[0] {
		t.Fatal("stale snapshot did not install its captured selection")
	}
	restore()
	if values[0] {
		t.Fatal("stale snapshot did not restore the newer process default")
	}
}

func TestBindingsApplySnapshotSerializesConcurrentSet(t *testing.T) {
	bindings, values, definitions := testBindings(t, "amd64", true)
	infos, snapshot := bindings.Snapshot()
	selection := infoValues(infos)
	name := definitions[0].Name
	selection[name] = false
	restore, err := bindings.ApplySnapshot(selection, snapshot, map[string]bool{name: false})
	if err != nil {
		t.Fatal(err)
	}

	entered := make(chan struct{})
	done := make(chan bool, 1)
	go func() {
		close(entered)
		done <- bindings.Set(name, false)
	}()
	<-entered
	select {
	case <-done:
		t.Fatal("Set completed while a compile snapshot held the binding lease")
	default:
	}
	restore()
	if !<-done {
		t.Fatalf("Set(%q) failed", name)
	}
	if values[0] {
		t.Fatal("concurrent Set did not become the process default after restore")
	}
}

func TestBindingsDefaultApplyAllocationBudget(t *testing.T) {
	bindings, _, _ := testBindings(t, "amd64", false)
	infos, snapshot := bindings.Snapshot()
	selection := infoValues(infos)
	allocs := testing.AllocsPerRun(1000, func() {
		restore, err := bindings.ApplySnapshot(selection, snapshot, nil)
		if err != nil {
			panic(err)
		}
		restore()
	})
	if allocs > 1 {
		t.Fatalf("matching default ApplySnapshot allocations = %.0f, want <= 1", allocs)
	}
}

func testBindings(t *testing.T, arch string, initial bool) (*Bindings, []bool, []Definition) {
	t.Helper()
	definitions := ForArch(arch)
	values := make([]bool, len(definitions))
	specs := make([]BindingSpec, len(definitions))
	for index, definition := range definitions {
		values[index] = initial
		specs[index] = Bind(definition.Name, &values[index])
	}
	return NewBindings(arch, specs...), values, definitions
}

func infoValues(infos []Info) map[string]bool {
	values := make(map[string]bool, len(infos))
	for _, info := range infos {
		values[info.Name] = info.On
	}
	return values
}

func assertPanics(t *testing.T, f func()) {
	t.Helper()
	defer func() {
		if recover() == nil {
			t.Fatal("call did not panic")
		}
	}()
	f()
}
