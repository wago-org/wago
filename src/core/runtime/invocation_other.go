//go:build amd64 || arm64

package runtime

func (*Engine) prepareNativeInvocation([]byte, []byte) {}
func (*Engine) resetNativeInvocation()                 {}

// TinyGo stores its generated entry address in e.stackTop. Resume needs the
// physical foreign-stack top instead, which remains derivable from e.stack.
func (e *Engine) nativeResumeStackTop() uintptr { return e.StackTop() }
