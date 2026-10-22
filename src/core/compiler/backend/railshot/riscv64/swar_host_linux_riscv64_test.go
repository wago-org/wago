//go:build linux && riscv64 && !tinygo && !wago_target_tinygo

package riscv64

import (
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	goruntime "runtime"
	"testing"

	enc "github.com/wago-org/wago/src/core/encoder/riscv64"
	coreruntime "github.com/wago-org/wago/src/core/runtime"
)

// This test provider validates the actual import prefix before emitting every
// body. Dynamic host thunks/dispatch/context and all code/call arenas stay live
// until calls join; it does not change either private module admission policy.
func swarHostExecutionRV64(t *testing.T, count, target, slots int, nested, squareRoot bool) (importedCallFixture, []fixtureFunc, []byte) {
	t.Helper()
	source, fixtures, hostTypes := swarHostSource(count, target, slots, nested)
	if squareRoot {
		if slots != 4 || nested {
			t.Fatal("FP host fixture shape")
		}
		fixtures[0].body = append(fixtures[0].body[:len(fixtures[0].body)-1], 0x9f, 0x0b)
		codePayload := []byte{1}
		body := append(bytes.Clone(fixtures[0].locals), fixtures[0].body...)
		codePayload = append(uleb(codePayload, uint32(len(body))), body...)
		source = modifySection(source, 10, 10, codePayload, true)
	}
	m, types, functions := swarValidatedHostSource(t, source)
	for i := range m.Code {
		if _, err := walkSWARPairsWithImports(&m.Code[i], functions[count+i], false, types, functions, nil, 0, nil, nil, 0, count, nil); err != nil {
			t.Fatal("whole private host profile", i, err)
		}
	}
	var image []byte
	entries := make([]int, len(m.Code))
	var calls []directCall
	for i := range m.Code {
		mode := emissionMode{production: true, interruptible: true, functionIndex: uint32(count + i), entryPC: m.Code[i].LocalDeclBytes}
		code, _, relocations, err := compileSWARPairsWithImports(&m.Code[i], functions[count+i], nil, types, functions, nil, 0, nil, nil, 0, count, mode)
		if err != nil {
			t.Fatal(err)
		}
		for len(image)&15 != 0 {
			image = append(image, 0x13, 0, 0, 0)
		}
		entries[i] = len(image)
		for j := range relocations {
			relocations[j].site += entries[i]
		}
		calls = append(calls, relocations...)
		image = append(image, code...)
	}
	a := enc.Asm{B: image}
	for _, call := range calls {
		if err := a.PatchLong(call.site, int64(entries[call.callee])); err != nil {
			t.Fatal(err)
		}
	}
	e, job, args, results, trap, ctrl := thunkFixtureRV64(t, slots)
	entry := thunkImageRV64(t, image)
	thunk := thunkImageRV64(t, HostIndirectSyncThunk(uint32(target), slots, slots))
	arena, err := coreruntime.NewArena(count*coreruntime.ImportDispatchEntryBytes + 128)
	if err != nil {
		t.Fatal(err)
	}
	context := thunkPointerRV64(arena.Alloc(16))
	f := importedCallFixture{e: e, j: job, args: args, results: results, trap: trap, ctrl: ctrl, dispatch: arena.Alloc(count * coreruntime.ImportDispatchEntryBytes), directory: arena.Alloc(coreruntime.FuncRefDescBytes), entry: entry, thunk: thunk, context: context, count: count, target: target}
	f.restore()
	t.Cleanup(func() {
		job.SetImportDispatchPtr(0)
		job.SetFuncRefDesc(0)
		if err := arena.Close(); err != nil {
			t.Error(err)
		}
	})
	return f, fixtures, hostTypes
}

func TestSWARRV64ScalarHostRawLiveCarriersWideSlotsAndNestedCalls(t *testing.T) {
	previous := goruntime.GOMAXPROCS(1)
	defer goruntime.GOMAXPROCS(previous)
	for _, count := range []int{1, 65, 256} {
		for _, slots := range []int{0, 4, 65, 126} {
			for _, nested := range []bool{false, true} {
				if slots == 126 && nested {
					continue // Maximum128 logical results leave no second caller prefix.
				}
				t.Run(fmt.Sprintf("imports%d/slots%d/nested%t", count, slots, nested), func(t *testing.T) {
					f, _, types := swarHostExecutionRV64(t, count, count-1, slots, nested, false)
					input := [3]uint64{0x7ff0000000000001, 0x80000000deadbeef, 0xfff0000000000001}
					write := func() {
						for i, value := range input {
							binary.LittleEndian.PutUint64(f.args[i*8:], value)
						}
						for i := range types {
							binary.LittleEndian.PutUint64(f.args[24+i*8:], 0xdeadbeef80000000|uint64(i))
						}
						for i := range f.results {
							f.results[i] = 0xa5
						}
					}
					calls := 0
					callback := func(context uintptr, index uint32, args, results []uint64) {
						if context != thunkPointerRV64(f.ctrl) || index != uint32(count-1) || len(args) != slots || len(results) != slots {
							panic("mixed host metadata")
						}
						if calls == 0 {
							goruntime.GC() // true parked guest frame with live raw V/F64
						}
						calls++
						for i, value := range args {
							want := uint64(0xdeadbeef80000000) | uint64(i)
							if types[i] == 0x7f || types[i] == 0x7d {
								want = uint64(uint32(want))
							}
							if value != want || results[i] != 0 {
								panic("mixed host raw/narrow argument or stale result")
							}
							results[i] = 0xfedcba9880000000 | uint64(i+1)
						}
					}
					write()
					oldTail := bytes.Clone(f.args[(3+slots)*8:])
					if err := f.e.CallWithHostBase(f.entry, f.args, f.j.LinMemBase(), f.trap, f.results, f.ctrl, callback); err != nil || calls != 1 {
						t.Fatal("mixed parked host", calls, err)
					}
					prefix := 3
					if nested {
						prefix = 6
					}
					for i := 0; i < prefix; i++ {
						if got := binary.LittleEndian.Uint64(f.results[i*8:]); got != input[i%3] {
							t.Fatal("raw carrier live across native/parked frames", i, got, input[i%3])
						}
					}
					for i := range types {
						want := uint64(0xfedcba9880000000) | uint64(i+1)
						if types[i] == 0x7f || types[i] == 0x7d {
							want = uint64(uint32(want))
						}
						if got := binary.LittleEndian.Uint64(f.results[(prefix+i)*8:]); got != want {
							t.Fatal("typed host result", i, got, want)
						}
					}
					if !bytes.Equal(f.results[(prefix+slots)*8:], bytes.Repeat([]byte{0xa5}, len(f.results)-(prefix+slots)*8)) || !bytes.Equal(f.args[(3+slots)*8:], oldTail) {
						t.Fatal("mixed host call arena tails")
					}
					if n := testing.AllocsPerRun(50, func() {
						write()
						if err := f.e.CallWithHostBase(f.entry, f.args, f.j.LinMemBase(), f.trap, f.results, f.ctrl, callback); err != nil {
							panic(err)
						}
					}); n != 0 {
						t.Fatal("mixed host warm allocations", n)
					}
				})
			}
		}
	}
}

func TestSWARRV64ScalarHostGuardsPrecedeCompactWritesAndReuse(t *testing.T) {
	f, fixtures, _ := swarHostExecutionRV64(t, 256, 255, 65, false, false)
	for _, bad := range []string{"nil-dispatch", "nil-code", "unaligned-code", "foreign-home", "nil-context", "unequal-context", "different-current-context", "zero-current-context", "nil-directory"} {
		f.restore()
		ent := f.dispatch[255*coreruntime.ImportDispatchEntryBytes:]
		switch bad {
		case "nil-dispatch":
			f.j.SetImportDispatchPtr(0)
		case "nil-code":
			binary.LittleEndian.PutUint64(ent, 0)
		case "unaligned-code":
			binary.LittleEndian.PutUint64(ent, uint64(f.thunk+2))
		case "foreign-home":
			binary.LittleEndian.PutUint64(ent[8:], uint64(f.j.LinMemBase()+8))
		case "nil-context":
			binary.LittleEndian.PutUint64(ent[16:], 0)
		case "unequal-context":
			binary.LittleEndian.PutUint64(ent[24:], uint64(f.context+8))
		case "different-current-context":
			binary.LittleEndian.PutUint64(f.directory[32:], uint64(f.context+8))
		case "zero-current-context":
			binary.LittleEndian.PutUint64(f.directory[32:], 0)
		case "nil-directory":
			f.j.SetFuncRefDesc(0)
		}
		for i := range f.args {
			f.args[i] = byte(i*37+i/8*19) ^ 0x5a
		}
		for i := range f.results {
			f.results[i] = 0xa5
		}
		beforeArgs, beforeResults := bytes.Clone(f.args), bytes.Clone(f.results)
		err := f.e.CallWithHostBase(f.entry, f.args, f.j.LinMemBase(), f.trap, f.results, f.ctrl, func(uintptr, uint32, []uint64, []uint64) { panic("invalid host target called") })
		var tr *coreruntime.TrapError
		pc := uint32(len(fixtures[0].locals) + bytes.Index(fixtures[0].body, []byte{0x10, 0xff, 1}))
		if !errors.As(err, &tr) || tr.Code != coreruntime.TrapCalledFnNotLinked || binary.LittleEndian.Uint64(f.trap[16:]) != uint64(pc)<<32|257 || !bytes.Equal(f.args, beforeArgs) || !bytes.Equal(f.results, beforeResults) {
			t.Fatal("mixed host guard source/prewrite", bad, err)
		}
		clear(f.trap)
	}
	f.restore()
	if err := f.e.CallWithHostBase(f.entry, f.args, f.j.LinMemBase(), f.trap, f.results, f.ctrl, func(uintptr, uint32, []uint64, []uint64) {}); err != nil {
		t.Fatal("mixed host guard reuse", err)
	}
}

func TestSWARRV64ScalarHostGoNestedPanicCancelFPAndReuse(t *testing.T) {
	f, _, _ := swarHostExecutionRV64(t, 256, 255, 4, false, true)
	inner, _, _ := swarHostExecutionRV64(t, 1, 0, 0, true, false)
	input := [7]uint64{0x7ff0000000000001, 0x80000000deadbeef, 0xfff0000000000001, 0xdeadbeef80000000, 0x8123456789abcdef, 0xa5a5a5a57f800123, 0x7ff0000000000001}
	write := func() {
		for i, value := range input {
			binary.LittleEndian.PutUint64(f.args[i*8:], value)
		}
		for i := range f.results {
			f.results[i] = 0xa5
		}
	}
	callback := func(_ uintptr, _ uint32, args, results []uint64) {
		goruntime.GC()
		innerInput := [3]uint64{0xfff0000000000001, 0x7fa1234500000001, 0x8000000000000000}
		for i, value := range innerInput {
			binary.LittleEndian.PutUint64(inner.args[i*8:], value)
		}
		if err := inner.e.CallWithHostBase(inner.entry, inner.args, inner.j.LinMemBase(), inner.trap, inner.results, inner.ctrl, func(uintptr, uint32, []uint64, []uint64) { goruntime.GC() }); err != nil {
			panic(err)
		}
		for i := range 6 {
			if binary.LittleEndian.Uint64(inner.results[i*8:]) != innerInput[i%3] {
				panic("nested parked guest raw carriers")
			}
		}
		results[0], results[1], results[2], results[3] = 0xffffffff80000001, args[1]^1, 0xeeeeeeee80000000, 0x4010000000000000 // sqrt(4)=2 after host resume
	}
	check := func() {
		write()
		if err := f.e.CallWithHostBase(f.entry, f.args, f.j.LinMemBase(), f.trap, f.results, f.ctrl, callback); err != nil || !bytes.Equal(f.results[:24], binary.LittleEndian.AppendUint64(binary.LittleEndian.AppendUint64(binary.LittleEndian.AppendUint64(nil, input[0]), input[1]), input[2])) || binary.LittleEndian.Uint64(f.results[24:]) != 0x80000001 || binary.LittleEndian.Uint64(f.results[32:]) != input[4]^1 || binary.LittleEndian.Uint64(f.results[40:]) != 0x80000000 || binary.LittleEndian.Uint64(f.results[48:]) != 0x4000000000000000 || !bytes.Equal(f.results[56:], bytes.Repeat([]byte{0xa5}, len(f.results)-56)) {
			t.Fatal("parked mixed FP/nested guest/raw live/tails", err, f.results[:56])
		}
	}
	check()
	marker := new(int)
	write()
	func() {
		defer func() {
			if recover() != marker {
				t.Error("mixed host panic identity lost")
			}
		}()
		_ = f.e.CallWithHostBase(f.entry, f.args, f.j.LinMemBase(), f.trap, f.results, f.ctrl, func(uintptr, uint32, []uint64, []uint64) { panic(marker) })
	}()
	if !bytes.Equal(f.results, bytes.Repeat([]byte{0xa5}, len(f.results))) {
		t.Fatal("panicked host resumed mixed guest/results")
	}
	check()
	write()
	calls := 0
	err := f.e.CallWithHostBase(f.entry, f.args, f.j.LinMemBase(), f.trap, f.results, f.ctrl, func(uintptr, uint32, []uint64, []uint64) { calls++; coreruntime.RequestInterrupt(f.trap) })
	var tr *coreruntime.TrapError
	if !errors.As(err, &tr) || tr.Code != coreruntime.TrapInterrupted || calls != 1 || !bytes.Equal(f.results, bytes.Repeat([]byte{0xa5}, len(f.results))) {
		t.Fatal("cancelled host resumed mixed guest/results", calls, err)
	}
	clear(f.trap)
	check()
}
