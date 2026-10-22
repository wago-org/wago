//go:build linux && !tinygo

package riscv64

import (
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"runtime"
	"sync"
	"testing"

	native "github.com/wago-org/wago/src/core/runtime/riscv64"
)

type globalFixture struct {
	wide, mutable bool
	value         int64
}

func stateModule(fs []fixtureFunc, imported bool, min, max uint32, hasMax bool, globals []globalFixture, data []native.MemoryInitializer) []byte {
	b := binaryModule(fs...)
	limits := []byte{0}
	if hasMax {
		limits[0] = 1
	}
	limits = uleb(limits, min)
	if hasMax {
		limits = uleb(limits, max)
	}
	if imported {
		im := []byte{1, 1, 'm', 1, 'x', 2}
		im = append(im, limits...)
		b = modifySection(b, 3, 2, im, false)
	} else {
		b = modifySection(b, 7, 5, append([]byte{1}, limits...), false)
	}
	if len(globals) != 0 {
		g := uleb(nil, uint32(len(globals)))
		for _, v := range globals {
			typ, op := byte(0x7f), byte(0x41)
			if v.wide {
				typ, op = 0x7e, 0x42
			}
			mut := byte(0)
			if v.mutable {
				mut = 1
			}
			g = append(g, typ, mut, op)
			g = sleb(g, v.value)
			g = append(g, 0x0b)
		}
		b = modifySection(b, 7, 6, g, false)
	}
	exports := uleb(nil, uint32(len(fs)+1+len(globals)))
	for j := range fs {
		exports = append(exports, 1, byte('a'+j), 0)
		exports = uleb(exports, uint32(j))
	}
	exports = append(exports, 3, 'm', 'e', 'm', 2, 0)
	for j := range globals {
		name := fmt.Sprintf("g%d", j)
		exports = uleb(exports, uint32(len(name)))
		exports = append(exports, name...)
		exports = append(exports, 3)
		exports = uleb(exports, uint32(j))
	}
	b = modifySection(b, 7, 7, exports, true)
	if len(data) != 0 {
		d := uleb(nil, uint32(len(data)))
		for _, s := range data {
			d = append(d, 0, 0x41)
			d = sleb(d, int64(int32(s.Offset)))
			d = append(d, 0x0b)
			d = uleb(d, uint32(len(s.Bytes)))
			d = append(d, s.Bytes...)
		}
		b = section(b, 11, d)
	}
	return b
}
func memoryFixture(op byte, off uint32) fixtureFunc {
	_, wide, _, store, ok := memoryOpcode(op)
	if !ok {
		panic("memory opcode")
	}
	typ := byte(0x7f)
	if wide {
		typ = 0x7e
	}
	f := fixtureFunc{params: []byte{0x7f}, result: typ, body: []byte{0x20, 0}}
	if store {
		f.params = append(f.params, typ)
		f.body = append(f.body, 0x20, 1)
	}
	f.body = append(f.body, op, 0)
	f.body = uleb(f.body, off)
	if store {
		f.body = append(f.body, 0x20, 1)
	}
	f.body = append(f.body, 0x0b)
	return f
}
func stateInstance(t *testing.T, data []byte, opts InstantiateOptions) *Instance {
	t.Helper()
	c, err := Compile(data)
	if err != nil {
		t.Fatal(err)
	}
	if runtime.GOARCH != "riscv64" {
		t.Skip("RV64 execution only; admission passed")
	}
	i, err := c.InstantiateWithOptions(native.MinNativeStackBytes, opts)
	if err != nil {
		if i != nil {
			i.Close()
		}
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := i.Close(); err != nil {
			t.Error(err)
		}
	})
	return i
}
func loadExpected(b []byte, wide, signed bool) uint64 {
	var v uint64
	for j, x := range b {
		v |= uint64(x) << (8 * j)
	}
	if signed {
		switch len(b) {
		case 1:
			v = uint64(int64(int8(v)))
		case 2:
			v = uint64(int64(int16(v)))
		case 4:
			v = uint64(int64(int32(v)))
		}
	}
	if !wide {
		v = uint64(uint32(v))
	}
	return v
}
func TestNativeMemoryUnalignedBoundsAndNormalization(t *testing.T) {
	for _, op := range []byte{0x28, 0x29, 0x2c, 0x2d, 0x2e, 0x2f, 0x30, 0x31, 0x32, 0x33, 0x34, 0x35, 0x36, 0x37, 0x3a, 0x3b, 0x3c, 0x3d, 0x3e} {
		t.Run(fmt.Sprintf("%x", op), func(t *testing.T) {
			size, wide, signed, store, _ := memoryOpcode(op)
			i := stateInstance(t, stateModule([]fixtureFunc{memoryFixture(op, 3)}, false, 1, 2, true, nil, nil), InstantiateOptions{})
			if !i.compiled.functions[0].Fallback {
				t.Fatal("memory function did not use fallback")
			}
			var pattern [8]byte
			binary.LittleEndian.PutUint64(pattern[:], 0xfedcba98765480ff)
			for _, off := range []uint32{3, 4, 5, 6, 7, 8, 9, 10, 11, 65536 - uint32(size)} {
				addr := uint64(off-3) | 0xdeadbeef00000000
				if store {
					// Distinct initial bytes prove a guest write occurred. Include
					// both guards where in bounds, and the left guard at memory end.
					n := size + 2
					if off+uint32(size) == 65536 {
						n--
					}
					var initial, wantBytes [10]byte
					for j := 0; j < n; j++ {
						initial[j] = 0x5a
					}
					wantBytes = initial
					copy(wantBytes[1:], pattern[:size])
					if err := i.WriteMemory("mem", off-1, initial[:n]); err != nil {
						t.Fatal(err)
					}
					val := uint64(0xfedcba98765480ff)
					want := val
					if !wide {
						want = uint64(uint32(val))
					}
					expectInteger(t, i, want, 0, addr, val)
					var got [10]byte
					if err := i.ReadMemory("mem", off-1, got[:n]); err != nil || got != wantBytes {
						t.Fatal("store bytes or adjacent guard", got, wantBytes, err)
					}
				} else {
					if err := i.WriteMemory("mem", off, pattern[:size]); err != nil {
						t.Fatal(err)
					}
					expectInteger(t, i, loadExpected(pattern[:size], wide, signed), 0, addr)
				}
			}
			// Full-store preflight: even the first otherwise-valid byte must remain.
			before := []byte{71, 72, 73, 74, 75, 76, 77, 78}
			if err := i.WriteMemory("mem", 65528, before); err != nil {
				t.Fatal(err)
			}
			for _, addr := range []uint64{65536 - uint64(size) - 2, 0xffffffff, 0xdeadbeeffffffffe} {
				if store {
					expectInteger(t, i, 0, TrapMemoryBounds, addr, ^uint64(0))
				} else {
					expectInteger(t, i, 0, TrapMemoryBounds, addr)
				}
			}
			after := make([]byte, 8)
			if err := i.ReadMemory("mem", 65528, after); err != nil || string(after) != string(before) {
				t.Fatal("partial OOB store", after, err)
			}
		})
	}
}
func TestNativeMemoryOffsetOverflow(t *testing.T) {
	i := stateInstance(t, stateModule([]fixtureFunc{memoryFixture(0x2d, 0xffffffff)}, false, 1, 1, true, nil, nil), InstantiateOptions{})
	for _, addr := range []uint64{0, 1, 0xffffffff, 0xdeadbeef00000001} {
		expectInteger(t, i, 0, TrapMemoryBounds, addr)
	}
}
func growFixture() fixtureFunc {
	return fixtureFunc{params: []byte{0x7f}, result: 0x7f, body: []byte{0x20, 0, 0x40, 0, 0x0b}}
}
func sizeFixture() fixtureFunc { return fixtureFunc{result: 0x7f, body: []byte{0x3f, 0, 0x0b}} }
func TestNativeMemoryGrowthAndImportedLifetime(t *testing.T) {
	m, err := native.NewLinearMemory(1, 3)
	if err != nil {
		t.Fatal(err)
	}
	defer m.Close()
	wasm := stateModule([]fixtureFunc{growFixture(), sizeFixture(), memoryFixture(0x2d, 0)}, true, 1, 3, true, nil, nil)
	opts := InstantiateOptions{ImportedMemory: m, ImportModule: "m", ImportName: "x"}
	a := stateInstance(t, wasm, opts)
	b := stateInstance(t, wasm, opts)
	expectInteger(t, a, 1, 0, 0xdeadbeef00000001)
	if got, err := b.Invoke(context.Background(), "b"); err != nil || got != 2 {
		t.Fatal("stale second importer", got, err)
	}
	if pages, err := m.Pages(); err != nil || pages != 2 {
		t.Fatal("stale host length", pages, err)
	}
	if got, err := b.Invoke(context.Background(), "c", 65536); err != nil || got != 0 {
		t.Fatal("new page zero", got, err)
	}
	expectInteger(t, b, 0xffffffff, 0, 0xdeadbeefffffffff)
	expectInteger(t, b, 2, 0, 1)
	expectInteger(t, a, 0xffffffff, 0, 1)
	expectInteger(t, a, 3, 0, 0)
	if err = m.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err = m.Lease(); !errors.Is(err, native.ErrClosed) {
		t.Fatal("new importer after close", err)
	}
	expectInteger(t, a, 3, 0, 0)
	if err = b.WriteMemory("mem", 3, []byte{99}); err != nil {
		t.Fatal("leased host access after creator close", err)
	}
	if got, err := a.Invoke(context.Background(), "c", 3); err != nil || got != 99 {
		t.Fatal(got, err)
	}
	var wg sync.WaitGroup
	for range 4 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for range 20 {
				if _, err := a.Invoke(context.Background(), "b"); err != nil {
					t.Error(err)
				}
			}
		}()
	}
	wg.Wait()
}
func TestNativeMemoryOrderedDataAndPerInstanceGlobals(t *testing.T) {
	m, err := native.NewLinearMemory(1, 2)
	if err != nil {
		t.Fatal(err)
	}
	defer m.Close()
	fs := []fixtureFunc{memoryFixture(0x2d, 0)}
	c, err := Compile(stateModule(fs, true, 1, 2, true, nil, []native.MemoryInitializer{{Offset: 3, Bytes: []byte{42}}, {Offset: 65535, Bytes: []byte{11, 22}}}))
	if err != nil {
		t.Fatal(err)
	}
	if runtime.GOARCH != "riscv64" {
		t.Skip("RV64 execution only; admission passed")
	}
	if i, err := c.InstantiateWithOptions(native.MinNativeStackBytes, InstantiateOptions{ImportedMemory: m, ImportModule: "m", ImportName: "x"}); i != nil || err == nil {
		t.Fatal(i, err)
	}
	var b [1]byte
	if err = m.Read(3, b[:]); err != nil || b[0] != 42 {
		t.Fatal("earlier imported initializer lost", b, err)
	}
	globals := make([]globalFixture, 256)
	for j := range globals {
		globals[j] = globalFixture{wide: j%2 == 1, mutable: true, value: int64(j)}
	}
	get := fixtureFunc{result: 0x7e, body: []byte{0x23, 0xff, 1, 0x0b}}
	set := fixtureFunc{params: []byte{0x7e}, result: 0x7e, body: []byte{0x20, 0, 0x24, 0xff, 1, 0x23, 0xff, 1, 0x0b}}
	i := stateInstance(t, stateModule([]fixtureFunc{get, set}, false, 0, 0, true, globals, nil), InstantiateOptions{})
	j := stateInstance(t, stateModule([]fixtureFunc{get, set}, false, 0, 0, true, globals, nil), InstantiateOptions{})
	expectInteger(t, i, 255, 0)
	if got, err := i.Invoke(context.Background(), "b", 0xfedcba9876543210); err != nil || got != 0xfedcba9876543210 {
		t.Fatal(got, err)
	}
	expectInteger(t, i, 0xfedcba9876543210, 0)
	expectInteger(t, j, 255, 0)
	if got, err := i.Global("g255"); err != nil || got != 0xfedcba9876543210 {
		t.Fatal(got, err)
	}
}
func TestNativeMemoryHighCapacityContextAndAllocations(t *testing.T) {
	large := fixtureFunc{params: make([]byte, 256), result: 0x7f, body: []byte{0x41, 7, 0x0b}}
	for j := range large.params {
		large.params[j] = 0x7f
	}
	i := stateInstance(t, stateModule([]fixtureFunc{memoryFixture(0x2d, 0), large}, false, 1, 1, true, nil, []native.MemoryInitializer{{Offset: 3, Bytes: []byte{17}}}), InstantiateOptions{})
	if i.compiled.state.capacity != 256 {
		t.Fatal(i.compiled.state.capacity)
	}
	expectInteger(t, i, 17, 0, 3)
	args := []uint64{3} // Reuse caller storage; failure formatting in helper escapes it.
	if got := testing.AllocsPerRun(100, func() { expectInteger(t, i, 17, 0, args...) }); got != 0 {
		t.Fatal("steady memory call allocations", got)
	}
}
