// Example 10: scoped lifecycle observation.
//
// Exact observer and interceptor handles let a plugin trace compile,
// instantiate, invoke, and close without receiving raw Runtime, Instance, or
// Module authority. Run:
//
//	go run ./examples/10-hooks
package main

import (
	"context"
	"fmt"
	"time"

	wago "github.com/wago-org/wago"
	"github.com/wago-org/wago/examples/internal/mods"
)

type tracer struct{}

var tracerDefinition = wago.PluginDefinition{
	ID:          "example.com/wago/tracer",
	Name:        "Tracer",
	Version:     "1.0.0",
	Description: "Observes runtime operations without controlling them.",
	Stability:   wago.Experimental,
	Provenance: wago.PluginProvenance{
		Repository: "https://example.com/wago/tracer",
		License:    "Apache-2.0",
	},
	Authorities: []wago.AuthorityRequest{
		{Name: wago.AuthorityModuleCompileObserve, Mode: wago.AuthorityRequired, Reason: "record successful compilation"},
		{Name: wago.AuthorityInstanceInstantiateObserve, Mode: wago.AuthorityRequired, Reason: "record successful instantiation"},
		{Name: wago.AuthorityInstanceCloseObserve, Mode: wago.AuthorityRequired, Reason: "record logical instance close"},
		{Name: wago.AuthorityInstanceInvokeIntercept, Mode: wago.AuthorityRequired, Reason: "record invocation start"},
		{Name: wago.AuthorityInstanceInvokeObserve, Mode: wago.AuthorityRequired, Reason: "record invocation result and duration"},
	},
}

func (tracer) Register(reg *wago.Registrar) error {
	compiled, err := reg.ModuleCompileObserver()
	if err != nil {
		return err
	}
	if err := compiled.Observe(func(wago.ModuleCompiledEvent) {
		fmt.Println("[trace] compiled module")
	}); err != nil {
		return err
	}

	instantiated, err := reg.InstanceInstantiateObserver()
	if err != nil {
		return err
	}
	if err := instantiated.After(func(wago.InstantiationEvent) {
		fmt.Println("[trace] instantiated module")
	}); err != nil {
		return err
	}

	closed, err := reg.InstanceCloseObserver()
	if err != nil {
		return err
	}
	if err := closed.Before(func(wago.InstanceCloseEvent) {
		fmt.Println("[trace] disposing instance")
	}); err != nil {
		return err
	}
	if err := closed.After(func(wago.InstanceCloseEvent) {
		fmt.Println("[trace] disposed instance")
	}); err != nil {
		return err
	}

	invoking, err := reg.InstanceInvokeInterceptor()
	if err != nil {
		return err
	}
	if err := invoking.Before(func(event wago.InvocationRequest) error {
		fmt.Printf("[trace] -> %s(%v)\n", event.Export, event.Args)
		return nil
	}); err != nil {
		return err
	}

	invoked, err := reg.InstanceInvokeObserver()
	if err != nil {
		return err
	}
	return invoked.After(func(event wago.InvocationEvent) {
		elapsed := time.Since(event.Start)
		fmt.Printf("[trace] <- %s => %v (%s, err=%v)\n", event.Export, event.Results, elapsed.Round(time.Microsecond), event.Err)
	})
}

func tracerPluginSet() wago.PluginSet {
	provider := wago.PluginProvider{
		Definition: tracerDefinition,
		New:        func() wago.Plugin { return tracer{} },
	}
	digest, err := wago.DefinitionDigest(tracerDefinition)
	if err != nil {
		panic(err)
	}
	grants := make([]wago.AuthorityGrant, 0, len(tracerDefinition.Authorities))
	for _, request := range tracerDefinition.Authorities {
		grants = append(grants, wago.AuthorityGrant{Name: request.Name})
	}
	return wago.PluginSet{
		Providers: []wago.PluginProvider{provider},
		Selections: []wago.PluginSelection{{
			ID:               tracerDefinition.ID,
			DefinitionDigest: digest,
			Direct:           true,
			Dependencies:     map[string]string{},
			Grants:           grants,
		}},
	}
}

func main() {
	rt := wago.NewRuntime()
	defer rt.Close()
	if err := rt.LoadPlugins(context.Background(), tracerPluginSet()); err != nil {
		panic(err)
	}

	mod, _ := rt.Compile(mods.Add())
	ctx := context.Background()
	inst, _ := rt.Instantiate(ctx, mod)
	defer inst.Close()

	_, _ = inst.Call(ctx, "add", wago.ValueI32(20), wago.ValueI32(22))
}
