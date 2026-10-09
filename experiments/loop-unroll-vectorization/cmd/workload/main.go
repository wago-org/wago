package main

import (
	"encoding/json"
	"flag"
	"fmt"
	wago "github.com/wago-org/wago"
	"os"
	"path/filepath"
	"runtime"
	"time"
)

func main() {
	path := flag.String("wasm", "corpus/workloads/synthetic/memory.wasm", "input Wasm")
	export := flag.String("export", "sum", "function")
	repeat := flag.Int("repeat", 1, "number of verified calls")
	expected := flag.Uint64("expected", 0, "expected result; zero records the first result")
	argument := flag.Int64("arg", -1, "optional i32 argument")
	initialize := flag.String("init", "", "optional initialization export")
	flag.Parse()
	raw, err := os.ReadFile(*path)
	must(err)
	var before, compiled, done runtime.MemStats
	runtime.ReadMemStats(&before)
	start := time.Now()
	mod, err := wago.Compile(nil, raw)
	must(err)
	compileNS := time.Since(start).Nanoseconds()
	runtime.ReadMemStats(&compiled)
	defer mod.Close()
	start = time.Now()
	imports := wago.NewImports()
	imports.HostFunc("env", "abort", func(wago.HostCall) { panic("unexpected env.abort") })
	inst, err := wago.Instantiate(mod, wago.InstantiateOptions{Imports: imports})
	must(err)
	defer inst.Close()
	instantiateNS := time.Since(start).Nanoseconds()
	if *initialize != "" {
		_, err = inst.Invoke(*initialize)
		must(err)
	}
	if *export == "sum" && filepath.Base(*path) == "memory.wasm" {
		_, err = inst.Invoke("fill", wago.I32(8192))
		must(err)
	}
	start = time.Now()
	want := *expected
	var result uint64
	for i := 0; i < *repeat; i++ {
		var out []uint64
		if *export == "sum" {
			n := int32(8192)
			if *argument >= 0 {
				n = int32(*argument)
			}
			out, err = inst.Invoke(*export, wago.I32(n))
		} else if *argument >= 0 {
			out, err = inst.Invoke(*export, wago.I32(int32(*argument)))
		} else {
			out, err = inst.Invoke(*export)
		}
		must(err)
		if len(out) != 1 {
			must(fmt.Errorf("expected one result"))
		}
		result = out[0]
		if *export != "sum" || filepath.Base(*path) != "memory.wasm" {
			result = uint64(uint32(out[0]))
		}
		if want == 0 {
			want = result
		}
		if result != want {
			must(fmt.Errorf("result %d differs from %d", result, want))
		}
	}
	executionNS := time.Since(start).Nanoseconds()
	runtime.ReadMemStats(&done)
	must(json.NewEncoder(os.Stdout).Encode(map[string]any{"wasm": *path, "export": *export, "repeat": *repeat, "result": result, "compile_ns": compileNS, "instantiate_ns": instantiateNS, "execute_ns": executionNS, "compile_bytes": compiled.TotalAlloc - before.TotalAlloc, "compile_allocs": compiled.Mallocs - before.Mallocs, "remaining_bytes": done.TotalAlloc - compiled.TotalAlloc, "remaining_allocs": done.Mallocs - compiled.Mallocs}))
}
func must(err error) {
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
