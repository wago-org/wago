// Example 03: host imports.
//
// A guest can call back into the host. Ordinary Go functions bind directly and
// identically on standard Go and TinyGo (no reflection). Run:
//
//	go run ./examples/03-host-import
package main

import (
	"fmt"

	wago "github.com/wago-org/wago"
	"github.com/wago-org/wago/examples/internal/mods"
)

func main() {
	// The module imports host.mul(a, b i32) -> i32 and exports square(x) = mul(x, x).
	compiled, err := wago.Compile(nil, mods.SquareViaHost())
	if err != nil {
		panic(err)
	}

	inst, err := wago.Instantiate(compiled, wago.InstantiateOptions{Imports: wago.Imports{
		"host.mul": func(a, b int32) int32 { return a * b },
	}})
	if err != nil {
		panic(err)
	}
	defer inst.Close()

	out, err := inst.Invoke("square", wago.I32(9))
	if err != nil {
		panic(err)
	}
	fmt.Printf("square(9) = %d\n", wago.AsI32(out[0]))
}
