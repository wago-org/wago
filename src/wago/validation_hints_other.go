//go:build !arm64 || wago_precompiled

package wago

import "github.com/wago-org/wago/src/core/compiler/wasm"

func railshotValidationHintFactory(*RuntimeConfig, int) wasm.ValidationHintFactory             { return nil }
func railshotAttachValidationHintProof(*railshotCompileOptions, *wasm.ValidatedModuleAnalysis) {}
