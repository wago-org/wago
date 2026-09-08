package wago

import "fmt"

// Four element copies cover decoded/frozen storage and bounded scratch.
const artifactCollectionCopies = 4

type artifactDecodeBudget struct {
	remaining uint64
	limit     uint64
}

func newArtifactDecodeBudget(limit int64) *artifactDecodeBudget {
	if limit == 0 {
		limit = DefaultArtifactLimits().MaxDecodedBytes
	}
	return &artifactDecodeBudget{remaining: uint64(limit), limit: uint64(limit)}
}

func (b *artifactDecodeBudget) charge(count, width uint64) error {
	if width != 0 && count > b.remaining/width {
		return fmt.Errorf("compiled metadata exceeds decoded allocation limit %d", b.limit)
	}
	b.remaining -= count * width
	return nil
}

func (r *compiledReader) reserve(count, width uint64) error {
	if r.budget == nil {
		r.budget = newArtifactDecodeBudget(0)
	}
	return r.budget.charge(count, width)
}
