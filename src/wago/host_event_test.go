//go:build (linux || darwin || windows) && (amd64 || arm64) && !tinygo

package wago

import (
	"errors"
	"reflect"
	"strings"
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
	wruntime "github.com/wago-org/wago/src/core/runtime"
	"github.com/wago-org/wago/tests/support/wasmtest"
)

func hostEventLoopModule() []byte {
	typeEvent := wasmtest.FuncType([]wasm.ValType{wasm.I32}, nil)
	importEvent := append(wasmtest.Name("env"), wasmtest.Name("event")...)
	importEvent = append(importEvent, 0x00)
	importEvent = append(importEvent, wasmtest.ULEB(0)...)
	body := []byte{
		0x02, 0x40, 0x03, 0x40, // block done; loop next
		0x20, 0x00, 0x45, 0x0d, 0x01, // count == 0: branch done
		0x20, 0x00, 0x10, 0x00, // event(count)
		0x20, 0x00, 0x41, 0x01, 0x6b, 0x21, 0x00, // count--
		0x0c, 0x00, 0x0b, 0x0b, // branch next; end loop/block
		0x0b,
	}
	return wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(typeEvent)),
		wasmtest.Section(2, wasmtest.Vec(importEvent)),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(0))),
		wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("run", 0, 1))),
		wasmtest.Section(10, wasmtest.Vec(wasmtest.Code(body))),
	)
}

func mixedHostEventModule() []byte {
	typeEvent := wasmtest.FuncType([]wasm.ValType{wasm.I32}, nil)
	typeQuery := wasmtest.FuncType([]wasm.ValType{wasm.I32}, []wasm.ValType{wasm.I32})
	typeRun := wasmtest.FuncType(nil, nil)
	event := append(wasmtest.Name("env"), wasmtest.Name("event")...)
	event = append(event, 0x00)
	event = append(event, wasmtest.ULEB(0)...)
	query := append(wasmtest.Name("env"), wasmtest.Name("query")...)
	query = append(query, 0x00)
	query = append(query, wasmtest.ULEB(1)...)
	return wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(typeEvent, typeQuery, typeRun)),
		wasmtest.Section(2, wasmtest.Vec(event, query)),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(2))),
		wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("run", 0, 2))),
		wasmtest.Section(10, wasmtest.Vec(wasmtest.Code([]byte{0x0b}))),
	)
}

func TestI32HostEventDefersOrderedDelivery(t *testing.T) {
	c := MustCompile(hostEventLoopModule())
	defer c.Close()

	var got []int32
	in, err := Instantiate(c, InstantiateOptions{Imports: Imports{
		"env.event": I32HostEvent(func(value int32) { got = append(got, value) }),
	}})
	if err != nil {
		t.Fatal(err)
	}
	defer in.Close()
	if in.syncMode {
		t.Fatal("deferred host event selected synchronous host control")
	}

	if results, err := in.Invoke("run", I32(5)); err != nil || len(results) != 0 {
		t.Fatalf("run = %v, %v; want no results", results, err)
	}
	if want := []int32{5, 4, 3, 2, 1}; !reflect.DeepEqual(got, want) {
		t.Fatalf("events = %v, want %v", got, want)
	}
}

func TestI32HostEventRequiresExactSignature(t *testing.T) {
	c := MustCompile(hostRoundtripLoopModule(t, 0))
	defer c.Close()
	if in, err := Instantiate(c, InstantiateOptions{Imports: Imports{
		"env.step": I32HostEvent(func(int32) {}),
	}}); err == nil || in != nil {
		t.Fatalf("Instantiate = %v, %v; want signature error", in, err)
	}
}

func TestI32HostEventRejectsMixedSynchronousModule(t *testing.T) {
	c := MustCompile(mixedHostEventModule())
	defer c.Close()
	in, err := Instantiate(c, InstantiateOptions{Imports: Imports{
		"env.event": I32HostEvent(func(int32) {}),
		"env.query": I32ToI32HostFunc(func(value int32) int32 { return value }),
	}})
	if err == nil || in != nil || !strings.Contains(err.Error(), "cannot be used by a module that requires synchronous host control") {
		t.Fatalf("Instantiate = %v, %v; want mixed-mode rejection", in, err)
	}
}

func TestI32HostEventOverflowTrapsWithoutPartialReplay(t *testing.T) {
	c := MustCompile(hostEventLoopModule())
	defer c.Close()

	delivered := 0
	in, err := Instantiate(c, InstantiateOptions{Imports: Imports{
		"env.event": I32HostEvent(func(int32) { delivered++ }),
	}})
	if err != nil {
		t.Fatal(err)
	}
	defer in.Close()

	_, err = in.Invoke("run", I32(MaxDeferredHostEventsPerInvocation+1))
	var trap *wruntime.TrapError
	if !errors.As(err, &trap) || trap.Code != wruntime.TrapHostEventOverflow {
		t.Fatalf("overflow error = %v; want %v", err, wruntime.TrapHostEventOverflow)
	}
	if delivered != 0 {
		t.Fatalf("overflow delivered %d partial events; want transactional discard", delivered)
	}
}

func TestI32HostEventPanicUsesHostTrapSemantics(t *testing.T) {
	c := MustCompile(hostEventLoopModule())
	defer c.Close()

	in, err := Instantiate(c, InstantiateOptions{Imports: Imports{
		"env.event": I32HostEvent(func(int32) { panic(HostTrap{Err: errors.New("event failed")}) }),
	}})
	if err != nil {
		t.Fatal(err)
	}
	defer in.Close()
	if _, err := in.Invoke("run", I32(1)); err == nil || !strings.Contains(err.Error(), "event failed") {
		t.Fatalf("event panic = %v; want host trap", err)
	}
}

func TestI32HostEventInstanceRejectsCrossInstanceExport(t *testing.T) {
	c := MustCompile(hostEventLoopModule())
	defer c.Close()
	in, err := Instantiate(c, InstantiateOptions{Imports: Imports{
		"env.event": I32HostEvent(func(int32) {}),
	}})
	if err != nil {
		t.Fatal(err)
	}
	defer in.Close()
	if export, err := in.ExportedFunc("run"); err == nil || export != nil || !strings.Contains(err.Error(), "deferred host events") {
		t.Fatalf("ExportedFunc = %v, %v; want deferred-event boundary error", export, err)
	}
}

func BenchmarkHostEventLoopManaged(b *testing.B) {
	benchmarkHostEventLoop(b, false)
}

func BenchmarkHostEventLoopDeferred(b *testing.B) {
	benchmarkHostEventLoop(b, true)
}

func benchmarkHostEventLoop(b *testing.B, deferred bool) {
	c := MustCompile(hostEventLoopModule())
	defer c.Close()
	var sum int64
	var callback any = HostFunc(func(_ HostModule, params, _ []uint64) {
		sum += int64(AsI32(params[0]))
	})
	if deferred {
		callback = I32HostEvent(func(value int32) { sum += int64(value) })
	}
	in, err := Instantiate(c, InstantiateOptions{Imports: Imports{
		"env.event": callback,
	}})
	if err != nil {
		b.Fatal(err)
	}
	defer in.Close()
	if _, err := in.Invoke("run", I32(1024)); err != nil {
		b.Fatal(err)
	}

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := in.Invoke("run", I32(1024)); err != nil {
			b.Fatal(err)
		}
	}
	b.StopTimer()
	if sum == 0 {
		b.Fatal("events were not delivered")
	}
}
