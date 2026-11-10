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

func TestSWARRV64PassiveDataInitDropBoundsAndInstanceLifetimes(t *testing.T) {
	payloads := make([][]byte, 256)
	for i := range payloads {
		payloads[i] = bytes.Repeat([]byte{byte(i)}, i%17)
	}
	payloads[255] = make([]byte, 257)
	for i := range payloads[255] {
		payloads[255][i] = byte(i*17 + 0x83)
	}
	fixtures := []fixtureFunc{swarDataFixture(255, false), swarDataFixture(255, true), swarDataFixture(0, false)}
	source := swarDataSource(fixtures, true, payloads)
	source = modifySection(source, 7, 6, append([]byte{1}, swarGlobalDeclaration(0x7b, true, [2]uint64{0x7ff0000000000001, 0x80000000deadbeef})...), false)
	image, err := compileSWARModule(source, emissionMode{production: true, interruptible: true})
	if err != nil || len(image.data) != 256 || image.functions[0].FrameBytes <= 2048 {
		t.Fatal("passive mixed module", err)
	}
	clear(source)
	entry := thunkImageRV64(t, image.code)
	type instance struct {
		engine              *coreruntime.Engine
		job                 *coreruntime.JobMemory
		args, results, trap []byte
		data                *swarDataOwner
		globals             *swarGlobalsOwner
	}
	var instances [2]instance
	for i := range instances {
		e, j, args, results, trap := swarScalarMemoryExecutionFixture(t)
		g, err := image.instantiateGlobals(j)
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() {
			if err := g.Close(); err != nil {
				t.Error(err)
			}
		})
		d, err := image.instantiateData(j)
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() {
			if err := d.Close(); err != nil {
				t.Error(err)
			}
		})
		instances[i] = instance{e, j, args, results, trap, d, g}
	}
	for n := range instances {
		in := &instances[n]
		dir := swarOwnedDataDirectory(in.data, 256)
		// A nonzero pad sentinel establishes that drop writes exactly len32.
		binary.LittleEndian.PutUint32(dir[255*16+12:], 0x5aa55aa5)
		dirBefore := bytes.Clone(dir)
		mem, before := in.job.CurrentBytes(), make([]byte, 65536)
		for _, ranges := range [][3]uint32{
			{0, 0, 257}, {1, 1, 17}, {7, 255, 2}, {65528, 249, 8}, {65529, 249, 8},
			{0, 250, 8}, {65536, 257, 0}, {65537, 0, 0}, {0, 258, 0},
			{0xffffffff, 0, 0}, {0, 0xffffffff, 0}, {0, 0, 0xffffffff},
		} {
			for i := range mem {
				mem[i] = byte(i*11 + 0xa5)
			}
			copy(before, mem)
			input := [5]uint64{0x7ff0000000000001, 0x80000000deadbeef, dirty(ranges[0]), dirty(ranges[1]), dirty(ranges[2])}
			for i, v := range input {
				binary.LittleEndian.PutUint64(in.args[i*8:], v)
			}
			for i := range in.results {
				in.results[i] = 0xa5
			}
			err := in.engine.CallPrepared(entry, in.args, in.job.LinMemBase(), in.trap, in.results)
			valid := uint64(ranges[0])+uint64(ranges[2]) <= uint64(len(mem)) && uint64(ranges[1])+uint64(ranges[2]) <= uint64(len(payloads[255]))
			if !valid {
				var tr *coreruntime.TrapError
				if !errors.As(err, &tr) || tr.Code != coreruntime.TrapLinMemOutOfBounds || binary.LittleEndian.Uint64(in.trap[16:]) != uint64(len(fixtures[0].locals)+8)<<32|1 || !bytes.Equal(mem, before) || !bytes.Equal(in.results, bytes.Repeat([]byte{0xa5}, len(in.results))) {
					t.Fatal("init complete bounds/source before writes", n, ranges, err, in.trap[16:24])
				}
				clear(in.trap)
			} else {
				copy(before[ranges[0]:ranges[0]+ranges[2]], payloads[255][ranges[1]:ranges[1]+ranges[2]])
				if err != nil || !bytes.Equal(mem, before) || binary.LittleEndian.Uint64(in.results) != input[0] || binary.LittleEndian.Uint64(in.results[8:]) != input[1] || !bytes.Equal(in.results[16:], bytes.Repeat([]byte{0xa5}, len(in.results)-16)) {
					t.Fatal("init copy/live V/tail", n, ranges, err)
				}
			}
			for i, v := range input {
				if binary.LittleEndian.Uint64(in.args[i*8:]) != v {
					t.Fatal("init argument changed", n, i)
				}
			}
			if !bytes.Equal(dir, dirBefore) || !bytes.Equal(swarOwnedDataPayload(dir[255*16:]), payloads[255]) {
				t.Fatal("init changed descriptor or source payload")
			}
		}
		// The naturally empty descriptor has ptr0,len0; zero-count success
		// must branch before ever trying to read that pointer.
		for _, src := range []uint32{0, 1} {
			binary.LittleEndian.PutUint64(in.args[16:], dirty(65536))
			binary.LittleEndian.PutUint64(in.args[24:], dirty(src))
			binary.LittleEndian.PutUint64(in.args[32:], dirty(0))
			err := in.engine.CallPrepared(entry+uintptr(image.functions[2].Entry), in.args, in.job.LinMemBase(), in.trap, in.results)
			if src == 0 && err != nil || src != 0 && err == nil {
				t.Fatal("empty payload zero-count bounds", src, err)
			}
			clear(in.trap)
		}
		binary.LittleEndian.PutUint64(in.args[16:], dirty(0))
		binary.LittleEndian.PutUint64(in.args[24:], dirty(0))
		binary.LittleEndian.PutUint64(in.args[32:], dirty(257))
		if allocs := testing.AllocsPerRun(50, func() {
			if err := in.engine.CallPrepared(entry, in.args, in.job.LinMemBase(), in.trap, in.results); err != nil {
				panic(err)
			}
		}); allocs != 0 {
			t.Fatal("positive init warm allocations", allocs)
		}
		if err := in.engine.CallPrepared(entry+uintptr(image.functions[1].Entry), in.args, in.job.LinMemBase(), in.trap, in.results); err != nil {
			t.Fatal("data drop", err)
		}
		wantDir := bytes.Clone(dirBefore)
		binary.LittleEndian.PutUint32(wantDir[255*16+8:], 0)
		if !bytes.Equal(dir, wantDir) {
			t.Fatal("drop changed pointer/pad/neighbor", n)
		}
		if n == 0 {
			other := swarOwnedDataDirectory(instances[1].data, 256)
			if binary.LittleEndian.Uint32(other[255*16+8:]) != 257 {
				t.Fatal("drop affected other instance")
			}
		}
		for _, count := range []uint32{0, 1} {
			copy(before, mem)
			binary.LittleEndian.PutUint64(in.args[16:], dirty(0))
			binary.LittleEndian.PutUint64(in.args[24:], dirty(0))
			binary.LittleEndian.PutUint64(in.args[32:], dirty(count))
			err := in.engine.CallPrepared(entry, in.args, in.job.LinMemBase(), in.trap, in.results)
			if count == 0 && err != nil || count != 0 && err == nil || !bytes.Equal(mem, before) {
				t.Fatal("dropped data zero/nonzero init", n, count, err)
			}
			clear(in.trap)
		}
		binary.LittleEndian.PutUint64(in.args[32:], dirty(0))
		if allocs := testing.AllocsPerRun(50, func() {
			if err := in.engine.CallPrepared(entry, in.args, in.job.LinMemBase(), in.trap, in.results); err != nil {
				panic(err)
			}
		}); allocs != 0 {
			t.Fatal("init dropped zero-count warm allocations", allocs)
		}
		if allocs := testing.AllocsPerRun(50, func() {
			if err := in.engine.CallPrepared(entry+uintptr(image.functions[1].Entry), in.args, in.job.LinMemBase(), in.trap, in.results); err != nil {
				panic(err)
			}
		}); allocs != 0 {
			t.Fatal("drop warm allocations", allocs)
		}
	}
	if err := instances[0].data.Close(); err != nil || instances[0].job.CaptureInstanceContext().PassiveDataPtr != 0 || instances[0].job.CaptureInstanceContext().GlobalsPtr != instances[0].globals.directory {
		t.Fatal("data close changed independent global owner", err)
	}
	in := &instances[1]
	if err := in.engine.CallPrepared(entry, in.args, in.job.LinMemBase(), in.trap, in.results); err != nil {
		t.Fatal("surviving data owner", err)
	}
	fresh, err := image.instantiateData(instances[0].job)
	if err != nil {
		t.Fatal(err)
	}
	if binary.LittleEndian.Uint32(swarOwnedDataDirectory(fresh, 256)[255*16+8:]) != 257 {
		t.Fatal("fresh data owner retained dropped length")
	}
	if err := fresh.Close(); err != nil {
		t.Fatal(err)
	}
}

func TestSWARRV64PassiveDataNestedActiveInitInterruptStopsContinuation(t *testing.T) {
	previous := goruntime.GOMAXPROCS(1)
	defer goruntime.GOMAXPROCS(previous)
	e, j, args, results, trap := swarScalarMemoryExecutionFixture(t)
	// Repeat bounded init operations in an outer loop to keep the guest active
	// without exceeding the source/payload quota or relying on one copy's time.
	leaf := fixtureFunc{params: []byte{0x7b, 0x7f, 0x7f, 0x7f, 0x7f}, result: 0x7b, locals: []byte{1, 123, 0x7b}, body: []byte{0x20, 0, 2, 0x40, 3, 0x40, 0x20, 4, 0x45, 0x0d, 1, 0x20, 1, 0x20, 2, 0x20, 3}}
	initPC := uint32(len(leaf.locals) + len(leaf.body))
	loopPC := uint32(len(leaf.locals) + 4)
	leaf.body = append(leaf.body, 0xfc, 8, 0, 0, 0x20, 4, 0x41, 1, 0x6b, 0x21, 4, 0x0c, 0, 0x0b, 0x0b, 0x0b)
	caller := fixtureFunc{params: leaf.params, result: 0x7b}
	for i := range leaf.params {
		caller.body = append(caller.body, 0x20, byte(i))
	}
	caller.body = append(caller.body, 0x10, 1, 0x41)
	caller.body = sleb(caller.body, 65520)
	caller.body = append(caller.body, 0x20, 0, 0xfd, 11, 0, 0, 0x0b)
	payload := bytes.Repeat([]byte{0x83}, 65520)
	image, err := compileSWARModule(swarDataSource([]fixtureFunc{caller, leaf}, true, [][]byte{payload}), emissionMode{production: true, interruptible: true})
	if err != nil {
		t.Fatal(err)
	}
	owner, err := image.instantiateData(j)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := owner.Close(); err != nil {
			t.Error(err)
		}
	})
	entry := thunkImageRV64(t, image.code)
	mem := j.CurrentBytes()
	for i := len(mem) - 16; i < len(mem); i++ {
		mem[i] = 0xa5
	}
	input := [6]uint64{0x7ff0000000000001, 0x80000000deadbeef, dirty(0), dirty(0), dirty(65520), dirty(0xffffffff)}
	for i, v := range input {
		binary.LittleEndian.PutUint64(args[i*8:], v)
	}
	for i := range results {
		results[i] = 0xa5
	}
	done := make(chan error, 1)
	go func() { done <- e.CallPrepared(entry, args, j.LinMemBase(), trap, results) }()
	deadline, started := time.Now().Add(2*time.Second), false
	word := (*uint32)(unsafe.Pointer(&mem[0]))
	for time.Now().Before(deadline) {
		if atomic.LoadUint32(word) != 0 {
			started = true
			break
		}
		select {
		case err := <-done:
			t.Fatal("active init loop unexpectedly completed", err)
		default:
		}
		time.Sleep(100 * time.Microsecond)
	}
	stop := coreruntime.RequestInterruptAsync(trap)
	defer stop()
	err = <-done
	stop()
	var tr *coreruntime.TrapError
	source := binary.LittleEndian.Uint64(trap[16:])
	if !started || !errors.As(err, &tr) || tr.Code != coreruntime.TrapInterrupted || source != uint64(initPC)<<32|2 && source != uint64(loopPC)<<32|2 || !bytes.Equal(mem[len(mem)-16:], bytes.Repeat([]byte{0xa5}, 16)) || !bytes.Equal(results, bytes.Repeat([]byte{0xa5}, len(results))) {
		t.Fatal("active init interruption/source/blocked continuation", started, err, source, initPC, loopPC)
	}
	// Valid copied bytes can remain. Join and stop precede trap clearing and
	// owner retirement; no transactional memory guarantee is asserted.
	clear(trap)
	binary.LittleEndian.PutUint64(args[40:], dirty(0))
	if err := e.CallPrepared(entry, args, j.LinMemBase(), trap, results); err != nil || binary.LittleEndian.Uint64(results) != input[0] || binary.LittleEndian.Uint64(results[8:]) != input[1] || !bytes.Equal(mem[len(mem)-16:], results[:16]) {
		t.Fatal("active init reuse/continuation", err)
	}
}
