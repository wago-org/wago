package x64

// ctrlFrame is a control-flow frame (block / loop / if / the function frame).
// Phase 0 defines only the shell; the block-param/result plumbing, branch merge
// points, and the spill-at-boundary logic (WARP's finalizeBlock / emitBranch /
// RegisterCopyResolver merge) land in Phase 3.
type ctrlFrame struct {
	kind    ctrlKind
	height  int // operand-stack height at frame entry
	results int // number of result values the frame yields
}

type ctrlKind uint8

const (
	cfFunc ctrlKind = iota
	cfBlock
	cfLoop
	cfIf
)
