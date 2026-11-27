// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.
//
// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Wago authors and contributors

// Package wasmcall parses and formats typed WebAssembly function values.
package wasmcall

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/wago-org/wago"
	"github.com/wago-org/wago/cli/standalone/arguments"
	"github.com/wago-org/wago/internal/buildfeatures"
)

// ValidateSignature rejects values that the command-line text format cannot
// represent. The low-level Go API remains available for v128 and reference
// values.
func ValidateSignature(params, results []wago.ValType) error {
	if err := validateSignatureValues("parameter", params); err != nil {
		return err
	}
	return validateSignatureValues("result", results)
}

func validateSignatureValues(kind string, types []wago.ValType) error {
	for index, typ := range types {
		if typ <= wago.ValF64 {
			continue
		}
		return fmt.Errorf("%s %d is %s; unsupported", kind, index, typ)
	}
	return nil
}

// The standalone argument parser uses the same scalar codes as wago.ValType.
var (
	_ [arguments.I32]struct{} = [wago.ValI32]struct{}{}
	_ [arguments.I64]struct{} = [wago.ValI64]struct{}{}
	_ [arguments.F32]struct{} = [wago.ValF32]struct{}{}
	_ [arguments.F64]struct{} = [wago.ValF64]struct{}{}
)

func ParseArgs(values []string, params []wago.ValType) ([]uint64, error) {
	if err := ValidateSignature(params, nil); err != nil {
		return nil, err
	}
	return ParseScalarArgs(values, params)
}

// ParseScalarArgs decodes parameters from an already validated scalar signature.
func ParseScalarArgs[T ~uint8](values []string, params []T) ([]uint64, error) {
	var slots []uint64
	if len(values) == len(params) {
		slots = make([]uint64, len(params))
	}
	if err := arguments.Decode(values, params, slots); err != nil {
		return nil, err
	}
	return slots, nil
}

func ParseValue(value string, valueType wago.ValType) (uint64, error) {
	return arguments.ParseValue(value, arguments.Type(valueType))
}

func FormatValue(bits uint64, valueType wago.ValType) string {
	switch {
	case buildfeatures.StandaloneResultTypes&(1<<wago.ValI64) != 0 && valueType == wago.ValI64:
		return strconv.FormatInt(wago.AsI64(bits), 10)
	case buildfeatures.StandaloneResultTypes&(1<<wago.ValF32) != 0 && valueType == wago.ValF32:
		return strconv.FormatFloat(float64(wago.AsF32(bits)), 'g', -1, 32)
	case buildfeatures.StandaloneResultTypes&(1<<wago.ValF64) != 0 && valueType == wago.ValF64:
		return strconv.FormatFloat(wago.AsF64(bits), 'g', -1, 64)
	default:
		return strconv.FormatInt(int64(wago.AsI32(bits)), 10)
	}
}

// FormatResults renders raw function results without a call-expression prefix.
func FormatResults[T ~uint8](results []uint64, resultTypes []T) string {
	if len(results) != len(resultTypes) {
		return fmt.Sprintf("invalid result count: got %d values for %d types", len(results), len(resultTypes))
	}
	formatted := make([]string, len(results))
	for index, value := range results {
		formatted[index] = FormatValue(value, wago.ValType(resultTypes[index]))
	}
	return strings.Join(formatted, ", ")
}
