//go:build amd64

package amd64

import (
	"testing"
	"unsafe"

	"github.com/wago-org/wago/src/core/compiler/wasm"
	"github.com/wago-org/wago/tests/support/wasmtest"
)

func gcCheckedRangeBody() []byte {
	return []byte{0,
		0x20, 0, 0xfb, 22, 0, 0xfb, 2, 0, 0, 0x1a,
		0x10, 1,
		0x20, 0, 0xfb, 22, 0,
		0x20, 0, 0xfb, 22, 0, 0xfb, 2, 0, 0,
		0x41, 1, 0x6a, 0xfb, 5, 0, 0,
		0x20, 0, 0xfb, 2, 0, 0, 0x0b}
}

func gcCheckedRangeStats(t *testing.T, body, composite, ft []byte) *CodegenStats {
	t.Helper()
	data := wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(composite, ft, wasmtest.FuncType(nil, nil))),
		wasmtest.Section(3, wasmtest.Vec([]byte{1}, []byte{2})),
		wasmtest.Section(10, wasmtest.Vec(append(wasmtest.ULEB(uint32(len(body))), body...), wasmtest.Code([]byte{0x0b}))),
	)
	m, err := wasm.DecodeModule(data)
	if err != nil {
		t.Fatal(err)
	}
	if err = wasm.ValidateModule(m); err != nil {
		t.Fatal(err)
	}
	var stats ModuleStats
	cm, err := CompileModuleWith(m, CompileOptions{GCStructHelpers: true, GCArrayHelpers: true, Stats: &stats, Optimizations: map[string]bool{"inline": false}})
	if err != nil {
		t.Fatal(err)
	}
	defer cm.CodeImage.Close()
	return stats.Funcs[0]
}

func TestGCCheckedRangeReloadAfterCall(t *testing.T) {
	requireCompilerDiagnostics(t)
	saved := gcCheckedRangeEnabled
	defer func() { gcCheckedRangeEnabled = saved }()
	for _, enabled := range []bool{false, true} {
		gcCheckedRangeEnabled = enabled
		stats := gcCheckedRangeStats(t, gcCheckedRangeBody(), []byte{0x5f, 1, 0x7f, 1}, []byte{0x60, 1, 0x64, 0, 1, 0x7f})
		want := 0
		if enabled {
			want = 1
		}
		if got := stats.Peephole["gc-checked-range-reload"]; got != want {
			t.Fatalf("enabled%v reloads%d want%d; %v", enabled, got, want, stats.Peephole)
		}
		if enabled && stats.Peephole["gc-resolve-reuse"] == 0 {
			t.Fatalf("setter did not reuse reloaded address: %v", stats.Peephole)
		}
	}
}

func TestGCCheckedRangeExclusions(t *testing.T) {
	requireCompilerDiagnostics(t)
	saved := gcCheckedRangeEnabled
	gcCheckedRangeEnabled = true
	defer func() { gcCheckedRangeEnabled = saved }()
	for _, tc := range []struct {
		name                  string
		composite, ft, prefix []byte
		wide                  bool
	}{
		{name: "generic-first"},
		{name: "control", prefix: []byte{0x02, 0x40, 0x0b}},
		{name: "assignment", prefix: []byte{0xfb, 1, 0, 0x21, 0}},
		{name: "nullable", ft: []byte{0x60, 1, 0x63, 0, 1, 0x7f}},
		{name: "open", composite: []byte{0x50, 0, 0x5f, 1, 0x7f, 1}},
		{name: "wider", composite: []byte{0x5f, 2, 0x7f, 1, 0x7f, 1}, wide: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			body := gcCheckedRangeBody()
			if tc.name == "generic-first" {
				body = append(append([]byte{}, body[:3]...), body[6:]...)
			}
			if tc.prefix != nil {
				body = append(append(append([]byte{}, body[:13]...), tc.prefix...), body[13:]...)
			}
			if tc.wide {
				body[26] = 1
				body[33] = 1
				body[39] = 1
			}
			composite, ft := tc.composite, tc.ft
			if composite == nil {
				composite = []byte{0x5f, 1, 0x7f, 1}
			}
			if ft == nil {
				ft = []byte{0x60, 1, 0x64, 0, 1, 0x7f}
			}
			stats := gcCheckedRangeStats(t, body, composite, ft)
			if got := stats.Peephole["gc-checked-range-reload"]; got != 0 {
				t.Fatalf("excluded reload%d: %v", got, stats.Peephole)
			}
		})
	}
}

func TestGCCheckedRangeFactStorage(t *testing.T) {
	if got := unsafe.Sizeof(gcReferenceTypeFact{}); got != 8 {
		t.Fatalf("fact size%d want8", got)
	}
	var facts gcLocalTypeFacts
	fact := gcReferenceTypeFact{typeIndex: 7, flags: gcReferenceFactCheckedRange, checkedBytes: 24}
	facts.set(0, fact)
	got, valid := facts.get(0)
	if got != fact || valid {
		t.Fatalf("range leaked into cast proof: %+v/%v", got, valid)
	}
	facts.remove(0)
	got, _ = facts.get(0)
	if got.checkedBytes != 0 {
		t.Fatal("assignment retained range")
	}
	facts.set(0, fact)
	facts.clear()
	got, _ = facts.get(0)
	if got.checkedBytes != 0 {
		t.Fatal("clear retained range")
	}
	for i := 0; i < maxGCLocalTypeFacts+1; i++ {
		facts.set(i, fact)
	}
	if facts.n != maxGCLocalTypeFacts {
		t.Fatalf("unbounded entries%d", facts.n)
	}
}
