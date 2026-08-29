package wago

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/binary"
	"errors"
	"math"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/wago-org/wago/src/core/compiler/wasm"
	"github.com/wago-org/wago/tests/wasmtest"
)

const uremRegallocWasmBase64 = "AGFzbQEAAAABEgNgBH9/f38Bf2ABfwBgAn9/AAIWAQVyZXBybwx1cGRhdGVfbm9uY2UAAQMEAwIAAAUDAQARBgYBfwFBAAsHIQIPX19zdGFja19wb2ludGVyAwALZmlsbF9ibG9ja3MAAwq5CQMDAAALBABBEgutCQIafwt+IwAiBCEaIAQkACAAKAIQIhhBAnQiBEVFBEABQQYhGQJAAkACQAJAIAAoAggiEyAYQQN0IgcgEyAHSxsgBG4iCCAEbCIVIAJLIgYNACAIQQJ0IhRFDQMgACgCDCEJIBQgFSAVIBRwayIXS0UEQAEgFEEKdCEKQQAhCyABIQUDQCALIg5BAWohCyAXIBRrIRcgBSAKaiEFQQAhFkEAIQQDQCARQQQ2AtQIIBFBBDYCzAggEUHAADYCxAggESADNgLACCARIBY2AjwgESAONgJAIBEgEUHAAGo2AtAIIBEgEUE8ajYCyAggEUHAEGpBAEEB/AsAIBFBwAhqQQMgEUHAEGpBgAgQAiIZQf8BcUESRw0DIARBgAhqIQwgFkEBaiEWIBFBwBBqIQJBgAghBEEAIRNBgQEhBwNAIARBB00NBiAHQX9qIgdFDQUgEyACKQAANwMAIBNBAWohEyACIARBCCAEQQhJGyISaiECIAQgEmsiBA0ACyAMIgQgBEcNAAsgFCAXTQ0ACwtBEiEZIAlFDQBBACABIAYbIQ8gCEEDbCIXQX9qIQsgAC0AUCIErUIDgyEmQgEhJyAJrSEkIBWtIShCACEfIAAoAkRBEEYhGyAEIRADQCAfIiBCAXwhHyAbICBQIgxyIQ4gECAMcSEcQgAhIQNAICEhHiARQQFGBH9BAQUBIBwLIRYgHkIBfCEhIBhFRQRAASAeUCEdIAghAyAIIB6nbCEKIB4gIIRC/////w+DISVCACEiA0AgEUHAAGpBAEGACPwLACARQcAIakEAQYAI/AsAIBFBwBBqQQBBgAj8CwACfwJAAkAgFkVFBEABIBEgJjcD6AggESAkNwPgCCARICg3A9gIIBEgHjcD0AggESAiNwPICCARICA3A8AIICVQRQ0BDAILICVQDQELIBQgIqdsIApqIgcgHWohBEEAIRIgCiEGIBEMAQtBAiESIBQgIqdsQQJyIgchBEEBCyEAIBIgCE9FBEABIAYhCSAEQX9qIQQgASAHQQp0aiETICKnIQUDQAJAAkAgFkUEQAEgBCAVTw0BIA8gBEEKdGohAgwCCwJAIBJB/wBxIgINAAsgEUHAAGogAkEDdGohAgwBCwALIAIpAwAhIwJ/IAxFRQRAASAARUUEQAEgBSENIBJBf2oMAgsgIiAjQiCIpyAYcCINrVFFBEABIAYgEkVrDAILIAkgEmoMAQsgIiAjQiCIpyAYcCINrVFFBEABIBcgEkVrDAELIAsgEmoLIgIgA2ogI0L/////D4MiIyAjfkIgiCACrX5CIIinQX9zaiAUcCECAkACQAJAAkAgBCAVT0UEQAEgAiANIBRsaiAVTw0BIA5FBEABIAcgFU8NA0EAIQQDQCATIARqIgIgAikDACARQcAYaiAEaikDAIU3AwAgBEEIaiIEQYAIRw0ACwwFCyAHIBVJDQMACwALAAsACyABIAdBCnRqIBFBwBhqQYAI/AoAAAsgE0EAaiETIAciBEEBaiEHIBJBAWoiEiAISQ0ACwsgIkIBfCIiICdSDQALCyAhQgRSDQALIB8gJFINAAsLIBokACAZDwsACwALAAsACw=="

// These runtime cases pin arithmetic, memory, host-call, and ABI regressions.
func TestIntegerOverflowWraps(t *testing.T) {
	i32Min := append([]byte{0x41}, wasmtest.SLEB32(math.MinInt32)...)
	i32Min = append(i32Min, 0x0b)
	i64Min := append([]byte{0x42}, wasmtest.SLEB64(math.MinInt64)...)
	i64Min = append(i64Min, 0x0b)
	i32Body := append([]byte{0x41}, wasmtest.SLEB32(math.MaxInt32)...)
	i32Body = append(i32Body, 0x41, 0x01, 0x6a, 0x23, 0x00, 0x46, 0x0b)
	i64Body := append([]byte{0x42}, wasmtest.SLEB64(math.MaxInt64)...)
	i64Body = append(i64Body, 0x42, 0x01, 0x7c, 0x23, 0x01, 0x51, 0x0b)

	mod := wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType(nil, []wasm.ValType{wasm.I32}))),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(0), wasmtest.ULEB(0))),
		wasmtest.Section(6, wasmtest.Vec(
			wasmtest.GlobalEntry(wasm.I32, false, i32Min),
			wasmtest.GlobalEntry(wasm.I64, false, i64Min),
		)),
		wasmtest.Section(7, wasmtest.Vec(
			wasmtest.ExportEntry("i32", 0, 0),
			wasmtest.ExportEntry("i64", 0, 1),
		)),
		wasmtest.Section(10, wasmtest.Vec(wasmtest.Code(i32Body), wasmtest.Code(i64Body))),
	)
	in := instantiateRegressionModule(t, mod)
	defer in.Close()
	for _, name := range []string{"i32", "i64"} {
		got, err := in.Invoke(name)
		if err != nil || len(got) != 1 || got[0] != 1 {
			t.Fatalf("%s overflow result = %v, err %v; want [1]", name, got, err)
		}
	}
}

func TestGlobalI32UnsignedExtension(t *testing.T) {
	init := append([]byte{0x41}, wasmtest.SLEB32(-1)...)
	init = append(init, 0x0b)
	mod := wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType(nil, []wasm.ValType{wasm.I64}))),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(0))),
		wasmtest.Section(6, wasmtest.Vec(wasmtest.GlobalEntry(wasm.I32, false, init))),
		wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("extend", 0, 0))),
		wasmtest.Section(10, wasmtest.Vec(wasmtest.Code([]byte{0x23, 0x00, 0xad, 0x0b}))),
	)
	in := instantiateRegressionModule(t, mod)
	defer in.Close()
	got, err := in.Invoke("extend")
	if err != nil || len(got) != 1 || got[0] != math.MaxUint32 {
		t.Fatalf("extend result = %#x, err %v; want %#x", got, err, uint64(math.MaxUint32))
	}
}

func TestMemorySizeGrowAndBounds(t *testing.T) {
	mod := wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(
			wasmtest.FuncType([]wasm.ValType{wasm.I32}, []wasm.ValType{wasm.I32}),
			wasmtest.FuncType(nil, []wasm.ValType{wasm.I32}),
			wasmtest.FuncType([]wasm.ValType{wasm.I32}, nil),
		)),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(0), wasmtest.ULEB(1), wasmtest.ULEB(2))),
		wasmtest.Section(5, wasmtest.Vec([]byte{0x00, 0x00})), // memory 0, no declared maximum
		wasmtest.Section(7, wasmtest.Vec(
			wasmtest.ExportEntry("grow", 0, 0),
			wasmtest.ExportEntry("size", 0, 1),
			wasmtest.ExportEntry("store", 0, 2),
			wasmtest.ExportEntry("memory", 2, 0),
		)),
		wasmtest.Section(10, wasmtest.Vec(
			wasmtest.Code([]byte{0x20, 0x00, 0x40, 0x00, 0x0b}),
			wasmtest.Code([]byte{0x3f, 0x00, 0x0b}),
			wasmtest.Code([]byte{0x20, 0x00, 0x42, 0x01, 0x37, 0x03, 0x00, 0x0b}),
		)),
	)
	in := instantiateRegressionModule(t, mod)
	defer in.Close()
	assertI32InvokeResult(t, in, "size", 0)
	if _, err := in.Invoke("store", I32(0)); err == nil {
		t.Fatal("store into zero-page memory succeeded")
	}
	assertI32InvokeResult(t, in, "grow", 0, I32(1))
	assertI32InvokeResult(t, in, "size", 1)
	if _, err := in.Invoke("store", I32(65536-8)); err != nil {
		t.Fatalf("store at end of grown page: %v", err)
	}
}

func TestWasmtimeBigMemoryBehavior(t *testing.T) {
	// Exercise the architectural memory32 transition from 65,535 to 65,536
	// pages: zero growth is observational, the final page succeeds exactly once,
	// and later growth returns the all-ones sentinel.
	limits := append([]byte{0x01}, wasmtest.ULEB(65535)...)
	limits = append(limits, wasmtest.ULEB(65536)...)
	mod := wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(
			wasmtest.FuncType([]wasm.ValType{wasm.I32}, []wasm.ValType{wasm.I32}),
			wasmtest.FuncType(nil, []wasm.ValType{wasm.I32}),
		)),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(0), wasmtest.ULEB(1))),
		wasmtest.Section(5, wasmtest.Vec(limits)),
		wasmtest.Section(7, wasmtest.Vec(
			wasmtest.ExportEntry("grow", 0, 0),
			wasmtest.ExportEntry("size", 0, 1),
		)),
		wasmtest.Section(10, wasmtest.Vec(
			wasmtest.Code([]byte{0x20, 0x00, 0x40, 0x00, 0x0b}),
			wasmtest.Code([]byte{0x3f, 0x00, 0x0b}),
		)),
	)
	in := instantiateRegressionModule(t, mod)
	defer in.Close()
	assertI32InvokeResult(t, in, "grow", 65535, I32(0))
	assertI32InvokeResult(t, in, "size", 65535)
	assertI32InvokeResult(t, in, "grow", 65535, I32(1))
	assertI32InvokeResult(t, in, "size", 65536)
	assertI32InvokeResult(t, in, "grow", 65536, I32(0))
	assertI32InvokeResult(t, in, "grow", -1, I32(1))
}

func TestCompiledModuleInstantiationIsolation(t *testing.T) {
	mod := wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType(nil, nil))),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(0))),
		wasmtest.Section(5, wasmtest.Vec([]byte{0x01, 0x01, 0x01})),
		wasmtest.Section(7, wasmtest.Vec(
			wasmtest.ExportEntry("store", 0, 0),
			wasmtest.ExportEntry("memory", 2, 0),
		)),
		wasmtest.Section(10, wasmtest.Vec(wasmtest.Code([]byte{0x41, 0x01, 0x42, 0xe8, 0x07, 0x37, 0x03, 0x00, 0x0b}))),
	)
	compiled, err := Compile(nil, mod)
	if err != nil {
		t.Fatalf("compile: %v", err)
	}
	for i := 0; i < 100; i++ {
		in, err := Instantiate(compiled, InstantiateOptions{Imports: Imports{}})
		if err != nil {
			t.Fatalf("instantiate %d: %v", i, err)
		}
		if got := binary.LittleEndian.Uint64(in.Memory().UnsafeBytes()[1:]); got != 0 {
			_ = in.Close()
			t.Fatalf("instance %d inherited memory value %d", i, got)
		}
		if _, err := in.Invoke("store"); err != nil {
			_ = in.Close()
			t.Fatalf("instance %d store: %v", i, err)
		}
		if got := binary.LittleEndian.Uint64(in.Memory().UnsafeBytes()[1:]); got != 1000 {
			_ = in.Close()
			t.Fatalf("instance %d memory value = %d, want 1000", i, got)
		}
		if err := in.Close(); err != nil {
			t.Fatalf("close instance %d: %v", i, err)
		}
	}
}

func TestHostFunctionSeesCallerMemory(t *testing.T) {
	funcImport := append(wasmtest.Name("host"), wasmtest.Name("store_int")...)
	funcImport = append(funcImport, 0x00, 0x00) // function import, type 0
	mod := wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType(
			[]wasm.ValType{wasm.I32, wasm.I64}, []wasm.ValType{wasm.I32},
		))),
		wasmtest.Section(2, wasmtest.Vec(funcImport)),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(0))),
		wasmtest.Section(5, wasmtest.Vec([]byte{0x01, 0x01, 0x01})),
		wasmtest.Section(7, wasmtest.Vec(
			wasmtest.ExportEntry("memory", 2, 0),
			wasmtest.ExportEntry("store_int", 0, 1),
		)),
		wasmtest.Section(10, wasmtest.Vec(wasmtest.Code([]byte{0x20, 0x00, 0x20, 0x01, 0x10, 0x00, 0x0b}))),
	)
	compiled, err := Compile(nil, mod)
	if err != nil {
		t.Fatalf("compile: %v", err)
	}
	host := HostFunc(func(m HostModule, params, results []uint64) {
		offset := uint32(params[0])
		if uint64(offset)+8 > uint64(len(m.Memory())) {
			results[0] = 1
			return
		}
		binary.LittleEndian.PutUint64(m.Memory()[offset:], params[1])
		results[0] = 0
	})
	in, err := Instantiate(compiled, InstantiateOptions{Imports: Imports{"host.store_int": host}})
	if err != nil {
		t.Fatalf("instantiate: %v", err)
	}
	defer in.Close()
	got, err := in.Invoke("store_int", I32(1), math.MaxUint64)
	if err != nil || len(got) != 1 || AsI32(got[0]) != 0 {
		t.Fatalf("store_int result = %v, err %v", got, err)
	}
	want := []byte{0, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0}
	if mem := in.Memory().UnsafeBytes()[:10]; string(mem) != string(want) {
		t.Fatalf("memory prefix = %x, want %x", mem, want)
	}
}

func TestRecursiveHostReentry(t *testing.T) {
	funcImport := append(wasmtest.Name("env"), wasmtest.Name("host_func")...)
	funcImport = append(funcImport, 0x00, 0x00)
	mainBody := []byte{
		0x02, 0x40, // block
		0x03, 0x40, // loop
		0x20, 0x00, 0x45, 0x0d, 0x01, // local.get 0; eqz; br_if block
		0x20, 0x00, 0x41, 0x7f, 0x6a, 0x21, 0x00, // decrement local 0
		0x10, 0x00, 0x0c, 0x00, // call host; br loop
		0x0b, 0x0b, 0x0b,
	}
	mod := wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(
			wasmtest.FuncType(nil, nil),
			wasmtest.FuncType([]wasm.ValType{wasm.I32}, nil),
			wasmtest.FuncType(nil, []wasm.ValType{wasm.I32}),
		)),
		wasmtest.Section(2, wasmtest.Vec(funcImport)),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(1), wasmtest.ULEB(2))),
		wasmtest.Section(7, wasmtest.Vec(
			wasmtest.ExportEntry("main", 0, 1),
			wasmtest.ExportEntry("called_by_host_func", 0, 2),
		)),
		wasmtest.Section(10, wasmtest.Vec(
			wasmtest.Code(mainBody),
			wasmtest.Code([]byte{0x41, 0xe4, 0x00, 0x0b}),
		)),
	)
	compiled, err := Compile(nil, mod)
	if err != nil {
		t.Fatalf("compile: %v", err)
	}
	var in *Instance
	hostCalls := 0
	host := HostFunc(func(mod HostModule, _, _ []uint64) {
		hostCalls++
		got, callErr := in.InvokeFromHost(context.Background(), mod, "called_by_host_func")
		if callErr != nil || len(got) != 1 || AsI32(got[0]) != 100 {
			t.Errorf("recursive host re-entry = %v, err %v", got, callErr)
		}
	})
	in, err = Instantiate(compiled, InstantiateOptions{Imports: Imports{"env.host_func": host}})
	if err != nil {
		t.Fatalf("instantiate: %v", err)
	}
	defer in.Close()
	if _, err := in.Invoke("main", I32(3)); err != nil {
		t.Fatalf("main: %v", err)
	}
	if hostCalls != 3 {
		t.Fatalf("host calls = %d, want 3", hostCalls)
	}
}

func TestHostReentryDepthCounterBoundsAndReleases(t *testing.T) {
	id := newInvocationID()
	for want := uint8(1); want <= maxHostReentryDepth; want++ {
		if got, ok := acquireHostReentryDepth(id); !ok || got != want {
			t.Fatalf("acquire depth = %d, %t, want %d, true", got, ok, want)
		}
	}
	if got, ok := acquireHostReentryDepth(id); ok || got != maxHostReentryDepth {
		t.Fatalf("overflow acquire depth = %d, %t, want %d, false", got, ok, maxHostReentryDepth)
	}
	for range maxHostReentryDepth {
		releaseHostReentryDepth(id)
	}
	hostReentryDepths.Lock()
	_, retained := hostReentryDepths.values[id]
	hostReentryDepths.Unlock()
	if retained {
		t.Fatal("released host re-entry depth retained its invocation")
	}
	if got, ok := acquireHostReentryDepth(id); !ok || got != 1 {
		t.Fatalf("reacquire depth = %d, %t, want 1, true", got, ok)
	}
	releaseHostReentryDepth(id)
}

func TestHostReentryDepthCounterReleasesOversizedMap(t *testing.T) {
	ids := make([]invocationID, maxRetainedHostReentryChains+1)
	for i := range ids {
		ids[i] = newInvocationID()
		if _, ok := acquireHostReentryDepth(ids[i]); !ok {
			t.Fatalf("acquire invocation %d failed", i)
		}
	}
	hostReentryDepths.Lock()
	resetWhenIdle := hostReentryDepths.resetWhenIdle
	hostReentryDepths.Unlock()
	if !resetWhenIdle {
		t.Fatal("oversized host re-entry map was not marked for reset")
	}
	for _, id := range ids {
		releaseHostReentryDepth(id)
	}
	hostReentryDepths.Lock()
	values := hostReentryDepths.values
	resetWhenIdle = hostReentryDepths.resetWhenIdle
	hostReentryDepths.Unlock()
	if values != nil || resetWhenIdle {
		t.Fatalf("drained oversized host re-entry map = %p, reset %t; want nil, false", values, resetWhenIdle)
	}
}

func assertHostReentryDepthsEmpty(t *testing.T) {
	t.Helper()
	hostReentryDepths.Lock()
	entries := len(hostReentryDepths.values)
	resetWhenIdle := hostReentryDepths.resetWhenIdle
	hostReentryDepths.Unlock()
	if entries != 0 || resetWhenIdle {
		t.Fatalf("host re-entry accounting after unwind = %d entries, reset %t; want 0, false", entries, resetWhenIdle)
	}
}

func TestRecursiveHostReentryDepthIsBounded(t *testing.T) {
	funcImport := append(wasmtest.Name("env"), wasmtest.Name("reenter")...)
	funcImport = append(funcImport, 0x00, 0x00)
	mod := wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType(nil, nil))),
		wasmtest.Section(2, wasmtest.Vec(funcImport)),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(0))),
		wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("recurse", 0, 1))),
		wasmtest.Section(10, wasmtest.Vec(wasmtest.Code([]byte{0x10, 0x00, 0x0b}))),
	)
	compiled, err := Compile(nil, mod)
	if err != nil {
		t.Fatal(err)
	}
	defer compiled.Close()
	var in *Instance
	host := HostFunc(func(caller HostModule, _, _ []uint64) {
		if _, err := in.InvokeFromHost(context.Background(), caller, "recurse"); err != nil {
			panic(HostTrap{Err: err})
		}
	})
	in, err = Instantiate(compiled, InstantiateOptions{Imports: Imports{"env.reenter": host}})
	if err != nil {
		t.Fatal(err)
	}
	defer in.Close()
	if _, err := in.Invoke("recurse"); err == nil || !errors.Is(err, ErrPermissionDenied) || !strings.Contains(err.Error(), "host re-entry depth") {
		t.Fatalf("recursive re-entry error = %v, want bounded-depth rejection", err)
	}
	assertHostReentryDepthsEmpty(t)
}

func TestRecursiveHostReentryDepthCrossesInstanceExport(t *testing.T) {
	typeSection := wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType(nil, nil)))
	producerImport := append(wasmtest.Name("env"), wasmtest.Name("reenter")...)
	producerImport = append(producerImport, 0x00, 0x00)
	producerModule := wasmtest.Module(
		typeSection,
		wasmtest.Section(2, wasmtest.Vec(producerImport)),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(0))),
		wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("call", 0, 1))),
		wasmtest.Section(10, wasmtest.Vec(wasmtest.Code([]byte{0x10, 0x00, 0x0b}))),
	)
	relayImport := append(wasmtest.Name("env"), wasmtest.Name("call")...)
	relayImport = append(relayImport, 0x00, 0x00)
	relayModule := wasmtest.Module(
		typeSection,
		wasmtest.Section(2, wasmtest.Vec(relayImport)),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(0))),
		wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("recurse", 0, 1))),
		wasmtest.Section(10, wasmtest.Vec(wasmtest.Code([]byte{0x10, 0x00, 0x0b}))),
	)
	producerCompiled, err := Compile(nil, producerModule)
	if err != nil {
		t.Fatal(err)
	}
	defer producerCompiled.Close()
	relayCompiled, err := Compile(nil, relayModule)
	if err != nil {
		t.Fatal(err)
	}
	defer relayCompiled.Close()

	var relay *Instance
	hostCalls := 0
	host := HostFunc(func(caller HostModule, _, _ []uint64) {
		hostCalls++
		if _, err := relay.InvokeFromHost(context.Background(), caller, "recurse"); err != nil {
			panic(HostTrap{Err: err})
		}
	})
	producer, err := Instantiate(producerCompiled, InstantiateOptions{Imports: Imports{"env.reenter": host}})
	if err != nil {
		t.Fatal(err)
	}
	defer producer.Close()
	export, err := producer.ExportedFunc("call")
	if err != nil {
		t.Fatal(err)
	}
	relay, err = Instantiate(relayCompiled, InstantiateOptions{Imports: Imports{"env.call": export}})
	if err != nil {
		t.Fatal(err)
	}
	defer relay.Close()

	if _, err := relay.Invoke("recurse"); err == nil || !errors.Is(err, ErrPermissionDenied) || !strings.Contains(err.Error(), "host re-entry depth") {
		t.Fatalf("cross-instance recursive re-entry error = %v, want bounded-depth rejection", err)
	}
	if hostCalls != maxHostReentryDepth+1 {
		t.Fatalf("host calls = %d, want %d", hostCalls, maxHostReentryDepth+1)
	}
	assertHostReentryDepthsEmpty(t)
}

func TestRecursiveHostReentryDepthTracksSavedOuterCaller(t *testing.T) {
	callImportModule := func(importName, exportName string) []byte {
		functionImport := append(wasmtest.Name("env"), wasmtest.Name(importName)...)
		functionImport = append(functionImport, 0x00, 0x00)
		return wasmtest.Module(
			wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType(nil, nil))),
			wasmtest.Section(2, wasmtest.Vec(functionImport)),
			wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(0))),
			wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry(exportName, 0, 1))),
			wasmtest.Section(10, wasmtest.Vec(wasmtest.Code([]byte{0x10, 0x00, 0x0b}))),
		)
	}
	outerCompiled, err := Compile(nil, callImportModule("enter", "start"))
	if err != nil {
		t.Fatal(err)
	}
	defer outerCompiled.Close()
	recursiveCompiled, err := Compile(nil, callImportModule("reenter", "recurse"))
	if err != nil {
		t.Fatal(err)
	}
	defer recursiveCompiled.Close()

	var outerCaller HostModule
	var recursive *Instance
	hostCalls := 0
	reenter := HostFunc(func(_ HostModule, _, _ []uint64) {
		hostCalls++
		if _, err := recursive.InvokeFromHost(context.Background(), outerCaller, "recurse"); err != nil {
			panic(HostTrap{Err: err})
		}
	})
	recursive, err = Instantiate(recursiveCompiled, InstantiateOptions{Imports: Imports{"env.reenter": reenter}})
	if err != nil {
		t.Fatal(err)
	}
	defer recursive.Close()
	enter := HostFunc(func(caller HostModule, _, _ []uint64) {
		outerCaller = caller
		if _, err := recursive.InvokeFromHost(context.Background(), caller, "recurse"); err != nil {
			panic(HostTrap{Err: err})
		}
	})
	outer, err := Instantiate(outerCompiled, InstantiateOptions{Imports: Imports{"env.enter": enter}})
	if err != nil {
		t.Fatal(err)
	}
	defer outer.Close()

	if _, err := outer.Invoke("start"); err == nil || !errors.Is(err, ErrPermissionDenied) || !strings.Contains(err.Error(), "host re-entry depth") {
		t.Fatalf("saved-caller recursive re-entry error = %v, want bounded-depth rejection", err)
	}
	if hostCalls != maxHostReentryDepth {
		t.Fatalf("saved-caller host calls = %d, want %d", hostCalls, maxHostReentryDepth)
	}
	assertHostReentryDepthsEmpty(t)
}

func TestConcurrentPublicInvokeWaitsForParkedHostCallback(t *testing.T) {
	funcImport := append(wasmtest.Name("env"), wasmtest.Name("host")...)
	funcImport = append(funcImport, 0x00, 0x00)
	mod := wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType(
			[]wasm.ValType{wasm.I32}, []wasm.ValType{wasm.I32},
		))),
		wasmtest.Section(2, wasmtest.Vec(funcImport)),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(0))),
		wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("run", 0, 1))),
		wasmtest.Section(10, wasmtest.Vec(wasmtest.Code([]byte{
			0x20, 0x00, // local.get 0
			0x10, 0x00, // call env.host
			0x0b,
		}))),
	)
	compiled, err := Compile(nil, mod)
	if err != nil {
		t.Fatalf("compile: %v", err)
	}
	defer compiled.Close()

	type callback struct {
		id     int32
		caller HostModule
	}
	entered := make(chan callback, 2)
	release := map[int32]chan struct{}{1: make(chan struct{}), 2: make(chan struct{})}
	host := HostFunc(func(caller HostModule, params, results []uint64) {
		id := AsI32(params[0])
		entered <- callback{id: id, caller: caller}
		<-release[id]
		results[0] = I32(id + 10)
	})
	in, err := Instantiate(compiled, InstantiateOptions{Imports: Imports{"env.host": host}})
	if err != nil {
		t.Fatalf("instantiate: %v", err)
	}
	defer in.Close()

	type outcome struct {
		id  int32
		got []uint64
		err error
	}
	done := make(chan outcome, 2)
	invoke := func(id int32) {
		got, err := in.Invoke("run", I32(id))
		done <- outcome{id: id, got: got, err: err}
	}
	go invoke(1)
	firstCallback := <-entered
	if firstCallback.id != 1 {
		t.Fatalf("first callback id = %d, want 1", firstCallback.id)
	}
	go invoke(2)

	select {
	case callback := <-entered:
		close(release[1])
		close(release[2])
		<-done
		<-done
		t.Fatalf("unrelated invocation %d entered while invocation 1 was parked", callback.id)
	case <-time.After(50 * time.Millisecond):
	}

	close(release[1])
	first := <-done
	if first.id != 1 || first.err != nil || len(first.got) != 1 || AsI32(first.got[0]) != 11 {
		t.Fatalf("first invocation = id %d, %v, %v; want 1, [11], nil", first.id, first.got, first.err)
	}
	secondCallback := <-entered
	if secondCallback.id != 2 {
		t.Fatalf("second callback id = %d, want 2", secondCallback.id)
	}
	if _, err := in.InvokeFromHost(context.Background(), firstCallback.caller, "run", I32(3)); err == nil || !strings.Contains(err.Error(), "permission denied") {
		t.Fatalf("expired first callback re-entry = %v, want permission denied", err)
	}
	close(release[2])
	second := <-done
	if second.id != 2 || second.err != nil || len(second.got) != 1 || AsI32(second.got[0]) != 12 {
		t.Fatalf("second invocation = id %d, %v, %v; want 2, [12], nil", second.id, second.got, second.err)
	}
}

func TestCallArityAndMultiResult(t *testing.T) {
	mod := wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType(
			[]wasm.ValType{wasm.I64, wasm.I64}, []wasm.ValType{wasm.I64, wasm.I64},
		))),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(0))),
		wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("func", 0, 0))),
		wasmtest.Section(10, wasmtest.Vec(wasmtest.Code([]byte{0x20, 0x00, 0x20, 0x01, 0x0b}))),
	)
	in := instantiateRegressionModule(t, mod)
	defer in.Close()
	got, err := in.Invoke("func", 1, 2)
	if err != nil || len(got) != 2 || got[0] != 1 || got[1] != 2 {
		t.Fatalf("func(1,2) = %v, err %v", got, err)
	}
	if _, err := in.Invoke("func"); err == nil {
		t.Fatal("call with no parameters succeeded")
	}
	if _, err := in.Invoke("func", 1, 2, 3); err == nil {
		t.Fatal("call with too many parameters succeeded")
	}
}

func TestARM64UremRegalloc(t *testing.T) {
	mod, err := base64.StdEncoding.DecodeString(uremRegallocWasmBase64)
	if err != nil {
		t.Fatalf("decode upstream fixture: %v", err)
	}
	// The original reproducer grows its 17-page memory through a host API. Wago
	// intentionally exposes growth only to wasm, so raise the encoded
	// initial size to 300 pages while leaving all function bytecode untouched.
	oldMemorySection := []byte{0x05, 0x03, 0x01, 0x00, 0x11}
	newMemorySection := []byte{0x05, 0x04, 0x01, 0x00, 0xac, 0x02}
	sectionAt := bytes.Index(mod, oldMemorySection)
	if sectionAt < 0 {
		t.Fatal("upstream fixture memory section not found")
	}
	resized := make([]byte, 0, len(mod)+1)
	resized = append(resized, mod[:sectionAt]...)
	resized = append(resized, newMemorySection...)
	resized = append(resized, mod[sectionAt+len(oldMemorySection):]...)

	compiled, err := Compile(nil, resized)
	if err != nil {
		t.Fatalf("compile: %v", err)
	}
	in, err := Instantiate(compiled, InstantiateOptions{Imports: Imports{
		"repro.update_nonce": HostFunc(func(_ HostModule, _, _ []uint64) {}),
	}})
	if err != nil {
		t.Fatalf("instantiate: %v", err)
	}
	defer in.Close()
	self := in.Memory().UnsafeBytes()[0x100000:]
	binary.LittleEndian.PutUint32(self[8:], 8)
	binary.LittleEndian.PutUint32(self[12:], 2)
	binary.LittleEndian.PutUint32(self[16:], 1)
	if err := in.SetGlobal("__stack_pointer", I32(0xfff00)); err != nil {
		t.Fatalf("set stack pointer: %v", err)
	}
	got, err := in.Invoke("fill_blocks", I32(0x100000), I32(0x120000), I32(8), I32(0xfff00))
	if err != nil || len(got) != 1 || AsI32(got[0]) != 18 {
		t.Fatalf("fill_blocks = %v, err %v; want 18", got, err)
	}
}

type crossRuntimeImportExt struct{}

func (crossRuntimeImportExt) Info() ExtensionInfo {
	return ExtensionInfo{ID: "test.cross-runtime", Version: "1.0.0", Stability: Stable}
}

func (crossRuntimeImportExt) Register(reg *Registry) error {
	reg.ImportModule("env").
		Func("proxy", HostFunc(func(_ HostModule, p, r []uint64) { r[0] = p[1] })).
		Params(ValI32, ValI64).Results(ValI64)
	return nil
}

func TestHugeCallStackUnwindsToStartTrap(t *testing.T) {
	mod, err := os.ReadFile(filepath.Join("..", "..", "tests", "regressions", "engine", "huge_call_stack_unwind.wasm"))
	if err != nil {
		t.Fatalf("read upstream fixture: %v", err)
	}
	compiled, err := Compile(nil, mod)
	if err != nil {
		t.Fatalf("compile: %v", err)
	}
	defer compiled.Close()
	in, err := Instantiate(compiled, InstantiateOptions{Imports: Imports{}})
	if in != nil {
		_ = in.Close()
		t.Fatal("recursive trapping start function unexpectedly instantiated")
	}
	if err == nil || !strings.Contains(err.Error(), "start function trapped: wasm trap: integer division by zero") {
		t.Fatalf("start trap = %v, want integer division by zero after deep unwind", err)
	}
}

func TestCrossRuntimeInstantiationUsesStructuralImportTypes(t *testing.T) {
	funcImport := append(wasmtest.Name("env"), wasmtest.Name("proxy")...)
	funcImport = append(funcImport, 0x00, 0x02) // function import, type index 2
	mod := wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(
			wasmtest.FuncType([]wasm.ValType{wasm.I32}, []wasm.ValType{wasm.I32}),
			wasmtest.FuncType([]wasm.ValType{wasm.I64}, []wasm.ValType{wasm.I64}),
			wasmtest.FuncType([]wasm.ValType{wasm.I32, wasm.I64}, []wasm.ValType{wasm.I64}),
		)),
		wasmtest.Section(2, wasmtest.Vec(funcImport)),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(0))),
		wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("f", 0, 1))),
		wasmtest.Section(10, wasmtest.Vec(wasmtest.Code([]byte{0x20, 0x00, 0x0b}))),
	)

	rt1 := NewRuntime()
	defer rt1.Close()
	compiled, err := rt1.Compile(mod)
	if err != nil {
		t.Fatalf("compile in runtime 1: %v", err)
	}
	defer compiled.Close()

	rt2 := NewRuntime()
	defer rt2.Close()
	if err := rt2.Use(crossRuntimeImportExt{}); err != nil {
		t.Fatalf("register runtime 2 import: %v", err)
	}
	if _, err := rt2.Instantiate(context.Background(), compiled); err == nil || !strings.Contains(err.Error(), "different runtime") {
		t.Fatalf("cross-runtime module accepted: %v", err)
	}
	bound, err := rt2.Module(compiled.Compiled())
	if err != nil {
		t.Fatalf("bind compiled artifact to runtime 2: %v", err)
	}
	in, err := rt2.Instantiate(context.Background(), bound)
	if err != nil {
		t.Fatalf("runtime-2 module with structurally identical import: %v", err)
	}
	defer in.Close()
	got, err := in.Invoke("f", I32(37))
	if err != nil || len(got) != 1 || AsI32(got[0]) != 37 {
		t.Fatalf("f(37) = %v, err %v", got, err)
	}
}

func TestHugeMixedValueStack(t *testing.T) {
	mod, err := os.ReadFile(filepath.Join("..", "..", "tests", "regressions", "engine", "hugestack.wasm"))
	if err != nil {
		t.Fatalf("read upstream fixture: %v", err)
	}
	compiled, err := Compile(nil, mod)
	if err != nil {
		t.Fatalf("compile: %v", err)
	}
	defer compiled.Close()
	in, err := Instantiate(compiled, InstantiateOptions{Imports: Imports{}})
	if err != nil {
		t.Fatalf("instantiate: %v", err)
	}
	defer in.Close()
	verify := func(t *testing.T, got []uint64, err error) {
		t.Helper()
		if err != nil {
			t.Fatalf("invoke: %v", err)
		}
		if len(got) != 180 {
			t.Fatalf("result slots = %d, want 180", len(got))
		}
		for i, value := range got {
			if value != uint64(i+1) {
				t.Fatalf("result slot %d = %d, want %d", i, value, i+1)
			}
		}
	}
	got, err := in.Invoke("main", 0, 0, 0, 0, 0, 0)
	verify(t, got, err)

	offsets := []int32{0, 2, 4, 8, 16, 32, 48, 64, 128, 256, 512, 1024, 2048, 4096, 8192, 16384, 32768}
	sizes := []int32{0, 2, 4, 8, 16, 32, 48, 64, 128, 256, 512, 1024, 2048, 4096, 8192, 16384}
	for _, offset := range offsets {
		for _, size := range sizes {
			got, err = in.Invoke("memory_fill_after_main", I32(offset), I32(0xff), I32(size))
			verify(t, got, err)
		}
	}
}

func instantiateRegressionModule(t *testing.T, mod []byte) *Instance {
	t.Helper()
	compiled, err := Compile(nil, mod)
	if err != nil {
		t.Fatalf("compile: %v", err)
	}
	t.Cleanup(func() {
		if err := compiled.Close(); err != nil {
			t.Errorf("close compiled module: %v", err)
		}
	})
	in, err := Instantiate(compiled, InstantiateOptions{Imports: Imports{}})
	if err != nil {
		t.Fatalf("instantiate: %v", err)
	}
	return in
}

func assertI32InvokeResult(t *testing.T, in *Instance, export string, want int32, args ...uint64) {
	t.Helper()
	got, err := in.Invoke(export, args...)
	if err != nil || len(got) != 1 || AsI32(got[0]) != want {
		t.Fatalf("%s%v = %v, err %v; want %d", export, args, got, err, want)
	}
}
