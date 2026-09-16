// Example 23: the complete public API path.
//
// Rebuild the checked-in guest fixture with wabt's wat2wasm:
//
//	wat2wasm examples/23-public-api/guest.wat -o examples/23-public-api/guest.wasm
//
// Run from the repository root with:
//
//	go run ./examples/23-public-api
package main

import (
	"context"
	"fmt"
	"os"

	wago "github.com/wago-org/wago"
)

func loadGuest() ([]byte, error) {
	wasm, err := os.ReadFile("examples/23-public-api/guest.wasm")
	if os.IsNotExist(err) {
		return os.ReadFile("guest.wasm") // Package tests run in this directory.
	}
	return wasm, err
}

func main() {
	if err := run(context.Background()); err != nil {
		panic(err)
	}
}

func run(ctx context.Context) error {
	wasm, err := loadGuest()
	if err != nil {
		return err
	}

	add := func(a, b int32) int32 { return a + b }
	imports := wago.NewImports()
	imports.HostFunc("env", "add", add)
	imports.HostFunc("foo", "scale", func(call wago.HostCall) {
		call.SetF64(0, float64(call.I32(0))*call.F64(1))
	}).Params(wago.ValI32, wago.ValF64).Results(wago.ValF64)
	imports.HostFunc("env", "log", func(caller wago.Caller, call wago.HostCall) {
		ptr, size := uint64(uint32(call.I32(0))), uint64(uint32(call.I32(1)))
		memory := caller.Memory()
		if ptr > uint64(len(memory)) || size > uint64(len(memory))-ptr {
			panic(wago.HostTrap{Err: fmt.Errorf("log range [%d:%d] is outside guest memory", ptr, ptr+size)})
		}
		// memory[ptr:ptr+size] is borrowed and must not escape the callback.
		fmt.Printf("guest log: %q\n", memory[ptr:ptr+size])
	}).Params(wago.ValI32, wago.ValI32)

	rt := wago.NewRuntime()
	defer rt.Close()
	module, err := rt.Compile(wasm)
	if err != nil {
		return err
	}
	defer module.Close()
	instance, err := rt.Instantiate(ctx, module, wago.WithImports(imports))
	if err != nil {
		return err
	}
	defer instance.Close()

	// A registered implementation remains an ordinary Go function.
	fmt.Printf("host add(20, 22) = %d\n", add(20, 22))

	step, err := instance.WasmFunc("step")
	if err != nil {
		return err
	}
	out, err := step.Invoke(wago.I32(41))
	if err != nil {
		return err
	}
	fmt.Printf("step(41) = %d\n", wago.AsI32(out[0]))

	// Invoke by name. run calls env.log, env.add, and foo.scale.
	out, err = instance.Invoke("run", wago.I32(0), wago.I32(3), wago.F64(2.5))
	if err != nil {
		return err
	}
	fmt.Printf("run(0, 3, 2.5) = %.1f\n", wago.AsF64(out[0]))

	sum5, err := instance.WasmFunc("sum5")
	if err != nil {
		return err
	}
	out, err = sum5.Invoke(wago.I32(1), wago.I32(2), wago.I32(3), wago.I32(4), wago.I32(5))
	if err != nil {
		return err
	}
	// Results are borrowed until the next invocation on this instance. Copy any
	// result slots that must survive another call.
	savedSum := append([]uint64(nil), out...)
	fmt.Printf("sum5(1, 2, 3, 4, 5) = %d\n", wago.AsI32(savedSum[0]))

	args := [1]uint64{}
	for i := int32(0); i < 1000; i++ {
		args[0] = wago.I32(i)
		out, err = step.Invoke(args[:]...)
		if err != nil {
			return err
		}
	}
	fmt.Printf("step(999) = %d\n", wago.AsI32(out[0]))
	return nil
}
