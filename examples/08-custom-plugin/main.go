// Example 08: writing your own plugin.
//
// A plugin has one Register method. An explicit provider pairs its factory with
// an immutable definition; the host activates that provider with reviewed exact
// Authority Grants. Host functions use the portable stack form. Run:
//
//	go run ./examples/08-custom-plugin
package main

import (
	"context"
	"fmt"

	wago "github.com/wago-org/wago"
	"github.com/wago-org/wago/examples/internal/mods"
)

// randPlugin exposes a deterministic "random" number generator under the
// wago_rand module. It is deterministic so the example output stays stable.
type randPlugin struct{ seed uint64 }

// CapRand is the guest capability this plugin provides. Guest capability policy
// is separate from the Plugin Authority needed to define the import.
const CapRand = wago.Capability("rand.read")

var randDefinition = wago.PluginDefinition{
	ID:          "example.com/wago/rand",
	Name:        "Rand",
	Version:     "1.0.0",
	Description: "A deterministic pseudo-random source for guests.",
	Stability:   wago.Experimental,
	Compatibility: wago.Compatibility{
		Engines: map[string]string{"wago": ">=0.1.0"},
	},
	Provenance: wago.PluginProvenance{
		Repository: "https://example.com/wago/rand",
		License:    "Apache-2.0",
	},
	Authorities: []wago.AuthorityRequest{{
		Name:   wago.AuthorityHostImportDefine,
		Mode:   wago.AuthorityRequired,
		Reason: "define the wago_rand guest API",
		Scope:  wago.AuthorityScope{Modules: []string{"wago_rand"}},
	}},
}

func (e *randPlugin) Register(reg *wago.Registrar) error {
	if err := reg.GuestCapability(CapRand, wago.CapabilityDocs("read pseudo-random numbers")); err != nil {
		return err
	}
	imports, err := reg.HostImports()
	if err != nil {
		return err
	}
	module, err := imports.Module("wago_rand")
	if err != nil {
		return err
	}

	// next() -> i64 advances an xorshift state and returns it.
	module.Func("next", func(_ wago.HostModule, _, results []uint64) {
		e.seed ^= e.seed << 13
		e.seed ^= e.seed >> 7
		e.seed ^= e.seed << 17
		results[0] = e.seed
	}).Results(wago.ValI64).Capability(CapRand).
		Docs("advance the RNG and return the next 64-bit value")
	return nil
}

func randPluginSet() wago.PluginSet {
	provider := wago.PluginProvider{
		Definition: randDefinition,
		New:        func() wago.Plugin { return &randPlugin{seed: 42} },
	}
	digest, err := wago.DefinitionDigest(randDefinition)
	if err != nil {
		panic(err)
	}
	return wago.PluginSet{
		Providers: []wago.PluginProvider{provider},
		Selections: []wago.PluginSelection{{
			ID:               randDefinition.ID,
			DefinitionDigest: digest,
			Direct:           true,
			Dependencies:     map[string]string{},
			Grants: []wago.AuthorityGrant{{
				Name:  wago.AuthorityHostImportDefine,
				Scope: wago.AuthorityScope{Modules: []string{"wago_rand"}},
			}},
		}},
	}
}

func main() {
	rt := wago.NewRuntime()
	defer rt.Close()
	if err := rt.LoadPlugins(context.Background(), randPluginSet()); err != nil {
		panic(err)
	}

	// A guest importing wago_rand.next() -> i64, re-exported as roll().
	mod, _ := rt.Compile(mods.ImportCaller("wago_rand", "next", "roll", []byte{mods.I64}))
	ctx := context.Background()
	inst, _ := rt.Instantiate(ctx, mod)
	defer inst.Close()

	for i := 0; i < 3; i++ {
		out, _ := inst.Call(ctx, "roll")
		fmt.Printf("roll() = %d\n", uint64(out[0].I64()))
	}
}
