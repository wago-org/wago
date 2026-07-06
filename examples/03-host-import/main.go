// Example 03: host imports.
//
// A guest can call back into the host. Host functions use the single, portable
// stack form wago.HostFunc — it reads params and writes results as raw slots and
// binds identically on standard Go and TinyGo (no reflection). Run:
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
	compiled, err := wago.Compile(mods.SquareViaHost())
	if err != nil {
		panic(err)
	}

	// A HostFunc reads its wasm params from params[] and writes results into
	// results[]. Here: mul(a, b) = a * b.
	mul := wago.HostFunc(func(_ wago.HostModule, params, results []uint64) {
		a, b := wago.AsI32(params[0]), wago.AsI32(params[1])
		results[0] = wago.I32(a * b)
	})

	inst, err := wago.Instantiate(compiled, wago.Imports{"host.mul": mul})
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
