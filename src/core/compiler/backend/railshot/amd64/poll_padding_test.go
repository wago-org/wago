//go:build amd64 && wago_experimental_poll_padding

package amd64

import (
	"bytes"
	x64 "github.com/wago-org/wago/src/core/encoder/amd64"
	"reflect"
	"testing"
)

func TestInterruptPollPaddingSelector(t *testing.T) {
	for value, want := range map[string]int{"": 0, "0": 0, "32": 32} {
		if got := parseInterruptPollPadding(value); got != want {
			t.Fatalf("%q: %d", value, got)
		}
	}
	for _, value := range []string{"1", "64", "-32", " 32"} {
		func() {
			defer func() {
				if recover() == nil {
					t.Errorf("accepted %q", value)
				}
			}()
			parseInterruptPollPadding(value)
		}()
	}
}
func TestInterruptPollPaddingPreservesPoll(t *testing.T) {
	for _, scratch := range []Reg{RAX, RSI, R8, R15} {
		for offset := 0; offset < 32; offset++ {
			var a x64.Asm
			a.EmitBytes(bytes.Repeat([]byte{0x90}, offset))
			f := fn{a: &a, sc: newScratch(), interruptible: true, traceFuncIdx: 7, wasmPC: 123}
			// Construct the original poll independently, so environment-selected mode
			// does not affect the reference and no mutable test global is needed.
			a.Load64(scratch, RBX, -offTrapCellPtr)
			a.Load32(scratch, scratch, 0)
			a.TestSelf(scratch, false)
			f.trapIf(condNE, trapInterrupted)
			original := append([]byte(nil), a.B...)
			sites := append([]trapSite(nil), f.sc.trapSites[trapInterrupted]...)
			f.padInterruptPollTo(offset, 0)
			if !bytes.Equal(original, a.B) {
				t.Fatal("disabled mode changed bytes")
			}
			f.padInterruptPollTo(offset, 32)
			if (a.Len()-offset)%32 != 0 || !bytes.Equal(original, a.B[:len(original)]) {
				t.Fatal("poll prefix/relative length changed")
			}
			for _, b := range a.B[len(original):] {
				if b != 0x90 {
					t.Fatal("padding can alter flags/registers")
				}
			}
			if !reflect.DeepEqual(sites, f.sc.trapSites[trapInterrupted]) || len(sites) != 1 || sites[0].function != 7 || sites[0].pc != 123 {
				t.Fatal("trap attribution changed")
			}
			size := a.Len()
			f.padInterruptPollTo(offset, 32)
			if a.Len() != size {
				t.Fatal("already padded poll grew")
			}
		}
	}
}
