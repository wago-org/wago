//go:build arm64

package arm64

import (
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
)

func mergeRegResidencyModuleARM64(t testing.TB) *wasm.Module {
	t.Helper()
	return modFuncs(t,
		funcDef{params: []wasm.ValType{wasm.I32}, results: []wasm.ValType{wasm.I32}, body: []byte{
			0x00,
			0x20, 0x00, 0x41, 0x01, 0x6a, 0x21, 0x00,
			0x20, 0x00, 0x41, 0x01, 0x71,
			0x04, 0x40,
			0x20, 0x00, 0x41, 0x02, 0x6a, 0x21, 0x00,
			0x05,
			0x20, 0x00, 0x41, 0x03, 0x6a, 0x21, 0x00,
			0x0b,
			0x20, 0x00,
			0x41, 0x00, 0x04, 0x40,
			0x20, 0x00, 0x10, 0x01, 0x1a,
			0x0b, 0x0b,
		}},
		funcDef{params: []wasm.ValType{wasm.I32}, results: []wasm.ValType{wasm.I32}, body: []byte{0x00, 0x20, 0x00, 0x0b}},
	)
}

func TestMergeRegisterResidencyARM64(t *testing.T) {
	m := mergeRegResidencyModuleARM64(t)
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
		for name, opts := range map[string]CompileOptions{
			"disabled": {Optimizations: map[string]bool{"merge-reg-residency": false, "inline": false}},
			"enabled":  {Optimizations: map[string]bool{"merge-reg-residency": true, "inline": false}},
		} {
			got, err := runArm64WrapperWithOptions(t, m, opts, n)
			if err != nil || got != want {
				t.Fatalf("%s f(%d) = %d, %v; want %d", name, n, got, err, want)
			}
		}
	}
}

func TestMergeRegionSummaryBarriersARM64(t *testing.T) {
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

func TestMergeRegionSummaryBoundedARM64(t *testing.T) {
	body := make([]byte, 0, 3*(maxMergeRegionHints+2)+1)
	starts := make([]int, 0, maxMergeRegionHints+1)
	for range maxMergeRegionHints + 1 {
		body = append(body, 0x02, 0x40)
		starts = append(starts, len(body))
		body = append(body, 0x0b)
	}
	body = append(body, 0x0b)
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

func TestMergeRegisterResidencyNoElseSkipsFalseEdgeConvergenceARM64(t *testing.T) {
	m := modFuncs(t,
		funcDef{params: []wasm.ValType{wasm.I32}, results: []wasm.ValType{wasm.I32}, body: []byte{
			0x01, 0x01, 0x7f,
			0x10, 0x01,
			0x20, 0x00, 0x41, 0x00, 0x4c, 0x72, 0x45,
			0x04, 0x40,
			0x20, 0x00, 0x41, 0x07, 0x6a, 0x21, 0x01,
			0x0b,
			0x20, 0x01, 0x0b,
		}},
		funcDef{results: []wasm.ValType{wasm.I32}, body: []byte{0x00, 0x10, 0x02, 0x0b}},
		funcDef{results: []wasm.ValType{wasm.I32}, body: []byte{0x00, 0x41, 0x00, 0x0b}},
	)
	opts := CompileOptions{Optimizations: map[string]bool{"merge-reg-residency": true, "inline": false}}
	for _, tc := range []struct {
		in, want uint64
	}{{0, 0}, {1, 8}, {17, 24}} {
		got, err := runArm64WrapperWithOptions(t, m, opts, tc.in)
		if err != nil || got != tc.want {
			t.Fatalf("f(%d) = %d, %v; want %d", tc.in, got, err, tc.want)
		}
	}
}

func TestMergeRegisterResidencySizeObjectiveFallsBackARM64(t *testing.T) {
	m := mergeRegResidencyModuleARM64(t)
	objective := OptimizeSize
	var stats ModuleStats
	if _, err := CompileModuleWith(m, CompileOptions{Objective: &objective, Optimizations: map[string]bool{"merge-reg-residency": true, "inline": false}, Stats: &stats}); err != nil {
		t.Fatal(err)
	}
	if got := stats.Funcs[0].Peephole["merge-reg-resident"]; got != 0 {
		t.Fatalf("size objective retained %d dirty merge(s)", got)
	}
}
