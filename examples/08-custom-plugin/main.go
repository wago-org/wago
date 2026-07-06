// Example 08: writing your own plugin.
//
// A plugin is any type implementing wago.Extension: Info() describes it, and
// Register() declares the host imports and capabilities it contributes. Host
// functions use the portable stack form. Run:
//
//	go run ./examples/08-custom-plugin
package main

import (
	"context"
	"fmt"

	wago "github.com/wago-org/wago"
	"github.com/wago-org/wago/examples/internal/mods"
)

// randExt is a tiny custom plugin that exposes a deterministic "random" number
// generator to guests under the wago_rand module. (Deterministic so the example
// output is stable.)
type randExt struct{ seed uint64 }

// CapRand is the capability this plugin provides; a policy can allow or deny it.
const CapRand = wago.Capability("rand.read")

func (e *randExt) Info() wago.ExtensionInfo {
	return wago.ExtensionInfo{
		ID:          "example.rand",
		Name:        "Rand",
		Version:     "1.0.0",
		Description: "A deterministic pseudo-random source for guests.",
		Stability:   wago.Experimental,
		License:     "Apache-2.0",
		Tags:        []string{"random", "example"},
		Private:     true, // a demo plugin — not for public listing/publication
		Compat:      wago.Compatibility{Engines: map[string]string{"wago": ">=0.1.0", "tinygo": "*"}},
	}
}

func (e *randExt) Register(reg *wago.Registry) error {
	reg.Capability(CapRand, wago.CapabilityDocs("read pseudo-random numbers"))

	// next() -> i64 advances an xorshift state and returns it.
	reg.ImportModule("wago_rand").
		Func("next", func(_ wago.HostModule, _, results []uint64) {
			e.seed ^= e.seed << 13
			e.seed ^= e.seed >> 7
			e.seed ^= e.seed << 17
			results[0] = e.seed
		}).
		Results(wago.ValI64).Capability(CapRand).
		Docs("advance the RNG and return the next 64-bit value")

	return nil
}

func main() {
	rt := wago.NewRuntime()
	defer rt.Close()

	if err := rt.Use(&randExt{seed: 42}); err != nil {
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
