// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.
//
// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Wago authors and contributors

// Package catalog defines wago plugin catalog.
package catalog

import "github.com/wago-org/wago/cli/internal/command"

type Options struct {
	Manifest string
	Check    bool
}

type Environment interface {
	Catalog(Options)
}

func Command(environment Environment) *command.Cmd {
	return &command.Cmd{
		Name:       "catalog",
		Summary:    "generate the immutable provider catalog snapshot",
		Automation: command.DryRun,
		Flags: []command.Flag{
			{Name: "manifest", Short: "m", Arg: "<p>", Help: "manifest path (default wago.json)"},
			{Name: "check", Bool: true, Help: "verify the committed snapshot is current"},
		},
		Run: func(c *command.Ctx) {
			environment.Catalog(Options{Manifest: c.Str("manifest"), Check: c.Bool("check")})
		},
	}
}
