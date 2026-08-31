// Command draglinemetrics compiles one validated module through Dragline and
// writes canonical machine-readable compiler measurements. On a function
// failure, -replay writes the corresponding strict replay artifact.
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"

	corecompiler "github.com/wago-org/wago/src/core/compiler"
	"github.com/wago-org/wago/src/core/compiler/backend/dragline"
	"github.com/wago-org/wago/src/core/compiler/wasm"
	runtimeabi "github.com/wago-org/wago/src/core/runtime/abi"
)

func main() {
	outPath := flag.String("out", "", "write JSON metrics to this path instead of stdout")
	codePath := flag.String("code", "", "write the generated native code image to this path")
	layoutPath := flag.String("layout", "", "write generated entry offsets to this path")
	replayPath := flag.String("replay", "", "write a replay artifact here if a function fails")
	targetMode := flag.String("target", "compat", "target mode: compat or native")
	boundsMode := flag.String("bounds", "explicit", "bounds mode: explicit or signals")
	flag.Parse()
	if flag.NArg() != 1 {
		fmt.Fprintln(os.Stderr, "usage: draglinemetrics [-target compat|native] [-bounds explicit|signals] [-out metrics.json] [-replay failure.json] module.wasm")
		os.Exit(2)
	}
	if *targetMode != "compat" && *targetMode != "native" {
		fmt.Fprintln(os.Stderr, "draglinemetrics: -target must be compat or native")
		os.Exit(2)
	}
	if *boundsMode != "explicit" && *boundsMode != "signals" {
		fmt.Fprintln(os.Stderr, "draglinemetrics: -bounds must be explicit or signals")
		os.Exit(2)
	}

	source, err := os.ReadFile(flag.Arg(0))
	if err != nil {
		fail("read module", err)
	}
	m, err := wasm.DecodeModule(source)
	if err != nil {
		fail("decode module", err)
	}
	if err := wasm.ValidateModule(m); err != nil {
		fail("validate module", err)
	}

	var metrics dragline.Metrics
	compiler := dragline.Compiler{Metrics: &metrics}
	if *replayPath != "" {
		compiler.Replay = func(replay corecompiler.ReplayArtifact) error {
			encoded, err := corecompiler.MarshalReplay(replay)
			if err != nil {
				return err
			}
			return os.WriteFile(*replayPath, append(encoded, '\n'), 0o644)
		}
	}
	mode := corecompiler.TargetCompatibility
	if *targetMode == "native" {
		mode = corecompiler.TargetNative
	}
	bounds := corecompiler.BoundsExplicit
	if *boundsMode == "signals" {
		bounds = corecompiler.BoundsSignals
	}
	target, err := corecompiler.HostTarget(mode)
	if err != nil {
		fail("resolve target", err)
	}
	compiled, err := compiler.Compile(corecompiler.Input{
		Module: m, Source: source,
		Runtime: corecompiler.RuntimeContract{ABIRevision: runtimeabi.Revision},
		Target:  target,
		Bounds:  bounds,
	})
	if err != nil {
		fail("compile", err)
	}
	if *codePath != "" {
		if err := os.WriteFile(*codePath, compiled.Code, 0o644); err != nil {
			fail("write code", err)
		}
	}
	if *layoutPath != "" {
		layout, err := json.MarshalIndent(struct {
			Entry         []int `json:"entry"`
			InternalEntry []int `json:"internal_entry"`
		}{compiled.Entry, compiled.InternalEntry}, "", "  ")
		if err != nil {
			fail("encode layout", err)
		}
		if err := os.WriteFile(*layoutPath, append(layout, '\n'), 0o644); err != nil {
			fail("write layout", err)
		}
	}

	var output = os.Stdout
	if *outPath != "" {
		output, err = os.Create(*outPath)
		if err != nil {
			fail("create metrics", err)
		}
		defer output.Close()
	}
	encoder := json.NewEncoder(output)
	encoder.SetIndent("", "  ")
	if err := encoder.Encode(metrics); err != nil {
		fail("write metrics", err)
	}
}

func fail(operation string, err error) {
	fmt.Fprintf(os.Stderr, "draglinemetrics: %s: %v\n", operation, err)
	os.Exit(1)
}
