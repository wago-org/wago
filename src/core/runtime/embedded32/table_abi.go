package embedded32

// TableABI is one fixed target-side descriptor in the context's indexed table
// directory. Table entries contain zero for null or a function identity plus
// one. The three function arrays index entry, structural type, and owning module
// context by the decoded bundle-wide identity.
type TableABI struct {
	EntriesBase          uint32
	Length               uint32
	Maximum              uint32
	FunctionEntriesBase  uint32
	FunctionTypesBase    uint32
	FunctionContextsBase uint32
	ElementSegmentsBase  uint32
	ElementSegmentCount  uint32
}

const (
	TableABIEntriesBaseOffset          = 0
	TableABILengthOffset               = 4
	TableABIMaximumOffset              = 8
	TableABIFunctionEntriesBaseOffset  = 12
	TableABIFunctionTypesBaseOffset    = 16
	TableABIFunctionContextsBaseOffset = 20
	TableABIElementSegmentsBaseOffset  = 24
	TableABIElementSegmentCountOffset  = 28
	TableABIBytes                      = 32
)
