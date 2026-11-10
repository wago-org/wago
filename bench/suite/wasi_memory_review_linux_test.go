//go:build linux && amd64 && wago_guardpage

package wagobench

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"runtime"
	"runtime/debug"
	"runtime/metrics"
	"runtime/pprof"
	"strconv"
	"sync"
	"syscall"
	"testing"
	"time"

	"github.com/wago-org/wago"
	core "github.com/wago-org/wago/src/core/runtime"
)

var reviewCallers = flag.Int("wago.review.callers", 1, "independent compiler callers")
var reviewWorkers = flag.Int("wago.review.workers", 1, "requested public compiler workers")
var reviewModule = flag.String("wago.review.module", "minimal-wasi", "memory review workload")
var reviewAPI = flag.String("wago.review.api", "raw", "raw or provider memory lifecycle")
var reviewCommands = flag.Int("wago.review.commands", 1000, "commands per epoch")
var reviewWarm = flag.Int("wago.review.warm", 1, "warm commands, zero or one")
var reviewEpochs = flag.Int("wago.review.epochs", 1, "completed epochs, at most four")
var reviewIntervention = flag.String("wago.review.intervention", "normal", "normal, gc, or scavenge")
var reviewProfile = flag.String("wago.review.profile", "", "separate diagnostic heap profile path")

// Fixed test storage avoids allocating observation records during the lifecycle.
var reviewPoints [12]reviewMemoryPoint
var reviewPointCount int
var reviewCompilerConfig workerDiagnosticConfig
var reviewMetrics = [...]metrics.Sample{
	{Name: "/gc/heap/live:bytes"}, {Name: "/gc/heap/goal:bytes"},
	{Name: "/gc/heap/objects:objects"}, {Name: "/memory/classes/heap/released:bytes"},
	{Name: "/memory/classes/metadata/other:bytes"}, {Name: "/memory/classes/metadata/mcache/inuse:bytes"}, {Name: "/memory/classes/metadata/mspan/inuse:bytes"}, {Name: "/memory/classes/heap/unused:bytes"}, {Name: "/memory/classes/heap/free:bytes"}, {Name: "/memory/classes/os-stacks:bytes"}, {Name: "/cpu/classes/gc/total:cpu-seconds"}, {Name: "/gc/scan/globals:bytes"}, {Name: "/gc/scan/heap:bytes"},
}

type reviewMemoryPoint struct {
	Phase                                                              string
	HeapAlloc, HeapObjects, HeapInuse, HeapSys, HeapIdle, HeapReleased uint64
	StackInuse, Sys, TotalAlloc, Mallocs, Frees, NextGC, PauseTotalNs  uint64
	NumGC                                                              uint32
	Goroutines                                                         int
	Metrics                                                            [len(reviewMetrics)]uint64
	MetricAvailable                                                    [len(reviewMetrics)]bool
	MetricFloat                                                        [len(reviewMetrics)]float64
	Native                                                             core.NativeMemoryStats
}

func init() {
	if os.Getenv("WAGO_MEMORY_REVIEW") == "1" {
		reviewCheckpoint("startup")
	}
}

func reviewCheckpoint(phase string) {
	if reviewPointCount >= len(reviewPoints) {
		panic("too many memory checkpoints")
	}
	var m runtime.MemStats
	runtime.ReadMemStats(&m)
	p := &reviewPoints[reviewPointCount]
	*p = reviewMemoryPoint{Phase: phase, HeapAlloc: m.HeapAlloc, HeapObjects: m.HeapObjects, HeapInuse: m.HeapInuse, HeapSys: m.HeapSys, HeapIdle: m.HeapIdle, HeapReleased: m.HeapReleased, StackInuse: m.StackInuse, Sys: m.Sys, TotalAlloc: m.TotalAlloc, Mallocs: m.Mallocs, Frees: m.Frees, NextGC: m.NextGC, NumGC: m.NumGC, PauseTotalNs: m.PauseTotalNs, Goroutines: runtime.NumGoroutine(), Native: core.ProcessNativeMemoryStats()}
	metrics.Read(reviewMetrics[:])
	for i := range reviewMetrics {
		if reviewMetrics[i].Value.Kind() == metrics.KindUint64 {
			p.MetricAvailable[i] = true
			p.Metrics[i] = reviewMetrics[i].Value.Uint64()
		} else if reviewMetrics[i].Value.Kind() == metrics.KindFloat64 {
			p.MetricAvailable[i] = true
			p.MetricFloat[i] = reviewMetrics[i].Value.Float64()
		}
	}
	index := byte(reviewPointCount)
	reviewPointCount++
	notify, err := strconv.Atoi(os.Getenv("WAGO_REVIEW_NOTIFY_FD"))
	if err != nil {
		panic(err)
	}
	ack, err := strconv.Atoi(os.Getenv("WAGO_REVIEW_ACK_FD"))
	if err != nil {
		panic(err)
	}
	if n, err := syscall.Write(notify, []byte{index}); err != nil || n != 1 {
		panic("memory checkpoint notification failed")
	}
	var reply [1]byte
	for {
		n, err := syscall.Read(ack, reply[:])
		if err == syscall.EINTR {
			continue
		}
		if err != nil || n != 1 || reply[0] != index {
			panic("memory checkpoint acknowledgement failed")
		}
		break
	}
}

func reviewWork(t *testing.T) string {
	if *reviewAPI == "compile" {
		return reviewCompileWork(t)
	}
	if *reviewModule == "startup" {
		reviewCheckpoint("setup")
		reviewCheckpoint("warm")
		for epoch := 0; epoch < *reviewEpochs; epoch++ {
			reviewCheckpoint([4]string{"epoch1", "epoch2", "epoch3", "epoch4"}[epoch])
		}
		return fmt.Sprintf("%x", sha256.Sum256(nil))
	}
	m := minimalWASICommand()
	if *reviewModule == "host1024" {
		m.ID = "host1024"
		m.bytes = wasiHostLoopModule()
	}
	if *reviewModule != m.ID {
		found := false
		for _, candidate := range commandCorpus(t) {
			if candidate.ID == *reviewModule {
				m = candidate
				found = true
				break
			}
		}
		if !found {
			t.Fatal("missing review command")
		}
	}
	if commandPreopen(m) != "" {
		t.Fatal("raw review commands must not own OS files")
	}
	hash := fmt.Sprintf("%x", sha256.Sum256(m.bytes))
	stdin := commandInput(t, m)
	ctx := context.Background()
	var rt *wago.Runtime
	var c *wago.Compiled
	var module *wago.Module
	var err error
	if *reviewAPI == "provider" {
		rt = wago.NewRuntime(wago.WithGuestArguments(commandArgs(m)))
		if err = rt.LoadPlugins(ctx, ownedWASISet(t)); err != nil {
			_ = rt.CloseContext(ctx)
			t.Fatal(err)
		}
		module, err = rt.Compile(m.bytes)
	} else {
		c, err = wago.Compile(nil, m.bytes)
	}
	if err != nil {
		if rt != nil {
			_ = rt.CloseContext(ctx)
		}
		t.Fatal(err)
	}
	defer func() {
		if c != nil {
			_ = c.Close()
		}
		if module != nil {
			_ = module.Close()
		}
		if rt != nil {
			_ = rt.CloseContext(ctx)
		}
	}()
	run := func() {
		if rt == nil && *reviewModule == "host1024" {
			imports, err := commandRuntimeImports(m, "", stdin, nil, nil)
			if err != nil {
				t.Fatal(err)
			}
			in, err := wago.Instantiate(c, wago.InstantiateOptions{Imports: imports})
			if err != nil {
				t.Fatal(err)
			}
			result, callErr := in.Invoke("run", 1024)
			closeErr := in.Close()
			if callErr != nil || closeErr != nil || len(result) != 1 || result[0] != 0 {
				t.Fatalf("host call=%v close=%v result=%v", callErr, closeErr, result)
			}
			return
		}
		if rt == nil {
			if _, err := runWagoCommand(m, c, stdin, false); err != nil {
				t.Fatal(err)
			}
			return
		}
		in, err := rt.Instantiate(ctx, module)
		if err != nil {
			t.Fatal(err)
		}
		var args []uint64
		if *reviewModule == "host1024" {
			args = []uint64{1024}
		}
		result, callErr := in.Invoke(m.Command.Export, args...)
		closeErr := in.Close()
		if !commandExitOK(callErr) || closeErr != nil {
			t.Fatalf("invoke=%v close=%v", callErr, closeErr)
		}
		if err := validateCommandOutput(m, commandOutput{results: result}); err != nil {
			t.Fatal(err)
		}
	}
	reviewCheckpoint("setup")
	for i := 0; i < *reviewWarm; i++ {
		run()
	}
	reviewCheckpoint("warm")
	for epoch := 0; epoch < *reviewEpochs; epoch++ {
		for i := 0; i < *reviewCommands; i++ {
			run()
		}
		reviewCheckpoint([4]string{"epoch1", "epoch2", "epoch3", "epoch4"}[epoch])
	}
	if c != nil {
		if err := c.Close(); err != nil {
			t.Fatal(err)
		}
		c = nil
	}
	if module != nil {
		if err := module.Close(); err != nil {
			t.Fatal(err)
		}
		module = nil
	}
	if rt != nil {
		if err := rt.CloseContext(ctx); err != nil {
			t.Fatal(err)
		}
		rt = nil
	}
	return hash
}

func TestWASIMemoryReview(t *testing.T) {
	if os.Getenv("WAGO_MEMORY_REVIEW") != "1" {
		t.Skip("requires the external memory runner")
	}
	if *reviewCommands < 0 || *reviewCommands > 10000 || *reviewWarm < 0 || *reviewWarm > 1 || *reviewEpochs < 1 || *reviewEpochs > 4 {
		t.Fatal("review work exceeds bounds")
	}
	if *reviewAPI != "raw" && *reviewAPI != "provider" && *reviewAPI != "compile" {
		t.Fatal("unknown API")
	}
	if *reviewIntervention != "normal" && *reviewIntervention != "gc" && *reviewIntervention != "scavenge" {
		t.Fatal("unknown intervention")
	}
	if *reviewCallers < 1 || *reviewCallers > 16 || *reviewWorkers < 0 || *reviewWorkers > 8 {
		t.Fatal("compiler work exceeds bounds")
	}
	reviewCheckpoint("ready")
	hash := reviewWork(t) // All compiled/runtime owners leave scope before the endpoint.
	reviewCheckpoint("released")
	switch *reviewIntervention {
	case "gc":
		runtime.GC()
	case "scavenge":
		debug.FreeOSMemory()
	}
	reviewCheckpoint("post")
	if *reviewProfile != "" {
		f, err := os.Create(*reviewProfile)
		if err != nil {
			t.Fatal(err)
		}
		err = pprof.WriteHeapProfile(f)
		closeErr := f.Close()
		if err != nil || closeErr != nil {
			t.Fatalf("profile=%v close=%v", err, closeErr)
		}
	}
	data, err := json.Marshal(struct {
		Module, API, Intervention, SHA256                              string
		Commands, Epochs, Warm                                         int
		Callers, Workers, PassedWorkers, ValidationLimit, BackendLimit int
		Points                                                         []reviewMemoryPoint
	}{*reviewModule, *reviewAPI, *reviewIntervention, hash, *reviewCommands, *reviewEpochs, *reviewWarm, *reviewCallers, *reviewWorkers, reviewCompilerConfig.passed, reviewCompilerConfig.validationLimit, reviewCompilerConfig.backendLimit, reviewPoints[:reviewPointCount]})
	if err != nil {
		t.Fatal(err)
	}
	fmt.Printf("MEMORY_REVIEW %s\n", data)
}

func reviewCompileWork(t *testing.T) string {
	var selected *corpusModule
	for _, m := range loadCorpus(t) {
		if m.ID == *reviewModule {
			v := m
			selected = &v
			break
		}
	}
	if selected == nil {
		t.Fatal("missing compiler module")
	}
	cfg := wago.NewRuntimeConfig().WithFunctionWorkers(*reviewWorkers)
	decoded := selected.decoded(t)
	body := 0
	for _, f := range decoded.Code {
		body += len(f.BodyBytes)
	}
	config := configureWorkerDiagnostic("public", *reviewWorkers, len(decoded.Code), body)
	reviewCompilerConfig = config
	reviewCheckpoint("setup")
	for i := 0; i < *reviewWarm; i++ {
		c, err := wago.Compile(cfg, selected.bytes)
		if err != nil {
			t.Fatal(err)
		}
		if err := c.Close(); err != nil {
			t.Fatal(err)
		}
	}
	reviewCheckpoint("warm")
	for epoch := 0; epoch < *reviewEpochs; epoch++ {
		var wg sync.WaitGroup
		var elapsed [16]time.Duration
		start := time.Now()
		wg.Add(*reviewCallers)
		for caller := 0; caller < *reviewCallers; caller++ {
			go func(caller int) {
				defer wg.Done()
				begin := time.Now()
				for i := 0; i < *reviewCommands; i++ {
					c, err := wago.Compile(cfg, selected.bytes)
					if err != nil {
						t.Error(err)
						return
					}
					if err := c.Close(); err != nil {
						t.Error(err)
						return
					}
				}
				elapsed[caller] = time.Since(begin)
			}(caller)
		}
		wg.Wait()
		total := time.Since(start)
		reviewCheckpoint([4]string{"epoch1", "epoch2", "epoch3", "epoch4"}[epoch])
		t.Logf("compiler api=public requested=%d passed=%d validation_limit=%d backend_limit=%d callers=%d gomaxprocs=%d calls=%d elapsed_ns=%d caller_elapsed_ns=%v", *reviewWorkers, config.passed, config.validationLimit, config.backendLimit, *reviewCallers, runtime.GOMAXPROCS(0), *reviewCommands**reviewCallers, total.Nanoseconds(), elapsed[:*reviewCallers])
	}
	return fmt.Sprintf("%x", sha256.Sum256(selected.bytes))
}
