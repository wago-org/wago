// Example 17: a plugin-owned, quota-bound instance.
//
// Run:
//
//	go run ./examples/17-managed-instances
package main

import (
	"context"
	"fmt"

	wago "github.com/wago-org/wago"
	"github.com/wago-org/wago/examples/internal/exampleplugin"
	"github.com/wago-org/wago/examples/internal/mods"
)

type workerPlugin struct {
	instances *wago.InstanceManager
}

var definition = wago.PluginDefinition{
	ID:          "example.com/wago/worker",
	Name:        "Worker",
	Version:     "1.0.0",
	Description: "Owns bounded Wasm worker instances.",
	Stability:   wago.Experimental,
	Provenance: wago.PluginProvenance{
		Repository: "https://example.com/wago/worker",
		License:    "Apache-2.0",
	},
	Authorities: []wago.AuthorityRequest{{
		Name:   wago.AuthorityInstanceManage,
		Mode:   wago.AuthorityRequired,
		Reason: "own up to two worker instances",
		Scope: wago.AuthorityScope{
			MaxInstances:   2,
			MaxMemoryBytes: 2 * 65536,
		},
	}},
}

func (p *workerPlugin) Register(reg *wago.Registrar) error {
	instances, err := reg.ManagedInstances()
	if err != nil {
		return err
	}
	p.instances = instances
	return nil
}

func main() {
	worker := &workerPlugin{}
	rt := wago.NewRuntime()
	defer rt.Close()
	set := exampleplugin.MustSet(definition, func() wago.Plugin { return worker })
	if err := rt.LoadPlugins(context.Background(), set); err != nil {
		panic(err)
	}

	module, err := rt.Compile(mods.Add())
	if err != nil {
		panic(err)
	}
	defer module.Close()
	owned, err := worker.instances.Instantiate(context.Background(), module)
	if err != nil {
		panic(err)
	}
	// Close is callback-safe and starts logical close. Use WaitClosed when the
	// caller must wait for active calls and terminal close hooks to finish.
	defer owned.Close()

	result, err := owned.Instance().Call(
		context.Background(),
		"add",
		wago.ValueI32(20),
		wago.ValueI32(22),
	)
	if err != nil {
		panic(err)
	}
	fmt.Println("managed worker returned", result[0].I32())
}
