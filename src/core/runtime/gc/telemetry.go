package gc

import "runtime"

// TelemetrySchemaVersion identifies the stable machine-readable collector
// telemetry schema. Additive fields do not require a version change; incompatible
// unit or meaning changes do.
const TelemetrySchemaVersion uint32 = 1

// TelemetryAvailable reports whether collector timing/counters and JSON reports
// were compiled into this binary with wago_gcstats.
func TelemetryAvailable() bool { return collectorTelemetryEnabled }

const telemetryPauseSubBuckets = 16
const telemetryPauseBuckets = 1 + 63*telemetryPauseSubBuckets

// RootClass identifies one exact collector root source. RootSet values without
// an explicit classification are reported as native-frame roots because that is
// the allocation-safepoint use of the public root argument.
type RootClass uint8

const (
	RootNativeFrame RootClass = iota
	RootGlobal
	RootTable
	RootPublicToken
	RootForeignInstance
	RootSnapshotTemporary
	rootClassCount
)

// PhaseTelemetry reports cumulative phase wall time. ReferenceScanningNS is
// removed from the enclosing tracing/marking phase, so the fields remain
// additive rather than double-counting object-slot scans.
type PhaseTelemetry struct {
	RootEnumerationNS         uint64 `json:"root_enumeration_ns"`
	PersistentRootsNS         uint64 `json:"persistent_roots_ns"`
	NativeFrameRootsNS        uint64 `json:"native_frame_roots_ns"`
	RememberedRootsNS         uint64 `json:"remembered_roots_ns"`
	TracingNS                 uint64 `json:"tracing_ns"`
	MarkingNS                 uint64 `json:"marking_ns"`
	ReferenceScanningNS       uint64 `json:"reference_scanning_ns"`
	PromotionCopyNS           uint64 `json:"promotion_copy_ns"`
	SweepNS                   uint64 `json:"sweep_ns"`
	FreeSpaceReconstructionNS uint64 `json:"free_space_reconstruction_ns"`
	FragmentationRecoveryNS   uint64 `json:"fragmentation_recovery_ns"`
	MetadataCleanupNS         uint64 `json:"metadata_cleanup_ns"`
}

// PauseTelemetry is a bounded-histogram summary. Percentiles are upper bounds
// of fixed logarithmic buckets and therefore remain allocation-free to record.
type PauseTelemetry struct {
	Count uint64 `json:"count"`
	P50NS uint64 `json:"p50_ns"`
	P90NS uint64 `json:"p90_ns"`
	P95NS uint64 `json:"p95_ns"`
	P99NS uint64 `json:"p99_ns"`
	MaxNS uint64 `json:"max_ns"`
}

// RootTelemetry separates root counts and enumeration time by ownership class.
type RootTelemetry struct {
	NativeFrames        uint64 `json:"native_frames"`
	Globals             uint64 `json:"globals"`
	Tables              uint64 `json:"tables"`
	PublicTokens        uint64 `json:"public_tokens"`
	ForeignInstances    uint64 `json:"foreign_instances"`
	SnapshotTemporaries uint64 `json:"snapshot_temporaries"`
	NativeFrameNS       uint64 `json:"native_frame_ns"`
	GlobalNS            uint64 `json:"global_ns"`
	TableNS             uint64 `json:"table_ns"`
	PublicTokenNS       uint64 `json:"public_token_ns"`
	ForeignInstanceNS   uint64 `json:"foreign_instance_ns"`
	SnapshotTemporaryNS uint64 `json:"snapshot_temporary_ns"`
}

// TraceTelemetry counts deterministic collector work.
type TraceTelemetry struct {
	ObjectsVisited        uint64 `json:"objects_visited"`
	PayloadBytesVisited   uint64 `json:"payload_bytes_visited"`
	ReferenceSlotsVisited uint64 `json:"reference_slots_visited"`
	ObjectsSwept          uint64 `json:"objects_swept"`
	PayloadBytesSwept     uint64 `json:"payload_bytes_swept"`
}

// NurseryTelemetry reports nursery allocation and first-survival behavior. The
// current collector promotes every first survivor, so age bucket one equals the
// promoted population and later buckets remain zero until adaptive tenuring is
// implemented.
type NurseryTelemetry struct {
	AllocatedObjects uint64    `json:"allocated_objects"`
	AllocatedBytes   uint64    `json:"allocated_bytes"`
	SurvivedObjects  uint64    `json:"survived_objects"`
	SurvivedBytes    uint64    `json:"survived_bytes"`
	PromotedObjects  uint64    `json:"promoted_objects"`
	PromotedBytes    uint64    `json:"promoted_bytes"`
	CopiedBytes      uint64    `json:"copied_bytes"`
	AgeObjects       [8]uint64 `json:"age_objects"`
	AgeBytes         [8]uint64 `json:"age_bytes"`
}

// CardTelemetry reports fixed-card and dirty-root work for Throughput minor
// collection, including explicit whole-object fallback scans.
type CardTelemetry struct {
	DirtyObjectCards        uint64 `json:"dirty_object_cards"`
	DirtyRootCards          uint64 `json:"dirty_root_cards"`
	UsefulObjectCards       uint64 `json:"useful_object_cards"`
	UsefulRootCards         uint64 `json:"useful_root_cards"`
	DuplicateDirties        uint64 `json:"duplicate_dirties"`
	ScannedSlots            uint64 `json:"scanned_slots"`
	WholeObjectScans        uint64 `json:"whole_object_scans"`
	WholeObjectScansAvoided uint64 `json:"whole_object_scans_avoided"`
	ClearedCards            uint64 `json:"cleared_cards"`
}

// CollectionTelemetry aggregates one collection kind across all completed
// cycles, including failed transactional cycles.
type CollectionTelemetry struct {
	Cycles       uint64           `json:"cycles"`
	FailedCycles uint64           `json:"failed_cycles"`
	TotalNS      uint64           `json:"total_ns"`
	Pause        PauseTelemetry   `json:"pause"`
	Phases       PhaseTelemetry   `json:"phases"`
	Roots        RootTelemetry    `json:"roots"`
	Trace        TraceTelemetry   `json:"trace"`
	Nursery      NurseryTelemetry `json:"nursery"`
	Cards        CardTelemetry    `json:"cards"`
}

// PathTelemetry counts dynamic allocation, collector, and metadata paths. Native
// fast allocations are derived from the collector's authoritative allocation
// counter minus successful Go allocation paths.
type PathTelemetry struct {
	NativeFastAllocations  uint64 `json:"native_fast_allocations"`
	GoAllocationPaths      uint64 `json:"go_allocation_paths"`
	GoHelperTransitions    uint64 `json:"go_helper_transitions"`
	ConditionalMediumPaths uint64 `json:"conditional_medium_paths"`
	CardGrowths            uint64 `json:"card_growths"`
	HandleRefills          uint64 `json:"handle_refills"`
	NurseryExhaustions     uint64 `json:"nursery_exhaustions"`
	MinorCollections       uint64 `json:"minor_collections"`
	FullCollections        uint64 `json:"full_collections"`
	BackingGrowths         uint64 `json:"backing_growths"`
	BackingBytesCopied     uint64 `json:"backing_bytes_copied"`
}

// ManagedHeapTelemetry reports current WasmGC memory and retained metadata.
// OccupancyHistogram buckets are 0-9%, 10-19%, ..., 90-99%, and 100%.
type ManagedHeapTelemetry struct {
	LiveObjects        uint64     `json:"live_objects"`
	LiveBytes          uint64     `json:"live_bytes"`
	AllocatedBytes     uint64     `json:"allocated_bytes"`
	CommittedBytes     uint64     `json:"committed_bytes"`
	ReservedBytes      uint64     `json:"reserved_bytes"`
	FreeBytes          uint64     `json:"free_bytes"`
	LargestFreeBytes   uint64     `json:"largest_free_bytes"`
	FreeSpanCount      uint64     `json:"free_span_count"`
	MetadataBytes      uint64     `json:"metadata_bytes"`
	FragmentationPPM   uint64     `json:"fragmentation_ppm"`
	OccupancyHistogram [11]uint64 `json:"occupancy_histogram"`
}

// TelemetrySnapshot is the collector-side portion of a benchmark report. Its
// work counters are deterministic for a fixed fixture; pause and phase timing is
// host-dependent. It is safe to JSON-marshal directly.
type TelemetrySnapshot struct {
	SchemaVersion uint32               `json:"schema_version"`
	Profile       Profile              `json:"profile"`
	Minor         CollectionTelemetry  `json:"minor"`
	Full          CollectionTelemetry  `json:"full"`
	Paths         PathTelemetry        `json:"paths"`
	Heap          ManagedHeapTelemetry `json:"managed_heap"`
}

// MemoryDomains keeps compiler, runtime, managed, executable, and process memory
// distinct. CompilerHeapBytes and ExecutableJITBytes are supplied by the product
// benchmark at the lifecycle boundaries where those domains are attributable.
type MemoryDomains struct {
	GoCompilerHeapBytes uint64 `json:"go_compiler_heap_bytes"`
	GoRuntimeHeapBytes  uint64 `json:"go_runtime_heap_bytes"`
	WasmManagedBytes    uint64 `json:"wasm_managed_bytes"`
	ExecutableJITBytes  uint64 `json:"executable_jit_bytes"`
	PeakRSSBytes        uint64 `json:"peak_rss_bytes"`
}

// Add merges independently measured dynamic path counters into p.
func (p *PathTelemetry) Add(other PathTelemetry) {
	if p == nil {
		return
	}
	p.NativeFastAllocations += other.NativeFastAllocations
	p.GoAllocationPaths += other.GoAllocationPaths
	p.GoHelperTransitions += other.GoHelperTransitions
	p.ConditionalMediumPaths += other.ConditionalMediumPaths
	p.CardGrowths += other.CardGrowths
	p.HandleRefills += other.HandleRefills
	p.NurseryExhaustions += other.NurseryExhaustions
	p.MinorCollections += other.MinorCollections
	p.FullCollections += other.FullCollections
	p.BackingGrowths += other.BackingGrowths
	p.BackingBytesCopied += other.BackingBytesCopied
}

// NativeCodeTelemetry attributes generated native bytes. Producers may report
// zero for unsupported categories, but must keep categories separate rather than
// folding them into TotalBytes.
type NativeCodeTelemetry struct {
	TotalBytes            uint64 `json:"total_bytes"`
	AllocationBytes       uint64 `json:"allocation_bytes"`
	HandleResolutionBytes uint64 `json:"handle_resolution_bytes"`
	TypeCastBytes         uint64 `json:"type_cast_bytes"`
	NullCheckBytes        uint64 `json:"null_check_bytes"`
	BoundsCheckBytes      uint64 `json:"bounds_check_bytes"`
	BarrierBytes          uint64 `json:"barrier_bytes"`
	SpillReloadBytes      uint64 `json:"spill_reload_bytes"`
	HelperCallBytes       uint64 `json:"helper_call_bytes"`
	SharedStubBytes       uint64 `json:"shared_stub_bytes"`
	TrapStubBytes         uint64 `json:"trap_stub_bytes"`
	RootMapBytes          uint64 `json:"root_map_bytes"`
}

// Add merges native-code attribution from multiple modules or architectures.
func (n *NativeCodeTelemetry) Add(other NativeCodeTelemetry) {
	if n == nil {
		return
	}
	n.TotalBytes += other.TotalBytes
	n.AllocationBytes += other.AllocationBytes
	n.HandleResolutionBytes += other.HandleResolutionBytes
	n.TypeCastBytes += other.TypeCastBytes
	n.NullCheckBytes += other.NullCheckBytes
	n.BoundsCheckBytes += other.BoundsCheckBytes
	n.BarrierBytes += other.BarrierBytes
	n.SpillReloadBytes += other.SpillReloadBytes
	n.HelperCallBytes += other.HelperCallBytes
	n.SharedStubBytes += other.SharedStubBytes
	n.TrapStubBytes += other.TrapStubBytes
	n.RootMapBytes += other.RootMapBytes
}

// BenchmarkConfiguration describes the stable dimensions used to construct one
// A/B workload. Fields not relevant to a workload remain zero/empty.
type BenchmarkConfiguration struct {
	Profile            string `json:"profile,omitempty"`
	Collection         string `json:"collection,omitempty"`
	Layout             string `json:"layout,omitempty"`
	SurvivalPercent    uint32 `json:"survival_percent,omitempty"`
	Objects            uint64 `json:"objects,omitempty"`
	RootClass          string `json:"root_class,omitempty"`
	Roots              uint64 `json:"roots,omitempty"`
	StaticSites        uint64 `json:"static_sites,omitempty"`
	DynamicAllocations uint64 `json:"dynamic_allocations,omitempty"`
}

// BenchmarkTelemetryReport is the stable A/B interchange format. Host-specific
// timing and memory fields remain separate from deterministic collector counters.
type BenchmarkTelemetryReport struct {
	SchemaVersion    uint32                 `json:"schema_version"`
	Name             string                 `json:"name"`
	GOOS             string                 `json:"goos"`
	GOARCH           string                 `json:"goarch"`
	GoVersion        string                 `json:"go_version"`
	Commit           string                 `json:"commit,omitempty"`
	Configuration    BenchmarkConfiguration `json:"configuration"`
	Collector        TelemetrySnapshot      `json:"collector"`
	Memory           MemoryDomains          `json:"memory"`
	NativeCode       NativeCodeTelemetry    `json:"native_code"`
	LinkedBytes      uint64                 `json:"linked_bytes"`
	CompileNS        uint64                 `json:"compile_ns"`
	MutatorNS        uint64                 `json:"mutator_ns"`
	Operations       uint64                 `json:"operations"`
	BytesPerOp       uint64                 `json:"bytes_per_op"`
	AllocsPerOp      uint64                 `json:"allocs_per_op"`
	SemanticChecksum uint64                 `json:"semantic_checksum"`
	ExpectedTrap     string                 `json:"expected_trap,omitempty"`
}

// NewBenchmarkTelemetryReport fills stable host identity fields.
func NewBenchmarkTelemetryReport(name string) BenchmarkTelemetryReport {
	return BenchmarkTelemetryReport{
		SchemaVersion: TelemetrySchemaVersion,
		Name:          name,
		GOOS:          runtime.GOOS,
		GOARCH:        runtime.GOARCH,
		GoVersion:     runtime.Version(),
	}
}
