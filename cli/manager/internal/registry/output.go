// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.
//
// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Wago authors and contributors

package registry

import "github.com/wago-org/wago/cli/internal/ui"

func fatal(format string, args ...any) { ui.Fatal(format, args...) }
func bold(value string) string         { return ui.Bold(value) }
func dim(value string) string          { return ui.Dim(value) }
func cyan(value string) string         { return ui.Cyan(value) }
