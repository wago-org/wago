// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.
//
// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Wago authors and contributors

//go:build darwin

package gocommand

import (
	"context"
	"os/exec"
	"testing"
	"time"

	"golang.org/x/sys/unix"
)

func TestDarwinExitObserverCatchesImmediateLeaderExit(t *testing.T) {
	for i := 0; i < 256; i++ {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		command := &Command{Cmd: exec.CommandContext(ctx, "sh", "-c", "exit 0"), ctx: ctx}
		err := command.Run()
		cancel()
		if err != nil {
			t.Fatalf("immediate exit iteration %d: %v", i, err)
		}
	}
}

func TestDarwinExitObserverRegistersAfterChildIsZombie(t *testing.T) {
	command := exec.Command("sh", "-c", "exit 0")
	if err := command.Start(); err != nil {
		t.Fatal(err)
	}
	defer command.Wait()
	pid := command.Process.Pid
	for deadline := time.Now().Add(2 * time.Second); ; {
		info, err := unix.SysctlKinfoProc("kern.proc.pid", pid)
		if err == nil && info != nil && info.Proc.P_stat == 5 /* SZOMB */ {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("direct child never became an observable zombie: %v", err)
		}
		time.Sleep(10 * time.Millisecond)
	}
	done := make(chan error, 1)
	go func() { done <- waitDirectExit(pid) }()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("observe already-exited child: %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("observer missed exit before kqueue registration")
	}
}
