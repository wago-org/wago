package wago

import (
	"crypto/sha256"
	"fmt"
)

// stagedGCArrayProduct identifies exact compile-only gc/array products. It is
// deliberately separate from stagedGCStructProduct: array metadata, helpers,
// roots, and public ownership must be proven independently.
type stagedGCArrayProduct uint8

const (
	stagedGCArrayProductDeclarations stagedGCArrayProduct = iota + 1
	stagedGCArrayProductBindings
	stagedGCArrayProductNumericDefault
	stagedGCArrayProductNumericFixed
	stagedGCArrayProductNullDereference
	stagedGCArrayProductNumericLocal
)

const stagedGCArrayNumericLocalSHA256 = "cfa515e66b094db434e59a3bbd21b66e99f391aaa52614e2fa1a5fec4f0e7b3b"

func (p stagedGCArrayProduct) String() string {
	switch p {
	case stagedGCArrayProductDeclarations:
		return "declarations"
	case stagedGCArrayProductBindings:
		return "bindings"
	case stagedGCArrayProductNumericDefault:
		return "numeric-default"
	case stagedGCArrayProductNumericFixed:
		return "numeric-fixed"
	case stagedGCArrayProductNullDereference:
		return "null-dereference"
	case stagedGCArrayProductNumericLocal:
		return "numeric-local-helper"
	default:
		return "unknown"
	}
}

func (p stagedGCArrayProduct) requiresHelpers() bool {
	return p == stagedGCArrayProductNumericLocal || p == stagedGCArrayProductNumericDefault || p == stagedGCArrayProductNumericFixed || p == stagedGCArrayProductNullDereference
}

func (p stagedGCArrayProduct) metadataOnly() bool {
	return p == stagedGCArrayProductDeclarations || p == stagedGCArrayProductBindings
}

func stagedGCArrayExecutionProduct(data []byte) (stagedGCArrayProduct, bool) {
	digest := fmt.Sprintf("%x", sha256.Sum256(data))
	if len(data) == 146 && digest == stagedGCArrayNumericLocalSHA256 {
		return stagedGCArrayProductNumericLocal, true
	}
	return 0, false
}
