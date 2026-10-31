//go:build linux && amd64 && !tinygo && !wago_target_tinygo

package amd64

import (
	"os"
	"runtime"

	"github.com/wago-org/wago/src/core/compiler/backend/railshot/shared"
	coreruntime "github.com/wago-org/wago/src/core/runtime"
)

func newOperandArena() *operandArena {
	if os.Getenv("WAGO_MAPPED_OPERAND_ARENA") == "0" {
		return nil
	}
	buffer, err := coreruntime.NewCodeBuffer(shared.MaxRetainedStackArenaBytes)
	if err != nil {
		return nil
	}
	runtime.SetFinalizer(buffer, func(owner *operandArena) { _ = owner.Close() })
	return buffer
}
