//go:build linux && riscv64 && !tinygo && !wago_target_tinygo

package wago

import (
	"context"
	"errors"
	"runtime"
	"slices"
	"sync/atomic"
	"testing"
	"time"
	"unsafe"

	"github.com/wago-org/wago/src/core/compiler/wasm"
	coreruntime "github.com/wago-org/wago/src/core/runtime"
	"github.com/wago-org/wago/tests/support/wasmtest"
)

func TestRV64PrivateReferenceCancellationPreservesCompleteEntry(t *testing.T) {
	previous := runtime.GOMAXPROCS(1)
	defer runtime.GOMAXPROCS(previous)
	data := wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType(nil, nil), wasmtest.FuncType(nil, []wasm.ValType{wasm.I32}))),
		wasmtest.Section(3, []byte{4, 0, 1, 1, 1}), ownedTableDeclaration(3), wasmtest.Section(5, []byte{1, 0, 1}),
		wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("spin", 0, 0), wasmtest.ExportEntry("value", 0, 3))), ownedTableElements(0, 1, 2),
		wasmtest.Section(10, wasmtest.Vec(
			wasmtest.Code([]byte{0x03, 0x40, 0x41, 2, 0xd2, 1, 0x26, 0, 0x41, 0, 0x41, 1, 0x36, 2, 0, 0x41, 2, 0xd2, 2, 0x26, 0, 0x0c, 0, 0x0b, 0x0b}),
			wasmtest.Code([]byte{0x41, 7, 0x0b}),
			wasmtest.Code([]byte{0x41, 9, 0x0b}),
			wasmtest.Code([]byte{0x41, 2, 0x11, 1, 0, 0x0b}),
		)),
	)
	c := ownedTableCompile(t, data)

	in, err := Instantiate(c)
	if err != nil {
		t.Fatal(err)
	}
	defer in.Close()
	_ = c.Close()
	_ = c.Close()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() { _, err := in.InvokeContext(ctx, "spin"); done <- err }()
	flag := (*uint32)(unsafe.Pointer(&in.jm.CurrentBytes()[0]))
	deadline := time.Now().Add(5 * time.Second)
	for atomic.LoadUint32(flag) == 0 && time.Now().Before(deadline) {
		runtime.Gosched()
	}
	entered := atomic.LoadUint32(flag) != 0
	cancel()
	select {
	case err := <-done:
		if !entered || !errors.Is(err, context.Canceled) {
			t.Fatal("mutation did not enter/cancel", entered, err)
		}
	case <-time.After(5 * time.Second):
		stop := coreruntime.RequestInterruptAsync(in.trap)
		<-done
		stop()
		t.Fatal("mutation loop failed to stop")
	}
	table := unsafe.Slice((*byte)(offHeapPtr(in.tableDescPtr)), in.tableDescLen)
	expected := uint64(7)
	if slices.Equal(table[72:104], table[40:72]) {
		expected = 9
	} else if !slices.Equal(table[72:104], table[8:40]) {
		t.Fatal("interrupt exposed a partial entry")
	}
	importedGlobalInvoke(t, in, "value", []uint64{expected})
}
