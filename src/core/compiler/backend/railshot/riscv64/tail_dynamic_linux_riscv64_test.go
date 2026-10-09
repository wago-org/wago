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

func tailDynamicBindForTest(t *testing.T, s NativeTailSite, bytes int) *NativeTailDispatcher {
	t.Helper()
	o, err := NewNativeTailDispatcher([]NativeTailSite{s}, bytes)
	if o != nil {
		t.Cleanup(func() {
			if err := o.Close(); err != nil {
				t.Error(err)
			}
		})
	}
	if err != nil {
		t.Fatal(err)
	}
	return o
}

func tailDynamicReferenceForTest(e *integerEmitter, source uintptr) {
	e.a.Constant64(enc.X29, int64(source))
	e.a.Ld(enc.X29, enc.X29, 0)
	// A descriptor is a full-width pointer even when its originating table
	// index/carrier is i32. Store it outside every canonical parameter home.
	e.a.Sd(enc.X29, enc.SP, int32(e.frameBytes-8))
}

func TestTailDynamicRV64ForeignCycleOneRecord(t *testing.T) {
	for _, shared := range []bool{false, true} {
		for _, vector := range []bool{false, true} {
			t.Run(fmt.Sprintf("shared%t-vector%t", shared, vector), func(t *testing.T) {
				payload := []wasm.ValType{wasm.I32, wasm.F32, wasm.I64, wasm.F64}
				if vector {
					payload = append(payload, wasm.V128)
				}
				sig := &wasm.CompType{Kind: wasm.CompFunc, Params: append([]wasm.ValType{wasm.I64}, payload...), Results: payload}
				f, view := tailInvocationFixtureForTest(t, shared, swarSignatureSlots(sig.Params), swarSignatureSlots(sig.Results))
				body := func(source uintptr) func(*integerEmitter) {
					return func(e *integerEmitter) {
						e.Load(5, callFrameHeader, true)
						normal := e.a.BranchLong(enc.EQ, enc.X5, enc.Zero)
						e.a.Addi(enc.X5, enc.X5, -1)
						e.Store(callFrameHeader, 5, true)
						// Compare incoming record SP across both contexts. The first hop
						// creates the record; every later activation must reuse it.
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
						if vector {
							e.Load(5, 48+5*16, true)
							e.Load(6, 48+5*16+8, true)
							e.a.Addi(enc.X7, enc.Zero, 0)
							e.a.Addi(enc.X9, enc.Zero, 0)
							e.swarAddSub(8, false)
							e.Store(48+5*16, 5, true)
							e.Store(48+5*16+8, 6, true)
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
				b := tailRANativeFrameInvocationLocals(t, sig, f.b, f.cb, f.db, 0, true, 4, body(tailRAPointer(f.witness)+232))
				oa := tailDynamicOwnerForTest(t, []tailRAFunction{a})
				ob := tailDynamicOwnerForTest(t, []tailRAFunction{b})
				a, b = oa.functions[0], ob.functions[0]
				f.da, f.db = oa.descriptors, ob.descriptors
				binary.LittleEndian.PutUint64(f.witness[224:], uint64(tailRAPointer(ob.descriptors)+40))
				binary.LittleEndian.PutUint64(f.witness[232:], uint64(tailRAPointer(oa.descriptors)+40))
				for _, fn := range []tailRAFunction{a, b} {
					if err := fn.code.Seal(); err != nil {
						t.Fatal(err)
					}
				}
				root := tailRANativeRoot(t, a, f.witness)
				if err := root.Seal(); err != nil {
					t.Fatal(err)
				}
				var owners []*NativeTailDefinedOwner
				for _, owner := range []tailRADynamicOwner{oa, ob} {
					fn, _ := tailPublishedFunctionForTest(t, owner.functions[0])
					derived, err := NewNativeTailDefinedOwner(fn.published, fn.job, owner.context, owner.descriptors, owner.directory, []int{0}, []wasm.CompType{*sig})
					if err != nil {
						t.Fatal("derived actual defined owner", err)
					}
					owners = append(owners, derived)
				}
				var dispatchers []*NativeTailDispatcher
				for _, fn := range []tailRAFunction{a, b} {
					s := tailDynamicSiteForTest(t, fn, nil, fn.info.FrameBytes-8)
					s.dynamic = nil
					s.Continuations = append(s.Continuations, NativeTailCodeRange{root.Base(), uintptr(len(root.Bytes()))})
					s, err := NativeTailDefinedSite(s, fn.info.FrameBytes-8, owners)
					if err != nil {
						t.Fatal("derived actual defined site", err)
					}
					dispatchers = append(dispatchers, tailDynamicBindForTest(t, s, len(a.snapshot)+len(b.snapshot)+len(root.Bytes())))
				}
				var want []uint64
				cell := 1
				binary.LittleEndian.PutUint64(f.args, 2049)
				for n, typ := range payload {
					v := uint64(0xffffaaaa7f800001) ^ uint64(n)*0x101010101
					binary.LittleEndian.PutUint64(f.args[cell*8:], v)
					cell++
					if swarClassOf(typ).narrow() {
						want = append(want, uint64(uint32(v)))
					} else {
						want = append(want, v)
					}
					if wasm.EqualValType(typ, wasm.V128) {
						binary.LittleEndian.PutUint64(f.args[cell*8:], ^v)
						cell++
						want = append(want, ^v)
					}
				}
				input := bytes.Clone(f.args)
				invoke := func() error {
					for i, dispatcher := range dispatchers {
						if err := dispatcher.Detach(); err != nil {
							return err
						}
						if err := dispatcher.Bind(owners[i]); err != nil {
							return err
						}
					}
					copy(f.args, input)
					clear(f.witness[:16])
					f.bind(t, f.e.StackLimit())
					f.a.SetRV64InvocationView(view)
					return f.e.CallPrepared(root.Base(), f.args, f.a.LinMemBase(), f.trap, f.results)
				}
				if err := invoke(); err != nil {
					t.Fatal(err)
				}
				for n, v := range want {
					if got := binary.LittleEndian.Uint64(f.results[n*8:]); got != v {
						t.Fatalf("cell%d=%x want%x", n, got, v)
					}
				}
				if binary.LittleEndian.Uint64(f.witness) == 0 || binary.LittleEndian.Uint64(f.witness[8:]) != 0 {
					t.Fatal("dynamic cycle grew stack", f.witness[:16])
				}
				f.assertReturned(t)
				if f.a.RV64InvocationView() != view {
					t.Fatal("original parent view lost")
				}
				if n := testing.AllocsPerRun(4, func() {
					if err := invoke(); err != nil {
						panic(err)
					}
				}); n != 0 {
					t.Fatal("warm dynamic allocations", n)
				}
			})
		}
	}
}

func TestTailDynamicRV64RejectsBeforeArgumentPublication(t *testing.T) {
	tailDynamicRV64RejectsBeforeArgumentPublication(t, false)
}
func TestTailDynamicRV64FrozenSignatureAuthentication(t *testing.T) {
	tailDynamicRV64RejectsBeforeArgumentPublication(t, true)
}
func tailDynamicRV64RejectsBeforeArgumentPublication(t *testing.T, frozen bool) {
	sig := &wasm.CompType{Kind: wasm.CompFunc, Params: []wasm.ValType{wasm.I32}, Results: []wasm.ValType{wasm.I32}}
	f, view := tailInvocationFixtureForTest(t, false, 1, 1)
	a := tailRANativeFrameInvocation(t, sig, f.a, f.ca, f.da, 0, true, func(e *integerEmitter) {
		tailDynamicReferenceForTest(e, tailRAPointer(f.witness)+224)
		e.tailRAProofCall(0, sig, 0, func(i int) int32 { return int32(48 + i*16) })
	})
	b := tailRANativeFrameInvocation(t, sig, f.b, f.cb, f.db, 0, true, func(e *integerEmitter) { tailRANativeSerialize(e, sig.Results, 0) })
	wrongSig := &wasm.CompType{Kind: wasm.CompFunc, Params: []wasm.ValType{wasm.F32}, Results: []wasm.ValType{wasm.I32}}
	wrong := tailRANativeFrameInvocation(t, wrongSig, f.b, f.cb, f.db, 1, true, func(e *integerEmitter) { tailRANativeSerialize(e, wrongSig.Results, 0) })
	oa := tailDynamicOwnerForTest(t, []tailRAFunction{a})
	ob := tailDynamicOwnerForTest(t, []tailRAFunction{b, wrong})
	a, b = oa.functions[0], ob.functions[0]
	f.da, f.db = oa.descriptors, ob.descriptors
	for _, fn := range []tailRAFunction{a, b, ob.functions[1]} {
		if err := fn.code.Seal(); err != nil {
			t.Fatal(err)
		}
	}
	if frozen {
		ob.signatureKeys = []uint64{wasm.StructuralFuncTypeKey(sig), wasm.StructuralFuncTypeKey(wrongSig)}
	}
	tailDynamicBindForTest(t, tailDynamicSiteForTest(t, a, []tailRADynamicOwner{ob}, a.info.FrameBytes-8), len(a.snapshot)+len(b.snapshot)+len(wrong.snapshot))
	desc, dir, ctx := bytes.Clone(ob.descriptors), bytes.Clone(ob.directory), bytes.Clone(ob.context)
	cases := []struct {
		name   string
		mutate func()
	}{
		{"nil", func() { binary.LittleEndian.PutUint64(f.witness[224:], 0) }},
		{"unmapped", func() { binary.LittleEndian.PutUint64(f.witness[224:], 1) }},
		{"wrapped", func() { binary.LittleEndian.PutUint64(f.witness[224:], ^uint64(0)-7) }},
		{"anchor", func() { binary.LittleEndian.PutUint64(f.witness[224:], uint64(tailRAPointer(ob.descriptors))) }},
		{"interior", func() { binary.LittleEndian.PutUint64(f.witness[224:], uint64(tailRAPointer(ob.descriptors)+48)) }},
		{"self", func() { binary.LittleEndian.PutUint64(ob.descriptors[64:], 1) }},
		{"signature", func() { binary.LittleEndian.PutUint64(ob.descriptors[48:], 1) }},
		{"context", func() { binary.LittleEndian.PutUint64(ob.descriptors[72:], 1) }},
		{"home-tag", func() { binary.LittleEndian.PutUint64(ob.descriptors[56:], uint64(f.b.LinMemBase())) }},
		{"anchor-context", func() { binary.LittleEndian.PutUint64(ob.descriptors[32:], 1) }},
		{"context-anchor", func() { binary.LittleEndian.PutUint64(ob.context[16:], 1) }},
		{"directory-anchor", func() { binary.LittleEndian.PutUint64(ob.descriptors[8:], 1) }},
		{"directory-magic", func() { binary.LittleEndian.PutUint64(ob.directory, 0) }},
		{"directory-version", func() { binary.LittleEndian.PutUint32(ob.directory[8:], 2) }},
		{"directory-count", func() { binary.LittleEndian.PutUint32(ob.directory[12:], 0) }},
		{"directory-count-expanded", func() { binary.LittleEndian.PutUint32(ob.directory[12:], 3) }},
		{"frozen-signature", func() {
			binary.LittleEndian.PutUint64(f.witness[224:], uint64(tailRAPointer(ob.descriptors)+80))
			binary.LittleEndian.PutUint64(ob.descriptors[88:], wasm.StructuralFuncTypeKey(sig))
		}},
		{"directory-owner", func() { binary.LittleEndian.PutUint64(ob.directory[16:], 1) }},
		{"directory-context", func() { binary.LittleEndian.PutUint64(ob.directory[24:], 1) }},
		{"zero-entry", func() { binary.LittleEndian.PutUint64(ob.directory[32:], 0) }},
		{"foreign-home", func() { binary.LittleEndian.PutUint64(ob.directory[40:], uint64(f.a.LinMemBase())) }},
		{"foreign-context", func() { binary.LittleEndian.PutUint64(ob.directory[48:], uint64(tailRAPointer(f.ca))) }},
		{"host-entry", func() { binary.LittleEndian.PutUint64(ob.directory[56:], abi.RV64TailTargetsHost) }},
		{"coordinated-entry", func() {
			binary.LittleEndian.PutUint64(ob.directory[32:], 0x10000)
			binary.LittleEndian.PutUint64(ob.descriptors[40:], 0x10000)
		}},
	}
	if frozen {
		cases = append(cases, struct {
			name   string
			mutate func()
		}{"authentic-other-signature", func() {
			binary.LittleEndian.PutUint64(f.witness[224:], uint64(tailRAPointer(ob.descriptors)+80))
		}}, struct {
			name   string
			mutate func()
		}{"frozen-wrong-entry", func() {
			binary.LittleEndian.PutUint64(f.witness[224:], uint64(tailRAPointer(ob.descriptors)+80))
			binary.LittleEndian.PutUint64(ob.descriptors[80:], 0x10000)
			binary.LittleEndian.PutUint64(ob.directory[64:], 0x10000)
		}})
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			copy(ob.descriptors, desc)
			copy(ob.directory, dir)
			copy(ob.context, ctx)
			binary.LittleEndian.PutUint64(f.witness[224:], uint64(tailRAPointer(ob.descriptors)+40))
			tc.mutate()
			binary.LittleEndian.PutUint64(f.args, 0xffffaaaa80000001)
			binary.LittleEndian.PutUint64(f.results, 0xfeeddeadbeeffeed)
			f.bind(t, f.e.StackLimit())
			f.a.SetRV64InvocationView(view)
			err := f.e.CallPrepared(a.code.Base(), f.args, f.a.LinMemBase(), f.trap, f.results)
			var trap *coreruntime.TrapError
			want := coreruntime.TrapCode(abi.TrapCalledFnNotLinked)
			if frozen && tc.name == "authentic-other-signature" {
				want = coreruntime.TrapCode(abi.TrapIndirectWrongSig)
			}
			if !errors.As(err, &trap) || trap.Code != want {
				t.Fatal("bad reference", err)
			}
			if len(trap.Frames) != 1 || trap.Frames[0].ProgramCounter != 37 {
				t.Fatal("source trap attribution", trap.Frames)
			}
			if binary.LittleEndian.Uint64(f.args) != 0xffffaaaa80000001 || binary.LittleEndian.Uint64(f.results) != 0xfeeddeadbeeffeed {
				t.Fatal("guard wrote original transport")
			}
			f.assertCanaries(t)
		})
	}
	copy(ob.descriptors, desc)
	copy(ob.directory, dir)
	copy(ob.context, ctx)
	binary.LittleEndian.PutUint64(f.witness[224:], uint64(tailRAPointer(ob.descriptors)+40))
	f.bind(t, f.e.StackLimit())
	f.a.SetRV64InvocationView(view)
	if err := f.e.CallPrepared(a.code.Base(), f.args, f.a.LinMemBase(), f.trap, f.results); err != nil || binary.LittleEndian.Uint64(f.results) != 0x80000001 {
		t.Fatal("cold reuse", err)
	}
}
