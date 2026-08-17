package shared

// ValueFacts is bounded semantic provenance attached directly to Valent storage.
// It deliberately remains one byte so backends can use existing struct padding
// rather than allocating side tables on the ordinary path.
type ValueFacts uint8

const (
	ValueFactUpper32Zero ValueFacts = 1 << iota
	ValueFactBoolean
	ValueFactSignExt8
	ValueFactSignExt16
	ValueFactSignExt32
	ValueFactNonZero
	ValueFactI31
)

func (facts ValueFacts) Has(want ValueFacts) bool { return facts&want == want }

// MergeValueFacts keeps only facts true on every reachable predecessor.
func MergeValueFacts(a, b ValueFacts) ValueFacts { return a & b }

// ValueFactsForIntLoad returns facts established by a scalar integer load.
// Both native backends materialize i32 results through a 32-bit destination,
// which clears the physical register's upper half even for signed byte/word
// loads. Sign facts describe extension to the Wasm result width.
func ValueFactsForIntLoad(size int, signed, result64 bool) ValueFacts {
	var facts ValueFacts
	if !result64 {
		facts |= ValueFactUpper32Zero
	}
	if !signed {
		return facts
	}
	switch size {
	case 1:
		facts |= ValueFactSignExt8 | ValueFactSignExt16
		if result64 {
			facts |= ValueFactSignExt32
		}
	case 2:
		facts |= ValueFactSignExt16
		if result64 {
			facts |= ValueFactSignExt32
		}
	case 4:
		if result64 {
			facts |= ValueFactSignExt32
		}
	}
	return facts
}
