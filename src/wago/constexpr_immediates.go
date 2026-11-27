// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.
//
// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Wago authors and contributors

package wago

import (
	"fmt"

	"github.com/wago-org/wago/internal/buildfeatures"
	"github.com/wago-org/wago/src/core/compiler/wasm"
)

const (
	gcConstOpcodesForImage             = buildfeatures.StandaloneGCConstOpcodes
	constExprLowOpcodesForImage        = buildfeatures.StandaloneConstExprLowOpcodes
	constExprNumericOpcodesForImage    = buildfeatures.StandaloneConstExprNumericOpcodes
	constExprReferenceOpcodesForImage  = buildfeatures.StandaloneConstExprReferenceOpcodes
	constExprArithmeticOpcodesForImage = buildfeatures.StandaloneConstExprArithmeticOpcodes
)

func requiredFeaturesForConstExprBytes(body []byte, importedGlobals int) CoreFeatures {
	r := wasm.ReaderFrom(body)
	var out CoreFeatures
	usesArithmetic, usesPriorLocal := false, false
	for r.HasNext() {
		op, err := r.Byte()
		if err != nil {
			break
		}
		index, features, err := readConstImmediate(&r, op)
		out |= features
		if err != nil {
			break
		}
		switch {
		case constExprLowOpcodesForImage&(1<<0x23) != 0 && op == 0x23:
			usesPriorLocal = usesPriorLocal || uint64(index) >= uint64(importedGlobals)
		case constExprArithmeticOpcodesForImage != 0 && constExprArithmeticOpcodesForImage&(uint64(1)<<(op-0x40)) != 0:
			usesArithmetic = true
		}
	}
	if usesPriorLocal {
		out |= CoreFeatureExtendedConstExpressions
	} else if usesArithmetic {
		out |= CoreFeatureExtendedConst
	}
	return out
}

// readConstImmediate scans the initializer grammar retained in a compiled
// image. Ordinary function instructions never enter this grammar.
func readConstImmediate(r *wasm.Reader, op byte) (index uint32, features CoreFeatures, err error) {
	switch {
	case op == 0x0b || constExprArithmeticOpcodesForImage != 0 && constExprArithmeticOpcodesForImage&(uint64(1)<<(op-0x40)) != 0:
		return 0, 0, nil
	case constExprLowOpcodesForImage&(1<<0x23) != 0 && op == 0x23:
		index, err = r.U32()
		return index, 0, err
	case constExprNumericOpcodesForImage&(1<<(0x41-0x40)) != 0 && op == 0x41:
		_, err = r.I32()
	case constExprNumericOpcodesForImage&(1<<(0x42-0x40)) != 0 && op == 0x42:
		_, err = r.I64()
	case constExprNumericOpcodesForImage&(1<<(0x43-0x40)) != 0 && op == 0x43:
		_, err = r.Bytes(4)
	case constExprNumericOpcodesForImage&(1<<(0x44-0x40)) != 0 && op == 0x44:
		_, err = r.Bytes(8)
	case constExprReferenceOpcodesForImage&(1<<(0xd0-0xc0)) != 0 && op == 0xd0:
		_, heap, err := r.RefHeapType()
		if err != nil {
			return 0, 0, err
		}
		return 0, requiredFeaturesForHeapImmediate(heap), nil
	case constExprReferenceOpcodesForImage&(1<<(0xd2-0xc0)) != 0 && op == 0xd2:
		index, err = r.U32()
		return index, CoreFeatureReferenceTypes, err
	case constExprReferenceOpcodesForImage&(1<<(0xfb-0xc0)) != 0 && op == 0xfb:
		const gc = CoreFeatureReferenceTypes | CoreFeatureGC
		if !wasmGCBuilt {
			return 0, gc, checkCompiledFeatures(CoreFeatureGC)
		}
		sub, err := r.U32()
		if err != nil {
			return 0, 0, err
		}
		// struct.new, struct.new_default, array.new, array.new_default
		const typeIndexOpcodes = gcConstOpcodesForImage & (1<<0 | 1<<1 | 1<<6 | 1<<7)
		// any.convert_extern, extern.convert_any, ref.i31
		const operandlessOpcodes = gcConstOpcodesForImage & (1<<26 | 1<<27 | 1<<28)
		switch {
		case typeIndexOpcodes != 0 && typeIndexOpcodes&(uint32(1)<<sub) != 0:
			_, err = r.U32()
		case gcConstOpcodesForImage&(1<<8) != 0 && sub == 8: // array.new_fixed
			if _, err = r.U32(); err == nil {
				_, err = r.U32()
			}
		case operandlessOpcodes != 0 && operandlessOpcodes&(uint32(1)<<sub) != 0:
		default:
			return 0, 0, fmt.Errorf("unsupported GC constant expression opcode %d", sub)
		}
		return sub, gc, err
	case constExprReferenceOpcodesForImage&(1<<(0xfd-0xc0)) != 0 && op == 0xfd:
		if buildfeatures.SIMD == 0 {
			return 0, CoreFeatureSIMD, checkCompiledFeatures(CoreFeatureSIMD)
		}
		_, err = readV128ConstExpr(r)
		return 0, CoreFeatureSIMD, err
	default:
		return 0, 0, fmt.Errorf("unsupported constant expression opcode 0x%02x", op)
	}
	return 0, 0, err
}
