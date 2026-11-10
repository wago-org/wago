// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.
//
// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Wago authors and contributors

//go:build amd64 && !tinygo

package main

import (
	"github.com/wago-org/wago/codegen"
	amd64 "github.com/wago-org/wago/codegen/amd64"
)

func zeroCodegen() codegen.Lowering {
	return &amd64.Lowering{
		Compatibility: amd64.CompatibilityFullAccess,
		Emit: func(ctx amd64.Context) error {
			value := ctx.AllocYMM()
			ctx.Encoder().YPxor(value, value, value)
			return ctx.OutputCustom(value)
		},
	}
}

func discardCodegen() codegen.Lowering {
	return &amd64.Lowering{
		Compatibility: amd64.CompatibilityFullAccess,
		Emit: func(ctx amd64.Context) error {
			value, err := ctx.InputCustom(0)
			if err != nil {
				return err
			}
			for _, register := range value {
				ctx.ReleaseVector(register)
			}
			return nil
		},
	}
}
