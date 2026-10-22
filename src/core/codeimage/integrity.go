package codeimage

import "crypto/sha256"

// IntegrityDigest authenticates the immutable logical span of one emitted
// native-code object. It is code-ownership metadata, never a workload selector.
type IntegrityDigest [sha256.Size]byte

// Digest computes the integrity identity of emitted native code.
func Digest(code []byte) IntegrityDigest { return sha256.Sum256(code) }
