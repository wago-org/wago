// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.
//
// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Wago authors and contributors

package wasm

import (
	"sync"

	"github.com/wago-org/wago/internal/buildfeatures"
)

func (v *funcValidator) stepSIMD(in Instruction) error {
	e := simdOpEffect(in.Kind)
	if in.Kind == InstrI8x16Shuffle {
		for _, lane := range in.Lanes() {
			if lane >= 32 {
				return v.verr(ErrTypeMismatch, "simd lane out of range")
			}
		}
	}
	if e.laneLimit != 0 && in.Lane >= e.laneLimit {
		return v.verr(ErrTypeMismatch, "simd lane out of range")
	}
	switch e.cat {
	case simdEffLoad:
		addr, err := v.checkMemArg(in.MemArg(), uint32(e.align))
		if err != nil {
			return err
		}
		if err := v.popExpect(addr); err != nil {
			return err
		}
		v.push(V128)
		return nil
	case simdEffStore:
		addr, err := v.checkMemArg(in.MemArg(), 4)
		if err != nil {
			return err
		}
		if err := v.popExpect(V128); err != nil {
			return err
		}
		return v.popExpect(addr)
	case simdEffMemLoadLane:
		addr, err := v.checkMemArg(in.MemArg(), uint32(e.align))
		if err != nil {
			return err
		}
		if err := v.popExpect(V128); err != nil {
			return err
		}
		if err := v.popExpect(addr); err != nil {
			return err
		}
		v.push(V128)
		return nil
	case simdEffMemStoreLane:
		addr, err := v.checkMemArg(in.MemArg(), uint32(e.align))
		if err != nil {
			return err
		}
		if err := v.popExpect(V128); err != nil {
			return err
		}
		return v.popExpect(addr)
	case simdEffSplat:
		if err := v.popExpect(e.scalar.valType()); err != nil {
			return err
		}
		v.push(V128)
		return nil
	case simdEffExtract:
		if err := v.popExpect(V128); err != nil {
			return err
		}
		v.push(e.scalar.valType())
		return nil
	case simdEffReplace:
		if err := v.popExpect(e.scalar.valType()); err != nil {
			return err
		}
		if err := v.popExpect(V128); err != nil {
			return err
		}
		v.push(V128)
		return nil
	case simdEffShift:
		if err := v.popExpect(I32); err != nil {
			return err
		}
		if err := v.popExpect(V128); err != nil {
			return err
		}
		v.push(V128)
		return nil
	case simdEffUnary:
		if err := v.popExpect(V128); err != nil {
			return err
		}
		v.push(V128)
		return nil
	case simdEffBinary:
		if err := v.popExpect(V128); err != nil {
			return err
		}
		if err := v.popExpect(V128); err != nil {
			return err
		}
		v.push(V128)
		return nil
	case simdEffTernary:
		if err := v.popExpect(V128); err != nil {
			return err
		}
		if err := v.popExpect(V128); err != nil {
			return err
		}
		if err := v.popExpect(V128); err != nil {
			return err
		}
		v.push(V128)
		return nil
	case simdPopV128PushI32:
		if err := v.popExpect(V128); err != nil {
			return err
		}
		v.push(I32)
		return nil
	case simdBitselect:
		if err := v.popExpect(V128); err != nil {
			return err
		}
		if err := v.popExpect(V128); err != nil {
			return err
		}
		if err := v.popExpect(V128); err != nil {
			return err
		}
		v.push(V128)
		return nil
	case simdConst:
		v.push(V128)
		return nil
	}
	return v.verr(ErrUnsupportedValidationOpcode, in.Kind.String())
}

type simdEffectCat uint8

const (
	simdNone simdEffectCat = iota
	simdEffLoad
	simdEffStore
	simdEffMemLoadLane
	simdEffMemStoreLane
	simdEffSplat
	simdEffExtract
	simdEffReplace
	simdEffShift
	simdEffUnary
	simdEffBinary
	simdEffTernary
	simdPopV128PushI32
	simdBitselect
	simdConst
)

type simdEffect struct {
	cat       simdEffectCat
	scalar    effectValue
	align     uint8
	laneLimit LaneIdx
}

var simdEffects [InstrI8x16RelaxedSwizzle - InstrV128Load]simdEffect
var simdEffectsOnce sync.Once
var relaxedSIMDEffects = [InstrI32x4RelaxedDotI8x16I7x16AddS - InstrI8x16RelaxedSwizzle + 1]simdEffect{
	InstrI32x4RelaxedTruncF32x4S - InstrI8x16RelaxedSwizzle:     {cat: simdEffUnary},
	InstrI32x4RelaxedTruncF32x4U - InstrI8x16RelaxedSwizzle:     {cat: simdEffUnary},
	InstrI32x4RelaxedTruncZeroF64x2S - InstrI8x16RelaxedSwizzle: {cat: simdEffUnary},
	InstrI32x4RelaxedTruncZeroF64x2U - InstrI8x16RelaxedSwizzle: {cat: simdEffUnary},
	0: {cat: simdEffBinary},
	InstrF32x4RelaxedMin - InstrI8x16RelaxedSwizzle:               {cat: simdEffBinary},
	InstrF32x4RelaxedMax - InstrI8x16RelaxedSwizzle:               {cat: simdEffBinary},
	InstrF64x2RelaxedMin - InstrI8x16RelaxedSwizzle:               {cat: simdEffBinary},
	InstrF64x2RelaxedMax - InstrI8x16RelaxedSwizzle:               {cat: simdEffBinary},
	InstrI16x8RelaxedQ15mulrS - InstrI8x16RelaxedSwizzle:          {cat: simdEffBinary},
	InstrI16x8RelaxedDotI8x16I7x16S - InstrI8x16RelaxedSwizzle:    {cat: simdEffBinary},
	InstrF32x4RelaxedMadd - InstrI8x16RelaxedSwizzle:              {cat: simdEffTernary},
	InstrF32x4RelaxedNmadd - InstrI8x16RelaxedSwizzle:             {cat: simdEffTernary},
	InstrF64x2RelaxedMadd - InstrI8x16RelaxedSwizzle:              {cat: simdEffTernary},
	InstrF64x2RelaxedNmadd - InstrI8x16RelaxedSwizzle:             {cat: simdEffTernary},
	InstrI32x4RelaxedDotI8x16I7x16AddS - InstrI8x16RelaxedSwizzle: {cat: simdEffTernary},
	InstrI8x16RelaxedLaneselect - InstrI8x16RelaxedSwizzle:        {cat: simdBitselect},
	InstrI16x8RelaxedLaneselect - InstrI8x16RelaxedSwizzle:        {cat: simdBitselect},
	InstrI32x4RelaxedLaneselect - InstrI8x16RelaxedSwizzle:        {cat: simdBitselect},
	InstrI64x2RelaxedLaneselect - InstrI8x16RelaxedSwizzle:        {cat: simdBitselect},
}

func simdOpEffect(kind InstrKind) simdEffect {
	if buildfeatures.SIMD != 0 {
		if index := kind - InstrV128Load; index < InstrKind(len(simdEffects)) {
			return simdEffects[index]
		}
		if buildfeatures.RelaxedSIMD != 0 {
			if index := kind - InstrI8x16RelaxedSwizzle; index < InstrKind(len(relaxedSIMDEffects)) {
				return relaxedSIMDEffects[index]
			}
		}
	}
	return simdEffect{}
}

type simdMemEffect struct {
	kind  InstrKind
	align uint8
}

type simdScalarEffect struct {
	kind   InstrKind
	scalar effectValue
}

type simdLaneLimit struct {
	kind  InstrKind
	limit LaneIdx
}

var simdLoads = [...]simdMemEffect{
	{kind: InstrV128Load, align: 4},
	{kind: InstrV128Load8x8S, align: 3},
	{kind: InstrV128Load8x8U, align: 3},
	{kind: InstrV128Load16x4S, align: 3},
	{kind: InstrV128Load16x4U, align: 3},
	{kind: InstrV128Load32x2S, align: 3},
	{kind: InstrV128Load32x2U, align: 3},
	{kind: InstrV128Load8Splat, align: 0},
	{kind: InstrV128Load16Splat, align: 1},
	{kind: InstrV128Load32Splat, align: 2},
	{kind: InstrV128Load64Splat, align: 3},
	{kind: InstrV128Load32Zero, align: 2},
	{kind: InstrV128Load64Zero, align: 3},
}

var simdMemLane = [...]simdMemEffect{
	{kind: InstrV128Load8Lane, align: 0},
	{kind: InstrV128Load16Lane, align: 1},
	{kind: InstrV128Load32Lane, align: 2},
	{kind: InstrV128Load64Lane, align: 3},
	{kind: InstrV128Store8Lane, align: 0},
	{kind: InstrV128Store16Lane, align: 1},
	{kind: InstrV128Store32Lane, align: 2},
	{kind: InstrV128Store64Lane, align: 3},
}

var simdLaneLimits = [...]simdLaneLimit{
	{kind: InstrI8x16ExtractLaneS, limit: 16},
	{kind: InstrI8x16ExtractLaneU, limit: 16},
	{kind: InstrI8x16ReplaceLane, limit: 16},
	{kind: InstrI16x8ExtractLaneS, limit: 8},
	{kind: InstrI16x8ExtractLaneU, limit: 8},
	{kind: InstrI16x8ReplaceLane, limit: 8},
	{kind: InstrI32x4ExtractLane, limit: 4},
	{kind: InstrI32x4ReplaceLane, limit: 4},
	{kind: InstrF32x4ExtractLane, limit: 4},
	{kind: InstrF32x4ReplaceLane, limit: 4},
	{kind: InstrI64x2ExtractLane, limit: 2},
	{kind: InstrI64x2ReplaceLane, limit: 2},
	{kind: InstrF64x2ExtractLane, limit: 2},
	{kind: InstrF64x2ReplaceLane, limit: 2},
	{kind: InstrV128Load8Lane, limit: 16},
	{kind: InstrV128Store8Lane, limit: 16},
	{kind: InstrV128Load16Lane, limit: 8},
	{kind: InstrV128Store16Lane, limit: 8},
	{kind: InstrV128Load32Lane, limit: 4},
	{kind: InstrV128Store32Lane, limit: 4},
	{kind: InstrV128Load64Lane, limit: 2},
	{kind: InstrV128Store64Lane, limit: 2},
}

var simdSplat = [...]simdScalarEffect{
	{kind: InstrI8x16Splat, scalar: effectI32},
	{kind: InstrI16x8Splat, scalar: effectI32},
	{kind: InstrI32x4Splat, scalar: effectI32},
	{kind: InstrI64x2Splat, scalar: effectI64},
	{kind: InstrF32x4Splat, scalar: effectF32},
	{kind: InstrF64x2Splat, scalar: effectF64},
}

var simdExtract = [...]simdScalarEffect{
	{kind: InstrI8x16ExtractLaneS, scalar: effectI32},
	{kind: InstrI8x16ExtractLaneU, scalar: effectI32},
	{kind: InstrI16x8ExtractLaneS, scalar: effectI32},
	{kind: InstrI16x8ExtractLaneU, scalar: effectI32},
	{kind: InstrI32x4ExtractLane, scalar: effectI32},
	{kind: InstrI64x2ExtractLane, scalar: effectI64},
	{kind: InstrF32x4ExtractLane, scalar: effectF32},
	{kind: InstrF64x2ExtractLane, scalar: effectF64},
}

var simdReplace = [...]simdScalarEffect{
	{kind: InstrI8x16ReplaceLane, scalar: effectI32},
	{kind: InstrI16x8ReplaceLane, scalar: effectI32},
	{kind: InstrI32x4ReplaceLane, scalar: effectI32},
	{kind: InstrI64x2ReplaceLane, scalar: effectI64},
	{kind: InstrF32x4ReplaceLane, scalar: effectF32},
	{kind: InstrF64x2ReplaceLane, scalar: effectF64},
}

var simdShift = [...]InstrKind{
	InstrI8x16Shl,
	InstrI8x16ShrS,
	InstrI8x16ShrU,
	InstrI16x8Shl,
	InstrI16x8ShrS,
	InstrI16x8ShrU,
	InstrI32x4Shl,
	InstrI32x4ShrS,
	InstrI32x4ShrU,
	InstrI64x2Shl,
	InstrI64x2ShrS,
	InstrI64x2ShrU,
}

var simdUnary = [...]InstrKind{
	InstrI8x16Swizzle,
	InstrV128Not,
	InstrF32x4DemoteF64x2Zero,
	InstrF64x2PromoteLowF32x4,
	InstrI8x16Abs,
	InstrI8x16Neg,
	InstrI8x16Popcnt,
	InstrI16x8ExtaddPairwiseI8x16S,
	InstrI16x8ExtaddPairwiseI8x16U,
	InstrI32x4ExtaddPairwiseI16x8S,
	InstrI32x4ExtaddPairwiseI16x8U,
	InstrF32x4Ceil,
	InstrF32x4Floor,
	InstrF32x4Trunc,
	InstrF32x4Nearest,
	InstrF64x2Ceil,
	InstrF64x2Floor,
	InstrF64x2Trunc,
	InstrF64x2Nearest,
	InstrI16x8Abs,
	InstrI16x8Neg,
	InstrI32x4Abs,
	InstrI32x4Neg,
	InstrI64x2Abs,
	InstrI64x2Neg,
	InstrI64x2ExtendLowI32x4S,
	InstrI64x2ExtendHighI32x4S,
	InstrI64x2ExtendLowI32x4U,
	InstrI64x2ExtendHighI32x4U,
	InstrF32x4Abs,
	InstrF32x4Neg,
	InstrF32x4Sqrt,
	InstrF64x2Abs,
	InstrF64x2Neg,
	InstrF64x2Sqrt,
	InstrI32x4TruncSatF32x4S,
	InstrI32x4TruncSatF32x4U,
	InstrF32x4ConvertI32x4S,
	InstrF32x4ConvertI32x4U,
	InstrI32x4TruncSatF64x2SZero,
	InstrI32x4TruncSatF64x2UZero,
	InstrF64x2ConvertLowI32x4S,
	InstrF64x2ConvertLowI32x4U,
	InstrI16x8ExtendLowI8x16S,
	InstrI16x8ExtendHighI8x16S,
	InstrI16x8ExtendLowI8x16U,
	InstrI16x8ExtendHighI8x16U,
	InstrI32x4ExtendLowI16x8S,
	InstrI32x4ExtendHighI16x8S,
	InstrI32x4ExtendLowI16x8U,
	InstrI32x4ExtendHighI16x8U,
}

var simdBinary = [...]InstrKind{
	InstrI8x16Shuffle,
	InstrV128And,
	InstrV128Andnot,
	InstrV128Or,
	InstrV128Xor,
	InstrI8x16Eq,
	InstrI8x16Ne,
	InstrI8x16LtS,
	InstrI8x16LtU,
	InstrI8x16GtS,
	InstrI8x16GtU,
	InstrI8x16LeS,
	InstrI8x16LeU,
	InstrI8x16GeS,
	InstrI8x16GeU,
	InstrI16x8Eq,
	InstrI16x8Ne,
	InstrI16x8LtS,
	InstrI16x8LtU,
	InstrI16x8GtS,
	InstrI16x8GtU,
	InstrI16x8LeS,
	InstrI16x8LeU,
	InstrI16x8GeS,
	InstrI16x8GeU,
	InstrI32x4Eq,
	InstrI32x4Ne,
	InstrI32x4LtS,
	InstrI32x4LtU,
	InstrI32x4GtS,
	InstrI32x4GtU,
	InstrI32x4LeS,
	InstrI32x4LeU,
	InstrI32x4GeS,
	InstrI32x4GeU,
	InstrF32x4Eq,
	InstrF32x4Ne,
	InstrF32x4Lt,
	InstrF32x4Gt,
	InstrF32x4Le,
	InstrF32x4Ge,
	InstrF64x2Eq,
	InstrF64x2Ne,
	InstrF64x2Lt,
	InstrF64x2Gt,
	InstrF64x2Le,
	InstrF64x2Ge,
	InstrI8x16NarrowI16x8S,
	InstrI8x16NarrowI16x8U,
	InstrI8x16Shl,
	InstrI8x16ShrS,
	InstrI8x16ShrU,
	InstrI8x16Add,
	InstrI8x16AddSatS,
	InstrI8x16AddSatU,
	InstrI8x16Sub,
	InstrI8x16SubSatS,
	InstrI8x16SubSatU,
	InstrI8x16MinS,
	InstrI8x16MinU,
	InstrI8x16MaxS,
	InstrI8x16MaxU,
	InstrI8x16AvgrU,
	InstrI16x8Q15mulrSatS,
	InstrI16x8NarrowI32x4S,
	InstrI16x8NarrowI32x4U,
	InstrI16x8Shl,
	InstrI16x8ShrS,
	InstrI16x8ShrU,
	InstrI16x8Add,
	InstrI16x8AddSatS,
	InstrI16x8AddSatU,
	InstrI16x8Sub,
	InstrI16x8SubSatS,
	InstrI16x8SubSatU,
	InstrI16x8Mul,
	InstrI16x8MinS,
	InstrI16x8MinU,
	InstrI16x8MaxS,
	InstrI16x8MaxU,
	InstrI16x8AvgrU,
	InstrI16x8ExtmulLowI8x16S,
	InstrI16x8ExtmulHighI8x16S,
	InstrI16x8ExtmulLowI8x16U,
	InstrI16x8ExtmulHighI8x16U,
	InstrI32x4Add,
	InstrI32x4Sub,
	InstrI32x4Mul,
	InstrI32x4MinS,
	InstrI32x4MinU,
	InstrI32x4MaxS,
	InstrI32x4MaxU,
	InstrI32x4DotI16x8S,
	InstrI32x4ExtmulLowI16x8S,
	InstrI32x4ExtmulHighI16x8S,
	InstrI32x4ExtmulLowI16x8U,
	InstrI32x4ExtmulHighI16x8U,
	InstrI64x2Add,
	InstrI64x2Sub,
	InstrI64x2Mul,
	InstrI64x2ExtmulLowI32x4S,
	InstrI64x2ExtmulHighI32x4S,
	InstrI64x2ExtmulLowI32x4U,
	InstrI64x2ExtmulHighI32x4U,
	InstrI64x2Eq,
	InstrI64x2Ne,
	InstrI64x2LtS,
	InstrI64x2GtS,
	InstrI64x2LeS,
	InstrI64x2GeS,
	InstrF32x4Add,
	InstrF32x4Sub,
	InstrF32x4Mul,
	InstrF32x4Div,
	InstrF32x4Min,
	InstrF32x4Max,
	InstrF32x4Pmin,
	InstrF32x4Pmax,
	InstrF64x2Add,
	InstrF64x2Sub,
	InstrF64x2Mul,
	InstrF64x2Div,
	InstrF64x2Min,
	InstrF64x2Max,
	InstrF64x2Pmin,
	InstrF64x2Pmax,
}

func ensureSIMDEffects() {
	if buildfeatures.SIMD != 0 {
		simdEffectsOnce.Do(fillSIMDEffects)
	}
}

func fillSIMDEffects() {
	for _, effect := range simdLoads {
		simdEffects[effect.kind-InstrV128Load] = simdEffect{cat: simdEffLoad, align: effect.align}
	}
	simdEffects[InstrV128Store-InstrV128Load] = simdEffect{cat: simdEffStore}
	for _, effect := range simdMemLane {
		cat := simdEffMemLoadLane
		if effect.kind >= InstrV128Store8Lane && effect.kind <= InstrV128Store64Lane {
			cat = simdEffMemStoreLane
		}
		simdEffects[effect.kind-InstrV128Load] = simdEffect{cat: cat, align: effect.align}
	}
	for _, effect := range simdSplat {
		simdEffects[effect.kind-InstrV128Load] = simdEffect{cat: simdEffSplat, scalar: effect.scalar}
	}
	for _, effect := range simdExtract {
		simdEffects[effect.kind-InstrV128Load] = simdEffect{cat: simdEffExtract, scalar: effect.scalar}
	}
	for _, effect := range simdReplace {
		simdEffects[effect.kind-InstrV128Load] = simdEffect{cat: simdEffReplace, scalar: effect.scalar}
	}
	for _, kind := range simdShift {
		simdEffects[kind-InstrV128Load] = simdEffect{cat: simdEffShift}
	}
	for _, kind := range simdUnary {
		simdEffects[kind-InstrV128Load] = simdEffect{cat: simdEffUnary}
	}
	simdEffects[InstrI8x16Swizzle-InstrV128Load] = simdEffect{cat: simdEffBinary}
	for _, kind := range simdBinary {
		if simdEffects[kind-InstrV128Load].cat == simdEffShift {
			continue
		}
		simdEffects[kind-InstrV128Load] = simdEffect{cat: simdEffBinary}
	}
	for _, k := range [...]InstrKind{
		InstrV128AnyTrue,
		InstrI8x16AllTrue, InstrI16x8AllTrue, InstrI32x4AllTrue, InstrI64x2AllTrue,
		InstrI8x16Bitmask, InstrI16x8Bitmask, InstrI32x4Bitmask, InstrI64x2Bitmask,
	} {
		simdEffects[k-InstrV128Load] = simdEffect{cat: simdPopV128PushI32}
	}
	simdEffects[InstrV128Bitselect-InstrV128Load] = simdEffect{cat: simdBitselect}
	simdEffects[InstrV128Const-InstrV128Load] = simdEffect{cat: simdConst}
	for _, lane := range simdLaneLimits {
		effect := simdEffects[lane.kind-InstrV128Load]
		effect.laneLimit = lane.limit
		simdEffects[lane.kind-InstrV128Load] = effect
	}
}

// IsSIMDValidationInstructionKind reports whether wasm validation admits kind
// as a SIMD instruction. It is allocation-free and shares the validator's
// compact effect table.
func IsSIMDValidationInstructionKind(kind InstrKind) bool {
	ensureSIMDEffects()
	return simdOpEffect(kind).cat != simdNone
}

// SIMDValidationInstructionKinds returns an immutable snapshot of the SIMD
// instruction kinds admitted by wasm validation. It is intended for downstream
// support/admission parity checks; callers receive a copy so the validator's
// internal tables cannot be mutated.
func SIMDValidationInstructionKinds() map[InstrKind]struct{} {
	ensureSIMDEffects()
	out := make(map[InstrKind]struct{}, 268)
	for kind := InstrV128Load; kind <= InstrI32x4RelaxedDotI8x16I7x16AddS; kind++ {
		if simdOpEffect(kind).cat != simdNone {
			out[kind] = struct{}{}
		}
	}
	return out
}
