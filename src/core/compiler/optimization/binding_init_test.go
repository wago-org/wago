package optimization

import (
	"fmt"
	"reflect"
	"testing"
)

func TestBindingsUnaffectedByPublicDefinitionMutation(t *testing.T) {
	for _, arch := range []string{"amd64", "arm64", "riscv64"} {
		definitions := ForArch(arch)
		values := make([]bool, len(definitions))
		specs := make([]BindingSpec, len(definitions))
		for i, d := range definitions {
			values[i] = d.Default
			specs[i] = Bind(d.Name, &values[i])
		}
		bindings := NewBindings(arch, specs...)
		before := bindings.Infos()
		for i := range definitions {
			looked, ok := Lookup(arch, definitions[i].Name)
			if !ok {
				t.Fatal("missing definition")
			}
			for j := range looked.Architectures {
				looked.Architectures[j] = "caller-mutated"
			}
			for j := range definitions[i].Architectures {
				definitions[i].Architectures[j] = "caller-mutated"
			}
			definitions[i].Name = "caller-mutated"
		}
		if !reflect.DeepEqual(bindings.Infos(), before) {
			t.Fatal("public mutation changed bindings")
		}
		for _, entry := range bindings.entries {
			if !supports(entry.definition, arch) {
				t.Fatal("public mutation reached internal architecture slice")
			}
		}
		if _, err := bindings.ResolveSnapshot(nil, Snapshot{}, nil); err != nil {
			t.Fatal(err)
		}
	}
}

var bindingInitSink *Bindings

func BenchmarkBindingsInitialization(b *testing.B) {
	for _, arch := range []string{"amd64", "arm64", "riscv64"} {
		b.Run(arch, func(b *testing.B) {
			definitions := ForArch(arch)
			values := make([]bool, len(definitions))
			specs := make([]BindingSpec, len(definitions))
			for i, d := range definitions {
				values[i] = d.Default
				specs[i] = Bind(d.Name, &values[i])
			}
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				bindingInitSink = NewBindings(arch, specs...)
			}
		})
	}
}

func TestBindingsInitializationValidationErrors(t *testing.T) {
	definitions := ForArch("amd64")
	values := make([]bool, len(definitions))
	specs := make([]BindingSpec, len(definitions))
	for i, d := range definitions {
		specs[i] = Bind(d.Name, &values[i])
	}
	cases := []struct {
		name  string
		specs []BindingSpec
		want  string
	}{
		{"nil", []BindingSpec{Bind(definitions[0].Name, nil)}, fmt.Sprintf("amd64 optimization binding %q has a nil value", definitions[0].Name)},
		{"duplicate", []BindingSpec{specs[0], specs[0]}, fmt.Sprintf("amd64 optimization binding %q is duplicated", definitions[0].Name)},
		{"unknown", []BindingSpec{Bind("not-registered", &values[0])}, "amd64 optimization binding \"not-registered\" is not registered"},
		{"missing", specs[:len(specs)-1], fmt.Sprintf("amd64 optimization %q has no backend binding", definitions[len(definitions)-1].Name)},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			defer func() {
				if got := fmt.Sprint(recover()); got != c.want {
					t.Fatalf("got %q want %q", got, c.want)
				}
			}()
			NewBindings("amd64", c.specs...)
			t.Fatal("accepted invalid binding")
		})
	}
}
