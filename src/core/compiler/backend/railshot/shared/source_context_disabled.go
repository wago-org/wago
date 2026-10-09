//go:build !wago_regalloccheck

package shared

import (
	"github.com/wago-org/wago/src/core/compiler/codegen"
	"github.com/wago-org/wago/src/core/compiler/wasm"
)

type SourceAttempt struct{}

func PrepareSourceWorker(*ScalarState, int, int) {}

func SetSourceContext(*ScalarState, *codegen.SourceContext)                 {}
func SetSourceWorkerContext(*ScalarState, *codegen.SourceContext, int, int) {}
func BeginSourceAttempt(*ScalarState, *wasm.Module, int) *SourceAttempt     { return nil }
func EndSourceAttempt(*SourceAttempt)                                       {}
func finishSourceWorker(*ScalarState)                                       {}
