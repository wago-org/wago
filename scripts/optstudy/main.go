//go:build darwin || linux

// optstudy measures one pinned wasm.fyi application in a fresh process.
package main

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strconv"
	"time"

	wago "github.com/wago-org/wago"
)

type contract struct {
	ID       string   `json:"id"`
	Artifact string   `json:"artifact"`
	SHA256   string   `json:"sha256"`
	ABI      string   `json:"abi"`
	Export   string   `json:"export"`
	Args     []uint64 `json:"args"`
	Reset    string   `json:"reset"`
	Oracle   struct {
		Kind     string   `json:"kind"`
		Expected []string `json:"expected"`
	} `json:"oracle"`
}

func main() {
	root := flag.String("root", "", "wasm.fyi corpora/applications directory")
	id := flag.String("id", "", "contract ID")
	option := flag.String("option", "", "optimization to override")
	enabled := flag.Bool("enabled", true, "override value")
	list := flag.Bool("list", false, "print architecture optimization catalog")
	repeats := flag.Int("repeat", 5, "checked warm execution repetitions")
	raw := flag.String("raw", "", "measure compilation only for a raw module (no execution oracle)")
	hold := flag.Duration("hold", 0, "hold a compiled raw module for external residency inspection")
	flag.Parse()
	if *raw != "" {
		measureRaw(*raw, *hold, *option, *enabled)
		return
	}
	if *list {
		must(json.NewEncoder(os.Stdout).Encode(wago.OptimizationInfosForArch(runtime.GOARCH)))
		return
	}
	if *repeats < 1 {
		panic("repeat must be positive")
	}
	manifest, err := os.ReadFile(filepath.Join(*root, "manifest.json"))
	must(err)
	var contracts []contract
	must(json.Unmarshal(manifest, &contracts))
	var selected *contract
	for i := range contracts {
		if contracts[i].ID == *id {
			selected = &contracts[i]
			break
		}
	}
	if selected == nil {
		panic("unknown contract: " + *id)
	}
	c := selected
	if c.ABI != "core" || c.Oracle.Kind != "exact_u64" || c.Reset != "stateless" {
		panic("unsupported contract shape")
	}
	data, err := os.ReadFile(filepath.Join(*root, c.Artifact))
	must(err)
	sum := sha256.Sum256(data)
	if hex.EncodeToString(sum[:]) != c.SHA256 {
		panic("artifact hash mismatch")
	}
	want := make([]uint64, len(c.Oracle.Expected))
	for i, s := range c.Oracle.Expected {
		want[i], err = strconv.ParseUint(s, 10, 64)
		must(err)
	}
	cfg := wago.NewRuntimeConfig().WithFunctionWorkers(1)
	if *option != "" {
		cfg = cfg.WithOptimization(*option, *enabled)
	}
	runtime.GC()
	var before, after runtime.MemStats
	runtime.ReadMemStats(&before)
	start := time.Now()
	cm, err := cfg.Compile(data)
	must(err)
	compileNS := time.Since(start).Nanoseconds()
	runtime.ReadMemStats(&after)
	compileRSS := peakRSS()
	defer cm.Close()
	codeHash := sha256.New()
	_, err = cm.WriteCodeTo(codeHash)
	must(err)
	in, err := wago.Instantiate(cm, wago.InstantiateOptions{})
	must(err)
	defer in.Close()
	fn, err := in.WasmFunc(c.Export)
	must(err)
	check := func() {
		got, e := fn.Invoke(c.Args...)
		must(e)
		if !slices.Equal(got, want) {
			panic(fmt.Sprintf("oracle: got %v want %v", got, want))
		}
	}
	check()
	timings := make([]int64, *repeats)
	for i := range timings {
		start = time.Now()
		check()
		timings[i] = time.Since(start).Nanoseconds()
	}
	must(json.NewEncoder(os.Stdout).Encode(map[string]any{
		"id": c.ID, "input_sha256": c.SHA256, "input_bytes": len(data),
		"option": *option, "enabled": *enabled, "go": runtime.Version(),
		"platform": runtime.GOOS + "/" + runtime.GOARCH, "gomaxprocs": runtime.GOMAXPROCS(0),
		"compile_ns": compileNS, "allocated_bytes": after.TotalAlloc - before.TotalAlloc,
		"allocations": after.Mallocs - before.Mallocs, "compile_peak_rss_bytes": compileRSS,
		"code_bytes": cm.CodeSize(), "code_sha256": hex.EncodeToString(codeHash.Sum(nil)),
		"exec_ns": timings, "oracle": "pass",
	}))
}

func must(err error) {
	if err != nil {
		panic(err)
	}
}

func measureRaw(path string, hold time.Duration, option string, enabled bool) {
	data, err := os.ReadFile(path)
	must(err)
	cfg := wago.NewRuntimeConfig().WithFunctionWorkers(1)
	if option != "" {
		cfg = cfg.WithOptimization(option, enabled)
	}
	runtime.GC()
	var before, after runtime.MemStats
	runtime.ReadMemStats(&before)
	startupRSS := peakRSS()
	start := time.Now()
	cm, err := cfg.Compile(data)
	must(err)
	elapsed := time.Since(start).Nanoseconds()
	runtime.ReadMemStats(&after)
	rss := peakRSS()
	defer cm.Close()
	if hold > 0 {
		_, err := os.Stdout.WriteString("READY\n")
		must(err)
		time.Sleep(hold)
	}
	h := sha256.New()
	_, err = cm.WriteCodeTo(h)
	must(err)
	must(json.NewEncoder(os.Stdout).Encode(map[string]any{
		"input": path, "input_sha256": fmt.Sprintf("%x", sha256.Sum256(data)),
		"option": option, "enabled": enabled,
		"compile_ns": elapsed, "allocated_bytes": after.TotalAlloc - before.TotalAlloc,
		"startup_peak_rss_bytes": startupRSS, "heap_after_compile_bytes": after.HeapAlloc,
		"heap_sys_bytes": after.HeapSys, "heap_idle_bytes": after.HeapIdle,
		"heap_released_bytes": after.HeapReleased, "stack_sys_bytes": stackSystemBytes(&after),
		"compile_peak_rss_bytes": rss, "code_bytes": cm.CodeSize(),
		"code_sha256": hex.EncodeToString(h.Sum(nil)), "oracle": "not-run-compile-only",
		"platform": runtime.GOOS + "/" + runtime.GOARCH, "go": runtime.Version(),
	}))
}
