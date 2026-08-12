package plugin

import (
	"context"
	"errors"
	"testing"

	"github.com/wago-org/wago"
)

type registerFunc func(*wago.Registrar) error

func (f registerFunc) Register(r *wago.Registrar) error { return f(r) }

func typedDef(id string) wago.PluginDefinition {
	return wago.PluginDefinition{ID: id, Version: "1.0.0", Provenance: wago.PluginProvenance{Repository: "https://example.com/" + id, License: "MIT"}}
}

func TestTypedContractRequiredOptionalAndMany(t *testing.T) {
	contract := NewContract[int]("example.com/typed/counter", 1)
	p1, p2, c := typedDef("example.com/typed/p1"), typedDef("example.com/typed/p2"), typedDef("example.com/typed/consumer")
	p1.Provides = []wago.ContractSpec{contract.Spec()}
	p2.Provides = []wago.ContractSpec{contract.Spec()}
	c.Consumes = []wago.ContractRequirement{{ID: contract.ID(), Major: 1, Mode: wago.ContractMany}}
	var ref *ManyRef[int]
	providers := []wago.PluginProvider{{Definition: p1, New: func() wago.Plugin {
		return registerFunc(func(r *wago.Registrar) error { return Provide(r, contract, 1) })
	}}, {Definition: p2, New: func() wago.Plugin {
		return registerFunc(func(r *wago.Registrar) error { return Provide(r, contract, 2) })
	}}, {Definition: c, New: func() wago.Plugin {
		return registerFunc(func(r *wago.Registrar) error { var err error; ref, err = Many(r, contract); return err })
	}}}
	set := wago.PluginSet{Providers: providers}
	for _, p := range providers {
		digest, err := wago.DefinitionDigest(p.Definition)
		if err != nil {
			t.Fatal(err)
		}
		sel := wago.PluginSelection{ID: p.Definition.ID, DefinitionDigest: digest, Direct: true, Dependencies: map[string]string{}}
		for _, requirement := range p.Definition.Requires {
			sel.Dependencies[requirement.ID] = requirement.Version
		}
		if p.Definition.ID == c.ID {
			sel.Contracts = []wago.ContractBinding{{ID: contract.ID(), Major: 1, Providers: []string{p1.ID, p2.ID}}}
		}
		set.Selections = append(set.Selections, sel)
	}
	rt := wago.NewRuntime()
	if err := rt.LoadPlugins(context.Background(), set); err != nil {
		t.Fatal(err)
	}
	var sum int
	if err := ref.With(func(values []int) error {
		for _, v := range values {
			sum += v
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if sum != 3 {
		t.Fatalf("sum=%d", sum)
	}
	if err := rt.Close(); err != nil {
		t.Fatal(err)
	}
	if err := ref.With(func([]int) error { return nil }); !errors.Is(err, wago.ErrPermissionDenied) {
		t.Fatalf("after close=%v", err)
	}

	optional := typedDef("example.com/typed/optional")
	optional.Consumes = []wago.ContractRequirement{{ID: contract.ID(), Major: 1, Mode: wago.ContractOptional}}
	var opt *OptionalRef[int]
	op := wago.PluginProvider{Definition: optional, New: func() wago.Plugin {
		return registerFunc(func(r *wago.Registrar) error { var err error; opt, err = Optional(r, contract); return err })
	}}
	digest, _ := wago.DefinitionDigest(optional)
	empty := wago.PluginSet{Providers: []wago.PluginProvider{op}, Selections: []wago.PluginSelection{{ID: optional.ID, DefinitionDigest: digest, Direct: true, Dependencies: map[string]string{}, Contracts: []wago.ContractBinding{{ID: contract.ID(), Major: 1}}}}}
	rt = wago.NewRuntime()
	if err := rt.LoadPlugins(context.Background(), empty); err != nil {
		t.Fatal(err)
	}
	if err := opt.With(func(_ int, present bool) error {
		if present {
			t.Fatal("unexpected provider")
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
}
