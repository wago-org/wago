package wago

func (p stagedGCArrayProduct) requiresHelpers() bool {
	return p == stagedGCArrayProductNumericLocal || p == stagedGCArrayProductNumericUniform || p == stagedGCArrayProductNumericFixedLocal || p == stagedGCArrayProductNumericDefault || p == stagedGCArrayProductNumericFixed || p == stagedGCArrayProductPackedData || p == stagedGCArrayProductReferenceElements || p == stagedGCArrayProductNullDereference || p == stagedGCArrayProductBulkFill || p == stagedGCArrayProductBulkCopy || p == stagedGCArrayProductInitData || p == stagedGCArrayProductInitElem || p == stagedGCArrayProductNewData || p == stagedGCArrayProductNewElem || p == stagedGCArrayProductGeneric
}
