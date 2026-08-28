// Command executionworker measures one prepared Wago export in a fresh process.
package main

import (
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/wago-org/wago/src/wago"
)

type result struct {
	Engine     string  `json:"engine"`
	Module     string  `json:"module"`
	Export     string  `json:"export"`
	Round      int     `json:"round"`
	Iterations uint64  `json:"iterations"`
	ElapsedNS  int64   `json:"elapsed_ns"`
	NSPerOp    float64 `json:"ns_per_op"`
}

func main() {
	engineName := flag.String("engine", "", "dragline or railshot")
	modulePath := flag.String("module", "", "input Wasm module")
	initName := flag.String("init", "", "optional initialization export")
	exportName := flag.String("export", "", "export to benchmark")
	argsText := flag.String("args", "", "comma-separated i32 arguments")
	round := flag.Int("round", 0, "measurement round")
	benchtime := flag.Duration("benchtime", 100*time.Millisecond, "target measured duration")
	outPath := flag.String("out", "", "append JSON Lines here")
	flag.Parse()
	if *modulePath == "" || *exportName == "" || *outPath == "" || *benchtime <= 0 {
		fatal(errors.New("-module, -export, -out, and a positive -benchtime are required"))
	}
	var compiler wago.CompilerEngine
	switch *engineName {
	case "dragline":
		compiler = wago.CompilerDragline
	case "railshot":
		compiler = wago.CompilerRailshot
	default:
		fatal(fmt.Errorf("unknown engine %q", *engineName))
	}
	source, err := os.ReadFile(*modulePath)
	if err != nil {
		fatal(err)
	}
	compiled, err := wago.NewRuntimeConfig().WithCompiler(compiler).WithTarget(wago.TargetNative).
		WithBoundsChecks(wago.BoundsChecksExplicit).WithFunctionWorkers(8).Compile(source)
	if err != nil {
		fatal(err)
	}
	defer compiled.Close()
	instance, err := wago.Instantiate(compiled, wago.InstantiateOptions{Imports: wago.Imports{
		"env.abort": wago.HostFunc(func(wago.HostModule, []uint64, []uint64) {}),
	}})
	if err != nil {
		fatal(err)
	}
	defer instance.Close()
	if *initName != "" {
		if _, err := instance.Invoke(*initName); err != nil {
			fatal(err)
		}
	}
	fn, err := instance.PrepareFunction(*exportName)
	if err != nil {
		fatal(err)
	}
	args, err := parseArgs(*argsText)
	if err != nil {
		fatal(err)
	}
	invoke := func(iterations uint64) error {
		for range iterations {
			if _, err := fn.Invoke(args...); err != nil {
				return err
			}
		}
		return nil
	}
	if err := invoke(1); err != nil {
		fatal(err)
	}
	iterations := calibrate(invoke, *benchtime)
	started := time.Now()
	if err := invoke(iterations); err != nil {
		fatal(err)
	}
	elapsed := time.Since(started)
	row := result{Engine: *engineName, Module: *modulePath, Export: *exportName, Round: *round,
		Iterations: iterations, ElapsedNS: elapsed.Nanoseconds(), NSPerOp: float64(elapsed.Nanoseconds()) / float64(iterations)}
	output, err := os.OpenFile(*outPath, os.O_WRONLY|os.O_CREATE|os.O_APPEND, 0o644)
	if err != nil {
		fatal(err)
	}
	defer output.Close()
	if err := json.NewEncoder(output).Encode(row); err != nil {
		fatal(err)
	}
}

func calibrate(invoke func(uint64) error, target time.Duration) uint64 {
	iterations := uint64(1)
	for {
		started := time.Now()
		if err := invoke(iterations); err != nil {
			fatal(err)
		}
		elapsed := time.Since(started)
		if elapsed >= target/10 || iterations >= 1<<40 {
			if elapsed <= 0 {
				return iterations
			}
			scaled := uint64(float64(iterations) * float64(target) / float64(elapsed))
			if scaled < 1 {
				return 1
			}
			return scaled
		}
		iterations *= 10
	}
}

func parseArgs(text string) ([]uint64, error) {
	if text == "" {
		return nil, nil
	}
	parts := strings.Split(text, ",")
	args := make([]uint64, len(parts))
	for i, part := range parts {
		value, err := strconv.ParseInt(part, 10, 32)
		if err != nil {
			return nil, err
		}
		args[i] = wago.I32(int32(value))
	}
	return args, nil
}

func fatal(err error) {
	fmt.Fprintln(os.Stderr, "executionworker:", err)
	os.Exit(1)
}
