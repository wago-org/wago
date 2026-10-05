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

func swarBulkTableExecutionRV64(t *testing.T, j *coreruntime.JobMemory, f fixtureFunc) (uintptr, []byte, []byte, uint32) {
	t.Helper()
	fn, ft, functions, tableType := swarValidatedBulkTableFunction(t, f)
	code, info, _, err := compileSWARPairsWithTable(fn, ft, nil, nil, functions, nil, 0, tableType, emissionMode{production: true, interruptible: true, entryPC: fn.LocalDeclBytes})
	if err != nil || info.FrameBytes <= 2048 {
		t.Fatal("mixed bulk table frame", err)
	}
	entry := thunkImageRV64(t, code)
	table, directory := swarOwnedTableFixtureRV64(t, j, entry, f)
	// This provider's full1024-entry allocation matches the newly validated
	// min3/max1024 declaration. Initialize its real length/capacity before entry.
	binary.LittleEndian.PutUint32(table, 3)
	binary.LittleEndian.PutUint32(table[4:], 1024)
	return entry, table, directory, fn.LocalDeclBytes
}

func swarTableInputsRV64(args []byte, operands ...uint32) []byte {
	for i := range args {
		args[i] = byte(i*37+i/8*19) ^ 0x5a
	}
	binary.LittleEndian.PutUint64(args, 0x7ff0000000000001)
	binary.LittleEndian.PutUint64(args[8:], 0x80000000deadbeef)
	for i, v := range operands {
		binary.LittleEndian.PutUint64(args[16+i*8:], dirty(v))
	}
	return bytes.Clone(args)
}

func TestSWARRV64BulkTableCopyOverlapDirtyRangesAndLiveVector(t *testing.T) {
	e, j, args, results, trap, _ := thunkFixtureRV64(t, 256)
	f := swarBulkTableFixture(14)
	entry, table, directory, pcBase := swarBulkTableExecutionRV64(t, j, f)
	for _, r := range [][3]uint32{{0, 0, 0}, {3, 3, 0}, {1024, 1024, 0}, {0, 1, 2}, {1, 0, 2}, {0, 0, 3}, {1000, 1, 24}, {1, 1000, 24}, {0, 0, 1024}, {1024, 0, 1}, {0, 1024, 1}, {1025, 0, 0}, {0, 0xffffffff, 0}, {0xffffffff, 0, 1}, {1, 1, 0xffffffff}} {
		binary.LittleEndian.PutUint32(table, 1024)
		for i := 0; i < 1024; i++ {
			row := table[8+i*32 : 8+(i+1)*32]
			clear(row)
			if i%3 != 0 {
				copy(row, directory[40+(i%2)*40:72+(i%2)*40])
			}
		}
		old := bytes.Clone(table)
		want := bytes.Clone(table)
		valid := uint64(r[0])+uint64(r[2]) <= 1024 && uint64(r[1])+uint64(r[2]) <= 1024
		if valid {
			copy(want[8+int(r[0])*32:8+int(r[0]+r[2])*32], want[8+int(r[1])*32:8+int(r[1]+r[2])*32])
		}
		input := swarTableInputsRV64(args, r[:]...)
		for i := range results {
			results[i] = 0xa5
		}
		err := e.CallPrepared(entry, args, j.LinMemBase(), trap, results)
		if !valid {
			var tr *coreruntime.TrapError
			pc := pcBase + uint32(bytes.Index(f.body, []byte{0xfc, 14}))
			if !errors.As(err, &tr) || tr.Code != coreruntime.TrapTableOutOfBounds || binary.LittleEndian.Uint64(trap[16:]) != uint64(pc)<<32|1 || !bytes.Equal(table, old) || !bytes.Equal(results, bytes.Repeat([]byte{0xa5}, len(results))) {
				t.Fatal("copy full bounds/source/prewrite", r, err)
			}
			clear(trap)
		} else {
			if err != nil || !bytes.Equal(table, want) || !bytes.Equal(results[:16], input[:16]) || binary.LittleEndian.Uint64(results[16:]) != 1024 || !bytes.Equal(results[24:], bytes.Repeat([]byte{0xa5}, len(results)-24)) {
				t.Fatal("copy memmove/full32B/live V/tails", r, err)
			}
		}
		if !bytes.Equal(args, input) {
			t.Fatal("copy argument writes", r)
		}
	}
	swarTableInputsRV64(args, 1, 0, 2)
	if n := testing.AllocsPerRun(50, func() {
		if err := e.CallPrepared(entry, args, j.LinMemBase(), trap, results); err != nil {
			panic(err)
		}
	}); n != 0 {
		t.Fatal("copy warm allocations", n)
	}
}

func TestSWARRV64BulkTableFillReferenceGuardsAndWholeEntries(t *testing.T) {
	e, j, args, results, trap, _ := thunkFixtureRV64(t, 256)
	f := swarBulkTableFixture(17)
	entry, table, directory, pcBase := swarBulkTableExecutionRV64(t, j, f)
	for _, r := range [][2]uint32{{0, 0}, {3, 0}, {1024, 0}, {0, 1}, {1023, 1}, {0, 1024}, {1024, 1}, {1025, 0}, {0xffffffff, 0}, {1, 0xffffffff}} {
		for _, nonnull := range []uint32{0, 1, 0x80000000, 0xffffffff} {
			binary.LittleEndian.PutUint32(table, 1024)
			for i := 0; i < 1024; i++ {
				copy(table[8+i*32:8+(i+1)*32], directory[40:72])
			}
			old, want := bytes.Clone(table), bytes.Clone(table)
			valid := uint64(r[0])+uint64(r[1]) <= 1024
			if valid {
				for i := r[0]; i < r[0]+r[1]; i++ {
					row := want[8+int(i)*32 : 8+(int(i)+1)*32]
					clear(row)
					if nonnull != 0 {
						copy(row, directory[80:112])
					}
				}
			}
			input := swarTableInputsRV64(args, r[0], r[1], nonnull)
			for i := range results {
				results[i] = 0xa5
			}
			err := e.CallPrepared(entry, args, j.LinMemBase(), trap, results)
			if !valid {
				var tr *coreruntime.TrapError
				pc := pcBase + uint32(bytes.Index(f.body, []byte{0xfc, 17}))
				if !errors.As(err, &tr) || tr.Code != coreruntime.TrapTableOutOfBounds || binary.LittleEndian.Uint64(trap[16:]) != uint64(pc)<<32|1 || !bytes.Equal(table, old) || !bytes.Equal(results, bytes.Repeat([]byte{0xa5}, len(results))) {
					t.Fatal("fill bounds/source/prewrite", r, nonnull, err)
				}
				clear(trap)
			} else if err != nil || !bytes.Equal(table, want) || !bytes.Equal(results[:16], input[:16]) || binary.LittleEndian.Uint64(results[16:]) != 1024 || !bytes.Equal(results[24:], bytes.Repeat([]byte{0xa5}, len(results)-24)) {
				t.Fatal("fill entire32B/reference/live V/tails", r, nonnull, err)
			}
			if !bytes.Equal(args, input) {
				t.Fatal("fill argument writes")
			}
		}
	}
	originalTag, originalCtx := binary.LittleEndian.Uint64(directory[96:]), binary.LittleEndian.Uint64(directory[112:])
	for _, bad := range []string{"owner", "context", "context-zero", "directory-null", "table-null", "bounds-before-owner"} {
		binary.LittleEndian.PutUint64(directory[96:], originalTag)
		binary.LittleEndian.PutUint64(directory[112:], originalCtx)
		j.SetFuncRefDesc(thunkPointerRV64(directory))
		j.SetTablePtr(thunkPointerRV64(table))
		dest, wantCode := uint32(0), coreruntime.TrapIndirectOutOfBounds
		wantPC := pcBase + uint32(bytes.Index(f.body, []byte{0xfc, 17}))
		switch bad {
		case "owner":
			binary.LittleEndian.PutUint64(directory[96:], originalTag^0x1000)
		case "context":
			binary.LittleEndian.PutUint64(directory[112:], originalCtx+8)
		case "context-zero":
			clear(directory[112:120])
		case "directory-null":
			j.SetFuncRefDesc(0)
			wantPC = pcBase + uint32(bytes.Index(f.body, []byte{0xd2, 1}))
		case "table-null":
			j.SetTablePtr(0)
			wantCode = coreruntime.TrapTableOutOfBounds
		case "bounds-before-owner":
			dest = 1024
			binary.LittleEndian.PutUint64(directory[96:], originalTag^0x1000)
			wantCode = coreruntime.TrapTableOutOfBounds
		}
		input, old := swarTableInputsRV64(args, dest, 1, 1), bytes.Clone(table)
		for i := range results {
			results[i] = 0xa5
		}
		err := e.CallPrepared(entry, args, j.LinMemBase(), trap, results)
		var tr *coreruntime.TrapError
		if !errors.As(err, &tr) || tr.Code != wantCode || binary.LittleEndian.Uint64(trap[16:]) != uint64(wantPC)<<32|1 || !bytes.Equal(args, input) || !bytes.Equal(table, old) || !bytes.Equal(results, bytes.Repeat([]byte{0xa5}, len(results))) {
			t.Fatal("fill owned guards/prewrite", bad, err, wantCode)
		}
		clear(trap)
	}
	binary.LittleEndian.PutUint64(directory[96:], originalTag)
	binary.LittleEndian.PutUint64(directory[112:], originalCtx)
	j.SetFuncRefDesc(thunkPointerRV64(directory))
	j.SetTablePtr(thunkPointerRV64(table))
	swarTableInputsRV64(args, 0, 3, 1)
	if n := testing.AllocsPerRun(50, func() {
		if err := e.CallPrepared(entry, args, j.LinMemBase(), trap, results); err != nil {
			panic(err)
		}
	}); n != 0 {
		t.Fatal("fill reuse/warm allocations", n)
	}
}

func TestSWARRV64BulkTableGrowEscapedDeltaCapacityAndSize(t *testing.T) {
	e, j, args, results, trap, _ := thunkFixtureRV64(t, 256)
	f := swarBulkTableFixture(15)
	entry, table, directory, pcBase := swarBulkTableExecutionRV64(t, j, f)
	for _, delta := range []uint32{0, 1, 2, 1021, 1022, 0xffffffff} {
		for _, nonnull := range []uint32{0, 1, 0xffffffff} {
			binary.LittleEndian.PutUint32(table, 3)
			for i := 0; i < 1024; i++ {
				copy(table[8+i*32:8+(i+1)*32], directory[40:72])
			}
			old, want := bytes.Clone(table), bytes.Clone(table)
			success := delta <= 1021
			oldLength, newLength := uint64(0xffffffff), uint64(3)
			if success {
				oldLength = 3
				newLength += uint64(delta)
				binary.LittleEndian.PutUint32(want, uint32(newLength))
				for i := 3; i < int(newLength); i++ {
					row := want[8+i*32 : 8+(i+1)*32]
					clear(row)
					if nonnull != 0 {
						copy(row, directory[80:112])
					}
				}
			}
			input := swarTableInputsRV64(args, delta, nonnull)
			for i := range results {
				results[i] = 0xa5
			}
			err := e.CallPrepared(entry, args, j.LinMemBase(), trap, results)
			if err != nil || !bytes.Equal(table, want) || !bytes.Equal(args, input) || !bytes.Equal(results[:16], input[:16]) || binary.LittleEndian.Uint64(results[16:]) != oldLength || binary.LittleEndian.Uint64(results[24:]) != newLength || !bytes.Equal(results[32:], bytes.Repeat([]byte{0xa5}, len(results)-32)) {
				t.Fatal("grow escaped16B delta/fullcells/narrow result/size", delta, nonnull, err, results[:32])
			}
			if !success && !bytes.Equal(table, old) {
				t.Fatal("failed grow writes")
			}
		}
	}
	// Capacity failure precedes owner validation and cannot initialize entries.
	tag := binary.LittleEndian.Uint64(directory[96:])
	binary.LittleEndian.PutUint64(directory[96:], tag^0x1000)
	binary.LittleEndian.PutUint32(table, 3)
	swarTableInputsRV64(args, 1022, 1)
	old := bytes.Clone(table)
	if err := e.CallPrepared(entry, args, j.LinMemBase(), trap, results); err != nil || binary.LittleEndian.Uint64(results[16:]) != 0xffffffff || !bytes.Equal(table, old) {
		t.Fatal("grow capacity priority", err)
	}
	swarTableInputsRV64(args, 1, 1)
	for i := range results {
		results[i] = 0xa5
	}
	err := e.CallPrepared(entry, args, j.LinMemBase(), trap, results)
	var tr *coreruntime.TrapError
	pc := pcBase + uint32(bytes.Index(f.body, []byte{0xfc, 15}))
	if !errors.As(err, &tr) || tr.Code != coreruntime.TrapIndirectOutOfBounds || binary.LittleEndian.Uint64(trap[16:]) != uint64(pc)<<32|1 || !bytes.Equal(table, old) || !bytes.Equal(results, bytes.Repeat([]byte{0xa5}, len(results))) {
		t.Fatal("grow owner prewrite/source", err)
	}
	clear(trap)
	binary.LittleEndian.PutUint64(directory[96:], tag)
	if n := testing.AllocsPerRun(50, func() {
		binary.LittleEndian.PutUint32(table, 3)
		if err := e.CallPrepared(entry, args, j.LinMemBase(), trap, results); err != nil {
			panic(err)
		}
	}); n != 0 {
		t.Fatal("grow reuse/warm allocations", n)
	}
	// table.size has its own null-table trap before producing even the live V.
	size := swarBulkTableFixture(16)
	sizeEntry, _, _, sizePC := swarBulkTableExecutionRV64(t, j, size)
	j.SetTablePtr(0)
	for i := range results {
		results[i] = 0xa5
	}
	err = e.CallPrepared(sizeEntry, args, j.LinMemBase(), trap, results)
	if !errors.As(err, &tr) || tr.Code != coreruntime.TrapTableOutOfBounds || binary.LittleEndian.Uint64(trap[16:]) != uint64(sizePC+2)<<32|1 || !bytes.Equal(results, bytes.Repeat([]byte{0xa5}, len(results))) {
		t.Fatal("size null/source/prewrite", err)
	}
	clear(trap)
}

func TestSWARRV64BulkTableActiveFillInterruptCompleteRowsAndReuse(t *testing.T) {
	previous := goruntime.GOMAXPROCS(1)
	defer goruntime.GOMAXPROCS(previous)
	e, j, args, results, trap, _ := thunkFixtureRV64(t, 256)
	f := swarBulkTableFixture(17)
	f.body = []byte{0x20, 0, 0x20, 1, 4, 0x40, 3, 0x40, 0x41, 0, 0xd2, 1, 0x20, 2, 0xfc, 17, 0, 0x0c, 0, 0x0b, 0x0b}
	f.body = sleb(append(f.body, 0x41), 1023)
	f.body = append(f.body, 0xd0, 0x70, 0x26, 0, 0xfc, 16, 0, 0x0b)
	entry, table, directory, pcBase := swarBulkTableExecutionRV64(t, j, f)
	binary.LittleEndian.PutUint32(table, 1024)
	clear(table[8:])
	copy(table[8+1023*32:], directory[40:72])
	input := swarTableInputsRV64(args, 1, 1024, 1)
	for i := range results {
		results[i] = 0xa5
	}
	done := make(chan error, 1)
	go func() { done <- e.CallPrepared(entry, args, j.LinMemBase(), trap, results) }()
	first := (*uint64)(unsafe.Pointer(&table[8]))
	started := false
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if atomic.LoadUint64(first) != 0 {
			started = true
			break
		}
		select {
		case err := <-done:
			t.Fatal("active table fill returned", err)
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
	fillPC, loopPC := pcBase+14, pcBase+6
	if !started || !errors.As(err, &tr) || tr.Code != coreruntime.TrapInterrupted || (source != uint64(fillPC)<<32|1 && source != uint64(loopPC)<<32|1) || !bytes.Equal(results, bytes.Repeat([]byte{0xa5}, len(results))) || !bytes.Equal(args, input) || binary.LittleEndian.Uint32(table) != 1024 || binary.LittleEndian.Uint32(table[4:]) != 1024 {
		t.Fatal("active table progress/interrupt/source/prewrite", started, err, trap[16:24])
	}
	for i := 0; i < 1024; i++ {
		row := table[8+i*32 : 8+(i+1)*32]
		if bytes.Equal(row, directory[80:112]) {
			continue
		}
		if i == 1023 {
			if !bytes.Equal(row, directory[40:72]) {
				t.Fatal("caller continuation or split last row")
			}
		} else if !bytes.Equal(row, make([]byte, 32)) {
			t.Fatal("interrupted partial row", i)
		}
	}
	clear(trap)
	swarTableInputsRV64(args, 0, 1024, 1)
	if err := e.CallPrepared(entry, args, j.LinMemBase(), trap, results); err != nil || !bytes.Equal(results[:16], input[:16]) || binary.LittleEndian.Uint64(results[16:]) != 1024 || !bytes.Equal(table[8+1023*32:], make([]byte, 32)) {
		t.Fatal("table interrupt reuse", err)
	}
	if n := testing.AllocsPerRun(50, func() {
		if err := e.CallPrepared(entry, args, j.LinMemBase(), trap, results); err != nil {
			panic(err)
		}
	}); n != 0 {
		t.Fatal("table interruptible warm allocations", n)
	}
}

func TestSWARRV64BulkTableActiveCopyGrowInterruptAndRetry(t *testing.T) {
	// A ready second P observes the first changed row and requests interruption.
	// Finite512/1021-row operations can finish first; bounded retries distinguish
	// a late request from an actual cold safepoint using its recorded source PC.
	previous := goruntime.GOMAXPROCS(2)
	defer goruntime.GOMAXPROCS(previous)
	for _, sub := range []byte{14, 15} {
		e, j, args, results, trap, _ := thunkFixtureRV64(t, 256)
		f := swarBulkTableFixture(sub)
		entry, table, directory, pcBase := swarBulkTableExecutionRV64(t, j, f)
		pc := pcBase + uint32(bytes.Index(f.body, []byte{0xfc, sub}))
		observed := false
		for attempt := 0; attempt < 8; attempt++ {
			clear(trap)
			for i := 0; i < 1024; i++ {
				copy(table[8+i*32:8+(i+1)*32], directory[40:72])
			}
			watchIndex := 1023
			if sub == 14 {
				binary.LittleEndian.PutUint32(table, 1024)
				for i := 0; i < 512; i++ {
					copy(table[8+i*32:8+(i+1)*32], directory[80:112])
				}
				swarTableInputsRV64(args, 512, 0, 512)
			} else {
				binary.LittleEndian.PutUint32(table, 3)
				swarTableInputsRV64(args, 1021, 1)
				watchIndex = 3
			}
			oldWord := binary.LittleEndian.Uint64(directory[40:])
			word := (*uint64)(unsafe.Pointer(&table[8+watchIndex*32]))
			beforeArgs := bytes.Clone(args)
			for i := range results {
				results[i] = 0xa5
			}
			ready, finished, joined := make(chan struct{}), make(chan struct{}), make(chan struct{})
			go func() {
				defer close(joined)
				close(ready)
				for {
					select {
					case <-finished:
						return
					default:
					}
					if atomic.LoadUint64(word) != oldWord {
						coreruntime.RequestInterrupt(trap)
						return
					}
					goruntime.Gosched()
				}
			}()
			<-ready
			err := e.CallPrepared(entry, args, j.LinMemBase(), trap, results)
			close(finished)
			<-joined // No late publication can race clear or retry.
			var tr *coreruntime.TrapError
			if !errors.As(err, &tr) || tr.Code != coreruntime.TrapInterrupted || binary.LittleEndian.Uint64(trap[16:]) != uint64(pc)<<32|1 {
				// A completed operation may have a late cooperative request without a
				// generated cold trap. Its full length/rows must still be consistent.
				if err != nil && (!errors.As(err, &tr) || tr.Code != coreruntime.TrapInterrupted) {
					t.Fatal("active finite mutation", sub, err)
				}
				if binary.LittleEndian.Uint32(table) != 1024 {
					t.Fatal("late mutation incomplete length", sub)
				}
				start := 512
				if sub == 15 {
					start = 3
				}
				for i := start; i < 1024; i++ {
					if !bytes.Equal(table[8+i*32:8+(i+1)*32], directory[80:112]) {
						t.Fatal("late mutation incomplete row", sub, i)
					}
				}
				continue
			}
			observed = true
			if !bytes.Equal(args, beforeArgs) || !bytes.Equal(results, bytes.Repeat([]byte{0xa5}, len(results))) || binary.LittleEndian.Uint32(table[4:]) != 1024 {
				t.Fatal("active mutation arena/header writes", sub)
			}
			wantLength := uint32(1024)
			start := 512
			untouchedIndex := 512
			if sub == 15 {
				wantLength = 3
				start = 3
				untouchedIndex = 1023
			}
			if binary.LittleEndian.Uint32(table) != wantLength || !bytes.Equal(table[8+watchIndex*32:8+(watchIndex+1)*32], directory[80:112]) || !bytes.Equal(table[8+untouchedIndex*32:8+(untouchedIndex+1)*32], directory[40:72]) {
				t.Fatal("active mutation progress/hidden spare length/unfinished edge", sub)
			}
			for i := start; i < 1024; i++ {
				row := table[8+i*32 : 8+(i+1)*32]
				if !bytes.Equal(row, directory[40:72]) && !bytes.Equal(row, directory[80:112]) {
					t.Fatal("active mutation split32B row", sub, i)
				}
			}
			clear(trap)
			if sub == 15 {
				swarTableInputsRV64(args, 1021, 0)
			} // Null retry overwrites all hidden initialized rows.
			if err := e.CallPrepared(entry, args, j.LinMemBase(), trap, results); err != nil || binary.LittleEndian.Uint32(table) != 1024 || !bytes.Equal(results[:16], beforeArgs[:16]) {
				t.Fatal("active mutation retry", sub, err)
			}
			for i := start; i < 1024; i++ {
				want := directory[80:112]
				if sub == 15 {
					want = make([]byte, 32)
				}
				if !bytes.Equal(table[8+i*32:8+(i+1)*32], want) {
					t.Fatal("retry stale initialized row", sub, i)
				}
			}
			if sub == 15 && (binary.LittleEndian.Uint64(results[16:]) != 3 || binary.LittleEndian.Uint64(results[24:]) != 1024) {
				t.Fatal("retry grow normalization", results[:32])
			}
			break
		}
		if !observed {
			t.Fatal("no genuine active table safepoint reached within bounded retries", sub)
		}
	}
}
