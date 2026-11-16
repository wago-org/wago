//go:build linux && amd64 && (wago_gc_compact_copy || wago_gc_compact_refstruct || wago_gc_compact_scalar_arrays || wago_gc_compact_array_alloc || wago_gc_compact_refstore || wago_gc_compact_shared_checks || wago_gc_compact_alloc || wago_gc_compact_reads || wago_gc_compact_helpers || wago_gc_helper_only || wago_gc_direct_reads || wago_gc_direct_alloc || wago_gc_direct_refstore || wago_gc_direct_trusted || wago_gc_direct_trusted_store || wago_gc_direct_scalar_arrays || wago_gc_direct_trusted_arrays || wago_gc_direct_trusted_loop || wago_gc_direct_pair_loop || wago_gc_direct_range_loop || wago_gc_direct_weighted_sum || wago_gc_direct_weighted_sse2 || wago_gc_direct_array_alloc || wago_gc_direct_trusted_refstruct || wago_gc_direct_trusted_alloc || wago_gc_direct_fill || wago_gc_direct_fill_sse2 || wago_gc_direct_copy || wago_gc_compact_refstruct)

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
	if mode != "direct" && mode != "compact" && mode != "handles" {
		t.Fatal("GC_BRIDGE_MODE must be direct, compact or handles")
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
	config, e := gcBridgeGeometry(mode, os.Getenv("GC_BRIDGE_EDEN"), os.Getenv("GC_BRIDGE_OLD"), os.Getenv("GC_BRIDGE_AGE2") == "1")
	if e != nil {
		t.Fatal(e)
	}
	eden, age2 := uint64(config.NurseryBytes), config.ExperimentalCompactAge2
	in, e := rt.Instantiate(context.Background(), mod, WithGC(config))
	if e != nil {
		t.Fatal(e)
	}
	defer in.Close()
	if in.gc == nil || in.gc.UsesDirectOffsets() != (mode == "direct" || mode == "compact") {
		t.Fatal("collector selection mismatch")
	}
	// Inspect actual collector geometry before guest execution, not just requested config.
	var geometry map[string]uint64
	if mode == "direct" && !age2 {
		v := in.gc.DirectView()
		if v == nil || v.NurseryEnd-v.NurseryStart != config.NurseryBytes || v.NurseryStart != 16 || v.OldStart != v.NurseryEnd || uint64(v.ArenaBytes) != 16+uint64(config.NurseryBytes)+2*uint64(config.ThroughputHeapBytes) {
			t.Fatal("actual direct collector geometry mismatch")
		}
		geometry = map[string]uint64{"arena_bytes": uint64(v.ArenaBytes), "nursery_bytes": uint64(v.NurseryEnd - v.NurseryStart), "each_old_bytes": (uint64(v.ArenaBytes) - uint64(v.NurseryEnd)) / 2, "start_entries": uint64(v.StartCount), "dirty_entries": uint64(v.DirtyCount), "derived_fixed_bytes": gcBridgeFixedBytes(config.NurseryBytes, config.ThroughputHeapBytes)}
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
	if perCall, ok := map[string]uint64{"struct-allocation-access": 262208, "array-allocation-access": 262208, "array-fill": 262208, "array-copy-checksum": 128, "array-loop-copy-checksum": 128, "tree-survival": 67551}[name]; ok {
		if got, want := after.Allocations-before.Allocations, uint64(samples*batch)*perCall; got != want {
			t.Fatalf("allocation accounting got %d want %d", got, want)
		}
	}

	runtime.ReadMemStats(&afterMem)
	if after.MinorCollections+after.FullCollections == 0 && os.Getenv("GC_BRIDGE_ALLOW_ZERO_COLLECTION") != "1" {
		t.Fatal("probe completed without any real collection; increase warmup")
	}
	var usage syscall.Rusage
	if e = syscall.Getrusage(syscall.RUSAGE_SELF, &usage); e != nil {
		t.Fatal(e)
	}
	out := map[string]any{"case": name, "mode": mode, "artifact_sha256": digest, "checksum": want, "warmup_calls": warmup, "samples": samples, "batch": batch, "samples_ns": times, "config": config, "before": before, "after": after, "zero_collection_diagnostic_allowed": os.Getenv("GC_BRIDGE_ALLOW_ZERO_COLLECTION") == "1", "timed_collections": after.MinorCollections + after.FullCollections - before.MinorCollections - before.FullCollections, "maxrss_kib": usage.Maxrss, "go_before": map[string]any{"HeapAlloc": beforeMem.HeapAlloc, "HeapInuse": beforeMem.HeapInuse, "HeapSys": beforeMem.HeapSys, "Sys": beforeMem.Sys}, "go_after": map[string]any{"HeapAlloc": afterMem.HeapAlloc, "HeapInuse": afterMem.HeapInuse, "HeapSys": afterMem.HeapSys, "Sys": afterMem.Sys, "TotalAllocDelta": afterMem.TotalAlloc - beforeMem.TotalAlloc, "MallocsDelta": afterMem.Mallocs - beforeMem.Mallocs}, "allocation_elision": false, "compiler_mode": os.Getenv("GC_BRIDGE_COMPILER_MODE"), "native_direct_view_installed": in.gcDirectView != nil, "runtime_owned": true, "go_version": runtime.Version(), "gomaxprocs": runtime.GOMAXPROCS(0)}
	out["actual_direct_geometry"] = geometry
	if mode == "direct" || mode == "compact" {
		out["direct_old_semispaces"] = 2
		out["total_young_budget_bytes"] = eden
		active := eden
		if age2 {
			active /= 2
		}
		out["active_nursery_bytes"] = active
		out["oversized_allocation_threshold_bytes"] = active
		if age2 {
			out["survivor_cap_bytes"] = active / 2
		} else {
			out["survivor_cap_bytes"] = 0
		}
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

// This probe deliberately keeps the old override to the qualified promote-all
// direct collector. Other collectors have different geometry/accounting rules.
func gcBridgeGeometry(mode, youngText, oldText string, age2 bool) (GCConfig, error) {
	cfg := GCConfig{ExperimentalCompactAge2: age2, ExperimentalDirectOffsets: mode == "direct", ExperimentalCompactOffsets: mode == "compact", NurseryBytes: 64 << 10, ThroughputHeapBytes: 8 << 20}
	if oldText != "" && (mode != "direct" || age2) {
		return cfg, fmt.Errorf("GC_BRIDGE_OLD requires promote-all direct mode")
	}
	for _, field := range []struct {
		name, text string
		dest       *uint32
	}{{"GC_BRIDGE_EDEN", youngText, &cfg.NurseryBytes}, {"GC_BRIDGE_OLD", oldText, &cfg.ThroughputHeapBytes}} {
		if field.text == "" {
			continue
		}
		v, err := strconv.ParseUint(field.text, 10, 32)
		if err != nil || v < 16 || v%16 != 0 {
			return cfg, fmt.Errorf("%s must be positive uint32 bytes aligned to 16", field.name)
		}
		*field.dest = uint32(v)
	}
	if uint64(16)+uint64(cfg.NurseryBytes)+2*uint64(cfg.ThroughputHeapBytes) > uint64(^uint32(0)) {
		return cfg, fmt.Errorf("direct arena exceeds uint32 offsets")
	}
	return cfg, nil
}

// Matches directgc.FixedBytes: excludes descriptors, slice/object headers and
// host runtime storage. This is reserved metadata, not measured process RSS.
func gcBridgeFixedBytes(young, old uint32) uint64 {
	r := uint64(16) + uint64(young) + 2*uint64(old)
	return r + 15 + 2*(r/8) + 8*((r/16+63)/64) + 4*(r/16) + 4*min(uint64(1024), uint64(old)/16)
}
