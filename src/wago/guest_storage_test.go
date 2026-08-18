//go:build (linux || darwin || windows) && (amd64 || arm64)

package wago

import (
	"context"
	"strings"
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
	"github.com/wago-org/wago/tests/wasmtest"
)

func guestStorageHostModuleBytes() []byte {
	hostImport := append(wasmtest.Name("host"), wasmtest.Name("inspect")...)
	hostImport = append(hostImport, 0x00, 0x00) // function import, type 0
	return wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(
			wasmtest.FuncType([]wasm.ValType{wasm.I32}, []wasm.ValType{wasm.I64}),
			wasmtest.FuncType(nil, []wasm.ValType{wasm.I32}),
		)),
		wasmtest.Section(2, wasmtest.Vec(hostImport)),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(1), wasmtest.ULEB(1))),
		wasmtest.Section(5, wasmtest.Vec([]byte{0x01, 0x01, 0x01})),
		wasmtest.Section(7, wasmtest.Vec(
			wasmtest.ExportEntry("run", 0, 1),
			wasmtest.ExportEntry("peek", 0, 2),
		)),
		wasmtest.Section(10, wasmtest.Vec(
			wasmtest.Code([]byte{
				0x41, 0x07, // i32.const 7
				0x10, 0x00, // call imported host.inspect
				0x1a,       // drop i64 result
				0x41, 0x00, // i32.const 0
				0x2d, 0x00, 0x00, // i32.load8_u align=0 offset=0
				0x0b,
			}),
			wasmtest.Code([]byte{0x41, 0x00, 0x2d, 0x00, 0x00, 0x0b}),
		)),
	)
}

func TestHostGuestStorageCallbackLifetimeAndReentry(t *testing.T) {
	compiled, err := Compile(NewRuntimeConfig().WithBoundsChecks(BoundsChecksExplicit), guestStorageHostModuleBytes())
	if err != nil {
		t.Fatal(err)
	}
	defer compiled.Close()

	var in *Instance
	var retained GuestStorage
	var callbackCalls int
	host := HostFunc(func(m HostModule, args, results []uint64) {
		callbackCalls++
		if len(args) != 1 || uint32(args[0]) != 7 || len(results) != 1 {
			panic(HostTrap{Err: context.Canceled})
		}
		storageModule, ok := m.(GuestStorageHostModule)
		if !ok {
			panic(HostTrap{Err: context.DeadlineExceeded})
		}
		if err := storageModule.WithGuestStorage(func(storage GuestStorage) error {
			retained = storage
			info, err := storage.MemoryInfo(0)
			if err != nil {
				return err
			}
			if info.AddressType != GuestMemory32 || info.ByteLength != 65536 {
				return &guestStorageTestError{"memory info"}
			}
			buf, err := storage.MemoryRange(0, 0, 1, GuestStorageWrite)
			if err != nil {
				return err
			}
			buf[0] = 42
			if _, err := storage.MemoryRange(0, info.ByteLength, 1, GuestStorageRead); err == nil {
				return &guestStorageTestError{"out-of-bounds range accepted"}
			}
			param, ok := storage.ImportParamType(0)
			if !ok || param.Kind != ValueTypeI32 {
				return &guestStorageTestError{"exact parameter type"}
			}
			result, ok := storage.ImportResultType(0)
			if !ok || result.Kind != ValueTypeI64 {
				return &guestStorageTestError{"exact result type"}
			}
			if err := storageModule.WithGuestStorage(func(GuestStorage) error { return nil }); err == nil {
				return &guestStorageTestError{"nested borrow accepted"}
			}
			if _, err := in.InvokeFromHost(context.Background(), m, "peek"); err == nil || !strings.Contains(err.Error(), "guest storage is borrowed") {
				return &guestStorageTestError{"re-entry during borrow was not rejected"}
			}
			gcModule, ok := m.(GCHostModule)
			if !ok {
				return &guestStorageTestError{"GC host module unavailable"}
			}
			if err := gcModule.CollectGC(); err == nil || !strings.Contains(err.Error(), "guest storage is borrowed") {
				return &guestStorageTestError{"collection during borrow was not rejected"}
			}
			results[0] = 0
			return nil
		}); err != nil {
			panic(HostTrap{Err: err})
		}
	})

	in, err = Instantiate(compiled, InstantiateOptions{Imports: Imports{"host.inspect": host}})
	if err != nil {
		t.Fatal(err)
	}
	defer in.Close()
	got, err := in.Invoke("run")
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || uint32(got[0]) != 42 || callbackCalls != 1 {
		t.Fatalf("run = %v calls=%d, want [42]/1", got, callbackCalls)
	}
	if retained == nil {
		t.Fatal("host callback did not retain the test view")
	}
	if _, err := retained.MemoryInfo(0); err == nil || !strings.Contains(err.Error(), "no longer active") {
		t.Fatalf("expired guest-storage view error = %v", err)
	}
}

type guestStorageTestError struct{ what string }

func (e *guestStorageTestError) Error() string { return "guest storage test: " + e.what }
