// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.
//
// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Wago authors and contributors

//go:build windows && (amd64 || arm64) && !tinygo

package wago

import (
	"context"
	"errors"
	"runtime"
	"sync/atomic"
	"testing"
	"time"
	"unsafe"

	wruntime "github.com/wago-org/wago/src/core/runtime"
)

func TestCancellationWatchRearmsOverwrittenTrap(t *testing.T) {
	var in Instance
	trap := make([]byte, 4)
	cell := (*uint32)(unsafe.Pointer(&trap[0]))
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	stop, err := in.startCancellationWatch(ctx, trap)
	if err != nil {
		t.Fatal(err)
	}
	defer stop()
	cancel()
	deadline := time.Now().Add(time.Second)
	for atomic.LoadUint32(cell) != uint32(wruntime.TrapInterrupted) && time.Now().Before(deadline) {
		runtime.Gosched()
	}
	if atomic.LoadUint32(cell) != uint32(wruntime.TrapInterrupted) {
		t.Fatal("cancellation did not set the trap")
	}
	// A native host call can publish its pending status after cancellation.
	atomic.StoreUint32(cell, 0)
	deadline = time.Now().Add(100 * time.Millisecond)
	for atomic.LoadUint32(cell) != uint32(wruntime.TrapInterrupted) && time.Now().Before(deadline) {
		runtime.Gosched()
	}
	if atomic.LoadUint32(cell) != uint32(wruntime.TrapInterrupted) {
		t.Fatal("cancellation was lost after the native status overwrote the trap")
	}
}

func TestManagedVoidTableReportsHostCancellation(t *testing.T) {
	rt := NewRuntime()
	defer rt.Close()
	manager := newPendingInstanceManager("cancel-test", AuthorityScope{})
	manager.activate(rt)
	defer manager.close()
	mod, err := rt.Compile(managedReentryModule())
	if err != nil {
		t.Fatal(err)
	}
	defer mod.Close()
	var cancel context.CancelFunc
	owned, err := manager.Instantiate(nil, mod, WithImports(testImports("env.host", slotHostFunc(func(_ HostModule, _, _ []uint64) {
		cancel()
	}))))
	if err != nil {
		t.Fatal(err)
	}
	defer owned.Close()
	for i := 0; i < 20; i++ {
		ctx, stop := context.WithCancel(context.Background())
		cancel = stop
		err := owned.InvokeVoidTable(ctx, 0)
		stop()
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("iteration %d: table call = %v, want context cancellation", i, err)
		}
	}
}
