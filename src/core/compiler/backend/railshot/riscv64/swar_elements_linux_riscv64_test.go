//go:build linux && riscv64 && !tinygo && !wago_target_tinygo

package riscv64

import (
	"bytes"
	"encoding/binary"
	"errors"
	goruntime "runtime"
	"strconv"
	"sync/atomic"
	"testing"
	"time"
	"unsafe"

	"github.com/wago-org/wago/src/core/compiler/wasm"
	coreruntime "github.com/wago-org/wago/src/core/runtime"
)

func swarElementCodeRV64(t *testing.T, f fixtureFunc) (uintptr, uint32) {
	t.Helper()
	fn, ft, functions, table := swarValidatedElementFunction(t, f)
	code, info, calls, err := compileSWARPairsWithElements(fn, ft, nil, nil, functions, nil, 0, table, nil, 256, emissionMode{production: true, interruptible: true, entryPC: fn.LocalDeclBytes})
	if err != nil || info.FrameBytes <= 2048 || calls != nil {
		t.Fatal("mixed original element frame", err)
	}
	return thunkImageRV64(t, code), fn.LocalDeclBytes
}

// Complete canonical rows come from normally validated legacy function-index
// segments. Original declarative/empty indices are represented as dropped.
func swarElementExecutionRV64(t *testing.T, j *coreruntime.JobMemory, entry uintptr, f fixtureFunc) ([]byte, []byte, []byte) {
	t.Helper()
	m, err := wasm.DecodeModule(swarElementSource(f))
	if err != nil {
		t.Fatal(err)
	}
	if err := wasm.ValidateModuleWithConfig(m, wasm.ValidationFeatures{}, 1, wasm.ValidationLimits{MaxFunctionLocals: 128}); err != nil {
		t.Fatal(err)
	}
	if j.PassiveElemPtr() != 0 {
		t.Fatal("element provider requires idle unbound job")
	}
	table, functions := swarOwnedTableModuleRV64(t, j, entry, m)
	binary.LittleEndian.PutUint32(table[4:], 1024)
	ar, err := coreruntime.NewArena(256*16 + 1024*32)
	if err != nil {
		t.Fatal(err)
	}
	descriptors := ar.Alloc(256 * 16)
	for i, segment := range m.Elements {
		var payload []byte
		if segment.Mode.Kind == wasm.ElemPassive && len(segment.Kind.Funcs) != 0 {
			payload = ar.Alloc(len(segment.Kind.Funcs) * 32)
			for n, index := range segment.Kind.Funcs {
				copy(payload[n*32:(n+1)*32], functions[(int(index)+1)*40:(int(index)+1)*40+32])
			}
		}
		if len(payload) != 0 {
			binary.LittleEndian.PutUint64(descriptors[i*16:], uint64(thunkPointerRV64(payload)))
		}
		binary.LittleEndian.PutUint32(descriptors[i*16+8:], uint32(len(payload)/32))
		binary.LittleEndian.PutUint32(descriptors[i*16+12:], 0x5aa50000|uint32(i))
	}
	j.SetPassiveElemPtr(thunkPointerRV64(descriptors))
	t.Cleanup(func() {
		j.SetPassiveElemPtr(0)
		if err := ar.Close(); err != nil {
			t.Error(err)
		}
	})
	return table, functions, descriptors
}

func TestSWARRV64ElementInitOriginalIndicesFullRangesAndRawVector(t *testing.T) {
	for _, index := range []uint32{0, 17, 254, 255} {
		t.Run(strconv.FormatUint(uint64(index), 10), func(t *testing.T) {
			e, j, args, results, trap, _ := thunkFixtureRV64(t, 256)
			f := swarElementFixture(false, index)
			entry, pcBase := swarElementCodeRV64(t, f)
			table, functions, descriptors := swarElementExecutionRV64(t, j, entry, f)
			beforeDescriptors := bytes.Clone(descriptors)
			for i := 0; i < 256; i++ {
				desc := descriptors[i*16 : (i+1)*16]
				wantLength := uint32(4)
				if i == 17 || i == 254 {
					wantLength = 0
				}
				if binary.LittleEndian.Uint32(desc[8:]) != wantLength || binary.LittleEndian.Uint32(desc[12:]) != 0x5aa50000|uint32(i) || (binary.LittleEndian.Uint64(desc) == 0) != (wantLength == 0) {
					t.Fatal("original256 descriptor shape", i)
				}
			}
			sourceLength := uint64(binary.LittleEndian.Uint32(descriptors[int(index)*16+8:]))
			for _, r := range [][3]uint32{{0, 0, 0}, {1024, 0, 0}, {1024, 4, 0}, {0, 4, 0}, {0, 0, 1}, {1, 1, 3}, {1020, 0, 4}, {1023, 0, 1}, {1024, 0, 1}, {1025, 0, 0}, {0, 5, 0}, {0xffffffff, 0, 0}, {0, 0xffffffff, 0}, {0, 0, 0xffffffff}, {0, 0xffffffff, 0xffffffff}} {
				for i := 0; i < 1024; i++ {
					copy(table[8+i*32:8+(i+1)*32], functions[40:72])
				}
				old, want := bytes.Clone(table), bytes.Clone(table)
				valid := uint64(r[0])+uint64(r[2]) <= 1024 && uint64(r[1])+uint64(r[2]) <= sourceLength
				indices := [4]int{1, 0, 1, 1}
				if valid {
					for n := uint32(0); n < r[2]; n++ {
						source := (indices[r[1]+n] + 1) * 40
						copy(want[8+int(r[0]+n)*32:8+(int(r[0]+n)+1)*32], functions[source:source+32])
					}
				}
				input := swarTableInputsRV64(args, r[:]...)
				for i := range results {
					results[i] = 0xa5
				}
				err := e.CallPrepared(entry, args, j.LinMemBase(), trap, results)
				if !valid {
					var tr *coreruntime.TrapError
					pc := pcBase + uint32(bytes.Index(f.body, []byte{0xfc, 12}))
					if !errors.As(err, &tr) || tr.Code != coreruntime.TrapTableOutOfBounds || binary.LittleEndian.Uint64(trap[16:]) != uint64(pc)<<32|1 || !bytes.Equal(table, old) || !bytes.Equal(results, bytes.Repeat([]byte{0xa5}, len(results))) {
						t.Fatal("element init full bounds/source/prewrite", index, r, err)
					}
					clear(trap)
				} else if err != nil || !bytes.Equal(table, want) || !bytes.Equal(results[:16], input[:16]) || binary.LittleEndian.Uint64(results[16:]) != 1024 || !bytes.Equal(results[24:], bytes.Repeat([]byte{0xa5}, len(results)-24)) {
					t.Fatal("canonical32B init/raw V/tails", index, r, err)
				}
				if !bytes.Equal(descriptors, beforeDescriptors) || !bytes.Equal(args, input) {
					t.Fatal("init descriptor/argument writes", index, r)
				}
			}
			swarTableInputsRV64(args, 0, 0, uint32(sourceLength))
			if n := testing.AllocsPerRun(50, func() {
				if err := e.CallPrepared(entry, args, j.LinMemBase(), trap, results); err != nil {
					panic(err)
				}
			}); n != 0 {
				t.Fatal("element init warm allocations", n)
			}
		})
	}
}

func TestSWARRV64ElementDropPreservesPointersPadsAndIndependentState(t *testing.T) {
	e, j, args, results, trap, _ := thunkFixtureRV64(t, 256)
	// Drop and subsequent init belong to one validated function image: canonical
	// func0 references keep the same checked signature before and after drop.
	f := fixtureFunc{params: []byte{0x7b, 0x7f, 0x7f, 0x7f, 0x7f}, results: []byte{0x7b, 0x7f}, locals: []byte{1, 123, 0x7b}, body: []byte{0x20, 0, 0x20, 4, 4, 0x40, 0xfc, 13, 0xff, 1, 0x0b, 0x20, 1, 0x20, 2, 0x20, 3, 0xfc, 12, 0xff, 1, 0, 0xfc, 16, 0, 0x0b}}
	entry, pcBase := swarElementCodeRV64(t, f)
	table, _, descriptors := swarElementExecutionRV64(t, j, entry, f)
	input := swarTableInputsRV64(args, 1024, 0, 0, 1)
	oldTable, old := bytes.Clone(table), bytes.Clone(descriptors)
	for i := range results {
		results[i] = 0xa5
	}
	binary.LittleEndian.PutUint32(old[255*16+8:], 0)
	for round := 0; round < 2; round++ {
		if err := e.CallPrepared(entry, args, j.LinMemBase(), trap, results); err != nil || !bytes.Equal(descriptors, old) || !bytes.Equal(table, oldTable) || !bytes.Equal(args, input) || !bytes.Equal(results[:16], input[:16]) || binary.LittleEndian.Uint64(results[16:]) != 1024 || !bytes.Equal(results[24:], bytes.Repeat([]byte{0xa5}, len(results)-24)) {
			t.Fatal("drop255 idempotence/full neighbors/pointer/pad", round, err)
		}
	}
	for _, r := range [][3]uint32{{1024, 0, 0}, {0, 0, 0}, {0, 1, 0}, {0, 0, 1}} {
		swarTableInputsRV64(args, r[0], r[1], r[2], 0)
		for i := range results {
			results[i] = 0xa5
		}
		err := e.CallPrepared(entry, args, j.LinMemBase(), trap, results)
		if r[1] != 0 || r[2] != 0 {
			var tr *coreruntime.TrapError
			pc := pcBase + uint32(bytes.Index(f.body, []byte{0xfc, 12}))
			if !errors.As(err, &tr) || tr.Code != coreruntime.TrapTableOutOfBounds || binary.LittleEndian.Uint64(trap[16:]) != uint64(pc)<<32|1 || !bytes.Equal(results, bytes.Repeat([]byte{0xa5}, len(results))) {
				t.Fatal("dropped init range/source", r, err)
			}
			clear(trap)
		} else if err != nil {
			t.Fatal("dropped exactend zero", r, err)
		}
		if !bytes.Equal(descriptors, old) || !bytes.Equal(table, oldTable) {
			t.Fatal("dropped init writes")
		}
	}
	// The same code and signatures execute against independent owned descriptors.
	e2, j2, args2, results2, trap2, _ := thunkFixtureRV64(t, 256)
	table2, _, desc2 := swarElementExecutionRV64(t, j2, entry, f)
	swarTableInputsRV64(args2, 0, 0, 4, 0)
	if err := e2.CallPrepared(entry, args2, j2.LinMemBase(), trap2, results2); err != nil || binary.LittleEndian.Uint32(desc2[255*16+8:]) != 4 || !bytes.Equal(descriptors, old) || bytes.Equal(table2[8:40], make([]byte, 32)) {
		t.Fatal("fresh element state isolation", err)
	}
	j.SetPassiveElemPtr(0)
	swarTableInputsRV64(args, 1024, 0, 0, 1)
	for i := range results {
		results[i] = 0xa5
	}
	err := e.CallPrepared(entry, args, j.LinMemBase(), trap, results)
	var tr *coreruntime.TrapError
	pc := pcBase + uint32(bytes.Index(f.body, []byte{0xfc, 13}))
	if !errors.As(err, &tr) || tr.Code != coreruntime.TrapTableOutOfBounds || binary.LittleEndian.Uint64(trap[16:]) != uint64(pc)<<32|1 || !bytes.Equal(descriptors, old) || !bytes.Equal(results, bytes.Repeat([]byte{0xa5}, len(results))) {
		t.Fatal("drop missing directory/source/prewrite", err)
	}
	clear(trap)
	j.SetPassiveElemPtr(thunkPointerRV64(descriptors))
	if n := testing.AllocsPerRun(50, func() {
		if err := e.CallPrepared(entry, args, j.LinMemBase(), trap, results); err != nil {
			panic(err)
		}
	}); n != 0 {
		t.Fatal("drop reuse/warm allocations", n)
	}
}

func TestSWARRV64ElementInitMissingOwnersPayloadAndReuse(t *testing.T) {
	e, j, args, results, trap, _ := thunkFixtureRV64(t, 256)
	f := swarElementFixture(false, 255)
	entry, pcBase := swarElementCodeRV64(t, f)
	table, _, descriptors := swarElementExecutionRV64(t, j, entry, f)
	oldTable, oldDesc := bytes.Clone(table), bytes.Clone(descriptors)
	pointer := binary.LittleEndian.Uint64(descriptors[255*16:])
	for _, bad := range []string{"table-null", "directory-null", "payload-null"} {
		j.SetTablePtr(thunkPointerRV64(table))
		j.SetPassiveElemPtr(thunkPointerRV64(descriptors))
		binary.LittleEndian.PutUint64(descriptors[255*16:], pointer)
		switch bad {
		case "table-null":
			j.SetTablePtr(0)
		case "directory-null":
			j.SetPassiveElemPtr(0)
		case "payload-null":
			binary.LittleEndian.PutUint64(descriptors[255*16:], 0)
		}
		old := bytes.Clone(descriptors)
		input := swarTableInputsRV64(args, 0, 0, 1)
		for i := range results {
			results[i] = 0xa5
		}
		err := e.CallPrepared(entry, args, j.LinMemBase(), trap, results)
		var tr *coreruntime.TrapError
		pc := pcBase + uint32(bytes.Index(f.body, []byte{0xfc, 12}))
		if !errors.As(err, &tr) || tr.Code != coreruntime.TrapTableOutOfBounds || binary.LittleEndian.Uint64(trap[16:]) != uint64(pc)<<32|1 || !bytes.Equal(descriptors, old) || !bytes.Equal(table, oldTable) || !bytes.Equal(args, input) || !bytes.Equal(results, bytes.Repeat([]byte{0xa5}, len(results))) {
			t.Fatal("init missing owner/payload/source/prewrite", bad, err)
		}
		clear(trap)
	}
	j.SetTablePtr(thunkPointerRV64(table))
	j.SetPassiveElemPtr(thunkPointerRV64(descriptors))
	binary.LittleEndian.PutUint64(descriptors[255*16:], pointer)
	swarTableInputsRV64(args, 0, 0, 4)
	if err := e.CallPrepared(entry, args, j.LinMemBase(), trap, results); err != nil || !bytes.Equal(descriptors, oldDesc) {
		t.Fatal("element missing owner reuse", err)
	}
}

func TestSWARRV64ElementInitActiveInterruptWholeRowsAndReuse(t *testing.T) {
	previous := goruntime.GOMAXPROCS(1)
	defer goruntime.GOMAXPROCS(previous)
	e, j, args, results, trap, _ := thunkFixtureRV64(t, 256)
	f := swarElementFixture(false, 255)
	f.body = uleb([]byte{0x20, 0, 0x20, 1, 4, 0x40, 3, 0x40, 0x41, 0, 0x20, 2, 0x20, 3, 0xfc, 12}, 255)
	f.body = append(f.body, 0, 0x20, 2, 0x41, 1, 0x73, 0x21, 2, 0x0c, 0, 0x0b, 0x0b, 0x41, 3, 0xd0, 0x70, 0x26, 0, 0xfc, 16, 0, 0x0b)
	entry, pcBase := swarElementCodeRV64(t, f)
	table, functions, descriptors := swarElementExecutionRV64(t, j, entry, f)
	for i := 0; i < 1024; i++ {
		copy(table[8+i*32:8+(i+1)*32], functions[40:72])
	}
	clear(table[8:40])
	old := bytes.Clone(descriptors)
	// Alternating windows [1,0,1] and [0,1,1] expose mixed32B rows
	// even after the first pass; row3 remains a continuation sentinel.
	input := swarTableInputsRV64(args, 1, 0, 3)
	for i := range results {
		results[i] = 0xa5
	}
	done := make(chan error, 1)
	go func() { done <- e.CallPrepared(entry, args, j.LinMemBase(), trap, results) }()
	word := (*uint64)(unsafe.Pointer(&table[8]))
	started := false
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if atomic.LoadUint64(word) != 0 {
			started = true
			break
		}
		select {
		case err := <-done:
			t.Fatal("active init returned", err)
		default:
		}
		time.Sleep(100 * time.Microsecond)
	}
	stop := coreruntime.RequestInterruptAsync(trap)
	defer stop()
	err := <-done
	stop()
	var tr *coreruntime.TrapError
	source := binary.LittleEndian.Uint64(trap[16:])
	initPC, loopPC := pcBase+14, pcBase+6
	if !started || !errors.As(err, &tr) || tr.Code != coreruntime.TrapInterrupted || (source != uint64(initPC)<<32|1 && source != uint64(loopPC)<<32|1) || !bytes.Equal(args, input) || !bytes.Equal(descriptors, old) || !bytes.Equal(results, bytes.Repeat([]byte{0xa5}, len(results))) {
		t.Fatal("actual init progress/cancel/source/prewrite", started, err, trap[16:24])
	}
	for i := 0; i < 1024; i++ {
		row := table[8+i*32 : 8+(i+1)*32]
		if !bytes.Equal(row, functions[40:72]) && (i > 2 || !bytes.Equal(row, functions[80:112])) {
			t.Fatal("init interrupted split row or caller continuation", i)
		}
	}
	clear(trap)
	swarTableInputsRV64(args, 0, 0, 3)
	if err := e.CallPrepared(entry, args, j.LinMemBase(), trap, results); err != nil || !bytes.Equal(results[:16], input[:16]) || !bytes.Equal(table[8+3*32:8+4*32], make([]byte, 32)) {
		t.Fatal("element cancellation reuse", err)
	}
	if n := testing.AllocsPerRun(50, func() {
		if err := e.CallPrepared(entry, args, j.LinMemBase(), trap, results); err != nil {
			panic(err)
		}
	}); n != 0 {
		t.Fatal("init interruptible warm allocations", n)
	}
}
