//go:build (linux || darwin) && arm64

package arm64

import (
	"bytes"
	"encoding/binary"
	"github.com/wago-org/wago/src/core/compiler/wasm"
	enc "github.com/wago-org/wago/src/core/encoder/arm64"
	"github.com/wago-org/wago/tests/support/wasmtest"
	"testing"
)

func TestFrameLoadForwardWindow(t *testing.T) {
	for _, tc := range []struct {
		name string
		emit func(*enc.Asm)
		want bool
	}{
		{"identity", func(a *enc.Asm) {}, true},
		{"constant and linear store", func(a *enc.Asm) {
			a.MovImm64(X16, 0x1677c)
			a.Add32(X10, X20, X16)
			a.StoreIdx(linMemReg, X10, X9, 0, 1)
		}, true},
		{"linear base overwritten", func(a *enc.Asm) { a.MovReg64(linMemReg, X0) }, false},
		{"source overwritten", func(a *enc.Asm) { a.MovImm64(X9, 1) }, false},
		{"overlapping frame store", func(a *enc.Asm) { a.Store32(X10, SP, 4) }, false},
		{"unknown native store", func(a *enc.Asm) { a.Store64(X10, X19, 8) }, false},
		{"stack adjustment", func(a *enc.Asm) { a.SubImm64(SP, SP, 16) }, false},
		{"branch", func(a *enc.Asm) { a.Branch() }, false},
		{"unknown", func(a *enc.Asm) { a.Ret() }, false},
		{"window cap", func(a *enc.Asm) {
			for range 13 {
				a.Nop()
			}
		}, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := fn{a: &enc.Asm{}, policy: currentCodegenPolicy(), frameForwardEnd: 4, frameForwardOff: 8, frameForwardReg: X9}
			f.a.Store64(X9, SP, 8)
			tc.emit(f.a)
			if got := f.forwardFrameLoad(X10, 8); got != tc.want {
				t.Fatalf("forward=%v want=%v words=%x", got, tc.want, f.a.B)
			}
		})
	}
	for _, op := range []byte{0, 2, 3, 4, 5, 0x0b, 0x0c, 0x0d, 0x0e, 0x0f, 0x10, 0x11, 0x12, 0x14, 0x1f, 0xfc, 0xfd, 0xfe} {
		if frameForwardOpcode(op) {
			t.Fatalf("boundary %x admitted", op)
		}
	}
}

func TestFrameLoadForwardMemoryAndJoins(t *testing.T) {
	for _, join := range []bool{false, true} {
		body := []byte{1, 70, 0x7e, 0x41, 0, 0x20, 0, 0x42}
		body = append(body, wasmtest.SLEB64(0x1122334455667788)...)
		body = append(body, 0x85, 0x22, 70, 0x3c, 0, 0)
		if join {
			body = append(body, 0x20, 0, 0x50, 0x04, 0x40, 0x42, 9, 0x21, 70, 0x0b)
		}
		body = append(body, 0x41, 8, 0x20, 70, 0x42, 8, 0x88, 0x37, 3, 0, 0x41, 0, 0x0b)
		m := modMem(t, 1, []wasm.ValType{wasm.I64}, []wasm.ValType{wasm.I32}, body)
		for _, on := range []bool{false, true} {
			for _, arg := range []uint64{0, 1, 7, 0xffffffffffffffff, 0x8877665544332211} {
				var stats ModuleStats
				opts := CompileOptions{Optimizations: map[string]bool{"store-load-fwd": on}}
				if diagnosticsEnabled {
					opts.Stats = &stats
				}
				got, err := runAlignedStoreMemory(t, m, opts, arg)
				want := make([]byte, 65536)
				value := arg ^ 0x1122334455667788
				want[0] = byte(value)
				if join && arg == 0 {
					value = 9
				}
				binary.LittleEndian.PutUint64(want[8:], value>>8)
				if err != nil || !bytes.Equal(got, want) {
					t.Fatalf("join=%v on=%v arg=%x err=%v memoryEqual=%v", join, on, arg, err, bytes.Equal(got, want))
				}
				if diagnosticsEnabled && !join && (stats.Funcs[0].Peephole["frame-load-forward"] != 0) != on {
					t.Fatalf("on=%v selections=%v", on, stats.Funcs[0].Peephole)
				}
			}
		}
	}
}
