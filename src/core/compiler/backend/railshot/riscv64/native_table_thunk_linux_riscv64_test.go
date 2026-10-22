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

	enc "github.com/wago-org/wago/src/core/encoder/riscv64"
	coreruntime "github.com/wago-org/wago/src/core/runtime"
	"github.com/wago-org/wago/src/core/runtime/abi"
)

const nativeTableTestKey = uint64(0xfedcba9876543210)

func nativeTableFixtureRV64(t *testing.T, shared bool) *nativeImportFixtureRV64 {
	t.Helper()
	f := newNativeImportFixtureRV64(t, shared)
	if err := coreruntime.InitHostCtrlFrame(f.ctrl); err != nil {
		t.Fatal(err)
	}
	f.caller.SetRV64NativeContext(thunkPointerRV64(f.callerContext))
	return f
}

func nativeTableDescriptorRV64(f *nativeImportFixtureRV64, entry uintptr) []byte {
	d := f.witness[:coreruntime.FuncRefDescBytes]
	binary.LittleEndian.PutUint64(d, uint64(entry))
	binary.LittleEndian.PutUint64(d[8:], nativeTableTestKey)
	binary.LittleEndian.PutUint64(d[16:], uint64(f.target.LinMemBase())|abi.FuncRefLocalWrapperHomeTag)
	binary.LittleEndian.PutUint64(d[24:], uint64(thunkPointerRV64(d)))
	binary.LittleEndian.PutUint64(d[32:], uint64(thunkPointerRV64(f.targetContext)))
	return d
}

func nativeTableRootRV64(t *testing.T, bridge uintptr, descriptor []byte, after ...uintptr) uintptr {
	t.Helper()
	var root enc.Asm
	root.Addi(enc.SP, enc.SP, -16)
	root.Sd(enc.RA, enc.SP, 0)
	root.Constant64(enc.X14, int64(thunkPointerRV64(descriptor)))
	root.Constant64(enc.X28, int64(bridge))
	root.Jalr(enc.RA, enc.X28, 0)
	for _, entry := range after {
		root.Constant64(enc.X28, int64(entry))
		root.Jalr(enc.RA, enc.X28, 0)
	}
	root.Ld(enc.RA, enc.SP, 0)
	root.Addi(enc.SP, enc.SP, 16)
	root.Ret()
	if err := root.Err(); err != nil {
		t.Fatal(err)
	}
	return thunkImageRV64(t, root.B)
}

func TestPrivateNativeTableRV64IndependentTargetContext(t *testing.T) {
	for _, shared := range []bool{false, true} {
		f := nativeTableFixtureRV64(t, shared)
		// Producer word0 is deliberately unusable; its active stack frame must
		// come from the caller's authenticated trampoline, not this mutable word.
		binary.LittleEndian.PutUint64(f.targetContext, 1)
		offsets := []int32{40, 80, abi.FuncRefDescPtrOffset, abi.PassiveElemPtrOffset, abi.GlobalsPtrOffset, abi.PassiveDataPtrOffset, abi.TableDirPtrOffset, abi.MemoryDirPtrOffset, abi.ImportDispatchPtrOffset, abi.GCNativeViewPtrOffset, abi.EHTagDirPtrOffset, abi.RV64NativeContextOffset}
		var leaf enc.Asm
		for i, offset := range offsets {
			leaf.Ld(enc.X5, enc.X25, -offset)
			leaf.Sd(enc.X5, enc.X13, int32(i*8))
		}
		leaf.Ret()
		d := nativeTableDescriptorRV64(f, thunkImageRV64(t, leaf.B))
		code, err := nativeTableThunk(nativeTableTestKey, 0, len(offsets), 7)
		if err != nil {
			t.Fatal(err)
		}
		entry := nativeTableRootRV64(t, thunkImageRV64(t, code), d)
		if err := f.e.CallPrepared(entry, nil, f.caller.LinMemBase(), f.trap, f.results); err != nil {
			t.Fatal(err)
		}
		ctrl := binary.LittleEndian.Uint64(f.results)
		if ctrl < uint64(f.e.StackLimit()) || ctrl&15 != 0 || ctrl == uint64(thunkPointerRV64(f.ctrl)) || ctrl == uint64(thunkPointerRV64(f.targetCtrl)) {
			t.Fatal("independent active stack control", ctrl)
		}
		for i := 1; i < len(offsets); i++ {
			source := i * 8
			if i == 9 {
				source = coreruntime.InstanceContextGCNativeViewOffset
			} else if i == 10 {
				source = coreruntime.InstanceContextEHTagDirOffset
			}
			want := uint64(thunkPointerRV64(f.targetContext))
			if i != 11 {
				want = binary.LittleEndian.Uint64(f.targetContext[source:])
			}
			if got := binary.LittleEndian.Uint64(f.results[i*8:]); got != want {
				t.Fatal("independent dynamic target context", offsets[i], got, want)
			}
		}
		f.assertCaller(t)
	}
}

func TestPrivateNativeTableRV64FullTransportAndContexts(t *testing.T) {
	for _, shared := range []bool{false, true} {
		for _, slots := range []int{0, 1, 64, 128, 255, 256} {
			t.Run(fmt.Sprintf("shared%t-slots%d", shared, slots), func(t *testing.T) {
				f := nativeTableFixtureRV64(t, shared)
				var leaf enc.Asm
				// Independent raw ABI observer uses all selected argument/result cells.
				for i := 0; i < slots; i++ {
					leaf.Ld(enc.X5, enc.X10, int32(i*8))
					leaf.Xori(enc.X5, enc.X5, -1)
					leaf.Sd(enc.X5, enc.X13, int32(i*8))
				}
				leaf.Ret()
				if err := leaf.Err(); err != nil {
					t.Fatal(err)
				}
				d := nativeTableDescriptorRV64(f, thunkImageRV64(t, leaf.B))
				code, err := nativeTableThunk(nativeTableTestKey, slots, slots, 7)
				if err != nil {
					t.Fatal(err)
				}
				entry := nativeTableRootRV64(t, thunkImageRV64(t, code), d)
				for i := 0; i < slots; i++ {
					binary.LittleEndian.PutUint64(f.args[i*8:], 0x7ff0000000000001^uint64(i))
				}
				before := bytes.Clone(f.args)
				for range 3 {
					for i := range f.results {
						f.results[i] = 0xa5
					}
					if err := f.e.CallPrepared(entry, f.args, f.caller.LinMemBase(), f.trap, f.results); err != nil {
						t.Fatal(err)
					}
					for i := 0; i < slots; i++ {
						if got := binary.LittleEndian.Uint64(f.results[i*8:]); got != ^binary.LittleEndian.Uint64(f.args[i*8:]) {
							t.Fatal("native table raw cell", i, got)
						}
					}
					if !bytes.Equal(before, f.args) || !bytes.Equal(f.results[slots*8:], bytes.Repeat([]byte{0xa5}, len(f.results)-slots*8)) {
						t.Fatal("native table caller transport canaries")
					}
					f.assertCaller(t)
					if f.caller.RV64NativeContext() != thunkPointerRV64(f.callerContext) {
						t.Fatal("native table caller identity restoration")
					}
				}
			})
		}
	}
}

func TestPrivateNativeTableRV64GuardFramePrewrite(t *testing.T) {
	for _, shared := range []bool{false, true} {
		for _, mode := range []string{"exact", "short", "reversed", "identity", "transport", "key", "entry", "tag", "self", "context", "anchor", "trampoline"} {
			t.Run(fmt.Sprintf("shared%t-%s", shared, mode), func(t *testing.T) {
				f := nativeTableFixtureRV64(t, shared)
				var leaf enc.Asm
				leaf.Ret()
				d := nativeTableDescriptorRV64(f, thunkImageRV64(t, leaf.B))
				code, err := nativeTableThunk(nativeTableTestKey, 0, 0, 7)
				if err != nil {
					t.Fatal(err)
				}
				bridge := thunkImageRV64(t, code)
				switch mode {
				case "identity":
					f.caller.SetRV64NativeContext(thunkPointerRV64(f.targetContext))
				case "key":
					binary.LittleEndian.PutUint64(d[8:], 17)
				case "entry":
					binary.LittleEndian.PutUint64(d, 0)
				case "tag":
					binary.LittleEndian.PutUint64(d[16:], uint64(f.target.LinMemBase())|abi.FuncRefInternalHomeTag)
				case "self":
					binary.LittleEndian.PutUint64(d[24:], 0)
				case "context":
					binary.LittleEndian.PutUint64(d[32:], 0)
				case "anchor":
					binary.LittleEndian.PutUint64(f.targetDirectory[32:], uint64(thunkPointerRV64(f.callerContext)))
				case "trampoline":
					binary.LittleEndian.PutUint64(f.ctrl[HostTrampolineOffset:], 0)
				}
				var root enc.Asm
				root.Addi(enc.SP, enc.SP, -16)
				root.Sd(enc.RA, enc.SP, 0)
				root.Constant64(enc.X5, nativeTableFrameBytes)
				root.Sub(enc.X5, enc.SP, enc.X5)
				root.Constant64(enc.X29, -6510615555426900571)
				for i := 0; i < nativeTableFrameBytes; i += 8 {
					root.Sd(enc.X29, enc.X5, 0)
					root.Addi(enc.X5, enc.X5, 8)
				}
				available := nativeTableFrameBytes
				if mode == "short" {
					available--
				} else if mode == "reversed" {
					available = -1
				}
				root.Constant64(enc.X29, int64(available))
				root.Sub(enc.X29, enc.SP, enc.X29)
				root.Sd(enc.X29, enc.X25, -abi.StackFenceOffset)
				root.Constant64(enc.X29, nativeTableFrameBytes)
				root.Sub(enc.X29, enc.SP, enc.X29)
				root.Constant64(enc.X28, int64(thunkPointerRV64(f.witness[40:])))
				root.Sd(enc.X29, enc.X28, 0)
				if mode == "transport" {
					root.Addi(enc.X11, enc.X25, 8)
				}
				root.Constant64(enc.X14, int64(thunkPointerRV64(d)))
				root.Constant64(enc.X28, int64(bridge))
				root.Jalr(enc.RA, enc.X28, 0)
				root.Ld(enc.RA, enc.SP, 0)
				root.Addi(enc.SP, enc.SP, 16)
				root.Ret()
				if err := root.Err(); err != nil {
					t.Fatal(err)
				}
				beforeCaller, beforeTarget := f.caller.CaptureInstanceContext(), f.target.CaptureInstanceContext()
				err = f.e.CallPrepared(thunkImageRV64(t, root.B), nil, f.caller.LinMemBase(), f.trap, nil)
				if mode == "exact" {
					if err != nil {
						t.Fatal("exact dynamic frame rejected", err)
					}
					f.assertCaller(t)
				} else {
					var trap *coreruntime.TrapError
					want := coreruntime.TrapCalledFnNotLinked
					if mode == "short" || mode == "reversed" {
						want = coreruntime.TrapStackFenceBreached
					}
					if !errors.As(err, &trap) || trap.Code != want || len(trap.Frames) != 1 || trap.Frames[0].FunctionIndex != 7 || trap.Frames[0].HasProgramCounter {
						t.Fatal("dynamic bridge guard source", err)
					}
					address := uintptr(binary.LittleEndian.Uint64(f.witness[40:]))
					pointer := *(*unsafe.Pointer)(unsafe.Pointer(&address))
					frame := unsafe.Slice((*byte)(pointer), nativeTableFrameBytes)
					if !bytes.Equal(frame, bytes.Repeat([]byte{0xa5}, nativeTableFrameBytes)) || beforeCaller != f.caller.CaptureInstanceContext() || beforeTarget != f.target.CaptureInstanceContext() {
						t.Fatal("guard wrote dynamic frame/context")
					}
				}
			})
		}
	}
}

func TestPrivateNativeTableRV64AsymmetricTransport(t *testing.T) {
	for _, shared := range []bool{false, true} {
		for _, params := range []int{0, 256} {
			results := 1
			if params == 0 {
				results = 256
			}
			t.Run(fmt.Sprintf("shared%t-%d-to-%d", shared, params, results), func(t *testing.T) {
				f := nativeTableFixtureRV64(t, shared)
				var leaf enc.Asm
				want := uint64(0)
				if params == 0 {
					for i := 0; i < results; i++ {
						leaf.Constant64(enc.X5, int64(0x7ff0000000000001+uint64(i)))
						leaf.Sd(enc.X5, enc.X13, int32(i*8))
					}
				} else {
					leaf.Addi(enc.X5, enc.Zero, 0)
					for i := 0; i < params; i++ {
						value := 0x7ff0000000000001 ^ uint64(i)
						binary.LittleEndian.PutUint64(f.args[i*8:], value)
						want += value
						leaf.Ld(enc.X6, enc.X10, int32(i*8))
						leaf.Add(enc.X5, enc.X5, enc.X6)
					}
					leaf.Sd(enc.X5, enc.X13, 0)
				}
				leaf.Ret()
				if err := leaf.Err(); err != nil {
					t.Fatal(err)
				}
				d := nativeTableDescriptorRV64(f, thunkImageRV64(t, leaf.B))
				code, err := nativeTableThunk(nativeTableTestKey, params, results, 7)
				if err != nil {
					t.Fatal(err)
				}
				entry := nativeTableRootRV64(t, thunkImageRV64(t, code), d)
				before := bytes.Clone(f.args)
				for i := range f.results {
					f.results[i] = 0xa5
				}
				if err := f.e.CallPrepared(entry, f.args, f.caller.LinMemBase(), f.trap, f.results); err != nil {
					t.Fatal(err)
				}
				for i := 0; i < results; i++ {
					if params == 0 {
						want = 0x7ff0000000000001 + uint64(i)
					}
					if got := binary.LittleEndian.Uint64(f.results[i*8:]); got != want {
						t.Fatal("asymmetric dynamic cell", i, got, want)
					}
				}
				if !bytes.Equal(before, f.args) || !bytes.Equal(f.results[results*8:], bytes.Repeat([]byte{0xa5}, len(f.results)-results*8)) {
					t.Fatal("asymmetric dynamic transport canaries")
				}
				f.assertCaller(t)
			})
		}
	}
}

func TestPrivateNativeTableRV64ColdTargetAndCallerReuse(t *testing.T) {
	for _, shared := range []bool{false, true} {
		f := nativeTableFixtureRV64(t, shared)
		entries := nativeImportModuleRV64(t, fixtureFunc{result: 0x7f, body: []byte{0x00, 0x0b}}, fixtureFunc{result: 0x7f, body: []byte{0x41, 23, 0x0b}})
		d := nativeTableDescriptorRV64(f, entries[0])
		code, err := nativeTableThunk(nativeTableTestKey, 0, 1, 7)
		if err != nil {
			t.Fatal(err)
		}
		entry := nativeTableRootRV64(t, thunkImageRV64(t, code), d)
		for i := range f.results {
			f.results[i] = 0xa5
		}
		beforeArgs := bytes.Clone(f.args)
		for range 3 {
			beforeResults := bytes.Clone(f.results)
			err := f.e.CallPrepared(entry, nil, f.caller.LinMemBase(), f.trap, f.results[:8])
			var trap *coreruntime.TrapError
			if !errors.As(err, &trap) || trap.Code != coreruntime.TrapUnreachable || len(trap.Frames) != 1 || trap.Frames[0].FunctionIndex != 0 || trap.Frames[0].ProgramCounter != 1 {
				t.Fatal("dynamic target cold origin", err)
			}
			if identity, present, pending := trap.TakeNativeOrigin(); identity != thunkPointerRV64(f.targetContext) || !present || !pending {
				t.Fatal("dynamic target native provenance", identity, present, pending)
			}
			if !bytes.Equal(f.args, beforeArgs) || !bytes.Equal(f.results, beforeResults) {
				t.Fatal("cold target published caller transport")
			}
			f.bindCaller(t)
			f.caller.SetRV64NativeContext(thunkPointerRV64(f.callerContext))
			binary.LittleEndian.PutUint64(d, uint64(entries[1]))
			if err := f.e.CallPrepared(entry, nil, f.caller.LinMemBase(), f.trap, f.results[:8]); err != nil || binary.LittleEndian.Uint64(f.results) != 23 {
				t.Fatal("same dynamic bridge next target", err)
			}
			f.assertCaller(t)
			binary.LittleEndian.PutUint64(d, uint64(entries[0]))
		}
	}
}

func TestPrivateNativeTableRV64OwnedHostParkAndGC(t *testing.T) {
	for _, shared := range []bool{false, true} {
		for _, cold := range []bool{false, true} {
			f := nativeTableFixtureRV64(t, shared)
			nestedEngine, err := coreruntime.NewEngineWithStackBytes(coreruntime.MinNativeStackBytes)
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { _ = nestedEngine.Close() })
			ar, err := coreruntime.NewArena(128)
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { _ = ar.Close() })
			nestedTrap, nestedResult, nestedCtrl := ar.Alloc(coreruntime.TrapBufferBytes), ar.Alloc(8), ar.Alloc(8)
			local := nativeImportModuleRV64(t, fixtureFunc{result: 0x7f, body: []byte{0x41, 49, 0x0b}}, fixtureFunc{result: 0x7f, body: []byte{0x41, 1, 0x41, 0, 0x6d, 0x0b}})
			d := nativeTableDescriptorRV64(f, thunkImageRV64(t, HostIndirectOwnedSyncThunk(37, 1, 1)))
			code, err := nativeTableThunk(nativeTableTestKey, 1, 1, 7)
			if err != nil {
				t.Fatal(err)
			}
			var after []uintptr
			if cold {
				after = append(after, local[1])
			}
			entry := nativeTableRootRV64(t, thunkImageRV64(t, code), d, after...)
			binary.LittleEndian.PutUint64(f.args, 17)
			calls := 0
			host := func(ctrl uintptr, index uint32, in, out []uint64) {
				calls++
				if index != 37 || len(in) != 1 || len(out) != 1 || in[0] != 17 || ctrl == thunkPointerRV64(f.ctrl) || ctrl == thunkPointerRV64(f.targetCtrl) {
					panic("dynamic stack-owned host activation")
				}
				// A real Go->guest reentry replaces caller controls with another engine
				// while the foreign activation is parked. Resume must recover its own
				// refreshed continuation and subsequently restore the original caller.
				f.caller.BindInstanceContextBytes(f.callerContext)
				f.caller.SetCustomCtx(thunkPointerRV64(nestedCtrl))
				f.caller.SetRV64NativeContext(thunkPointerRV64(f.callerContext))
				f.caller.SetStackFence(nestedEngine.StackLimit())
				if err := f.caller.RebindTrapCell(nestedTrap); err != nil {
					panic(err)
				}
				if err := nestedEngine.CallPrepared(local[0], nil, f.caller.LinMemBase(), nestedTrap, nestedResult); err != nil || binary.LittleEndian.Uint64(nestedResult) != 49 {
					panic("dynamic parked caller reentry")
				}
				goruntime.GC()
				if shared {
					f.target.BindInstanceContextBytes(f.targetContext)
					f.target.SetCustomCtx(ctrl)
					f.target.SetRV64NativeContext(thunkPointerRV64(f.targetContext))
				}
				out[0] = 31
			}
			for range 3 {
				err := f.e.CallWithHostBase(entry, f.args[:8], f.caller.LinMemBase(), f.trap, f.results[:8], f.ctrl, host)
				if binary.LittleEndian.Uint64(f.results) != 31 {
					t.Fatal("dynamic host return", err)
				}
				if cold {
					var trap *coreruntime.TrapError
					if !errors.As(err, &trap) || trap.Code != coreruntime.TrapDivZero || len(trap.Frames) != 1 || trap.Frames[0].FunctionIndex != 1 || trap.Frames[0].ProgramCounter != 5 {
						t.Fatal("dynamic caller cold after foreign host reentry", err)
					}
					if identity, present, pending := trap.TakeNativeOrigin(); identity != thunkPointerRV64(f.callerContext) || !present || !pending {
						t.Fatal("dynamic caller native provenance", identity, present, pending)
					}
				} else if err != nil {
					t.Fatal("dynamic host normal return", err)
				}
				f.assertCaller(t)
				f.bindCaller(t)
				f.caller.SetRV64NativeContext(thunkPointerRV64(f.callerContext))
			}
			if calls != 3 {
				t.Fatal("dynamic host calls", calls)
			}
		}
	}
}
