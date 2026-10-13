//go:build arm64

package arm64

import (
	"fmt"
	"github.com/wago-org/wago/src/core/compiler/wasm"
)

// emitCheckedLoop consumes a boolean guard already on the operand stack. Both
// paths use ordinary control convergence and the actual source positions. The
// false arm compiles the original loop, preserving traps and partial effects.
func (f *fn) emitCheckedLoop(r *wasm.Reader, end int, emitFast func() error) error {
	ifReader := *r
	if err := f.opBlock(&ifReader, 0x04); err != nil {
		return err
	}
	if err := emitFast(); err != nil {
		return err
	}
	if err := f.opElse(); err != nil {
		return err
	}
	depth := len(f.ctrl)
	if err := f.opBlock(r, 0x03); err != nil {
		return err
	}
	if err := f.bodyLoop(r, depth); err != nil {
		return err
	}
	if r.Offset() != end {
		return fmt.Errorf("arm64: checked vector loop source end mismatch")
	}
	return f.opEnd(r)
}
