//go:build (linux || darwin || windows) && amd64

package amd64

import (
	"bytes"
	"github.com/wago-org/wago/src/core/compiler/wasm"
	encoder "github.com/wago-org/wago/src/core/encoder/amd64"
	"testing"
)

func TestBitsetBrTable(t *testing.T) {
	for _, n := range []int{8, 31, 32, 63, 64} {
		labels := make([]uint32, n)
		for i := range labels {
			if i%3 == 1 || i == n-1 {
				labels[i] = 1
			}
		}
		m := brTableLabelsInRAX(t, labels, 0)
		var stats ModuleStats
		opts := CompileOptions{}
		if diagnosticsEnabled {
			opts.Stats = &stats
		}
		cm, err := CompileModuleWith(m, opts)
		if err != nil {
			t.Fatal(err)
		}
		if cm.CodeImage != nil {
			defer cm.CodeImage.Close()
		}
		if diagnosticsEnabled && stats.Funcs[0].Peephole["br-table-bitset"] != 1 {
			t.Fatalf("n=%d missing bitset: %v", n, stats.Funcs[0].Peephole)
		}
		for index := uint64(0); index < uint64(n+2); index++ {
			want := uint64(1000)
			if index < uint64(n) && labels[index] == 1 {
				want++
			}
			if got := runCompiledAmd64u(t, cm, index, 1); got != want {
				t.Fatalf("n=%d index=%d got=%d want=%d", n, index, got, want)
			}
		}
		for _, index := range []uint64{127, 0x80000000, 0xffffffff} {
			if got := runCompiledAmd64u(t, cm, index, 1); got != 1000 {
				t.Fatalf("index=%x wraps into mask: got=%d", index, got)
			}
		}
	}
}

// Force the existing call-free restoration path to move the table index from
// a relinquished local register to RDX. Execute the generated edge code as well
// as checking its shape; the transported value and restored local both survive.
func TestBitsetIndexAfterPinRelinquishment(t *testing.T) {
	for _, index := range []uint32{0, 1, 7, 8, 63, 64, 0x80000000, 0xffffffff} {
		f := callfreeSpilledPin(mtI64)
		f.locals[0].state, f.pinRelinquished = lsReg, false
		f.pinnedLocalMask = maskOf(R12)
		f.ctrl = []ctrlFrame{
			{kind: cfBlock, resultN: 1, branchN: 1, res0: mtI64},
			{kind: cfBlock, resultN: 1, branchN: 1, res0: mtI64},
		}
		var stats CodegenStats
		f.stats = &stats
		f.a.SubRsp(256)
		f.a.Store64(RSP, 8, RCX) // wrapper result pointer
		f.a.Load64(R12, RDI, 0)  // value of the local that allocation will borrow
		f.a.Load64(RAX, RDI, 8)
		f.a.Store64(RSP, f.spillOff(0), RAX)
		f.pushValue(storage{kind: stSlot, typ: mtI64, slot: 0})
		for _, reg := range gpAlloc {
			if reg != R12 {
				f.pinned = f.pinned.add(reg)
			}
		}
		f.pushValue(storage{kind: stConst, typ: mtI32, cval: int64(index)})
		var physicalWrites uint32
		oldObserver := f.a.ObserveGPWrites(func(mask uint32) { physicalWrites |= mask })
		if err := f.opBrTable(wasm.NewReader([]byte{8, 0, 1, 0, 1, 0, 1, 0, 1, 0})); err != nil {
			t.Fatal(err)
		}
		f.a.ObserveGPWrites(oldObserver)
		if regallocCheckEnabled && physicalWrites&(1<<uint(RDX)) == 0 {
			t.Fatal("checked encoder observer did not observe the RDX index write")
		}
		if !f.pinRelinquished || f.locals[0].state != lsReg {
			t.Fatal("fixture did not borrow and restore the local pin")
		}
		move, restore, bt := &encoder.Asm{}, &encoder.Asm{}, &encoder.Asm{}
		move.MovRegReg32(RDX, R12)
		restore.Load64(R12, RSP, f.localAddr(0))
		bt.BtReg(RAX, RDX, true)
		moveAt, restoreAt, btAt := bytes.Index(f.a.B, move.B), bytes.Index(f.a.B, restore.B), bytes.Index(f.a.B, bt.B)
		if moveAt < 0 || restoreAt < moveAt+len(move.B) || btAt < restoreAt+len(restore.B) {
			t.Fatalf("index=%x move/restore/BT order=%d/%d/%d", index, moveAt, restoreAt, btAt)
		}
		if diagnosticsEnabled && (stats.PinRelinquishments != 1 || stats.Peephole["br-table-bitset"] != 1) {
			t.Fatalf("inactive pressure/dispatch counters: %+v", stats)
		}
		// The default inner-block edge adds seven; the member outer-block edge
		// skips it. Both preserve the branch payload and the reclaimed local.
		f.patchFrameEndSites(f.frameEndSites(&f.ctrl[1]))
		f.a.Load64(RAX, RSP, f.spillOff(0))
		f.a.AluRI(0, RAX, 7, true)
		f.a.Store64(RSP, f.spillOff(0), RAX)
		f.patchFrameEndSites(f.frameEndSites(&f.ctrl[0]))
		f.a.Load64(RAX, RSP, f.spillOff(0))
		f.a.AluRR(0x01, RAX, R12, true)
		f.a.Load64(RCX, RSP, 8)
		f.a.Store64(RCX, 0, RAX)
		f.a.AddRsp(256)
		f.a.Ret()
		cm := &encoder.CompiledModule{Code: f.a.B, Entry: []int{0}}
		for _, value := range []uint64{0, 0x8000000000000000, ^uint64(0)} {
			want := value + 123
			if index >= 8 || index&1 == 0 {
				want += 7
			}
			if got := runCompiledAmd64u(t, cm, 123, value); got != want {
				t.Fatalf("index=%x value=%x got=%x want=%x", index, value, got, want)
			}
		}
	}
}

func TestBitsetBrTableTypedMerge(t *testing.T) {
	body := []byte{0, 0x02, 0x7e, 0x02, 0x7e, 0x20, 1, 0x20, 0, 0x0e, 8, 0, 1, 0, 1, 0, 1, 0, 1, 0, 0x0b, 0x42, 7, 0x7c, 0x0b, 0x0b}
	m := mod1(t, []wasm.ValType{wasm.I32, wasm.I64}, []wasm.ValType{wasm.I64}, body)
	for _, compact := range []bool{false, true} {
		var stats ModuleStats
		opts := CompileOptions{CompactNative: compact}
		if diagnosticsEnabled {
			opts.Stats = &stats
		}
		cm, e := CompileModuleWith(m, opts)
		if e != nil {
			t.Fatal(e)
		}
		if cm.CodeImage != nil {
			defer cm.CodeImage.Close()
		}
		if diagnosticsEnabled && stats.Funcs[0].Peephole["br-table-bitset"] != 1 {
			t.Fatalf("typed dispatch lacks active bitset counter: %v", stats.Funcs[0].Peephole)
		}
		for _, value := range []uint64{0, 1, 0x8000000000000000, ^uint64(0)} {
			for _, index := range []uint64{0, 1, 7, 8, 63, 64, 0xffffffff} {
				want := value + 7
				if index < 8 && index%2 == 1 {
					want = value
				}
				if got := runCompiledAmd64u(t, cm, index, value); got != want {
					t.Fatalf("compact=%v index=%x value=%x got=%x want=%x", compact, index, value, got, want)
				}
			}
		}
	}
}
