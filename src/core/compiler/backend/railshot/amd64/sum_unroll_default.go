//go:build amd64 && !wago_sumunroll

package amd64

// This wrapper inlines to the existing emitter in normal builds.
func (f *fn) trySelectedLinearSumLatch(loop *ctrlFrame, counter int) bool {
	return f.tryUnrolledLinearSumLatch(loop, counter)
}
