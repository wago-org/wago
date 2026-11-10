//go:build amd64

package amd64

import (
	"reflect"
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
	encoder "github.com/wago-org/wago/src/core/encoder/amd64"
)

func TestGlobalPinIndexOrderingAndReset(t *testing.T) {
	f := &fn{m: &wasm.Module{Globals: make([]wasm.Global, 4096)}}
	f.initGlobalRegs(4096)
	// Module pin order follows profitability, not global index.
	f.installModuleGlobals([]moduleGlobalPin{{global: 3000, reg: R15}, {global: 7, reg: R14}})
	f.globalReg[100] = R12 | globalRegDirty
	f.recordGlobalPin(100)
	f.recordGlobalPin(3000)
	if got := f.globalPinIndices[:f.nGlobalPins]; !reflect.DeepEqual(got, []uint32{7, 100, 3000}) {
		t.Fatalf("global pin order = %v", got)
	}
	if allocs := testing.AllocsPerRun(100, func() { f.recordGlobalPin(100) }); allocs != 0 {
		t.Fatalf("existing pin allocated %v times", allocs)
	}
	f.initGlobalRegs(4096)
	if f.nGlobalPins != 0 || f.globalReg[100] != regNone {
		t.Fatal("reused function retained global pin state")
	}
}

func TestGlobalPinSyncKeepsAscendingEmissionOrder(t *testing.T) {
	m := &wasm.Module{Globals: make([]wasm.Global, 4096)}
	for i := range m.Globals {
		m.Globals[i].Type = wasm.GlobalType{Type: wasm.I64, Mutable: true}
	}
	f := &fn{m: m, a: &encoder.Asm{}}
	f.initGlobalRegs(len(m.Globals))
	for _, p := range []moduleGlobalPin{{global: 3000, reg: R12}, {global: 7, reg: R13}, {global: 100, reg: R14}} {
		f.globalReg[p.global] = p.reg | globalRegDirty
		f.recordGlobalPin(p.global)
	}
	f.derivePinnedGlobals()
	want := &encoder.Asm{}
	for _, p := range []moduleGlobalPin{{global: 7, reg: R13}, {global: 100, reg: R14}, {global: 3000, reg: R12}} {
		// Use one-pin lowering as the oracle for each ordered transfer.
		one := &fn{m: m, a: want}
		one.initGlobalRegs(len(m.Globals))
		one.globalReg[p.global] = p.reg
		one.recordGlobalPin(p.global)
		one.derivePinnedGlobals()
	}
	if !reflect.DeepEqual(f.a.B, want.B) {
		t.Fatalf("pin load order changed: %x != %x", f.a.B, want.B)
	}
}

func BenchmarkGlobalPinSynchronization(b *testing.B) {
	m := &wasm.Module{Globals: make([]wasm.Global, 65536)}
	m.Globals[65535].Type = wasm.GlobalType{Type: wasm.I64, Mutable: true}
	f := &fn{m: m, a: &encoder.Asm{}}
	f.initGlobalRegs(len(m.Globals))
	f.globalReg[65535] = R12
	f.recordGlobalPin(65535)
	f.derivePinnedGlobals()
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		f.a.B = f.a.B[:0]
		f.derivePinnedGlobals()
	}
}
