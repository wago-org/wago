// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.
//
// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Wago authors and contributors

package options

import (
	"errors"

	"github.com/wago-org/wago/cli/internal/command"
)

type Action string

const (
	Interactive Action = "interactive"
	List        Action = "list"
	Get         Action = "get"
	Set         Action = "set"
	Reset       Action = "reset"
	Diff        Action = "diff"
)

type Request struct {
	Action       Action
	Key          string
	Value        string
	Experimental bool
	All          bool
	Global       bool
	Local        bool
}

type Environment interface {
	Configure(Request)
}

func ScopeFlags() []command.Flag {
	return []command.Flag{
		{Name: "global", Short: "g", Bool: true, Help: "read or change user-wide defaults"},
		{Name: "local", Short: "l", Bool: true, Help: "read or change this project's overrides"},
	}
}

func ApplyScope(ctx *command.Ctx, request *Request) error {
	request.Global, request.Local = ctx.Bool("global"), ctx.Bool("local")
	if request.Global && request.Local {
		return errors.New("choose --global or --local")
	}
	return nil
}
