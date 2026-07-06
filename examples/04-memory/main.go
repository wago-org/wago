// Example 04: reading guest memory from a host function.
//
// The classic host-import pattern is (ptr, len) -> read bytes out of the guest's
// linear memory. HostModule.Memory() gives the host a view of that memory during
// the call. Run:
//
//	go run ./examples/04-memory
package main

import (
	"fmt"

	wago "github.com/wago-org/wago"
	"github.com/wago-org/wago/examples/internal/mods"
)

func main() {
	// The module holds "hello from wasm" in its memory and calls
	// env.write(ptr, len) with the location of that string.
	compiled, err := wago.Compile(mods.MemWriter("hello from wasm"))
	if err != nil {
		panic(err)
	}

	write := wago.HostFunc(func(m wago.HostModule, params, results []uint64) {
		ptr, n := uint32(params[0]), uint32(params[1])
		mem := m.Memory() // a view of the calling instance's linear memory
		if int(ptr)+int(n) > len(mem) {
			results[0] = wago.I32(-1) // out of bounds
			return
		}
		fmt.Printf("guest wrote: %q\n", mem[ptr:ptr+n])
		results[0] = wago.I32(int32(n))
	})

	inst, err := wago.Instantiate(compiled, wago.Imports{"env.write": write})
	if err != nil {
		panic(err)
	}
	defer inst.Close()

	out, err := inst.Invoke("run")
	if err != nil {
		panic(err)
	}
	fmt.Printf("run() wrote %d bytes\n", wago.AsI32(out[0]))

	// You can also read/write the instance's memory directly from the host.
	inst.WriteUint8(0, 'H')
	b, _ := inst.ReadUint8(0)
	fmt.Printf("memory[0] is now %q\n", rune(b))
}
