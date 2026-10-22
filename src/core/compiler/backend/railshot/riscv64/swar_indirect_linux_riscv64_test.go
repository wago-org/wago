//go:build linux && riscv64 && !tinygo && !wago_target_tinygo

package riscv64

import (
	"bytes"
	"encoding/binary"
	"errors"
	"math"
	goruntime "runtime"
	"sync/atomic"
	"testing"
	"time"
	"unsafe"

	"github.com/wago-org/wago/src/core/compiler/wasm"
	coreruntime "github.com/wago-org/wago/src/core/runtime"
	"github.com/wago-org/wago/src/core/runtime/abi"
)

// This private provider uses normally validated signatures and immutable checked
// identities. Mappings and owned descriptors live until the invocation joins.
// It does not admit tables or SIMD into compileSWARModule or any public path.
func swarIndirectExecutionFixtureRV64(t *testing.T, j *coreruntime.JobMemory, caller, leaf fixtureFunc, interruptible bool) (uintptr, []byte, []byte) {
	t.Helper()
	source := swarIndirectSource(caller, leaf)
	if pages := len(j.CurrentBytes()) / 65536; pages != 0 {
		source = modifySection(source, 7, 5, uleb([]byte{1, 0}, uint32(pages)), false)
	}
	m, functions, keys := swarValidatedIndirectSource(t, source)
	var memory *wasm.MemType
	if len(m.Memories) != 0 {
		memory = &m.Memories[0]
	}
	var entries [2]uintptr
	for i := range entries {
		code, _, calls, err := compileSWARPairsWithIndirectKeys(&m.Code[i], functions[i], memory, functions, functions, nil, 0, &m.Tables[0].Type, keys, emissionMode{production: true, interruptible: interruptible, functionIndex: uint32(i), entryPC: m.Code[i].LocalDeclBytes})
		if err != nil || len(calls) != 0 {
			t.Fatal("private dynamic image", i, err, calls)
		}
		entries[i] = thunkImageRV64(t, code)
	}
	ar, err := coreruntime.NewArena(8 + 1024*32 + 3*40 + 8)
	if err != nil {
		t.Fatal(err)
	}
	table, directory, context := ar.Alloc(8+1024*32), ar.Alloc(3*40), ar.Alloc(8)
	contextBits := uint64(thunkPointerRV64(context))
	binary.LittleEndian.PutUint32(table, 1024)
	binary.LittleEndian.PutUint32(table[4:], 0x5aa55aa5)
	binary.LittleEndian.PutUint64(directory[32:], contextBits)
	for i, target := range entries {
		desc := directory[(i+1)*40:]
		binary.LittleEndian.PutUint64(desc, uint64(target))
		binary.LittleEndian.PutUint64(desc[8:], keys[i])
		binary.LittleEndian.PutUint64(desc[16:], uint64(j.LinMemBase())|uint64(abi.FuncRefLocalWrapperHomeTag))
		binary.LittleEndian.PutUint64(desc[24:], uint64(thunkPointerRV64(desc)))
		binary.LittleEndian.PutUint64(desc[32:], contextBits)
	}
	j.SetTablePtr(thunkPointerRV64(table))
	j.SetFuncRefDesc(thunkPointerRV64(directory))
	t.Cleanup(func() {
		j.SetTablePtr(0)
		j.SetFuncRefDesc(0)
		if err := ar.Close(); err != nil {
			t.Error(err)
		}
	})
	copy(table[8:40], directory[80:112])
	return entries[0], table, directory
}

func TestSWARRV64IndirectMixedCallsAndGuardPrewrite(t *testing.T) {
	e, j, args, results, trap, _ := thunkFixtureRV64(t, 256)
	caller, leaf := swarIndirectFixtures()
	entry, table, directory := swarIndirectExecutionFixtureRV64(t, j, caller, leaf, false)
	for _, index := range []uint32{0, 1, 1023} {
		clear(table[8:])
		copy(table[8+int(index)*32:], directory[80:112])
		before := bytes.Clone(table)
		for _, pair := range dirtyInputs() {
			input := [9]uint64{pair[0], 0x7ff0000000000001, pair[1], pair[1], pair[0], ^pair[1], ^pair[0], 0x8000000000000000, dirty(index)}
			write := func() {
				for i, v := range input {
					binary.LittleEndian.PutUint64(args[i*8:], v)
				}
			}
			write()
			for i := range results {
				results[i] = 0xa5
			}
			want := [10]uint64{input[5], input[6], input[1] ^ input[5], input[2] ^ input[6], uint64(uint32(input[0])), input[7], input[5], input[6], uint64(uint32(input[3])), input[4]}
			if err := e.CallPrepared(entry, args, j.LinMemBase(), trap, results); err != nil {
				t.Fatal(index, pair, err)
			}
			for i, v := range want {
				if got := binary.LittleEndian.Uint64(results[i*8:]); got != v {
					t.Fatal("indirect raw/narrow/live cell", index, pair, i, got, v)
				}
			}
			if !bytes.Equal(results[80:], bytes.Repeat([]byte{0xa5}, len(results)-80)) || !bytes.Equal(table, before) || binary.LittleEndian.Uint64(args[64:]) != input[8] {
				t.Fatal("indirect tail/table/index writes")
			}
			if allocs := testing.AllocsPerRun(50, func() {
				write()
				if err := e.CallPrepared(entry, args, j.LinMemBase(), trap, results); err != nil {
					panic(err)
				}
			}); allocs != 0 {
				t.Fatal("indirect warm allocations", allocs)
			}
		}
	}
	// Every failed guard preserves even the dirty incoming narrow cells, proving
	// argument normalization/copying did not begin. Signature has owner priority.
	for _, bad := range []string{"bounds", "max-index", "null-code", "signature", "signature-and-owner", "owner", "canonical-null", "context-zero", "context-foreign", "directory-null", "table-null"} {
		clear(table[8:])
		copy(table[8:40], directory[80:112])
		binary.LittleEndian.PutUint64(directory[112:], binary.LittleEndian.Uint64(directory[32:]))
		j.SetTablePtr(thunkPointerRV64(table))
		j.SetFuncRefDesc(thunkPointerRV64(directory))
		index, wantCode := uint32(0), coreruntime.TrapIndirectOutOfBounds
		switch bad {
		case "bounds":
			index = 1024
		case "max-index":
			index = 0xffffffff
		case "null-code":
			clear(table[8:16])
		case "signature", "signature-and-owner":
			binary.LittleEndian.PutUint64(table[16:], binary.LittleEndian.Uint64(table[16:])^1)
			wantCode = coreruntime.TrapIndirectWrongSig
			if bad == "signature-and-owner" {
				clear(table[24:32])
			}
		case "owner":
			binary.LittleEndian.PutUint64(table[24:], binary.LittleEndian.Uint64(table[24:])^0x1000)
		case "canonical-null":
			clear(table[32:40])
		case "context-zero":
			clear(directory[112:120])
		case "context-foreign":
			binary.LittleEndian.PutUint64(directory[112:], binary.LittleEndian.Uint64(directory[32:])+8)
		case "directory-null":
			j.SetFuncRefDesc(0)
		case "table-null":
			j.SetTablePtr(0)
		}
		for i := range args {
			args[i] = byte(i*37+i/8*19) ^ 0x5a
		}
		binary.LittleEndian.PutUint64(args[64:], dirty(index))
		for i := range results {
			results[i] = 0xa5
		}
		oldArgs, oldTable := bytes.Clone(args), bytes.Clone(table)
		err := e.CallPrepared(entry, args, j.LinMemBase(), trap, results)
		var tr *coreruntime.TrapError
		pc := uint32(3 + len(caller.body) - 4) // 3-byte local declaration, call_indirect before end.
		if !errors.As(err, &tr) || tr.Code != wantCode || binary.LittleEndian.Uint64(trap[16:]) != uint64(pc)<<32|1 || !bytes.Equal(args, oldArgs) || !bytes.Equal(table, oldTable) || !bytes.Equal(results, bytes.Repeat([]byte{0xa5}, len(results))) {
			t.Fatal("indirect guard source/prewrite", bad, err, wantCode, trap[16:24])
		}
		clear(trap)
	}
	binary.LittleEndian.PutUint64(directory[112:], binary.LittleEndian.Uint64(directory[32:]))
	j.SetTablePtr(thunkPointerRV64(table))
	j.SetFuncRefDesc(thunkPointerRV64(directory))
	copy(table[8:40], directory[80:112])
	binary.LittleEndian.PutUint64(args[64:], dirty(0))
	if err := e.CallPrepared(entry, args, j.LinMemBase(), trap, results); err != nil {
		t.Fatal("indirect guard reuse", err)
	}
}

func TestSWARRV64IndirectMaximumVectorArenas(t *testing.T) {
	e, j, args, results, trap, _ := thunkFixtureRV64(t, 256)
	params := bytes.Repeat([]byte{0x7b}, 128)
	caller, leaf := fixtureFunc{params: params, results: params}, fixtureFunc{params: params, results: params}
	for i := 0; i < 128; i++ {
		caller.body = append(caller.body, 0x20, byte(i))
		leaf.body = append(leaf.body, 0x20, byte(127-i))
	}
	caller.body = append(caller.body, 0x41, 0, 0x11, 1, 0, 0x0b)
	leaf.body = append(leaf.body, 0x0b)
	entry, _, _ := swarIndirectExecutionFixtureRV64(t, j, caller, leaf, false)
	for i := 0; i < 256; i++ {
		binary.LittleEndian.PutUint64(args[i*8:], 0xfff00000deadbeef^uint64(i)*0x101010101)
	}
	input := bytes.Clone(args)
	run := func() {
		copy(args, input)
		if err := e.CallPrepared(entry, args, j.LinMemBase(), trap, results); err != nil {
			panic(err)
		}
	}
	run()
	for i := 0; i < 128; i++ {
		if !bytes.Equal(results[i*16:i*16+16], input[(127-i)*16:(128-i)*16]) {
			t.Fatal("maximum indirect compact vectors", i)
		}
	}
	if allocs := testing.AllocsPerRun(50, run); allocs != 0 {
		t.Fatal("maximum indirect warm allocations", allocs)
	}
}

func TestSWARRV64IndirectRecursiveStackTrapAndReuse(t *testing.T) {
	e, j, args, results, trap, _ := thunkFixtureRV64(t, 256)
	leaf := swarRecursiveCallFixture()
	leaf.locals = []byte{1, 126, 0x7b}
	leaf.body = bytes.ReplaceAll(leaf.body, []byte{0x10, 0}, []byte{0x41, 0, 0x11, 1, 0})
	caller := fixtureFunc{params: leaf.params, result: leaf.result, body: []byte{0x20, 0, 0x20, 1, 0x41, 0, 0x11, 1, 0, 0x0b}}
	entry, _, _ := swarIndirectExecutionFixtureRV64(t, j, caller, leaf, true)
	vector := []byte{1, 0, 0, 0, 0, 0, 0xf0, 0x7f, 0xef, 0xbe, 0xad, 0xde, 0, 0, 0, 0x80}
	for _, depth := range []uint32{0, 1, 2, 17, 31, 0xffffffff, 1} {
		copy(args, vector)
		binary.LittleEndian.PutUint64(args[16:], dirty(depth))
		for i := range results {
			results[i] = 0xa5
		}
		err := e.CallPrepared(entry, args, j.LinMemBase(), trap, results)
		if depth == 0xffffffff {
			var tr *coreruntime.TrapError
			if !errors.As(err, &tr) || tr.Code != coreruntime.TrapStackFenceBreached || binary.LittleEndian.Uint64(trap[16:]) != uint64(3)<<32|2 || !bytes.Equal(results, bytes.Repeat([]byte{0xa5}, len(results))) {
				t.Fatal("indirect recursive stack/source/prewrite", err, trap[16:24])
			}
			clear(trap)
			continue
		}
		if err != nil || !bytes.Equal(results[:16], vector) || !bytes.Equal(results[16:], bytes.Repeat([]byte{0xa5}, len(results)-16)) {
			t.Fatal("indirect recursion/raw V/reuse", depth, err)
		}
	}
}

func TestSWARRV64IndirectCalleeColdTrapSource(t *testing.T) {
	e, j, args, results, trap, _ := thunkFixtureRV64(t, 256)
	for _, body := range [][]byte{{0, 0x0b}, {0x41, 1, 0x41, 0, 0x6d, 0x1a, 0x20, 0, 0x0b}} {
		caller := fixtureFunc{params: []byte{0x7b}, result: 0x7b, body: []byte{0x20, 0, 0x41, 0, 0x11, 1, 0, 0x0b}}
		leaf := fixtureFunc{params: []byte{0x7b}, result: 0x7b, body: body}
		entry, _, _ := swarIndirectExecutionFixtureRV64(t, j, caller, leaf, false)
		for i := range results {
			results[i] = 0xa5
		}
		err := e.CallPrepared(entry, args, j.LinMemBase(), trap, results)
		code, pc := coreruntime.TrapUnreachable, uint32(1)
		if body[0] != 0 {
			code, pc = coreruntime.TrapDivZero, 5
		}
		var tr *coreruntime.TrapError
		if !errors.As(err, &tr) || tr.Code != code || binary.LittleEndian.Uint64(trap[16:]) != uint64(pc)<<32|2 || !bytes.Equal(results, bytes.Repeat([]byte{0xa5}, len(results))) {
			t.Fatal("indirect callee cold source/prewrite", err, trap[16:24])
		}
		clear(trap)
	}
}

func TestSWARRV64IndirectCalleeLoopActiveInterruptAndReuse(t *testing.T) {
	previous := goruntime.GOMAXPROCS(1)
	defer goruntime.GOMAXPROCS(previous)
	e, j, args, results, trap := swarMemoryExecutionFixture(t)
	leaf := swarIndexedCases()[3].function
	marker := []byte{0x41, 0, 0x20, 0, 0xfd, 11, 0, 0}
	leaf.body = append(append(append([]byte(nil), leaf.body[:8]...), marker...), leaf.body[8:]...)
	caller := fixtureFunc{params: []byte{0x7b, 0x7f}, result: 0x7b, body: []byte{0x20, 0, 0x20, 1, 0x41, 0, 0x11, 1, 0, 0x41, 16, 0x20, 0, 0xfd, 11, 0, 0, 0x0b}}
	entry, _, _ := swarIndirectExecutionFixtureRV64(t, j, caller, leaf, true)
	binary.LittleEndian.PutUint64(args, 1)
	binary.LittleEndian.PutUint64(args[8:], 0x7ff0000000000001)
	binary.LittleEndian.PutUint64(args[16:], dirty(0xffffffff))
	vector := bytes.Clone(args[:16])
	for i := 16; i < 32; i++ {
		j.CurrentBytes()[i] = 0xa5
	}
	for i := range results {
		results[i] = 0xa5
	}
	done := make(chan error, 1)
	go func() { done <- e.CallPrepared(entry, args, j.LinMemBase(), trap, results) }()
	word := (*uint32)(unsafe.Pointer(&j.CurrentBytes()[0]))
	deadline, started := time.Now().Add(2*time.Second), false
	for time.Now().Before(deadline) {
		if atomic.LoadUint32(word) != 0 {
			started = true
			break
		}
		select {
		case err := <-done:
			t.Fatal("callee loop returned before interrupt", err)
		default:
		}
		time.Sleep(100 * time.Microsecond)
	}
	stop := coreruntime.RequestInterruptAsync(trap)
	defer stop()
	err := <-done // Join before any code, memory, trap, or call arena cleanup.
	stop()
	var tr *coreruntime.TrapError
	// Leaf local declarations occupy three bytes; its loop starts at body 4.
	if !started || !errors.As(err, &tr) || tr.Code != coreruntime.TrapInterrupted || binary.LittleEndian.Uint64(trap[16:]) != uint64(7)<<32|2 || !bytes.Equal(results, bytes.Repeat([]byte{0xa5}, len(results))) || !bytes.Equal(j.CurrentBytes()[16:32], bytes.Repeat([]byte{0xa5}, 16)) {
		t.Fatal("nested callee interruption/source/prewrite", started, err, trap[16:24])
	}
	clear(trap)
	copy(args, vector)
	binary.LittleEndian.PutUint64(args[16:], dirty(0))
	if err := e.CallPrepared(entry, args, j.LinMemBase(), trap, results); err != nil || !bytes.Equal(results, vector) || !bytes.Equal(j.CurrentBytes()[16:32], vector) {
		t.Fatal("callee reuse after interrupt", err, results)
	}
	if allocs := testing.AllocsPerRun(50, func() {
		copy(args, vector)
		binary.LittleEndian.PutUint64(args[16:], dirty(0))
		if err := e.CallPrepared(entry, args, j.LinMemBase(), trap, results); err != nil {
			panic(err)
		}
	}); allocs != 0 {
		t.Fatal("callee interruptible warm allocations", allocs)
	}
}

func TestSWARRV64IndirectFloatCalleePreservesLiveRawCarriers(t *testing.T) {
	e, j, args, results, trap, _ := thunkFixtureRV64(t, 256)
	caller := fixtureFunc{params: []byte{0x7b, 0x7c, 0x7d}, results: []byte{0x7b, 0x7c, 0x7d}, body: []byte{0x20, 0, 0x20, 1, 0x20, 2, 0x41, 0, 0x11, 1, 0, 0x0b}}
	leaf := fixtureFunc{params: []byte{0x7d}, result: 0x7d, body: []byte{0x20, 0, 0x20, 0, 0x92, 0x0b}}
	entry, _, _ := swarIndirectExecutionFixtureRV64(t, j, caller, leaf, false)
	for _, bits := range []uint32{0, 0x80000000, 1, 0x80000001, 0x3fc00000, 0x7f7fffff, 0x7f800000, 0xff800000, 0x7f800001, 0xffc12345} {
		input := [4]uint64{0x7ff0000000000001, 0x80000000deadbeef, 0xfff0000000000001, dirty(bits)}
		write := func() {
			for i, value := range input {
				binary.LittleEndian.PutUint64(args[i*8:], value)
			}
		}
		write()
		for i := range results {
			results[i] = 0xa5
		}
		if err := e.CallPrepared(entry, args, j.LinMemBase(), trap, results); err != nil {
			t.Fatal(err)
		}
		for i := 0; i < 3; i++ {
			if binary.LittleEndian.Uint64(results[i*8:]) != input[i] {
				t.Fatal("FP callee clobbered live raw vector/F64", bits, i)
			}
		}
		value := math.Float32frombits(bits)
		want := math.Float32bits(value + value)
		got := binary.LittleEndian.Uint64(results[24:])
		if math.IsNaN(float64(value)) {
			if got>>32 != 0 || uint32(got)&0x7fc00000 != 0x7fc00000 {
				t.Fatal("FP callee quiet NaN/narrow normalization", bits, got)
			}
		} else if got != uint64(want) {
			t.Fatal("FP callee RNE result/narrow normalization", bits, got, want)
		}
		if !bytes.Equal(results[32:], bytes.Repeat([]byte{0xa5}, len(results)-32)) {
			t.Fatal("FP call result tail")
		}
		if allocs := testing.AllocsPerRun(50, func() {
			write()
			if err := e.CallPrepared(entry, args, j.LinMemBase(), trap, results); err != nil {
				panic(err)
			}
		}); allocs != 0 {
			t.Fatal("FP callee warm allocations", allocs)
		}
	}
}
