//go:build linux

package wagobench

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"runtime/pprof"
	"strconv"
	"strings"
	"syscall"
	"testing"

	wago "github.com/wago-org/wago"
	"github.com/wago-org/wago/src/core/compiler/wasm"
	core "github.com/wago-org/wago/src/core/runtime"
)

// sharingMemoryWorkers keeps memory diagnostics fixed-work while allowing the
// same corpus and lifecycle to qualify explicit and automatic worker policies.
func sharingMemoryWorkers(t *testing.T) int {
	t.Helper()
	value := os.Getenv("WAGO_SHARING_WORKERS")
	if value == "" {
		return 1
	}
	workers, err := strconv.Atoi(value)
	if err != nil || workers < 0 {
		t.Fatalf("invalid WAGO_SHARING_WORKERS %q", value)
	}
	return workers
}

func sharingSnapshot(t *testing.T, phase string, outputs []*wago.Module) {
	runtime.GC()
	var m runtime.MemStats
	runtime.ReadMemStats(&m)
	var u syscall.Rusage
	syscall.Getrusage(syscall.RUSAGE_SELF, &u)
	rss, _ := os.ReadFile("/proc/self/statm")
	code := 0
	for _, c := range outputs {
		code += c.Compiled().CodeSize()
	}
	rssFields := strings.Fields(string(rss))
	rssBytes := uint64(0)
	if len(rssFields) > 1 {
		pages, _ := strconv.ParseUint(rssFields[1], 10, 64)
		rssBytes = pages * uint64(os.Getpagesize())
	}
	data, _ := json.Marshal(map[string]any{"rss_bytes": rssBytes, "phase": phase, "modules": len(outputs), "code_bytes": code, "heap_alloc": m.HeapAlloc, "heap_inuse": m.HeapInuse, "heap_objects": m.HeapObjects, "heap_released": m.HeapReleased, "total_alloc": m.TotalAlloc, "mallocs": m.Mallocs, "statm": string(rss), "peak_rss_kib": u.Maxrss, "native": core.ProcessNativeMemoryStats()})
	t.Log(string(data))
	if dir := os.Getenv("WAGO_SHARING_PROFILE_DIR"); dir != "" {
		f, e := os.Create(filepath.Join(dir, phase+".heap"))
		if e != nil {
			t.Fatal(e)
		}
		if e = pprof.WriteHeapProfile(f); e != nil {
			t.Fatal(e)
		}
		f.Close()
	}
	runtime.KeepAlive(outputs)
}
func TestSharingMemory(t *testing.T) {
	if os.Getenv("WAGO_SHARING_MEMORY") != "1" {
		t.Skip("diagnostic")
	}
	fs := sharingFixtures()
	cfg := wago.NewRuntimeConfig().WithFunctionWorkers(sharingMemoryWorkers(t))
	rt := wago.NewRuntime(wago.WithRuntimeConfig(cfg))
	sharingSnapshot(t, "initial", nil)
	for i := 0; i < 30; i++ {
		for _, f := range fs {
			c, e := rt.Compile(f.bytes)
			if e != nil {
				t.Fatal(e)
			}
			if e = c.Close(); e != nil {
				t.Fatal(e)
			}
		}
	}
	sharingSnapshot(t, "compile_release_270", nil)
	for cycle := 0; cycle < 3; cycle++ {
		var out []*wago.Module
		for i := 0; i < 4; i++ {
			for _, f := range fs {
				c, e := rt.Compile(f.bytes)
				if e != nil {
					t.Fatal(e)
				}
				out = append(out, c)
			}
		}
		sharingSnapshot(t, fmt.Sprintf("retained_%d", cycle), out)
		for _, c := range out {
			if e := c.Close(); e != nil {
				t.Fatal(e)
			}
		}
		out = nil
		sharingSnapshot(t, fmt.Sprintf("released_%d", cycle), nil)
	}
	runtime.KeepAlive(rt)
	if e := rt.CloseContext(context.Background()); e != nil {
		t.Fatal(e)
	}
	rt = nil
	sharingSnapshot(t, "runtime_released", nil)
	runtime.KeepAlive(fs)
	runtime.KeepAlive(cfg)
}

// Native mapping retention is measured separately from public Runtime compile,
// which deliberately defers executable mappings until an instance needs code.
func TestSharingMappedMemory(t *testing.T) {
	if os.Getenv("WAGO_SHARING_MEMORY") != "1" {
		t.Skip("diagnostic")
	}
	fs := sharingFixtures()
	workers := sharingMemoryWorkers(t)
	mods := make([]*wasm.Module, len(fs))
	functions := 0
	for i, f := range fs {
		m, e := wasm.DecodeModule(f.bytes)
		if e != nil {
			t.Fatal(e)
		}
		if e = wasm.ValidateModule(m); e != nil {
			t.Fatal(e)
		}
		mods[i] = m
		functions += len(m.Code)
	}
	snapshot := func(phase string, out []*benchCompiledModule) {
		runtime.GC()
		var m runtime.MemStats
		runtime.ReadMemStats(&m)
		var u syscall.Rusage
		syscall.Getrusage(syscall.RUSAGE_SELF, &u)
		code, mapped, payloadRounded := sharingNativeStorage(t, out)
		rss, _ := os.ReadFile("/proc/self/statm")
		parts := strings.Fields(string(rss))
		pages := uint64(0)
		if len(parts) > 1 {
			pages, _ = strconv.ParseUint(parts[1], 10, 64)
		}
		d, _ := json.Marshal(map[string]any{"phase": phase, "modules": len(out), "functions_per_corpus": functions, "code_bytes": code, "mapped_bytes": mapped, "payload_bytes_page_rounded": payloadRounded, "heap_alloc": m.HeapAlloc, "heap_inuse": m.HeapInuse, "heap_objects": m.HeapObjects, "heap_released": m.HeapReleased, "total_alloc": m.TotalAlloc, "mallocs": m.Mallocs, "rss_bytes": pages * uint64(os.Getpagesize()), "peak_rss_kib": u.Maxrss})
		t.Log(string(d))
		runtime.KeepAlive(out)
	}
	snapshot("initial", nil)
	for i := 0; i < 30; i++ {
		for _, m := range mods {
			c, e := benchCompileModuleWorkers(m, workers)
			if e != nil {
				t.Fatal(e)
			}
			if e = c.Close(); e != nil {
				t.Fatal(e)
			}
		}
	}
	snapshot("compile_release_270", nil)
	for cycle := 0; cycle < 3; cycle++ {
		var out []*benchCompiledModule
		for i := 0; i < 4; i++ {
			for _, m := range mods {
				c, e := benchCompileModuleWorkers(m, workers)
				if e != nil {
					t.Fatal(e)
				}
				out = append(out, c)
			}
		}
		snapshot(fmt.Sprintf("retained_%d", cycle), out)
		for _, c := range out {
			if e := c.Close(); e != nil {
				t.Fatal(e)
			}
		}
		out = nil
		snapshot(fmt.Sprintf("released_%d", cycle), nil)
	}
	runtime.KeepAlive(mods)
	runtime.KeepAlive(fs)
}

// The owned mapping retains reserved capacity beyond the final payload. Neither
// mapping capacity nor rounded payload is a physical RSS measurement.
func sharingNativeStorage(t testing.TB, outputs []*benchCompiledModule) (code, mapped, payloadRounded int) {
	t.Helper()
	page := os.Getpagesize()
	for _, c := range outputs {
		owner, ok := c.image.(*core.CodeBuffer)
		if !ok {
			t.Fatal("native diagnostic output has no CodeBuffer owner")
		}
		code += len(c.Code)
		mapped += len(owner.Mapping())
		payloadRounded += (len(c.Code) + page - 1) / page * page
	}
	return
}

func TestSharingMappedStorageUsesOwnerCapacity(t *testing.T) {
	page := os.Getpagesize()
	owner, err := core.NewCodeBuffer(3 * page)
	if err != nil {
		t.Fatal(err)
	}
	defer owner.Close()
	if err = owner.AppendZeros(17); err != nil {
		t.Fatal(err)
	}
	c := &benchCompiledModule{Code: owner.Bytes(), image: owner}
	code, mapped, payload := sharingNativeStorage(t, []*benchCompiledModule{c})
	if code != 17 || mapped != 3*page || payload != page {
		t.Fatalf("code/mapped/payload=%d/%d/%d, want 17/%d/%d", code, mapped, payload, 3*page, page)
	}
}
