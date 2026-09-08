// Example 11: transform Wasm bytes before compilation.
//
// Run:
//
//	go run ./examples/11-source-transform
package main

import (
	"context"
	"fmt"

	wago "github.com/wago-org/wago"
	"github.com/wago-org/wago/examples/internal/exampleplugin"
	"github.com/wago-org/wago/examples/internal/mods"
)

type markerPlugin struct{}

var definition = wago.PluginDefinition{
	ID:          "example.com/wago/source-marker",
	Name:        "Source marker",
	Version:     "1.0.0",
	Description: "Adds a custom section before Wago compiles a module.",
	Stability:   wago.Experimental,
	Provenance: wago.PluginProvenance{
		Repository: "https://example.com/wago/source-marker",
		License:    "Apache-2.0",
	},
	Authorities: []wago.AuthorityRequest{
		{
			Name:   wago.AuthorityModuleSourceTransform,
			Mode:   wago.AuthorityRequired,
			Reason: "append the example custom section",
		},
		{
			Name:   wago.AuthorityModuleCompileObserve,
			Mode:   wago.AuthorityRequired,
			Reason: "confirm the transformed source compiled",
		},
	},
}

func (markerPlugin) Register(reg *wago.Registrar) error {
	transformer, err := reg.ModuleSourceTransformer()
	if err != nil {
		return err
	}
	if err := transformer.Transform(func(_ wago.ModuleSourceContext, source []byte) ([]byte, error) {
		name := []byte("example.marker")
		section := append([]byte{0, byte(len(name) + 1), byte(len(name))}, name...)
		return append(append([]byte(nil), source...), section...), nil
	}); err != nil {
		return err
	}

	observer, err := reg.ModuleCompileObserver()
	if err != nil {
		return err
	}
	return observer.Observe(func(event wago.ModuleCompiledEvent) {
		fmt.Println("compiled transformed source", event.SourceDigest)
	})
}

func main() {
	rt := wago.NewRuntime()
	defer rt.Close()
	set := exampleplugin.MustSet(definition, func() wago.Plugin { return markerPlugin{} })
	if err := rt.LoadPlugins(context.Background(), set); err != nil {
		panic(err)
	}
	module, err := rt.Compile(mods.Add())
	if err != nil {
		panic(err)
	}
	defer module.Close()
	fmt.Println("exports:", module.Exports())
}
