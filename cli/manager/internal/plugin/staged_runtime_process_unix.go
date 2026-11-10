// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.
//
// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Wago authors and contributors

//go:build linux || darwin

package plugin

import (
	"errors"
	"os"
	"os/exec"
	"syscall"
)

// runBoundStagedRuntime is defense in depth behind the platform process-creation
// restriction: the private group still makes cancellation and cleanup atomic.
func runBoundStagedRuntime(command *exec.Cmd) error {
	command.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	command.Cancel = func() error {
		return killStagedRuntimeGroup(command)
	}
	err := command.Run()
	// Also reap descendants when the runtime exits before its deadline. Without
	// this cleanup, a successful initializer could leave a background child.
	cleanupErr := killStagedRuntimeGroup(command)
	if errors.Is(cleanupErr, os.ErrProcessDone) {
		cleanupErr = nil
	}
	return errors.Join(err, cleanupErr)
}

func killStagedRuntimeGroup(command *exec.Cmd) error {
	if command.Process == nil {
		// Start failures have no process group to reap; preserve the original
		// exec error instead of panicking while performing defensive cleanup.
		return os.ErrProcessDone
	}
	err := syscall.Kill(-command.Process.Pid, syscall.SIGKILL)
	if errors.Is(err, syscall.ESRCH) {
		return os.ErrProcessDone
	}
	return err
}
