// Example 18: a custom compiler instruction used by a WAT guest.
//
// Run:
//
//	go run ./examples/18-custom-instruction
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

type instructionPlugin struct{}

var definition = wago.PluginDefinition{
	ID:          "example.com/wago/i4",
	Name:        "Four-bit arithmetic",
	Version:     "1.0.0",
	Description: "Adds a four-bit instruction to the Wago compiler.",
	Stability:   wago.Experimental,
	Provenance: wago.PluginProvenance{
		Repository: "https://example.com/wago/i4",
		License:    "Apache-2.0",
	},
	Authorities: []wago.AuthorityRequest{{
		Name:   wago.AuthorityCompilerInstructionDefine,
		Mode:   wago.AuthorityRequired,
		Reason: "define four-bit arithmetic",
		Scope:  wago.AuthorityScope{Modules: []string{"wago:instr/example.int"}},
	}},
}

func (instructionPlugin) Register(reg *wago.Registrar) error {
	instructions, err := reg.CompilerInstructions()
	if err != nil {
		return err
	}
	return instructions.Define(wago.InstructionSpec{
		Module: "wago:instr/example.int",
		Name:   "i4.add",
		Input:  []int32{4, 4},
		Output: []int32{4},
		Handler: func(_ wago.InstructionContext, args []wago.Bits) ([]wago.Bits, error) {
			sum, err := wago.BitsFromUint32(4, args[0].Uint32()+args[1].Uint32())
			return []wago.Bits{sum}, err
		},
		Lower: func(ctx wago.LoweringContext) error {
			ctx.Output(0, ctx.Add(ctx.Input(0), ctx.Input(1)))
			return nil
		},
	})
}

func main() {
	rt := wago.NewRuntime()
	defer rt.Close()
	set := exampleplugin.MustSet(definition, func() wago.Plugin { return instructionPlugin{} })
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

	result, err := instance.Call(context.Background(), "add", wago.ValueI32(15), wago.ValueI32(3))
	if err != nil {
		panic(err)
	}
	fmt.Println("i4.add(15, 3) =", result[0].I32())
}
