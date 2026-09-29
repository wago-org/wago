//go:build linux && amd64 && wago_profile

// Command wasmunwind qualifies compiler-generated direct-recursion frame rules.
// It uses Wago's normal public API and native transitions, not a test trampoline.
package main

import (
	"encoding/json"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"time"

	"github.com/wago-org/wago"
	"github.com/wago-org/wago/profile"
	"github.com/wago-org/wago/src/core/compiler/wasm"
	"github.com/wago-org/wago/tests/support/wasmtest"
)

func must(err error) {
	if err != nil {
		panic(err)
	}
}
func save(dir, name string, value any) {
	data, err := json.MarshalIndent(value, "", "  ")
	must(err)
	must(os.WriteFile(filepath.Join(dir, name), data, 0600))
}

func recursiveModule(mixed bool) []byte {
	// Recurse until depth zero. The deepest activation counts down 100,000
	// times, then returns zero (integer ABI) or the forwarded f64 argument.
	params := []wasm.ValType{wasm.I32}
	result := wasm.I32
	counter := byte(1)
	leaf := []byte{0x41, 0}
	args := []byte{0x20, 0, 0x41, 1, 0x6b}
	if mixed {
		params = append(params, wasm.F64)
		result = wasm.F64
		counter = 2
		leaf = []byte{0x20, 1}
		args = append(args, 0x20, 1)
	}
	body := []byte{1, 1, 0x7f, 0x20, 0, 0x45, 0x04, wasm.MustEncodeValType(result), 0x41}
	body = append(body, wasmtest.SLEB32(100000)...)
	body = append(body, 0x21, counter, 0x03, 0x40, 0x20, counter, 0x41, 1, 0x6b, 0x22, counter, 0x0d, 0, 0x0b)
	body = append(body, leaf...)
	body = append(body, 0x05)
	body = append(body, args...)
	body = append(body, 0x10, 0, 0x0b, 0x0b)
	return wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType(params, []wasm.ValType{result}))),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(0))),
		wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("recurse", 0, 0))),
		wasmtest.Section(10, wasmtest.Vec(append(wasmtest.ULEB(uint32(len(body))), body...))),
	)
}

func main() {
	if len(os.Args) != 4 || (os.Args[2] != "reg" && os.Args[2] != "wrapper" && os.Args[2] != "mixed") || (os.Args[3] != "maps" && os.Args[3] != "none") {
		panic("usage: wasmunwind NEW_DIRECTORY reg|wrapper|mixed maps|none")
	}
	dir := os.Args[1]
	must(os.Mkdir(dir, 0700))
	maps := os.Args[3] == "maps"
	session := wago.NewCodeProfile(wago.CodeProfileOptions{IncludeCode: true, UnwindMaps: maps})
	defer session.Close()
	config := wago.NewRuntimeConfig().WithCodeProfile(session).WithOptimization("inline", false).WithOptimization("reg-abi", os.Args[2] != "wrapper")
	wasmBytes := recursiveModule(os.Args[2] == "mixed")
	must(os.WriteFile(filepath.Join(dir, "module.wasm"), wasmBytes, 0600))
	compiled, err := wago.Compile(config, wasmBytes)
	must(err)
	defer compiled.Close()
	instance, err := wago.Instantiate(compiled)
	must(err)
	defer instance.Close()
	events, status := session.Read(0)
	if status.Dropped != 0 || len(events) != 1 || events[0].Image == nil {
		panic("invalid image publication")
	}
	if (len(events[0].Image.Unwind) > 0) != maps {
		panic("compiler did not supply requested frame rules")
	}
	dump, err := profile.OpenJITDump(dir)
	must(err)
	defer dump.Close()
	must(dump.Write(events))
	args := []uint64{8}
	expected := uint64(0)
	if os.Args[2] == "mixed" {
		expected = math.Float64bits(42.25)
		args = append(args, expected)
	}
	start := time.Now()
	iterations := 0
	for time.Since(start) < 2*time.Second {
		result, err := instance.Invoke("recurse", args...)
		must(err)
		if len(result) != 1 || result[0] != expected {
			panic("incorrect recursive result")
		}
		iterations++
	}
	elapsed := time.Since(start)
	must(instance.Close())
	must(compiled.Close())
	events, status = session.Read(0)
	if status.Dropped != 0 || len(events) != 2 || events[1].Kind != "retire" {
		panic("invalid retirement history")
	}
	must(dump.Write(events[1:]))
	must(dump.Close())
	save(dir, "images.json", events)
	save(dir, "fixture.json", map[string]any{"kind": "Railshot direct recursion", "register_abi": os.Args[2] != "wrapper", "mixed_signature": os.Args[2] == "mixed", "inlining": false, "unwind_maps": maps, "expected_guest_depth": 9, "iterations": iterations, "elapsed_ns": elapsed.Nanoseconds(), "metadata_status": status})
	fmt.Printf("validated %d invocations; requested unwind maps=%v\n", iterations, maps)
}
