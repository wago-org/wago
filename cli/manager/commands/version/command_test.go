// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.
//
// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Wago authors and contributors

package version

import (
	"strings"
	"testing"

	"github.com/wago-org/wago/cli/manager/commands/version/install"
)

type testEnvironment struct{}

func (testEnvironment) List()                                                            {}
func (testEnvironment) Current()                                                         {}
func (testEnvironment) Which()                                                           {}
func (testEnvironment) Switch(string, string, string)                                    {}
func (testEnvironment) InstallRequested(install.Options)                                 {}
func (testEnvironment) UpdateVersion([]string, bool, bool, bool, string, string, string) {}
func (testEnvironment) UninstallVersions([]string)                                       {}
func (testEnvironment) UninstallAllVersions()                                            {}

func TestCommandTree(t *testing.T) {
	cmd := Command(testEnvironment{})
	var names []string
	for _, child := range cmd.Children {
		names = append(names, child.Name)
	}
	if got := strings.Join(names, ","); got != "list,current,which,switch,install,update,uninstall" {
		t.Fatalf("version commands = %q", got)
	}
	if got := strings.Join(cmd.Children[3].Aliases, ","); got != "use,swap" || cmd.Children[4].Aliases[0] != "add" {
		t.Fatalf("version aliases = switch:%v install:%v", cmd.Children[3].Aliases, cmd.Children[4].Aliases)
	}
}
