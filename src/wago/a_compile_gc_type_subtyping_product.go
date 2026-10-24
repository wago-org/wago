package wago

func (p stagedGCTypeSubtypingProduct) usesRefTest() bool {
	return p == stagedGCTypeSubtypingRefTestSingle || p == stagedGCTypeSubtypingRefTestMulti || p == stagedGCTypeSubtypingRefTestDirectionFalse
}

func (p stagedGCTypeSubtypingProduct) usesRuntimeFunctionIdentity() bool {
	return p == stagedGCTypeSubtypingRuntimeCallCast || p == stagedGCTypeSubtypingRuntimeFinalityCallCast || p == stagedGCTypeSubtypingRuntimeTypedTableCall || p == stagedGCTypeSubtypingNonFlatExport
}

func (p stagedGCTypeSubtypingProduct) usesLinkFunctionIdentity() bool {
	return p == stagedGCTypeSubtypingLinkProvider || p == stagedGCTypeSubtypingLinkConsumer || p == stagedGCTypeSubtypingFinalityLinkProvider || p == stagedGCTypeSubtypingFinalityLinkConsumer || p == stagedGCTypeSubtypingStructLinkProvider || p == stagedGCTypeSubtypingStructLinkConsumer || p == stagedGCTypeSubtypingStructProjectionLinkProvider || p == stagedGCTypeSubtypingStructProjectionLinkConsumer || p == stagedGCTypeSubtypingStructMismatchLinkProvider || p == stagedGCTypeSubtypingStructMismatchLinkConsumer || p == stagedGCTypeSubtypingIndependentStructLinkProvider || p == stagedGCTypeSubtypingIndependentStructLinkConsumer || p == stagedGCTypeSubtypingExtendedProjectionLinkProvider || p == stagedGCTypeSubtypingExtendedProjectionLinkConsumer || p == stagedGCTypeSubtypingDuplicateRecursiveLinkProvider || p == stagedGCTypeSubtypingDuplicateRecursiveLinkConsumer || p == stagedGCTypeSubtypingExtendedRecursiveLinkProvider || p == stagedGCTypeSubtypingExtendedRecursiveLinkConsumer || p == stagedGCTypeSubtypingCrossGroupMismatchLinkProvider || p == stagedGCTypeSubtypingCrossGroupMismatchLinkConsumer || p == stagedGCTypeSubtypingTransitiveMismatchLinkProvider || p == stagedGCTypeSubtypingTransitiveMismatchLinkConsumer
}
