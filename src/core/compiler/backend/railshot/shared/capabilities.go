package shared

// Capabilities identifies coarse compiler subsystems present in this build.
// It is separate from optimization.Selection: capabilities describe whether an
// implementation exists, while selections choose among implementations that do.
type Capabilities uint64

const (
	CapabilityNativeCompaction Capabilities = 1 << iota
	CapabilityProducerNeedles
	CapabilityNativeGCOptimizations
)

const ProductionCapabilities = CapabilityNativeCompaction |
	CapabilityProducerNeedles |
	CapabilityNativeGCOptimizations

// CompiledCapabilities is the immutable capability mask for this binary.
const CompiledCapabilities = nativeCompactionCapability |
	producerNeedlesCapability |
	nativeGCOptimizationsCapability

// Has reports whether every requested capability is present.
func (c Capabilities) Has(requested Capabilities) bool { return c&requested == requested }
