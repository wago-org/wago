// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.
//
// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Wago authors and contributors

//go:build linux && !wago_lean

package run

import (
	"errors"
	"fmt"
	"os"

	"github.com/wago-org/wago/cli/internal/handoff"
	"github.com/wago-org/wago/cli/internal/watchsupervisor"
)

func watchedChildLaunch() (string, []string, error) {
	return watchedChildLaunchWithProbe(watchsupervisor.Probe)
}

func watchedChildLaunchWithProbe(probe func(string) error) (string, []string, error) {
	manager := handoff.FromEnvironment().ManagerExecutable
	if manager == "" {
		return "", nil, errors.New("linux watch requires launch through the wago manager")
	}
	if err := probe(manager); err != nil {
		return "", nil, fmt.Errorf("linux watch requires a compatible wago manager: %w", err)
	}
	guest, err := os.Executable()
	if err != nil {
		return "", nil, err
	}
	return manager, watchsupervisor.Environment(nil, guest), nil
}
