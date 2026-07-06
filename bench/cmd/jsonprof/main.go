// jsonprof runs the json-as (SWAR) serialize/deserialize workload through the
// amd64 backend in a tight loop, and writes a /tmp/perf-<pid>.map JIT symbol map so
// `perf` can attribute samples to individual wasm functions.
//
// Usage:
//
//	go build -o /tmp/jsonprof ./cmd/jsonprof
//	perf record -g -F 4000 -o /tmp/j.data -- /tmp/jsonprof 15s
//	perf report -i /tmp/j.data --stdio | head -60
//
// Set WAGO_JSON_MODULE to the module path, or it defaults to
// $HOME/Code/AssemblyScript/json-as/build/wago-bench.swar.wasm.
// Pass "guard" as a 2nd arg to use signals-based (guard-page) bounds.
package main

import (
	"fmt"
	"os"
	"time"

	"github.com/wago-org/wago/src/wago"
)

func modulePath() string {
	if p := os.Getenv("WAGO_JSON_MODULE"); p != "" {
		return p
	}
	return os.Getenv("HOME") + "/Code/AssemblyScript/json-as/build/wago-bench.swar.wasm"
}

func main() {
	dur := 15 * time.Second
	if len(os.Args) > 1 {
		if d, err := time.ParseDuration(os.Args[1]); err == nil {
			dur = d
		}
	}
	b, err := os.ReadFile(modulePath())
	if err != nil {
		fmt.Fprintln(os.Stderr, "read module:", err)
		os.Exit(1)
	}
	cfg := wago.NewRuntimeConfig()
	if len(os.Args) > 2 && os.Args[2] == "guard" {
		cfg = cfg.WithBoundsChecks(wago.BoundsChecksSignalsBased)
	}
	c, err := wago.CompileWithConfig(cfg, b)
	if err != nil {
		fmt.Fprintln(os.Stderr, "compile:", err)
		os.Exit(1)
	}
	in, err := wago.Instantiate(c, wago.Imports{"env.abort": wago.HostFunc(func(wago.HostModule, []uint64, []uint64) {})})
	if err != nil {
		fmt.Fprintln(os.Stderr, "instantiate:", err)
		os.Exit(1)
	}
	if _, err := in.Invoke("_initialize"); err != nil {
		fmt.Fprintln(os.Stderr, "_initialize:", err)
		os.Exit(1)
	}

	writePerfMap(in, c)

	fmt.Printf("jsonprof pid=%d running %s (perf map at /tmp/perf-%d.map)\n", os.Getpid(), dur, os.Getpid())
	deadline := time.Now().Add(dur)
	var sink int64
	only := os.Getenv("WAGO_JSONPROF_ONLY") // "ser" / "deser" / "" (both)
	for time.Now().Before(deadline) {
		for i := 0; i < 200; i++ {
			if only != "deser" {
				r, err := in.Invoke("serializeN", 256)
				if err != nil {
					fmt.Fprintln(os.Stderr, "serializeN:", err)
					os.Exit(1)
				}
				sink += int64(r[0])
			}
			if only != "ser" {
				r, err := in.Invoke("deserializeN", 256)
				if err != nil {
					fmt.Fprintln(os.Stderr, "deserializeN:", err)
					os.Exit(1)
				}
				sink += int64(r[0])
			}
		}
	}
	fmt.Printf("done (sink=%d)\n", sink&1)
}

// writePerfMap emits a /tmp/perf-<pid>.map with one line per local wasm function:
// "<start_hex> <size_hex> <name>". perf reads this to symbolize JIT code.
func writePerfMap(in *wago.Instance, c *wago.Compiled) {
	base, entries := in.CodeBase()
	f, err := os.Create(fmt.Sprintf("/tmp/perf-%d.map", os.Getpid()))
	if err != nil {
		fmt.Fprintln(os.Stderr, "perf map:", err)
		return
	}
	defer f.Close()
	codeLen := len(c.Code)
	for i, off := range entries {
		end := codeLen
		if i+1 < len(entries) {
			end = entries[i+1]
		}
		name, ok := c.LocalFuncName(i)
		if !ok || name == "" {
			name = fmt.Sprintf("wasmfunc%d", i)
		}
		fmt.Fprintf(f, "%x %x %s\n", base+uintptr(off), end-off, name)
	}
}
