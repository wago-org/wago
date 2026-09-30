//go:build linux && amd64 && tinygo

package amd64

import "testing"

// Instruction decoding is performed by the standard-Go suite using objdump.
// TinyGo runs the same semantic vectors without depending on os/exec support.
func assertSIMDBaseline(_ *testing.T, _ []byte) {}
