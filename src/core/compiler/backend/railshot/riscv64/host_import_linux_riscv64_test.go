//go:build linux && riscv64 && !tinygo && !wago_target_tinygo

package riscv64

import (
	"bytes"
	"encoding/binary"
	"fmt"
	goruntime "runtime"
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
	coreruntime "github.com/wago-org/wago/src/core/runtime"
)

type importedCallFixture struct {
	e                                              *coreruntime.Engine
	j                                              *coreruntime.JobMemory
	args, results, trap, ctrl, dispatch, directory []byte
	entry, thunk, context                          uintptr
	count, target                                  int
}

func actualImportedCallRV64(t *testing.T, count, target int, types []byte, nested bool) importedCallFixture {
	return actualImportedModuleRV64(t, hostImportModule(count, target, types, nested), count, target, types)
}

func actualImportedModuleRV64(t *testing.T, data []byte, count, target int, types []byte) importedCallFixture {
	t.Helper()
	e, j, args, results, trap, ctrl := thunkFixtureRV64(t, len(types))
	m, err := wasm.DecodeModule(data)
	if err != nil {
		t.Fatal(err)
	}
	image, err := CompileModuleWith(m, CompileOptions{ImportBindings: hostImportBindings(count), Interruptible: true, DeferCodeMapping: true})
	if err != nil {
		t.Fatal(err)
	}
	entry := thunkImageRV64(t, image.Code) + uintptr(image.Entry[0])
	thunk := thunkImageRV64(t, HostIndirectSyncThunk(uint32(target), len(types), len(types)))
	ar, err := coreruntime.NewArena(count*coreruntime.ImportDispatchEntryBytes + 128)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := ar.Close(); err != nil {
			t.Error(err)
		}
	})
	dispatch := ar.Alloc(count * coreruntime.ImportDispatchEntryBytes)
	directory := ar.Alloc(coreruntime.FuncRefDescBytes)
	context := thunkPointerRV64(ar.Alloc(16))
	f := importedCallFixture{e: e, j: j, args: args, results: results, trap: trap, ctrl: ctrl, dispatch: dispatch, directory: directory, entry: entry, thunk: thunk, context: context, count: count, target: target}
	f.restore()
	return f
}

func (f *importedCallFixture) restore() {
	f.j.SetImportDispatchPtr(thunkPointerRV64(f.dispatch))
	f.j.SetFuncRefDesc(thunkPointerRV64(f.directory))
	binary.LittleEndian.PutUint64(f.directory[coreruntime.FuncRefContextOffset:], uint64(f.context))
	ent := f.dispatch[f.target*coreruntime.ImportDispatchEntryBytes:]
	binary.LittleEndian.PutUint64(ent[coreruntime.ImportDispatchCodePtrOffset:], uint64(f.thunk))
	binary.LittleEndian.PutUint64(ent[coreruntime.ImportDispatchHomeLinMemOffset:], uint64(f.j.LinMemBase()))
	binary.LittleEndian.PutUint64(ent[coreruntime.ImportDispatchTargetContextOffset:], uint64(f.context))
	binary.LittleEndian.PutUint64(ent[coreruntime.ImportDispatchCallerContextOffset:], uint64(f.context))
}

func TestHostImportRV64ActualTypedCallsAndLocalRelocations(t *testing.T) {
	previous := goruntime.GOMAXPROCS(1)
	defer goruntime.GOMAXPROCS(previous)
	for _, count := range []int{1, 65, 256} {
		for _, nested := range []bool{false, true} {
			t.Run(fmt.Sprintf("count%d/nested%t", count, nested), func(t *testing.T) {
				f := actualImportedCallRV64(t, count, count-1, []byte{0x7f, 0x7e, 0x7d, 0x7c}, nested)
				in := []uint64{0xfeedface80000000, 0x8123456789abcdef, 0xa5a5a5a57f800123, 0x7ff0000000000001}
				out := []uint64{0xabcdabcdfffffff7, 0xffedcba987654321, 0xeeeeeeee80000000, 0xfff8000000000123}
				for i, v := range in {
					binary.LittleEndian.PutUint64(f.args[i*8:], v)
				}
				calls := 0
				host := func(p uintptr, index uint32, args, results []uint64) {
					calls++
					if p != thunkPointerRV64(f.ctrl) || index != uint32(count-1) || len(args) != 4 || len(results) != 4 {
						panic("import metadata")
					}
					for i, v := range in {
						if i == 0 || i == 2 {
							v = uint64(uint32(v))
						}
						if args[i] != v {
							panic("dirty typed argument")
						}
						results[i] = out[i]
					}
				}
				if err := f.e.CallWithHostBase(f.entry, f.args, f.j.LinMemBase(), f.trap, f.results, f.ctrl, host); err != nil || calls != 1 {
					t.Fatal(calls, err)
				}
				for i, v := range out {
					if i == 0 || i == 2 {
						v = uint64(uint32(v))
					}
					if got := binary.LittleEndian.Uint64(f.results[i*8:]); got != v {
						t.Fatal("typed result", i, got, v)
					}
				}
				if alloc := testing.AllocsPerRun(50, func() {
					if err := f.e.CallWithHostBase(f.entry, f.args, f.j.LinMemBase(), f.trap, f.results, f.ctrl, host); err != nil {
						panic(err)
					}
				}); alloc != 0 {
					t.Fatal("warm import allocation", alloc)
				}
			})
		}
	}
	//65 slots cross the inline boundary.256 mixed slots put spill homes above
	//2047 while X28 holds the target; resource imports must not alter ordinal255.
	for _, slots := range []int{65, 256} {
		types := make([]byte, slots)
		for i := range types {
			types[i] = []byte{0x7f, 0x7e, 0x7d, 0x7c}[i%4]
		}
		f := actualImportedModuleRV64(t, hostImportResourceModule(256, 255, types, true), 256, 255, types)
		for i := range types {
			binary.LittleEndian.PutUint64(f.args[i*8:], 0xdeadbeef00000000|uint64(i))
		}
		if err := f.e.CallWithHostBase(f.entry, f.args, f.j.LinMemBase(), f.trap, f.results, f.ctrl, func(_ uintptr, index uint32, args, results []uint64) {
			if index != 255 {
				panic("resource/function ordinal")
			}
			for i, v := range args {
				expect := uint64(0xdeadbeef00000000) | uint64(i)
				if i%2 == 0 {
					expect = uint64(uint32(expect))
				}
				if v != expect {
					panic("wide typed arg")
				}
				results[i] = 0xfedcba9800000000 | uint64(i+1)
			}
		}); err != nil {
			t.Fatal(err)
		}
		for i := range types {
			expect := uint64(0xfedcba9800000000) | uint64(i+1)
			if i%2 == 0 {
				expect = uint64(uint32(expect))
			}
			if binary.LittleEndian.Uint64(f.results[i*8:]) != expect {
				t.Fatal("wide typed result", slots, i)
			}
		}
	}
}

func TestHostImportRV64GuardsPrecedeArenaWritesAndKeepExactSource(t *testing.T) {
	f := actualImportedCallRV64(t, 256, 255, []byte{0x7f, 0x7e, 0x7d, 0x7c}, false)
	for _, bad := range []string{"nil-dispatch", "nil-code", "unaligned-code", "foreign-home", "nil-context", "unequal-context", "different-current-context", "zero-current-context", "nil-directory"} {
		t.Run(bad, func(t *testing.T) {
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
				f.args[i] = 0xa5
			}
			for i := range f.results {
				f.results[i] = 0xb6
			}
			beforeArgs, beforeResults := append([]byte(nil), f.args...), append([]byte(nil), f.results...)
			err := f.e.CallWithHostBase(f.entry, f.args, f.j.LinMemBase(), f.trap, f.results, f.ctrl, func(uintptr, uint32, []uint64, []uint64) { t.Fatal("invalid target called host") })
			if err == nil || binary.LittleEndian.Uint32(f.trap) != uint32(coreruntime.TrapCalledFnNotLinked) || binary.LittleEndian.Uint64(f.trap[16:]) != uint64(9)<<32|257 || !bytes.Equal(f.args, beforeArgs) || !bytes.Equal(f.results, beforeResults) {
				t.Fatal("guard/source/arena contract", bad, err, binary.LittleEndian.Uint64(f.trap[16:]))
			}
		})
	}
	f.restore()
	if err := f.e.CallWithHostBase(f.entry, f.args, f.j.LinMemBase(), f.trap, f.results, f.ctrl, func(uintptr, uint32, []uint64, []uint64) {}); err != nil {
		t.Fatal("reuse", err)
	}
}

func TestHostImportRV64ActualVoidCall(t *testing.T) {
	f := actualImportedCallRV64(t, 1, 0, nil, true)
	calls := 0
	if err := f.e.CallWithHostBase(f.entry, f.args, f.j.LinMemBase(), f.trap, f.results, f.ctrl, func(_ uintptr, index uint32, args, results []uint64) {
		if index != 0 || len(args) != 0 || len(results) != 0 {
			panic("void shape")
		}
		calls++
	}); err != nil || calls != 1 {
		t.Fatal(calls, err)
	}
}
