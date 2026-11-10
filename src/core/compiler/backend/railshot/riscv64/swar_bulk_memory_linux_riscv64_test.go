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

	coreruntime "github.com/wago-org/wago/src/core/runtime"
)

func TestSWARRV64BulkMemoryOverlapBoundsLiveVectorAndReuse(t *testing.T) {
	e, j, args, results, trap := swarScalarMemoryExecutionFixture(t)
	mem := j.CurrentBytes()
	before := make([]byte, len(mem))
	for _, fill := range []bool{false, true} {
		fn, ft, memory := swarValidatedMemoryFunction(t, swarBulkMemoryFixture(fill))
		code, info, err := compileSWARPairsWithMemory(fn, ft, memory, emissionMode{production: true, interruptible: true, functionIndex: 22})
		if err != nil || info.FrameBytes <= 2048 {
			t.Fatal("mixed bulk frame", fill, info, err)
		}
		entry := thunkImageRV64(t, code)
		for _, ranges := range [][3]uint32{
			{0, 1, 33}, {1, 0, 33}, {0, 0, 33}, {1, 7, 17}, {7, 1, 17},
			{65528, 0, 8}, {0, 65528, 8}, {65529, 0, 8}, {0, 65529, 8},
			{65536, 65536, 0}, {65537, 0, 0}, {0, 65537, 0},
			{0xffffffff, 0, 0}, {0, 0xffffffff, 0}, {0, 0, 0xffffffff},
			{0, 0xfffffff1, 8}, {65536, 0xffffffff, 0},
		} {
			for i := range mem {
				mem[i] = byte(i*17 + 0x83)
			}
			copy(before, mem)
			input := [5]uint64{0x7ff0000000000001, 0x80000000deadbeef, dirty(ranges[0]), dirty(ranges[1]), dirty(ranges[2])}
			for i, value := range input {
				binary.LittleEndian.PutUint64(args[i*8:], value)
			}
			for i := range results {
				results[i] = 0xa5
			}
			err := e.CallPrepared(entry, args, j.LinMemBase(), trap, results)
			valid := uint64(ranges[0])+uint64(ranges[2]) <= uint64(len(mem))
			if !fill {
				valid = valid && uint64(ranges[1])+uint64(ranges[2]) <= uint64(len(mem))
			}
			if !valid {
				var tr *coreruntime.TrapError
				if !errors.As(err, &tr) || tr.Code != coreruntime.TrapLinMemOutOfBounds || binary.LittleEndian.Uint64(trap[16:]) != uint64(fn.LocalDeclBytes+8)<<32|23 || !bytes.Equal(mem, before) || !bytes.Equal(results, bytes.Repeat([]byte{0xa5}, len(results))) {
					t.Fatal("bulk complete-range prewrite/source", fill, ranges, err, trap[16:24])
				}
				clear(trap)
				continue
			}
			if fill {
				for i := uint32(0); i < ranges[2]; i++ {
					before[ranges[0]+i] = byte(ranges[1])
				}
			} else {
				copy(before[ranges[0]:ranges[0]+ranges[2]], before[ranges[1]:ranges[1]+ranges[2]])
			}
			if err != nil || !bytes.Equal(mem, before) || binary.LittleEndian.Uint64(results) != input[0] || binary.LittleEndian.Uint64(results[8:]) != input[1] || !bytes.Equal(results[16:], bytes.Repeat([]byte{0xa5}, len(results)-16)) {
				t.Fatal("bulk memmove/byte fill/live vector/tail", fill, ranges, err)
			}
			for i, value := range input {
				if binary.LittleEndian.Uint64(args[i*8:]) != value {
					t.Fatal("bulk mutated argument arena", fill, ranges, i)
				}
			}
		}
		binary.LittleEndian.PutUint64(args[16:], dirty(0))
		binary.LittleEndian.PutUint64(args[24:], dirty(1))
		binary.LittleEndian.PutUint64(args[32:], dirty(33))
		if allocs := testing.AllocsPerRun(50, func() {
			if err := e.CallPrepared(entry, args, j.LinMemBase(), trap, results); err != nil {
				panic(err)
			}
		}); allocs != 0 {
			t.Fatal("mixed bulk warm allocations/reuse", fill, allocs)
		}
	}
}

func TestSWARRV64BulkMemoryNestedActiveInterruptStopsContinuation(t *testing.T) {
	previous := goruntime.GOMAXPROCS(1)
	defer goruntime.GOMAXPROCS(previous)
	// Match the existing production bulk interruption fixture's eight-MiB
	// operation so the sole Go P can resume while the finite guest is active.
	for _, fill := range []bool{false, true} {
		e, _, args, results, trap, _ := thunkFixtureRV64(t, 256)
		j, err := coreruntime.NewJobMemory(16 << 20)
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() {
			if err := j.Close(); err != nil {
				t.Error(err)
			}
		})
		j.SetStackFence(e.StackLimit())
		if err := j.RebindTrapCell(trap); err != nil {
			t.Fatal(err)
		}
		leaf := swarBulkMemoryFixture(fill)
		caller := fixtureFunc{params: leaf.params, result: 0x7b, body: []byte{0x20, 0, 0x20, 1, 0x20, 2, 0x20, 3, 0x10, 1, 0x41}}
		caller.body = sleb(caller.body, (16<<20)-16)
		caller.body = append(caller.body, 0x20, 0, 0xfd, 11, 0, 0, 0x0b)
		data := modifySection(binaryModule(caller, leaf), 7, 5, []byte{1, 1, 0x80, 2, 0x80, 2}, false)
		image, err := compileSWARModule(data, emissionMode{production: true, interruptible: true})
		if err != nil {
			t.Fatal(err)
		}
		entry := thunkImageRV64(t, image.code)
		mem := j.CurrentBytes()
		for i := 8 << 20; i < len(mem); i++ {
			mem[i] = 1
		}
		for i := len(mem) - 16; i < len(mem); i++ {
			mem[i] = 0xa5
		}
		vector := [2]uint64{0x7ff0000000000001, 0x80000000deadbeef}
		binary.LittleEndian.PutUint64(args, vector[0])
		binary.LittleEndian.PutUint64(args[8:], vector[1])
		binary.LittleEndian.PutUint64(args[16:], dirty(0))
		source := uint32(8 << 20)
		if fill {
			source = 0xffffffab
		}
		binary.LittleEndian.PutUint64(args[24:], dirty(source))
		binary.LittleEndian.PutUint64(args[32:], dirty(8<<20))
		for i := range results {
			results[i] = 0xa5
		}
		done := make(chan error, 1)
		go func() { done <- e.CallPrepared(entry, args, j.LinMemBase(), trap, results) }()
		word := (*uint32)(unsafe.Pointer(&mem[0]))
		deadline, started := time.Now().Add(2*time.Second), false
		for time.Now().Before(deadline) {
			if atomic.LoadUint32(word) != 0 {
				started = true
				break
			}
			select {
			case err := <-done:
				t.Fatal("nested bulk completed before active interrupt", fill, err)
			default:
			}
			time.Sleep(100 * time.Microsecond)
		}
		stop := coreruntime.RequestInterruptAsync(trap)
		defer stop()
		err = <-done
		stop()
		var tr *coreruntime.TrapError
		if !started || !errors.As(err, &tr) || tr.Code != coreruntime.TrapInterrupted || binary.LittleEndian.Uint64(trap[16:]) != uint64(11)<<32|2 || !bytes.Equal(mem[len(mem)-16:], bytes.Repeat([]byte{0xa5}, 16)) || !bytes.Equal(results, bytes.Repeat([]byte{0xa5}, len(results))) {
			t.Fatal("nested bulk interrupt/source/continuation", fill, started, err, trap[16:24])
		}
		// Already-copied bytes are permitted; interruption is not transactional.
		clear(trap)
		binary.LittleEndian.PutUint64(args[32:], dirty(0))
		if err := e.CallPrepared(entry, args, j.LinMemBase(), trap, results); err != nil || binary.LittleEndian.Uint64(results) != vector[0] || binary.LittleEndian.Uint64(results[8:]) != vector[1] || !bytes.Equal(mem[len(mem)-16:], results[:16]) {
			t.Fatal("nested bulk reuse and continuation store", fill, err)
		}
	}
}
