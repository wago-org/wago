//go:build arm64

package arm64

import (
	"encoding/binary"
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
	a64 "github.com/wago-org/wago/src/core/encoder/arm64"
)

func TestTailSelfLoopRetainsNativeFrame(t *testing.T) {
	i32 := []wasm.ValType{wasm.I32}
	m := modFuncs(t, funcDef{[]wasm.ValType{wasm.I32, wasm.I32}, i32, []byte{
		1, 1, 0x7f, // one declared i32 local must be reset on every tail invocation
		0x20, 0, 0x45, 0x04, 0x7f,
		0x20, 1, 0x20, 2, 0x6a,
		0x05, 0x41, 0xe3, 0, 0x21, 2,
		0x20, 0, 0x41, 1, 0x6b, 0x20, 1, 0x41, 17, 0x6a, 0x12, 0,
		0x0b, 0x0b,
	}})
	cm, err := CompileModule(m)
	if err != nil {
		t.Fatal(err)
	}
	defer cm.CodeImage.Close()
	entry := cm.InternalEntry[0]
	found := false
	for at := entry; at+4 <= len(cm.Code); at += 4 {
		word := binary.LittleEndian.Uint32(cm.Code[at:])
		if word&0xfc000000 != 0x14000000 {
			continue
		}
		target := at + int(int32(word<<6)>>6)*4
		if target >= entry && target < at {
			found = true
			if !profileEnabled && target == entry {
				t.Fatalf("self tail at %#x re-enters frame allocation at %#x", at, entry)
			}
			if profileEnabled && target != entry {
				t.Fatalf("profiled self-tail bypasses ordinary entry: %#x", target)
			}
		}
	}
	if !found {
		t.Fatal("missing self-tail backedge")
	}
}

func TestTailSelfLeafProofRejectsPhysicalCallsAndHelpers(t *testing.T) {
	f := fn{m: &wasm.Module{Globals: []wasm.Global{{Type: wasm.GlobalType{Type: wasm.ExternRef}}, {Type: wasm.GlobalType{Type: wasm.I32}}}}, ft: &wasm.CompType{Params: []wasm.ValType{wasm.I32}, Results: []wasm.ValType{wasm.I32}}, traceFuncIdx: 3, policy: currentCodegenPolicy()}
	for _, tc := range []struct {
		name string
		body []byte
		want bool
	}{
		{"self", []byte{0x20, 0, 0x12, 3, 0x0b}, true},
		{"other", []byte{0x20, 0, 0x12, 4, 0x0b}, false},
		{"ordinary-call", []byte{0x20, 0, 0x10, 3, 0x0b}, false},
		{"indirect-tail", []byte{0x20, 0, 0x41, 0, 0x13, 0, 0, 0x0b}, false},
		{"reference-tail", []byte{0x20, 0, 0x15, 0, 0x0b}, false},
		{"grow", []byte{0x41, 1, 0x40, 0, 0x1a, 0x20, 0, 0x12, 3, 0x0b}, false},
		{"bulk", []byte{0xfc, 11, 0, 0x20, 0, 0x12, 3, 0x0b}, false},
		{"GC", []byte{0xfb, 0, 0x20, 0, 0x12, 3, 0x0b}, false},
		{"SIMD", []byte{0xfd, 0, 0x20, 0, 0x12, 3, 0x0b}, false},
		{"atomic", []byte{0xfe, 0, 0x20, 0, 0x12, 3, 0x0b}, false},
		{"extension", []byte{0xff, 0, 0x20, 0, 0x12, 3, 0x0b}, false},
		{"truncated", []byte{0x20, 0, 0x12, 0x80}, false},
		{"reference-global", []byte{0x23, 0, 0x1a, 0x20, 0, 0x12, 3, 0x0b}, false},
		{"numeric-global", []byte{0x23, 1, 0x1a, 0x20, 0, 0x12, 3, 0x0b}, true},
		{"no-tail", []byte{0x20, 0, 0x0b}, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := f.onlyDirectSelfTailCalls(tc.body); got != (tc.want && !profileEnabled) {
				t.Fatalf("proof=%t want=%t", got, tc.want && !profileEnabled)
			}
		})
	}
}

func TestTailSelfEntryTestThreadsBothEdges(t *testing.T) {
	for _, wide := range []bool{false, true} {
		for _, nonzero := range []bool{false, true} {
			a := &a64.Asm{}
			a.Nop()
			a.Nop()
			entry := a.Len()
			var header int
			if wide {
				if nonzero {
					header = a.Cbnz64(X6)
				} else {
					header = a.Cbz64(X6)
				}
			} else {
				if nonzero {
					header = a.Cbnz32(X6)
				} else {
					header = a.Cbz32(X6)
				}
			}
			a.Nop()
			a.Nop()
			hot := a.Len()
			a.Nop()
			a.Nop()
			if !a.PatchBranch19(header, hot) {
				t.Fatal("header range")
			}
			f := fn{a: a, entryTrapEnd: entry}
			old := a.Len()
			if !f.threadSelfTailEntryTest() {
				t.Fatal("entry test was not threaded")
			}
			if a.Len() != old+8 {
				t.Fatal("unexpected edge size")
			}
			h, w := rdWord(a.B, header), rdWord(a.B, old)
			if h&0xff00001f != w&0xff00001f {
				t.Fatal("entry width, register or polarity changed")
			}
			target, ok := branchTarget(old, w)
			if !ok || target != hot {
				t.Fatalf("hot edge=%d,%t want=%d", target, ok, hot)
			}
			target, ok = branchTarget(old+4, rdWord(a.B, old+4))
			if !ok || target != entry+4 {
				t.Fatalf("cold edge=%d,%t want=%d", target, ok, entry+4)
			}
		}
	}
}

func TestTailSelfEntryTestRetainsInitializationAndRange(t *testing.T) {
	for _, mode := range []string{"initialization", "unpatched", "range"} {
		a := &a64.Asm{}
		a.Nop()
		a.Nop()
		entry := a.Len()
		header := a.Cbnz32(X0)
		a.Nop()
		a.Nop()
		hot := a.Len()
		a.Nop()
		a.Nop()
		switch mode {
		case "initialization":
			wrWord(a.B, entry, nopWord)
		case "range":
			a.PatchBranch19(header, hot)
			a.B = append(a.B, make([]byte, 1<<20)...)
		}
		f := fn{a: a, entryTrapEnd: entry}
		old := a.Len()
		if f.threadSelfTailEntryTest() || a.Len() != old {
			t.Fatalf("%s: invalid entry test admitted", mode)
		}
	}
}
