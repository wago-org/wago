package wago

func snapshotLimitError(limit uint64) error {
	return &ResourceLimitError{Scope: "compiled snapshot", Resource: "metadata bytes", Requested: limit + 1, Limit: limit}
}

type snapshotBudget struct {
	remaining, limit uint64
	err              error
}
