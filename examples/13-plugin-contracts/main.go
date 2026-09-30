// Example 13: two plugins connected by a typed Contract.
//
// Run:
//
//	go run ./examples/13-plugin-contracts
package main

import (
	"context"
	"fmt"

	wago "github.com/wago-org/wago"
	"github.com/wago-org/wago/examples/internal/exampleplugin"
	"github.com/wago-org/wago/plugin"
)

type Greeter interface {
	Greeting(string) string
}

var greetingContract = plugin.NewContract[Greeter]("example.com/contracts/greeting", 1)

type english struct{}

func (english) Greeting(name string) string { return "hello, " + name }

type greetingProvider struct{}

func (greetingProvider) Register(reg *wago.Registrar) error {
	return plugin.Provide(reg, greetingContract, Greeter(english{}))
}

type app struct{}

func (app) Register(reg *wago.Registrar) error {
	greeter, err := plugin.Require(reg, greetingContract)
	if err != nil {
		return err
	}
	return reg.Lifecycle(wago.PluginLifecycle{
		Start: func(context.Context) error {
			return greeter.With(func(service Greeter) error {
				fmt.Println(service.Greeting("Wago"))
				return nil
			})
		},
	})
}

func definition(id string) wago.PluginDefinition {
	return wago.PluginDefinition{
		ID:        id,
		Name:      id,
		Version:   "1.0.0",
		Stability: wago.Experimental,
		Provenance: wago.PluginProvenance{
			Repository: "https://" + id,
			License:    "Apache-2.0",
		},
	}
}

func main() {
	providerDefinition := definition("example.com/plugins/english")
	providerDefinition.Provides = []wago.ContractSpec{greetingContract.Spec()}
	consumerDefinition := definition("example.com/plugins/app")
	consumerDefinition.Requires = []wago.PluginRequirement{{
		ID:      providerDefinition.ID,
		Version: "^1.0.0",
	}}
	consumerDefinition.Consumes = []wago.ContractRequirement{{
		ID:    greetingContract.ID(),
		Major: greetingContract.Major(),
		Mode:  wago.ContractRequired,
	}}

	set := exampleplugin.MustSetAll(
		wago.PluginProvider{
			Definition: providerDefinition,
			New:        func() wago.Plugin { return greetingProvider{} },
		},
		wago.PluginProvider{
			Definition: consumerDefinition,
			New:        func() wago.Plugin { return app{} },
		},
	)

	rt := wago.NewRuntime()
	defer rt.Close()
	if err := rt.LoadPlugins(context.Background(), set); err != nil {
		panic(err)
	}
}
