// Command explain compiles a wasm module through the railshot backend and prints
// its per-function CodegenStats dashboard (docs/no-ir-plan.md P1) — the counters
// every later optimization proves itself against: pins, flushes, condenses,
// forced deferred loads, bounds checks, calls by kind, and peephole hits.
//
// Usage:
//
//	go run ./cmd/explain [-guard] [module.wasm]
//
// With no path it defaults to corpus/json-as.wasm. -guard selects guard-page
// (bounds-elided) mode instead of explicit bounds. Equivalent to setting
// WAGO_EXPLAIN=1 on any run that compiles the module, but standalone and
// corpus-aware.
package main

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"

	wasm "github.com/wago-org/wago/src/core/compiler/wasm"
)

func main() {
	guard := flag.Bool("guard", false, "guard-page (bounds-elided) mode instead of explicit bounds")
	flag.Parse()

	path := filepath.Join("corpus", "json-as.wasm")
	if flag.NArg() > 0 {
		path = flag.Arg(0)
	}

	b, err := os.ReadFile(path)
	if err != nil {
		fmt.Fprintln(os.Stderr, "read module:", err)
		os.Exit(1)
	}
	m, err := wasm.DecodeModule(b)
	if err != nil {
		fmt.Fprintln(os.Stderr, "decode:", err)
		os.Exit(1)
	}

	stats, err := compileExplain(m, *guard)
	if err != nil {
		fmt.Fprintln(os.Stderr, "compile:", err)
		os.Exit(1)
	}

	mode := "explicit-bounds"
	if *guard {
		mode = "guard-page"
	}
	fmt.Printf("# %s  (%s)\n", path, mode)
	fmt.Print(stats)
}
