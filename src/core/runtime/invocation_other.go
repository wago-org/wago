//go:build amd64 || arm64

package runtime

func (*Engine) prepareNativeInvocation([]byte, []byte) {}
func (*Engine) resetNativeInvocation()                 {}
