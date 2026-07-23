//go:build amd64 && tinygo

package amd64

// TinyGo does not link Go assembly declarations. Keep its existing AVX2 path;
// semantic behavior is unchanged and ordinary Go builds still auto-select ZMM.
func hostSupportsAVX512() bool         { return false }
func hostPrefersFullWidthAVX512() bool { return false }
