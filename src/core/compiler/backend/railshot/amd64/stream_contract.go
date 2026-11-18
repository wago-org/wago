//go:build amd64

package amd64

// streamSummary is admission metadata, not an instruction array. Admission is
// performed on validated bytecode before any function instruction is emitted.
type streamSummary struct {
	Eligible, HasIf          bool
	ResultN                  int
	ResultWide, Memory, Bulk bool
	MaxControlDepth          uint8
	MaxStack, Nodes          int
}
