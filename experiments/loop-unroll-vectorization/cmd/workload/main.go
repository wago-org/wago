package main

import (
	"encoding/json"
	"flag"
	"fmt"
	wago "github.com/wago-org/wago"
	"os"
	"runtime"
	"time"
)

func main() {
	path := flag.String("wasm", "corpus/workloads/synthetic/memory.wasm", "input Wasm")
	export := flag.String("export", "sum", "function")
	repeat := flag.Int("repeat", 1, "number of verified calls")
	expected := flag.Uint64("expected", 0, "expected result; zero records the first result")
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
	inst, err := wago.Instantiate(mod, wago.InstantiateOptions{})
	must(err)
	defer inst.Close()
	instantiateNS := time.Since(start).Nanoseconds()
	if *export == "sum" {
		_, err = inst.Invoke("fill", wago.I32(8192))
		must(err)
	}
	start = time.Now()
	want := *expected
	var result uint64
	for i := 0; i < *repeat; i++ {
		var out []uint64
		if *export == "sum" {
			out, err = inst.Invoke(*export, wago.I32(8192))
		} else {
			out, err = inst.Invoke(*export)
		}
		must(err)
		if len(out) != 1 {
			must(fmt.Errorf("expected one result"))
		}
		result = out[0]
		if *export != "sum" {
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
