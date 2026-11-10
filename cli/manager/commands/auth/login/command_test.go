// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.
//
// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Wago authors and contributors

package login

import (
	"testing"

	"github.com/wago-org/wago/cli/internal/command"
)

type testEnvironment struct {
	options Options
}

func (e *testEnvironment) Login(options Options) { e.options = options }

func TestRunOwnsLoginFlagParsing(t *testing.T) {
	environment := &testEnvironment{}
	Command(environment).Run(command.NewContext(
		nil,
		map[string]string{"token": "secret"},
		nil,
	))
	if environment.options.Token != "secret" || environment.options.WithToken {
		t.Fatalf("options = %#v", environment.options)
	}
}
