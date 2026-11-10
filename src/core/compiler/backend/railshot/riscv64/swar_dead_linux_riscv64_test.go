//go:build linux && riscv64 && !tinygo && !wago_target_tinygo

package riscv64

import (
	"bytes"
	"encoding/binary"
	goruntime "runtime"
	"testing"
)

func TestSWARRV64DeadTrapsMemoryFPAndLoopsNeverExecute(t *testing.T) {
	previous := goruntime.GOMAXPROCS(1)
	defer goruntime.GOMAXPROCS(previous)
	e, j, args, results, trap := swarMemoryExecutionFixture(t)
	mem := j.CurrentBytes()
	for i := range mem {
		mem[i] = byte(i*17 + 3)
	}
	before := bytes.Clone(mem)
	mode := emissionMode{production: true, interruptible: true, functionIndex: 14}
	fixture := fixtureFunc{params: []byte{0x7f, 0x7b}, result: 0x7b, locals: []byte{1, 126, 0x7b}, body: []byte{0x20, 1, 0x0f, 0x0b}}
	fn, ft, memory := swarValidatedMemoryFunction(t, fixture)
	baseline, baselineInfo, err := compileSWARPairsWithMemory(fn, ft, memory, mode)
	if err != nil {
		t.Fatal(err)
	}
	for _, tail := range [][]byte{
		{0xfd, 0, 0, 0, 0x1a},                // address is polymorphic, load must not occur
		{0xfd, 11, 0, 0},                     // polymorphic vector store must not occur
		{0x41, 7, 0x41, 0, 0x6d, 0x1a},       // explicit division by zero
		{0x43, 1, 0, 0x80, 0x7f, 0xa8, 0x1a}, // trapping signaling-NaN conversion
		{0x41, 0, 4, 0x7b, 0x20, 1, 5, 0x20, 1, 0x0b, 0x1a},
		{3, 0x40, 0x0c, 0, 0x0b}, // an infinite source loop after return
		{0x1b, 0x1a},             // wholly polymorphic implicit select
	} {
		f := fixture
		f.body = append(append(append([]byte(nil), fixture.body[:3]...), tail...), 0x0b)
		fn, ft, memory := swarValidatedMemoryFunction(t, f)
		code, info, err := compileSWARPairsWithMemory(fn, ft, memory, mode)
		if err != nil || !bytes.Equal(code, baseline) || info.FrameBytes != baselineInfo.FrameBytes || info.SpillSlots != baselineInfo.SpillSlots {
			t.Fatal("ghost machine code/frame", tail, info, baselineInfo, err)
		}
		entry := thunkImageRV64(t, code)
		binary.LittleEndian.PutUint64(args[:], dirty(0xffffffff))
		binary.LittleEndian.PutUint64(args[8:], 0x7ff0000000000001)
		binary.LittleEndian.PutUint64(args[16:], 0x80000000deadbeef)
		input := bytes.Clone(args)
		for i := range results {
			results[i] = 0xa5
		}
		if err := e.CallPrepared(entry, args, j.LinMemBase(), trap, results); err != nil || !bytes.Equal(results, args[8:24]) || !bytes.Equal(args, input) || !bytes.Equal(mem, before) {
			t.Fatal("dead trap/memory/loop executed or live return lost", tail, err, results)
		}
		call := func() {
			if err := e.CallPrepared(entry, args, j.LinMemBase(), trap, results); err != nil {
				panic(err)
			}
		}
		if allocs := testing.AllocsPerRun(50, call); allocs != 0 {
			t.Fatal("dead-tail warm allocations", tail, allocs)
		}
	}
}

func TestSWARRV64DeadOuterBranchJoinResumesEmission(t *testing.T) {
	e, j, args, results, trap := swarMemoryExecutionFixture(t)
	for _, ghost := range []bool{false, true} {
		fn, ft := swarValidatedFunction(t, swarDeadOuterJoinFixture(ghost))
		code, _, err := compileSWARPairs(fn, ft, emissionMode{production: true})
		if err != nil {
			t.Fatal(err)
		}
		entry := thunkImageRV64(t, code)
		for _, input := range dirtyInputs() {
			binary.LittleEndian.PutUint64(args, input[0])
			binary.LittleEndian.PutUint64(args[8:], input[1])
			binary.LittleEndian.PutUint64(args[16:], input[1])
			binary.LittleEndian.PutUint64(args[24:], ^input[0])
			if err := e.CallPrepared(entry, args, j.LinMemBase(), trap, results); err != nil || binary.LittleEndian.Uint64(results) != input[0]^input[1] || binary.LittleEndian.Uint64(results[8:]) != input[1]^^input[0] {
				t.Fatal("live operation after outer join", ghost, input, results, err)
			}
		}
	}
}
