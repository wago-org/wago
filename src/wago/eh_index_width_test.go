//go:build ((linux && amd64) || ((linux || darwin) && arm64)) && !tinygo && !wago_guardpage

package wago

import (
	"context"
	"os"
	"os/exec"
	"strconv"
	"testing"
	"time"

	"github.com/wago-org/wago/src/core/compiler/wasm"
	"github.com/wago-org/wago/tests/support/wasmtest"
)

func ehHandlerIndexBoundaryModule(depth int) []byte {
	body := []byte{
		0x02, 0x7f, // block (result i32)
		0x1f, 0x40, 0x01, byte(wasm.CatchTag), 0x00, 0x00, // outer try_table catches tag 0 at the block
	}
	for range depth - 1 {
		body = append(body, 0x1f, 0x40, 0x00) // nested try_table with no catches
	}
	body = append(body, 0x41)
	body = append(body, wasmtest.SLEB32(42)...)
	body = append(body, 0x08, 0x00) // throw tag 0
	for range depth {
		body = append(body, 0x0b) // end try_table
	}
	body = append(body, 0x00, 0x0b, 0x0b) // unreachable; end block; end function
	return wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(
			wasmtest.FuncType([]wasm.ValType{wasm.I32}, nil),
			wasmtest.FuncType(nil, []wasm.ValType{wasm.I32}),
		)),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(1))),
		wasmtest.Section(13, wasmtest.Vec([]byte{0x00, 0x00})),
		wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("run", byte(wasm.ExternFunc), 0))),
		wasmtest.Section(10, wasmtest.Vec(wasmtest.Code(body))),
	)
}

func TestEHHandlerIndexDoesNotWrapAt256(t *testing.T) {
	const child = "WAGO_TEST_EH_HANDLER_INDEX_CHILD"
	if os.Getenv(child) == "1" {
		invokeEHIndexModule(t, ehHandlerIndexBoundaryModule(257), 42)
		return
	}
	invokeEHIndexModule(t, ehHandlerIndexBoundaryModule(256), 42)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, os.Args[0], "-test.run=^TestEHHandlerIndexDoesNotWrapAt256$", "-test.count=1")
	cmd.Env = append(os.Environ(), child+"=1")
	if output, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("257 nested handlers: %v\n%s", err, output)
	}
}

func invokeEHIndexModule(t *testing.T, data []byte, want uint64) {
	t.Helper()
	compiled, err := Compile(NewRuntimeConfig().WithCoreFeatures(CoreFeaturesV3), data)
	if err != nil {
		t.Fatal(err)
	}
	defer compiled.Close()
	in, err := Instantiate(compiled, InstantiateOptions{})
	if err != nil {
		t.Fatal(err)
	}
	defer in.Close()
	if got, err := in.Invoke("run"); err != nil || len(got) != 1 || got[0] != want {
		t.Fatalf("run = %v, %v; want [%d]", got, err, want)
	}
}

func ehRootIndexBoundaryModule(count int) []byte {
	instructions := make([]byte, 0, count*18)
	for i := 1; i <= count; i++ {
		instructions = append(instructions,
			0x02, 0x02, // block (type 2): (result i32 exnref)
			0x1f, 0x40, 0x01, byte(wasm.CatchRef), 0x00, 0x00, // try_table (catch_ref tag 0 label 0)
			0x41,
		)
		instructions = append(instructions, wasmtest.SLEB32(int32(i))...)
		instructions = append(instructions,
			0x08, 0x00, // throw tag 0
			0x0b, 0x00, 0x0b, // end try_table; unreachable; end block
		)
		if i == 1 {
			instructions = append(instructions, 0x21, 0x00, 0x1a) // save exnref; drop payload
		} else {
			instructions = append(instructions, 0x1a, 0x1a) // drop exnref and payload
		}
	}
	instructions = append(instructions,
		0x02, 0x7f, // block (result i32)
		0x1f, 0x40, 0x01, byte(wasm.CatchTag), 0x00, 0x00, // catch rethrown tag 0
		0x20, 0x00, 0x0a, // local.get 0; throw_ref
		0x0b, 0x00, 0x0b, // end try_table; unreachable; end block
		0x0b, // end function
	)
	function := append([]byte{0x01, 0x01, 0x69}, instructions...) // one exnref local
	code := append(wasmtest.ULEB(uint32(len(function))), function...)
	return wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(
			wasmtest.FuncType([]wasm.ValType{wasm.I32}, nil),
			wasmtest.FuncType(nil, []wasm.ValType{wasm.I32}),
			wasmtest.FuncType(nil, []wasm.ValType{wasm.I32, wasm.RefVal(wasm.AbsRef(wasm.HeapExn))}),
		)),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(1))),
		wasmtest.Section(13, wasmtest.Vec([]byte{0x00, 0x00})),
		wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("run", byte(wasm.ExternFunc), 0))),
		wasmtest.Section(10, wasmtest.Vec(code)),
	)
}

func TestEHRootIndexDoesNotWrapAt256(t *testing.T) {
	for _, count := range []int{256, 257} {
		t.Run(strconv.Itoa(count), func(t *testing.T) {
			invokeEHIndexModule(t, ehRootIndexBoundaryModule(count), 1)
		})
	}
}
