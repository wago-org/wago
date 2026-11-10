// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.
//
// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Wago authors and contributors

//go:build linux && riscv64 && !tinygo && !wago_target_tinygo

package wago

import (
	"context"
	"fmt"
	"runtime"
	"sync/atomic"
	"testing"
	"time"
	"unsafe"

	"github.com/wago-org/wago/src/core/compiler/wasm"
	"github.com/wago-org/wago/tests/support/wasmtest"
)

func rv64SharedGrowModule() []byte {
	limits := []byte{3, 1, 3}
	entry := append(wasmtest.Name("env"), wasmtest.Name("memory")...)
	entry = append(append(entry, 2), limits...)
	return wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(
			wasmtest.FuncType([]wasm.ValType{wasm.I32}, []wasm.ValType{wasm.I32}),
			wasmtest.FuncType([]wasm.ValType{wasm.I32}, []wasm.ValType{wasm.I32}),
		)),
		wasmtest.Section(2, wasmtest.Vec(entry)),
		wasmtest.Section(3, wasmtest.Vec([]byte{0}, []byte{1})),
		wasmtest.Section(7, wasmtest.Vec(
			wasmtest.ExportEntry("grow", 0, 0), wasmtest.ExportEntry("observe", 0, 1),
		)),
		wasmtest.Section(10, wasmtest.Vec(
			wasmtest.Code([]byte{0x20, 0, 0x40, 0, 0x0b}),
			// Publish entry, then repeatedly execute dynamic grow(0) in one
			// activation until another grower publishes a second page.
			wasmtest.Code([]byte{0x41, 4, 0x41, 1, 0xfe, 0x17, 2, 0,
				0x03, 0x40, 0x41, 0, 0x40, 0, 0x41, 2, 0x49, 0x0d, 0, 0x0b,
				0x41, 0x80, 0x80, 4, 0x28, 2, 0, 0x0b}),
		)),
	)
}

func rv64SharedGrowInstance(t *testing.T, quota uint32) (*Instance, *Memory) {
	t.Helper()
	memory, err := NewSharedMemory(1, 3)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { memory.Close() })
	cfg := NewRuntimeConfig().WithFeature(CoreFeatureThreads, true).WithBoundsChecks(BoundsChecksExplicit)
	c, err := cfg.Compile(rv64SharedGrowModule())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { c.Close() })
	in, err := Instantiate(c, InstantiateOptions{Imports: testImports("env.memory", memory), memoryLimitPages: quota})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { in.Close() })
	return in, memory
}

func TestRV64SharedGrowDynamicZeroBypassesQuota(t *testing.T) {
	in, memory := rv64SharedGrowInstance(t, 1)
	if got := memory.growShared(1, 0); got != 1 {
		t.Fatalf("external growth = %d, want 1", got)
	}
	for _, tc := range []struct{ delta, want uint64 }{{0, 2}, {1, ^uint64(0)}, {0, 2}, {1 << 32, 2}} {
		got, err := in.Invoke("grow", tc.delta)
		if err != nil || len(got) != 1 || got[0] != uint64(uint32(tc.want)) {
			t.Fatalf("grow(%x) = %v, %v; want %x", tc.delta, got, err, uint32(tc.want))
		}
	}
}

func TestRV64SharedGrowDynamicZeroObservesConcurrentGrowth(t *testing.T) {
	in, memory := rv64SharedGrowInstance(t, 0)
	flag := (*uint32)(unsafe.Pointer(&memory.UnsafeBytes()[4]))
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	timer := time.AfterFunc(5*time.Second, cancel)
	defer timer.Stop()
	done := make(chan error, 1)
	go func() {
		got, err := in.InvokeContext(ctx, "observe", 0)
		if err == nil && (len(got) != 1 || got[0] != 0) {
			err = fmt.Errorf("new page = %v", got)
		}
		done <- err
	}()
	for atomic.LoadUint32(flag) == 0 {
		select {
		case err := <-done:
			t.Fatalf("observer returned before growth: %v", err)
		case <-ctx.Done():
			t.Fatal(ctx.Err())
		default:
			runtime.Gosched()
		}
	}
	if got := memory.growShared(1, 0); got != 1 {
		t.Fatalf("concurrent growth = %d, want 1", got)
	}
	if err := <-done; err != nil {
		t.Fatal(err)
	}
}
