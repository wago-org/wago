//go:build linux && (amd64 || arm64)

package wagobench

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"runtime"
	"strconv"
	"strings"
	"testing"
	"time"

	wago "github.com/wago-org/wago"
)

type cowProcessMemory struct {
	PSSKB, RSSKB, PrivateDirtyKB, SharedCleanKB int
	HeapAllocKB                                 uint64
}

func cowReadProcessMemory() (cowProcessMemory, error) {
	raw, err := os.ReadFile("/proc/self/smaps_rollup")
	if err != nil {
		return cowProcessMemory{}, err
	}
	var out cowProcessMemory
	for _, line := range strings.Split(string(raw), "\n") {
		fields := strings.Fields(line)
		if len(fields) < 2 {
			continue
		}
		value, err := strconv.Atoi(fields[1])
		if err != nil {
			continue
		}
		switch fields[0] {
		case "Pss:":
			out.PSSKB = value
		case "Rss:":
			out.RSSKB = value
		case "Private_Dirty:":
			out.PrivateDirtyKB = value
		case "Shared_Clean:":
			out.SharedCleanKB = value
		}
	}
	var mem runtime.MemStats
	runtime.ReadMemStats(&mem)
	out.HeapAllocKB = mem.HeapAlloc / 1024
	if out.PSSKB == 0 || out.RSSKB == 0 {
		return out, fmt.Errorf("incomplete smaps_rollup: PSS=%d RSS=%d", out.PSSKB, out.RSSKB)
	}
	return out, nil
}

type cowPostExecReport struct {
	Mode, StateSHA256        string
	Instances, SourceBytes   int
	NativeBytes, MemoryBytes int
	CompileMS, InstantiateMS float64
	ExecuteMS, HashMS        float64
	Compiled, Initialized    cowProcessMemory
	Executed, ReadAll        cowProcessMemory
}

// Run this test in a separate process for each mode so the Go heap, memfd,
// native image, and live Wasm instances do not contaminate the other arm.
// Example: WAGO_924_POSTEXEC_MODE=cow WAGO_924_POSTEXEC_INSTANCES=8
// go test ./bench/suite -run '^TestCowPHPPostExecutionMemory$' -v
// -args -wago.corpus=php-buckets.
func TestCowPHPPostExecutionMemory(t *testing.T) {
	mode := os.Getenv("WAGO_924_POSTEXEC_MODE")
	if mode == "" {
		t.Skip("opt-in separate-process post-execution memory study")
	}
	if mode != "baseline" && mode != "cow" {
		t.Fatalf("invalid mode %q", mode)
	}
	count, err := strconv.Atoi(os.Getenv("WAGO_924_POSTEXEC_INSTANCES"))
	if err != nil || count < 1 || count > 10 {
		t.Fatalf("instances must be 1..10: %v", err)
	}
	flag := "0"
	if mode == "cow" {
		flag = "1"
	}
	t.Setenv("WAGO_EXPERIMENT_COW_IMAGE", flag)
	m := cowPHPCommand(t)
	stdin := commandInput(t, m)
	report := cowPostExecReport{Mode: mode, Instances: count, SourceBytes: len(m.bytes)}
	started := time.Now()
	compiled, err := wago.Compile(wago.NewRuntimeConfig().WithBoundsChecks(wago.BoundsChecksExplicit), m.bytes)
	report.CompileMS = float64(time.Since(started).Microseconds()) / 1000
	if err != nil {
		t.Fatal(err)
	}
	defer compiled.Close()
	report.NativeBytes = compiled.CodeSize()
	if report.Compiled, err = cowReadProcessMemory(); err != nil {
		t.Fatal(err)
	}
	instances := make([]*wago.Instance, 0, count)
	cleanups := make([]func(), 0, count)
	type commandState struct {
		preopen        string
		stdout, stderr *bytes.Buffer
	}
	states := make([]commandState, 0, count)
	defer func() {
		for _, in := range instances {
			_ = in.Close()
		}
		for _, cleanup := range cleanups {
			cleanup()
		}
	}()
	for i := 0; i < count; i++ {
		preopen, cleanup, err := commandScratchPreopen(m)
		if err != nil {
			t.Fatal(err)
		}
		cleanups = append(cleanups, cleanup)
		stdout, stderr := new(bytes.Buffer), new(bytes.Buffer)
		imports, err := commandRuntimeImports(m, preopen, stdin, stdout, stderr)
		if err != nil {
			t.Fatal(err)
		}
		started = time.Now()
		in, err := wago.Instantiate(compiled, wago.InstantiateOptions{Imports: imports})
		report.InstantiateMS += float64(time.Since(started).Microseconds()) / 1000
		if err != nil {
			t.Fatal(err)
		}
		instances = append(instances, in)
		states = append(states, commandState{preopen: preopen, stdout: stdout, stderr: stderr})
	}
	if report.Initialized, err = cowReadProcessMemory(); err != nil {
		t.Fatal(err)
	}
	// The command loop uses those same retained instances and their host bindings.
	for i, in := range instances {
		started = time.Now()
		results, invokeErr := in.Invoke(m.Command.Export)
		report.ExecuteMS += float64(time.Since(started).Microseconds()) / 1000
		if !commandExitOK(invokeErr) {
			t.Fatalf("instance %d: %v", i, invokeErr)
		}
		files, err := commandOutputFiles(m, states[i].preopen)
		if err != nil {
			t.Fatal(err)
		}
		got := commandOutput{results: results, stdout: states[i].stdout.Bytes(), stderr: states[i].stderr.Bytes(), files: files}
		if err := validateCommandOutput(m, got); err != nil {
			t.Fatalf("instance %d oracle: %v", i, err)
		}
	}
	if report.Executed, err = cowReadProcessMemory(); err != nil {
		t.Fatal(err)
	}
	started = time.Now()
	h := sha256.New()
	for _, in := range instances {
		memory := in.Memory().UnsafeBytes()
		if report.MemoryBytes == 0 {
			report.MemoryBytes = len(memory)
		} else if report.MemoryBytes != len(memory) {
			t.Fatalf("different linear-memory sizes: %d and %d", report.MemoryBytes, len(memory))
		}
		_, _ = h.Write(memory)
	}
	report.HashMS = float64(time.Since(started).Microseconds()) / 1000
	report.StateSHA256 = hex.EncodeToString(h.Sum(nil))
	if report.ReadAll, err = cowReadProcessMemory(); err != nil {
		t.Fatal(err)
	}
	encoded, err := json.Marshal(report)
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("COW_POSTEXEC %s", encoded)
}
