//go:build (linux || darwin || windows) && arm64

package arm64

import (
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
	"github.com/wago-org/wago/tests/support/wasmtest"
)

func intervalControlModule(t testing.TB) *wasm.Module {
	// More locals than whole-function pinning supports. Each local remains live
	// across a backedge, and one changes in each arm of a control-flow diamond.
	body := []byte{1, 80, 0x7f, 0x3f, 0, 0x1a, 0x02, 0x40, 0x20, 0, 0x45, 0x0d, 0, 0x03, 0x40}
	for x := byte(3); x <= 72; x++ {
		body = append(body, 0x20, x, 0x41)
		body = append(body, wasmtest.SLEB32(int32(x))...)
		body = append(body, 0x6a, 0x21, x)
	}
	body = append(body, 0x20, 1, 0x04, 0x40, 0x20, 3, 0x41, 7, 0x6a, 0x21, 3,
		0x05, 0x20, 4, 0x41, 11, 0x6a, 0x21, 4, 0x0b,
		0x20, 0, 0x41, 1, 0x6b, 0x22, 0, 0x0d, 0, 0x0b, 0x0b, 0x41, 0)
	for x := byte(3); x <= 72; x++ {
		body = append(body, 0x20, x, 0x6a)
	}
	body = append(body, 0x0b)
	m := mod1(t, []wasm.ValType{wasm.I32, wasm.I32}, []wasm.ValType{wasm.I32}, body)
	m.Memories = []wasm.MemType{{Limits: wasm.Limits{Min: 1}}}
	return m
}

func TestIntervalControlBackedgesAndDiamonds(t *testing.T) {
	saved := intervalControlsEnabled
	intervalControlsEnabled = true
	defer func() { intervalControlsEnabled = saved }()
	m := intervalControlModule(t)
	for _, enabled := range []bool{false, true} {
		opts := CompileOptions{Optimizations: map[string]bool{"interval-region-pins": enabled}}
		if diagnosticsEnabled {
			stats := &ModuleStats{}
			opts.Stats = stats
			cm, err := CompileModuleWith(m, opts)
			if err != nil {
				t.Fatal(err)
			}
			if cm.CodeImage != nil {
				cm.CodeImage.Close()
			}
			if (stats.Funcs[0].Peephole["interval-region"] != 0) != enabled {
				t.Fatalf("region admission enabled=%v stats=%v", enabled, stats.Funcs[0].Peephole)
			}
			opts.Stats = nil
		}
		for _, n := range []uint64{0, 1, 2, 7, 31} {
			for _, condition := range []uint64{0, 1, 0x80000000} {
				got, err := runArm64WrapperWithOptions(t, m, opts, n, condition)
				if err != nil {
					t.Fatal(err)
				}
				extra := uint64(11)
				if condition != 0 {
					extra = 7
				}
				want := n * (2625 + extra)
				if got != want {
					t.Fatalf("enabled=%v n=%d condition=%x: got %d want %d", enabled, n, condition, got, want)
				}
			}
		}
	}
}

func intervalDetachedSelectModule(t testing.TB) *wasm.Module {
	body := []byte{1, 64, 0x7f}
	for x := byte(3); x <= 20; x++ {
		body = append(body, 0x41)
		body = append(body, wasmtest.SLEB32(int32(x)*7+1)...)
		body = append(body, 0x21, x)
	}
	body = append(body, 0x03, 0x40)
	// Explicit bounds and four distinct loop constants reserve registers. Their
	// values have no significance; they expose pressure-driven lease eviction.
	for _, address := range []byte{0, 4} {
		body = append(body, 0x41, address, 0x28, 2, 0, 0x1a)
	}
	for _, value := range []int32{0x12345678, 0x23456712, 0x34561234, 0x45612345} {
		body = append(body, 0x20, 0, 0x41)
		body = append(body, wasmtest.SLEB32(value)...)
		body = append(body, 0x6c, 0x1a)
	}
	for x := byte(3); x <= 20; x++ {
		body = append(body, 0x20, x, 0x1a)
	}
	for _, x := range []byte{7, 12, 9, 8, 17, 19, 20, 4, 5, 18, 13, 6, 15} {
		// Keep several results live while computing another select. The true arm
		// can leave the stack as a borrowed regional local before the false arm
		// allocates its zero register, which must not evict that detached value.
		body = append(body, 0x20, x, 0x41, 0, 0x20, 0, 0x1b)
	}
	for range 12 {
		body = append(body, 0x73)
	}
	body = append(body, 0x21, 2, 0x20, 0, 0x41, 1, 0x6b, 0x22, 0, 0x0d, 0, 0x0b, 0x20, 2, 0x0b)
	m := mod1(t, []wasm.ValType{wasm.I32}, []wasm.ValType{wasm.I32}, body)
	m.Memories = []wasm.MemType{{Limits: wasm.Limits{Min: 1}}}
	return m
}

func TestIntervalDetachedSelectSurvivesEviction(t *testing.T) {
	saved := intervalControlsEnabled
	intervalControlsEnabled = true
	defer func() { intervalControlsEnabled = saved }()
	m := intervalDetachedSelectModule(t)
	for _, enabled := range []bool{false, true} {
		for _, n := range []uint64{1, 2, 7} {
			got, err := runArm64WrapperWithOptions(t, m, CompileOptions{Optimizations: map[string]bool{"interval-region-pins": enabled}}, n)
			if err != nil {
				t.Fatal(err)
			}
			if got != 54 {
				t.Fatalf("enabled=%v n=%d: got %d want 54", enabled, n, got)
			}
		}
	}
}

func intervalControlFusedExitModule(t testing.TB) *wasm.Module {
	body := []byte{1, 80, 0x7f, 0x3f, 0, 0x1a, 0x02, 0x40, 0x20, 0, 0x45, 0x0d, 0, 0x03, 0x40}
	for x := byte(3); x <= 72; x++ {
		body = append(body, 0x20, x, 0x41)
		body = append(body, wasmtest.SLEB32(int32(x))...)
		body = append(body, 0x6a, 0x21, x)
	}
	body = append(body, 0x20, 2, 0x1a, 0x20, 2, 0x1a, 0x20, 2, 0x1a, 0x20, 0, 0x41, 1, 0x6b, 0x22, 0, 0x41, 0, 0x46, 0x22, 2, 0x0d, 1, 0x0c, 0, 0x0b, 0x0b, 0x41, 0)
	for x := byte(3); x <= 72; x++ {
		body = append(body, 0x20, x, 0x6a)
	}
	body = append(body, 0x20, 2, 0x6a, 0x0b)
	m := mod1(t, []wasm.ValType{wasm.I32}, []wasm.ValType{wasm.I32}, body)
	m.Memories = []wasm.MemType{{Limits: wasm.Limits{Min: 1}}}
	return m
}

func TestIntervalControlFusedExitCanonicalizesLocals(t *testing.T) {
	saved := intervalControlsEnabled
	intervalControlsEnabled = true
	defer func() { intervalControlsEnabled = saved }()
	m := intervalControlFusedExitModule(t)
	for _, enabled := range []bool{false, true} {
		for _, n := range []uint64{0, 1, 2, 7} {
			got, err := runArm64WrapperWithOptions(t, m, CompileOptions{Optimizations: map[string]bool{"interval-region-pins": enabled}}, n)
			if err != nil {
				t.Fatal(err)
			}
			want := n * 2625
			if n != 0 {
				want++
			}
			if got != want {
				t.Fatalf("enabled=%v n=%d: got %d want %d", enabled, n, got, want)
			}
		}
	}
}

func intervalCountedLoopModule(t testing.TB) *wasm.Module {
	body := []byte{1, 63, 0x7f, 0x3f, 0, 0x1a, 0x02, 0x40, 0x03, 0x40, 0x20, 0, 0x45, 0x0d, 1}
	for x := byte(1); x <= 36; x++ {
		body = append(body, 0x20, x, 0x41, x, 0x6a, 0x21, x)
	}
	body = append(body, 0x20, 0, 0x41, 1, 0x6b, 0x21, 0, 0x0c, 0, 0x0b, 0x0b, 0x41, 0)
	for x := byte(1); x <= 36; x++ {
		body = append(body, 0x20, x, 0x6a)
	}
	body = append(body, 0x0b)
	m := mod1(t, []wasm.ValType{wasm.I32}, []wasm.ValType{wasm.I32}, body)
	m.Memories = []wasm.MemType{{Limits: wasm.Limits{Min: 1}}}
	return m
}

func TestIntervalCountedLoopCanonicalizesBackedge(t *testing.T) {
	saved := intervalControlsEnabled
	intervalControlsEnabled = true
	defer func() { intervalControlsEnabled = saved }()
	m := intervalCountedLoopModule(t)
	for _, enabled := range []bool{false, true} {
		for _, n := range []uint64{0, 1, 2, 7} {
			got, err := runArm64WrapperWithOptions(t, m, CompileOptions{Optimizations: map[string]bool{"interval-region-pins": enabled}}, n)
			if err != nil {
				t.Fatal(err)
			}
			if got != n*666 {
				t.Fatalf("enabled=%v n=%d: got %d want %d", enabled, n, got, n*666)
			}
		}
	}
}
