// Package codeimage defines the ownership boundary between native-code
// producers and the runtime that seals and executes their output.
package codeimage

// Image transfers an unsealed machine-code mapping from a producer to its
// runtime owner. Take succeeds once; Close releases an image not yet taken.
type Image interface {
	Take() (mapping []byte, base uintptr, err error)
	Close() error
}
