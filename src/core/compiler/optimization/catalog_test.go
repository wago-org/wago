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

func assertPanics(t *testing.T, f func()) {
	t.Helper()
	defer func() {
		if recover() == nil {
			t.Fatal("call did not panic")
		}
	}()
	f()
}
