// Example 20: narrowly scoped core handles used during plugin startup.
//
// Run:
//
//	go run ./examples/20-core-handles
package main

import (
	"context"
	"errors"
	"fmt"

	wago "github.com/wago-org/wago"
	"github.com/wago-org/wago/examples/internal/exampleplugin"
	"github.com/wago-org/wago/examples/internal/mods"
)

type corePlugin struct {
	compiler     *wago.CoreModuleCompiler
	instantiator *wago.CoreInstanceInstantiator
	functions    *wago.CoreFuncRefFactory
	function     *wago.HostFuncRef
}

var definition = wago.PluginDefinition{
	ID:          "example.com/wago/core-worker",
	Name:        "Core worker",
	Version:     "1.0.0",
	Description: "Compiles and owns a bounded worker during startup.",
	Stability:   wago.Experimental,
	Provenance: wago.PluginProvenance{
		Repository: "https://example.com/wago/core-worker",
		License:    "Apache-2.0",
	},
	Authorities: []wago.AuthorityRequest{
		{Name: wago.AuthorityCoreModuleCompile, Mode: wago.AuthorityRequired, Reason: "compile the worker module"},
		{
			Name:   wago.AuthorityCoreInstanceInstantiate,
			Mode:   wago.AuthorityRequired,
			Reason: "instantiate one worker",
			Scope:  wago.AuthorityScope{MaxInstances: 1, MaxMemoryBytes: 65536},
		},
		{Name: wago.AuthorityCoreFuncRefCreate, Mode: wago.AuthorityRequired, Reason: "create a typed callback"},
	},
}

func (p *corePlugin) Register(reg *wago.Registrar) error {
	var err error
	p.compiler, err = reg.CoreModuleCompiler()
	if err != nil {
		return err
	}
	p.instantiator, err = reg.CoreInstanceInstantiator()
	if err != nil {
		return err
	}
	p.functions, err = reg.CoreFuncRefFactory()
	if err != nil {
		return err
	}

	return reg.Lifecycle(wago.PluginLifecycle{
		Start: p.start,
		Stop: func(context.Context) error {
			if p.function == nil {
				return nil
			}
			return p.function.Close()
		},
	})
}

func (p *corePlugin) start(ctx context.Context) error {
	callback, err := p.functions.New(
		func(_ wago.HostModule, params, results []uint64) {
			results[0] = wago.I32(wago.AsI32(params[0]) + 1)
		},
		wago.FuncSig{Params: []wago.ValType{wago.ValI32}, Results: []wago.ValType{wago.ValI32}},
	)
	if err != nil {
		return err
	}
	p.function = callback

	module, err := p.compiler.Compile(mods.Add())
	if err != nil {
		return err
	}
	owned, instantiateErr := p.instantiator.Instantiate(ctx, module)
	if instantiateErr != nil {
		return errors.Join(instantiateErr, module.Close())
	}
	values, callErr := owned.Instance().Call(ctx, "add", wago.ValueI32(20), wago.ValueI32(22))
	if callErr == nil {
		fmt.Println("startup worker returned", values[0].I32())
	}
	return errors.Join(callErr, owned.Close(), module.Close())
}

func main() {
	rt := wago.NewRuntime()
	set := exampleplugin.MustSet(definition, func() wago.Plugin { return &corePlugin{} })
	if err := rt.LoadPlugins(context.Background(), set); err != nil {
		panic(err)
	}
	if err := rt.CloseContext(context.Background()); err != nil {
		panic(err)
	}
}
