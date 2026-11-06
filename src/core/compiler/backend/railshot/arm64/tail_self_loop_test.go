//go:build arm64

package arm64

import (
	"bytes"
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
	f := fn{m: &wasm.Module{Types: []wasm.RecType{{SubTypes: []wasm.SubType{{Comp: wasm.CompType{Kind: wasm.CompFunc, Params: []wasm.ValType{wasm.I32}, Results: []wasm.ValType{wasm.I32}}}}}}, FuncTypes: []wasm.TypeIdx{{}, {}, {}, {}}, Globals: []wasm.Global{{Type: wasm.GlobalType{Type: wasm.ExternRef}}, {Type: wasm.GlobalType{Type: wasm.I32}}}}, ft: &wasm.CompType{Params: []wasm.ValType{wasm.I32}, Results: []wasm.ValType{wasm.I32}}, traceFuncIdx: 3, policy: currentCodegenPolicy()}
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
			if got := f.onlyLocalTailTransfers(tc.body); got != (tc.want && !profileEnabled) {
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
			if !f.threadSelfTailEntryTest(f.entryTrapEnd) {
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
		if f.threadSelfTailEntryTest(f.entryTrapEnd) || a.Len() != old {
			t.Fatalf("%s: invalid entry test admitted", mode)
		}
	}
}

func TestTailLocalLeafTransfersKeepCycleRelocations(t *testing.T) {
	requireCompilerDiagnostics(t)
	i32 := []wasm.ValType{wasm.I32}
	body := func(target byte) []byte {
		return []byte{0, 0x20, 0, 0x45, 0x04, 0x7f, 0x41, 0, 0x05, 0x20, 0, 0x41, 1, 0x6b, 0x12, target, 0x0b, 0x0b}
	}
	m := modFuncs(t, funcDef{i32, i32, body(1)}, funcDef{i32, i32, body(0)})
	var stats ModuleStats
	c, err := CompileModuleWith(m, CompileOptions{Stats: &stats})
	if err != nil {
		t.Fatal(err)
	}
	defer c.CodeImage.Close()
	for i := range 2 {
		want := 1
		if profileEnabled {
			want = 0
		}
		if got := stats.Funcs[i].Peephole["tail-local-leaf"]; got != want {
			t.Fatalf("function %d leaf=%d want=%d", i, got, want)
		}
	}
}

func TestTailLocalEntryTestThreadingGuards(t *testing.T) {
	for _, wide := range []bool{false, true} {
		for _, nonzero := range []bool{false, true} {
			for _, siteFirst := range []bool{false, true} {
				for _, bad := range []int{-1, 0, 1, 2, 3, 4, 5, 6} {
					a := &a64.Asm{}
					for range 24 {
						a.Nop()
					}
					site, target := 80, 0
					if siteFirst {
						site, target = 12, 48
					}
					entry := target + 12
					word := uint32(0x34000006)
					if wide {
						word |= 1 << 31
					}
					if nonzero {
						word |= 1 << 24
					}
					a.PatchU32(entry, word|3<<5)
					a.PatchU32(site, 0x14000000)
					if !a.PatchBranch26(site, target) {
						t.Fatal("branch")
					}
					if bad >= 0 && bad < 3 {
						a.PatchU32(site-12+bad*4, 0x910043ff)
					}
					if bad >= 3 && bad < 6 {
						a.PatchU32(target+(bad-3)*4, 0xd10043ff)
					}
					if bad == 6 {
						a.PatchU32(entry, 0x91000400)
					}
					original := append([]byte(nil), a.B...)
					got := threadTailEntryTest(a.B, site, target)
					if got != (bad < 0) {
						t.Fatalf("wide=%t nonzero=%t siteFirst=%t bad=%d got=%t", wide, nonzero, siteFirst, bad, got)
					}
					if !got {
						if !bytes.Equal(original, a.B) {
							t.Fatal("failed proof changed code")
						}
						continue
					}
					h, ok := branchTarget(site-12, rdWord(a.B, site-12))
					if !ok || h != entry+12 {
						t.Fatal("hot target")
					}
					c, ok := branchTarget(site-8, rdWord(a.B, site-8))
					if !ok || c != entry+4 {
						t.Fatal("cold target")
					}
					if rdWord(a.B, site-12)&0xff00001f != word&0xff00001f {
						t.Fatal("test width, polarity or register changed")
					}
					if rdWord(a.B, site) != rdWord(original, site) {
						t.Fatal("call graph branch changed")
					}
				}
			}
		}
	}
}
