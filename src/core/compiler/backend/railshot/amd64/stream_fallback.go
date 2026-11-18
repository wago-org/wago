//go:build amd64 && (tinygo || wago_profile)

package amd64

import (
	"fmt"
	"github.com/wago-org/wago/src/core/compiler/wasm"
)

var streamingOperandsEnabled = false

type streamState struct{}

func (*streamState) FinishWorker()                    {}
func (*streamState) Memory() uint64                   { return 0 }
func (f *fn) admitStreaming(*wasm.Func) streamSummary { return streamSummary{} }
func (f *fn) streamBody(*wasm.Func) error {
	return fmt.Errorf("streaming operands unavailable in this build")
}
