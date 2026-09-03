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

func TestMeasuredLowValueOptimizationsDefaultOff(t *testing.T) {
	wantOff := map[string]map[string]bool{
		"amd64": {
			"loop-precheck": true,
			"v128-sink":     true,
		},
		"arm64": {
			"loop-precheck": true,
		},
	}
	for arch, names := range wantOff {
		seen := map[string]bool{}
		for _, definition := range ForArch(arch) {
			if definition.Name == "inline-loop-callees" {
				t.Fatalf("%s still registers removed inline-loop-callees", arch)
			}
			if names[definition.Name] {
				seen[definition.Name] = true
				if definition.Default {
					t.Errorf("%s %s defaults on", arch, definition.Name)
				}
			}
		}
		for name := range names {
			if !seen[name] {
				t.Errorf("%s default-off optimization %s is not registered", arch, name)
			}
		}
	}
}

func TestV128SinkIsAMD64Only(t *testing.T) {
	if _, ok := Lookup("arm64", "v128-sink"); ok {
		t.Fatal("arm64 still exposes the measured-low-value vector sink")
	}
	definition, ok := Lookup("amd64", "v128-sink")
	if !ok {
		t.Fatal("amd64 vector sink was removed before native verification")
	}
	if definition.Default {
		t.Fatal("amd64 vector sink unexpectedly defaults on")
	}
}

func TestDeepFPPinsAreRemoved(t *testing.T) {
	if _, ok := Lookup("arm64", "deep-fp-pins"); ok {
		t.Fatal("arm64 still exposes measured-low-value deep float pins")
	}
}

func TestV128ConstCacheIsAMD64Only(t *testing.T) {
	if _, ok := Lookup("arm64", "v128-const-cache"); ok {
		t.Fatal("arm64 still exposes the measured-low-value v128 constant cache")
	}
	definition, ok := Lookup("amd64", "v128-const-cache")
	if !ok {
		t.Fatal("amd64 lost its high-value v128 constant cache")
	}
	if !definition.Default {
		t.Fatal("amd64 v128 constant cache no longer defaults on")
	}
}

func TestSubstantialOptimizationFamiliesAreCatalogued(t *testing.T) {
	wantDefaultOff := map[string]map[string]bool{
		"amd64": {"gc-ref-facts": true},
	}
	want := map[string][]string{
		"amd64": {
			"simd-superopt",
			"swar-idioms",
			"interval-region-pins",
			"magic-div",
			"shared-trap-body",
			"shared-adapters",
			"dead-gc-new",
			"gc-ref-facts",
			"gc-native-alloc",
		},
		"arm64": {
			"simd-superopt",
			"swar-idioms",
			"interval-region-pins",
			"magic-div",
			"shared-trap-body",
			"shared-adapters",
			"zero-branch",
			"mul-add-fuse",
			"entry-init-elision",
			"v128-direct-results",
		},
	}
	for arch, names := range want {
		for _, name := range names {
			definition, ok := Lookup(arch, name)
			if !ok {
				t.Errorf("%s substantial optimization %q is not registered", arch, name)
				continue
			}
			if definition.Default == wantDefaultOff[arch][name] {
				t.Errorf("%s optimization %q default = %t, want %t", arch, name, definition.Default, !wantDefaultOff[arch][name])
			}
		}
	}
	for _, name := range []string{"dead-gc-new", "gc-ref-facts", "gc-native-alloc"} {
		if _, ok := Lookup("arm64", name); ok {
			t.Errorf("arm64 exposes amd64-only optimization %q", name)
		}
	}
	for _, name := range []string{"zero-branch", "mul-add-fuse", "entry-init-elision", "v128-direct-results"} {
		if _, ok := Lookup("amd64", name); ok {
			t.Errorf("amd64 exposes arm64-only optimization %q", name)
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
	restore.Restore()
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
	restore.Restore()
	if !values[0] {
		t.Fatal("matching snapshot delta was not restored")
	}
}

func TestBindingsApplySnapshotUsesChangedOverrideMissingFromDeltas(t *testing.T) {
	bindings, values, definitions := testBindings(t, "amd64", true)
	infos, snapshot := bindings.Snapshot()
	selection := infoValues(infos)
	selection[definitions[0].Name] = false

	restore, err := bindings.ApplySnapshot(selection, snapshot, nil)
	if err != nil {
		t.Fatal(err)
	}
	if values[0] {
		t.Fatal("changed override omitted from deltas was not applied")
	}
	restore.Restore()
}

func TestBindingsApplySnapshotUsesSelectionWhenDeltaConflicts(t *testing.T) {
	bindings, values, definitions := testBindings(t, "amd64", true)
	infos, snapshot := bindings.Snapshot()
	selection := infoValues(infos)
	name := definitions[0].Name
	selection[name] = false

	restore, err := bindings.ApplySnapshot(selection, snapshot, map[string]bool{name: true})
	if err != nil {
		t.Fatal(err)
	}
	if values[0] {
		t.Fatal("conflicting delta took precedence over complete selection")
	}
	restore.Restore()
}

func TestBindingsApplySnapshotRejectsUnknownOverrideAtMatchingRevision(t *testing.T) {
	bindings, _, _ := testBindings(t, "amd64", true)
	infos, snapshot := bindings.Snapshot()
	selection := infoValues(infos)
	selection["unknown"] = false

	if _, err := bindings.ApplySnapshot(selection, snapshot, nil); err == nil {
		t.Fatal("unknown override was accepted at matching revision")
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
	restore.Restore()
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
	restore.Restore()
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
		restore.Restore()
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
