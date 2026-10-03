//go:build !tinygo

package railshot

import (
	"bytes"
	"testing"
)

func TestInternalCompilerErrorCapturesBoundedStack(t *testing.T) {
	ice := NewInternalCompilerError("amd64", 0, -1, "invariant")
	if len(ice.Stack) == 0 || !bytes.Contains(ice.Stack, []byte("TestInternalCompilerErrorCapturesBoundedStack")) {
		t.Fatalf("stack context missing: %q", ice.Stack)
	}
}
