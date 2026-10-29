//go:build linux && riscv64 && !tinygo && !wago_target_tinygo

package wago

import (
	railshot "github.com/wago-org/wago/src/core/compiler/backend/railshot/riscv64"
	"github.com/wago-org/wago/src/core/compiler/backend/railshot/shared"
)

// The canonical image contract binds code, frame entries, types and tags.
// Collector payloads also require native GC ABI and exact retained frame maps.
func rv64ExceptionHandling(c *Compiled) bool {
	if c == nil || !c.requiredFeatures.IsEnabled(CoreFeatureExceptionHandling) || !c.nativeContextTrapABI || !c.needsFuncRefContextHeader || !rv64SourceContractValid(c) {
		return false
	}
	if c.memoryDir == nil {
		return true
	}
	if len(c.memoryDir.ehTags) > railshot.MaxFunctions {
		return false
	}
	for _, tag := range c.memoryDir.ehTags {
		if int(tag.TypeIndex) >= len(c.Types) {
			return false
		}
		sig := c.Types[tag.TypeIndex]
		if len(sig.Results) != 0 || len(sig.Params) > shared.MaxEHTagPayloadWords {
			return false
		}
		numericWords := len(sig.Params)
		for _, typ := range sig.Params {
			abi, ok := typ.ABIType(c.Types)
			if !ok {
				return false
			}
			switch abi {
			case ValI32, ValI64, ValF32, ValF64, ValFuncRef, ValExternRef:
			case ValExnRef:
				if !c.needsOwnedExceptionReferences() || c.nativeGCABIRequirement() == 0 || c.genericGCFrameRoots() == nil {
					return false
				}
			case ValV128:
				numericWords++
			case ValAnyRef, ValI31Ref:
				if !c.requiredFeatures.IsEnabled(CoreFeatureGC) || c.nativeGCABIRequirement() == 0 || c.genericGCFrameRoots() == nil {
					return false
				}
			default:
				return false
			}
		}
		if numericWords > shared.MaxEHTagPayloadWords {
			return false
		}
	}
	for _, index := range c.memoryDir.ehTagExports {
		if index < 0 || index >= len(c.memoryDir.ehTags) {
			return false
		}
	}
	return true
}
