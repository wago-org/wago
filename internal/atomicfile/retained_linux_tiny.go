//go:build linux && tinygo && wago_lean && wago_minimal

package atomicfile

import "os"

// The TinyGo minimal runtime registers only the run command, and every linked
// atomicfile caller uses ordinary replacement. Keep an unexpected build-only
// retention request fail-closed without linking the much larger private-stage
// implementation into this size-constrained profile.
type retainedReplaceHandle struct{}

func createRetainedReplacementTemp(string, bool) (*os.File, retainedReplaceHandle, error) {
	return nil, retainedReplaceHandle{}, newError("retained atomic staging is unavailable")
}

func (retainedReplaceHandle) valid() bool          { return false }
func (retainedReplaceHandle) replace(string) error { return newError("missing retained replacement") }
func (retainedReplaceHandle) remove() error        { return nil }
func (retainedReplaceHandle) close() error         { return nil }
