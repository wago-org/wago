package wago

// compactNativeCode drops a disproportionately large speculative compiler
// backing array before it becomes part of a reusable Compiled product. Railshot
// emits directly into a bounded tail for the common path; keeping that tail as
// public Compiled.Code capacity would otherwise retain up to 1 MiB even for a
// tiny module. Avoid a second copy unless it recovers at least half the backing
// allocation, keeping the transient copy cost bounded by the saved footprint.
func compactNativeCode(code []byte) []byte {
	if len(code) == 0 || cap(code) <= len(code)*2 {
		return code
	}
	return append([]byte(nil), code...)
}
