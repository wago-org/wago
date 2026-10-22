//go:build linux && riscv64 && !tinygo && !wago_target_tinygo

package riscv64

import (
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	goruntime "runtime"
	"testing"
	"unsafe"

	"github.com/wago-org/wago/src/core/compiler/wasm"
	enc "github.com/wago-org/wago/src/core/encoder/riscv64"
	coreruntime "github.com/wago-org/wago/src/core/runtime"
	"github.com/wago-org/wago/src/core/runtime/abi"
)

// Everything consumed by generated code is retained mmap storage. These tests
// call the private bridge directly; they do not admit foreign public imports.
type nativeImportFixtureRV64 struct {
	e                                                              *coreruntime.Engine
	caller, target                                                 *coreruntime.JobMemory
	args, results, trap, ctrl, targetCtrl                          []byte
	callerContext, targetContext, callerDirectory, targetDirectory []byte
	targetArguments, targetResults, witness                        []byte
}

func newNativeImportFixtureRV64(t *testing.T, sharedHome bool) *nativeImportFixtureRV64 {
	t.Helper()
	f := new(nativeImportFixtureRV64)
	f.e, f.caller, f.args, f.results, f.trap, f.ctrl = thunkFixtureRV64(t, 256)
	f.target = f.caller
	if !sharedHome {
		var err error
		f.target, err = coreruntime.NewJobMemory(65536)
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() {
			if err := f.target.Close(); err != nil {
				t.Error(err)
			}
		})
	}
	ar, err := coreruntime.NewArena(32768)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := ar.Close(); err != nil {
			t.Error(err)
		}
	})
	f.callerContext, f.targetContext = ar.Alloc(coreruntime.InstanceContextBytes), ar.Alloc(coreruntime.InstanceContextBytes)
	f.callerDirectory, f.targetDirectory = ar.Alloc(40), ar.Alloc(40)
	ctrlBytes, err := coreruntime.HostCtrlFrameBytesForSlots(256)
	if err != nil {
		t.Fatal(err)
	}
	f.targetCtrl = ar.Alloc(ctrlBytes)
	if err := coreruntime.InitHostCtrlFrame(f.targetCtrl); err != nil {
		t.Fatal(err)
	}
	if err := coreruntime.RegisterHostCtrlFrame(f.targetCtrl); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { coreruntime.UnregisterHostCtrlFrame(f.targetCtrl) })
	f.targetArguments, f.targetResults = ar.Alloc(nativeImportTransportBytes), ar.Alloc(nativeImportTransportBytes)
	f.witness = ar.Alloc(128)
	for _, item := range []struct{ ctx, directory []byte }{{f.callerContext, f.callerDirectory}, {f.targetContext, f.targetDirectory}} {
		// Populate the runtime's serialized layout independently of the emitter's
		// selected field list, so omitting a field cannot weaken its own test.
		for offset := 0; offset < coreruntime.InstanceContextGCDomainOffset; offset += 8 {
			binary.LittleEndian.PutUint64(item.ctx[offset:], uint64(thunkPointerRV64(ar.Alloc(8))))
		}
		binary.LittleEndian.PutUint64(item.ctx[coreruntime.InstanceContextGCNativeViewOffset:], uint64(thunkPointerRV64(ar.Alloc(8))))
		binary.LittleEndian.PutUint64(item.ctx[coreruntime.InstanceContextEHTagDirOffset:], uint64(thunkPointerRV64(ar.Alloc(8))))
		// Immutable GC-domain and tail scratch metadata is never copied to home.
		for offset := coreruntime.InstanceContextGCDomainOffset; offset < coreruntime.InstanceContextGCNativeViewOffset; offset += 8 {
			binary.LittleEndian.PutUint64(item.ctx[offset:], 0xfeed000000000000|uint64(offset))
		}
		binary.LittleEndian.PutUint64(item.ctx[16:], uint64(thunkPointerRV64(item.directory)))
		binary.LittleEndian.PutUint64(item.directory[coreruntime.FuncRefContextOffset:], uint64(thunkPointerRV64(item.ctx)))
	}
	binary.LittleEndian.PutUint64(f.targetContext, uint64(thunkPointerRV64(f.targetCtrl)))
	f.bindCaller(t)
	return f
}

func (f *nativeImportFixtureRV64) bindCaller(t *testing.T) {
	t.Helper()
	f.caller.BindInstanceContextBytes(f.callerContext)
	// The live invocation control frame intentionally differs from canonical ctx.
	f.caller.SetCustomCtx(thunkPointerRV64(f.ctrl))
	f.caller.SetStackFence(f.e.StackLimit())
	if err := f.caller.RebindTrapCell(f.trap); err != nil {
		t.Fatal(err)
	}
	clear(f.trap)
}

func (f *nativeImportFixtureRV64) thunk(t *testing.T, entry uintptr, arguments, results int) uintptr {
	t.Helper()
	code, err := nativeImportThunk(nativeImportTarget{
		entry: entry, home: f.target.LinMemBase(), context: thunkPointerRV64(f.targetContext),
		arguments: f.targetArguments, results: f.targetResults, argumentSlots: arguments, resultSlots: results,
	}, f.caller.LinMemBase(), thunkPointerRV64(f.callerContext), 7)
	if err != nil {
		t.Fatal(err)
	}
	return thunkImageRV64(t, code)
}

func nativeImportWordRV64(home uintptr, offset int32) uint64 {
	address := home - uintptr(offset)
	pointer := *(*unsafe.Pointer)(unsafe.Pointer(&address))
	return *(*uint64)(pointer)
}

func (f *nativeImportFixtureRV64) assertCaller(t *testing.T) {
	t.Helper()
	word := func(offset int) uintptr { return uintptr(binary.LittleEndian.Uint64(f.callerContext[offset:])) }
	want := coreruntime.InstanceContext{
		CustomCtx: thunkPointerRV64(f.ctrl), TablePtr: word(8), FuncRefDescPtr: word(16),
		PassiveElemPtr: word(24), GlobalsPtr: word(32), PassiveDataPtr: word(40),
		TableDirPtr: word(48), MemoryDirPtr: word(56), ImportDispatch: word(64),
		EHTagDirPtr: word(coreruntime.InstanceContextEHTagDirOffset),
	}
	if got := f.caller.CaptureInstanceContext(); got != want {
		t.Fatal("caller runtime context restoration", got, want)
	}
	if got := nativeImportWordRV64(f.caller.LinMemBase(), abi.GCNativeViewPtrOffset); got != uint64(word(coreruntime.InstanceContextGCNativeViewOffset)) {
		t.Fatal("caller native GC view restoration", got)
	}
	if f.caller.CurrentPages() != 0 || f.caller.MaxPages() != 0 || f.target != f.caller && (f.target.CurrentPages() != 1 || f.target.MaxPages() != 1) {
		t.Fatal("context switch changed memory sizing")
	}
}

func nativeImportModuleRV64(t *testing.T, functions ...fixtureFunc) []uintptr {
	t.Helper()
	m, err := wasm.DecodeModule(binaryModule(functions...))
	if err != nil {
		t.Fatal(err)
	}
	image, err := CompileModuleWith(m, CompileOptions{DeferCodeMapping: true, Interruptible: true})
	if err != nil {
		t.Fatal(err)
	}
	base := thunkImageRV64(t, image.Code)
	entries := make([]uintptr, len(image.Entry))
	for i, offset := range image.Entry {
		entries[i] = base + uintptr(offset)
	}
	return entries
}

func TestPrivateNativeImportRV64CompleteRuntimeContextSelection(t *testing.T) {
	for _, shared := range []bool{false, true} {
		t.Run(fmt.Sprint(shared), func(t *testing.T) {
			f := newNativeImportFixtureRV64(t, shared)
			// An independent ABI observer records all runtime pointer destinations
			// while the callee is active. It has zero stack descent and no calls.
			var observer enc.Asm
			for i, offset := range []int32{40, 80, abi.FuncRefDescPtrOffset, abi.PassiveElemPtrOffset, abi.GlobalsPtrOffset, abi.PassiveDataPtrOffset, abi.TableDirPtrOffset, abi.MemoryDirPtrOffset, abi.ImportDispatchPtrOffset, abi.GCNativeViewPtrOffset, abi.EHTagDirPtrOffset} {
				observer.Ld(enc.X5, enc.X25, -offset)
				observer.Sd(enc.X5, enc.X13, int32(i*8))
			}
			observer.Ret()
			if err := observer.Err(); err != nil {
				t.Fatal(err)
			}
			entry := f.thunk(t, thunkImageRV64(t, observer.B), 0, 11)
			if err := f.e.CallPrepared(entry, nil, f.caller.LinMemBase(), f.trap, f.results[:11*8]); err != nil {
				t.Fatal(err)
			}
			for i := 0; i < 11; i++ {
				sourceOffset := i * 8
				if i == 9 {
					sourceOffset = coreruntime.InstanceContextGCNativeViewOffset
				}
				if i == 10 {
					sourceOffset = coreruntime.InstanceContextEHTagDirOffset
				}
				if got, want := binary.LittleEndian.Uint64(f.results[i*8:]), binary.LittleEndian.Uint64(f.targetContext[sourceOffset:]); got != want {
					t.Fatal("active target runtime context", i, got, want)
				}
			}
			f.assertCaller(t)
		})
	}
}

func TestPrivateNativeImportRV64TypedTransportAndContextReturn(t *testing.T) {
	for _, shared := range []bool{false, true} {
		for _, vector := range []bool{false, true} {
			t.Run(fmt.Sprintf("shared=%t/vector=%t", shared, vector), func(t *testing.T) {
				f := newNativeImportFixtureRV64(t, shared)
				params := make([]byte, 128)
				for i := range params {
					params[i] = []byte{0x7f, 0x7e, 0x7d, 0x7c, 0x7b}[i%5]
					if vector {
						params[i] = 0x7b
					}
				}
				leaf := fixtureFunc{params: params, results: bytes.Clone(params)}
				// Reverse the actual typed signature, including complete vector pairs.
				for i := range params {
					leaf.results[i] = params[len(params)-1-i]
				}
				for i := len(params) - 1; i >= 0; i-- {
					leaf.body = uleb(append(leaf.body, 0x20), uint32(i))
				}
				leaf.body = append(leaf.body, 0x0b)
				slots := 0
				var want []uint64
				values := make([][]uint64, len(params))
				for i, kind := range params {
					v := uint64(0xfff00000deadbeef) ^ uint64(i)*0x101010101
					binary.LittleEndian.PutUint64(f.args[slots*8:], v)
					values[i] = []uint64{v}
					slots++
					if kind == 0x7f || kind == 0x7d {
						values[i][0] = uint64(uint32(v))
					}
					if kind == 0x7b {
						binary.LittleEndian.PutUint64(f.args[slots*8:], ^v)
						values[i] = append(values[i], ^v)
						slots++
					}
				}
				for i := len(values) - 1; i >= 0; i-- {
					want = append(want, values[i]...)
				}
				beforeArgs := bytes.Clone(f.args)
				beforeCaller, beforeTarget := bytes.Clone(f.callerContext), bytes.Clone(f.targetContext)
				entry := f.thunk(t, nativeImportModuleRV64(t, leaf)[0], slots, slots)
				var root enc.Asm
				root.Addi(enc.SP, enc.SP, -16)
				root.Sd(enc.RA, enc.SP, 0)
				root.Constant64(enc.X24, 0x123456789abcdef)
				root.Constant64(enc.X28, int64(entry))
				root.Jalr(enc.RA, enc.X28, 0)
				root.Constant64(enc.X28, int64(thunkPointerRV64(f.witness)))
				for i, reg := range []enc.Reg{enc.X10, enc.X11, enc.X12, enc.X13, enc.X24, enc.X25} {
					root.Sd(reg, enc.X28, int32(i*8))
				}
				root.Ld(enc.RA, enc.SP, 0)
				root.Addi(enc.SP, enc.SP, 16)
				root.Ret()
				if err := root.Err(); err != nil {
					t.Fatal(err)
				}
				entry = thunkImageRV64(t, root.B)
				for run := 0; run < 3; run++ {
					for i := range f.results {
						f.results[i] = 0xa5
					}
					if err := f.e.CallPrepared(entry, f.args[:slots*8], f.caller.LinMemBase(), f.trap, f.results[:slots*8]); err != nil {
						t.Fatal(err)
					}
					for i, expected := range want {
						if got := binary.LittleEndian.Uint64(f.results[i*8:]); got != expected {
							t.Fatal("typed carrier", i, got, expected)
						}
					}
					if !bytes.Equal(f.args, beforeArgs) || !bytes.Equal(f.results[slots*8:], bytes.Repeat([]byte{0xa5}, len(f.results)-slots*8)) || !bytes.Equal(f.callerContext, beforeCaller) || !bytes.Equal(f.targetContext, beforeTarget) {
						t.Fatal("caller transport tail/immutable context write")
					}
					f.assertCaller(t)
					for i, expected := range []uint64{uint64(thunkPointerRV64(f.args)), uint64(f.caller.LinMemBase()), uint64(thunkPointerRV64(f.trap)), uint64(thunkPointerRV64(f.results)), 0x123456789abcdef, uint64(f.caller.LinMemBase())} {
						if got := binary.LittleEndian.Uint64(f.witness[i*8:]); got != expected {
							t.Fatal("transport/reserved register restoration", i, got, expected)
						}
					}
				}
				if allocations := testing.AllocsPerRun(10, func() {
					if err := f.e.CallPrepared(entry, f.args[:slots*8], f.caller.LinMemBase(), f.trap, f.results[:slots*8]); err != nil {
						panic(err)
					}
				}); allocations != 0 {
					t.Fatal("warm native bridge allocations", allocations)
				}
			})
		}
	}
}

func TestPrivateNativeImportRV64NarrowExportWideInternalTransport(t *testing.T) {
	for _, shared := range []bool{false, true} {
		t.Run(fmt.Sprint(shared), func(t *testing.T) {
			f := newNativeImportFixtureRV64(t, shared)
			wide := fixtureFunc{params: bytes.Repeat([]byte{0x7b}, 128), results: bytes.Repeat([]byte{0x7b}, 128)}
			caller := fixtureFunc{result: 0x7f}
			for i := 0; i < 128; i++ {
				caller.body = append(caller.body, swarLiteral(uint64(i), ^uint64(i))...)
				wide.body = uleb(append(wide.body, 0x20), uint32(127-i))
			}
			wide.body = append(wide.body, 0x0b)
			caller.body = append(caller.body, 0x10, 1)
			for i := 0; i < 128; i++ {
				caller.body = append(caller.body, 0x1a)
			}
			caller.body = append(caller.body, 0x41, 17, 0x0b)
			entry := f.thunk(t, nativeImportModuleRV64(t, caller, wide)[0], 0, 1)
			for i := range f.args {
				f.args[i] = 0x5a
			}
			for i := range f.results {
				f.results[i] = 0xa5
			}
			before := bytes.Clone(f.args)
			if err := f.e.CallPrepared(entry, nil, f.caller.LinMemBase(), f.trap, f.results[:8]); err != nil {
				t.Fatal(err)
			}
			if binary.LittleEndian.Uint64(f.results) != 17 || !bytes.Equal(f.args, before) || !bytes.Equal(f.results[8:], bytes.Repeat([]byte{0xa5}, len(f.results)-8)) {
				t.Fatal("wide internal call touched narrow caller arenas")
			}
			// The final cells prove the nested function really used the full arena.
			if binary.LittleEndian.Uint64(f.targetArguments[255*8:]) != ^uint64(127) || binary.LittleEndian.Uint64(f.targetResults[255*8:]) != ^uint64(0) {
				t.Fatal("wide internal arena not exercised")
			}
			f.assertCaller(t)
		})
	}
}

func TestPrivateNativeImportRV64IdentityAndFramePrewrite(t *testing.T) {
	for _, stackOwned := range []bool{false, true} {
		frameBytes := nativeImportFrameBytes
		if stackOwned {
			frameBytes += 16 + HostFixedFrameBytes + 2*nativeImportTransportBytes
		}
		for _, shared := range []bool{false, true} {
			for _, mode := range []string{"exact", "short", "reversed", "home", "transport", "anchor", "sibling"} {
				t.Run(fmt.Sprintf("stack=%t/shared=%t/%s", stackOwned, shared, mode), func(t *testing.T) {
					f := newNativeImportFixtureRV64(t, shared)
					// A zero-descent ABI probe isolates the complete selected bridge frame.
					var leaf enc.Asm
					leaf.Ret()
					target := nativeImportTarget{entry: thunkImageRV64(t, leaf.B), home: f.target.LinMemBase(), context: thunkPointerRV64(f.targetContext), arguments: f.targetArguments, results: f.targetResults, stackOwnedActivation: stackOwned, hostTrampoline: uintptr(binary.LittleEndian.Uint64(f.targetCtrl[HostTrampolineOffset:]))}
					callerHome := f.caller.LinMemBase()
					if mode == "home" {
						callerHome += 8
					}
					code, err := nativeImportThunk(target, callerHome, thunkPointerRV64(f.callerContext), 7)
					if err != nil {
						t.Fatal(err)
					}
					entry := thunkImageRV64(t, code)
					if mode == "anchor" {
						f.caller.SetFuncRefDesc(0)
					}
					if mode == "sibling" {
						binary.LittleEndian.PutUint64(f.callerDirectory[32:], uint64(thunkPointerRV64(f.targetContext)))
					}
					var root enc.Asm
					root.Addi(enc.SP, enc.SP, -16)
					root.Sd(enc.RA, enc.SP, 0)
					root.Constant64(enc.X28, int64(thunkPointerRV64(f.witness)))
					// Fill and snapshot every prospective thunk frame word beforehand.
					root.Constant64(enc.X29, -6510615555426900571) // 0xa5a5a5a5a5a5a5a5
					root.Constant64(enc.X5, int64(frameBytes))
					root.Sub(enc.X5, enc.SP, enc.X5)
					for i := 0; i < frameBytes; i += 8 {
						root.Sd(enc.X29, enc.X5, 0)
						root.Addi(enc.X5, enc.X5, 8)
					}
					available := frameBytes
					if mode == "short" {
						available--
					}
					if mode == "reversed" {
						available = -1
					}
					if mode == "exact" || mode == "short" || mode == "reversed" {
						root.Constant64(enc.X29, int64(available))
						root.Sub(enc.X29, enc.SP, enc.X29)
						root.Sd(enc.X29, enc.X25, -abi.StackFenceOffset)
					}
					root.Constant64(enc.X29, int64(frameBytes))
					root.Sub(enc.X29, enc.SP, enc.X29)
					root.Sd(enc.X29, enc.X28, 0)
					if mode == "transport" {
						root.Addi(enc.X11, enc.X25, 8)
					}
					root.Constant64(enc.X28, int64(entry))
					root.Jalr(enc.RA, enc.X28, 0)
					root.Ld(enc.RA, enc.SP, 0)
					root.Addi(enc.SP, enc.SP, 16)
					root.Ret()
					if err := root.Err(); err != nil {
						t.Fatal(err)
					}
					for i := range f.results {
						f.results[i] = 0xa5
					}
					for i := range f.targetArguments {
						f.targetArguments[i] = 0x5a
						f.targetResults[i] = 0x5a
					}
					beforeTarget := f.target.CaptureInstanceContext()
					err = f.e.CallPrepared(thunkImageRV64(t, root.B), nil, f.caller.LinMemBase(), f.trap, nil)
					if mode == "exact" {
						if err != nil {
							t.Fatal("exact frame rejected", err)
						}
						f.assertCaller(t)
					} else {
						var tr *coreruntime.TrapError
						want := coreruntime.TrapCalledFnNotLinked
						if mode == "short" || mode == "reversed" {
							want = coreruntime.TrapStackFenceBreached
						}
						if !errors.As(err, &tr) || tr.Code != want || len(tr.Frames) != 1 || tr.Frames[0].FunctionIndex != 7 || tr.Frames[0].HasProgramCounter {
							t.Fatal("guard source", err)
						}
						addr := uintptr(binary.LittleEndian.Uint64(f.witness))
						ptr := *(*unsafe.Pointer)(unsafe.Pointer(&addr))
						frame := unsafe.Slice((*byte)(ptr), frameBytes)
						if !bytes.Equal(frame, bytes.Repeat([]byte{0xa5}, frameBytes)) || f.target.CaptureInstanceContext() != beforeTarget || !bytes.Equal(f.targetArguments, bytes.Repeat([]byte{0x5a}, nativeImportTransportBytes)) || !bytes.Equal(f.targetResults, bytes.Repeat([]byte{0x5a}, nativeImportTransportBytes)) || !bytes.Equal(f.results, bytes.Repeat([]byte{0xa5}, len(f.results))) {
							t.Fatal("guard wrote frame/context/transport")
						}
					}
					binary.LittleEndian.PutUint64(f.callerDirectory[32:], uint64(thunkPointerRV64(f.callerContext)))
					f.bindCaller(t)
					reusable := f.thunk(t, nativeImportModuleRV64(t, fixtureFunc{result: 0x7f, body: []byte{0x41, 19, 0x0b}})[0], 0, 1)
					if err := f.e.CallPrepared(reusable, nil, f.caller.LinMemBase(), f.trap, f.results[:8]); err != nil || binary.LittleEndian.Uint64(f.results) != 19 {
						t.Fatal("owner reuse after guard", err)
					}
				})
			}
		}
	}
}

func TestPrivateNativeImportRV64ProducerColdTrapAndBothOwnerReuse(t *testing.T) {
	for _, shared := range []bool{false, true} {
		t.Run(fmt.Sprint(shared), func(t *testing.T) {
			f := newNativeImportFixtureRV64(t, shared)
			entries := nativeImportModuleRV64(t, fixtureFunc{result: 0x7f, body: []byte{0x00, 0x0b}}, fixtureFunc{result: 0x7f, body: []byte{0x41, 23, 0x0b}})
			entry := f.thunk(t, entries[0], 0, 1)
			for i := range f.results {
				f.results[i] = 0xa5
			}
			var tr *coreruntime.TrapError
			err := f.e.CallPrepared(entry, nil, f.caller.LinMemBase(), f.trap, f.results[:8])
			if !errors.As(err, &tr) || tr.Code != coreruntime.TrapUnreachable || len(tr.Frames) != 1 || tr.Frames[0].FunctionIndex != 0 || !tr.Frames[0].HasProgramCounter || tr.Frames[0].ProgramCounter != 1 || !bytes.Equal(f.results, bytes.Repeat([]byte{0xa5}, len(f.results))) {
				t.Fatal("producer raw trap attribution/caller output", err)
			}
			// Cold unwind skips bridge epilogue. Quiescent owners explicitly rebind.
			f.target.BindInstanceContextBytes(f.targetContext)
			f.target.SetStackFence(f.e.StackLimit())
			if err := f.target.RebindTrapCell(f.trap); err != nil {
				t.Fatal(err)
			}
			if err := f.e.CallPrepared(entries[1], nil, f.target.LinMemBase(), f.trap, f.targetResults[:8]); err != nil || binary.LittleEndian.Uint64(f.targetResults) != 23 {
				t.Fatal("producer reuse", err)
			}
			f.bindCaller(t)
			if err := f.e.CallPrepared(f.thunk(t, entries[1], 0, 1), nil, f.caller.LinMemBase(), f.trap, f.results[:8]); err != nil || binary.LittleEndian.Uint64(f.results) != 23 {
				t.Fatal("consumer reuse", err)
			}
			f.assertCaller(t)
		})
	}
}

func TestPrivateNativeImportRV64HostReentryReturnAndLaterTrap(t *testing.T) {
	for _, shared := range []bool{false, true} {
		for _, ending := range []string{"normal", "cold", "second-host-cold"} {
			t.Run(fmt.Sprintf("shared=%t/%s", shared, ending), func(t *testing.T) {
				f := newNativeImportFixtureRV64(t, shared)
				if err := coreruntime.InitHostCtrlFrame(f.targetCtrl); err != nil {
					t.Fatal(err)
				}
				nestedEngine, err := coreruntime.NewEngineWithStackBytes(coreruntime.MinNativeStackBytes)
				if err != nil {
					t.Fatal(err)
				}
				t.Cleanup(func() {
					if err := nestedEngine.Close(); err != nil {
						t.Error(err)
					}
				})
				nestedArena, err := coreruntime.NewArena(128)
				if err != nil {
					t.Fatal(err)
				}
				t.Cleanup(func() {
					if err := nestedArena.Close(); err != nil {
						t.Error(err)
					}
				})
				nestedTrap, nestedResults, nestedCtrl := nestedArena.Alloc(coreruntime.TrapBufferBytes), nestedArena.Alloc(8), nestedArena.Alloc(8)
				local := nativeImportModuleRV64(t,
					fixtureFunc{result: 0x7f, body: []byte{0x41, 49, 0x0b}},
					fixtureFunc{result: 0x7f, body: []byte{0x41, 1, 0x41, 0, 0x6d, 0x0b}},
				)
				callee := thunkImageRV64(t, HostIndirectOwnedSyncThunk(37, 1, 1))
				entry := f.thunk(t, callee, 1, 1)
				second := thunkImageRV64(t, HostIndirectOwnedSyncThunk(38, 1, 1))
				var root enc.Asm
				root.Addi(enc.SP, enc.SP, -16)
				root.Sd(enc.RA, enc.SP, 0)
				root.Constant64(enc.X28, int64(entry))
				root.Jalr(enc.RA, enc.X28, 0)
				if ending == "second-host-cold" {
					root.Constant64(enc.X28, int64(second))
					root.Jalr(enc.RA, enc.X28, 0)
				}
				if ending != "normal" {
					root.Constant64(enc.X28, int64(local[1]))
					root.Jalr(enc.RA, enc.X28, 0)
				}
				root.Ld(enc.RA, enc.SP, 0)
				root.Addi(enc.SP, enc.SP, 16)
				root.Ret()
				if err := root.Err(); err != nil {
					t.Fatal(err)
				}
				entry = thunkImageRV64(t, root.B)
				binary.LittleEndian.PutUint64(f.args, 17)
				calls := 0
				host := func(ctrl uintptr, index uint32, in, out []uint64) {
					calls++
					if len(in) != 1 || len(out) != 1 || in[0] != 17 {
						panic("native cross-import host transport")
					}
					if index == 37 {
						if calls != 1 || ctrl != thunkPointerRV64(f.targetCtrl) {
							panic("producer host owner")
						}
						// A's controls are replaced by a different engine while B is parked.
						// Same-home reentry also replaces B's live pointer fields.
						f.caller.BindInstanceContextBytes(f.callerContext)
						f.caller.SetCustomCtx(thunkPointerRV64(nestedCtrl))
						f.caller.SetStackFence(nestedEngine.StackLimit())
						if err := f.caller.RebindTrapCell(nestedTrap); err != nil {
							panic(err)
						}
						if err := nestedEngine.CallPrepared(local[0], nil, f.caller.LinMemBase(), nestedTrap, nestedResults); err != nil || binary.LittleEndian.Uint64(nestedResults) != 49 {
							panic("nested caller execution")
						}
						goruntime.GC()
						if shared {
							f.target.BindInstanceContextBytes(f.targetContext)
						}
						out[0] = 31
					} else {
						if ending != "second-host-cold" || calls != 2 || index != 38 || ctrl != thunkPointerRV64(f.ctrl) {
							panic("restored active caller host control")
						}
						out[0] = 19
					}
				}
				err = f.e.CallWithHostBase(entry, f.args[:8], f.caller.LinMemBase(), f.trap, f.results[:8], f.ctrl, host)
				wantCalls, wantResult := 1, uint64(31)
				if ending == "second-host-cold" {
					wantCalls, wantResult = 2, 19
				}
				if calls != wantCalls || binary.LittleEndian.Uint64(f.results) != wantResult {
					t.Fatal("return/second park", calls, f.results[:8], err)
				}
				if ending == "normal" {
					if err != nil {
						t.Fatal(err)
					}
				} else {
					var tr *coreruntime.TrapError
					if !errors.As(err, &tr) || tr.Code != coreruntime.TrapDivZero || len(tr.Frames) != 1 || tr.Frames[0].FunctionIndex != 1 || tr.Frames[0].ProgramCounter != 5 {
						t.Fatal("caller cold trap after producer host reentry", err)
					}
				}
				f.assertCaller(t)
				f.bindCaller(t)
				if err := f.e.CallPrepared(local[0], nil, f.caller.LinMemBase(), f.trap, f.results[:8]); err != nil || binary.LittleEndian.Uint64(f.results) != 49 {
					t.Fatal("caller reuse after host reentry", err)
				}
				f.target.BindInstanceContextBytes(f.targetContext)
				f.target.SetStackFence(f.e.StackLimit())
				if err := f.target.RebindTrapCell(f.trap); err != nil {
					t.Fatal(err)
				}
				if err := f.e.CallPrepared(local[0], nil, f.target.LinMemBase(), f.trap, f.targetResults[:8]); err != nil || binary.LittleEndian.Uint64(f.targetResults) != 49 {
					t.Fatal("producer reuse after host reentry", err)
				}
			})
		}
	}
}
