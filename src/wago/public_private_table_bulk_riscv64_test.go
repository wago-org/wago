//go:build linux && riscv64 && !tinygo && !wago_target_tinygo

package wago

import (
	"context"
	"encoding/binary"
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

func TestRV64PrivateTableGrowthReservationBoundaries(t *testing.T) {
	for _, tc := range []struct {
		minimum       uint32
		hasMax, admit bool
	}{{512, false, true}, {513, false, true}, {513, true, true}, {1024, false, true}, {1025, false, true}, {1025, true, false}} {
		c, err := Compile(ownedTableConfig(), privateTableBulkSizedModule(tc.hasMax, tc.minimum, 1024))
		if !tc.admit {
			if c != nil || err == nil {
				if c != nil {
					_ = c.Close()
				}
				t.Fatal("minimum exceeds declared maximum", c, err)
			}
			continue
		}
		if err != nil {
			t.Fatal(err)
		}
		defer c.Close()
		in, err := Instantiate(c)
		if err != nil {
			t.Fatal(err)
		}
		defer in.Close()
		target := max(uint32(1024), tc.minimum)
		importedGlobalInvoke(t, in, "grow-a", []uint64{uint64(tc.minimum)}, uint64(target-tc.minimum))
		importedGlobalInvoke(t, in, "size", []uint64{uint64(target)})
		if tc.minimum == target {
			ownedTableTrap(t, invokePrivateReference(t, in, "call", uint64(target-1), 5), coreruntime.TrapIndirectOutOfBounds)
			importedGlobalInvoke(t, in, "fill-a", nil, uint64(target-1), 1)
		}
		importedGlobalInvoke(t, in, "call", []uint64{12}, uint64(target-1), 5)
	}
}

func privateTableMutationLoop(operation string) []byte {
	minimum := uint32(1024)
	if operation == "grow" {
		minimum = 2
	}
	limits := append([]byte{1, 0x70, 1}, wasmtest.ULEB(minimum)...)
	limits = append(limits, wasmtest.ULEB(1024)...)
	elements := []uint32{1, 2}
	if operation == "copy" {
		elements = make([]uint32, 1024)
		for i := range elements {
			elements[i] = uint32(1 + i%2)
		}
	}
	flag := []byte{0x41, 0, 0x41, 1, 0x36, 2, 0}
	body := slices.Clone(flag)
	body = append(body, 0x03, 0x40)
	switch operation {
	case "fill":
		for _, ref := range []byte{1, 2} {
			body = append(body, 0x41, 2, 0xd2, ref)
			body = append(body, 0x41)
			body = append(body, wasmtest.SLEB32(1022)...)
			body = append(body, 0xfc, 17, 0)
		}
	case "copy":
		body = append(body, 0x41, 1, 0x41, 0, 0x41)
		body = append(body, wasmtest.SLEB32(1023)...)
		body = append(body, 0xfc, 14, 0, 0)
		body = append(body, 0x41, 0, 0x41, 1, 0x41)
		body = append(body, wasmtest.SLEB32(1023)...)
		body = append(body, 0xfc, 14, 0, 0)
	case "grow":
		body = append(body, 0xd2, 1, 0x41)
		body = append(body, wasmtest.SLEB32(1022)...)
		body = append(body, 0xfc, 15, 0, 0x1a)
	}
	body = append(body, 0x0c, 0, 0x0b, 0x0b)
	return wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType(nil, nil), wasmtest.FuncType(nil, []wasm.ValType{wasm.I32}))),
		wasmtest.Section(3, []byte{5, 0, 1, 1, 1, 1}), wasmtest.Section(4, limits), wasmtest.Section(5, []byte{1, 0, 1}),
		wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("spin", 0, 0), wasmtest.ExportEntry("value", 0, 3), wasmtest.ExportEntry("retry-grow", 0, 4))), ownedTableElements(0, elements...),
		wasmtest.Section(10, wasmtest.Vec(wasmtest.Code(body), wasmtest.Code([]byte{0x41, 7, 0x0b}), wasmtest.Code([]byte{0x41, 9, 0x0b}), wasmtest.Code([]byte{0x41, 0, 0x11, 1, 0, 0x0b}), wasmtest.Code([]byte{0xd2, 2, 0x41, 1, 0xfc, 15, 0, 0x0b}))),
	)
}

func TestRV64PrivateTableBulkCancellationCompleteEntriesAndGrowth(t *testing.T) {
	rv64PrivateTableBulkCancellation(t, false, false)
}
func rv64PrivateTableBulkCancellation(t *testing.T, privateFrame, vector bool) {
	previous := runtime.GOMAXPROCS(1)
	defer runtime.GOMAXPROCS(previous)
	for _, operation := range []string{"fill", "copy", "grow"} {
		t.Run(operation, func(t *testing.T) {
			var c *Compiled
			if privateFrame {
				c = rv64ReferenceTableBulkCompile(t, "cancel-"+operation, privateTableMutationLoop(operation), vector, false)
			} else {
				c = ownedTableCompile(t, privateTableMutationLoop(operation))
			}
			var in *Instance
			var err error
			if privateFrame {
				in = rv64ReferenceTableBulkInstantiate(t, c)
			} else {
				in, err = Instantiate(c)
			}
			if err != nil {
				t.Fatal(err)
			}
			defer in.Close()
			a := slices.Clone(in.funcRefDescs[80:112])
			b := slices.Clone(in.funcRefDescs[120:152])
			zero := make([]byte, 32)
			witnessIndex := 2
			if operation == "copy" {
				witnessIndex = 1023
			}
			witness := (*uint64)(unsafe.Pointer(&privateTableBytes(in)[8+witnessIndex*32]))
			initialCode := atomic.LoadUint64(witness)
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			done := make(chan error, 1)
			go func() { _, err := in.InvokeContext(ctx, "spin"); done <- err }()
			flag := (*uint32)(unsafe.Pointer(&in.jm.CurrentBytes()[0]))
			deadline := time.Now().Add(5 * time.Second)
			for (atomic.LoadUint32(flag) == 0 || atomic.LoadUint64(witness) == initialCode) && time.Now().Before(deadline) {
				runtime.Gosched()
			}
			entered := atomic.LoadUint32(flag) != 0 && atomic.LoadUint64(witness) != initialCode
			cancel()
			select {
			case err := <-done:
				if !entered || !errors.Is(err, context.Canceled) {
					t.Fatal("bulk entry/cancel", entered, err)
				}
			case <-time.After(5 * time.Second):
				stop := coreruntime.RequestInterruptAsync(in.trap)
				<-done
				stop()
				t.Fatal("bulk cancellation stalled")
			}
			table := privateTableBytes(in)
			length := binary.LittleEndian.Uint32(table)
			if operation == "grow" && length != 2 && length != 1024 {
				t.Fatal("partial length publication", length)
			}
			for i := uint32(0); i < length; i++ {
				entry := table[8+int(i)*32 : 8+int(i+1)*32]
				if !slices.Equal(entry, a) && !slices.Equal(entry, b) && !slices.Equal(entry, zero) {
					t.Fatal("partial descriptor after cancel", i)
				}
				if operation == "grow" && i >= 2 && !slices.Equal(entry, a) {
					t.Fatal("growth published uninitialized entry", i)
				}
			}
			importedGlobalInvoke(t, in, "value", []uint64{7})
			if operation == "grow" {
				if length == 2 {
					importedGlobalInvoke(t, in, "retry-grow", []uint64{2})
					if !slices.Equal(privateTableBytes(in)[72:104], b) {
						t.Fatal("retry did not overwrite hidden initialization")
					}
				} else {
					importedGlobalInvoke(t, in, "retry-grow", []uint64{0xffffffff})
				}
			}
		})
	}
}
