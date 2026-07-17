package shared

// ImportBinding describes a link-time imported-function binding. The zero value
// selects host-import lowering; CrossInstance supplies the native context and
// wrapper entry needed by either architecture's direct-call lowering.
type ImportBinding struct {
	CrossInstance bool
	CalleeLinMem  uint64
	CalleeEntry   uint64
}
