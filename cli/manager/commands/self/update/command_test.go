// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.
//
// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Wago authors and contributors

package update

import (
	"testing"

	"github.com/wago-org/wago/cli/internal/command"
)

type testEnvironment struct{ force bool }

func (environment *testEnvironment) Update(force bool) { environment.force = force }

func TestCommandForwardsForce(t *testing.T) {
	environment := &testEnvironment{}
	Command(environment).Run(command.NewContext(nil, nil, map[string]bool{"force": true}))
	if !environment.force {
		t.Fatal("self update did not forward --force")
	}
}
