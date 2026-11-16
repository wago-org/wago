//go:build linux && amd64 && (wago_gc_helper_only || wago_gc_direct_reads || wago_gc_direct_alloc)

package wago

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"
)

// TestGCDirectBridgeProbe is an opt-in evidence collector, not a performance
// acceptance test. Handle/direct helper comparisons use the SAME compiled
// binary/source. The separate native-read binary admits only direct offsets.
func TestGCDirectBridgeProbe(t *testing.T) {
	name := os.Getenv("GC_BRIDGE_CASE")
	if name == "" {
		t.Skip("set GC_BRIDGE_CASE to run isolated evidence probe")
	}
	mode := os.Getenv("GC_BRIDGE_MODE")
	if mode != "direct" && mode != "handles" {
		t.Fatal("GC_BRIDGE_MODE must be direct or handles")
	}
	readInt := func(key string, def int) int {
		if s := os.Getenv(key); s != "" {
			v, e := strconv.Atoi(s)
			if e != nil || v < 0 {
				t.Fatalf("invalid %s", key)
			}
			return v
		}
		return def
	}
	warmup, samples, batch := readInt("GC_BRIDGE_WARMUP", 12), readInt("GC_BRIDGE_SAMPLES", 0), readInt("GC_BRIDGE_BATCH", 4)
	if batch == 0 {
		t.Fatal("batch must be positive")
	}
	dir := "../../bench/experiments/gc-v8"
	manifest, e := os.ReadFile(filepath.Join(dir, "cases.json"))
	if e != nil {
		t.Fatal(e)
	}
	var cases []struct {
		Name, File, Export, SHA256 string
		N, Want                    uint64
	}
	if e = json.Unmarshal(manifest, &cases); e != nil {
		t.Fatal(e)
	}
	found := false
	var data []byte
	var export, digest string
	var n, want uint64
	for _, tc := range cases {
		if tc.Name == name {
			found = true
			data, e = os.ReadFile(filepath.Join(dir, tc.File))
			if e != nil {
				t.Fatal(e)
			}
			digest = fmt.Sprintf("%x", sha256.Sum256(data))
			if digest != tc.SHA256 {
				t.Fatal("artifact digest mismatch")
			}
			export, n, want = tc.Export, tc.N, tc.Want
			break
		}
	}
	if !found {
		t.Fatal("unknown case", name)
	}
	cfg := NewRuntimeConfig().WithCoreFeatures(CoreFeaturesV3).WithOptimization("dead-gc-new", false)
	rt := NewRuntime(WithRuntimeConfig(cfg))
	defer rt.Close()
	mod, e := rt.Compile(data)
	if e != nil {
		t.Fatal(e)
	}
	defer mod.Close()
	config := GCConfig{ExperimentalDirectOffsets: mode == "direct", NurseryBytes: 64 << 10, ThroughputHeapBytes: 8 << 20}
	in, e := rt.Instantiate(context.Background(), mod, WithGC(config))
	if e != nil {
		t.Fatal(e)
	}
	defer in.Close()
	if in.gc == nil || in.gc.UsesDirectOffsets() != (mode == "direct") {
		t.Fatal("collector selection mismatch")
	}
	call := func() {
		got, e := in.Invoke(export, n)
		if e != nil || len(got) != 1 || got[0] != want {
			t.Fatalf("checksum %v err %v want %d", got, e, want)
		}
	}
	for i := 0; i < warmup; i++ {
		call()
	}
	// Go host collection is outside measurement, and applied equally to both modes.
	runtime.GC()
	var beforeMem, afterMem runtime.MemStats
	runtime.ReadMemStats(&beforeMem)
	before := in.gc.Stats()
	times := make([]float64, 0, samples)
	for sample := 0; sample < samples; sample++ {
		start := time.Now()
		for i := 0; i < batch; i++ {
			call()
		}
		times = append(times, float64(time.Since(start).Nanoseconds())/float64(batch))
	}
	after := in.gc.Stats()
	runtime.ReadMemStats(&afterMem)
	if after.MinorCollections+after.FullCollections == 0 {
		t.Fatal("probe completed without any real collection; increase warmup")
	}
	var usage syscall.Rusage
	if e = syscall.Getrusage(syscall.RUSAGE_SELF, &usage); e != nil {
		t.Fatal(e)
	}
	out := map[string]any{"case": name, "mode": mode, "artifact_sha256": digest, "checksum": want, "warmup_calls": warmup, "samples": samples, "batch": batch, "samples_ns": times, "config": config, "before": before, "after": after, "timed_collections": after.MinorCollections + after.FullCollections - before.MinorCollections - before.FullCollections, "maxrss_kib": usage.Maxrss, "go_before": map[string]any{"HeapAlloc": beforeMem.HeapAlloc, "HeapInuse": beforeMem.HeapInuse, "HeapSys": beforeMem.HeapSys, "Sys": beforeMem.Sys}, "go_after": map[string]any{"HeapAlloc": afterMem.HeapAlloc, "HeapInuse": afterMem.HeapInuse, "HeapSys": afterMem.HeapSys, "Sys": afterMem.Sys, "TotalAllocDelta": afterMem.TotalAlloc - beforeMem.TotalAlloc, "MallocsDelta": afterMem.Mallocs - beforeMem.Mallocs}, "allocation_elision": false, "compiler_mode": os.Getenv("GC_BRIDGE_COMPILER_MODE"), "native_direct_view_installed": in.gcDirectView != nil, "runtime_owned": true, "go_version": runtime.Version(), "gomaxprocs": runtime.GOMAXPROCS(0)}
	if mode == "direct" {
		out["direct_old_semispaces"] = 2
		out["direct_arena_bytes"] = 16 + config.NurseryBytes + 2*config.ThroughputHeapBytes
		out["unimplemented_direct_stats"] = "copied/promoted bytes and scan counters are not instrumented; zeros are not measured zero work"
	}
	// /proc mm counters reset at exec, unlike ru_maxrss which may retain the
	// launching parent's pre-exec high-water floor on this system.
	if status, e := os.ReadFile("/proc/self/status"); e == nil {
		for _, line := range strings.Split(string(status), "\n") {
			fields := strings.Fields(line)
			if len(fields) >= 2 && (fields[0] == "VmHWM:" || fields[0] == "VmRSS:") {
				if value, e := strconv.ParseUint(fields[1], 10, 64); e == nil {
					out["proc_"+strings.TrimSuffix(fields[0], ":")+"_kib"] = value
				}
			}
		}
	}
	b, e := json.Marshal(out)
	if e != nil {
		t.Fatal(e)
	}
	t.Log("DIRECT_BRIDGE_RESULT " + string(b))
}
