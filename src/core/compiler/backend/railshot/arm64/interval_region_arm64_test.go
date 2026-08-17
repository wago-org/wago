//go:build (linux || darwin) && arm64

package arm64

import (
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
)

func intervalRegionModuleArm64(t *testing.T) *wasm.Module {
	body := []byte{0x01, 0x20, 0x7f}            // thirty-two i32 locals
	body = append(body, 0x41, 0x00, 0x21, 0x00) // sum = 0
	for x := byte(1); x < 32; x++ {
		body = append(body,
			0x41, x, 0x21, x, // local[x] = x
			0x20, 0x00, 0x20, x, 0x6a, 0x21, 0x00, // sum += local[x]
		)
	}
	body = append(body, 0x20, 0x00, 0x0b)
	return mod1(t, nil, []wasm.ValType{wasm.I32}, body)
}

func TestIntervalRegionDynamicReuseArm64(t *testing.T) {
	saved := intervalRegionPinsEnabled
	defer func() { intervalRegionPinsEnabled = saved }()
	m := intervalRegionModuleArm64(t)

	intervalRegionPinsEnabled = true
	on := compileWithStats(t, m, false).Funcs[0]
	if got := runArm64(t, m); got != 496 {
		t.Fatalf("enabled result = %d, want 496", got)
	}
	if on.Peephole["interval-region"] != 1 {
		t.Fatalf("interval-region = %d, want 1 (all: %v)", on.Peephole["interval-region"], on.Peephole)
	}
	if on.Peephole["interval-region-reactivate"] == 0 {
		t.Fatalf("dynamic regional cache did not reuse a register: %v", on.Peephole)
	}

	intervalRegionPinsEnabled = false
	off := compileWithStats(t, m, false).Funcs[0]
	if on.CodeBytes >= off.CodeBytes {
		t.Fatalf("regional code = %d bytes, whole-function code = %d; want smaller", on.CodeBytes, off.CodeBytes)
	}
	if got := runArm64(t, m); got != 496 {
		t.Fatalf("disabled result = %d, want 496", got)
	}
}

func TestEntryInitializedLocalSkipsZeroArm64(t *testing.T) {
	body := []byte{
		0x01, 0x01, 0x7f, // one i32 local
		0x41, 0x07, 0x21, 0x00, // local[0] = 7
		0x20, 0x00, 0x0b, // return local[0]
	}
	m := mod1(t, nil, []wasm.ValType{wasm.I32}, body)
	var onStats ModuleStats
	if _, err := CompileModuleWith(m, CompileOptions{Stats: &onStats}); err != nil {
		t.Fatal(err)
	}
	on := onStats.Funcs[0]
	if got := on.Peephole["entry-init-elide"]; got != 1 {
		t.Fatalf("entry-init-elide = %d, want 1 (all: %v)", got, on.Peephole)
	}
	if got := runArm64(t, m); got != 7 {
		t.Fatalf("result = %d, want 7", got)
	}
	var offStats ModuleStats
	if _, err := CompileModuleWith(m, CompileOptions{Stats: &offStats, Optimizations: map[string]bool{"entry-init-elide": false}}); err != nil {
		t.Fatal(err)
	}
	off := offStats.Funcs[0]
	if got := off.Peephole["entry-init-elide"]; got != 0 {
		t.Fatalf("disabled entry-init-elide = %d, want 0", got)
	}
	got, err := runArm64WrapperWithOptions(t, m, CompileOptions{Optimizations: map[string]bool{"entry-init-elide": false}})
	if err != nil || got != 7 {
		t.Fatalf("disabled result = %d, err = %v; want 7", got, err)
	}
}

func TestStructuredDefiniteAssignmentExecArm64(t *testing.T) {
	body := []byte{
		0x01, 0x01, 0x7f, // one i32 local at index 1
		0x20, 0x00, 0x04, 0x40, // local.get 0; if
		0x41, 0x0b, 0x21, 0x01, //   local[1] = 11
		0x05, 0x41, 0x16, 0x21, 0x01, 0x0b, // else; local[1] = 22; end
		0x20, 0x01, 0x0b, // return local[1]
	}
	m := mod1(t, []wasm.ValType{wasm.I32}, []wasm.ValType{wasm.I32}, body)
	for _, tc := range []struct {
		arg, want uint64
	}{{0, 22}, {1, 11}} {
		if got := runArm64u(t, m, tc.arg); got != tc.want {
			t.Fatalf("arg %d: result = %d, want %d", tc.arg, got, tc.want)
		}
	}
}

func TestIntervalOverwriteBeforeReadArm64(t *testing.T) {
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
			f.intervalOwner[X19] = 3
			if got := f.intervalOverwriteBeforeRead().has(X19); got != tc.want {
				t.Fatalf("overwrite proof = %v, want %v", got, tc.want)
			}
		})
	}
}
