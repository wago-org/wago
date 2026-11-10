// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.
//
// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Wago authors and contributors

package switchcmd

import (
	"testing"

	"github.com/wago-org/wago/cli/internal/command"
)

type testEnvironment struct {
	called                  bool
	version, profile, build string
}

func (e *testEnvironment) Switch(version, profile, build string) {
	e.called = true
	e.version, e.profile, e.build = version, profile, build
}

func TestRunDelegatesInteractiveSelectionWhenVersionIsOmitted(t *testing.T) {
	environment := &testEnvironment{}
	Command(environment).Run(command.NewContext(nil, nil, nil))
	if !environment.called || environment.version != "" {
		t.Fatalf("switch = %v %q", environment.called, environment.version)
	}
}

func TestRunForwardsSelection(t *testing.T) {
	environment := &testEnvironment{}
	Command(environment).Run(command.NewContext(
		[]string{"canary"},
		map[string]string{"profile": "minimal", "build": "tiny"},
		nil,
	))
	if environment.version != "canary" ||
		environment.profile != "minimal" ||
		environment.build != "tiny" {
		t.Fatalf("selection = %q/%s/%s", environment.version, environment.profile, environment.build)
	}
}
