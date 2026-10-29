//go:build linux && riscv64 && !tinygo && !wago_target_tinygo

package wago

import (
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	goruntime "runtime"
	"slices"
	"strings"
	"sync/atomic"
	"testing"
	"time"
	"unsafe"

	railshot "github.com/wago-org/wago/src/core/compiler/backend/railshot/riscv64"
	"github.com/wago-org/wago/src/core/compiler/wasm"
	coreruntime "github.com/wago-org/wago/src/core/runtime"
	"github.com/wago-org/wago/tests/support/wasmtest"
)

func rv64PublicSWARMixedCallsModule() []byte {
	params := []wasm.ValType{wasm.I32, wasm.V128, wasm.F32, wasm.I64, wasm.V128, wasm.F64}
	result := []wasm.ValType{wasm.V128, wasm.I32, wasm.F64, wasm.V128, wasm.F32, wasm.I64}
	caller := []byte{0x20, 4}
	middle := []byte{}
	for i := range params {
		caller = append(caller, 0x20, byte(i))
		middle = append(middle, 0x20, byte(i))
	}
	caller = append(caller, 0x10, 1, 0x0b)
	middle = append(middle, 0x10, 2, 0x0b)
	leaf := []byte{0x20, 1, 0x20, 4, 0xfd, 81, 0x20, 0, 0x20, 5, 0x20, 4, 0x20, 2, 0x20, 3, 0x0b}
	code := func(body []byte) []byte {
		b := append([]byte{1, 122, 0x7b}, body...)
		return append(wasmtest.ULEB(uint32(len(b))), b...)
	}
	return wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType(params, append([]wasm.ValType{wasm.V128}, result...)), wasmtest.FuncType(params, result))),
		wasmtest.Section(3, []byte{3, 0, 1, 1}), wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("call", 0, 0))),
		wasmtest.Section(10, wasmtest.Vec(code(caller), code(middle), code(leaf))),
	)
}

func TestRV64PublicSWARArtifactMixedCarriersSessionsAndLiveOwner(t *testing.T) {
	if !SupportedFeatures().IsEnabled(CoreFeatureSIMD) || !hostSupportsSIMD() {
		t.Fatal("baseline SWAR unavailable")
	}
	source := rv64PublicSWARMixedCallsModule()
	if c, err := Compile(NewRuntimeConfig().WithFeature(CoreFeatureSIMD, false), source); c != nil || err == nil {
		t.Fatal("disabled SIMD accepted")
	}
	for _, load := range []bool{false} {
		c, err := Compile(nil, source)
		if err != nil {
			t.Fatal(err)
		}
		defer c.Close()
		if c.codeCache.base != 0 || c.requiredAMD64Features != 0 || !c.requiredFeatures.IsEnabled(CoreFeatureSIMD) {
			t.Fatal("deferred baseline artifact identity")
		}

		in, err := Instantiate(c)
		if err != nil {
			t.Fatal(err)
		}
		defer in.Close()
		if err := c.Close(); err != nil {
			t.Fatal(err)
		}
		goruntime.GC()
		input := []uint64{0xfeedface80000001, 0x7ff0000000000001, 0x8123456789abcdef, 0xaaaaaaaa7f800123, 0xffeeddbbccaa9988, 0xfff0000000000001, 0x7fa1234500000001, 0x8000000000000000}
		want := []uint64{input[5], input[6], input[1] ^ input[5], input[2] ^ input[6], uint64(uint32(input[0])), input[7], input[5], input[6], uint64(uint32(input[3])), input[4]}
		got, err := in.Invoke("call", input...)
		if err != nil || !slices.Equal(got, want) {
			t.Fatal("mixed carrier artifact", load, got, err)
		}
		fn, err := in.WasmFunc("call")
		if err != nil {
			t.Fatal(err)
		}
		if fn.directIntFast || fn.directFloatFast {
			t.Fatal("unqualified direct entry")
		}
		got, err = fn.Invoke(input...)
		if err != nil || !slices.Equal(got, want) {
			t.Fatal("prepared mixed", got, err)
		}
		session, err := fn.OpenSession()
		if err != nil {
			t.Fatal(err)
		}
		got, err = session.Invoke(input...)
		session.Close()
		if err != nil || !slices.Equal(got, want) {
			t.Fatal("session mixed", got, err)
		}
		if n := testing.AllocsPerRun(50, func() {
			got, err = in.Invoke("call", input...)
			if err != nil || !slices.Equal(got, want) {
				panic("mixed warm result")
			}
		}); n != 0 {
			t.Fatal("warm allocation", n)
		}
	}
}

func rv64PublicSWARMemoryLoopModule() []byte {
	vector := []byte{1, 0, 0, 0, 0, 0, 0xf0, 0x7f, 0xef, 0xbe, 0xad, 0xde, 0, 0, 0, 0x80}
	spin := []byte{3, 0x40, 0x41, 0, 0xfd, 12}
	spin = append(spin, vector...)
	spin = append(spin, 0xfd, 11, 0, 0, 0x0c, 0, 0x0b, 0x0b)
	return wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType(nil, nil), wasmtest.FuncType([]wasm.ValType{wasm.I32, wasm.V128}, nil), wasmtest.FuncType([]wasm.ValType{wasm.I32}, []wasm.ValType{wasm.V128}))),
		wasmtest.Section(3, []byte{3, 0, 1, 2}), wasmtest.Section(5, []byte{1, 1, 1, 1}),
		wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("spin", 0, 0), wasmtest.ExportEntry("store", 0, 1), wasmtest.ExportEntry("load", 0, 2))),
		wasmtest.Section(10, wasmtest.Vec(wasmtest.Code(spin), wasmtest.Code([]byte{0x20, 0, 0x20, 1, 0xfd, 11, 0, 0, 0x0b}), wasmtest.Code([]byte{0x20, 0, 0xfd, 0, 0, 0, 0x0b}))),
	)
}

func TestRV64PublicSWARFullWidthMemoryAndLoadedActiveCancellation(t *testing.T) {
	previous := goruntime.GOMAXPROCS(1)
	defer goruntime.GOMAXPROCS(previous)
	c, err := Compile(nil, rv64PublicSWARMemoryLoopModule())
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()

	in, err := Instantiate(c)
	if err != nil {
		t.Fatal(err)
	}
	defer in.Close()
	value := []uint64{0x7ff0000000000001, 0x8123456789abcdef}
	if _, err := in.Invoke("store", 0xdeadbeef00000003, value[0], value[1]); err != nil {
		t.Fatal(err)
	}
	got, err := in.Invoke("load", 0xdeadbeef00000003)
	if err != nil || !slices.Equal(got, value) {
		t.Fatal("unaligned raw vector", got, err)
	}
	mem := in.jm.CurrentBytes()
	before := slices.Clone(mem[len(mem)-16:])
	var boundsTrap *coreruntime.TrapError
	if _, err := in.Invoke("store", 65521, value[0], value[1]); !errors.As(err, &boundsTrap) || boundsTrap.Code != coreruntime.TrapLinMemOutOfBounds || len(boundsTrap.Frames) != 1 || boundsTrap.Frames[0].FunctionIndex != 1 || boundsTrap.Frames[0].ProgramCounter != 5 || !slices.Equal(before, mem[len(mem)-16:]) {
		t.Fatal("split store wrote before complete bounds", err)
	}
	clear(mem[:16])
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() { _, err := in.InvokeContext(ctx, "spin"); done <- err }()
	marker := (*uint32)(unsafe.Pointer(&mem[0]))
	deadline := time.Now().Add(5 * time.Second)
	for atomic.LoadUint32(marker) == 0 && time.Now().Before(deadline) {
		goruntime.Gosched()
	}
	entered := atomic.LoadUint32(marker) != 0
	cancel()
	select {
	case err := <-done:
		if !entered || !errors.Is(err, context.Canceled) {
			t.Fatal("active SWAR cancellation", entered, err)
		}
	case <-time.After(5 * time.Second):
		stop := coreruntime.RequestInterruptAsync(in.trap)
		<-done
		stop()
		t.Fatal("SWAR cancellation timeout")
	}
	got, err = in.Invoke("load", 0)
	if err != nil || binary.LittleEndian.Uint64(mem) != got[0] || binary.LittleEndian.Uint64(mem[8:]) != got[1] {
		t.Fatal("post-cancel reuse", got, err)
	}
}

func TestRV64PublicSWARArtifactProfileRejectionsBeforeMapping(t *testing.T) {
	c, err := Compile(nil, rv64PublicSWARMixedCallsModule())
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	base := c.executionView()
	for _, test := range []struct {
		name   string
		mutate func(*Compiled)
	}{
		{"vector-arity", func(v *Compiled) {
			v.Funcs = slices.Clone(v.Funcs)
			v.Funcs[0].Params = make([]ValType, railshot.MaxVectorFunctionValues+1)
		}},
		{"ISA", func(v *Compiled) { v.requiredAMD64Features = 1 }},
		{"global-export-index", func(v *Compiled) { v.GlobalExports = map[string]int{"g": 0} }},
		{"start", func(v *Compiled) { v.HasStart = true }},
	} {
		t.Run(test.name, func(t *testing.T) {
			view := *base
			test.mutate(&view)
			if err := railshotValidateCompiled(&view); !errors.Is(err, railshot.ErrUnsupported) {
				t.Fatal("unsupported SWAR artifact", err)
			}
			if c.codeCache.base != 0 {
				t.Fatal("rejection mapped code")
			}
		})
	}
}

func rv64PublicSWARStateModule(host bool) []byte {
	initial := []byte{1, 0, 0, 0, 0, 0, 0xf0, 0x7f, 0xef, 0xbe, 0xad, 0xde, 0, 0, 0, 0x80}
	payload := []byte{0xff, 0xee, 0xdd, 0xcc, 0xbb, 0xaa, 0x99, 0x88, 1, 2, 3, 4, 5, 6, 7, 8}
	global := append(append([]byte{0xfd, 12}, initial...), 0x0b)
	data := wasmtest.Section(11, wasmtest.Vec([]byte{0, 0x41, 7, 0x0b, 2, 0xa5, 0x5a}, append([]byte{1, 16}, payload...)))
	parts := [][]byte{}
	if host {
		parts = append(parts, wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType([]wasm.ValType{wasm.I32}, []wasm.ValType{wasm.I32}), wasmtest.FuncType([]wasm.ValType{wasm.V128}, []wasm.ValType{wasm.V128, wasm.I32, wasm.V128}))))
		parts = append(parts, wasmtest.Section(2, wasmtest.Vec(append(append(wasmtest.Name("env"), wasmtest.Name("host")...), 0, 0))), wasmtest.Section(3, []byte{2, 1, 0}))
	} else {
		parts = append(parts, wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType([]wasm.ValType{wasm.V128, wasm.I32}, []wasm.ValType{wasm.V128, wasm.V128, wasm.V128, wasm.I32}), wasmtest.FuncType([]wasm.ValType{wasm.V128}, []wasm.ValType{wasm.V128}), wasmtest.FuncType(nil, nil))))
		parts = append(parts, wasmtest.Section(3, []byte{4, 0, 1, 1, 2}), wasmtest.Section(4, []byte{1, 0x70, 1, 1, 2}))
	}
	parts = append(parts, wasmtest.Section(5, []byte{1, 1, 1, 1}), wasmtest.Section(6, wasmtest.Vec(wasmtest.GlobalEntry(wasm.V128, true, global))))
	if host {
		parts = append(parts, wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("call", 0, 1))))
	} else {
		parts = append(parts, wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("call", 0, 0), wasmtest.ExportEntry("drop", 0, 3))), wasmtest.Section(9, wasmtest.Vec([]byte{0, 0x41, 0, 0x0b, 1, 1}, []byte{1, 0, 1, 2})))
	}
	parts = append(parts, wasmtest.Section(12, []byte{2}))
	if host {
		root := []byte{0x23, 0, 0x20, 0, 0x24, 0, 0x41, 42, 0x10, 2, 0x41, 0xe5, 0, 0x41, 0, 0x41, 16, 0xfc, 8, 1, 0, 0x41, 0xe5, 0, 0xfd, 0, 0, 0, 0x0b}
		parts = append(parts, wasmtest.Section(10, wasmtest.Vec(wasmtest.Code(root), wasmtest.Code([]byte{0x20, 0, 0x10, 0, 0x0b}))))
	} else {
		root := []byte{0x23, 0, 0x20, 1, 0x41, 0, 0x41, 16, 0xfc, 8, 1, 0, 0x20, 1, 0xfd, 0, 0, 0, 0x20, 0, 0x24, 0, 0x41, 0, 0x41, 0, 0x41, 1, 0xfc, 12, 1, 0, 0x20, 0, 0x41, 0, 0x11, 1, 0, 0xfc, 16, 0, 0x0b}
		leaf := append([]byte{0x20, 0, 0xfd, 12}, payload...)
		leaf = append(leaf, 0xfd, 81, 0x0b)
		parts = append(parts, wasmtest.Section(10, wasmtest.Vec(wasmtest.Code(root), wasmtest.Code([]byte{0x20, 0, 0x0b}), wasmtest.Code(leaf), wasmtest.Code([]byte{0xfc, 9, 1, 0xfc, 13, 1, 0x0b}))))
	}
	parts = append(parts, data)
	return wasmtest.Module(parts...)
}

func TestRV64PublicSWARLoadedGlobalDataTableOwnersAndDrops(t *testing.T) {
	c, err := Compile(nil, rv64PublicSWARStateModule(false))
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()

	a, err := Instantiate(c)
	if err != nil {
		t.Fatal(err)
	}
	defer a.Close()
	b, err := Instantiate(c)
	if err != nil {
		t.Fatal(err)
	}
	defer b.Close()
	if err := c.Close(); err != nil {
		t.Fatal(err)
	}
	goruntime.GC()
	input := []uint64{0xfff0000000000001, 0x7fa1234500000001, 0xdeadbeef00000065}
	initial := []uint64{0x7ff0000000000001, 0x80000000deadbeef}
	payload := []uint64{0x8899aabbccddeeff, 0x0807060504030201}
	want := []uint64{initial[0], initial[1], payload[0], payload[1], input[0] ^ payload[0], input[1] ^ payload[1], 1}
	for _, in := range []*Instance{a, b} {
		got, err := in.Invoke("call", input...)
		if err != nil || !slices.Equal(got, want) {
			t.Fatal("combined vector table/state", got, want, err)
		}
		mem := in.jm.CurrentBytes()
		if mem[7] != 0xa5 || mem[8] != 0x5a || binary.LittleEndian.Uint64(mem[101:]) != payload[0] || binary.LittleEndian.Uint64(mem[109:]) != payload[1] {
			t.Fatal("active/passive memory state")
		}
	}
	if _, err := a.Invoke("drop"); err != nil {
		t.Fatal(err)
	}
	if _, err := a.Invoke("call", input...); err == nil {
		t.Fatal("dropped vector data revived")
	}
	if err := a.Close(); err != nil {
		t.Fatal(err)
	}
	goruntime.GC()
	want[0], want[1] = input[0], input[1]
	got, err := b.Invoke("call", input...)
	if err != nil || !slices.Equal(got, want) {
		t.Fatal("sibling data/table/global owner damaged", got, err)
	}
	if n := testing.AllocsPerRun(50, func() {
		got, err = b.Invoke("call", input...)
		if err != nil || !slices.Equal(got, want) {
			panic("combined warm state")
		}
	}); n != 0 {
		t.Fatal("combined warm allocation", n)
	}
}

func TestRV64PublicSWARLoadedScalarHostNestedGCAndPanicReuse(t *testing.T) {
	c, err := Compile(nil, rv64PublicSWARStateModule(true))
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()

	inner, err := Instantiate(c, InstantiateOptions{Imports: testImports("env.host", func(v int32) int32 { goruntime.GC(); return v + 1 })})
	if err != nil {
		t.Fatal(err)
	}
	defer inner.Close()
	input := []uint64{0xfff0000000000001, 0x7fa1234500000001}
	payload := []uint64{0x8899aabbccddeeff, 0x0807060504030201}
	initial := []uint64{0x7ff0000000000001, 0x80000000deadbeef}
	want := []uint64{initial[0], initial[1], 43, payload[0], payload[1]}
	panicHost := false
	cause := errors.New("private host panic identity")
	host := HostCallFunc(func(call HostCall) {
		if panicHost {
			panic(HostTrap{Err: cause})
		}
		goruntime.GC()
		got, err := inner.Invoke("call", input...)
		if err != nil || len(got) != 5 || got[2] != 43 || got[3] != payload[0] || got[4] != payload[1] {
			panic(fmt.Sprintf("nested SWAR host/state: result=%x error=%v", got, err))
		}
		if call.ParamCount() != 1 || call.ResultCount() != 1 || call.ParamSlots()[0] != 42 {
			panic("scalar host carrier")
		}
		call.ResultSlots()[0] = 0xdeadbeef0000002b
	})
	outer, err := Instantiate(c, InstantiateOptions{Imports: testImports("env.host", host)})
	if err != nil {
		t.Fatal(err)
	}
	defer outer.Close()
	if err := c.Close(); err != nil {
		t.Fatal(err)
	}
	got, err := outer.Invoke("call", input...)
	if err != nil || !slices.Equal(got, want) {
		t.Fatal("mixed parked host", got, err)
	}
	panicHost = true
	if _, err := outer.Invoke("call", input...); !errors.Is(err, cause) {
		t.Fatal("host panic identity", err)
	}
	panicHost = false
	want[0], want[1] = input[0], input[1]
	got, err = outer.Invoke("call", input...)
	if err != nil || !slices.Equal(got, want) {
		t.Fatal("parked panic reuse", got, err)
	}
}

func TestRV64PublicSWARMaximumVectorTransportAndInlineTypes(t *testing.T) {
	params := make([]wasm.ValType, 128)
	for i := range params {
		params[i] = wasm.V128
	}
	caller, leaf := []byte{}, []byte{}
	for i := range params {
		caller = append(caller, 0x20, byte(i))
		leaf = append(leaf, 0x20, byte(127-i))
	}
	caller = append(caller, 0x10, 1, 0x0b)
	leaf = append(leaf, 0x0b)
	source := wasmtest.Module(wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType(params, params))), wasmtest.Section(3, []byte{2, 0, 0}), wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("call", 0, 0))), wasmtest.Section(10, wasmtest.Vec(wasmtest.Code(caller), wasmtest.Code(leaf))))
	c, err := Compile(nil, source)
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()

	in, err := Instantiate(c)
	if err != nil {
		t.Fatal(err)
	}
	defer in.Close()
	input, want := make([]uint64, 256), make([]uint64, 256)
	for i := range input {
		input[i] = 0x7ff00000deadbeef ^ uint64(i)*0x101010101
	}
	for i := range params {
		want[i*2], want[i*2+1] = input[(127-i)*2], input[(127-i)*2+1]
	}
	got, err := in.Invoke("call", input...)
	if err != nil || !slices.Equal(got, want) {
		t.Fatal("public128V compact transport", err)
	}
	for _, body := range [][]byte{{0x02, 0x7b, 0x00, 0x0b, 0x1a, 0x0b}, {0x03, 0x7b, 0x00, 0x0b, 0x1a, 0x0b}, {0x41, 1, 0x04, 0x7b, 0x00, 0x05, 0x00, 0x0b, 0x1a, 0x0b}, {0x00, 0x41, 1, 0x1c, 1, 0x7b, 0x1a, 0x0b}} {
		source := wasmtest.Module(wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType(nil, nil))), wasmtest.Section(3, []byte{1, 0}), wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("f", 0, 0))), wasmtest.Section(10, wasmtest.Vec(wasmtest.Code(body))))
		if denied, err := Compile(NewRuntimeConfig().WithFeature(CoreFeatureSIMD, false), source); denied != nil || err == nil {
			t.Fatal("disabled inline vector accepted")
		}
		c, err := Compile(nil, source)
		if err != nil {
			t.Fatal("inline vector source", err)
		}
		defer c.Close()
		in, err := Instantiate(c)
		if err != nil {
			t.Fatal(err)
		}
		defer in.Close()
		var tr *coreruntime.TrapError
		if _, err := in.Invoke("f"); !errors.As(err, &tr) || tr.Code != coreruntime.TrapUnreachable {
			t.Fatal("inline vector trap", body, err)
		}
	}
}

func TestRV64PublicSWARIndirectVectorKeyNullAndBounds(t *testing.T) {
	source := wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType([]wasm.ValType{wasm.V128, wasm.I32}, []wasm.ValType{wasm.V128}), wasmtest.FuncType([]wasm.ValType{wasm.V128}, []wasm.ValType{wasm.V128}), wasmtest.FuncType([]wasm.ValType{wasm.I64}, []wasm.ValType{wasm.I64}))),
		wasmtest.Section(3, []byte{3, 0, 1, 2}), wasmtest.Section(4, []byte{1, 0x70, 1, 3, 3}), wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("call", 0, 0))),
		wasmtest.Section(9, wasmtest.Vec([]byte{0, 0x41, 0, 0x0b, 2, 1, 2})),
		wasmtest.Section(10, wasmtest.Vec(wasmtest.Code([]byte{0x20, 0, 0x20, 1, 0x11, 1, 0, 0x0b}), wasmtest.Code([]byte{0x20, 0, 0x0b}), wasmtest.Code([]byte{0x20, 0, 0x0b}))),
	)
	c, err := Compile(nil, source)
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	if c.FuncTypeID[1] == c.FuncTypeID[2] {
		t.Fatal("vector structural key collapsed to scalar")
	}

	in, err := Instantiate(c)
	if err != nil {
		t.Fatal(err)
	}
	defer in.Close()
	input := []uint64{0x7ff0000000000001, 0x8123456789abcdef}
	for _, test := range []struct {
		index uint32
		trap  coreruntime.TrapCode
	}{{0, 0}, {1, coreruntime.TrapIndirectWrongSig}, {2, coreruntime.TrapIndirectOutOfBounds}, {3, coreruntime.TrapIndirectOutOfBounds}, {^uint32(0), coreruntime.TrapIndirectOutOfBounds}, {0, 0}} {
		got, err := in.Invoke("call", input[0], input[1], 0xdeadbeef00000000|uint64(test.index))
		if test.trap == 0 {
			if err != nil || !slices.Equal(got, input) {
				t.Fatal("vector indirect/reuse", got, err)
			}
			continue
		}
		var tr *coreruntime.TrapError
		if !errors.As(err, &tr) || tr.Code != test.trap || got != nil || len(tr.Frames) != 1 || tr.Frames[0].FunctionIndex != 0 || tr.Frames[0].ProgramCounter != 5 {
			t.Fatal("vector indirect precise guard/source", test.index, got, err)
		}
	}
}

func TestRV64PublicSWARSourceUnsupportedStateImportsRemainClosed(t *testing.T) {
	for _, test := range []struct {
		kind             byte
		typ              []byte
		importedFunction bool
	}{
		{1, []byte{0x6f, 0, 1}, false}, {3, []byte{0x70, 1}, false}, {3, []byte{0x6f, 0}, false}, {0, []byte{0}, true},
	} {
		index := uint32(0)
		if test.importedFunction {
			index = 1
		}
		entry := append(append(wasmtest.Name("env"), wasmtest.Name("state")...), test.kind)
		entry = append(entry, test.typ...)
		source := wasmtest.Module(wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType([]wasm.ValType{wasm.V128}, []wasm.ValType{wasm.V128}))), wasmtest.Section(2, wasmtest.Vec(entry)), wasmtest.Section(3, []byte{1, 0}), wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("f", 0, index))), wasmtest.Section(10, wasmtest.Vec(wasmtest.Code([]byte{0x20, 0, 0x0b}))))
		m, err := wasm.DecodeModule(source)
		if err != nil {
			t.Fatal(err)
		}
		if err := wasm.ValidateModule(m); err != nil {
			t.Fatal("rejected fixture must be normally valid", err)
		}
		c, err := Compile(nil, source)
		if test.importedFunction {
			if err != nil {
				t.Fatal("native vector import source rejected", err)
			}
			defer c.Close()
			if in, err := Instantiate(c, testImports("env.state", HostCallFunc(func(HostCall) {}))); in != nil || err == nil || !strings.Contains(err.Error(), "v128 host callbacks are not supported") || c.codeCache.base != 0 {
				t.Fatal("unused vector Go host import published", err)
			}
			continue
		}
		if c != nil {
			c.Close()
		}
		rv64RequireCore3SourceValidation(t, source)
	}
}

func TestRV64PublicSWARCPUAdmissionBeforePublication(t *testing.T) {
	source := rv64PublicSWARMixedCallsModule()
	c, err := Compile(nil, source)
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()

	prior := simdHostFeaturesSupported
	simdHostFeaturesSupported = func() bool { return false }
	defer func() { simdHostFeaturesSupported = prior }()
	if SupportedFeatures().IsEnabled(CoreFeatureSIMD) {
		t.Fatal("unavailable capability advertised")
	}
	if compiled, err := Compile(nil, source); compiled != nil || err == nil {
		t.Fatal("unavailable baseline admitted source")
	}
}
