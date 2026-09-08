// Example 12: callback context and synchronous re-entry into the active guest.
//
// Run:
//
//	go run ./examples/12-caller-context
package main

import (
	"context"
	_ "embed"
	"fmt"

	wago "github.com/wago-org/wago"
	"github.com/wago-org/wago/examples/internal/exampleplugin"
)

//go:embed guest.wasm
var guest []byte

type callbackPlugin struct{}

var definition = wago.PluginDefinition{
	ID:          "example.com/wago/callback",
	Name:        "Callback",
	Version:     "1.0.0",
	Description: "Re-enters the guest that made the current host call.",
	Stability:   wago.Experimental,
	Provenance: wago.PluginProvenance{
		Repository: "https://example.com/wago/callback",
		License:    "Apache-2.0",
	},
	Authorities: []wago.AuthorityRequest{
		{
			Name:   wago.AuthorityHostImportDefine,
			Mode:   wago.AuthorityRequired,
			Reason: "define the callback bridge",
			Scope:  wago.AuthorityScope{Modules: []string{"env"}},
		},
		{
			Name:   wago.AuthorityHostCallerIdentify,
			Mode:   wago.AuthorityRequired,
			Reason: "read cancellation from the active guest call",
		},
		{
			Name:   wago.AuthorityHostCallerInvoke,
			Mode:   wago.AuthorityRequired,
			Reason: "invoke the active guest's callback export",
		},
	},
}

func (callbackPlugin) Register(reg *wago.Registrar) error {
	callers, err := reg.HostCallers()
	if err != nil {
		return err
	}
	invoker, err := reg.HostCallerInvoker()
	if err != nil {
		return err
	}
	imports, err := reg.HostImports()
	if err != nil {
		return err
	}
	env, err := imports.Module("env")
	if err != nil {
		return err
	}

	env.Func("outer", func(caller wago.HostModule, params, results []uint64) {
		ctx, err := callers.InvocationContext(caller)
		if err != nil {
			panic(wago.HostTrap{Err: err})
		}
		nested, err := invoker.Invoke(ctx, caller, "callback", params...)
		if err != nil {
			panic(wago.HostTrap{Err: err})
		}
		copy(results, nested)
	}).Params(wago.ValI32).Results(wago.ValI32)
	return nil
}

func main() {
	rt := wago.NewRuntime()
	defer rt.Close()
	set := exampleplugin.MustSet(definition, func() wago.Plugin { return callbackPlugin{} })
	if err := rt.LoadPlugins(context.Background(), set); err != nil {
		panic(err)
	}
	module, err := rt.Compile(guest)
	if err != nil {
		panic(err)
	}
	defer module.Close()
	instance, err := rt.Instantiate(context.Background(), module)
	if err != nil {
		panic(err)
	}
	defer instance.Close()

	result, err := instance.Call(context.Background(), "run", wago.ValueI32(41))
	if err != nil {
		panic(err)
	}
	fmt.Println("guest callback returned", result[0].I32())
}
