package railssa

import "fmt"

// BudgetError reports a valid input that exceeds one bounded compiler data
// structure. It is distinct from unsupported Wasm semantics and verifier
// failures so the engine boundary can apply an explicit recovery policy.
type BudgetError struct {
	Resource string
	Required uint64
	Limit    uint64
}

func (e *BudgetError) Error() string {
	return fmt.Sprintf("railssa: %s requires %d, exceeds budget %d", e.Resource, e.Required, e.Limit)
}
