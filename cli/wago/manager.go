// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.
//
// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Wago authors and contributors

//go:build !wago_runtime

// Command wago manages Wago installations and dispatches runtime commands to
// the selected wago-runtime executable.
package main

import "github.com/wago-org/wago/cli/manager"

// version is stamped at build time via -ldflags "-X main.version=<tag>" (see
// `just build`). It must be an uninitialized var: TinyGo only honors -X for
// variables declared without an initializer.
var version string

func main() { manager.Main(version) }
