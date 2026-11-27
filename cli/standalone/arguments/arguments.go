// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.
//
// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Wago authors and contributors

// Package arguments parses scalar arguments for native WebAssembly launchers.
package arguments

import (
	"fmt"
	"math"
	"strconv"
	"strings"
)

type Type uint8

const (
	I32 Type = iota
	I64
	F32
	F64
)

// Decode parses a scalar signature. Pass nil slots to validate the arguments
// without storing their bits, or a slot for each parameter to keep the values.
func Decode[T ~uint8](values []string, params []T, slots []uint64) error {
	if len(values) != len(params) {
		return fmt.Errorf("expected %d arg(s), got %d", len(params), len(values))
	}
	for index, value := range values {
		valueType := Type(params[index])
		literal := value
		if suffix := strings.LastIndexByte(value, ':'); suffix >= 0 {
			literal = value[:suffix]
			switch value[suffix+1:] {
			case "i32":
				valueType = I32
			case "i64":
				valueType = I64
			case "f32":
				valueType = F32
			case "f64":
				valueType = F64
			default:
				return fmt.Errorf("arg %d: bad type suffix in %q", index, value)
			}
		}
		parsed, err := ParseValue(literal, valueType)
		if err != nil {
			return fmt.Errorf("arg %d (%q): %w", index, value, err)
		}
		if slots != nil {
			slots[index] = parsed
		}
	}
	return nil
}

func ParseValue(value string, valueType Type) (uint64, error) {
	switch valueType {
	case I64:
		if integer, err := strconv.ParseInt(value, 0, 64); err == nil {
			return uint64(integer), nil
		}
		unsigned, err := strconv.ParseUint(value, 0, 64)
		return unsigned, err
	case F32:
		float, err := strconv.ParseFloat(value, 32)
		return uint64(math.Float32bits(float32(float))), err
	case F64:
		float, err := strconv.ParseFloat(value, 64)
		return math.Float64bits(float), err
	default:
		if integer, err := strconv.ParseInt(value, 0, 32); err == nil {
			return uint64(uint32(integer)), nil
		}
		unsigned, err := strconv.ParseUint(value, 0, 32)
		return unsigned, err
	}
}
