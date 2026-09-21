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
	"syscall"
	"testing"

	"github.com/wago-org/wago"
	core "github.com/wago-org/wago/src/core/runtime"
)

var reviewModule = flag.String("wago.review.module", "minimal-wasi", "memory review workload")
var reviewAPI = flag.String("wago.review.api", "raw", "raw or provider memory lifecycle")
var reviewCommands = flag.Int("wago.review.commands", 1000, "commands per epoch")
var reviewEpochs = flag.Int("wago.review.epochs", 1, "completed epochs, at most four")
var reviewIntervention = flag.String("wago.review.intervention", "normal", "normal, gc, or scavenge")
var reviewProfile = flag.String("wago.review.profile", "", "separate diagnostic heap profile path")

// Fixed test storage avoids allocating observation records during the lifecycle.
var reviewPoints [12]reviewMemoryPoint
var reviewPointCount int
var reviewMetrics = [4]metrics.Sample{
	{Name: "/gc/heap/live:bytes"}, {Name: "/gc/heap/goal:bytes"},
	{Name: "/gc/heap/objects:objects"}, {Name: "/memory/classes/heap/released:bytes"},
}

type reviewMemoryPoint struct {
	Phase                                                              string
	HeapAlloc, HeapObjects, HeapInuse, HeapSys, HeapIdle, HeapReleased uint64
	StackInuse, Sys, TotalAlloc, Mallocs, Frees, NextGC, PauseTotalNs  uint64
	NumGC                                                              uint32
	Goroutines                                                         int
	Metrics                                                            [4]uint64
	MetricAvailable                                                    [4]bool
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
	m := minimalWASICommand()
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
		result, callErr := in.Invoke(m.Command.Export)
		closeErr := in.Close()
		if !commandExitOK(callErr) || closeErr != nil {
			t.Fatalf("invoke=%v close=%v", callErr, closeErr)
		}
		if err := validateCommandOutput(m, commandOutput{results: result}); err != nil {
			t.Fatal(err)
		}
	}
	reviewCheckpoint("setup")
	run()
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
	if *reviewCommands < 1 || *reviewCommands > 1000 || *reviewEpochs < 1 || *reviewEpochs > 4 {
		t.Fatal("review work exceeds bounds")
	}
	if *reviewAPI != "raw" && *reviewAPI != "provider" {
		t.Fatal("unknown API")
	}
	if *reviewIntervention != "normal" && *reviewIntervention != "gc" && *reviewIntervention != "scavenge" {
		t.Fatal("unknown intervention")
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
		Module, API, Intervention, SHA256 string
		Commands, Epochs                  int
		Points                            []reviewMemoryPoint
	}{*reviewModule, *reviewAPI, *reviewIntervention, hash, *reviewCommands, *reviewEpochs, reviewPoints[:reviewPointCount]})
	if err != nil {
		t.Fatal(err)
	}
	fmt.Printf("MEMORY_REVIEW %s\n", data)
}
