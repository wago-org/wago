package embedded32

// CallABI is the conventional firmware boundary for generated exported-function
// entry thunks. Parameters and results point to little-endian serialized 32-bit
// slots using the module metadata widths. The thunk returns a Trap value and
// writes Results only when that value is TrapNone.
type CallABI struct {
	Context    uint32
	Parameters uint32
	Results    uint32
}

const (
	CallABIContextOffset    = 0
	CallABIParametersOffset = 4
	CallABIResultsOffset    = 8
	CallABIBytes            = 12
)
