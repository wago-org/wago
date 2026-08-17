//go:build linux && amd64

package amd64

import (
	"bytes"
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
)

func intervalRegionModule(t *testing.T) *wasm.Module {
	body := []byte{0x01, 0x14, 0x7f} // twenty i32 locals
	for x := byte(0); x < 20; x++ {
		body = append(body, 0x41, x+1, 0x21, x) // local[x] = x+1
	}
	body = append(body, 0x20, 0x00)
	for x := byte(1); x < 20; x++ {
		body = append(body, 0x20, x, 0x6a)
	}
	body = append(body, 0x0b)
	return mod1(t, nil, []wasm.ValType{wasm.I32}, body)
}

func TestIntervalRegionDynamicReuse(t *testing.T) {
	saved := intervalRegionPinsEnabled
	defer func() { intervalRegionPinsEnabled = saved }()
	m := intervalRegionModule(t)

	intervalRegionPinsEnabled = true
	on := compileWithStats(t, m, false).Funcs[0]
	if got := runAmd64(t, m); got != 210 {
		t.Fatalf("enabled result = %d, want 210", got)
	}
	if on.Peephole["interval-region"] != 1 {
		t.Fatalf("interval-region = %d, want 1 (all: %v)", on.Peephole["interval-region"], on.Peephole)
	}
	if on.Peephole["interval-region-reactivate"] == 0 {
		t.Fatalf("dynamic regional cache did not reuse a register: %v", on.Peephole)
	}
	if on.Peephole["tree-order"] != 0 {
		t.Fatalf("tree ordering must stay disabled while regional registers are active: %v", on.Peephole)
	}
	one, err := CompileModuleWith(m, CompileOptions{Workers: 1})
	if err != nil {
		t.Fatal(err)
	}
	two, err := CompileModuleWith(m, CompileOptions{Workers: 2})
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(one.Code, two.Code) {
		t.Fatal("serial and parallel regional code differ")
	}

	intervalRegionPinsEnabled = false
	if got := runAmd64(t, m); got != 210 {
		t.Fatalf("disabled result = %d, want 210", got)
	}
}

func TestIntervalRegionLastGetStorageOnlyForCandidates(t *testing.T) {
	saved := intervalRegionPinsEnabled
	defer func() { intervalRegionPinsEnabled = saved }()
	intervalRegionPinsEnabled = true

	m := intervalRegionModule(t)
	m.FuncTypes = append(m.FuncTypes, m.FuncTypes[0])
	m.Code = append(m.Code, wasm.Func{
		Locals:    wasm.Locals{Runs: []wasm.LocalRun{{Count: 100, Type: wasm.I32}}},
		BodyBytes: []byte{0x0b},
	})
	hints, _, err := computeModuleHints(m, 0, 0, nil, false)
	if err != nil {
		t.Fatal(err)
	}
	if got := len(hints[0].localLastGet); got != 20 {
		t.Fatalf("candidate last-get storage = %d locals, want 20", got)
	}
	if hints[1].localLastGet != nil {
		t.Fatalf("ineligible function reserved %d last-get entries", len(hints[1].localLastGet))
	}
}

func TestIntervalOverwriteBeforeReadAMD64(t *testing.T) {
	for _, tc := range []struct {
		name string
		body []byte
		want bool
	}{
		{name: "overwrite", body: []byte{0x41, 0x00, 0x21, 0x03, 0x0b}, want: true},
		{name: "read", body: []byte{0x20, 0x03, 0x1a, 0x21, 0x03, 0x0b}},
		{name: "control-boundary", body: []byte{0x02, 0x40, 0x21, 0x03, 0x0b, 0x0b}},
		{name: "fuel-cap", body: append(append(make([]byte, maxIntervalNextUseOps), 0x21, 0x03), 0x0b)},
	} {
		t.Run(tc.name, func(t *testing.T) {
			for i := range tc.body {
				if tc.name == "fuel-cap" && i < maxIntervalNextUseOps {
					tc.body[i] = 0x01 // nop
				}
			}
			r := wasm.ReaderFrom(tc.body)
			f := fn{bodyReader: r}
			for i := range f.intervalOwner {
				f.intervalOwner[i] = -1
			}
			f.intervalOwner[R12] = 3
			if got := f.intervalOverwriteBeforeRead().has(R12); got != tc.want {
				t.Fatalf("overwrite proof = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestIntervalEvictionPrefersCleanLocalAMD64(t *testing.T) {
	f := intervalEvictionFn([]byte{0x0b}, []uint32{2, 3})
	f.locals[0].state = lsReg
	f.locals[1].state = lsStackReg
	if got := f.evictIntervalLocalBelow(0, 4); got != R13 {
		t.Fatalf("evicted register = %v, want clean R13", got)
	}
	if got := f.stats.Peephole["interval-region-clean-evict"]; got != 1 {
		t.Fatalf("clean eviction hits = %d, want 1", got)
	}
}

func TestIntervalEvictionPrefersFarthestNextUseAMD64(t *testing.T) {
	body := []byte{0x20, 0x00}
	for range 8 {
		body = append(body, 0x01)
	}
	body = append(body, 0x20, 0x01, 0x0b)
	f := intervalEvictionFn(body, []uint32{2, 3})
	if got := f.evictIntervalLocalBelow(0, 4); got != R13 {
		t.Fatalf("evicted register = %v, want farthest-use R13", got)
	}
	if got := f.stats.Peephole["interval-region-next-use-evict"]; got != 1 {
		t.Fatalf("next-use eviction hits = %d, want 1", got)
	}
}

func TestIntervalEvictionUnresolvedNextUseFallsBackAMD64(t *testing.T) {
	body := append([]byte{0x20, 0x00}, make([]byte, maxIntervalNextUseOps)...)
	for i := 2; i < len(body); i++ {
		body[i] = 0x01
	}
	f := intervalEvictionFn(body, []uint32{2, 3})
	if got := f.evictIntervalLocalBelow(0, 4); got != R12 {
		t.Fatalf("evicted register = %v, want score fallback R12", got)
	}
	if got := f.stats.Peephole["interval-region-next-use-evict"]; got != 0 {
		t.Fatalf("next-use eviction hits = %d, want 0", got)
	}
}

func intervalEvictionFn(body []byte, scores []uint32) *fn {
	stats := new(CodegenStats)
	f := &fn{
		s:             newStack(),
		bodyReader:    wasm.ReaderFrom(body),
		intervalReg:   []Reg{R12, R13},
		intervalScore: scores,
		locals: []localDef{
			{reg: R12, state: lsStackReg},
			{reg: R13, state: lsStackReg},
		},
		pinnedLocalMask: maskOf(R12, R13),
		stats:           stats,
	}
	for i := range f.intervalOwner {
		f.intervalOwner[i] = -1
	}
	f.intervalOwner[R12] = 0
	f.intervalOwner[R13] = 1
	return f
}
