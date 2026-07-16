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
	stagedGCArrayProductPackedData
	stagedGCArrayProductReferenceElements
	stagedGCArrayProductNullDereference
	stagedGCArrayProductNumericLocal
	stagedGCArrayProductBulkFill
	stagedGCArrayProductBulkCopy
	stagedGCArrayProductInitData
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
	case stagedGCArrayProductPackedData:
		return "packed-data"
	case stagedGCArrayProductReferenceElements:
		return "reference-elements"
	case stagedGCArrayProductNullDereference:
		return "null-dereference"
	case stagedGCArrayProductNumericLocal:
		return "numeric-local-helper"
	case stagedGCArrayProductBulkFill:
		return "bulk-fill"
	case stagedGCArrayProductBulkCopy:
		return "bulk-copy"
	case stagedGCArrayProductInitData:
		return "init-data"
	default:
		return "unknown"
	}
}

func (p stagedGCArrayProduct) requiresHelpers() bool {
	return p == stagedGCArrayProductNumericLocal || p == stagedGCArrayProductNumericDefault || p == stagedGCArrayProductNumericFixed || p == stagedGCArrayProductPackedData || p == stagedGCArrayProductReferenceElements || p == stagedGCArrayProductNullDereference || p == stagedGCArrayProductBulkFill || p == stagedGCArrayProductBulkCopy || p == stagedGCArrayProductInitData
}

func (p stagedGCArrayProduct) metadataOnly() bool {
	return p == stagedGCArrayProductDeclarations || p == stagedGCArrayProductBindings
}

func stagedGCArrayExecutionProduct(data []byte) (stagedGCArrayProduct, bool) {
	digest := fmt.Sprintf("%x", sha256.Sum256(data))
	switch {
	case len(data) == 146 && digest == stagedGCArrayNumericLocalSHA256:
		return stagedGCArrayProductNumericLocal, true
	case len(data) == 80 && digest == "995b6f4472185333316f224edf99518254df392aa1592239c2d9a0d81e2c052a":
		return stagedGCArrayProductDeclarations, true
	case len(data) == 55 && digest == "a812822a7372385725cb75c70f0c3cfa7b9cca83a2bb8306a752adc44dc546bd":
		return stagedGCArrayProductBindings, true
	case len(data) == 115 && digest == "b6446904a92663c6dc462e8c7f4b1a2077c7b942ce7be0fa053c32ecb990b96a":
		return stagedGCArrayProductNullDereference, true
	case len(data) == 250 && digest == "dff18bcf6b1ed6fdb6ae63692baa8e649e22794de7f4dbf3bc76e0f2b0f28898":
		return stagedGCArrayProductNumericDefault, true
	case len(data) == 268 && digest == "6ff5956b84b5035df8d3419edc8c67348cffd06d5a4cad86cfba56c415acbf25":
		return stagedGCArrayProductNumericFixed, true
	case len(data) == 351 && digest == "7fc4afb6a2e3b2f6b1562b4d0185b6d5d4426c579bcda44cce3b3a1401247bce":
		return stagedGCArrayProductPackedData, true
	case len(data) == 396 && digest == "19178a5db9c6ded41e185a9422c558a65d4bc1f11e7b0df11a776226f22812a9":
		return stagedGCArrayProductReferenceElements, true
	case len(data) == 183 && digest == "0893caa7ae7ab2d870329da9697d405a51592cb3ecc1b4b833780ef9b2580169":
		return stagedGCArrayProductBulkFill, true
	case len(data) == 402 && digest == "3ce0c22105571618832b6d97164a26e4b7dee035f540957422b887c4c04d4f35":
		return stagedGCArrayProductBulkCopy, true
	case len(data) == 335 && digest == "c17da56ed5c65083ee20023738cc5d9a36d1e301d2f06f23e2645d6ec8a9ca77":
		return stagedGCArrayProductInitData, true
	case len(data) == 435 && digest == "05827a01cec2e9f3623e9d00b54aff258bbc7b497f47b76ffd31452bbcb9b31f":
		return stagedGCArrayProductInitData, true
	default:
		return 0, false
	}
}
