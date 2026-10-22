//go:build linux && !tinygo

package riscv64

import (
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"runtime"
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
	native "github.com/wago-org/wago/src/core/runtime/riscv64"
)

func callInstance(t *testing.T, data []byte, regs []uint8, force bool) *Instance {
	t.Helper()
	if regs == nil {
		regs = scalarRegisters[:]
	}
	c, err := compileOptions(data, regs, CompileOptions{ForceFallback: force})
	if err != nil {
		t.Fatal(err)
	}
	if runtime.GOARCH != "riscv64" {
		t.Skip("RV64 execution only; complete admission passed")
	}
	i, err := c.Instantiate(native.MinNativeStackBytes)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := i.Close(); err != nil {
			t.Error(err)
		}
	})
	return i
}

func sumCallFixture(index byte) fixtureFunc {
	// Retain n below the recursive argument; add it to the returned sum.
	return fixtureFunc{params: []byte{0x7f}, result: 0x7f, body: []byte{0x20, 0, 0x45, 0x04, 0x7f, 0x41, 0, 0x05, 0x20, 0, 0x20, 0, 0x41, 1, 0x6b, 0x10, index, 0x6a, 0x0b, 0x0b}}
}
func mutualCallFixture(index, base byte) fixtureFunc {
	return fixtureFunc{params: []byte{0x7f}, result: 0x7f, body: []byte{0x20, 0, 0x45, 0x04, 0x7f, 0x41, base, 0x05, 0x20, 0, 0x41, 1, 0x6b, 0x10, index, 0x0b, 0x0b}}
}
func pressureCallFixture() fixtureFunc {
	return fixtureFunc{params: []byte{0x7f, 0x7f}, result: 0x7f, locals: []byte{1, 1, 0x7f}, body: []byte{0x20, 0, 0x21, 2, 0x20, 2, 0x20, 0, 0x10, 1, 0x20, 0, 0x10, 1, 0x6a, 0x20, 2, 0x6a, 0x6a, 0x20, 1, 0x6e, 0x0b}}
}

func TestDirectCallRelocationAndAdmissionBounds(t *testing.T) {
	forward := fixtureFunc{result: 0x7f, body: []byte{0x10, 1, 0x0b}}
	backward := fixtureFunc{result: 0x7f, body: []byte{0x10, 0, 0x0b}}
	c, err := Compile(binaryModule(forward, backward, sumCallFixture(2)))
	if err != nil {
		t.Fatal(err)
	}
	wantTargets := []int{c.functions[1].Entry, c.functions[0].Entry, c.functions[2].Entry}
	for j, info := range c.functions {
		if !info.Fallback || info.FrameHeaderBytes != 48 || info.DirectCalls != 1 {
			t.Fatal("actual calling frame metadata", info)
		}
		found := 0
		for pc := info.Entry; pc+8 <= info.Entry+info.CodeBytes; pc += 4 {
			lo := binary.LittleEndian.Uint32(c.code[pc+4:])
			if lo&0xfffff != 0x67|1<<7|31<<15 {
				continue
			}
			hi := binary.LittleEndian.Uint32(c.code[pc:])
			if hi&0xfff != 0x17|31<<7 {
				t.Fatal("call envelope")
			}
			target := int64(pc) + int64(int32(hi&0xfffff000)) + int64(int32(lo)>>20)
			if target != int64(wantTargets[j]) {
				t.Fatal("call missing prologue/image rebasing", j, pc, target, wantTargets[j])
			}
			found++
		}
		if found != 1 {
			t.Fatal("actual linked call count", j, found)
		}
	}
	for _, n := range []int{maxDirectCalls, maxDirectCalls + 1} {
		f := fixtureFunc{result: 0x7f}
		for range n {
			f.body = append(f.body, 0x10, 1, 0x1a)
		}
		f.body = append(f.body, 0x41, 0, 0x0b)
		c, err := Compile(binaryModule(f, fixtureFunc{result: 0x7f, body: []byte{0x41, 1, 0x0b}}))
		if n == maxDirectCalls {
			if err != nil || c.functions[0].DirectCalls != n {
				t.Fatal("call-count boundary", err)
			}
		} else if c != nil || !errors.Is(err, ErrUnsupported) {
			t.Fatal("call-count bound", c, err)
		}
	}
	callee := fixtureFunc{params: bytes.Repeat([]byte{0x7f}, 256), result: 0x7f, body: []byte{0x20, 0, 0x0b}}
	for _, n := range []int{63, 64} {
		dead := fixtureFunc{result: 0x7f, body: []byte{0}}
		for range n {
			dead.body = append(dead.body, 0x10, 1, 0x1a)
		}
		dead.body = append(dead.body, 0x0b)
		c, err := Compile(binaryModule(dead, callee))
		if n == 63 {
			if err != nil || c.functions[0].DirectCalls != 0 {
				t.Fatal("dead call budget boundary", err)
			}
		} else if c != nil || !errors.Is(err, ErrUnsupported) {
			t.Fatal("dead call parameter work unbounded", c, err)
		}
	}
	void := binaryModule(fixtureFunc{result: 0x7f, body: []byte{0x10, 1, 0x41, 0, 0x0b}}, fixtureFunc{body: []byte{0x0b}})
	if m, err := wasm.DecodeModule(void); err != nil {
		t.Fatal(err)
	} else if err = wasm.ValidateModule(m); err != nil {
		t.Fatal("void fixture validity", err)
	}
	if c, err := Compile(void); c == nil || err != nil || c.functions[1].Results != 0 {
		t.Fatal("void call admission", c, err)
	}
	if c, err := Compile(binaryModule(fixtureFunc{result: 0x7f, body: []byte{0x10, 7, 0x0b}})); c != nil || err == nil || errors.Is(err, ErrUnsupported) {
		t.Fatal("invalid call skipped normal validation", c, err)
	}
}

func TestNativeDirectCallRecursionAndMutualCalls(t *testing.T) {
	for _, force := range []bool{false, true} {
		i := callInstance(t, binaryModule(sumCallFixture(0), mutualCallFixture(2, 1), mutualCallFixture(1, 0)), nil, force)
		for n := uint32(0); n <= 32; n++ {
			expectInteger(t, i, uint64(n*(n+1)/2), 0, dirty(n))
			v, err := i.Invoke(context.Background(), "b", dirty(n))
			if err != nil || v != uint64(1-n%2) {
				t.Fatal("mutual recursion", n, v, err)
			}
			v, err = i.Invoke(context.Background(), "c", dirty(n))
			if err != nil || v != uint64(n%2) {
				t.Fatal("reverse mutual recursion", n, v, err)
			}
		}
		for range 3 {
			expectInteger(t, i, 0, 1, 100000)
			expectInteger(t, i, 55, 0, dirty(10))
		}
		args := []uint64{dirty(12)}
		var bad bool
		allocs := testing.AllocsPerRun(50, func() { v, err := i.Invoke(context.Background(), "a", args...); bad = bad || err != nil || v != 78 })
		if bad || allocs != 0 {
			t.Fatal("recursive invocation allocation/result", bad, allocs)
		}
	}
}

func TestNativeDirectCallLeafWidthsAndEarlyReturn(t *testing.T) {
	wide := uint64(0xfedcba9876543210)
	fs := []fixtureFunc{
		{result: 0x7f, body: []byte{0x10, 1, 0x0f, 0, 0x0b}},
		{result: 0x7f, body: []byte{0x41, 0x7f, 0x0b}},
		{result: 0x7e, body: []byte{0x10, 3, 0x0b}},
		{result: 0x7e, body: append(sleb([]byte{0x42}, int64(wide)), 0x0b)},
		{result: 0x7f, body: []byte{0x10, 0, 0x41, 1, 0x6a, 0x0b}},
		{result: 0x7e, body: []byte{0x10, 0, 0xac, 0x10, 2, 0x7c, 0x0b}},
	}
	for _, force := range []bool{false, true} {
		i := callInstance(t, binaryModule(fs...), nil, force)
		for j, want := range []uint64{0xffffffff, 0xffffffff, wide, wide, 0, wide - 1} {
			got, err := i.Invoke(context.Background(), string(rune('a'+j)))
			if err != nil || got != want {
				t.Fatal("leaf/width/early-return call", j, got, want, err)
			}
		}
	}
}

func TestNativeDirectCallSharedThenRecursiveFence(t *testing.T) {
	root := fixtureFunc{params: []byte{0x7f}, result: 0x7f, body: []byte{0x20, 0, 0x10, 1, 0x1a, 0x20, 0, 0x10, 2, 0x0b}}
	i := callInstance(t, binaryModule(root, pressureFixture(), sumCallFixture(2)), []uint8{10, 11, 12, 13}, false)
	if i.compiled.functions[1].Fallback {
		t.Fatal("pressure callee must use shared transport registers")
	}
	for range 3 {
		expectInteger(t, i, 0, 1, 100000)
		expectInteger(t, i, 55, 0, dirty(10))
	}
}

func TestNativeDirectCallSharedTransportClobbers(t *testing.T) {
	data := binaryModule(pressureCallFixture(), pressureFixture())
	c, err := compile(data, []uint8{10, 11, 12, 13})
	if err != nil {
		t.Fatal(err)
	}
	if c.functions[1].Fallback {
		t.Fatal("callee must actually use shared allocator")
	}
	writes := uint32(0)
	callee := c.functions[1]
	for pc := callee.Entry; pc < callee.Entry+callee.CodeBytes; pc += 4 {
		w := binary.LittleEndian.Uint32(c.code[pc:])
		switch w & 127 {
		case 0x03, 0x13, 0x1b, 0x33, 0x3b, 0x17, 0x37, 0x67, 0x6f:
			writes |= 1 << ((w >> 7) & 31)
		}
	}
	if writes&(15<<10) != 15<<10 {
		t.Fatal("callee did not clobber all transport registers", writes)
	}
	i := callInstance(t, data, []uint8{10, 11, 12, 13}, false)
	for _, x := range []uint64{0, 1, 0xffffffff, dirty(0x80000000)} {
		for _, y := range []uint64{1, 2, dirty(3)} {
			want := uint64((uint32(x)*98 + 2352) / uint32(y))
			expectInteger(t, i, want, 0, x, y)
		}
		expectInteger(t, i, 0, TrapDivideByZero, x, dirty(0))
		expectInteger(t, i, uint64(uint32(x)*98+2352), 0, x, 1)
	}
}

func TestNativeDirectCallMixedWidthsHighArityAndContext(t *testing.T) {
	callee := fixtureFunc{result: 0x7e, body: []byte{0x20, 0, 0xad, 0x20, 0xff, 1, 0x7c, 0x20, 0x7f, 0x85, 0x23, 0, 0x7c, 0x0b}}
	caller := fixtureFunc{result: 0x7e, body: []byte{0x42, 7}}
	values := make([]uint64, 256)
	for j := 0; j < 256; j++ {
		if j%2 == 0 {
			callee.params = append(callee.params, 0x7f)
			values[j] = uint64(uint32(0x80000000 + uint32(j)))
			caller.body = append(caller.body, 0x41)
			caller.body = sleb(caller.body, int64(int32(values[j])))
		} else {
			callee.params = append(callee.params, 0x7e)
			values[j] = 0xfedcba9876540000 + uint64(j)
			caller.body = append(caller.body, 0x42)
			caller.body = sleb(caller.body, int64(values[j]))
		}
	}
	caller.body = append(caller.body, 0x10, 1, 0x41, 0x7f, 0x10, 2, 0xad, 0x7c, 0x7c, 0x0b)
	low := fixtureFunc{params: []byte{0x7f}, result: 0x7f, body: []byte{0x20, 0, 0x0b}}
	b := stateModule([]fixtureFunc{caller, callee, low}, false, 1, 1, true, []globalFixture{{wide: true, mutable: true, value: 91}}, nil)
	for _, force := range []bool{false, true} {
		i := callInstance(t, b, nil, force)
		want := ((values[0] + values[255]) ^ values[127]) + 91 + 0xffffffff + 7
		expectInteger(t, i, want, 0)
		if got, err := i.Global("g0"); err != nil || got != 91 {
			t.Fatal("call context clobbered", got, err)
		}
		if i.compiled.functions[0].SpillSlots < 257 || i.compiled.functions[0].FrameBytes <= 2047 {
			t.Fatal("large calling homes not exercised")
		}
	}
}

func TestNativeDirectCallTrapPropagationAndOrdering(t *testing.T) {
	// a -> b -> c. c writes one byte before a bounds trap. a/b must skip
	// later writes, preserve trap5, and unwind their calling frames.
	a := fixtureFunc{result: 0x7f, body: []byte{0x10, 1, 0x1a, 0x41, 12, 0x41, 31, 0x3a, 0, 0, 0x41, 7, 0x0b}}
	b := fixtureFunc{result: 0x7f, body: []byte{0x10, 2, 0x1a, 0x41, 11, 0x41, 32, 0x3a, 0, 0, 0x41, 8, 0x0b}}
	c := fixtureFunc{result: 0x7f, body: []byte{0x41, 3, 0x41, 17, 0x3a, 0, 0, 0x41, 0x7f, 0x2d, 0, 0, 0x0b}}
	i := callInstance(t, stateModule([]fixtureFunc{a, b, c, memoryFixture(0x2d, 0)}, false, 1, 1, true, nil, nil), nil, false)
	expectInteger(t, i, 0, TrapMemoryBounds)
	got := readBulkMemory(t, i, 16)
	want := make([]byte, 16)
	want[3] = 17
	if !bytes.Equal(got, want) {
		t.Fatal("nested trap changed effect ordering", got, want)
	}
	if v, err := i.Invoke(context.Background(), "d", dirty(3)); err != nil || v != 17 {
		t.Fatal("nested trap instance reuse", v, err)
	}
	for _, code := range []uint32{TrapUnreachable, TrapDivideByZero, TrapIntegerOverflow} {
		var leaf fixtureFunc
		switch code {
		case TrapUnreachable:
			leaf = fixtureFunc{result: 0x7f, body: []byte{0, 0x0b}}
		case TrapDivideByZero:
			leaf = fixtureFunc{result: 0x7f, body: []byte{0x41, 1, 0x41, 0, 0x6d, 0x0b}}
		case TrapIntegerOverflow:
			leaf = fixtureFunc{result: 0x7f, body: append(sleb([]byte{0x41}, -1<<31), 0x41, 0x7f, 0x6d, 0x0b)}
		}
		chain := fixtureFunc{result: 0x7f, body: []byte{0x10, 1, 0x41, 1, 0x6a, 0x0b}}
		j := callInstance(t, binaryModule(chain, leaf, fixtureFunc{result: 0x7f, body: []byte{0x41, 9, 0x0b}}), nil, false)
		expectInteger(t, j, 0, code)
		if v, err := j.Invoke(context.Background(), "c"); err != nil || v != 9 {
			t.Fatal("trap chain reuse", code, v, err)
		}
	}
}
