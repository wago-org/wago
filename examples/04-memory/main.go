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
	compiled, err := wago.Compile(nil, mods.MemWriter("hello from wasm"))
	if err != nil {
		panic(err)
	}

	write := func(caller wago.Caller, call wago.HostCall) {
		ptr, n := uint32(call.I32(0)), uint32(call.I32(1))
		mem := caller.Memory()
		if int(ptr)+int(n) > len(mem) {
			call.SetI32(0, -1)
			return
		}
		fmt.Printf("guest wrote: %q\n", mem[ptr:ptr+n])
		call.SetI32(0, int32(n))
	}

	imports := wago.NewImports()
	imports.HostFunc("env", "write", write).Params(wago.ValI32, wago.ValI32).Results(wago.ValI32)
	inst, err := wago.Instantiate(compiled, wago.InstantiateOptions{Imports: imports})
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
