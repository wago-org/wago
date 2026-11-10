// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.
//
// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Wago authors and contributors

//go:build darwin

package plugin

import (
	"context"
	"errors"
	"os"
	"os/exec"
)

func stagedRuntimeCommand(ctx context.Context, binary string) (*exec.Cmd, error) {
	const sandbox = "/usr/bin/sandbox-exec"
	if _, err := os.Stat(sandbox); err != nil {
		return nil, errors.New("staged runtime process containment is unavailable")
	}
	// Plugin validation only initializes metadata; denying fork prevents an
	// initializer from escaping process-group cleanup through a new session.
	return exec.CommandContext(ctx, sandbox, "-p", "(version 1) (allow default) (deny process-fork)", binary), nil
}
