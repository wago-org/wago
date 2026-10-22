//go:build linux && riscv64 && !tinygo && !wago_target_tinygo

package riscv64

import (
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"runtime"
	"sync/atomic"
	"testing"
	"time"
	"unsafe"

	"github.com/wago-org/wago/src/core/compiler/wasm"
	enc "github.com/wago-org/wago/src/core/encoder/riscv64"
	coreruntime "github.com/wago-org/wago/src/core/runtime"
	"github.com/wago-org/wago/src/core/runtime/abi"
)

// Only this private proof extends a context to128 bytes. All native-visible
// data/code mappings are independently retained until native completion.
type tailRANativeFixture struct {
	e                                  *coreruntime.Engine
	a, b                               *coreruntime.JobMemory
	args, results, trap, ctrl, witness []byte
	argStorage, resultStorage          []byte
	ca, cb, da, db                     []byte
}

func tailRANativeFixtureNew(t *testing.T, shared bool, argCells, resultCells int) *tailRANativeFixture {
	t.Helper()
	f := new(tailRANativeFixture)
	// Canary cells surround the actual admitted directions. Owner ranges do not
	// include these tails; zero-cell directions really pass nil to native code.
	f.e, f.a, _, _, f.trap, f.ctrl = thunkFixtureRV64(t, 256)
	f.b = f.a
	if !shared {
		var err error
		f.b, err = coreruntime.NewJobMemory(65536)
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() {
			if err := f.b.Close(); err != nil {
				t.Error(err)
			}
		})
	}
	ar, err := coreruntime.NewArena(16384)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := ar.Close(); err != nil {
			t.Error(err)
		}
	})
	f.argStorage, f.resultStorage = ar.Alloc(16+argCells*8), ar.Alloc(16+resultCells*8)
	for i := range f.argStorage {
		f.argStorage[i] = 0xa5
	}
	for i := range f.resultStorage {
		f.resultStorage[i] = 0xa5
	}
	if argCells != 0 {
		f.args = f.argStorage[8 : 8+argCells*8]
	}
	if resultCells != 0 {
		f.results = f.resultStorage[8 : 8+resultCells*8]
	}
	f.ca, f.cb, f.da, f.db = ar.Alloc(128), ar.Alloc(128), ar.Alloc(40), ar.Alloc(40)
	f.witness = ar.Alloc(256)
	for _, item := range []struct{ ctx, dir []byte }{{f.ca, f.da}, {f.cb, f.db}} {
		// Enumerate the actual serialized runtime layout independently of emitter.
		for offset := 0; offset < 72; offset += 8 {
			binary.LittleEndian.PutUint64(item.ctx[offset:], uint64(tailRAPointer(ar.Alloc(8))))
		}
		for _, offset := range []int{104, 112} {
			binary.LittleEndian.PutUint64(item.ctx[offset:], uint64(tailRAPointer(ar.Alloc(8))))
		}
		binary.LittleEndian.PutUint64(item.ctx[16:], uint64(tailRAPointer(item.dir)))
		binary.LittleEndian.PutUint64(item.dir[32:], uint64(tailRAPointer(item.ctx)))
	}
	f.bind(t, f.e.StackLimit())
	return f
}
func (f *tailRANativeFixture) bind(t *testing.T, fence uintptr) {
	t.Helper()
	f.a.BindInstanceContextBytes(f.ca)
	f.a.SetRV64NativeContext(tailRAPointer(f.ca))
	f.a.SetCustomCtx(tailRAPointer(f.ctrl))
	f.a.SetStackFence(fence)
	if err := f.a.RebindTrapCell(f.trap); err != nil {
		t.Fatal(err)
	}
	clear(f.trap)
}
func tailRANativeExpand(f *integerEmitter, sig *wasm.CompType) {
	// Framer initially copies compact8-byte input cells. Expand backward into
	// canonical16-byte homes without overwriting any unread pair half.
	cells := swarSignatureSlots(sig.Params)
	for i := len(sig.Params) - 1; i >= 0; i-- {
		typ := sig.Params[i]
		n := 1
		if wasm.EqualValType(typ, wasm.V128) {
			n = 2
		}
		cells -= n
		f.Load(5, int32(callFrameHeader+cells*8), !swarClassOf(typ).narrow())
		if n == 2 {
			f.Load(6, int32(callFrameHeader+(cells+1)*8), true)
		} else {
			f.a.Addi(enc.X6, enc.Zero, 0)
		}
		f.Store(int32(callFrameHeader+i*16), 5, true)
		f.Store(int32(callFrameHeader+i*16+8), 6, true)
	}
}
func tailRANativeFrame(t *testing.T, sig *wasm.CompType, job *coreruntime.JobMemory, ctx, dir []byte, index uint32, body func(*integerEmitter)) tailRAFunction {
	return tailRANativeFrameInvocation(t, sig, job, ctx, dir, index, false, body)
}

func tailRANativeFrameInvocation(t *testing.T, sig *wasm.CompType, job *coreruntime.JobMemory, ctx, dir []byte, index uint32, invocation bool, body func(*integerEmitter)) tailRAFunction {
	return tailRANativeFrameInvocationLocals(t, sig, job, ctx, dir, index, invocation, 0, body)
}

func tailRANativeFrameInvocationLocals(t *testing.T, sig *wasm.CompType, job *coreruntime.JobMemory, ctx, dir []byte, index uint32, invocation bool, extra int, body func(*integerEmitter)) tailRAFunction {
	t.Helper()
	widths := make([]bool, 2*len(sig.Params)+2+extra)
	for i := range widths {
		widths[i] = true
	}
	frame := (callFrameHeader + 8*len(widths) + 15) &^ 15
	mode := emissionMode{production: true, interruptible: true, invocationContext: invocation, functionIndex: index, entryPC: 3}
	f := integerEmitter{target: target{header: callFrameHeader, mode: mode, nlocals: len(widths)}, frameBytes: frame}
	f.sourcePC = 37
	tailRANativeExpand(&f, sig)
	body(&f)
	// All normal paths jump over appended cold islands to the framed epilogue.
	end := f.a.JumpLong(false)
	for _, tr := range f.traps {
		if err := f.a.PatchLong(tr.site, int64(f.a.Len())); err != nil {
			t.Fatal(err)
		}
		f.emitProductionTrap(productionTrapCode(tr.code), tr.sourcePC)
	}
	if err := f.a.PatchLong(end, int64(f.a.Len())); err != nil {
		t.Fatal(err)
	}
	if f.err != nil || f.a.Err() != nil {
		t.Fatal(f.err, f.a.Err())
	}
	info := FunctionInfo{Params: swarSignatureSlots(sig.Params), Results: swarSignatureSlots(sig.Results), FrameHeaderBytes: callFrameHeader, mode: mode}
	code, info, err := frameFunctionCalls(f.a.B, widths, swarSignatureSlots(sig.Params), 0, true, -1, info, nil)
	if err != nil {
		t.Fatal(err)
	}
	info.CodeBytes = len(code)
	if info.FrameBytes != frame {
		t.Fatal("frame preflight drift", frame, info.FrameBytes)
	}
	return tailRAFunction{code: tailRAProofBuffer(t, code), snapshot: bytes.Clone(code), info: info, signature: sig, job: job, context: ctx, directory: dir}
}
func tailRANativeSerialize(f *integerEmitter, types []wasm.ValType, start int) {
	f.restoreCallTransport()
	cell := 0
	for i, typ := range types {
		f.Load(5, int32(callFrameHeader+(start+i)*16), !swarClassOf(typ).narrow())
		f.a.Sd(enc.X5, enc.X13, int32(cell*8))
		cell++
		if wasm.EqualValType(typ, wasm.V128) {
			f.Load(6, int32(callFrameHeader+(start+i)*16+8), true)
			f.a.Sd(enc.X6, enc.X13, int32(cell*8))
			cell++
		}
	}
}
func tailRANativeRoot(t *testing.T, callee tailRAFunction, witness []byte) *coreruntime.CodeBuffer {
	t.Helper()
	var a enc.Asm
	a.Addi(enc.SP, enc.SP, -32)
	a.Sd(enc.RA, enc.SP, 0)
	a.Sd(enc.X24, enc.SP, 8)
	a.Constant64(enc.X28, int64(tailRAPointer(witness)))
	for i, r := range []enc.Reg{enc.X3, enc.X4, enc.X26, enc.X27} {
		a.Sd(r, enc.X28, int32(96+i*8))
	}
	// Enumerate GP/TP/CTXT/g in the bridge save frame independently. The
	// 32-byte root frame sits immediately below that save frame. A host park
	// returns to Go, so resume can legitimately install a new CTXT or TP;
	// guest code must preserve the current bridge's values on each entry.
	for i, offset := range []int32{16, 24, 112, 120} {
		a.Ld(enc.X5, enc.SP, 32+offset)
		a.Sd(enc.X5, enc.X28, int32(192+i*8))
	}
	a.Constant64(enc.X24, 0x123456789abcdef)
	a.Constant64(enc.X28, int64(callee.codeBase()+uintptr(callee.info.Entry)))
	a.Jalr(enc.RA, enc.X28, 0)
	a.Constant64(enc.X28, int64(tailRAPointer(witness)))
	for i, r := range []enc.Reg{enc.X10, enc.X11, enc.X12, enc.X13, enc.X24, enc.X25} {
		a.Sd(r, enc.X28, int32(32+i*8))
	}
	for i, r := range []enc.Reg{enc.X3, enc.X4, enc.X26, enc.X27} {
		a.Sd(r, enc.X28, int32(128+i*8))
	}
	for i, offset := range []int32{16, 24, 112, 120} {
		a.Ld(enc.X5, enc.SP, 32+offset)
		a.Sd(enc.X5, enc.X28, int32(160+i*8))
	}
	a.Sd(enc.SP, enc.X28, 80)
	a.Ld(enc.X24, enc.SP, 8)
	a.Ld(enc.RA, enc.SP, 0)
	a.Addi(enc.SP, enc.SP, 32)
	a.Ret()
	if err := a.Err(); err != nil {
		t.Fatal(err)
	}
	return tailRAProofBuffer(t, a.B)
}
func tailRANativeOwner(t *testing.T, f *tailRANativeFixture, sites []tailRAOwnerSite, root *coreruntime.CodeBuffer, fence uintptr, additionalContinuations ...*coreruntime.CodeBuffer) *tailRAOwner {
	t.Helper()
	exit, err := tailRAExitCode()
	if err != nil {
		t.Fatal(err)
	}
	hostTrampoline, err := coreruntime.HostCallTrampolinePointer()
	if err != nil {
		t.Fatal(err)
	}
	s := tailRAOwnerSpec{sites: sites, exit: tailRAProofBuffer(t, exit), continuations: []tailRARange{{root.Base(), uintptr(len(root.Bytes()))}}, retained: []any{f, root}, stackLimit: fence, stackTop: f.e.StackTop(), hostTrampoline: hostTrampoline}
	for _, code := range additionalContinuations {
		s.continuations = append(s.continuations, tailRARange{code.Base(), uintptr(len(code.Bytes()))})
		s.retained = append(s.retained, code)
	}
	if len(f.args) != 0 {
		s.arguments = []tailRARange{{tailRAPointer(f.args), uintptr(len(f.args))}}
	}
	if len(f.results) != 0 {
		s.results = []tailRARange{{tailRAPointer(f.results), uintptr(len(f.results))}}
	}
	o, err := newTailRAOwner(s)
	if err != nil {
		if o != nil {
			_ = o.Close()
		}
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := o.Close(); err != nil {
			t.Error(err)
		}
	})
	for _, code := range append([]*coreruntime.CodeBuffer{root}, additionalContinuations...) {
		if err := code.Seal(); err != nil {
			t.Fatal(err)
		}
	}
	return o
}
func tailRANativeInvoke(t *testing.T, f *tailRANativeFixture, o *tailRAOwner, root *coreruntime.CodeBuffer) error {
	t.Helper()
	if err := o.acquire(); err != nil {
		return err
	}
	defer o.release()
	err := f.e.CallPrepared(root.Base(), f.args, f.a.LinMemBase(), f.trap, f.results)
	if !f.canariesIntact() {
		return errors.New("actual-count native arena canary changed")
	}
	return err
}
func (f *tailRANativeFixture) canariesIntact() bool {
	for _, storage := range [][]byte{f.argStorage, f.resultStorage} {
		for _, region := range [][]byte{storage[:8], storage[len(storage)-8:]} {
			for _, b := range region {
				if b != 0xa5 {
					return false
				}
			}
		}
	}
	return true
}
func (f *tailRANativeFixture) assertCanaries(t *testing.T) {
	t.Helper()
	if !f.canariesIntact() {
		t.Fatal("actual-count native arena canary changed")
	}
}
func (f *tailRANativeFixture) assertReturned(t *testing.T) {
	t.Helper()
	f.assertCanaries(t)
	want := []uint64{uint64(tailRAPointer(f.args)), uint64(f.a.LinMemBase()), uint64(tailRAPointer(f.trap)), uint64(tailRAPointer(f.results)), 0x123456789abcdef, uint64(f.a.LinMemBase())}
	for i, w := range want {
		if g := binary.LittleEndian.Uint64(f.witness[32+i*8:]); g != w {
			t.Fatal("original transport/RA-root/X24 restoration", i, g, w)
		}
	}
	if !bytes.Equal(f.witness[96:128], f.witness[192:224]) || !bytes.Equal(f.witness[128:160], f.witness[160:192]) {
		t.Fatal("guest clobbered current bridge GP/TP/CTXT/g")
	}
	// Independently enumerate every context destination after the foreign return.
	for i, off := range []int32{80, abi.FuncRefDescPtrOffset, abi.PassiveElemPtrOffset, abi.GlobalsPtrOffset, abi.PassiveDataPtrOffset, abi.TableDirPtrOffset, abi.MemoryDirPtrOffset, abi.ImportDispatchPtrOffset, abi.GCNativeViewPtrOffset, abi.EHTagDirPtrOffset} {
		source := (i + 1) * 8
		if i == 8 {
			source = 104
		}
		if i == 9 {
			source = 112
		}
		if g, w := nativeImportWordRV64(f.a.LinMemBase(), off), binary.LittleEndian.Uint64(f.ca[source:]); g != w {
			t.Fatal("caller context field restored", off, g, w)
		}
	}
	if nativeImportWordRV64(f.a.LinMemBase(), 40) != uint64(tailRAPointer(f.ctrl)) || nativeImportWordRV64(f.a.LinMemBase(), abi.RV64NativeContextOffset) != uint64(tailRAPointer(f.ca)) {
		t.Fatal("active control/origin restored")
	}
}

func TestTailRAProofRV64ForeignChainConstantStackAndMixedTransport(t *testing.T) {
	for _, shared := range []bool{false, true} {
		for _, maximum := range []bool{false, true} {
			t.Run(fmt.Sprintf("shared=%t/max=%t", shared, maximum), func(t *testing.T) {
				types := []wasm.ValType{wasm.I32, wasm.V128, wasm.F32, wasm.F64, wasm.I64}
				if maximum {
					types = make([]wasm.ValType, 128)
					for i := range types {
						types[i] = wasm.V128
					}
				}
				// The full256-cell case uses no countdown parameter; one foreign hop. Mixed
				// case performs2049 genuine alternating foreign hops with one record.
				params := append([]wasm.ValType(nil), types...)
				counted := !maximum
				if counted {
					params = append([]wasm.ValType{wasm.I64}, params...)
				}
				sig := &wasm.CompType{Kind: wasm.CompFunc, Params: params, Results: types}
				f := tailRANativeFixtureNew(t, shared, swarSignatureSlots(params), swarSignatureSlots(types))
				var beforeArgs []byte
				cell := 0
				if counted {
					binary.LittleEndian.PutUint64(f.args, 2049)
					cell++
				}
				var want []uint64
				for i, typ := range types {
					value := uint64(0xffffaaaa7f800001) ^ uint64(i)*0x101010101
					binary.LittleEndian.PutUint64(f.args[cell*8:], value)
					cell++
					if swarClassOf(typ).narrow() {
						value = uint64(uint32(value))
					}
					want = append(want, value)
					if wasm.EqualValType(typ, wasm.V128) {
						value = ^value
						binary.LittleEndian.PutUint64(f.args[cell*8:], value)
						cell++
						want = append(want, value)
					}
				}
				beforeArgs = bytes.Clone(f.args)
				makeBody := func(site uint32, terminal bool) func(*integerEmitter) {
					return func(e *integerEmitter) {
						start := 0
						if counted {
							start = 1
						}
						// Genuine qualified vector arithmetic, not register poison: addition by
						// zero preserves raw lanes and leaves X11 as its non-home lane mask.
						vector := 0
						if counted {
							vector = 2
						}
						e.Load(5, int32(callFrameHeader+16*vector), true)
						e.Load(6, int32(callFrameHeader+16*vector+8), true)
						e.a.Addi(enc.X7, enc.Zero, 0)
						e.a.Addi(enc.X9, enc.Zero, 0)
						e.swarAddSub(8, false)
						e.Store(int32(callFrameHeader+16*vector), 5, true)
						e.Store(int32(callFrameHeader+16*vector+8), 6, true)
						if counted {
							e.Load(5, callFrameHeader, true)
							normal := e.a.BranchLong(enc.EQ, enc.X5, enc.Zero)
							e.a.Addi(enc.X5, enc.X5, -1)
							e.Store(callFrameHeader, 5, true)
							// Observe recurring incoming SP only after first record creation (count
							// <=2048). All later frames must have the same fixed incoming record SP.
							e.a.Constant64(enc.X28, int64(tailRAPointer(f.witness)))
							tailRAAddress(&e.a, enc.X29, enc.SP, e.frameBytes)
							e.a.Ld(enc.X7, enc.X28, 0)
							first := e.a.BranchLong(enc.EQ, enc.X7, enc.Zero)
							e.a.Sub(enc.X7, enc.X7, enc.X29)
							e.a.Ld(enc.X6, enc.X28, 8)
							e.a.Or(enc.X7, enc.X7, enc.X6)
							e.a.Sd(enc.X7, enc.X28, 8)
							next := e.a.JumpLong(false)
							if err := e.a.PatchLong(first, int64(e.a.Len())); err != nil {
								t.Fatal(err)
							}
							e.a.Sd(enc.X29, enc.X28, 0)
							if err := e.a.PatchLong(next, int64(e.a.Len())); err != nil {
								t.Fatal(err)
							}
							// Root's first incoming SP differs once; reset the witness at first-hop
							// count2048 before crossing, so only reusable-record activations compare.
							e.Load(5, callFrameHeader, true)
							e.a.Constant64(enc.X6, 2048)
							later := e.a.BranchLong(enc.NE, enc.X5, enc.X6)
							e.a.Sd(enc.Zero, enc.X28, 0)
							e.a.Sd(enc.Zero, enc.X28, 8)
							if err := e.a.PatchLong(later, int64(e.a.Len())); err != nil {
								t.Fatal(err)
							}
							e.tailRAProofCall(site, sig, 0, func(i int) int32 { return int32(callFrameHeader + i*16) })
							if err := e.a.PatchLong(normal, int64(e.a.Len())); err != nil {
								t.Fatal(err)
							}
						} else if !terminal {
							e.tailRAProofCall(site, sig, 0, func(i int) int32 { return int32(callFrameHeader + i*16) })
						}
						tailRANativeSerialize(e, types, start)
					}
				}
				a := tailRANativeFrame(t, sig, f.a, f.ca, f.da, 0, makeBody(0, false))
				b := tailRANativeFrame(t, sig, f.b, f.cb, f.db, 1, makeBody(1, maximum))
				root := tailRANativeRoot(t, a, f.witness)
				sites := []tailRAOwnerSite{{caller: a, target: b, sourcePC: 37, sourceBase: 48, sourceStride: 16}}
				if counted {
					sites = append(sites, tailRAOwnerSite{caller: b, target: a, sourcePC: 37, sourceBase: 48, sourceStride: 16})
				}
				o := tailRANativeOwner(t, f, sites, root, f.e.StackLimit())
				if err := tailRANativeInvoke(t, f, o, root); err != nil {
					t.Fatal(err)
				}
				for i, w := range want {
					if g := binary.LittleEndian.Uint64(f.results[i*8:]); g != w {
						t.Fatal("every raw result cell", i, g, w)
					}
				}
				f.assertReturned(t)
				if counted && (binary.LittleEndian.Uint64(f.witness) == 0 || binary.LittleEndian.Uint64(f.witness[8:]) != 0) {
					t.Fatal("foreign chain grew stack", f.witness[:16])
				}
				// Warm no per-hop Go allocation. Reconstruct compact args because native
				// call marshalling intentionally overwrites the serialized prefix.
				allocations := testing.AllocsPerRun(4, func() {
					copy(f.args, beforeArgs)
					f.bind(t, f.e.StackLimit())
					if err := tailRANativeInvoke(t, f, o, root); err != nil {
						t.Fatal(err)
					}
				})
				if allocations != 0 {
					t.Fatal("warm private native allocation", allocations)
				}
			})
		}
	}
}

func TestTailRAProofRV64ZeroNilDirectionsColdCleanupAndReuse(t *testing.T) {
	for _, cold := range []bool{false, true} {
		t.Run(fmt.Sprint(cold), func(t *testing.T) {
			f := tailRANativeFixtureNew(t, false, 0, 0)
			sig := &wasm.CompType{Kind: wasm.CompFunc}
			a := tailRANativeFrame(t, sig, f.a, f.ca, f.da, 0, func(e *integerEmitter) { e.tailRAProofCall(0, sig, 0, func(i int) int32 { return int32(48 + 16*i) }) })
			b := tailRANativeFrame(t, sig, f.b, f.cb, f.db, 1, func(e *integerEmitter) {
				if cold {
					e.emitProductionTrap(abi.TrapUnreachable, 91)
				}
			})
			root := tailRANativeRoot(t, a, f.witness)
			o := tailRANativeOwner(t, f, []tailRAOwnerSite{{caller: a, target: b, sourcePC: 37, sourceBase: 48, sourceStride: 16}}, root, f.e.StackLimit())
			err := tailRANativeInvoke(t, f, o, root)
			if cold {
				var te *coreruntime.TrapError
				if !errors.As(err, &te) || te.Code != coreruntime.TrapUnreachable || len(te.Frames) != 1 || te.Frames[0].FunctionIndex != 1 || te.Frames[0].ProgramCounter != 91 {
					t.Fatal("foreign cold source", err)
				}
				if o.active != 0 {
					t.Fatal("cold stranded lease")
				}
				f.bind(t, f.e.StackLimit())
				if err := f.e.CallPrepared(root.Base()+uintptr(len(root.Bytes())-4), nil, f.a.LinMemBase(), f.trap, nil); err != nil {
					t.Fatal("engine reuse after cold", err)
				}
			} else {
				if err != nil {
					t.Fatal(err)
				}
				f.assertReturned(t)
			}
			if err := o.Close(); err != nil || o.arena != nil || o.code != nil {
				t.Fatal("normal/cold cleanup", err)
			}
		})
	}
}

func tailRANativeStackSlice(t *testing.T, e *coreruntime.Engine, base uintptr, n int) []byte {
	t.Helper()
	if base < e.StackLimit() || n < 0 || base+uintptr(n) > e.StackTop() {
		t.Fatal("retained native stack range")
	}
	pointer := *(*unsafe.Pointer)(unsafe.Pointer(&base))
	return unsafe.Slice((*byte)(pointer), n)
}
func TestTailRAProofRV64RecordFenceBeforeAnyExchangeWrite(t *testing.T) {
	f := tailRANativeFixtureNew(t, false, 1, 1)
	sig := &wasm.CompType{Kind: wasm.CompFunc, Params: []wasm.ValType{wasm.I64}, Results: []wasm.ValType{wasm.I64}}
	// First measure the real caller incoming SP using identical production frame.
	probe := tailRANativeFrame(t, sig, f.a, f.ca, f.da, 0, func(e *integerEmitter) { tailRAAddress(&e.a, enc.X5, enc.SP, e.frameBytes); e.a.Sd(enc.X5, enc.X13, 0) })
	rootProbe := tailRANativeRoot(t, probe, f.witness)
	if err := probe.code.Seal(); err != nil {
		t.Fatal(err)
	}
	if err := rootProbe.Seal(); err != nil {
		t.Fatal(err)
	}
	if err := f.e.CallPrepared(rootProbe.Base(), f.args, f.a.LinMemBase(), f.trap, f.results); err != nil {
		t.Fatal(err)
	}
	incoming := uintptr(binary.LittleEndian.Uint64(f.results))
	a := tailRANativeFrame(t, sig, f.a, f.ca, f.da, 0, func(e *integerEmitter) { e.tailRAProofCall(0, sig, 0, func(i int) int32 { return int32(48 + 16*i) }) })
	b := tailRANativeFrame(t, sig, f.b, f.cb, f.db, 1, func(e *integerEmitter) { tailRANativeSerialize(e, sig.Results, 0) })
	root := tailRANativeRoot(t, a, f.witness)
	for _, boundary := range []string{"short-record", "exact-record", "full-target"} {
		fence := incoming - tailRARecordBytes
		if boundary == "short-record" {
			fence += 16
		}
		if boundary == "full-target" {
			fence -= uintptr(b.info.FrameBytes)
		}
		f.bind(t, fence)
		o := tailRANativeOwner(t, f, []tailRAOwnerSite{{caller: a, target: b, sourcePC: 37, sourceBase: 48, sourceStride: 16}}, root, fence)
		// Old caller frame writes are permitted. The complete unused part of the
		// proposed record below its SP must retain A5 on guard failure.
		lower := tailRANativeStackSlice(t, f.e, incoming-tailRARecordBytes, tailRARecordBytes-a.info.FrameBytes)
		for i := range lower {
			lower[i] = 0xa5
		}
		binary.LittleEndian.PutUint64(f.args, 0x7ff0000000000001)
		for i := range f.results {
			f.results[i] = 0xa5
		}
		args := bytes.Clone(f.args)
		err := tailRANativeInvoke(t, f, o, root)
		if boundary == "short-record" {
			var te *coreruntime.TrapError
			if !errors.As(err, &te) || te.Code != coreruntime.TrapStackFenceBreached || len(te.Frames) != 1 || te.Frames[0].FunctionIndex != 0 || te.Frames[0].ProgramCounter != 37 {
				t.Fatal("record fence source", err)
			}
			if !bytes.Equal(lower, bytes.Repeat([]byte{0xa5}, len(lower))) || !bytes.Equal(args, f.args) || !bytes.Equal(f.results, bytes.Repeat([]byte{0xa5}, len(f.results))) {
				t.Fatal("prewrite guard changed record/exchange")
			}
		} else if boundary == "exact-record" {
			var te *coreruntime.TrapError
			if !errors.As(err, &te) || te.Code != coreruntime.TrapStackFenceBreached || len(te.Frames) != 1 || te.Frames[0].FunctionIndex != 1 || te.Frames[0].ProgramCounter != 3 {
				t.Fatal("record accepted, callee independently guards its frame", err)
			}
			if bytes.Equal(lower, bytes.Repeat([]byte{0xa5}, len(lower))) || !bytes.Equal(f.results, bytes.Repeat([]byte{0xa5}, len(f.results))) {
				t.Fatal("exact record boundary witness")
			}
		} else {
			if err != nil || binary.LittleEndian.Uint64(f.results) != 0x7ff0000000000001 {
				t.Fatal("record plus target fits and returns", err)
			}
		}
		if err := o.Close(); err != nil {
			t.Fatal(err)
		}
	}
}

func TestTailRAProofRV64ActiveAlternatingForeignInterruptAndReuse(t *testing.T) {
	old := runtime.GOMAXPROCS(1)
	defer runtime.GOMAXPROCS(old)
	f := tailRANativeFixtureNew(t, false, 2, 2)
	sig := &wasm.CompType{Kind: wasm.CompFunc, Params: []wasm.ValType{wasm.V128}, Results: []wasm.ValType{wasm.V128}}
	body := func(site uint32) func(*integerEmitter) {
		return func(e *integerEmitter) {
			e.a.Constant64(enc.X28, int64(tailRAPointer(f.witness[224:])))
			e.a.Addi(enc.X5, enc.Zero, 1)
			e.a.Sw(enc.X5, enc.X28, 0)
			e.tailRAProofCall(site, sig, 0, func(i int) int32 { return int32(48 + 16*i) })
		}
	}
	a := tailRANativeFrame(t, sig, f.a, f.ca, f.da, 0, body(0))
	b := tailRANativeFrame(t, sig, f.b, f.cb, f.db, 1, body(1))
	root := tailRANativeRoot(t, a, f.witness)
	o := tailRANativeOwner(t, f, []tailRAOwnerSite{{caller: a, target: b, sourcePC: 37, sourceBase: 48, sourceStride: 16}, {caller: b, target: a, sourcePC: 37, sourceBase: 48, sourceStride: 16}}, root, f.e.StackLimit())
	for i := range f.results {
		f.results[i] = 0xa5
	}
	done := make(chan error, 1)
	go func() { done <- tailRANativeInvoke(t, f, o, root) }()
	word := (*uint32)(unsafe.Pointer(&f.witness[224]))
	deadline := time.Now().Add(2 * time.Second)
	started := false
	for time.Now().Before(deadline) {
		if atomic.LoadUint32(word) != 0 {
			started = true
			break
		}
		select {
		case err := <-done:
			t.Fatal("foreign cycle returned", err)
		default:
		}
		time.Sleep(100 * time.Microsecond)
	}
	stop := coreruntime.RequestInterruptAsync(f.trap)
	err := <-done
	stop()
	var te *coreruntime.TrapError
	if !started || !errors.As(err, &te) || te.Code != coreruntime.TrapInterrupted || len(te.Frames) != 1 || te.Frames[0].FunctionIndex > 1 || te.Frames[0].ProgramCounter != 3 || o.active != 0 {
		t.Fatal("foreign poll/cold lease", err)
	}
	if !bytes.Equal(f.results, bytes.Repeat([]byte{0xa5}, len(f.results))) {
		t.Fatal("interrupted chain published results")
	}
	f.bind(t, f.e.StackLimit())
	leaf := tailRANativeFrame(t, sig, f.a, f.ca, f.da, 2, func(e *integerEmitter) { tailRANativeSerialize(e, sig.Results, 0) })
	if err := leaf.code.Seal(); err != nil {
		t.Fatal(err)
	}
	binary.LittleEndian.PutUint64(f.args, 0x7ff0000000000001)
	binary.LittleEndian.PutUint64(f.args[8:], 0x8000000000000000)
	if err := f.e.CallPrepared(leaf.code.Base(), f.args, f.a.LinMemBase(), f.trap, f.results); err != nil || !bytes.Equal(f.args, f.results) {
		t.Fatal("cold native engine reuse", err)
	}
	if err := o.Close(); err != nil {
		t.Fatal(err)
	}
}

func TestTailRAProofRV64HostParkNestedNativeContextAndColdReturn(t *testing.T) {
	for _, shared := range []bool{false, true} {
		for _, cold := range []bool{false, true} {
			t.Run(fmt.Sprintf("shared=%t/cold=%t", shared, cold), func(t *testing.T) {
				f := tailRANativeFixtureNew(t, shared, 1, 1)
				sig := &wasm.CompType{Kind: wasm.CompFunc, Params: []wasm.ValType{wasm.I64}, Results: []wasm.ValType{wasm.I64}}
				hostCode := tailRAProofBuffer(t, HostIndirectOwnedSyncThunk(37, 1, 1))
				if err := hostCode.Seal(); err != nil {
					t.Fatal(err)
				}
				a := tailRANativeFrame(t, sig, f.a, f.ca, f.da, 0, func(e *integerEmitter) { e.tailRAProofCall(0, sig, 0, func(i int) int32 { return int32(48 + 16*i) }) })
				b := tailRANativeFrame(t, sig, f.b, f.cb, f.db, 1, func(e *integerEmitter) {
					e.restoreCallTransport()
					e.a.Constant64(enc.X28, int64(hostCode.Base()))
					e.a.Jalr(enc.RA, enc.X28, 0)
					e.restoreCallTransport()
					if cold {
						e.emitProductionTrap(abi.TrapDivZero, 92)
					}
				})
				root := tailRANativeRoot(t, a, f.witness)
				o := tailRANativeOwner(t, f, []tailRAOwnerSite{{caller: a, target: b, sourcePC: 37, sourceBase: 48, sourceStride: 16}}, root, f.e.StackLimit())
				nested, err := coreruntime.NewEngineWithStackBytes(coreruntime.MinNativeStackBytes)
				if err != nil {
					t.Fatal(err)
				}
				t.Cleanup(func() {
					if err := nested.Close(); err != nil {
						t.Error(err)
					}
				})
				ar, err := coreruntime.NewArena(128)
				if err != nil {
					t.Fatal(err)
				}
				t.Cleanup(func() {
					if err := ar.Close(); err != nil {
						t.Error(err)
					}
				})
				nestedTrap, nestedResult := ar.Alloc(coreruntime.TrapBufferBytes), ar.Alloc(8)
				leaf := tailRANativeFrame(t, &wasm.CompType{Kind: wasm.CompFunc, Results: []wasm.ValType{wasm.I64}}, f.a, f.ca, f.da, 2, func(e *integerEmitter) {
					e.restoreCallTransport()
					e.a.Constant64(enc.X5, 49)
					e.a.Sd(enc.X5, enc.X13, 0)
				})
				if err := leaf.code.Seal(); err != nil {
					t.Fatal(err)
				}
				binary.LittleEndian.PutUint64(f.args, 0x7ff0000000000001)
				for i := range f.results {
					f.results[i] = 0xa5
				}
				calls := 0
				callback := func(ctrl uintptr, index uint32, in, out []uint64) {
					calls++
					if index != 37 || len(in) != 1 || len(out) != 1 || in[0] != 0x7ff0000000000001 || ctrl < f.e.StackLimit() || ctrl+HostFixedFrameBytes > f.e.StackTop() {
						panic("private record host control/transport")
					}
					// Host portal must see the frozen target identity before and after actual
					// caller reentry on a different native stack, including shared-home case.
					if nativeImportWordRV64(f.b.LinMemBase(), abi.RV64NativeContextOffset) != uint64(tailRAPointer(f.cb)) {
						panic("target identity before park")
					}
					if err := o.Close(); !errors.Is(err, ErrUnsupported) {
						panic("parked owner retired")
					}
					f.a.BindInstanceContextBytes(f.ca)
					f.a.SetRV64NativeContext(tailRAPointer(f.ca))
					f.a.SetCustomCtx(tailRAPointer(f.ctrl))
					f.a.SetStackFence(nested.StackLimit())
					if err := f.a.RebindTrapCell(nestedTrap); err != nil {
						panic(err)
					}
					if err := nested.CallPrepared(leaf.code.Base(), nil, f.a.LinMemBase(), nestedTrap, nestedResult); err != nil || binary.LittleEndian.Uint64(nestedResult) != 49 {
						panic("nested caller native entry")
					}
					runtime.GC()
					// Owner-specific resume restores target pointer context; runtime restores
					// its execution continuation/fence/trap from the authenticated parked frame.
					f.b.BindInstanceContextBytes(f.cb)
					f.b.SetRV64NativeContext(tailRAPointer(f.cb))
					f.b.SetCustomCtx(ctrl)
					out[0] = 0x8000000000000031
				}
				if err := o.acquire(); err != nil {
					t.Fatal(err)
				}
				err = f.e.CallWithHostBase(root.Base(), f.args, f.a.LinMemBase(), f.trap, f.results, f.ctrl, callback)
				o.release()
				if calls != 1 {
					t.Fatal("actual host park count", calls, err)
				}
				if cold {
					var te *coreruntime.TrapError
					if !errors.As(err, &te) || te.Code != coreruntime.TrapDivZero || len(te.Frames) != 1 || te.Frames[0].FunctionIndex != 1 || te.Frames[0].ProgramCounter != 92 || !bytes.Equal(f.results, bytes.Repeat([]byte{0xa5}, len(f.results))) {
						t.Fatal("post-host cold source, no original result publication", err)
					}
				} else {
					if err != nil || binary.LittleEndian.Uint64(f.results) != 0x8000000000000031 {
						t.Fatal("host resumed native return", err)
					}
					f.assertReturned(t)
				}
				// Cold exits skip the native exit but never own independent resource roots.
				if o.active != 0 {
					t.Fatal("park/cold left lease")
				}
				f.bind(t, f.e.StackLimit())
				if err := f.e.CallPrepared(leaf.code.Base(), nil, f.a.LinMemBase(), f.trap, f.results); err != nil || binary.LittleEndian.Uint64(f.results) != 49 {
					t.Fatal("post-host normal/cold reuse", err)
				}
				if err := o.Close(); err != nil {
					t.Fatal(err)
				}
			})
		}
	}
}

func TestTailRAProofRV64CanonicalGuardsBeforeArgumentPublication(t *testing.T) {
	f := tailRANativeFixtureNew(t, true, 1, 1)
	sig := &wasm.CompType{Kind: wasm.CompFunc, Params: []wasm.ValType{wasm.I32}, Results: []wasm.ValType{wasm.I32}}
	a := tailRANativeFrame(t, sig, f.a, f.ca, f.da, 0, func(e *integerEmitter) { e.tailRAProofCall(0, sig, 0, func(i int) int32 { return int32(48 + 16*i) }) })
	b := tailRANativeFrame(t, sig, f.b, f.cb, f.db, 1, func(e *integerEmitter) { tailRANativeSerialize(e, sig.Results, 0) })
	root := tailRANativeRoot(t, a, f.witness)
	o := tailRANativeOwner(t, f, []tailRAOwnerSite{{caller: a, target: b, sourcePC: 37, sourceBase: 48, sourceStride: 16}}, root, f.e.StackLimit())
	originalCaller := binary.LittleEndian.Uint64(f.da[32:])
	originalTarget := binary.LittleEndian.Uint64(f.db[32:])
	originalMagic := binary.LittleEndian.Uint64(o.capability)
	for _, fault := range []string{"same-home-sibling-caller", "wrong-target-anchor", "wrong-capability-magic", "wrong-capability-version"} {
		f.bind(t, f.e.StackLimit())
		binary.LittleEndian.PutUint64(f.args, 0xffffaaaa80000001)
		for i := range f.results {
			f.results[i] = 0xa5
		}
		args := bytes.Clone(f.args)
		switch fault {
		case "same-home-sibling-caller":
			binary.LittleEndian.PutUint64(f.da[32:], uint64(tailRAPointer(f.cb)))
		case "wrong-target-anchor":
			binary.LittleEndian.PutUint64(f.db[32:], uint64(tailRAPointer(f.ca)))
		case "wrong-capability-magic":
			binary.LittleEndian.PutUint64(o.capability, 0)
		case "wrong-capability-version":
			binary.LittleEndian.PutUint32(o.capability[8:], 2)
		}
		err := tailRANativeInvoke(t, f, o, root)
		var te *coreruntime.TrapError
		if !errors.As(err, &te) || te.Code != coreruntime.TrapCalledFnNotLinked || len(te.Frames) != 1 || te.Frames[0].FunctionIndex != 0 || te.Frames[0].ProgramCounter != 37 || !bytes.Equal(f.args, args) || !bytes.Equal(f.results, bytes.Repeat([]byte{0xa5}, len(f.results))) {
			t.Fatal("canonical pre-publication guard", fault, err)
		}
		binary.LittleEndian.PutUint64(f.da[32:], originalCaller)
		binary.LittleEndian.PutUint64(f.db[32:], originalTarget)
		binary.LittleEndian.PutUint64(o.capability, originalMagic)
		binary.LittleEndian.PutUint32(o.capability[8:], tailRAVersion)
	}
	f.bind(t, f.e.StackLimit())
	binary.LittleEndian.PutUint64(f.args, 0xffffaaaa80000001)
	if err := tailRANativeInvoke(t, f, o, root); err != nil || binary.LittleEndian.Uint64(f.results) != 0x80000001 {
		t.Fatal("guard reuse+narrow normalization", err)
	}
}

func TestTailRAProofRV64ExitNormalizesEveryMaskWordAndOlderFramedTarget(t *testing.T) {
	t.Run("all-four-mask-words", func(t *testing.T) {
		types := make([]wasm.ValType, 256)
		for i := range types {
			types[i] = []wasm.ValType{wasm.I32, wasm.F32, wasm.I64, wasm.F64}[i%4]
		}
		sig := &wasm.CompType{Kind: wasm.CompFunc, Results: types}
		f := tailRANativeFixtureNew(t, false, 0, 256)
		a := tailRANativeFrame(t, sig, f.a, f.ca, f.da, 0, func(e *integerEmitter) { e.tailRAProofCall(0, sig, 0, func(i int) int32 { return int32(48 + 16*i) }) })
		b := tailRANativeFrame(t, sig, f.b, f.cb, f.db, 1, func(e *integerEmitter) {
			e.restoreCallTransport()
			for i := range types {
				e.a.Constant64(enc.X5, int64(uint64(0xffffaaaa80000001)^uint64(i)))
				e.a.Sd(enc.X5, enc.X13, int32(i*8))
			}
		})
		root := tailRANativeRoot(t, a, f.witness)
		o := tailRANativeOwner(t, f, []tailRAOwnerSite{{caller: a, target: b, sourcePC: 37, sourceBase: 48, sourceStride: 16}}, root, f.e.StackLimit())
		if err := tailRANativeInvoke(t, f, o, root); err != nil {
			t.Fatal(err)
		}
		for i, typ := range types {
			want := uint64(0xffffaaaa80000001) ^ uint64(i)
			if swarClassOf(typ).narrow() {
				want = uint64(uint32(want))
			}
			if got := binary.LittleEndian.Uint64(f.results[i*8:]); got != want {
				t.Fatal("terminal raw result normalization across mask boundaries", i, got, want)
			}
		}
		f.assertReturned(t)
	})
	t.Run("unchanged-source-compiled-fp-target", func(t *testing.T) {
		sig := &wasm.CompType{Kind: wasm.CompFunc, Params: []wasm.ValType{wasm.F64}, Results: []wasm.ValType{wasm.F64}}
		f := tailRANativeFixtureNew(t, false, 1, 1)
		a := tailRANativeFrame(t, sig, f.a, f.ca, f.da, 0, func(e *integerEmitter) { e.tailRAProofCall(0, sig, 0, func(i int) int32 { return int32(48 + 16*i) }) })
		// This is existing unchanged frontend-validated framed code, not a target
		// specialized for the new exit. It never sees the private context extension.
		input := binaryModule(fixtureFunc{params: []byte{0x7c}, result: 0x7c, body: []byte{0x20, 0, 0x44, 0, 0, 0, 0, 0, 0, 0xf0, 0x3f, 0xa0, 0x0b}})
		compiled, err := compileOptions(input, scalarRegisters[:], CompileOptions{production: true, Interruptible: true})
		if err != nil {
			t.Fatal(err)
		}
		if len(compiled.functions) != 1 || compiled.functions[0].FrameHeaderBytes != 16 {
			t.Fatal("ordinary framed target metadata")
		}
		b := tailRAFunction{code: tailRAProofBuffer(t, compiled.code), snapshot: bytes.Clone(compiled.code), info: compiled.functions[0], signature: sig, job: f.b, context: f.cb[:coreruntime.InstanceContextBytes], directory: f.db}
		root := tailRANativeRoot(t, a, f.witness)
		o := tailRANativeOwner(t, f, []tailRAOwnerSite{{caller: a, target: b, sourcePC: 37, sourceBase: 48, sourceStride: 16}}, root, f.e.StackLimit())
		binary.LittleEndian.PutUint64(f.args, 0x4008000000000000)
		if err := tailRANativeInvoke(t, f, o, root); err != nil || binary.LittleEndian.Uint64(f.results) != 0x4010000000000000 {
			t.Fatal("older framed static-FP target returned through reusable exit", err)
		}
		f.assertReturned(t)
	})
}

func TestTailRAProofRV64OrdinaryCallerContinuesAfterForeignTail(t *testing.T) {
	f := tailRANativeFixtureNew(t, false, 1, 1)
	sig := &wasm.CompType{Kind: wasm.CompFunc, Params: []wasm.ValType{wasm.I64}, Results: []wasm.ValType{wasm.I64}}
	callee := tailRANativeFrame(t, sig, f.b, f.cb, f.db, 2, func(e *integerEmitter) { tailRANativeSerialize(e, sig.Results, 0) })
	middle := tailRANativeFrame(t, sig, f.a, f.ca, f.da, 1, func(e *integerEmitter) { e.tailRAProofCall(0, sig, 0, func(i int) int32 { return int32(48 + 16*i) }) })
	ordinary := tailRANativeFrame(t, sig, f.a, f.ca, f.da, 0, func(e *integerEmitter) {
		e.swarCallArguments(sig, 0, func(i int) int32 { return int32(48 + 16*i) })
		e.a.Constant64(enc.X28, int64(middle.code.Base()))
		e.a.Jalr(enc.RA, enc.X28, 0)
		e.swarCallResults(sig, 0, func(i int) int32 { return int32(48 + 16*i) })
		e.Load(5, 48, true)
		e.a.Addi(enc.X5, enc.X5, 1)
		e.Store(48, 5, true)
		tailRANativeSerialize(e, sig.Results, 0)
	})
	root := tailRANativeRoot(t, ordinary, f.witness)
	o := tailRANativeOwner(t, f, []tailRAOwnerSite{{caller: middle, target: callee, sourcePC: 37, sourceBase: 48, sourceStride: 16}}, root, f.e.StackLimit(), ordinary.code)
	binary.LittleEndian.PutUint64(f.args, 0x123456789abcdef0)
	if err := tailRANativeInvoke(t, f, o, root); err != nil || binary.LittleEndian.Uint64(f.results) != 0x123456789abcdef1 {
		t.Fatal("ordinary caller RA/result home resumed after foreign tail", err)
	}
	f.assertReturned(t)
}
