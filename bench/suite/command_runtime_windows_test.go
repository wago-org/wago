// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.
//
// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Wago authors and contributors

//go:build windows

package wagobench

import (
	"fmt"
	"io"

	"github.com/wago-org/wago"
)

func commandRuntimeImports(m corpusModule, _ string, _ []byte, _, _ io.Writer) (*wago.Imports, error) {
	if m.Command.Runtime == "core" {
		return nil, nil
	}
	return nil, fmt.Errorf("unsupported command runtime %q on windows", m.Command.Runtime)
}
