// Command explain compiles a wasm module through the railshot backend and prints
// its per-function CodegenStats dashboard — the counters
// every later optimization proves itself against: pins, flushes, condenses,
// forced deferred loads, bounds checks, calls by kind, and peephole hits.
//
// Usage:
//
//	go run ./cmd/explain [-guard] [-compact] [-code image.bin] [-layout entries.json] [module.wasm]
//
// With no path it defaults to corpus/json-as.wasm. -guard selects guard-page
// (bounds-elided) mode instead of explicit bounds. Equivalent to setting
// WAGO_EXPLAIN=1 on any run that compiles the module, but standalone and
// corpus-aware.
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"

	wasm "github.com/wago-org/wago/src/core/compiler/wasm"
)

func main() {
	guard := flag.Bool("guard", false, "guard-page (bounds-elided) mode instead of explicit bounds")
	compact := flag.Bool("compact", false, "enable bounded native compaction")
	codePath := flag.String("code", "", "write the generated native code image to this path")
	layoutPath := flag.String("layout", "", "write generated function entry offsets to this path")
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

	code, entry, stats, err := compileExplain(m, *guard, *compact, *codePath != "")
	if err != nil {
		fmt.Fprintln(os.Stderr, "compile:", err)
		os.Exit(1)
	}
	if *codePath != "" {
		if err := os.WriteFile(*codePath, code, 0o644); err != nil {
			fmt.Fprintln(os.Stderr, "write code:", err)
			os.Exit(1)
		}
	}
	if *layoutPath != "" {
		layout, err := json.MarshalIndent(struct {
			Entry []int `json:"entry"`
		}{Entry: entry}, "", "  ")
		if err != nil {
			fmt.Fprintln(os.Stderr, "marshal layout:", err)
			os.Exit(1)
		}
		if err := os.WriteFile(*layoutPath, append(layout, '\n'), 0o644); err != nil {
			fmt.Fprintln(os.Stderr, "write layout:", err)
			os.Exit(1)
		}
	}

	mode := "explicit-bounds"
	if *guard {
		mode = "guard-page"
	}
	fmt.Printf("# %s  (%s, compact=%t)\n", path, mode, *compact)
	fmt.Print(stats)
}
