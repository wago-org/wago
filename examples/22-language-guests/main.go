// Example 22: the same guest API compiled from WAT, AssemblyScript, and TinyGo.
//
// Run:
//
//	go run ./examples/22-language-guests
package main

import (
	"context"
	_ "embed"
	"fmt"

	wago "github.com/wago-org/wago"
	"github.com/wago-org/wago/examples/internal/exampleplugin"
)

var (
	//go:embed wat/answer.wasm
	watGuest []byte
	//go:embed assemblyscript/answer.wasm
	assemblyScriptGuest []byte
	//go:embed tinygo/answer.wasm
	tinyGoGuest []byte
)

type answerPlugin struct{}

var definition = wago.PluginDefinition{
	ID:          "example.com/wago/answer",
	Name:        "Answer",
	Version:     "1.0.0",
	Description: "Returns the tutorial answer.",
	Stability:   wago.Experimental,
	Provenance: wago.PluginProvenance{
		Repository: "https://example.com/wago/answer",
		License:    "Apache-2.0",
	},
	Authorities: []wago.AuthorityRequest{{
		Name:   wago.AuthorityHostImportDefine,
		Mode:   wago.AuthorityRequired,
		Reason: "define the tutorial guest API",
		Scope:  wago.AuthorityScope{Modules: []string{"tutorial"}},
	}},
}

func (answerPlugin) Register(reg *wago.Registrar) error {
	imports, err := reg.HostImports()
	if err != nil {
		return err
	}
	tutorial, err := imports.Module("tutorial")
	if err != nil {
		return err
	}
	tutorial.Func("answer", func(_ wago.HostModule, _, results []uint64) {
		results[0] = wago.I32(42)
	}).Results(wago.ValI32)
	return nil
}

func newRuntime() *wago.Runtime {
	rt := wago.NewRuntime()
	set := exampleplugin.MustSet(definition, func() wago.Plugin { return answerPlugin{} })
	if err := rt.LoadPlugins(context.Background(), set); err != nil {
		panic(err)
	}
	return rt
}

func run(rt *wago.Runtime, source []byte, initialize bool) int32 {
	module, err := rt.Compile(source)
	if err != nil {
		panic(err)
	}
	defer module.Close()
	instance, err := rt.Instantiate(context.Background(), module)
	if err != nil {
		panic(err)
	}
	defer instance.Close()
	if initialize {
		if _, err := instance.Call(context.Background(), "_initialize"); err != nil {
			panic(err)
		}
	}
	result, err := instance.Call(context.Background(), "run")
	if err != nil {
		panic(err)
	}
	return result[0].I32()
}

func main() {
	rt := newRuntime()
	defer rt.Close()
	for _, guest := range []struct {
		name       string
		source     []byte
		initialize bool
	}{
		{"WAT", watGuest, false},
		{"AssemblyScript", assemblyScriptGuest, false},
		{"TinyGo", tinyGoGuest, true},
	} {
		fmt.Printf("%s answer = %d\n", guest.name, run(rt, guest.source, guest.initialize))
	}
}
