// Example 19: a custom 256-bit compiler value carried by ordinary externref.
//
// Run:
//
//	go run ./examples/19-custom-type
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

type typePlugin struct{}

var definition = wago.PluginDefinition{
	ID:          "example.com/wago/v256",
	Name:        "V256",
	Version:     "1.0.0",
	Description: "Defines an expression-scoped 256-bit compiler value.",
	Stability:   wago.Experimental,
	Provenance: wago.PluginProvenance{
		Repository: "https://example.com/wago/v256",
		License:    "Apache-2.0",
	},
	Authorities: []wago.AuthorityRequest{
		{
			Name:   wago.AuthorityCompilerTypeDefine,
			Mode:   wago.AuthorityRequired,
			Reason: "define the example.value type namespace",
			Scope:  wago.AuthorityScope{Modules: []string{"example.value"}},
		},
		{
			Name:   wago.AuthorityCompilerInstructionDefine,
			Mode:   wago.AuthorityRequired,
			Reason: "define producers and consumers for the custom value",
			Scope:  wago.AuthorityScope{Modules: []string{"example:types"}},
		},
	},
}

func (typePlugin) Register(reg *wago.Registrar) error {
	types, err := reg.CompilerTypes()
	if err != nil {
		return err
	}
	vector, err := types.Define(wago.CustomTypeSpec{
		Name:    "example.value/v256",
		Size:    32,
		Carrier: wago.WasmExternRef,
	})
	if err != nil {
		return err
	}

	instructions, err := reg.CompilerInstructions()
	if err != nil {
		return err
	}
	if err := instructions.Define(wago.InstructionSpec{
		Module:  "example:types",
		Name:    "zero",
		Output:  []int32{256},
		Custom:  &wago.CustomSignature{Output: &vector},
		Codegen: zeroCodegen(),
	}); err != nil {
		return err
	}
	return instructions.Define(wago.InstructionSpec{
		Module:  "example:types",
		Name:    "discard",
		Input:   []int32{256},
		Custom:  &wago.CustomSignature{Inputs: []wago.CustomType{vector}},
		Codegen: discardCodegen(),
	})
}

func main() {
	rt := wago.NewRuntime()
	defer rt.Close()
	set := exampleplugin.MustSet(definition, func() wago.Plugin { return typePlugin{} })
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
	if _, err := instance.Invoke("run"); err != nil {
		panic(err)
	}
	fmt.Println("created and consumed one v256 value")
}
