package wagobench

import (
	"fmt"
	"os"
	"strconv"
	"testing"
	"time"

	"github.com/wago-org/wago/src/wago"
)

// binary-trees is the canonical allocation/GC-heavy benchmark. Three AS runtime
// variants are built from one source (bench/corpus/as/binary-trees.ts) via
// bench/corpus/build-as.sh:
//
//	incremental — TLSF alloc + ITCMS: write barrier on every ref store, GC
//	              mark-stepping interleaved on every __new (AS default).
//	minimal     — TLSF alloc + explicit STW mark-sweep at __collect(); NO write
//	              barrier, NO interleaved stepping.
//	stub        — bump alloc, no free, no GC, no barrier (__collect is a no-op);
//	              the no-GC *ceiling* (leaks, so footprint is unbounded).
//
// The delta incremental->stub is the total cost GC imposes on this workload; the
// delta incremental->minimal isolates the write barrier + interleaved stepping
// (the part a host-driven STW collector can strip). Run:
//
//	WAGO_BT_REPORT=1 WAGO_BT_DEPTH=14 go test ./bench -run TestBinaryTreesReport -v
//
// It skips when the modules are absent (they are committed, so normally present).

var btVariants = []string{"incremental", "minimal", "stub"}

func btModulePath(rt string) string { return "corpus/binary-trees-" + rt + ".wasm" }

func btCompile(tb testing.TB, rt string) *wago.Compiled {
	b, err := os.ReadFile(btModulePath(rt))
	if err != nil {
		tb.Skipf("binary-trees %s module absent (run bench/corpus/build-as.sh): %v", rt, err)
	}
	cfg := wago.NewRuntimeConfig().WithBoundsChecks(wago.BoundsChecksExplicit)
	c, err := wago.Compile(cfg, b)
	if err != nil {
		tb.Fatalf("compile %s: %v", rt, err)
	}
	return c
}

// btFreshRun instantiates a fresh instance, runs _initialize (untimed), then
// times a single run(depth). A fresh instance per sample keeps stub's leaked
// heap from accumulating across samples and gives every variant the same
// cold-start conditions. Returns the run latency, the checksum, and the current
// linear-memory footprint in bytes after the run.
func btFreshRun(tb testing.TB, c *wago.Compiled, depth int) (lat time.Duration, checksum uint32, footprint int) {
	in, err := wago.Instantiate(c, wago.InstantiateOptions{
		Imports: wago.Imports{"env.abort": wago.HostFunc(func(wago.HostModule, []uint64, []uint64) {})},
	})
	if err != nil {
		tb.Fatalf("instantiate: %v", err)
	}
	defer in.Close()
	if _, err := in.Invoke("_initialize"); err != nil {
		tb.Fatalf("_initialize: %v", err)
	}
	start := time.Now()
	res, err := in.Invoke("run", uint64(depth))
	lat = time.Since(start)
	if err != nil {
		tb.Fatalf("run(depth=%d): %v", depth, err)
	}
	checksum = uint32(res[0])
	if m := in.Memory(); m != nil {
		footprint = len(m.Bytes())
	}
	return lat, checksum, footprint
}

func TestBinaryTreesReport(t *testing.T) {
	if os.Getenv("WAGO_BT_REPORT") != "1" {
		t.Skip("set WAGO_BT_REPORT=1 to run the binary-trees GC report")
	}
	depth := 14
	if s := os.Getenv("WAGO_BT_DEPTH"); s != "" {
		if d, err := strconv.Atoi(s); err == nil {
			depth = d
		}
	}
	samples := 15
	if s := os.Getenv("WAGO_BT_SAMPLES"); s != "" {
		if n, err := strconv.Atoi(s); err == nil {
			samples = n
		}
	}

	type row struct {
		rt        string
		best      time.Duration
		checksum  uint32
		footprint int
	}
	rows := make([]row, 0, len(btVariants))
	for _, rt := range btVariants {
		c := btCompile(t, rt)
		// warm up
		btFreshRun(t, c, depth)
		best := time.Duration(1<<62 - 1)
		var checksum uint32
		var footprint int
		for i := 0; i < samples; i++ {
			lat, cs, fp := btFreshRun(t, c, depth)
			if lat < best {
				best = lat
			}
			checksum, footprint = cs, fp
		}
		rows = append(rows, row{rt, best, checksum, footprint})
	}

	fmt.Printf("\nbinary-trees  depth=%d  samples=%d\n", depth, samples)
	fmt.Printf("%-13s %12s %14s %10s %12s\n", "runtime", "best", "footprint", "vs stub", "checksum")
	var stubBest time.Duration
	for _, r := range rows {
		if r.rt == "stub" {
			stubBest = r.best
		}
	}
	for _, r := range rows {
		ratio := ""
		if stubBest > 0 {
			ratio = fmt.Sprintf("%.2fx", float64(r.best)/float64(stubBest))
		}
		fmt.Printf("%-13s %12s %11.1f MiB %10s %12d\n",
			r.rt, r.best.Round(time.Microsecond), float64(r.footprint)/(1024*1024), ratio, r.checksum)
	}
	fmt.Println()

	// Correctness: every variant must compute the same checksum. stub never
	// collects, so its checksum is the ground truth.
	var truth uint32
	for _, r := range rows {
		if r.rt == "stub" {
			truth = r.checksum
		}
	}
	for _, r := range rows {
		if r.checksum != truth {
			t.Errorf("checksum mismatch: %s=%d vs stub=%d (GC freed a live object)", r.rt, r.checksum, truth)
		}
	}
}
