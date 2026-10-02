//go:build amd64 && wago_regalloccheck

package amd64

import (
	"bytes"
	"testing"

	"github.com/wago-org/wago/internal/regalloccheck"
	"github.com/wago-org/wago/src/core/compiler/wasm"
	encoder "github.com/wago-org/wago/src/core/encoder/amd64"
)

func TestRegallocCheckIndirectCallClobber(t *testing.T) {
	newFn := func() *fn {
		return &fn{a: &encoder.Asm{}, s: newStack(), globalCellReg: regNone, memSizeReg: regNone}
	}
	emit := func(f *fn) (uint32, uint32) {
		return f.emitIndirectCallHomeAware(&wasm.CompType{}, R10, R11)
	}
	var call encoder.Asm
	call.CallMem(RBX, -int32(offSpillRegion))
	f := newFn()
	sameReturn, crossReturn := emit(f)
	for _, tc := range []struct {
		name string
		end  uint32
	}{{"same-instance-without-cache", sameReturn}, {"cross-instance-without-cache", crossReturn}} {
		t.Run(tc.name, func(t *testing.T) {
			end := int(tc.end)
			if end < len(call.B) || end > len(f.a.B) || !bytes.Equal(f.a.B[end-len(call.B):end], call.B) {
				t.Fatalf("missing physical indirect call ending at %d", end)
			}
		})
	}
	for _, cross := range []bool{false, true} {
		name, callEnd := "same-instance-preloaded-float", sameReturn
		if cross {
			name, callEnd = "cross-instance-preloaded-float", crossReturn
		}
		t.Run(name, func(t *testing.T) {
			f := newFn()
			preloadBytes := 0
			preload := func() {
				before := len(f.a.B)
				if _, ok := f.preloadFloatConst(storage{kind: stConst, typ: mtF64, cval: 0x3ff0000000000000}); !ok {
					t.Fatal("immutable float cache was not preloaded")
				}
				preloadBytes += len(f.a.B) - before
			}
			injected := !cross
			if cross {
				// Inject the bad cache admission while emitting the cross-instance
				// arm, after the cache-free same-instance call. This isolates the
				// second hook instead of stopping at the first one's diagnostic.
				previous := f.a.ObserveRegalloc(func(effect regalloccheck.Effect) {
					if !injected && effect.Kind == regalloccheck.Copy &&
						effect.Dst == checkReg(RSI, false) && effect.Src == checkReg(R11, false) {
						injected = true
						preload()
					}
				})
				defer f.a.ObserveRegalloc(previous)
			} else {
				preload()
			}
			requireAllocationFailure(t, "immutable cache across physical call", func() { emit(f) })
			if !injected {
				t.Fatal("cross-instance cache admission was not reached")
			}
			if want := int(callEnd) - len(call.B) + preloadBytes; len(f.a.B) != want {
				t.Fatalf("diagnostic at byte %d, want immediately before physical call at %d", len(f.a.B), want)
			}
		})
	}
}
