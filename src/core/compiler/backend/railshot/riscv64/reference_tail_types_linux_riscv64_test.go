//go:build linux && riscv64 && !tinygo && !wago_target_tinygo

package riscv64

import (
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"runtime"
	"sync/atomic"
	"testing"
	"time"
	"unsafe"

	"github.com/wago-org/wago/src/core/compiler/wasm"
	coreruntime "github.com/wago-org/wago/src/core/runtime"
)

func referenceTailWitness(t *testing.T) []byte {
	t.Helper()
	arena, err := coreruntime.NewArena(16)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := arena.Close(); err != nil {
			t.Error(err)
		}
	})
	return arena.Alloc(16)
}

func TestReferenceTailRV64MixedCycleConstantSP(t *testing.T) {
	for _, vector := range []bool{false, true} {
		for _, indexed := range []bool{false, true} {
			for _, nullable := range []bool{false, true} {
				t.Run(fmt.Sprintf("vector%t-indexed%t-nullable%t", vector, indexed, nullable), func(t *testing.T) {
					typ, pad := wasm.F64, uint32(252)
					if vector {
						typ, pad = wasm.V128, 124
					}
					heap := wasm.AbsHeap(wasm.HeapFunc)
					if indexed {
						heap = wasm.IndexedHeap(wasm.TypeIdx{})
					}
					ref := wasm.RefVal(wasm.Ref(nullable, heap, false))
					resultsType := []wasm.ValType{typ, ref, wasm.I32, ref}
					a := &wasm.CompType{Kind: wasm.CompFunc, Params: []wasm.ValType{ref, wasm.I32, ref, typ}, Results: resultsType}
					b := &wasm.CompType{Kind: wasm.CompFunc, Params: []wasm.ValType{typ, ref, ref, wasm.I32}, Results: resultsType}
					// Each hop permutes the two references. The base returns the arriving
					// tuple, not the root tuple, after alternating distinct frame layouts.
					abody := []byte{0x20, 1, 0x45, 4, 0x40, 0x20, 3, 0x20, 0, 0x20, 1, 0x20, 2, 0x0f, 0x0b, 0x42, 7, 0x20, 3, 0x20, 2, 0x20, 0, 0x20, 1, 0x41, 1, 0x6b, 0x12, 1, 0x0b}
					bbody := []byte{0x20, 3, 0x45, 4, 0x40, 0x20, 0, 0x20, 1, 0x20, 3, 0x20, 2, 0x0f, 0x0b, 0x42, 7, 0x20, 2, 0x20, 3, 0x41, 1, 0x6b, 0x20, 1, 0x20, 0, 0x12, 0, 0x0b}
					m := referenceFunctionModule([]*wasm.CompType{{Kind: wasm.CompFunc}, a, b}, []uint32{1, 2}, [][]byte{abody, bbody})
					m.Code[1].Locals.Runs = []wasm.LocalRun{{Count: pad, Type: typ}}
					code, info := referenceFunctionImageWithTails(t, m, vector, true)
					if info[1].FrameBytes <= 2048 || info[1].FrameBytes <= info[0].FrameBytes {
						t.Fatal("alternating large frames", info)
					}
					witness := referenceTailWitness(t)
					code, entries := localTailInstrument(t, code, info, localTailSPProbe(thunkPointerRV64(witness)))
					base := thunkImageRV64(t, code)
					e, j, args, results, trap, _ := thunkFixtureRV64(t, 256)
					first := uint64(0x8000000080000040)
					if nullable {
						first = 0
					}
					second := uint64(0xfedcba9800000080)
					write := func(depth uint32) {
						binary.LittleEndian.PutUint64(witness, ^uint64(0))
						binary.LittleEndian.PutUint64(witness[8:], 0)
						for i := range args {
							args[i] = 0x5a
						}
						for i := range results {
							results[i] = 0xa5
						}
						clear(trap)
						binary.LittleEndian.PutUint64(args, first)
						binary.LittleEndian.PutUint64(args[8:], 0xdeadbeef00000000|uint64(depth))
						binary.LittleEndian.PutUint64(args[16:], second)
						binary.LittleEndian.PutUint64(args[24:], 0x7ff0000000000001)
						if vector {
							binary.LittleEndian.PutUint64(args[32:], 0x8000000000000000)
						}
					}
					var observed uint64
					for _, depth := range []uint32{0, 1, 33, 50000, 50001, 2} {
						write(depth)
						if err := e.CallPrepared(base+uintptr(entries[0]), args, j.LinMemBase(), trap, results); err != nil {
							t.Fatal("reference tail cycle", depth, err)
						}
						want := []uint64{0x7ff0000000000001}
						if vector {
							want = append(want, 0x8000000000000000)
						}
						left, right := first, second
						if depth%2 != 0 {
							left, right = right, left
						}
						want = append(want, left, 0, right)
						for i, v := range want {
							if got := binary.LittleEndian.Uint64(results[8*i:]); got != v {
								t.Fatal("arriving reference tail tuple", depth, i, got, v)
							}
						}
						cells := 4
						if vector {
							cells++
						}
						if !bytes.Equal(results[len(want)*8:], bytes.Repeat([]byte{0xa5}, len(results)-len(want)*8)) || !bytes.Equal(args[cells*8:], bytes.Repeat([]byte{0x5a}, len(args)-cells*8)) || !bytes.Equal(trap, make([]byte, len(trap))) {
							t.Fatal("tail compact sentinels")
						}
						lo, hi := binary.LittleEndian.Uint64(witness), binary.LittleEndian.Uint64(witness[8:])
						if lo == 0 || lo == ^uint64(0) || lo != hi || observed != 0 && lo != observed {
							t.Fatal("reference tails grew stack", depth, lo, hi, observed)
						}
						observed = lo
					}
					if n := testing.AllocsPerRun(10, func() {
						write(41)
						if err := e.CallPrepared(base+uintptr(entries[0]), args, j.LinMemBase(), trap, results); err != nil {
							panic(err)
						}
					}); n != 0 {
						t.Fatal("warm reference tails", n)
					}
				})
			}
		}
	}
}

func TestReferenceTailRV64OriginalRAAndContinuation(t *testing.T) {
	for _, vector := range []bool{false, true} {
		t.Run(fmt.Sprintf("vector%t", vector), func(t *testing.T) {
			typ := wasm.F64
			if vector {
				typ = wasm.V128
			}
			ft := &wasm.CompType{Kind: wasm.CompFunc, Params: []wasm.ValType{wasm.FuncRef, wasm.I32, wasm.FuncRef, typ}, Results: []wasm.ValType{typ, wasm.FuncRef, wasm.I32, wasm.FuncRef}}
			numeric := &wasm.CompType{Kind: wasm.CompFunc, Params: []wasm.ValType{wasm.I32}, Results: []wasm.ValType{wasm.I32}}
			// Both the tailing function and its ordinary caller invoke the numeric
			// callee. The final tuple exposes continuation effects and arriving refs.
			caller := []byte{0x20, 0, 0x20, 1, 0x20, 2, 0x20, 3, 0x10, 1, 0x21, 2, 0x10, 2, 0x20, 2, 0x0b}
			tail := []byte{0x20, 1, 0x10, 2, 0x21, 1, 0x20, 2, 0x20, 1, 0x20, 0, 0x20, 3, 0x12, 3, 0x0b}
			number := []byte{0x20, 0, 0x41, 3, 0x6a, 0x0b}
			leaf := []byte{0x20, 3, 0x20, 0, 0x20, 1, 0x41, 1, 0x6a, 0x20, 2, 0x0b}
			m := referenceFunctionModule([]*wasm.CompType{ft, numeric}, []uint32{0, 0, 1, 0}, [][]byte{caller, tail, number, leaf})
			code, infos := referenceFunctionImageWithTails(t, m, vector, true)
			if infos[0].DirectCalls != 2 || infos[1].DirectCalls != 2 {
				t.Fatal("ordinary and proper tail sites", infos)
			}
			entry := thunkImageRV64(t, code)
			e, j, args, results, trap, _ := thunkFixtureRV64(t, 256)
			for _, n := range []uint64{0xdeadbeef00000007, 0xffffffff00000000, 0x12345678ffffffff, 0x80000000} {
				for i := range args {
					args[i] = 0x5a
				}
				for i := range results {
					results[i] = 0xa5
				}
				clear(trap)
				input := []uint64{0x8000000000000040, n, 0xfedcba9800000080, 0x7ff0000000000001}
				if vector {
					input = append(input, 0x8000000000000000)
				}
				for i, v := range input {
					binary.LittleEndian.PutUint64(args[8*i:], v)
				}
				if err := e.CallPrepared(entry, args, j.LinMemBase(), trap, results); err != nil {
					t.Fatal("ordinary tail continuation", err)
				}
				want := []uint64{input[3]}
				if vector {
					want = append(want, input[4])
				}
				want = append(want, input[2], uint64(uint32(n)+7), input[0])
				for i, v := range want {
					if got := binary.LittleEndian.Uint64(results[8*i:]); got != v {
						t.Fatal("restored RA arriving tuple", i, got, v)
					}
				}
				if !bytes.Equal(results[len(want)*8:], bytes.Repeat([]byte{0xa5}, len(results)-len(want)*8)) || !bytes.Equal(args[len(input)*8:], bytes.Repeat([]byte{0x5a}, len(args)-len(input)*8)) {
					t.Fatal("continuation sentinels")
				}
			}
		})
	}
}

func TestReferenceTailRV64LargerTargetGuardAndReuse(t *testing.T) {
	for _, vector := range []bool{false, true} {
		t.Run(fmt.Sprintf("vector%t", vector), func(t *testing.T) {
			typ := wasm.F64
			if vector {
				typ = wasm.V128
			}
			ft := &wasm.CompType{Kind: wasm.CompFunc, Params: []wasm.ValType{wasm.FuncRef, typ}, Results: []wasm.ValType{typ, wasm.FuncRef}}
			m := referenceFunctionModule([]*wasm.CompType{ft}, []uint32{0, 0}, [][]byte{{0x20, 0, 0x20, 1, 0x12, 1, 0x0b}, {0x20, 1, 0x20, 0, 0x0b}})
			m.Code[1].Locals.Runs = []wasm.LocalRun{{Count: 64, Type: typ}}
			code, info := referenceFunctionImageWithTails(t, m, vector, true)
			witness := referenceTailWitness(t)
			binary.LittleEndian.PutUint64(witness, ^uint64(0))
			code, entries := localTailInstrument(t, code, info, localTailSPProbe(thunkPointerRV64(witness)))
			base := thunkImageRV64(t, code)
			e, j, args, results, trap, _ := thunkFixtureRV64(t, 256)
			write := func() {
				binary.LittleEndian.PutUint64(args, 0xfedcba9800000040)
				binary.LittleEndian.PutUint64(args[8:], 0x7ff0000000000001)
				if vector {
					binary.LittleEndian.PutUint64(args[16:], 0x8000000000000000)
				}
			}
			write()
			if err := e.CallPrepared(base+uintptr(entries[0]), args, j.LinMemBase(), trap, results); err != nil {
				t.Fatal(err)
			}
			sp := uintptr(binary.LittleEndian.Uint64(witness))
			low := sp - uintptr(info[1].FrameBytes)
			n := info[1].FrameBytes - info[0].FrameBytes
			if n <= 0 || low < e.StackLimit() || low+uintptr(n) > e.StackTop() {
				t.Fatal("reference guard sentinel outside stack")
			}
			pointer := *(*unsafe.Pointer)(unsafe.Pointer(&low))
			below := unsafe.Slice((*byte)(pointer), n)
			for i := range below {
				below[i] = 0xa5
			}
			for i := range results {
				results[i] = 0xa5
			}
			clear(trap)
			j.SetStackFence(low + 16)
			write()
			err := e.CallPrepared(base+uintptr(entries[0]), args, j.LinMemBase(), trap, results)
			var tr *coreruntime.TrapError
			if !errors.As(err, &tr) || tr.Code != coreruntime.TrapStackFenceBreached || binary.LittleEndian.Uint64(trap[16:]) != uint64(1)<<32|2 {
				t.Fatal("reference target guard/source", err, trap[16:24])
			}
			if !bytes.Equal(below, bytes.Repeat([]byte{0xa5}, n)) || !bytes.Equal(results, bytes.Repeat([]byte{0xa5}, len(results))) {
				t.Fatal("reference target wrote before guard")
			}
			clear(trap)
			j.SetStackFence(e.StackLimit())
			write()
			if err := e.CallPrepared(base+uintptr(entries[0]), args, j.LinMemBase(), trap, results); err != nil {
				t.Fatal("reference tail guard reuse", err)
			}
			want := []uint64{0x7ff0000000000001}
			if vector {
				want = append(want, 0x8000000000000000)
			}
			want = append(want, 0xfedcba9800000040)
			for i, v := range want {
				if got := binary.LittleEndian.Uint64(results[8*i:]); got != v {
					t.Fatal("reference guard arriving tuple", i, got, v)
				}
			}
			if !bytes.Equal(results[len(want)*8:], bytes.Repeat([]byte{0xa5}, len(results)-len(want)*8)) {
				t.Fatal("reference guard reuse result sentinel")
			}
		})
	}
}

func TestReferenceTailRV64ActiveInterruptAndReuse(t *testing.T) {
	old := runtime.GOMAXPROCS(1)
	defer runtime.GOMAXPROCS(old)
	for _, vector := range []bool{false, true} {
		t.Run(fmt.Sprintf("vector%t", vector), func(t *testing.T) {
			ft := &wasm.CompType{Kind: wasm.CompFunc, Params: []wasm.ValType{wasm.FuncRef}, Results: []wasm.ValType{wasm.FuncRef}}
			m := referenceFunctionModule([]*wasm.CompType{ft}, []uint32{0, 0}, [][]byte{{0x20, 0, 0x12, 0, 0x0b}, {0x20, 0, 0x0b}})
			code, info := referenceFunctionImageWithTails(t, m, vector, true)
			witness := referenceTailWitness(t)
			binary.LittleEndian.PutUint64(witness, ^uint64(0))
			code, entries := localTailInstrument(t, code, info, localTailSPProbe(thunkPointerRV64(witness)))
			base := thunkImageRV64(t, code)
			e, j, args, results, trap, _ := thunkFixtureRV64(t, 256)
			binary.LittleEndian.PutUint64(args, 0x8000000080000040)
			for i := range results {
				results[i] = 0xa5
			}
			done := make(chan error, 1)
			go func() { done <- e.CallPrepared(base+uintptr(entries[0]), args, j.LinMemBase(), trap, results) }()
			word := (*uint64)(unsafe.Pointer(&witness[0]))
			deadline := time.Now().Add(2 * time.Second)
			started := false
			for time.Now().Before(deadline) {
				if atomic.LoadUint64(word) != ^uint64(0) {
					started = true
					break
				}
				select {
				case err := <-done:
					t.Fatal("reference tail cycle returned", err)
				default:
				}
				time.Sleep(100 * time.Microsecond)
			}
			stop := coreruntime.RequestInterruptAsync(trap)
			err := <-done
			stop()
			var tr *coreruntime.TrapError
			if !started || !errors.As(err, &tr) || tr.Code != coreruntime.TrapInterrupted || binary.LittleEndian.Uint64(trap[16:]) != uint64(1)<<32|1 {
				t.Fatal("active reference tail interrupt/source", err, trap[16:24])
			}
			if !bytes.Equal(results, bytes.Repeat([]byte{0xa5}, len(results))) {
				t.Fatal("interrupted reference tail committed result")
			}
			clear(trap)
			binary.LittleEndian.PutUint64(args, 0x8000000080000040)
			if err := e.CallPrepared(base+uintptr(entries[1]), args, j.LinMemBase(), trap, results); err != nil || binary.LittleEndian.Uint64(results) != 0x8000000080000040 || !bytes.Equal(results[8:], bytes.Repeat([]byte{0xa5}, len(results)-8)) {
				t.Fatal("reference tail interrupt reuse", err)
			}
		})
	}
}
