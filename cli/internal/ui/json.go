// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.
//
// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Wago authors and contributors

package ui

import (
	"bytes"
	"encoding/json"
	"fmt"
)

// PrintJSON emits stable, indented JSON without HTML escaping.
func PrintJSON(value any) {
	var buffer bytes.Buffer
	encoder := json.NewEncoder(&buffer)
	encoder.SetEscapeHTML(false)
	encoder.SetIndent("", "  ")
	if err := encoder.Encode(value); err != nil {
		Fatal("json: %v", err)
	}
	fmt.Print(buffer.String())
}
