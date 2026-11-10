//go:build linux && riscv64 && !tinygo && !wago_target_tinygo

package riscv64

import (
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
	enc "github.com/wago-org/wago/src/core/encoder/riscv64"
	coreruntime "github.com/wago-org/wago/src/core/runtime"
	"github.com/wago-org/wago/src/core/runtime/abi"
)

type tailDynamicProxyFixture struct {
	f           *tailRANativeFixture
	view        uintptr
	a, b        tailRAFunction
	oa, ob      tailRADynamicOwner
	root        *coreruntime.CodeBuffer
	dispatchers []*NativeTailDispatcher
	sig         *wasm.CompType
	input       []byte
}

func tailDynamicProxyFixtureForTest(t *testing.T, shared, vector bool) *tailDynamicProxyFixture {
	t.Helper()
	payload := []wasm.ValType{wasm.I32, wasm.F32, wasm.I64, wasm.F64}
	if vector {
		payload = append(payload, wasm.V128)
	}
	sig := &wasm.CompType{Kind: wasm.CompFunc, Params: append([]wasm.ValType{wasm.I64}, payload...), Results: payload}
	f, view := tailInvocationFixtureForTest(t, shared, swarSignatureSlots(sig.Params), swarSignatureSlots(sig.Results))
	x := &tailDynamicProxyFixture{f: f, view: view, sig: sig}
	exit, err := nativeTailExitPointer()
	if err != nil {
		t.Fatal(err)
	}
	body := func(source uintptr) func(*integerEmitter) {
		return func(e *integerEmitter) {
			flag := int32(1)
			if source == tailRAPointer(f.witness)+232 {
				flag = 2
			}
			e.a.Constant64(enc.X28, int64(tailRAPointer(f.witness)+244))
			e.a.Lwu(enc.X5, enc.X28, 0)
			e.a.Addi(enc.X6, enc.Zero, flag)
			e.a.Or(enc.X5, enc.X5, enc.X6)
			e.a.Sw(enc.X5, enc.X28, 0)
			e.Load(5, 48, true)
			e.a.Constant64(enc.X6, 2049)
			root := e.a.BranchLong(enc.EQ, enc.X5, enc.X6)
			// The same real producer is callable via its ordinary NativeImportThunk.
			// Such a linked bridge gives it a different incoming RA. A valid proper
			// tail must enter with the common exit and leave this marker untouched.
			e.a.Ld(enc.X5, enc.SP, 0)
			e.a.Constant64(enc.X6, int64(exit))
			tail := e.a.BranchLong(enc.EQ, enc.X5, enc.X6)
			e.a.Constant64(enc.X28, int64(tailRAPointer(f.witness)+240))
			e.a.Addi(enc.X5, enc.Zero, 1)
			e.a.Sw(enc.X5, enc.X28, 0)
			if err := e.a.PatchLong(tail, int64(e.a.Len())); err != nil {
				t.Fatal(err)
			}
			if err := e.a.PatchLong(root, int64(e.a.Len())); err != nil {
				t.Fatal(err)
			}
			e.Load(5, 48, true)
			normal := e.a.BranchLong(enc.EQ, enc.X5, enc.Zero)
			e.a.Addi(enc.X5, enc.X5, -1)
			e.Store(48, 5, true)
			e.a.Constant64(enc.X28, int64(tailRAPointer(f.witness)))
			e.a.Constant64(enc.X6, 2048)
			firstHop := e.a.BranchLong(enc.EQ, enc.X5, enc.X6)
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
			if err := e.a.PatchLong(firstHop, int64(e.a.Len())); err != nil {
				t.Fatal(err)
			}
			tailDynamicReferenceForTest(e, source)
			e.tailRAProofCall(0, sig, 0, func(i int) int32 { return int32(48 + i*16) })
			if err := e.a.PatchLong(normal, int64(e.a.Len())); err != nil {
				t.Fatal(err)
			}
			tailRANativeSerialize(e, payload, 1)
		}
	}
	a := tailRANativeFrameInvocation(t, sig, f.a, f.ca, f.da, 0, true, body(tailRAPointer(f.witness)+224))
	b := tailRANativeFrameInvocationLocals(t, sig, f.b, f.cb, f.db, 1, true, 4, body(tailRAPointer(f.witness)+232))
	leaf := tailRANativeFrameInvocation(t, sig, f.b, f.cb, f.db, 2, true, func(e *integerEmitter) { tailRANativeSerialize(e, payload, 1) })
	cold := tailRANativeFrameInvocation(t, sig, f.b, f.cb, f.db, 3, true, func(e *integerEmitter) { e.emitProductionTrap(abi.TrapUnreachable, 53) })
	p, pp := tailDynamicProxyForTest(t, a, b, 0)
	q, qp := tailDynamicProxyForTest(t, b, a, 0)
	x.oa = tailDynamicOwnerForTest(t, []tailRAFunction{a, p})
	x.ob = tailDynamicOwnerForTest(t, []tailRAFunction{b, q, leaf, cold})
	x.a, x.b = x.oa.functions[0], x.ob.functions[0]
	f.da, f.db = x.oa.descriptors, x.ob.descriptors
	tailDynamicProxyRowsForTest(&x.oa, []tailRAFunction{x.a, x.b}, []*tailRADynamicProxy{nil, pp})
	tailDynamicProxyRowsForTest(&x.ob, []tailRAFunction{x.b, x.a, x.ob.functions[2], x.ob.functions[3]}, []*tailRADynamicProxy{nil, qp, nil, nil})
	binary.LittleEndian.PutUint64(f.witness[224:], uint64(tailRAPointer(x.oa.descriptors)+80))
	binary.LittleEndian.PutUint64(f.witness[232:], uint64(tailRAPointer(x.ob.descriptors)+80))
	x.root = tailRANativeRoot(t, x.a, f.witness)
	if err := x.root.Seal(); err != nil {
		t.Fatal(err)
	}
	guest := len(a.snapshot) + len(b.snapshot) + len(p.snapshot) + len(q.snapshot) + len(leaf.snapshot) + len(cold.snapshot) + len(x.root.Bytes())
	for _, fn := range []tailRAFunction{x.a, x.b} {
		s := tailDynamicSiteForTest(t, fn, []tailRADynamicOwner{x.oa, x.ob}, fn.info.FrameBytes-8)
		s.Continuations = append(s.Continuations, NativeTailCodeRange{x.root.Base(), uintptr(len(x.root.Bytes()))})
		x.dispatchers = append(x.dispatchers, tailDynamicBindForTest(t, s, guest))
	}
	cell := 1
	binary.LittleEndian.PutUint64(f.args, 2049)
	for n, typ := range payload {
		v := uint64(0xffffaaaa7f800001) ^ uint64(n)*0x101010101
		binary.LittleEndian.PutUint64(f.args[cell*8:], v)
		cell++
		if wasm.EqualValType(typ, wasm.V128) {
			binary.LittleEndian.PutUint64(f.args[cell*8:], ^v)
			cell++
		}
	}
	x.input = bytes.Clone(f.args)
	return x
}

func (x *tailDynamicProxyFixture) invoke(t *testing.T, entry uintptr, count uint64) error {
	t.Helper()
	copy(x.f.args, x.input)
	binary.LittleEndian.PutUint64(x.f.args, count)
	x.f.bind(t, x.f.e.StackLimit())
	x.f.a.SetRV64InvocationView(x.view)
	return x.f.e.CallPrepared(entry, x.f.args, x.f.a.LinMemBase(), x.f.trap, x.f.results)
}

func (x *tailDynamicProxyFixture) resultsMatch() bool {
	cell := 0
	for _, typ := range x.sig.Results {
		want := binary.LittleEndian.Uint64(x.input[(cell+1)*8:])
		if swarClassOf(typ).narrow() {
			want = uint64(uint32(want))
		}
		if binary.LittleEndian.Uint64(x.f.results[cell*8:]) != want {
			return false
		}
		cell++
		if wasm.EqualValType(typ, wasm.V128) {
			if binary.LittleEndian.Uint64(x.f.results[cell*8:]) != binary.LittleEndian.Uint64(x.input[(cell+1)*8:]) {
				return false
			}
			cell++
		}
	}
	return true
}

func TestTailDynamicProxyRV64CanonicalIdentityAndUnlinkedCycle(t *testing.T) {
	for _, shared := range []bool{false, true} {
		for _, vector := range []bool{false, true} {
			t.Run(fmt.Sprintf("shared%t-vector%t", shared, vector), func(t *testing.T) {
				x := tailDynamicProxyFixtureForTest(t, shared, vector)
				f := x.f
				ca, cb := bytes.Clone(x.oa.descriptors), bytes.Clone(x.ob.descriptors)
				// Prove the canonical descriptor remains an actual ordinary import bridge.
				if err := x.invoke(t, x.oa.functions[1].code.Base(), 0); err != nil || !x.resultsMatch() || binary.LittleEndian.Uint32(f.witness[240:]) != 1 {
					t.Fatal("ordinary proxy bridge witness", err)
				}
				clear(f.witness[:16])
				clear(f.witness[240:244])
				if err := x.invoke(t, x.root.Base(), 2049); err != nil || !x.resultsMatch() {
					t.Fatal("proper proxy cycle", err)
				}
				if binary.LittleEndian.Uint64(f.witness) == 0 || binary.LittleEndian.Uint64(f.witness[8:]) != 0 || binary.LittleEndian.Uint32(f.witness[240:]) != 0 {
					t.Fatal("proxy cycle used linked bridge/grew stack")
				}
				if !bytes.Equal(ca, x.oa.descriptors) || !bytes.Equal(cb, x.ob.descriptors) {
					t.Fatal("canonical proxy identity rewritten")
				}
				f.assertReturned(t)
				if f.a.RV64InvocationView() != x.view {
					t.Fatal("original parent view lost")
				}
				if n := testing.AllocsPerRun(4, func() {
					clear(f.witness[:16])
					if err := x.invoke(t, x.root.Base(), 2049); err != nil {
						panic(err)
					}
				}); n != 0 {
					t.Fatal("warm proxy allocations", n)
				}
			})
		}
	}
}

func TestTailDynamicProxyRV64FrozenGuardsAndColdReuse(t *testing.T) {
	for _, shared := range []bool{false, true} {
		t.Run(fmt.Sprintf("shared%t", shared), func(t *testing.T) {
			x := tailDynamicProxyFixtureForTest(t, shared, false)
			f := x.f
			da, ta, db, cb := bytes.Clone(x.oa.descriptors), bytes.Clone(x.oa.directory), bytes.Clone(x.ob.descriptors), bytes.Clone(x.ob.context)
			for _, tc := range []struct {
				name   string
				mutate func()
			}{
				{"proxy-entry", func() { binary.LittleEndian.PutUint64(x.oa.descriptors[80:], uint64(x.b.code.Base())) }},
				{"proxy-tag", func() {
					binary.LittleEndian.PutUint64(x.oa.descriptors[96:], uint64(f.b.LinMemBase())|abi.FuncRefCrossInstanceHomeTag)
				}},
				{"proxy-self", func() { binary.LittleEndian.PutUint64(x.oa.descriptors[104:], 1) }},
				{"proxy-key", func() { binary.LittleEndian.PutUint64(x.oa.descriptors[88:], 0) }},
				{"proxy-context", func() { binary.LittleEndian.PutUint64(x.oa.descriptors[112:], uint64(tailRAPointer(x.ob.context))) }},
				{"target-unmapped-context", func() { binary.LittleEndian.PutUint64(x.oa.directory[80:], 1) }},
				{"target-home", func() { binary.LittleEndian.PutUint64(x.oa.directory[72:], 1) }},
				{"ordinary-bridge-row", func() { binary.LittleEndian.PutUint64(x.oa.directory[64:], uint64(x.oa.functions[1].code.Base())) }},
				{"coordinated-source-target", func() {
					binary.LittleEndian.PutUint64(x.oa.descriptors[80:], uint64(x.a.code.Base()))
					binary.LittleEndian.PutUint64(x.oa.directory[64:], uint64(x.a.code.Base()))
					binary.LittleEndian.PutUint64(x.oa.directory[72:], uint64(f.a.LinMemBase()))
					binary.LittleEndian.PutUint64(x.oa.directory[80:], uint64(tailRAPointer(f.ca)))
				}},
				{"producer-context-anchor", func() { binary.LittleEndian.PutUint64(x.ob.context[16:], 1) }},
				{"producer-anchor-context", func() { binary.LittleEndian.PutUint64(x.ob.descriptors[32:], 1) }},
				{"producer-anchor-home", func() { binary.LittleEndian.PutUint64(x.ob.descriptors[16:], 1) }},
			} {
				t.Run(tc.name, func(t *testing.T) {
					copy(x.oa.descriptors, da)
					copy(x.oa.directory, ta)
					copy(x.ob.descriptors, db)
					copy(x.ob.context, cb)
					tc.mutate()
					copy(f.args, x.input)
					binary.LittleEndian.PutUint64(f.args, 1)
					before := bytes.Clone(f.args)
					for i := range f.results {
						f.results[i] = 0xa5
					}
					f.bind(t, f.e.StackLimit())
					f.a.SetRV64InvocationView(x.view)
					err := f.e.CallPrepared(x.a.code.Base(), f.args, f.a.LinMemBase(), f.trap, f.results)
					var trap *coreruntime.TrapError
					if !errors.As(err, &trap) || trap.Code != abi.TrapCalledFnNotLinked || len(trap.Frames) != 1 || trap.Frames[0].ProgramCounter != 37 {
						t.Fatal("proxy guard attribution", err)
					}
					if !bytes.Equal(before, f.args) || !bytes.Equal(f.results, bytes.Repeat([]byte{0xa5}, len(f.results))) {
						t.Fatal("proxy guard published transport")
					}
					f.assertCanaries(t)
				})
			}
			copy(x.oa.descriptors, da)
			copy(x.oa.directory, ta)
			copy(x.ob.descriptors, db)
			copy(x.ob.context, cb)
			// Changing the canonical reference selects other genuine retained rows;
			// row metadata itself stays immutable. Cold exits skip the normal record.
			binary.LittleEndian.PutUint64(f.witness[224:], uint64(tailRAPointer(x.ob.descriptors)+160))
			err := x.invoke(t, x.root.Base(), 1)
			var trap *coreruntime.TrapError
			if !errors.As(err, &trap) || trap.Code != abi.TrapUnreachable || len(trap.Frames) != 1 || trap.Frames[0].FunctionIndex != 3 || trap.Frames[0].ProgramCounter != 53 {
				t.Fatal("proxy cold target", err)
			}
			binary.LittleEndian.PutUint64(f.witness[224:], uint64(tailRAPointer(x.ob.descriptors)+120))
			if err := x.invoke(t, x.root.Base(), 1); err != nil || !x.resultsMatch() {
				t.Fatal("proxy cold reuse", err)
			}
			f.assertReturned(t)
		})
	}
}
