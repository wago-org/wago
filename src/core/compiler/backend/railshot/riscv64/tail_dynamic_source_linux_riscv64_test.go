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
	coreruntime "github.com/wago-org/wago/src/core/runtime"
)

type dynamicTailSourceFixture struct {
	f      *tailRANativeFixture
	view   uintptr
	i      *tailImportImage
	root   *coreruntime.CodeBuffer
	owners []tailRADynamicOwner
	tables [][]byte
	input  []byte
}

func dynamicTailSourceFixtureForTest(t *testing.T, force, vector, shared bool) *dynamicTailSourceFixture {
	return dynamicTailSourceFixtureWithKind(t, force, vector, shared, 1, false)
}

func dynamicTailSourceFixtureWithKind(t *testing.T, force, vector, shared bool, kind uint32, null bool) *dynamicTailSourceFixture {
	t.Helper()
	payload := []wasm.ValType{wasm.I32, wasm.F32, wasm.I64, wasm.F64}
	if vector {
		payload = append(payload, wasm.V128)
	}
	payload = append(payload, wasm.I32)
	params := append([]wasm.ValType{wasm.I64}, payload...)
	body := dynamicTailCountdownBody(len(payload))
	// A real guest table.set is observable before either native producer tails.
	// The last raw i32 parameter supplies the indirect index on every hop.
	body = append([]byte{0x41, 1, 0xd2, 1, 0x26, 0}, body...)
	if kind == 1 {
		body = append(body[:len(body)-7], 0x20, byte(len(params)-1), 0x13, 2, 0, 0x0b, 0x0b)
	} else {
		referenceOp := byte(0xd2)
		referenceIndex := byte(0) // ordinary imported canonical proxy
		if null {
			referenceOp, referenceIndex = 0xd0, 2 // typed nullable reference
		}
		body = append(body[:len(body)-7], referenceOp, referenceIndex, 0x15, 2, 0x0b, 0x0b)
	}
	source := dynamicTailTestSource(params, payload, body)
	a, err := compileDynamicTailSource(source, 0, force)
	if err != nil {
		t.Fatal(err)
	}
	b, err := compileDynamicTailSource(source, 0, force)
	if err != nil {
		t.Fatal(err)
	}
	f, view := tailInvocationFixtureForTest(t, shared, swarSignatureSlots(params), swarSignatureSlots(payload))
	af, bf := tailImportNativeFunctions(t, a, f.a, f.ca, f.da), tailImportNativeFunctions(t, b, f.b, f.cb, f.db)
	p, pp := tailDynamicProxyForTest(t, af[0], bf[0], 0)
	q, qp := tailDynamicProxyForTest(t, bf[0], af[0], 0)
	oa := tailDynamicOwnerForTest(t, []tailRAFunction{p, af[0]})
	ob := tailDynamicOwnerForTest(t, []tailRAFunction{q, bf[0]})
	af[0], bf[0] = oa.functions[1], ob.functions[1]
	f.da, f.db = oa.descriptors, ob.descriptors
	tailDynamicProxyRowsForTest(&oa, []tailRAFunction{bf[0], af[0]}, []*tailRADynamicProxy{pp, nil})
	tailDynamicProxyRowsForTest(&ob, []tailRAFunction{af[0], bf[0]}, []*tailRADynamicProxy{qp, nil})
	ar, err := coreruntime.NewArena(160)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := ar.Close(); err != nil {
			t.Error(err)
		}
	})
	x := &dynamicTailSourceFixture{f: f, view: view, i: a, owners: []tailRADynamicOwner{oa, ob}}
	for _, o := range x.owners {
		table := ar.Alloc(8 + 2*32)
		binary.LittleEndian.PutUint32(table, 2)
		binary.LittleEndian.PutUint32(table[4:], 2)
		// Canonical descriptor slot1 is the ordinary imported proxy. Slot2 is
		// the real local definition written to table[1] by the guest prelude.
		copy(table[8:32], o.descriptors[40:64])
		binary.LittleEndian.PutUint64(table[32:], uint64(tailRAPointer(o.descriptors)+40))
		binary.LittleEndian.PutUint64(o.context[8:], uint64(tailRAPointer(table)))
		x.tables = append(x.tables, table)
	}
	x.root = tailRANativeRoot(t, af[0], f.witness)
	if err := x.root.Seal(); err != nil {
		t.Fatal(err)
	}
	guest := len(a.code) + len(b.code) + len(p.snapshot) + len(q.snapshot) + len(x.root.Bytes())
	for n, image := range []*tailImportImage{a, b} {
		for _, plan := range image.plans {
			if plan.kind != kind || plan.typeIndex != 2 || plan.functionIndex != 1 {
				t.Fatal("exact source plan", plan)
			}
			caller := x.owners[n].functions[plan.functionIndex]
			sig := image.types[plan.typeIndex]
			s := tailDynamicSiteForTest(t, caller, x.owners, plan.referenceBase)
			s.Parameters, s.Results = sig.Params, sig.Results
			s.SourceBase, s.SourceStride = plan.sourceBase, plan.sourceStride
			s.SourcePC = plan.sourcePC
			s.dynamic.key = wasm.StructuralFuncTypeKey(sig)
			s.Continuations = append(s.Continuations, NativeTailCodeRange{x.root.Base(), uintptr(len(x.root.Bytes()))})
			tailDynamicBindForTest(t, s, guest)
		}
	}
	cell := 1
	binary.LittleEndian.PutUint64(f.args, 2049)
	for n, typ := range payload {
		v := uint64(0xffffaaaa7f800001) ^ uint64(n)*0x101010101
		if n == len(payload)-1 {
			v = 0xffffaaaa00000000
		}
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

func (x *dynamicTailSourceFixture) prepare(t *testing.T, count uint64) {
	t.Helper()
	copy(x.f.args, x.input)
	binary.LittleEndian.PutUint64(x.f.args, count)
	x.f.bind(t, x.f.e.StackLimit())
	x.f.a.SetRV64InvocationView(x.view)
}

func TestTailDynamicSourceRV64DecodedImportedCycles(t *testing.T) {
	for _, force := range []bool{false, true} {
		for _, vector := range []bool{false, true} {
			for _, shared := range []bool{false, true} {
				t.Run(fmt.Sprintf("force%t-vector%t-shared%t", force, vector, shared), func(t *testing.T) {
					x := dynamicTailSourceFixtureForTest(t, force, vector, shared)
					f := x.f
					x.prepare(t, 2049)
					if err := f.e.CallPrepared(x.root.Base(), f.args, f.a.LinMemBase(), f.trap, f.results); err != nil {
						t.Fatal("decoded proper indirect cycle", err)
					}
					cell := 0
					for _, typ := range x.i.types[2].Results {
						v := binary.LittleEndian.Uint64(x.input[(cell+1)*8:])
						if swarClassOf(typ).narrow() {
							v = uint64(uint32(v))
						}
						if binary.LittleEndian.Uint64(f.results[cell*8:]) != v {
							t.Fatal("raw typed result", cell)
						}
						cell++
						if wasm.EqualValType(typ, wasm.V128) {
							if binary.LittleEndian.Uint64(f.results[cell*8:]) != binary.LittleEndian.Uint64(x.input[(cell+1)*8:]) {
								t.Fatal("vector high cell")
							}
							cell++
						}
					}
					for n, table := range x.tables {
						if binary.LittleEndian.Uint64(table[64:]) != uint64(tailRAPointer(x.owners[n].descriptors)+80) {
							t.Fatal("guest table mutation not observed", n)
						}
					}
					f.assertReturned(t)
					if f.a.RV64InvocationView() != x.view {
						t.Fatal("original parent view lost")
					}
					if alloc := testing.AllocsPerRun(4, func() {
						x.prepare(t, 2049)
						if err := f.e.CallPrepared(x.root.Base(), f.args, f.a.LinMemBase(), f.trap, f.results); err != nil {
							panic(err)
						}
					}); alloc != 0 {
						t.Fatal("warm decoded tail allocation", alloc)
					}
				})
			}
		}
	}
}

func TestTailDynamicSourceRV64PrewriteTableAndTokenGuards(t *testing.T) {
	x := dynamicTailSourceFixtureForTest(t, false, false, false)
	f := x.f
	table := bytes.Clone(x.tables[0])
	plan := x.i.plans[0]
	for _, tc := range []struct {
		name   string
		code   coreruntime.TrapCode
		mutate func()
	}{
		{"bounds", coreruntime.TrapIndirectOutOfBounds, func() { binary.LittleEndian.PutUint64(f.args[len(f.args)-8:], 0xffffaaaa00000002) }},
		{"null-entry", coreruntime.TrapIndirectOutOfBounds, func() { binary.LittleEndian.PutUint64(x.tables[0][8:], 0) }},
		{"wrong-type", coreruntime.TrapIndirectWrongSig, func() { binary.LittleEndian.PutUint64(x.tables[0][16:], wasm.StructuralFuncTypeKey(x.i.types[1])) }},
		{"nil-reference", coreruntime.TrapIndirectOutOfBounds, func() { binary.LittleEndian.PutUint64(x.tables[0][32:], 0) }},
		{"unmapped-reference", coreruntime.TrapCalledFnNotLinked, func() { binary.LittleEndian.PutUint64(x.tables[0][32:], 1) }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			copy(x.tables[0], table)
			x.prepare(t, 1)
			tc.mutate()
			if tc.name == "wrong-type" && binary.LittleEndian.Uint64(x.tables[0][16:]) == binary.LittleEndian.Uint64(table[16:]) {
				t.Fatal("wrong-type fixture did not change the table key")
			}
			before := bytes.Clone(f.args)
			for i := range f.results {
				f.results[i] = 0xa5
			}
			err := f.e.CallPrepared(x.root.Base(), f.args, f.a.LinMemBase(), f.trap, f.results)
			var trap *coreruntime.TrapError
			if !errors.As(err, &trap) || trap.Code != tc.code || len(trap.Frames) != 1 || trap.Frames[0].FunctionIndex != 1 || trap.Frames[0].ProgramCounter != plan.sourcePC {
				t.Fatal("source guard attribution", err)
			}
			if !bytes.Equal(before, f.args) || !bytes.Equal(f.results, bytes.Repeat([]byte{0xa5}, len(f.results))) {
				t.Fatal("source guard published transport")
			}
			f.assertCanaries(t)
		})
	}
}

func TestTailDynamicSourceRV64ActiveMutationCycleInterrupt(t *testing.T) {
	tailSourceActiveCycleInterrupt(t, 1)
}

func tailSourceActiveCycleInterrupt(t *testing.T, kind uint32) {
	t.Helper()
	previous := runtime.GOMAXPROCS(1)
	defer runtime.GOMAXPROCS(previous)
	x := dynamicTailSourceFixtureWithKind(t, true, true, false, kind, false)
	f := x.f
	for _, table := range x.tables {
		clear(table[40:])
	}
	for i := range f.results {
		f.results[i] = 0xa5
	}
	x.prepare(t, ^uint64(0))
	done := make(chan error, 1)
	go func() { done <- f.e.CallPrepared(x.root.Base(), f.args, f.a.LinMemBase(), f.trap, f.results) }()
	deadline, started := time.Now().Add(2*time.Second), false
	for time.Now().Before(deadline) {
		a := atomic.LoadUint64((*uint64)(unsafe.Pointer(&x.tables[0][64])))
		b := atomic.LoadUint64((*uint64)(unsafe.Pointer(&x.tables[1][64])))
		if a != 0 && b != 0 {
			started = true
			break
		}
		select {
		case err := <-done:
			t.Fatal("decoded active cycle returned", err)
		default:
		}
		time.Sleep(100 * time.Microsecond)
	}
	stop := coreruntime.RequestInterruptAsync(f.trap)
	err := <-done
	stop()
	var trap *coreruntime.TrapError
	if !started || !errors.As(err, &trap) || trap.Code != coreruntime.TrapInterrupted || len(trap.Frames) != 1 || trap.Frames[0].FunctionIndex != 1 || trap.Frames[0].ProgramCounter != 1 {
		t.Fatal("decoded active entry poll", err)
	}
	if !bytes.Equal(f.results, bytes.Repeat([]byte{0xa5}, len(f.results))) {
		t.Fatal("cancelled source cycle committed result")
	}
	x.prepare(t, 2)
	if err := f.e.CallPrepared(x.root.Base(), f.args, f.a.LinMemBase(), f.trap, f.results); err != nil {
		t.Fatal("decoded cancellation cold reuse", err)
	}
	f.assertReturned(t)
}
