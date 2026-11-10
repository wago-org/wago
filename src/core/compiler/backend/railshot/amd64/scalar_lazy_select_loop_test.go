//go:build (linux || darwin || windows) && amd64

package amd64

import (
	"bytes"
	"encoding/binary"
	"testing"

	enc "github.com/wago-org/wago/src/core/encoder/amd64"
	rt "github.com/wago-org/wago/src/core/runtime"
)

func lazySelectTestBody(dst, src Reg, wide, swapped, byteTest bool, cc Cond) []byte {
	var a enc.Asm
	first := func() { a.LeaScaledW(dst, R9, R10, 1, 5, wide) }
	second := func() { a.LeaScaledW(src, RDX, RDX, 1, -129, wide) }
	if swapped {
		second()
		first()
	} else {
		first()
		second()
	}
	if byteTest {
		a.TestImm8(R8, 0x81)
	} else if wide {
		a.Cmp64(R8, RDX)
	} else {
		a.Cmp32(R8, RDX)
	}
	a.Cmovcc(cc, dst, src, wide)
	a.MovReg64(RDI, dst) // A remains live; B must not remain live.
	return a.B
}

func TestScalarLazySelectBodyMatchesNativeValuesAndFlags(t *testing.T) {
	engine, err := rt.NewEngine()
	if err != nil {
		t.Fatal(err)
	}
	defer engine.Close()
	memory, err := rt.NewJobMemory(65536)
	if err != nil {
		t.Fatal(err)
	}
	defer memory.Close()
	arena, err := rt.NewArena(4096)
	if err != nil {
		t.Fatal(err)
	}
	defer arena.Close()
	args, result, trap := arena.Alloc(256), arena.Alloc(256), arena.Alloc(rt.TrapBufferBytes)
	var setup enc.Asm
	setup.Load64(R9, RDI, 0)
	setup.Load64(R10, RDI, 8)
	setup.Load64(R8, RDI, 16)
	setup.Load64(RDX, RDI, 24)
	for cc := Cond(0); cc < 16; cc++ {
		for _, wide := range []bool{false, true} {
			for _, swapped := range []bool{false, true} {
				for _, byteTest := range []bool{false, true} {
					for _, regs := range [][2]Reg{{RAX, RSI}, {RAX, R11}, {R11, RSI}} {
						dst, src := regs[0], regs[1]
						body := lazySelectTestBody(dst, src, wide, swapped, byteTest, cc)
						var out [128]byte
						n, ok := scalarLazySelectBranchBody(body, out[:], 0)
						if !ok {
							t.Fatalf("rejected cc=%d wide=%v swapped=%v regs=%v body=%x", cc, wide, swapped, regs, body)
						}
						var tail enc.Asm
						tail.Store64(RCX, 0, dst)
						// SETcc memory stores preserve flags: observe every condition after the body.
						for c := byte(0); c < 16; c++ {
							tail.B = append(tail.B, 0x0f, 0x90|c, 0x41, 8+c)
						}
						tail.Ret()
						original := append(append(append([]byte{}, setup.B...), body...), tail.B...)
						rewritten := append(append(append([]byte{}, setup.B...), out[:n]...), tail.B...)
						oldMap, oldEntry, err := rt.MapCode(original)
						if err != nil {
							t.Fatal(err)
						}
						newMap, newEntry, err := rt.MapCode(rewritten)
						if err != nil {
							rt.Unmap(oldMap)
							t.Fatal(err)
						}
						for _, flags := range [][2]uint64{{0, 0}, {0, 1}, {1, 0}, {0x81, 3}, {0x80000000, 1}, {0x7fffffff, 0xffffffff}, {0x8000000000000000, 1}, {0x7fffffffffffffff, ^uint64(0)}} {
							inputs := [4]uint64{0xdeadbeef80000001, 0xcafebabefedcba98, flags[0], flags[1]}
							for i, v := range inputs {
								binary.LittleEndian.PutUint64(args[i*8:], v)
							}
							if err := engine.Call(oldEntry, args, memory.LinearMemory(), trap, result); err != nil {
								t.Fatal(err)
							}
							want := append([]byte{}, result[:24]...)
							if err := engine.Call(newEntry, args, memory.LinearMemory(), trap, result); err != nil {
								t.Fatal(err)
							}
							if !bytes.Equal(result[:24], want) || !wide && binary.LittleEndian.Uint64(result)>>32 != 0 {
								t.Fatalf("cc=%d wide=%v swapped=%v byteTest=%v regs=%v flags=%x got=%x want=%x", cc, wide, swapped, byteTest, regs, flags, result[:24], want)
							}
						}
						rt.Unmap(newMap)
						rt.Unmap(oldMap)
					}
				}
			}
		}
	}
}

func TestScalarLazySelectRejectsObservableOrDependentArms(t *testing.T) {
	body := lazySelectTestBody(RAX, RSI, false, false, true, condE)
	first, _ := decodeScalarLoopLEA(body)
	second, _ := decodeScalarLoopLEA(body[first.end:])
	prefix := first.end + second.end
	for _, live := range []regMask{maskOf(RSI), maskOf(RAX)} {
		var out [128]byte
		if _, ok := scalarLazySelectBranchBody(body, out[:], live); ok {
			t.Fatal("removed a protected arm")
		}
	}
	reject := func(name string, b []byte) {
		t.Helper()
		out := bytes.Repeat([]byte{0xa5}, 128)
		if _, ok := scalarLazySelectBranchBody(b, out, 0); ok || !bytes.Equal(out, bytes.Repeat([]byte{0xa5}, 128)) {
			t.Fatalf("%s accepted or mutated output", name)
		}
	}
	var tail enc.Asm
	tail.Cmp32(RSI, R8)
	reject("B used by latch", append(append([]byte{}, body...), tail.B...))
	var depends enc.Asm
	depends.LeaDispW(RAX, RSI, 7, false)
	depends.B = append(depends.B, body[first.end:]...)
	reject("A reads B", depends.B)
	depends = enc.Asm{}
	depends.B = append(depends.B, body[:first.end]...)
	depends.LeaDispW(RSI, RAX, 7, false)
	depends.B = append(depends.B, body[prefix:]...)
	reject("B reads A", depends.B)
	var condition enc.Asm
	condition.Cmp32(RAX, R8)
	changed := append(append([]byte{}, body[:prefix]...), condition.B...)
	// Replace the original four-byte TEST while retaining CMOV and the tail.
	changed = append(changed, body[prefix+4:]...)
	reject("condition reads A", changed)
	condition = enc.Asm{}
	condition.Add32(R8, RDX)
	condition.TestSelf(R8, false)
	changed = append(append([]byte{}, body[:prefix]...), condition.B...)
	changed = append(changed, body[prefix+4:]...)
	reject("condition writes", changed)
	changed = append([]byte{}, body...)
	changed[0] |= 8
	reject("mixed widths", changed)
	for _, bad := range [][]byte{nil, {0x8d}, {0x8d, 0x04}, {0x8d, 0x05, 0, 0, 0, 0}, {0x8d, 0x04, 0x25, 0, 0, 0, 0}, {0x41, 0x8d, 0x80, 0}} {
		if _, ok := decodeScalarLoopLEA(bad); ok {
			t.Fatalf("accepted malformed/relative LEA %x", bad)
		}
	}
	for _, size := range []int{0, 1, len(body) - 1} {
		out := bytes.Repeat([]byte{0xa5}, size)
		if _, ok := scalarLazySelectBranchBody(body, out, 0); ok || !bytes.Equal(out, bytes.Repeat([]byte{0xa5}, size)) {
			t.Fatalf("short output accepted or mutated size=%d", size)
		}
	}
}

func TestScalarLazySelectProtectsPersistentRegisters(t *testing.T) {
	cases := []func(*fn){
		func(f *fn) { f.pinned = maskOf(RSI) }, func(f *fn) { f.reserved = maskOf(RSI) }, func(f *fn) { f.pinnedLocalMask = maskOf(RSI) },
		func(f *fn) { f.locals = []localDef{{reg: RSI}} }, func(f *fn) { f.globalReg = []Reg{RSI | globalRegDirty} }, func(f *fn) { f.regUser[RSI] = &elem{} },
	}
	body := lazySelectTestBody(RAX, RSI, false, false, true, condE)
	for i, setup := range cases {
		var f fn
		setup(&f)
		var out [128]byte
		if _, ok := scalarLazySelectBranchBody(body, out[:], f.scalarLoopLiveRegisters()); ok {
			t.Fatalf("persistent owner %d was ignored", i)
		}
	}
}
