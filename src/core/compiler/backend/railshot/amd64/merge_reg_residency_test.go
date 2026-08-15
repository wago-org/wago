//go:build amd64

package amd64

import (
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
)

// mergeRegResidencyModule builds a call-making function whose pinned parameter
// stays dirty across an if/else and is consumed after the merge. A never-taken
// block contains a direct call so the function uses the call-aware local-state
// model without perturbing the exercised path.
func mergeRegResidencyModule(t testing.TB) *wasm.Module {
	t.Helper()
	return modFuncs(t,
		funcDef{params: []wasm.ValType{wasm.I32}, results: []wasm.ValType{wasm.I32}, body: []byte{
			0x00,
			0x20, 0x00, 0x41, 0x01, 0x6a, 0x21, 0x00, // x = x + 1 (dirty pin)
			0x20, 0x00, 0x41, 0x01, 0x71, // x & 1
			0x04, 0x40, // if
			0x20, 0x00, 0x41, 0x02, 0x6a, 0x21, 0x00, // x += 2
			0x05,                                     // else
			0x20, 0x00, 0x41, 0x03, 0x6a, 0x21, 0x00, // x += 3
			0x0b,
			0x20, 0x00, // result remains below the never-taken block
			0x41, 0x00, 0x04, 0x40, // if (0)
			0x20, 0x00, 0x10, 0x01, 0x1a, // call helper(x); drop
			0x0b,
			0x0b,
		}},
		funcDef{params: []wasm.ValType{wasm.I32}, results: []wasm.ValType{wasm.I32}, body: []byte{0x00, 0x20, 0x00, 0x0b}},
	)
}

func TestMergeRegisterResidency(t *testing.T) {
	m := mergeRegResidencyModule(t)
	var offStats ModuleStats
	off, err := CompileModuleWith(m, CompileOptions{
		Optimizations: map[string]bool{"merge-reg-residency": false, "inline": false},
		Stats:         &offStats,
	})
	if err != nil {
		t.Fatal(err)
	}
	var onStats ModuleStats
	on, err := CompileModuleWith(m, CompileOptions{
		Optimizations: map[string]bool{"merge-reg-residency": true, "inline": false},
		Stats:         &onStats,
	})
	if err != nil {
		t.Fatal(err)
	}
	if got := onStats.Funcs[0].Peephole["merge-reg-resident"]; got == 0 {
		t.Fatalf("merge-reg-resident hits = 0 (all: %v)", onStats.Funcs[0].Peephole)
	}
	if got, before := onStats.Funcs[0].LocalTraffic.ControlMergeStores, offStats.Funcs[0].LocalTraffic.ControlMergeStores; got >= before {
		t.Fatalf("merge stores enabled/disabled = %d/%d, want reduction", got, before)
	}
	if len(on.Code) >= len(off.Code) {
		t.Fatalf("code bytes enabled/disabled = %d/%d, want reduction", len(on.Code), len(off.Code))
	}
	for _, n := range []uint64{0, 1, 2, 17, ^uint64(0)} {
		want := uint64(uint32(n) + 3)
		if uint32(n)&1 != 0 {
			want = uint64(uint32(n) + 4)
		}
		if got := runCompiledAmd64u(t, off, n); got != want {
			t.Fatalf("disabled f(%d) = %d, want %d", n, got, want)
		}
		if got := runCompiledAmd64u(t, on, n); got != want {
			t.Fatalf("enabled f(%d) = %d, want %d", n, got, want)
		}
	}
}

func TestMergeRegisterResidencyForwardBlockFromSummary(t *testing.T) {
	m := modFuncs(t,
		funcDef{params: []wasm.ValType{wasm.I32}, results: []wasm.ValType{wasm.I32}, body: []byte{
			0x00,
			0x20, 0x00, 0x41, 0x01, 0x6a, 0x21, 0x00, // x++ (dirty pin)
			0x02, 0x40, // block
			0x20, 0x00, 0x41, 0x01, 0x71, 0x0d, 0x00, // br_if block (x & 1)
			0x20, 0x00, 0x41, 0x02, 0x6a, 0x21, 0x00, // x += 2
			0x0b,
			0x20, 0x00, // result remains below the never-taken call block
			0x41, 0x00, 0x04, 0x40,
			0x20, 0x00, 0x10, 0x01, 0x1a,
			0x0b, 0x0b,
		}},
		funcDef{params: []wasm.ValType{wasm.I32}, results: []wasm.ValType{wasm.I32}, body: []byte{0x00, 0x20, 0x00, 0x0b}},
	)
	var offStats, onStats ModuleStats
	off, err := CompileModuleWith(m, CompileOptions{Optimizations: map[string]bool{"merge-reg-residency": false, "inline": false}, Stats: &offStats})
	if err != nil {
		t.Fatal(err)
	}
	on, err := CompileModuleWith(m, CompileOptions{Optimizations: map[string]bool{"merge-reg-residency": true, "inline": false}, Stats: &onStats})
	if err != nil {
		t.Fatal(err)
	}
	if got := onStats.Funcs[0].Peephole["merge-reg-resident"]; got == 0 {
		t.Fatalf("forward block merge-reg-resident hits = 0 (all: %v)", onStats.Funcs[0].Peephole)
	}
	if got, before := onStats.Funcs[0].LocalTraffic.ControlMergeStores, offStats.Funcs[0].LocalTraffic.ControlMergeStores; got >= before {
		t.Fatalf("block merge stores enabled/disabled = %d/%d, want reduction", got, before)
	}
	for _, n := range []uint64{0, 1, 2, 17, ^uint64(0)} {
		want := uint64(uint32(n) + 1)
		if uint32(want)&1 == 0 {
			want = uint64(uint32(want) + 2)
		}
		if got := runCompiledAmd64u(t, off, n); got != want {
			t.Fatalf("disabled f(%d) = %d, want %d", n, got, want)
		}
		if got := runCompiledAmd64u(t, on, n); got != want {
			t.Fatalf("enabled f(%d) = %d, want %d", n, got, want)
		}
	}
}

func TestMergeRegisterResidencyNoElseSkipsFalseEdgeConvergence(t *testing.T) {
	// This is the control shape used by sqlite3_malloc: a call invalidates pinned
	// registers, the condition reloads a parameter, and the taken arm writes a
	// declared local. The taken arm must jump over any code emitted solely to
	// converge the condition-false edge at the end of an if without else.
	m := modFuncs(t,
		funcDef{params: []wasm.ValType{wasm.I32}, results: []wasm.ValType{wasm.I32}, body: []byte{
			0x01, 0x01, 0x7f, // one i32 local: result
			0x10, 0x01, // call helper
			0x20, 0x00, 0x41, 0x00, 0x4c, 0x72, 0x45, // !(helper() | (x <= 0))
			0x04, 0x40, // if (no else)
			0x20, 0x00, 0x41, 0x07, 0x6a, 0x21, 0x01, // result = x + 7
			0x0b,
			0x20, 0x01, 0x0b,
		}},
		funcDef{results: []wasm.ValType{wasm.I32}, body: []byte{0x00, 0x10, 0x02, 0x0b}},
		funcDef{results: []wasm.ValType{wasm.I32}, body: []byte{0x00, 0x41, 0x00, 0x0b}},
	)
	cm, err := CompileModuleWith(m, CompileOptions{Optimizations: map[string]bool{
		"merge-reg-residency": true,
		"inline":              false,
	}})
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		in, want uint64
	}{{0, 0}, {1, 8}, {17, 24}} {
		if got := runCompiledAmd64u(t, cm, tc.in); got != tc.want {
			t.Fatalf("f(%d) = %d, want %d", tc.in, got, tc.want)
		}
	}
}

func TestMergeRegisterResidencySizeObjectiveFallsBack(t *testing.T) {
	m := mergeRegResidencyModule(t)
	objective := OptimizeSize
	var stats ModuleStats
	if _, err := CompileModuleWith(m, CompileOptions{
		Objective:     &objective,
		Optimizations: map[string]bool{"merge-reg-residency": true, "inline": false},
		Stats:         &stats,
	}); err != nil {
		t.Fatal(err)
	}
	if got := stats.Funcs[0].Peephole["merge-reg-resident"]; got != 0 {
		t.Fatalf("size objective retained %d dirty merge(s)", got)
	}
}

func TestMergeRegionSummaryBoundedAndBarrierAware(t *testing.T) {
	body := make([]byte, 0, 3*(maxMergeRegionHints+2)+1)
	starts := make([]int, 0, maxMergeRegionHints+2)
	for range maxMergeRegionHints + 1 {
		body = append(body, 0x02, 0x40) // block
		starts = append(starts, len(body))
		body = append(body, 0x0b)
	}
	// A final block contains a direct call and must never be recorded even when
	// summary capacity is available.
	body = append(body, 0x02, 0x40, 0x10, 0x00, 0x0b, 0x0b)
	h, err := scanBodyBytes(body, 0, 0, 0)
	if err != nil {
		t.Fatal(err)
	}
	for _, start := range starts[:maxMergeRegionHints] {
		if !h.hasMergeRegion(start) {
			t.Fatalf("missing admitted merge region at body offset %d", start)
		}
	}
	if h.hasMergeRegion(starts[maxMergeRegionHints]) {
		t.Fatal("region beyond fixed summary capacity was admitted")
	}
}

func TestMergeRegionSummaryBarriers(t *testing.T) {
	tests := []struct {
		name string
		body []byte
		want bool
	}{
		{"straight", []byte{0x02, 0x40, 0x01, 0x0b, 0x0b}, true},
		{"direct-call", []byte{0x02, 0x40, 0x10, 0x00, 0x0b, 0x0b}, false},
		{"br-table", []byte{0x02, 0x40, 0x41, 0x00, 0x0e, 0x00, 0x00, 0x0b, 0x0b}, false},
		{"memory-grow", []byte{0x02, 0x40, 0x40, 0x00, 0x1a, 0x0b, 0x0b}, false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			h, err := scanBodyBytes(tc.body, 0, 0, 0)
			if err != nil {
				t.Fatal(err)
			}
			if got := h.hasMergeRegion(2); got != tc.want {
				t.Fatalf("merge region admitted = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestMergeRegisterResidencyCallRegionFallsBack(t *testing.T) {
	m := modFuncs(t,
		funcDef{params: []wasm.ValType{wasm.I32}, results: []wasm.ValType{wasm.I32}, body: []byte{
			0x00,
			0x20, 0x00, 0x41, 0x01, 0x6a, 0x21, 0x00, // dirty x
			0x20, 0x00, 0x04, 0x40, // if x
			0x20, 0x00, 0x10, 0x01, 0x1a, // call helper(x); drop
			0x20, 0x00, 0x41, 0x01, 0x6a, 0x21, 0x00, // dirty x after the call
			0x05, 0x01, 0x0b, // else nop; end
			0x20, 0x00, 0x0b,
		}},
		funcDef{params: []wasm.ValType{wasm.I32}, results: []wasm.ValType{wasm.I32}, body: []byte{0x00, 0x20, 0x00, 0x0b}},
	)
	var stats ModuleStats
	if _, err := CompileModuleWith(m, CompileOptions{
		Optimizations: map[string]bool{"merge-reg-residency": true, "inline": false},
		Stats:         &stats,
	}); err != nil {
		t.Fatal(err)
	}
	if got := stats.Funcs[0].Peephole["merge-reg-resident"]; got != 0 {
		t.Fatalf("call-containing region retained %d dirty merge(s)", got)
	}
	if got := stats.Funcs[0].LocalTraffic.ControlMergeStores; got == 0 {
		t.Fatal("call-containing region did not take canonical merge fallback")
	}
}

func TestMergeRegisterResidencyLoopTargetFixed(t *testing.T) {
	// sum(n) with a syntactically present, never-taken helper call after the loop.
	body := []byte{
		0x01, 0x01, 0x7f, // one i32 local: acc
		0x02, 0x40, 0x03, 0x40, // block; loop
		0x20, 0x00, 0x45, 0x0d, 0x01, // br_if block when n == 0
		0x20, 0x01, 0x20, 0x00, 0x6a, 0x21, 0x01, // acc += n
		0x20, 0x00, 0x41, 0x01, 0x6b, 0x21, 0x00, // n--
		0x0c, 0x00, 0x0b, 0x0b, // br loop; end; end
		0x20, 0x01, // result
		0x41, 0x00, 0x04, 0x40,
		0x20, 0x00, 0x10, 0x01, 0x1a,
		0x0b, 0x0b,
	}
	m := modFuncs(t,
		funcDef{params: []wasm.ValType{wasm.I32}, results: []wasm.ValType{wasm.I32}, body: body},
		funcDef{params: []wasm.ValType{wasm.I32}, results: []wasm.ValType{wasm.I32}, body: []byte{0x00, 0x20, 0x00, 0x0b}},
	)
	cm, err := CompileModuleWith(m, CompileOptions{Optimizations: map[string]bool{"merge-reg-residency": true, "inline": false}})
	if err != nil {
		t.Fatal(err)
	}
	for _, n := range []uint64{0, 1, 2, 10, 100} {
		want := n * (n + 1) / 2
		if got := runCompiledAmd64u(t, cm, n); got != want {
			t.Fatalf("sum(%d) = %d, want %d", n, got, want)
		}
	}
}
