//go:build linux && riscv64 && wago_rv64_experimental && !tinygo && !wago_target_tinygo

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

	railshot "github.com/wago-org/wago/src/core/compiler/backend/railshot/riscv64"
	"github.com/wago-org/wago/src/core/compiler/wasm"
	coreruntime "github.com/wago-org/wago/src/core/runtime"
	"github.com/wago-org/wago/tests/support/wasmtest"
)

func TestRV64PrivateElementArtifactBoundaries(t *testing.T) {
	c := ownedTableCompile(t, privateTableElementsModule())
	base := c.executionView()
	for _, tc := range []struct {
		name   string
		mutate func(*Compiled)
	}{
		{"count", func(v *Compiled) { v.passiveElems = make([]ElemInit, 257) }},
		{"missing-bulk", func(v *Compiled) { v.requiredFeatures &^= CoreFeatureBulkMemoryOperations }},
		{"unknown-mode", func(v *Compiled) { v.passiveElems[2].Mode = ElemMode(255) }},
		{"table-index", func(v *Compiled) { v.passiveElems[2].TableIndex = 1 }},
		{"invalid-exact-type", func(v *Compiled) {
			v.passiveElems[2].HasValueType = true
			v.passiveElems[2].ValueTypeIndex = ^uint32(0)
		}},
		{"extern", func(v *Compiled) { v.passiveElems[2].RefType = ValExternRef }},
		{"unused-offset", func(v *Compiled) { v.passiveElems[2].Offset.Base = 1 }},
		{"global-offset", func(v *Compiled) { v.passiveElems[2].Offset.HasGlobal = true }},
		{"expression-offset", func(v *Compiled) { v.passiveElems[2].Offset.Expr = []byte{0x41, 0, 0x0b} }},
		{"active-payload", func(v *Compiled) { v.passiveElems[0].Values = []RefInit{{FuncIndex: 0}} }},
		{"declarative-payload", func(v *Compiled) { v.passiveElems[1].Values = []RefInit{{FuncIndex: 1}} }},
		{"aggregate", func(v *Compiled) { v.passiveElems[2].Values = make([]RefInit, 1024) }},
		{"unknown-function", func(v *Compiled) { v.passiveElems[2].Values[0].FuncIndex = uint32(len(v.Funcs)) }},
		{"null", func(v *Compiled) { v.passiveElems[2].Values[0].Null = true }},
		{"global", func(v *Compiled) { v.passiveElems[2].Values[0].HasGlobal = true }},
		{"i31", func(v *Compiled) { v.passiveElems[2].Values[0].I31Wrap = true }},
		{"repeat", func(v *Compiled) { v.passiveElems[2].Values[0].RepeatPrevious = true }},
		{"expression", func(v *Compiled) { v.passiveElems[2].Values[0].Expr = []byte{0xd2, 0, 0x0b} }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			v := *base
			v.passiveElems = slices.Clone(base.passiveElems)
			for i := range v.passiveElems {
				v.passiveElems[i].Values = slices.Clone(base.passiveElems[i].Values)
			}
			tc.mutate(&v)
			if err := railshotValidateCompiled(&v); !errors.Is(err, railshot.ErrUnsupported) {
				t.Fatal("element metadata escaped", err)
			}
		})
	}
}

func TestRV64PrivateTableInitActiveCancellationAndReuse(t *testing.T) {
	previous := runtime.GOMAXPROCS(1)
	defer runtime.GOMAXPROCS(previous)
	functions := make([]uint32, 1024)
	for i := range functions {
		functions[i] = uint32(1 + i%2)
	}
	body := []byte{0x41, 0, 0x41, 1, 0x36, 2, 0, 0x03, 0x40}
	for _, source := range []byte{0, 1} {
		body = append(body, 0x41, 0, 0x41, source, 0x41)
		body = append(body, wasmtest.SLEB32(1023)...)
		body = append(body, 0xfc, 12, 0, 0)
	}
	body = append(body, 0x0c, 0, 0x0b, 0x0b)

	data := wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType(nil, nil), wasmtest.FuncType(nil, []wasm.ValType{wasm.I32}))),
		wasmtest.Section(3, []byte{5, 0, 1, 1, 1, 0}), wasmtest.Section(4, []byte{1, 0x70, 1, 0x80, 8, 0x80, 8}), wasmtest.Section(5, []byte{1, 0, 1}),
		wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("spin", 0, 0), wasmtest.ExportEntry("value", 0, 3), wasmtest.ExportEntry("drop", 0, 4))),
		wasmtest.Section(9, wasmtest.Vec(privateLegacyElement(1, functions...))),
		wasmtest.Section(10, wasmtest.Vec(wasmtest.Code(body), wasmtest.Code([]byte{0x41, 7, 0x0b}), wasmtest.Code([]byte{0x41, 9, 0x0b}), wasmtest.Code([]byte{0x41, 0, 0x11, 1, 0, 0x0b}), wasmtest.Code([]byte{0xfc, 13, 0, 0x0b}))),
	)
	c := ownedTableCompile(t, data)
	blob, err := c.MarshalBinary()
	if err != nil {
		t.Fatal(err)
	}
	loaded, err := LoadTrustedArtifact(blob)
	if err != nil {
		t.Fatal(err)
	}
	defer loaded.Close()
	in, err := Instantiate(loaded)
	if err != nil {
		t.Fatal(err)
	}
	defer in.Close()
	_ = c.Close()
	_ = loaded.Close()
	runtime.GC()
	a := slices.Clone(in.funcRefDescs[80:112])
	b := slices.Clone(in.funcRefDescs[120:152])
	zero := make([]byte, 32)
	table := privateTableBytes(in)
	witness := (*uint64)(unsafe.Pointer(&table[8]))
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() { _, err := in.InvokeContext(ctx, "spin"); done <- err }()
	flag := (*uint32)(unsafe.Pointer(&in.jm.CurrentBytes()[0]))
	deadline := time.Now().Add(5 * time.Second)
	for (atomic.LoadUint32(flag) == 0 || atomic.LoadUint64(witness) == 0) && time.Now().Before(deadline) {
		runtime.Gosched()
	}
	entered := atomic.LoadUint32(flag) != 0 && atomic.LoadUint64(witness) != 0
	cancel()
	select {
	case err := <-done:
		if !entered || !errors.Is(err, context.Canceled) {
			t.Fatal("table.init entry/cancel", entered, err)
		}
	case <-time.After(5 * time.Second):
		stop := coreruntime.RequestInterruptAsync(in.trap)
		<-done
		stop()
		t.Fatal("table.init cancellation stalled")
	}
	for i := 0; i < 1024; i++ {
		entry := table[8+i*32 : 8+(i+1)*32]
		if !slices.Equal(entry, a) && !slices.Equal(entry, b) && !slices.Equal(entry, zero) {
			t.Fatal("partial table.init descriptor", i)
		}
	}
	expected := uint64(7)
	if slices.Equal(table[8:40], b) {
		expected = 9
	}
	importedGlobalInvoke(t, in, "value", []uint64{expected})
	importedGlobalInvoke(t, in, "drop", nil)
	ownedTableTrap(t, invokePrivateReference(t, in, "spin"), coreruntime.TrapTableOutOfBounds)
	importedGlobalInvoke(t, in, "value", []uint64{expected})
}
