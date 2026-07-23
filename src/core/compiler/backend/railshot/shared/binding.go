package shared

// ImportBinding describes imported-function lowering. Dynamic selects the
// per-instance dispatch table, while CrossInstance retains the legacy immediate
// binding used by focused backend tests and older low-level callers.
type ImportBinding struct {
	Dynamic       bool
	ImportIndex   uint32
	CrossInstance bool
	CalleeLinMem  uint64
	CalleeEntry   uint64
}
