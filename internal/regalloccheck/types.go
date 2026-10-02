// Package regalloccheck describes the deliberately small allocation-transfer
// vocabulary used by debug compiler checks. Ordinary builds contain only stubs.
package regalloccheck

type Bank uint8

const (
	GP Bank = iota
	FP
	Frame
	Unknown // an untracked machine address; never an input fact
)

// Location names a physical register byte or a byte in the current native frame.
// GP and FP registers deliberately use disjoint namespaces.
type Location struct {
	Bank  Bank
	Index int32
	Byte  uint8
}

func Register(bank Bank, index uint8) Location { return Location{Bank: bank, Index: int32(index)} }
func Slot(offset int32) Location               { return Location{Bank: Frame, Index: offset} }

// Value is a snapshot of symbolic bytes. It is independent of allocator owners.
type Value []cell

type Kind uint8

const (
	Copy Kind = iota
	Swap
	Kill
	Call
	Read // an observed folded machine input; no state mutation
)

// Effect is an emitted physical transfer, rather than an allocator decision.
// Calls invalidate registers but leave this function's frame untouched.
type Effect struct {
	Kind     Kind
	Dst, Src Location
	Size     int
	ClearTo  int // destination bytes [Size, ClearTo) lose their old identities
}
