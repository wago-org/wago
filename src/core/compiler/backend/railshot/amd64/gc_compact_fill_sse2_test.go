//go:build linux && amd64 && (wago_gc_compact_fill || (wago_gc_compact_copy || wago_gc_compact_loop || wago_gc_compact_pair_loop || wago_gc_compact_range_loop || wago_gc_compact_range_stride))

package amd64

import (
	"github.com/wago-org/wago/src/core/compiler/wasm"
	"strings"
	"testing"
)

func TestCompactFillSSE2Instructions(t *testing.T) {
	m, e := wasm.DecodeModule(gcArrayFillReductionModuleBytes(true))
	if e != nil {
		t.Fatal(e)
	}
	if e = wasm.ValidateModule(m); e != nil {
		t.Fatal(e)
	}
	var stats *ModuleStats
	if diagnosticsEnabled {
		stats = &ModuleStats{}
	}
	cm, e := CompileModuleWith(m, CompileOptions{GCStructHelpers: true, GCArrayHelpers: true, AMD64FeaturesSet: true, AMD64Features: 0, Stats: stats})
	if e != nil {
		t.Fatal(e)
	}
	defer cm.CodeImage.Close()
	if stats != nil && stats.Funcs[0].Peephole["gc-compact-fill-sse2"] != 1 {
		t.Fatal("missing SSE2 fill selection", stats.Funcs[0].Peephole)
	}
	listing := disasm(t, cm.Code)
	if !strings.Contains(listing, "movdqu") || !strings.Contains(listing, "pshufd") {
		t.Fatal("missing SSE2 vector stores/broadcast", listing)
	}
	if strings.Contains(listing, "vmovdqu") || strings.Contains(listing, "vpshufd") {
		t.Fatal("unexpected AVX requirement", listing)
	}
	for _, line := range strings.Split(listing, "\n") {
		if strings.Contains(line, "movdqu") || strings.Contains(line, "pshufd") {
			t.Log(line)
		}
	}
}

func TestCompactFillSSE2NoFreeXMMFallsBack(t *testing.T) {
	m, e := wasm.DecodeModule(gcArrayFillReductionModuleBytes(true))
	if e != nil {
		t.Fatal(e)
	}
	if e = wasm.ValidateModule(m); e != nil {
		t.Fatal(e)
	}
	for _, owned := range []bool{false, true} {
		sc := newScratch()
		f := fn{a: sc.asm, sc: sc, s: sc.stack, m: m, spillFloor: 4, maxSpill: 4}
		if diagnosticsEnabled {
			f.stats = &CodegenStats{}
		}
		for r := Reg(0); r < 16; r++ {
			if owned {
				f.fregUser[r] = &elem{st: storage{kind: stReg, typ: mtF64, reg: r}}
			} else {
				f.fpinned = f.fpinned.add(r)
			}
		}
		beforePins, beforeOwners := f.fpinned, f.fregUser
		for slot, typ := range []machineType{mtI64, mtI32, mtI32, mtI32} {
			f.pushValue(storage{kind: stSlot, typ: typ, slot: uint32(slot)})
		}
		if !f.emitCompactArrayFill(0) {
			t.Fatal("numeric fill unexpectedly declined")
		}
		if f.fpinned != beforePins || f.fregUser != beforeOwners {
			t.Fatal("scalar fallback changed XMM ownership")
		}
		if f.stats != nil && f.stats.Peephole["gc-compact-fill-sse2"] != 0 {
			t.Fatal("vector path selected without a free XMM")
		}
		listing := disasm(t, f.a.B)
		for _, line := range strings.Split(listing, "\n") {
			if strings.Contains(line, "\t") && strings.Contains(line, "xmm") {
				t.Fatal("scalar fallback emitted XMM instruction", line)
			}
		}
		if len(f.a.B) == 0 || f.s.logicalDepth != 0 {
			t.Fatal("scalar fill was not emitted or operands remained")
		}
	}
}
