// Run from the Wago checkout whose compiler is being measured:
// go build -o /tmp/compile-corpus docs/benchmarks/wasm3-2026-10-12/compile-corpus.go
// /usr/bin/time -l /tmp/compile-corpus /path/to/absolute-wasm-files.json
// The manifest is a sorted JSON array of module/module_definition file paths.
package main

import (
	"encoding/json"
	"fmt"
	"os"
	"runtime"
	"time"

	"github.com/wago-org/wago/src/wago"
)

func main() {
	if len(os.Args) != 2 {
		panic("expected JSON array of Wasm file paths")
	}
	raw, err := os.ReadFile(os.Args[1])
	if err != nil {
		panic(err)
	}
	var paths []string
	if err := json.Unmarshal(raw, &paths); err != nil {
		panic(err)
	}
	if len(paths) == 0 {
		panic("empty corpus")
	}
	modules := make([][]byte, len(paths))
	for i, path := range paths {
		modules[i], err = os.ReadFile(path)
		if err != nil {
			panic(err)
		}
	}
	cfg := wago.NewRuntimeConfig().WithCoreFeatures(wago.CoreFeaturesV3)
	compilePass := func() int {
		codeBytes := 0
		for i, module := range modules {
			compiled, err := cfg.Compile(module)
			if err != nil {
				panic(fmt.Sprintf("%s: %v", paths[i], err))
			}
			codeBytes += compiled.CodeSize()
			if err := compiled.Close(); err != nil {
				panic(err)
			}
		}
		return codeBytes
	}
	compilePass() // Warm compiler pools before measuring allocations and latency.
	runtime.GC()
	var before, after runtime.MemStats
	runtime.ReadMemStats(&before)
	started := time.Now()
	const passes = 100
	codeBytes := 0
	for range passes {
		codeBytes += compilePass()
	}
	elapsed := time.Since(started)
	runtime.ReadMemStats(&after)
	runtime.KeepAlive(modules)
	result := struct {
		Modules, Passes, Compiles, CodeBytes int
		ElapsedNS, AllocBytes, Allocations   uint64
	}{len(modules), passes, len(modules) * passes, codeBytes,
		uint64(elapsed), after.TotalAlloc - before.TotalAlloc, after.Mallocs - before.Mallocs}
	if err := json.NewEncoder(os.Stdout).Encode(result); err != nil {
		panic(err)
	}
}
