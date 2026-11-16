package main

import (
	"encoding/json"
	"fmt"
	w "github.com/wago-org/wago/src/wago"
	"os"
	"strconv"
	"time"
)

func main() {
	if len(os.Args) != 5 {
		panic("usage: gc-runner module.wasm export n expected")
	}
	data, e := os.ReadFile(os.Args[1])
	if e != nil {
		panic(e)
	}
	name := os.Args[2]
	n, err := strconv.ParseUint(os.Args[3], 10, 32)
	if err != nil {
		panic(err)
	}
	want, err := strconv.ParseUint(os.Args[4], 10, 32)
	if err != nil {
		panic(err)
	}
	batch := 32
	cfg := w.NewRuntimeConfig().WithCoreFeatures(w.CoreFeaturesV3)
	if os.Getenv("GC_PROOFS") == "off" {
		cfg = cfg.WithOptimization("dead-gc-new", false)
	}
	c, e := w.Compile(cfg, data)
	if e != nil {
		panic(e)
	}
	defer c.Close()
	opts := w.InstantiateOptions{}
	if os.Getenv("GC_MODE") == "throughput" {
		opts.GC = w.GCConfig{NurseryBytes: 1 << 20, ThroughputHeapBytes: 64 << 20}
	}
	i, e := w.Instantiate(c, opts)
	if e != nil {
		panic(e)
	}
	defer i.Close()
	check := func() {
		r, e := i.Invoke(name, n)
		if e != nil || len(r) != 1 || r[0] != want {
			panic(fmt.Sprintf("%v %v want %d", r, e, want))
		}
	}
	for j := 0; j < 100; j++ {
		check()
	}
	samples := []float64{}
	for s := 0; s < 9; s++ {
		t := time.Now()
		for j := 0; j < batch; j++ {
			check()
		}
		samples = append(samples, float64(time.Since(t).Nanoseconds())/float64(batch))
	}
	json.NewEncoder(os.Stdout).Encode(map[string]any{"samples_ns": samples, "mode": os.Getenv("GC_MODE"), "allocation_elision": os.Getenv("GC_PROOFS") != "off"})
}
