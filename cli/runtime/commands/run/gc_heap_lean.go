// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.
//
// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Wago authors and contributors

//go:build wago_lean

package run

import (
	"context"

	"github.com/wago-org/wago"
	"github.com/wago-org/wago/cli/internal/command"
)

func gcFlags() []command.Flag { return nil }
func gcLongHelp() string      { return "" }

func gcConfiguration(*command.Ctx) (wago.GCConfig, bool, error) {
	return wago.GCConfig{}, false, nil
}

func instantiate(runtime *wago.Runtime, module *wago.Module, _ wago.GCConfig, _ bool) (*wago.Instance, error) {
	return runtime.Instantiate(context.Background(), module)
}
