// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.
//
// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Wago authors and contributors

//go:build !linux && !darwin && !windows

package gocommand

import (
	"context"
	"os/exec"
)

// Releases target Linux, Darwin, and Windows. Keep unsupported ports
// buildable; CommandContext and WaitDelay still bound the direct Go command.
type commandTree struct{}

func prepareCommandTree(*exec.Cmd) (*commandTree, error)     { return new(commandTree), nil }
func (*commandTree) attach(context.Context, *exec.Cmd) error { return nil }
func (*commandTree) beforeWait() error                       { return nil }
func (*commandTree) terminateAndWait() error                 { return nil }
func (*commandTree) close()                                  {}
