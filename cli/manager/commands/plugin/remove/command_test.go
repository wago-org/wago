// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.
//
// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Wago authors and contributors

package remove

import "testing"

type testEnvironment struct{ options Options }

func (e *testEnvironment) Remove(options Options) { e.options = options }

func TestCommandForwardsContractReviewChoice(t *testing.T) {
	environment := new(testEnvironment)
	command := Command(environment)
	ctx, err := command.Parse("wago plugin remove", []string{
		"--accept-contracts", "github.com/acme/plugin",
	})
	if err != nil {
		t.Fatal(err)
	}
	command.Run(ctx)
	if environment.options.Name != "github.com/acme/plugin" || !environment.options.AcceptContracts {
		t.Fatalf("options = %#v", environment.options)
	}
}
