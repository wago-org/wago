package runtime

// PreparedIntCall is one non-concurrent prepared integer activation block.
// The owning PreparedFunction fixes code, linMem, and stack once; each entry
// updates only arguments. Fields are deliberately private so only
// Engine can establish the foreign-stack transition contract.
type PreparedIntCall struct {
	code, linMem, stack uintptr
	a0, a1, a2, a3      uintptr
}
