//go:build linux && riscv64 && !tinygo && !wago_target_tinygo

package riscv64

import (
	"bytes"
	"encoding/binary"
	"errors"
	goruntime "runtime"
	"sync/atomic"
	"testing"
	"time"
	"unsafe"

	"github.com/wago-org/wago/src/core/compiler/wasm"
	coreruntime "github.com/wago-org/wago/src/core/runtime"
)

func TestSWARRV64IndexedParametersElseAndLoopBackRawCarriers(t *testing.T) {
	previous := goruntime.GOMAXPROCS(1)
	defer goruntime.GOMAXPROCS(previous)
	e, j, args, results, trap, _ := thunkFixtureRV64(t, 8)
	for index, c := range swarIndexedCases() {
		fn, ft, types := swarValidatedIndexedFunction(t, c)
		code, info, err := compileSWARPairsWithTypes(fn, ft, nil, types, emissionMode{production: true, interruptible: true})
		if err != nil || info.FrameBytes <= 2048 {
			t.Fatal("indexed frame", index, info, err)
		}
		entry := thunkImageRV64(t, code)
		conditions := []uint32{0, 1, 2, 17, 31}
		if !c.loop {
			conditions = append(conditions, 0x80000000, 0xffffffff)
		}
		for _, narrow := range []uint32{0x80000000, 0x7f800001, 0xffc0dead} {
			for _, condition := range conditions {
				var input [40]byte
				binary.LittleEndian.PutUint64(input[:], 0x7ff0000000000001)
				binary.LittleEndian.PutUint64(input[8:], 0x80000000deadbeef)
				binary.LittleEndian.PutUint64(input[16:], dirty(narrow))
				binary.LittleEndian.PutUint64(input[24:], 0xfff80000cafebabe)
				binary.LittleEndian.PutUint64(input[32:], dirty(condition))
				var expected [32]byte
				length := 32
				if index <= 1 {
					length = 24
					binary.LittleEndian.PutUint64(input[24:], dirty(condition))
					value := narrow
					if index == 1 && condition == 0 {
						value++
					}
					binary.LittleEndian.PutUint64(expected[:], uint64(value))
					copy(expected[8:24], input[:16])
					if index == 1 && condition == 0 {
						for lane := 8; lane < 24; lane++ {
							expected[lane] = ^expected[lane]
						}
					}
				} else {
					copy(expected[:], input[:32])
					binary.LittleEndian.PutUint64(expected[16:], uint64(narrow))
					if index == 3 {
						length = 16
						binary.LittleEndian.PutUint64(input[16:], dirty(condition))
					}
				}
				copy(args, input[:])
				for i := range results {
					results[i] = 0xa5
				}
				if err := e.CallPrepared(entry, args, j.LinMemBase(), trap, results); err != nil || !bytes.Equal(results[:length], expected[:length]) || !bytes.Equal(args[:40], input[:]) {
					t.Fatal("indexed params/else/backedge/raw", index, narrow, condition, results[:length], expected[:length], err)
				}
				for i := length; i < 64; i++ {
					if results[i] != 0xa5 {
						t.Fatal("indexed result tail", index, i)
					}
				}
			}
		}
		call := func() {
			if err := e.CallPrepared(entry, args, j.LinMemBase(), trap, results); err != nil {
				panic(err)
			}
		}
		if allocs := testing.AllocsPerRun(50, call); allocs != 0 {
			t.Fatal("indexed control allocations", index, allocs)
		}
	}
}

func TestSWARRV64IndexedLoopActiveInterruptExactSourceAndReuse(t *testing.T) {
	previous := goruntime.GOMAXPROCS(1)
	defer goruntime.GOMAXPROCS(previous)
	e, j, args, results, trap := swarMemoryExecutionFixture(t)
	f := swarIndexedCases()[3].function
	// Publish a vector progress marker while retaining the original vector
	// operand across the store, count arithmetic and every loop-back transfer.
	marker := []byte{0x41, 0, 0x20, 0, 0xfd, 11, 0, 0}
	f.body = append(append(append([]byte(nil), f.body[:8]...), marker...), f.body[8:]...)
	fn, ft, memory := swarValidatedMemoryFunction(t, f)
	code, _, err := compileSWARPairsWithTypes(fn, ft, memory, []*wasm.CompType{ft}, emissionMode{production: true, interruptible: true, functionIndex: 13})
	if err != nil {
		t.Fatal(err)
	}
	entry := thunkImageRV64(t, code)
	binary.LittleEndian.PutUint64(args[:], 1)
	binary.LittleEndian.PutUint64(args[8:], 0x7ff0000000000001)
	binary.LittleEndian.PutUint64(args[16:], dirty(0xffffffff))
	before := bytes.Clone(args)
	for i := range results {
		results[i] = 0xa5
	}
	done := make(chan error, 1)
	go func() { done <- e.CallPrepared(entry, args, j.LinMemBase(), trap, results) }()
	word := (*uint32)(unsafe.Pointer(&j.CurrentBytes()[0]))
	deadline := time.Now().Add(2 * time.Second)
	started := false
	for time.Now().Before(deadline) {
		if atomic.LoadUint32(word) != 0 {
			started = true
			break
		}
		select {
		case err := <-done:
			t.Fatal("loop returned before active interrupt", err)
		default:
		}
		time.Sleep(100 * time.Microsecond)
	}
	stop := coreruntime.RequestInterruptAsync(trap)
	defer stop()
	// Join the foreign call before any fixture cleanup can unmap its resources.
	err = <-done
	stop()
	var tr *coreruntime.TrapError
	pc := uint32(fn.LocalDeclBytes + 4)
	if !started || !errors.As(err, &tr) || tr.Code != coreruntime.TrapInterrupted || binary.LittleEndian.Uint64(trap[16:]) != uint64(pc)<<32|14 || !bytes.Equal(results, bytes.Repeat([]byte{0xa5}, len(results))) || !bytes.Equal(args, before) {
		t.Fatal("active indexed loop interruption/source/prewrite", started, err, trap[16:24])
	}
	clear(trap)
	binary.LittleEndian.PutUint64(args[16:], dirty(0))
	if err := e.CallPrepared(entry, args, j.LinMemBase(), trap, results); err != nil || !bytes.Equal(results[:16], args[:16]) {
		t.Fatal("loop reuse after interruption", err, results)
	}
	if allocs := testing.AllocsPerRun(50, func() {
		if err := e.CallPrepared(entry, args, j.LinMemBase(), trap, results); err != nil {
			panic(err)
		}
	}); allocs != 0 {
		t.Fatal("interruptible loop warm allocations", allocs)
	}
}
