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
	"github.com/wago-org/wago/src/core/runtime/abi"
	"github.com/wago-org/wago/tests/support/wasmtest"
)

func tailImportNativeFunctions(t *testing.T, i *tailImportImage, job *coreruntime.JobMemory, ctx, dir []byte) []tailRAFunction {
	t.Helper()
	code := tailRAProofBuffer(t, i.code)
	snapshot := bytes.Clone(i.code)
	var functions []tailRAFunction
	for n, info := range i.functions {
		functions = append(functions, tailRAFunction{code: code, snapshot: snapshot, info: info, signature: i.signatures[i.imports+n], job: job, context: ctx, directory: dir})
	}
	return functions
}

func tailImportNativeOwner(t *testing.T, f *tailRANativeFixture, bindings []tailImportBinding, root *coreruntime.CodeBuffer) *tailRAOwner {
	t.Helper()
	exit, err := tailRAExitCode()
	if err != nil {
		t.Fatal(err)
	}
	trampoline, err := coreruntime.HostCallTrampolinePointer()
	if err != nil {
		t.Fatal(err)
	}
	s := tailRAOwnerSpec{exit: tailRAProofBuffer(t, exit), continuations: []tailRARange{{root.Base(), uintptr(len(root.Bytes()))}}, retained: []any{f, root}, stackLimit: f.e.StackLimit(), stackTop: f.e.StackTop(), hostTrampoline: trampoline}
	if len(f.args) != 0 {
		s.arguments = []tailRARange{{tailRAPointer(f.args), uintptr(len(f.args))}}
	}
	if len(f.results) != 0 {
		s.results = []tailRARange{{tailRAPointer(f.results), uintptr(len(f.results))}}
	}
	s, err = tailImportOwnerSpec(bindings, s)
	if err != nil {
		t.Fatal(err)
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
	if err := root.Seal(); err != nil {
		t.Fatal(err)
	}
	return o
}

func tailImportCountdownBody(payload int) []byte {
	// Real Wasm: if count==0 return payload, else tail-import(count-1,payload).
	b := []byte{0x20, 0, 0x50, 0x04, 1}
	for n := 0; n < payload; n++ {
		b = append(b, 0x20, byte(n+1))
	}
	b = append(b, 0x05, 0x20, 0, 0x42, 1, 0x7d)
	for n := 0; n < payload; n++ {
		b = append(b, 0x20, byte(n+1))
	}
	return append(b, 0x12, 0, 0x0b, 0x0b)
}

func TestTailImportSourceRV64AlternatingDecodedChainsAndRawTransport(t *testing.T) {
	for _, swar := range []bool{false, true} {
		for _, shared := range []bool{false, true} {
			t.Run(fmt.Sprintf("swar=%t/shared=%t", swar, shared), func(t *testing.T) {
				payload := []wasm.ValType{wasm.I32, wasm.F32, wasm.I64, wasm.F64}
				if swar {
					payload = append(payload, wasm.V128)
				}
				params := append([]wasm.ValType{wasm.I64}, payload...)
				source := tailImportTestSource(params, payload, tailImportCountdownBody(len(payload)))
				a, err := compileTailImportSource(source, 0, swar)
				if err != nil {
					t.Fatal(err)
				}
				b, err := compileTailImportSource(source, uint32(len(a.plans)), swar)
				if err != nil {
					t.Fatal(err)
				}
				f := tailRANativeFixtureNew(t, shared, swarSignatureSlots(params), swarSignatureSlots(payload))
				af, bf := tailImportNativeFunctions(t, a, f.a, f.ca, f.da), tailImportNativeFunctions(t, b, f.b, f.cb, f.db)
				root := tailRANativeRoot(t, af[0], f.witness)
				o := tailImportNativeOwner(t, f, []tailImportBinding{{a, af, []tailRAFunction{bf[0]}}, {b, bf, []tailRAFunction{af[0]}}}, root)
				binary.LittleEndian.PutUint64(f.args, 2049)
				cell := 1
				var want []uint64
				for n, typ := range payload {
					v := uint64(0xffffaaaa7f800001) ^ uint64(n)*0x100010001
					binary.LittleEndian.PutUint64(f.args[cell*8:], v)
					cell++
					if swarClassOf(typ).narrow() {
						want = append(want, uint64(uint32(v)))
					} else {
						want = append(want, v)
					}
					if wasm.EqualValType(typ, wasm.V128) {
						v = 0x8000000000000031
						binary.LittleEndian.PutUint64(f.args[cell*8:], v)
						cell++
						want = append(want, v)
					}
				}
				if err := tailRANativeInvoke(t, f, o, root); err != nil {
					t.Fatal(err)
				}
				for n, v := range want {
					if got := binary.LittleEndian.Uint64(f.results[n*8:]); got != v {
						t.Fatal("source payload", n, got, v)
					}
				}
				f.assertReturned(t)
			})
		}
	}
}

func TestTailImportSourceRV64MaximumPairsNilDirectionsAndColdTrap(t *testing.T) {
	for _, maximum := range []bool{false, true} {
		var types []wasm.ValType
		var body []byte
		if maximum {
			for n := 0; n < 128; n++ {
				types = append(types, wasm.V128)
				body = append(body, 0x20, byte(n))
			}
		}
		caller := append(bytes.Clone(body), 0x12, 0, 0x0b)
		terminal := append(bytes.Clone(body), 0x0b)
		a, err := compileTailImportSource(tailImportTestSource(types, types, caller), 0, true)
		if err != nil {
			t.Fatal(err)
		}
		b, err := compileTailImportSource(tailImportTestSource(types, types, terminal), uint32(len(a.plans)), true)
		if err != nil {
			t.Fatal(err)
		}
		f := tailRANativeFixtureNew(t, false, swarSignatureSlots(types), swarSignatureSlots(types))
		af, bf := tailImportNativeFunctions(t, a, f.a, f.ca, f.da), tailImportNativeFunctions(t, b, f.b, f.cb, f.db)
		root := tailRANativeRoot(t, af[0], f.witness)
		o := tailImportNativeOwner(t, f, []tailImportBinding{{a, af, []tailRAFunction{bf[0]}}, {b, bf, []tailRAFunction{af[0]}}}, root)
		for n := 0; n < len(f.args)/8; n++ {
			binary.LittleEndian.PutUint64(f.args[n*8:], 0x8000000000000000^uint64(n))
		}
		if err := tailRANativeInvoke(t, f, o, root); err != nil || !bytes.Equal(f.args, f.results) {
			t.Fatal("full pairs or genuine nil directions", maximum, err)
		}
		f.assertReturned(t)
	}
	// Cold target is also source compiled, and publishes no original result.
	a, err := compileTailImportSource(tailImportTestSource(nil, []wasm.ValType{wasm.I64}, []byte{0x12, 0, 0x0b}), 0, false)
	if err != nil {
		t.Fatal(err)
	}
	b, err := compileTailImportSource(tailImportTestSource(nil, []wasm.ValType{wasm.I64}, []byte{0, 0x0b}), 1, false)
	if err != nil {
		t.Fatal(err)
	}
	f := tailRANativeFixtureNew(t, false, 0, 1)
	af, bf := tailImportNativeFunctions(t, a, f.a, f.ca, f.da), tailImportNativeFunctions(t, b, f.b, f.cb, f.db)
	root := tailRANativeRoot(t, af[0], f.witness)
	o := tailImportNativeOwner(t, f, []tailImportBinding{{a, af, []tailRAFunction{bf[0]}}, {b, bf, []tailRAFunction{af[0]}}}, root)
	copy(f.results, bytes.Repeat([]byte{0xa5}, len(f.results)))
	err = tailRANativeInvoke(t, f, o, root)
	var trap *coreruntime.TrapError
	if !errors.As(err, &trap) || trap.Code != coreruntime.TrapUnreachable || len(trap.Frames) != 1 || trap.Frames[0].FunctionIndex != 1 || !bytes.Equal(f.results, bytes.Repeat([]byte{0xa5}, len(f.results))) {
		t.Fatal("source target cold attribution and unpublished result", err)
	}
}

func TestTailImportSourceRV64ActualForeignCycleInterruptAndReuse(t *testing.T) {
	old := runtime.GOMAXPROCS(1)
	defer runtime.GOMAXPROCS(old)
	source := tailImportTestSource(nil, nil, []byte{0x12, 0, 0x0b})
	a, err := compileTailImportSource(source, 0, false)
	if err != nil {
		t.Fatal(err)
	}
	b, err := compileTailImportSource(source, 1, true)
	if err != nil {
		t.Fatal(err)
	}
	f := tailRANativeFixtureNew(t, false, 0, 0)
	af, bf := tailImportNativeFunctions(t, a, f.a, f.ca, f.da), tailImportNativeFunctions(t, b, f.b, f.cb, f.db)
	root := tailRANativeRoot(t, af[0], f.witness)
	o := tailImportNativeOwner(t, f, []tailImportBinding{{a, af, []tailRAFunction{bf[0]}}, {b, bf, []tailRAFunction{af[0]}}}, root)
	done := make(chan error, 1)
	go func() { done <- tailRANativeInvoke(t, f, o, root) }()
	// Keep a real pointer into the retained mapping while addressing its
	// basedata prefix; do not reconstruct a pointer from an integer address.
	word := (*uint64)(unsafe.Add(unsafe.Pointer(unsafe.SliceData(f.b.HostBytes())), -abi.RV64NativeContextOffset))
	deadline := time.Now().Add(2 * time.Second)
	started := false
	for time.Now().Before(deadline) {
		if atomic.LoadUint64(word) == uint64(tailRAPointer(f.cb)) {
			started = true
			break
		}
		select {
		case err := <-done:
			t.Fatal("source cycle returned early", err)
		default:
		}
		time.Sleep(100 * time.Microsecond)
	}
	stop := coreruntime.RequestInterruptAsync(f.trap)
	select {
	case err = <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("source cycle did not interrupt")
	}
	stop()
	var trap *coreruntime.TrapError
	if !started || !errors.As(err, &trap) || trap.Code != coreruntime.TrapInterrupted {
		t.Fatal("actual foreign entry witness/interruption", started, err)
	}
	f.bind(t, f.e.StackLimit())
	leaf := tailRANativeFrame(t, &wasm.CompType{Kind: wasm.CompFunc}, f.a, f.ca, f.da, 2, func(*integerEmitter) {})
	if err := leaf.code.Seal(); err != nil {
		t.Fatal(err)
	}
	if err := f.e.CallPrepared(leaf.code.Base(), nil, f.a.LinMemBase(), f.trap, nil); err != nil {
		t.Fatal("source cycle engine reuse", err)
	}
}

func TestTailImportSourceRV64SavedRAAfterLocalCallAndWiderTailArguments(t *testing.T) {
	// Public entry has one parameter, but its internal tail needs two actual
	// cells. An ordinary defined call first replaces live RA and uses transport.
	im := append(wasmtest.Name("p"), wasmtest.Name("tail")...)
	im = append(im, 0, 0)
	entry := []byte{0, 0x20, 0, 0x10, 2, 0x1a, 0x20, 0, 0x42, 7, 0x12, 0, 0x0b}
	leaf := []byte{0, 0x20, 0, 0x42, 1, 0x7c, 0x0b}
	source := wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType([]wasm.ValType{wasm.I64, wasm.I64}, []wasm.ValType{wasm.I64}), wasmtest.FuncType([]wasm.ValType{wasm.I64}, []wasm.ValType{wasm.I64}))),
		wasmtest.Section(2, wasmtest.Vec(im)),
		wasmtest.Section(3, []byte{2, 1, 1}),
		wasmtest.Section(10, wasmtest.Vec(append(wasmtest.ULEB(uint32(len(entry))), entry...), append(wasmtest.ULEB(uint32(len(leaf))), leaf...))),
	)
	for _, swar := range []bool{false, true} {
		a, err := compileTailImportSource(source, 0, swar)
		if err != nil {
			t.Fatal(err)
		}
		b, err := compileTailImportSource(tailImportTestSource([]wasm.ValType{wasm.I64, wasm.I64}, []wasm.ValType{wasm.I64}, []byte{0x20, 0, 0x20, 1, 0x7c, 0x0b}), 1, swar)
		if err != nil {
			t.Fatal(err)
		}
		f := tailRANativeFixtureNew(t, false, 2, 1)
		af, bf := tailImportNativeFunctions(t, a, f.a, f.ca, f.da), tailImportNativeFunctions(t, b, f.b, f.cb, f.db)
		root := tailRANativeRoot(t, af[0], f.witness)
		o := tailImportNativeOwner(t, f, []tailImportBinding{{a, af, []tailRAFunction{bf[0]}}, {b, bf, []tailRAFunction{bf[0]}}}, root)
		binary.LittleEndian.PutUint64(f.args, 42)
		binary.LittleEndian.PutUint64(f.args[8:], 0xdeadbeef)
		if err := tailRANativeInvoke(t, f, o, root); err != nil || binary.LittleEndian.Uint64(f.results) != 49 {
			t.Fatal("saved incoming RA/actual internal argument capacity", swar, err)
		}
		f.assertReturned(t)
	}
}
