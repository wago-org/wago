//go:build arm64

package arm64

// valueFacts is bounded semantic provenance carried directly by a Valent node.
// It shares one metadata byte with GC/EH root state, so facts neither allocate
// nor enlarge the operand-stack arena. Facts describe the value, not its current
// register or slot, and therefore survive materialization and spills.
type valueFacts uint8

const (
	factUpper32Zero valueFacts = 1 << iota
	factBoolean
	storageGCRoot
	storageEHRoot
	valueFactMask = factUpper32Zero | factBoolean
)

func (st *storage) setEHRoot(root bool) {
	if root {
		st.meta |= uint8(storageEHRoot)
	} else {
		st.meta &^= uint8(storageEHRoot)
	}
}
