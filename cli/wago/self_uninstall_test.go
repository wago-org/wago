// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.
//
// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Wago authors and contributors

//go:build !wago_runtime && !windows

package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

func TestSelfUninstallThroughAliasedWagoHome(t *testing.T) {
	root := t.TempDir()
	physicalParent := filepath.Join(root, "physical")
	aliasParent := filepath.Join(root, "alias")
	physicalHome := filepath.Join(physicalParent, "managed")
	aliasHome := filepath.Join(aliasParent, "managed")
	if err := os.MkdirAll(filepath.Join(physicalHome, "bin"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(physicalParent, aliasParent); err != nil {
		t.Fatal(err)
	}

	manager := filepath.Join(physicalHome, "bin", "wago")
	build := exec.Command("go", "build", "-o", manager, ".")
	if output, err := build.CombinedOutput(); err != nil {
		t.Fatalf("build manager: %v\n%s", err, output)
	}

	command := exec.Command(manager, "--no-input", "self", "uninstall", "--yes")
	command.Env = append(os.Environ(), "HOME="+root, "WAGO_HOME="+aliasHome)
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("self uninstall through WAGO_HOME alias: %v\n%s", err, output)
	}
	if _, err := os.Stat(physicalHome); !os.IsNotExist(err) {
		t.Fatalf("managed Wago home survived uninstall: %v", err)
	}
}
