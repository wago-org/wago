package railmach

import "fmt"

// BudgetError reports valid machine input that exceeds one bounded compact
// representation. Verifier and semantic errors deliberately use other types.
type BudgetError struct {
	Resource string
	Required uint64
	Limit    uint64
}

func (e *BudgetError) Error() string {
	return fmt.Sprintf("railmach: %s requires %d, exceeds budget %d", e.Resource, e.Required, e.Limit)
}
