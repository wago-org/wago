//go:build (linux || darwin) && arm64

package arm64

import (
	"testing"

	"github.com/wago-org/wago/src/core/compiler/backend/railshot/shared"
	"github.com/wago-org/wago/src/core/compiler/wasm"
	a64 "github.com/wago-org/wago/src/core/encoder/arm64"
)

// storeFwdModuleArm64 builds f(a i32, v i32) i32 that stores an owned computed
// value to mem[a] and immediately reloads it from the same local address+offset
// (mem[a] = v + 5; return mem[a]) — the exact-width store-forwarding shape.
func storeFwdModuleArm64(t *testing.T) *wasm.Module {
	return modMem(t, 1, []wasm.ValType{wasm.I32, wasm.I32}, []wasm.ValType{wasm.I32}, []byte{
		0x00,       // 0 locals
		0x20, 0x00, // local.get 0 (a)
		0x20, 0x01, // local.get 1 (v)
		0x41, 0x05, // i32.const 5
		0x6a,             // i32.add → v+5 (owned)
		0x36, 0x02, 0x00, // i32.store align=2 off=0
		0x20, 0x00, // local.get 0 (a)
		0x28, 0x02, 0x00, // i32.load align=2 off=0 (forwarded)
		0x0b, // end
	})
}

func TestStoreForwardExecArm64(t *testing.T) {
	m := storeFwdModuleArm64(t)
	for _, tc := range []struct{ a, v, want int32 }{
		{0, 42, 47}, {16, 100, 105}, {64, -5, 0},
	} {
		if got := runArm64(t, m, tc.a, tc.v); got != tc.want {
			t.Errorf("f(%d,%d) = %d, want %d", tc.a, tc.v, got, tc.want)
		}
	}
}

func TestStoreForwardFiresArm64(t *testing.T) {
	m := storeFwdModuleArm64(t)
	s := compileWithStats(t, m, false).Funcs[0]
	if got := s.Peephole["linear-store-load-fwd"]; got != 1 {
		t.Fatalf("linear-store-load-fwd = %d, want 1 (all: %v)", got, s.Peephole)
	}
}

func TestSizeStoreForwardCompactionClearsDeadByteLedgerArm64(t *testing.T) {
	a := &a64.Asm{}
	a.Store64(a64.X0, a64.SP, 0)
	a.Load64(a64.X0, a64.SP, 0)
	stats := &CodegenStats{}
	f := fn{a: a, sc: &scratch{}, stats: stats}
	if !f.forwardStoreLoadAt(a.B, len(a.B), 0, nil, true) {
		t.Fatal("same-register store/load did not forward")
	}
	if got := stats.NativeSize.StoreLoadNopBytes; got != 4 {
		t.Fatalf("pre-compaction store/load NOP bytes = %d, want 4", got)
	}
	offsets, err := shared.NewOffsetMap(len(a.B), []shared.DeletedRange{{Off: 4, Len: 4}})
	if err != nil {
		t.Fatal(err)
	}
	f.remapNativeSizeStats(&offsets, 0, 0)
	if got := stats.NativeSize.StoreLoadNopBytes; got != 0 {
		t.Fatalf("post-compaction store/load NOP bytes = %d, want 0", got)
	}
}

func TestStoreForwardKillSwitchEquivalentArm64(t *testing.T) {
	defer func(prev bool) { linearStoreForwardEnabled = prev }(linearStoreForwardEnabled)
	for _, tc := range []struct{ a, v int32 }{{0, 42}, {32, 7}, {8, -100}} {
		linearStoreForwardEnabled = true
		on := runArm64(t, storeFwdModuleArm64(t), tc.a, tc.v)
		linearStoreForwardEnabled = false
		off := runArm64(t, storeFwdModuleArm64(t), tc.a, tc.v)
		if on != off {
			t.Fatalf("f(%d,%d): on=%d off=%d", tc.a, tc.v, on, off)
		}
	}
}
