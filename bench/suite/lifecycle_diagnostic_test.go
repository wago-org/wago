//go:build !windows

package wagobench

import (
	"flag"
	"io"
	"testing"

	"github.com/wago-org/wago"
	"github.com/wago-org/wago/tests/support/wasmtest"
)

var lifecycleDiagnostics = flag.Bool("wago.bench.lifecycle", false, "enable setup and cleanup diagnostic benchmarks")

func minimalWASICommand() corpusModule {
	importEntry := append(wasmtest.Name("wasi_snapshot_preview1"), wasmtest.Name("args_sizes_get")...)
	importEntry = append(importEntry, 0, 0)
	return corpusModule{
		ID:      "minimal-wasi",
		Command: &commandEntry{Runtime: "wasi", Export: "run", Want: []uint64{0}},
		bytes: wasmtest.Module(
			wasmtest.Section(1, wasmtest.Vec([]byte{0x60, 2, 0x7f, 0x7f, 1, 0x7f}, []byte{0x60, 0, 1, 0x7f})),
			wasmtest.Section(2, wasmtest.Vec(importEntry)),
			wasmtest.Section(3, []byte{1, 1}),
			wasmtest.Section(5, []byte{1, 1, 1, 2}),
			wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("run", 0, 1), wasmtest.ExportEntry("memory", 2, 0))),
			wasmtest.Section(10, wasmtest.Vec(wasmtest.Code([]byte{0x41, 0, 0x41, 4, 0x10, 0, 0x0b}))),
		),
	}
}

func TestMinimalWASICommand(t *testing.T) {
	m := minimalWASICommand()
	c, err := wago.Compile(nil, m.bytes)
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	got, err := runWagoCommand(m, c, nil, true)
	if err != nil {
		t.Fatal(err)
	}
	if err := validateCommandOutput(m, got); err != nil {
		t.Fatal(err)
	}
}

// Phases exclude other work with the benchmark timer, not with the process-wide
// profiler. Profile Imports or Lifecycle to avoid attributing excluded setup to
// a timed phase. Every phase that creates a guest uses a fresh raw WASI bundle.
func BenchmarkCommandLifecycleDiagnostic(b *testing.B) {
	if !*lifecycleDiagnostics {
		b.Skip("enable with -wago.bench.lifecycle")
	}
	modules := []corpusModule{minimalWASICommand()}
	for _, m := range commandCorpus(b) {
		if m.ID == "tinyxml2" || m.ID == "cjson" {
			modules = append(modules, m)
		}
	}
	for _, m := range modules {
		b.Run(m.ID, func(b *testing.B) {
			c, err := wago.Compile(nil, m.bytes)
			if err != nil {
				b.Fatal(err)
			}
			defer c.Close()
			stdin := commandInput(b, m)
			got, err := runWagoCommand(m, c, stdin, true)
			if err != nil {
				b.Fatal(err)
			}
			if err := validateCommandOutput(m, got); err != nil {
				b.Fatal(err)
			}
			for _, phase := range []string{"Imports", "Instantiate", "LookupExecute", "Close", "Lifecycle"} {
				b.Run(phase, func(b *testing.B) {
					b.ReportAllocs()
					b.ResetTimer()
					for i := 0; i < b.N; i++ {
						if phase == "Lifecycle" {
							if _, err := runWagoCommand(m, c, stdin, false); err != nil {
								b.Fatal(err)
							}
							continue
						}
						if phase != "Imports" {
							b.StopTimer()
						}
						imports, err := commandRuntimeImports(m, stdin, io.Discard, io.Discard)
						if err != nil {
							b.Fatal(err)
						}
						if phase == "Imports" {
							continue
						}
						if phase == "Instantiate" {
							b.StartTimer()
						}
						in, err := wago.Instantiate(c, wago.InstantiateOptions{Imports: imports})
						if err != nil {
							b.Fatal(err)
						}
						if phase == "Instantiate" {
							b.StopTimer()
						}
						if phase == "LookupExecute" {
							b.StartTimer()
						}
						_, err = in.Invoke(m.Command.Export)
						if !commandExitOK(err) {
							in.Close()
							b.Fatal(err)
						}
						if phase == "LookupExecute" {
							b.StopTimer()
						}
						if phase == "Close" {
							b.StartTimer()
						}
						if err := in.Close(); err != nil {
							b.Fatal(err)
						}
						if phase == "Close" {
							b.StopTimer()
						}
					}
				})
			}
		})
	}
}
