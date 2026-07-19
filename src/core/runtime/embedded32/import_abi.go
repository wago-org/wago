package embedded32

// ImportFunctionABI describes one imported function target. Context is the
// callee's ContextABI address, allowing linked modules to switch state while
// preserving the serialized internal function-call ABI.
type ImportFunctionABI struct {
	Entry   uint32
	Context uint32
}

const (
	ImportFunctionEntryOffset   = 0
	ImportFunctionContextOffset = 4
	ImportFunctionABIBytes      = 8
)
