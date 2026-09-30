package gc

type failurePoint uint8

const (
	failPromotionPlan failurePoint = iota + 1
	failPromotionDestination
	failPromotionCommit
	failHandlePublication
	failObjectCardGrowth
	failSlotCardGrowth
	failBackingGrowth
	failThroughputReconciliation
)
