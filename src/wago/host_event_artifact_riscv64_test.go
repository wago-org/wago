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
	"errors"
	"testing"
	"time"
)

func TestRV64HostEventCancelCloseAndReentry(t *testing.T) {
	c := rv64TableStageCompile(t, "host-event-lifecycle", hostEventLoopModule(), true)
	defer c.Close()
	var in *Instance
	calls := 0
	var nestedErr error
	var closeErr error
	in, err := Instantiate(c, testImports("env.event", I32HostEvent(func(int32) {
		calls++
		if calls == 1 {
			ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
			_, nestedErr = in.InvokeContext(ctx, "run", 0)
			cancel()
			closeErr = in.Close()
		}
	})))
	if err != nil {
		t.Fatal(err)
	}
	defer in.Close()
	cancelled, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := in.InvokeContext(cancelled, "run", 2); !errors.Is(err, context.Canceled) || calls != 0 {
		t.Fatal("cancelled event admission", err, calls)
	}
	if _, err := in.Invoke("run", 2); err != nil || calls != 2 || closeErr != nil {
		t.Fatal("active replay Close", err, calls, closeErr)
	}
	if !errors.Is(nestedErr, context.DeadlineExceeded) {
		t.Fatal("capability-free nested call acquired live gate", nestedErr)
	}
	if _, err := in.Invoke("run", 1); err == nil || calls != 2 {
		t.Fatal("closed event instance entered", err, calls)
	}
}
