//go:build !wago_regalloccheck

package shared

import (
	"github.com/wago-org/wago/src/core/compiler/codegen"
	"github.com/wago-org/wago/src/core/compiler/wasm"
)

type SourceAttempt struct{}

func SetSourceContext(*ScalarState, *codegen.SourceContext)             {}
func BeginSourceAttempt(*ScalarState, *wasm.Module, int) *SourceAttempt { return nil }
func EndSourceAttempt(*SourceAttempt)                                   {}
func finishSourceWorker(*ScalarState)                                   {}
