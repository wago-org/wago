package embedded32

// TableABI is the fixed target-side descriptor for the single-table embedded
// profile. Table entries contain zero for null or function-index-plus-one for a
// local funcref. FunctionEntriesBase and FunctionTypesBase index parallel arrays
// by the decoded zero-based function index.
type TableABI struct {
	EntriesBase         uint32
	Length              uint32
	Maximum             uint32
	FunctionEntriesBase uint32
	FunctionTypesBase   uint32
	ElementSegmentsBase uint32
	ElementSegmentCount uint32
}

const (
	TableABIEntriesBaseOffset         = 0
	TableABILengthOffset              = 4
	TableABIMaximumOffset             = 8
	TableABIFunctionEntriesBaseOffset = 12
	TableABIFunctionTypesBaseOffset   = 16
	TableABIElementSegmentsBaseOffset = 20
	TableABIElementSegmentCountOffset = 24
	TableABIBytes                     = 28
)
