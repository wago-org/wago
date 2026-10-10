// Same-thread interleaved execution comparisons for unchanged application inputs.
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	wago "github.com/wago-org/wago"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"time"
)

type workload struct {
	ID       string   `json:"id"`
	Artifact string   `json:"artifact"`
	Export   string   `json:"export"`
	Args     []uint64 `json:"args"`
	Reset    string   `json:"reset"`
	Oracle   struct {
		Expected []string `json:"expected"`
	} `json:"oracle"`
}

func main() {
	coreExec := flag.Bool("core-exec", false, "paired simple cached-core execution with fresh instances per sample")
	coreCompile := flag.Bool("core-compile", false, "compile-only cached-core manifest; execution oracles must be qualified separately")
	root := flag.String("corpus", "/Users/work/Code/Web/wasm.fyi/corpora/applications", "application manifest directory")
	names := flag.String("workloads", "", "comma-separated workload basenames (required)")
	option := flag.String("option", "loop-memory-base", "optimization to compare")
	phase := flag.String("phase", "exec", "exec or compile")
	rounds := flag.Int("rounds", 8, "paired rounds")
	budget := flag.Duration("budget", 100*time.Millisecond, "target time per state per round")
	dump := flag.String("code-dir", "", "optional native image directory")
	flag.Parse()
	if *names == "" || *rounds < 1 || *budget <= 0 {
		panic("supply workloads, positive rounds and budget")
	}
	if *phase != "exec" && *phase != "compile" {
		panic("phase must be exec or compile")
	}
	selected := map[string]bool{}
	for _, name := range strings.Split(*names, ",") {
		selected[name] = true
	}
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	rc, qos := setBenchmarkQoS()
	if rc != 0 {
		panic(fmt.Sprintf("QoS request failed: %d", rc))
	}
	fmt.Printf("# locked OS thread; requested user-interactive QoS; returned class=%d\n", qos)
	data, err := os.ReadFile(filepath.Join(*root, "manifest.json"))
	must(err)
	var all []workload
	if *coreCompile || *coreExec {
		if *coreCompile && *coreExec || *coreCompile && *phase != "compile" || *coreExec && *phase != "exec" {
			panic("select one cached core mode matching the phase")
		}
		var manifest struct {
			Workloads []struct {
				ID, Artifact, ABI, Export, Reset, Initialize string
				HostProfile                                  string `json:"host_profile"`
				Input                                        json.RawMessage
				Args                                         []json.RawMessage
				Oracle                                       struct {
					Kind             string
					Expected, Memory []json.RawMessage
				}
			}
		}
		must(json.Unmarshal(data, &manifest))
		for _, w := range manifest.Workloads {
			if w.ABI != "core" {
				continue
			}
			normalized := workload{ID: w.ID, Artifact: w.Artifact, Reset: "stateless"}
			if *coreExec {
				if !selected[filepath.Base(w.ID)] {
					continue
				}
				if w.Oracle.Kind != "exact_u64" || len(w.Oracle.Memory) != 0 || w.HostProfile != "" || w.Initialize != "" || len(w.Input) != 0 && string(w.Input) != "null" || w.Reset != "fresh_instance_per_sample" && w.Reset != "stateless" {
					panic("core execution requires a simple exact-result contract")
				}
				normalized.Export = w.Export
				for _, value := range w.Args {
					normalized.Args = append(normalized.Args, parseCoreUint(value))
				}
				for _, value := range w.Oracle.Expected {
					normalized.Oracle.Expected = append(normalized.Oracle.Expected, strconv.FormatUint(parseCoreUint(value), 10))
				}
			}
			all = append(all, normalized)
		}
	} else {
		must(json.Unmarshal(data, &all))
	}
	encoder := json.NewEncoder(os.Stdout)
	for _, w := range all {
		name := filepath.Base(w.ID)
		if !selected[name] {
			continue
		}
		delete(selected, name)
		if w.Reset != "stateless" {
			panic("only stateless application contracts are supported")
		}
		code, err := os.ReadFile(filepath.Join(*root, w.Artifact))
		must(err)
		want := make([]uint64, len(w.Oracle.Expected))
		for i, s := range w.Oracle.Expected {
			want[i], err = strconv.ParseUint(s, 10, 64)
			must(err)
		}
		runs := make([]func(), 2)
		var renewSample [2]func()
		cleanups := make([]func(), 0, 2)
		for state := 0; state < 2; state++ {
			cfg := wago.NewRuntimeConfig().WithOptimization(*option, state == 1)
			if *coreCompile {
				// Execution correctness is checked by the cached-contract harness;
				// these samples isolate compilation with immutable policy options.
				runs[state] = func() {
					compiled, err := wago.CompileWithConfig(cfg, code)
					must(err)
					must(compiled.Close())
				}
				continue
			}
			c, err := wago.CompileWithConfig(cfg, code)
			must(err)
			if *dump != "" {
				must(os.MkdirAll(*dump, 0755))
				file, err := os.Create(filepath.Join(*dump, fmt.Sprintf("%s-%d.bin", name, state)))
				must(err)
				_, err = c.WriteCodeTo(file)
				must(err)
				must(file.Close())
			}
			if *coreExec {
				index := state
				var closeSample func()
				renewSample[index] = func() {
					if closeSample != nil {
						closeSample()
					}
					in, err := wago.Instantiate(c, wago.InstantiateOptions{})
					must(err)
					fn, err := in.WasmFunc(w.Export)
					must(err)
					runs[index] = func() {
						out, err := fn.Invoke(w.Args...)
						must(err)
						if len(out) != len(want) {
							panic("core oracle arity")
						}
						for i, value := range out {
							if value != want[i] {
								panic(fmt.Sprintf("%s oracle got %d want %d", name, value, want[i]))
							}
						}
					}
					closeSample = func() { must(in.Close()) }
					for i := 0; i < 5; i++ {
						runs[index]()
					}
				}
				renewSample[index]()
				cleanups = append(cleanups, func() { closeSample(); must(c.Close()) })
				continue
			}
			in, err := wago.Instantiate(c, wago.InstantiateOptions{})
			must(err)
			fn, err := in.WasmFunc(w.Export)
			must(err)
			runs[state] = func() {
				out, err := fn.Invoke(w.Args...)
				must(err)
				if len(out) != len(want) {
					panic("oracle result arity")
				}
				for i, v := range out {
					if v != want[i] {
						panic(fmt.Sprintf("%s oracle: got %d want %d", name, v, want[i]))
					}
				}
			}
			cleanups = append(cleanups, func() { must(in.Close()); must(c.Close()) })
			for i := 0; i < 5; i++ {
				runs[state]()
			}
			if *phase == "compile" {
				// Validate the original contract above, then time compilation and
				// release with the same per-compilation policy and source bytes.
				runs[state] = func() {
					compiled, err := wago.CompileWithConfig(cfg, code)
					must(err)
					must(compiled.Close())
				}
			}
		}
		start := time.Now()
		for i := 0; i < 32; i++ {
			runs[0]()
			runs[1]()
		}
		perCall := time.Since(start) / 64
		count := int(*budget / perCall)
		if count < 1 {
			count = 1
		}
		if count > 1000000 {
			count = 1000000
		}
		for round := 0; round < *rounds; round++ {
			order := [2]int{round % 2, 1 - round%2}
			for _, state := range order {
				if renewSample[state] != nil {
					renewSample[state]()
				}
				start := time.Now()
				for i := 0; i < count; i++ {
					runs[state]()
				}
				elapsed := time.Since(start)
				must(encoder.Encode(struct {
					Workload     string  `json:"workload"`
					Phase        string  `json:"phase"`
					Round        int     `json:"round"`
					On           bool    `json:"on"`
					Iterations   int     `json:"iterations"`
					Microseconds float64 `json:"us"`
				}{name, *phase, round, state == 1, count, float64(elapsed.Nanoseconds()) / float64(count) / 1000}))
			}
		}
		for _, close := range cleanups {
			close()
		}
	}
	if len(selected) != 0 {
		panic(fmt.Sprintf("unknown workloads: %v", selected))
	}
}
func must(err error) {
	if err != nil {
		panic(err)
	}
}

func parseCoreUint(raw json.RawMessage) uint64 {
	value := string(raw)
	if len(value) > 0 && value[0] == '"' {
		must(json.Unmarshal(raw, &value))
	}
	base := 10
	if strings.HasPrefix(value, "0x") || strings.HasPrefix(value, "0X") {
		base = 0
	}
	parsed, err := strconv.ParseUint(value, base, 64)
	must(err)
	return parsed
}
